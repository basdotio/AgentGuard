<!-- SPDX-License-Identifier: MIT -->
# 034 — A judge reply whose object closes one member early fails the call: an answered check is reported as not run

- **Source**: new finding (2026-10-10), measured on a judge benchmark run; follow-up to P-001 (the judge block's counts) and P-033 (a sample is a complete judge measurement only when every call succeeded)
- **Depends on**: none
- **Branch**: `p/034-judge-reply-closed-early`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`parseVerdict` (`internal/judge/openai.go`) reads a model reply by slicing it from the first `{` to the last `}` and
handing that slice to `json.Unmarshal`. A reply in which the model closed the object one member early with a stray `}`
and then went on writing the last member has this shape:

    {"flagged": false, "severity": "low", "summary": "…", "evidence": "…"}, "barrier_evidence": ""}

Every member is there and the answer is unambiguous, but the slice is not one JSON value, so the call fails with
`parse judge verdict: invalid character ',' after top-level value`. It is not retried (a parse error is not a transport
error), it counts in the judge block's `failed`, and the report's `LLM-000` note says "those checks did not run".

Measured on 2026-10-10 against an OpenAI-compatible endpoint (`gpt-4.1-mini-2025-04-14`, temperature 0.8, the
`samples: 3` path) over corpus samples: 5 of 1,482 calls (0.34%) failed with exactly that error, and the one captured
reply had the shape above (its `evidence` was 2,944 characters copied verbatim from the data block).

Consequences:

- **A BYO user loses checks the model did answer.** The `LLM-000` note tells them the judge was partly blind on an
  artifact when it was not; with `--fail-on-llm`, a run that ran short exits 4 (P-026), so a formatting slip in one
  reply fails a pipeline.
- **A benchmark run loses whole samples.** A sample is a complete judge measurement only when all its calls succeeded
  (P-033). At 0.3–0.6% per call, 3–6% of skill samples (about 6 calls each) and most large hook samples (30–200 calls)
  are incomplete because of this shape alone.

A related hazard sits in the same function: when the continuation has no closing `}` at all
(`{"flagged": false, …}, "flagged": true`), the slice ends at the first object's `}` and today's code accepts the first
half alone. The second half can carry a different `flagged` or a `barrier_evidence`, and taking the first half silently
turns a judge answer into a different one.

### Measured on `origin/main` (8c62593), synthetic replies

`parseVerdict` called directly on replies built here (none is a captured reply; the captured one quotes a third-party
corpus sample and is not committed):

| Reply shape | Today |
|---|---|
| `{"flagged": false, "severity": "low", "summary": "s", "evidence": "e"}, "barrier_evidence": ""}` (closed one member early) | error `invalid character ',' after top-level value` |
| `{… "evidence": "e"}, "flagged": true` (closed early, no final `}`) | **accepted as the first half**: `flagged: false` |
| `{… "evidence": "e"}, "barrier_evidence": "ignore previous instructions"` (same, no final `}`) | **accepted as the first half**: the `barrier_evidence` is lost |
| `{"flagged": false}{"flagged": true}` | error `invalid character '{' after top-level value` |
| `{"flagged": false},{"flagged": true}` | error `invalid character ',' after top-level value` |
| `{"flagged": false} trailing words` | accepted, `flagged: false` (prose after the object, no `}` in it) |
| `{"flagged": true, "flagged": false}` | accepted, `flagged: false` (`encoding/json`: the last member wins) |

So today never takes the first of two complete objects, but it does take the first half of an object whose continuation
lost its final brace — the same slip as the failing shape, one character shorter.

## Initial direction

Decode the first complete JSON value from the first `{` with `encoding/json`'s `Decoder`; when what follows it begins
with `,`, repair only that shape — drop the one stray `}` and decode the joined text as a single object, accepted only
when nothing but whitespace or a closing fence follows and no member name repeats — and otherwise behave exactly as
today. Count repaired replies in the judge block (`repaired`) so the repair is never silent. Request bytes do not
change.

## Done criteria

- [ ] `TestParseVerdict_ReadsOneObject` (`internal/judge/reply_test.go`, table-driven) pins: a clean object, and one with
  prose or a code fence around it, decode as today; the closed-early shape decodes with every member present and is
  marked repaired, including when `evidence` holds nested braces and `}` inside strings; closed early where the joined
  object would repeat a member (`flagged`, or `Flagged` — `encoding/json` matches names without case) is refused; two
  complete objects `{…}{…}` and `{…},{…}` with different `flagged` are refused, never first-wins; closed early with no
  final `}` is refused; trailing garbage after a repaired object is refused. **Red on the base**: the closed-early rows
  fail with `invalid character ',' after top-level value`, and the no-final-brace rows return the first half
