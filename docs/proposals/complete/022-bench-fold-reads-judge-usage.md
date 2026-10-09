<!-- SPDX-License-Identifier: MIT -->
# 022 — When the benchmark folds a judge run, the triage call count and the per-question cost are derived: aguard's JSON already reports the real numbers, and the rig reads none of them

- **Source**: a follow-up recorded by P-001 (`docs/proposals/complete/001-judge-usage-in-json.md`, third row of the table in
  "Problem", and "not in baselines" under "Out of scope"), 2026-10-09
- **Depends on**: P-001 (`JudgeSummary`'s `triage_calls` / `retries` / `prompt_tokens` / `completion_tokens`)
- **Branch**: `p/022-bench-fold-reads-judge-usage`

<!-- No "Status" line: the directory the file is in is its status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The benchmark rig reads `aguard … --json` once per sample (`Adapter.run` in `baselines/adapter/aguard/aguard.go`, decoded
into `model.ScanResult`), and then folds only one thing: `fill` folds the deterministic findings into one word by the gate
predicate. It reads not a single field of `res.Judge`. The `judge.jsonl` of a judge run is folded from raw/ **by hand** (the
first line of each judge run's `run.yaml` says "Written BY HAND"), and its usage fields are derived according to the `fold:`
in `2026-09-29-llm-gpt-4.1-mini-s3/run.yaml`:

- `triage_calls` = "one per artifact with static findings"
- `questions` = `(calls − triage) / 3`

Since P-001, the judge summary in `--json` carries the `triage_calls` and `retries` that aguard counts itself, and the token
counts the endpoint reports. The rig does not read them, so:

| Symptom | Consequence |
|---|---|
| **The derivation holds only when `skipped == 0`**. When a `max_calls` budget or `total_timeout` cuts off the tail of the plan, what gets cut is often exactly the triage (it comes last in each artifact's plan) | Temporary probe (real `judge.Run`, two skills each with one static finding, `samples: 3`, `max_calls: 13`): aguard reports `calls 13 · skipped 1 · triage_calls 1`; the derivation gives triage 2 and questions `(13−2)/3` = 3 remainder 2; the real number is 4 questions. **The per-question cost is wrong, and the rig cannot tell** |
| **Retries and tokens cannot be derived**, and the rig does not record them either | Nowhere in a judge run's `run.yaml` are the retry count and the token counts recorded, and that is exactly why P-001 added these fields |
| **raw/ is not the tool's raw output**. `keepRaw` re-`json.Marshal`s the decoded `model.ScanResult` and writes that to disk (the other two adapters write the tool's own bytes) | `triage_calls` / `retries` carry no `omitempty` in the current model: measuring a pre-P-001 binary with today's rig makes `"triage_calls":0,"retries":0` appear in raw/ out of nowhere. A fold that picks its basis by "is the field there" would read "not reported" as "reported 0", and questions would become `calls / 3` |

The four committed judge runs (`results/aguard/*-llm-*`) are not affected by the first row: measured by applying the
documented derivation to their raw/ (taken from the commit in the former repository agent-guard just before P-039 moved raw/
out of the tree), the `triage_calls` / `questions` of the two s3 runs match the committed `judge.jsonl` row for row, 224/224;
`skipped` is 0 in all four runs, and no sample carries the new fields. **What is wrong is not the numbers already published
but the next run**: the binary the next run uses already reports the real numbers, and the rig still derives them; if that
run sets `max_calls`, the derived numbers are wrong.

## Initial direction

Move the usage fold into the rig, and pick the basis by "is the field there": the aguard adapter reads the judge summary
from **the tool's own bytes**; when it carries `triage_calls`, the reported numbers are used (calls, triage_calls, retries,
the two token counts), and only when it does not is there a fallback to the documented derivation; it is recorded per sample
in `ledger.jsonl`, summed item by item into `judge_usage:` in `run.yaml`, with a statement of which basis this run used.
raw/ switches to writing the raw bytes of the tool's output (whitespace compacted, `<work>` replaced as before). The ledger
and run.yaml of a static run (no judge summary) do not change by a single byte. No rerun, no rewrite of any committed result.

## Done criteria

- [x] `TestJudgeUsage_ReportedCountsWinOverTheDerivation` (`baselines/adapter/aguard/usage_test.go`, new): sample JSON shaped
  like a budget cut (two skills each with one deterministic finding, `calls 13 · skipped 1 · triage_calls 1 · retries 2`, two
  token counts) → folds to `basis: reported`, `triage_calls 1`, `retries 2`, tokens as given; the test first asserts that the
  documented derivation gives 2 on the same fixture, which guarantees this is a fixture **on which the derivation is wrong**.
  Today this fold does not exist, so it is red at compile time
- [x] `TestScan_LedgerRowCarriesTheJudgeUsage` (same file, new): a stub binary answers with the JSON above → the ledger row
  `Scan` returns carries `judge_usage`, with the reported numbers; the stub answers with a static JSON (no `judge`) → the row
  is identical to today's, field for field
- [x] `TestRawKeepsTheToolsOwnBytes` (`baselines/adapter/aguard/raw_test.go`, new): the stub binary prints a judge summary
  **without** `triage_calls` / `retries` and with an unknown field → raw/ has neither key, and the unknown field is still
  there. Red when run today: raw/ is a re-encoding of `model.ScanResult`, `"triage_calls":0,"retries":0` appear out of
  nowhere, and the unknown field is lost
- [x] `TestSumJudgeUsage_*` (`baselines/run/judgeusage_test.go`, new): per-sample values are summed into `judge_usage:` in
  `run.yaml`, with the basis stated (`reported` / `derived` / `mixed`); a total that cannot be given for every sample
  (retries, tokens) is not written, and how many are missing is written instead; a sample with 0 calls does not count as "did
  not report tokens"
- [x] `TestWriteAllCarriesTheJudgeUsage` (`baselines/cmd/baseline/judgeusage_test.go`, new): the `ledger.jsonl` and
  `run.yaml` of a judge run carry `judge_usage`; the two files of a static run do not contain the word
- [x] Reverse assertion `TestJudgeUsage_WithoutTheFieldsFoldsAsBefore` (usage_test.go, new): a sample without the new fields
  (shaped after three rows of the committed s3 `judge.jsonl`) → `basis: derived`, `triage_calls` equals the documented
  derivation, retries and tokens are **absent keys** rather than 0
- [x] Reverse assertion (measured, not committed): the new fold's derivation fallback applied to the raw/ of the four
  committed judge runs: for the two s3 runs, `triage_calls` / `questions` match the committed `judge.jsonl` row for row,
  224/224; `judge_calls` match in all four runs, 819/819, 819/819, 224/224, 224/224
- [x] Reverse assertions `TestJudgeUsage_NoSummaryNoUsage`, `TestSumJudgeUsage_StaticRunHasNone` (new): no judge summary
  means no usage block, no `judge_usage` key in `run.yaml`, and the signature sentence unchanged; measured (not committed) by
  running the static driver on a small subset of the corpus: before and after the change, `ledger.jsonl`, `verdicts.jsonl`
  and `scorecard.txt` are byte-identical, and `run.yaml` differs only in `started_at`
- [x] Reverse assertion: `TestVerdictFollowsTheGatePredicate`, `TestJudgePassthroughIsExplicitAndScanOnly`,
  `TestAJudgeRunDisclosesTheUpload`, `TestSignatureNamesWhatIsOurs`, `TestYAMLRoundTripKeepsTheAttribution`,
  `TestEveryRowPassesTheLedgersOwnCheck` still green without a character changed; `TestRawOutputNamesNoWorkDirectory`
  changes only one line (`keepRaw` now takes bytes), the assertion untouched
- [x] `make verify` green

## Out of scope

- **No change to `cmd/`, `internal/`**: the binary already reports the numbers needed; the only thing missing is `samples`
  (see open question 3), and that one is not done
- **No rerun and no rewrite of any committed file under `baselines/results/`**. Under the new code, if the driver had
  written `run.yaml` for those four runs, there would be an extra `judge_usage:` block with basis `derived`, and the numbers
  would match the committed ones (two s3 runs: calls 1769 · skipped 0 · triage_calls 104; two samples:1 runs: calls 1923 ·
  triage_calls 241, counting only the 692 samples that carry a judge summary — the 127 that go through `check` have no
  summary, 55 of them have static findings, and applying "one per artifact with static findings" literally would count 55
  extra triage calls that never happened); their hand-written `run.yaml` has no such block, and its `fold:` section already
  says triage is derived. This is written down here, not changed there
- **No change to the verdict fold**: `fill`, `FlaggingRules`, `DimensionMap` are untouched; `verdicts.jsonl` and the
  scorecard stay unaffected by the judge, as before
- **No folding of the judge's findings in the rig**: `static` / `judge` / `judge_any` / `escalated_rules` / `votes` in
  `judge.jsonl` are still folded from raw/ by hand
- **No computation of `questions`, no estimate of the money spent**
- No change to the ccaudit / cisco adapters (their raw/ was always the tool's own bytes)

## Must not claim

- Do not say "the per-question cost is now measured": `questions = (calls − triage_calls) / samples` is still a formula,
  inexact when `skipped > 0`; the only change is that the `triage_calls` operand goes from derived to reported
- Do not say the four committed runs "use the reported basis": they use the derived basis, and on those four runs the
  derivation **happens to be** exact (`skipped` is 0 throughout, checked row by row)
- Do not say tokens are exact: they are self-reported by the endpoint, the same sentence as in P-001; when the endpoint does
  not report them, `judge_usage` has no token totals
- Do not say raw/ is byte-identical to before: it is now the tool's own output (whitespace compacted, `<work>` replaced as
  before), and its set of fields follows the binary version

## Work items

| W | In one sentence | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | Five new test files/cases + reverse assertions, run red | `baselines: tests — the judge fold ignores the counts aguard reports, and raw/ writes ones it never printed (P-022)` |
| 2 | raw/ writes the tool's own bytes | `baselines: raw/ keeps the bytes aguard printed instead of the rig's re-encoding of them (P-022)` |
| 3 | `ledger.JudgeUsage` + per-sample fold in the adapter: the reported numbers when reported, derived only when not | `baselines: each judged sample's ledger row carries what the judge cost, from the tool's own counts when it reports them (P-022)` |
| 4 | `run.JudgeUsage` + the driver writes the totals and the basis into `run.yaml`; the two sentences in the signature and the upload notice change so they no longer say "all in raw/" | `baselines: run.yaml sums the judge's cost and says whether it was reported or derived (P-022)` |
| 5 | "Measuring the judge" and the layout table in `baselines/README.md` | `docs: baselines README says where a judge run's cost comes from and when it is derived (P-022)` |
| 6 | This file, the index | `proposals: P-022 (P-022)` |

## Open questions

1. **Where does the per-sample usage go: on the `ledger.jsonl` row, in a separate file, or should the driver write
   `judge.jsonl`?**
   **Recommendation**: on the ledger row (`judge_usage`, `omitempty`). The ledger is already one row per sample, sorted by
   sample, and `Rules` / `Dimensions` are also diagnostic fields that `Check` does not look at; without a judge summary the
   whole block is absent, and a static run's ledger does not change by a byte. `judge.jsonl` is folded by hand; a file of the
   same name written by the driver with incomplete fields would collide with it.
   **Decided (2026-10-09)**: as recommended.
2. **What decides the basis?**
   **Recommendation**: per sample, whether the judge summary in **the tool's own bytes** has the key `triage_calls`; if it
   does, `reported`, if not, `derived`. Not by version number (the version string is for humans), and not by value (`0` is a
   legitimate reported number). Retries and the two token counts each go by whether their own key is there; a missing key
   stays missing and is not written as 0. At run level: all reported → `reported`, all derived → `derived`, otherwise
   `mixed`, with the number of samples of each written.
   **Decided (2026-10-09)**: as recommended.
3. **Should the rig compute `questions`?**
   **Recommendation**: no. The judge summary has no `samples`; the rig would either have to parse the operator config that
   `--config` points to (the rig does not read it now), or make the operator pass one more parameter that must stay
   consistent with the config — the latter is exactly the kind of "get one input wrong and every derived number is wrong"
   that this item removes. The `README` states the formula and that it is inexact when `skipped > 0`; adding `samples` to
   the summary means touching `internal/`, so it is recorded as a follow-up and not done in this item.
   **Decided (2026-10-09)**: as recommended.
4. **Token / retries totals: what to write when some samples did not report them?**
   **Recommendation**: write a total only when every sample that **sent calls** reported it; otherwise do not write it, and
   write `tokens_unreported_samples: N`. A sample with 0 calls does not count as unreported (the binary omits the keys for 0
   tokens anyway). Summing over half the samples and printing that as a "total" is exactly the misreading this guards
   against. Retries are written only when there is no `derived` sample.
   **Decided (2026-10-09)**: as recommended.
5. **Should raw/ hold the tool's raw bytes, or keep the re-encoding?**
   **Recommendation**: the raw bytes, with `json.Compact` removing the indentation (one line, like today's raw/), `<work>`
   replaced as before. Re-encoding adds keys to an old binary's output that it does not have and drops keys a new binary
   adds, so the "is the field there" basis criterion stops working on raw/. The other two adapters already write raw bytes.
   **Decided (2026-10-09)**: as recommended.
6. **Should the derivation fallback get two guards: triage recorded as 0 when there are 0 calls, and never greater than
   calls?**
   **Recommendation**: yes. A triage that was never sent is not a call; measured, these two guards change not a single row
   on the four committed runs.
   **Decided (2026-10-09)**: as recommended.
7. **The driver's two sentences — "anything these flags added lives in raw/ and is not committed" in the signature, and
   "the judge's output exists only in raw/" in the upload notice — should they change too?**
   **Recommendation**: yes. The signature gains the half sentence "usage is in judge_usage in run.yaml" only when there is a
   `judge_usage`; static runs and runs without usage do not change by a word. The upload notice only appears in `--llm` runs
   anyway; "the judge's output is only in raw/" becomes "the judge's findings are only in raw/, usage is in judge_usage".
   The signature is printed at the top of every scorecard; leaving it unchanged would make every judge scorecard state a
   sentence that is no longer entirely true.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #35 (2026-10-09; find the sha with git log --grep P-022)
Released: v0.19.0
Evidence: TestJudgeUsage_ReportedCountsWinOverTheDerivation, TestJudgeUsage_UnreportedTokensStayAbsent, TestScan_LedgerRowCarriesTheJudgeUsage (baselines/adapter/aguard/usage_test.go), TestSumJudgeUsage_* (baselines/run/judgeusage_test.go), TestWriteAllCarriesTheJudgeUsage (baselines/cmd/baseline/judgeusage_test.go); W1 red at compile time (usage_test.go:30:48: undefined: ledger.JudgeUsage; usage_test.go:36:9: undefined: judgeUsage; judgeusage_test.go:22:19: undefined: run.SumJudgeUsage; judgeusage_test.go:22:3: unknown field JudgeUsage in struct literal of type run.Run), green after W3 / W4
Evidence: TestRawKeepsTheToolsOwnBytes (baselines/adapter/aguard/raw_test.go); at W1, with usage_test.go temporarily moved aside and the test run alone, red at run time: raw/ carries "triage_calls", which the tool never printed — the raw file has "judge":{…,"triage_calls":0,"retries":0}, and also raw/ dropped a field the tool printed; green after W2
Evidence: temporary probe (real judge.Run, two skills each with one static finding, samples 3, not committed): no budget calls 14 · triage 2; max_calls 13 → calls 13 · skipped 1 · triage reported 1, derived 2, questions reported (13−1)/3 = 4, derived (13−2)/3 = 3 remainder 2; max_calls 12 → reported 1, derived 2
Evidence: end to end (real binary + this repository's driver + a fake endpoint on local loopback, no model connected, no network egress; 20 samples = the first 10 malicious + the first 10 benign in the s3 samples.jsonl, samples 3): no budget → run.yaml judge_usage basis reported · calls 108 · skipped 0 · triage_calls 9 (the derivation also gives 9) · retries 0 · tokens 10800 / 756 (= the 100 / 7 the endpoint reports per call × 108); max_calls 6 → calls 105 · skipped 3 · triage_calls 6, derivation 9; per sample, 3 (two hooks, one permission) reported 0 and derived 1, questions reported 2 and derived 1.67; each ledger row's triage_calls / retries equal the values the binary printed in that sample's raw
Evidence: reverse assertion TestJudgeUsage_WithoutTheFieldsFoldsAsBefore (usage_test.go, five cases, three shaped after committed s3 rows); measured (temporary test, not committed) by applying the new fold to the raw/ of the four committed judge runs (the raw/ in the former repository agent-guard's commit just before P-039): two s3 runs, judge_calls / triage_calls / questions each match 224/224, basis derived throughout, totals calls 1769 · triage_calls 104; two samples:1 runs, judge_calls match 819/819, 692 carry a summary, the 127 that go through check have no summary and produce no usage, totals calls 1923 · triage_calls 241; the two guards (0 calls records 0, never greater than calls) change 0 samples across the four runs
Evidence: reverse assertions (static) TestJudgeUsage_NoSummaryNoUsage, TestSumJudgeUsage_StaticRunHasNone; measured with the same binary and the same 20 samples running the static driver, origin/main source against this branch: ledger.jsonl, verdicts.jsonl byte-identical, scorecard.txt and run.yaml differ only in the start-time line, neither side has judge_usage; raw/ after decoding differs only in scanned_at (two scans)
Evidence: still green without a character changed — TestVerdictFollowsTheGatePredicate, TestJudgePassthroughIsExplicitAndScanOnly, TestAJudgeRunDisclosesTheUpload, TestSignatureNamesWhatIsOurs, TestOurOwnRunIsNotProvisional, TestYAMLRoundTripKeepsTheAttribution, TestEveryRowPassesTheLedgersOwnCheck, TestPlaceableArtifactIsScored; TestRawOutputNamesNoWorkDirectory only changes keepRaw's argument to the bytes of json.Marshal(res), the assertion untouched
Evidence: Out of scope — git diff --stat origin/main -- cmd internal baselines/results baselines/adapter/ccaudit baselines/adapter/cisco baselines/adapter/sarif go.mod go.sum plugin is empty; fill / FlaggingRules / DimensionMap are not in the diff
Evidence: make verify: all gates passed (golangci-lint 0 issues); go version go1.23.5 (no toolchain switch), the go directive in go.mod is still go 1.23.5, no new dependencies
```