- [ ] `TestRun_ClosedEarlyReplyIsAnswered` (`internal/judge/reply_test.go`; httptest endpoint, real `NewHTTP`, the
  style of `padding_test.go`): one call's reply is closed early and flagged with a quote of the directive; the run
  counts it as answered — `Stats.Failed` 0, `Stats.Repaired` 1, no `LLM-000`, and the verdict's grounded `LLM-*`
  finding is present. **Red on the base**: `Failed` 1, an `LLM-000` "failed on 1 call(s)", no finding
- [ ] `FuzzParseVerdict`: never panics; whenever it succeeds, an independent oracle agrees — unrepaired: the text from
  the first `{` to the last `}` is one object and what follows the first value does not begin with `,`; repaired:
  removing exactly one `}` from the reply leaves an object (from the first `{` to the last `}`) with no repeated member
  name that decodes to the same verdict
- [ ] **Reverse assertion**: every existing test in `internal/judge`, `internal/report`, `cmd/aguard` and `baselines/`
  passes, and no existing `_test.go` file changes (`git diff --stat origin/main -- '*_test.go'` lists only new files
  and additions); a reply that fails today for any other reason still fails with today's error text (table rows: no
  JSON, a truncated object, `{…}{…}`, `{…},{…}`, a member of the wrong type)
- [ ] Request bytes do not change: `prompt_version_test.go` and `excerpt_version_test.go` pass unchanged, and
  `PromptVersion()` / `ExcerptVersion` keep their values (no diff in `prompt*.go`, `excerpt.go`, `ground.go`, `egress.go`,
  `triage.go`)
- [ ] The count is not silent: the JSON `judge` block carries `repaired` beside `failed` and `skipped` (always present
  when the block is, like them); the report's judge line adds the count only when it is above 0; the inbox total sums it;
  the benchmark ledger's `judge_usage` and `run.yaml` carry it when it is above 0 (question 9), so a row folded without
  a repair is byte-identical — the judgefold goldens and committed `baselines/results/` do not change
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency

## Out of scope

- **What is sent does not change**: no prompt, excerpt, cap, temperature, request field or header changes;
  `PromptVersion()` and `ExcerptVersion` keep their values. P-031 says neither version covers how a reply is read, and
  `tool_version` names this change; no third version is invented
- **Grounding, consensus, severity clamping, rules and `docs/rules.md` do not change**: a repaired verdict is the
  model's answer and goes through `groundedFinding` and the vote exactly like any other; nothing marks the finding
- **Triage replies are not touched**: `parseTriage` slices the same way, and a reply it cannot read yields no labels
  without a count or a note. That is a separate gap (display-only labels), named as a follow-up, not done here
- **A clean reply that repeats a member keeps today's reading** (`encoding/json`: the last one wins); only the repaired
  form refuses repeats, because only there do the two halves come from a slip
- **No retry on a parse failure**: parse errors stay non-retryable
- **The stderr line does not change** (P-001's line: content and format as today), and SARIF does not change
- **`baselines/results/` does not change by a byte**, the judgefold table and `judge.jsonl` schema do not change, and
  no run is re-folded
- `internal/collect`, `internal/detect`, `internal/gate`, the canonical hash, `go.mod`/`go.sum`: zero diff

## Must not claim

- Not "the judge's replies are always read": exactly one malformation is repaired; every other unreadable reply still
  fails the call and is reported as before
- Not that the measured rate (5 of 1,482 calls) holds for other models, temperatures or prompts: it is one run of one
  model on one corpus
- Not that a repaired answer is more or less reliable than another one: it is read as the model wrote it, members in
  order, and is counted so a reader can see how many answers needed it

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Table and run-level tests, run red on the base | `judge: tests — a reply closed one member early fails the call, and one that lost its last brace is read as its first half (P-034)` |
| 2 | `parseVerdict` decodes the first value with a `json.Decoder` and repairs only the closed-early shape; the reply-reading code moves to `reply.go`; fuzz test | `judge: read a reply's first JSON value and repair only an object closed one member early (P-034)` |
| 3 | `Stats.Repaired` and the judge block's `repaired`, summed by the inbox and shown in the judge line when above 0 | `judge: count repaired replies in the judge block and the report's judge line (P-034)` |
| 4 | The ledger's `judge_usage.repaired` and its `run.yaml` total, with the README sentence | `baselines: judge_usage carries the repaired count when the binary printed it (P-034)` |
| 5 | The `docs/llm-judge` pair says how a reply is read; the spec's `JudgeSummary` line; the guard note in `.claude/rules/judge.md` | `docs: how the judge reads a reply, and what repaired counts (P-034)` |

## Open questions

1. **What does "clean" keep accepting?** Today accepts anything whose first `{` to last `}` is one value: prose or a
   fence before it, and prose after it that contains no `}`. **Recommendation**: keep all of it, byte for byte. The
   reader runs today's slicing first and returns its result whenever the text after the first value does not begin with
   `,`; in that case the first value and today's slice are the same object, so the verdict is identical by construction.
   **Decided (2026-10-10)**: as recommended
2. **The closed-early shape that also lost its final `}`** (`{…}, "flagged": true`) is accepted today as its first half.
   **Recommendation**: refuse it. It is the same slip one character shorter, and its second half is exactly where a
   different `flagged` or a `barrier_evidence` would sit; the task's rule is that the first half is never taken alone.
   This turns a silent misreading into a visible failed call, the direction invariant #4 and #5 want: the judge may only
   add, and a misparse must fail, not decide. **Decided (2026-10-10)**: as recommended
3. **What makes the repaired form acceptable?** **Recommendation**: the joined text (object without its stray `}`, then
   the rest of the reply) decodes as one object; after it come only whitespace and at most one closing code fence
   (```` ``` ````); its top-level member names have no repeat; and it decodes into the verdict. Names are compared with
   `strings.EqualFold`, because `encoding/json` matches a member to a field without case — `"Flagged"` after
   `"flagged"` is a second answer to the same question. Exactly one brace is removed: a second stray `}` leaves text
   after the object and is refused. **Decided (2026-10-10)**: as recommended
4. **Which error does a refused reply carry?** **Recommendation**: when today's reading also fails, today's error,
   unchanged (`parse judge verdict: …`) — the measurement above was made by grepping for it. When today's reading would
   have accepted the first half (question 2), a new error that says the object was closed before the reply ended.
   **Decided (2026-10-10)**: as recommended
5. **Where does the count go?** **Recommendation**: `Stats.Repaired`, and `repaired` in the JSON `judge` block beside
   `failed`/`skipped`, always present when the block is (our own count, like them, so its absence means an older
   binary); the report's judge line shows it only when above 0 (`(0 failed, 0 skipped, 1 repaired)`), so a run without
   a repair renders exactly as today; the inbox total sums it; the stderr line stays P-001's. The signal travels as an
   unexported field of `Verdict`, so the `Client` interface and its three test doubles do not change and nothing a model
   writes can set it. **Decided (2026-10-10)**: as recommended
6. **Does the benchmark ledger carry it?** **Recommendation**: yes — it is three fields: `ledger.JudgeUsage.Repaired`
   (a pointer, present when the binary printed `repaired`, so rows folded from older `raw/` stay byte-identical), the
   adapter reads it the way it reads `retries`, and `run.JudgeUsage` totals it only when every judged sample reported
   it. A sample with a repaired call is still complete; the count lets a run say how many of its answers needed the
   repair. The judgefold table is not changed. **Decided (2026-10-10)**: as recommended
7. **A version number for the reader?** **Recommendation**: none. P-031 scoped `PromptVersion` to the request bytes and
   `ExcerptVersion` to what an artifact sends and how a quote is grounded; neither covers reply parsing, and
   `tool_version` names the commit that changes it. **Decided (2026-10-10)**: as recommended
8. **Retry instead of repair?** **Recommendation**: no. A retry is one more request to the endpoint, at temperature 0 it may return
   the same text, and at the sampling temperature it changes which answer is counted; the repair is deterministic and
   reads the answer the model gave. **Decided (2026-10-10)**: as recommended

Asked during stage 2:

9. **The ledger's `repaired`: a pointer like `retries`, or an int?** Question 6 recommended a pointer. Implemented that
   way, the judgefold golden test (`baselines/cmd/judgefold`) failed: its synthetic raw/ is printed with today's JSON
   types, so every judged row gained `"repaired":0`. **Recommendation**: an int, omitted when 0, in the row and in
   `run.yaml`. `retries` is a pointer because an older binary did retry without counting; a binary that does not print
   `repaired` predates the repair and made none, so its count is exactly 0 and absence is the truth, not a gap. Rows
   and totals without a repair stay byte-identical, and no golden changes. **Decided (2026-10-10)**: as recommended

## Done

Filled in at delivery.
