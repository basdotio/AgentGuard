<!-- SPDX-License-Identifier: MIT -->
# 001 — The judge's tokens, triage calls and retries never reach the report; the cost can only be copied from stderr

- **Source**: new finding (2026-10-09) — the token usage, triage call count and retry count of each judge run are printed
  only to stderr, and with `--quiet` (which every Downloads item runs with) they are nowhere; without them in the JSON
  report there is no way to check afterwards how much a run used. Ported from P-042 in the former private repository
  agent-guard
- **Depends on**: none
- **Branch**: `p/001-judge-usage-in-json`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The usage of `--llm` appears in one place only: the line `runJudge` prints to stderr when it finishes
(`cmd/aguard/main.go:293-300`, "LLM judge: N call(s) … · R retry · F failed · P tokens in / C out").
It has three gaps:

| Gap | Consequence |
|---|---|
| **Token counts do not reach `--json`**. `model.JudgeSummary` (`internal/model/model.go:547-556`) has only `ran/reason/artifacts/calls/failed/skipped/findings/endpoint`, no token field | For the four judge runs committed under `baselines/results/aguard/` in this repository (`*-llm-*`), nowhere in `run.yaml` or `judge.jsonl` is a token count recorded (`grep -i token` matches only one model description and a few sample names). The baseline driver (`baselines/adapter/aguard`) reads `--json`; stderr is spliced into the error message only on failure, so the line from a successful run never reaches raw/ |
| **With `--quiet` even the stderr line is missing** (`if !quiet && stats.Calls > 0`). On the Downloads path every downloaded item runs with `quiet: true` (`main.go:581`) | The judge usage of downloaded items never appears anywhere, not even in the terminal |
| **Triage calls and judge calls are lumped together**. `Stats.Calls` is one number, `calls` is one number; with `samples: 3` the judge questions are multiplied by 3 and triage is not | When judge.jsonl is folded, `triage_calls` is **inferred** as "one per artifact with static findings", and `questions = (calls − triage) / 3` is inferred too (see `fold:` in `baselines/results/aguard/2026-09-29-llm-gpt-4.1-mini-s3/run.yaml`). If the inference is wrong, the per-question usage is wrong |

The retry count, too, is only on stderr.

In one sentence: **for a feature that is off by default and spends the user's own API quota, the report records whether
it ran and how many times, but not how much it used.** `docs/llm-judge.md` itself says "cost is the one thing about
`--llm` the report itself can't show you" — that describes the current state; it is not a design.

## Initial direction

`JudgeSummary` gains `prompt_tokens`, `completion_tokens`, `triage_calls` and `retries` (without `--llm` the whole block
still does not exist). `judge.Stats` counts triage separately by task type in the merge loop; `runJudge` fills the fields
from `*HTTPClient.Usage()` regardless of `quiet`, and the stderr line stays as it is; the per-item Downloads totals
follow. The human-readable reports gain not a single line. The `judge.Client` interface does not change (three test
doubles implement it).

## Done criteria

- [x] `TestRun_StatsCountTriageApart` (`internal/judge/consensus_test.go`, new): `samples: 3`, one skill with static
  findings → `Stats.TriageCalls == 1`, `Stats.Calls == 3 × questions + 1`. Today the field does not exist, so it is red at
  compile time
- [x] `TestE2E_JudgeSummaryCarriesCost` (`cmd/aguard/e2e_test.go`, new): a fake endpoint carries `usage` in every response,
  `scanEnv(…, llm: true, quiet: true)` → `Judge.PromptTokens` and `CompletionTokens` equal the totals the endpoint
  reported, `TriageCalls` equals the number of artifacts with static findings, `Retries == 0`.
  **`quiet: true` is deliberate**: today a quiet run does not even have the stderr line, and this criterion pins "quiet
  does not affect the accounting"
- [x] `TestScanInbox_JudgeCostAddsUp` (`cmd/aguard/main_test.go`, new): two Downloads candidates, `--llm` → the four new
  fields of `InboxReport.Judge` equal the sum over the items
- [x] Reverse assertion `TestE2E_UnreportedUsageIsNotAZero` (`cmd/aguard/e2e_test.go`, new): when the endpoint does **not**
  report `usage` (the existing `judgeServer`), the JSON has no `prompt_tokens` / `completion_tokens` keys — "not reported"
  is not written as 0 (see open question 1); the `retries` / `triage_calls` keys are present
- [x] Reverse assertion: `Judge == nil` without `--llm` (`TestScanEnv_JudgeRequestedButNotEnabled`,
  `cmd/aguard/main_test.go:925`) stays green unchanged; `TestText_JudgeLineStates` (`internal/report/text_test.go:515`)
  and `TestHTML_JudgeSectionPlaceholder` (`internal/report/html_test.go:124`) stay green unchanged — the human-readable
  reports do not change by a single byte
- [x] Reverse assertion: `TestConsensus_TriageIsNotSampled`, `TestConsensus_CostsWhatItSays`,
  `TestRun_RetriesOnlyRetryableErrors` and `TestHTTPClient_CountsTokens` stay green unchanged
- [x] `make verify` green

## Out of scope

- **The human-readable reports gain not a single line**: the judge sentence in text / markdown / html / sarif
  (`report.judgeLine`) does not change. Usage is for machines to read; the report is for people
- **The stderr line does not change**: its content, its format, and not printing it under `--quiet` all stay as today
- **The `judge.Client` interface does not change**: `Usage()` is only on `*HTTPClient`, and `runJudge` already holds the
  concrete type; adding a method to the interface would break three test doubles
  (`scriptedClient`, `fakeClient`, `countingTriageClient`)
- **Nothing goes into baselines**: the adapter writes `model.ScanResult` into raw/ as is, so the new fields come along
  automatically; folding them into judge.jsonl is a separate piece of work that this proposal does not do
- **No estimate of the money spent**: the unit price varies by vendor, tier and time; the report has only tokens
- The semantics of `max_calls` do not change, and no new network call is added

## Must not claim

- Do not say "the cost can now be calculated exactly": tokens are **self-reported by the endpoint**, and the comment at
  `internal/judge/openai.go:68` says, verbatim, "a cost baseline, not an accounting guarantee".
  For an endpoint that does not report usage, the JSON simply lacks these two keys
- Do not say "the question count is now recorded directly": `(calls − triage_calls) / samples` is exact in a run with
  nothing skipped and only an upper bound when something was skipped; this proposal records calls, not questions
- Do not say the Downloads usage "was already there": before, the quiet path had it nowhere

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Three new tests + one reverse assertion, run red | `judge, cmd: tests — the JSON judge summary has no tokens, triage calls or retries, and quiet runs record nothing (P-001)` |
| 2 | `judge.Stats` counts `TriageCalls` separately by task type in the merge loop | `judge: Stats counts triage calls apart from judge calls (P-001)` |
| 3 | `JudgeSummary` gains `prompt_tokens` / `completion_tokens` (absent = the endpoint did not report), `triage_calls` and `retries`; `runJudge` fills them whether quiet or not | `model, cmd: the JSON judge summary carries tokens, triage calls and retries, quiet or not (P-001)` |
| 4 | The Downloads section sums the four new fields over the items | `cmd: the Downloads judge summary adds up every item's cost (P-001)` |
| 5 | The spec §8 `Judge` comment; "cost is the one thing the report cannot show" and the JSON field table in `docs/llm-judge.md` and its zh pair; the comment on that line in `main.go` | `docs: spec §8 and the llm-judge pair say the judge's cost is in the JSON summary (P-001)` |
| 6 | This file, the index | `proposals: P-001 (P-001)` |

## Open questions

1. **When the endpoint does not report `usage`, are the two token fields written as 0 or left out?**
   **Recommendation**: left out (`omitempty`). A real call cannot have 0 prompt tokens, so "the key is missing" equals "the
   endpoint did not report", which is more honest than a 0 that looks like a measurement.
   `retries` and `triage_calls` **always appear**, like `calls` / `failed` / `skipped`: we count them ourselves, so 0
   means 0.
   **Decided (2026-10-08)**: as recommended.
2. **Should the p50 / p95 latency go into the JSON while we are at it?**
   **Recommendation**: no. Latency depends on the network and the machine; putting it into the JSON makes the reports of
   two runs on the same input differ even more; the stderr line keeps it.
   **Decided (2026-10-08)**: as recommended.
3. **Field names `prompt_tokens` / `completion_tokens` (OpenAI's naming), or `tokens_in` / `tokens_out` (the stderr line's
   naming)?**
   **Recommendation**: `prompt_tokens` / `completion_tokens`. They have the same names as the keys the endpoint returns,
   so whoever reads raw/ does not have to translate; stderr is for people, and each side keeps its own convention.
   **Decided (2026-10-08)**: as recommended.

## Done

```
Merged: PR #19 (2026-10-09; find the sha with git log --grep P-001)
Released: v0.19.0
Evidence: TestRun_StatsCountTriageApart (internal/judge/consensus_test.go); W1 red at compile time (consensus_test.go:290: s1.TriageCalls undefined (type Stats has no field or method TriageCalls)), green after W2; measured: with samples=1, 3 calls = 2 questions + 1 triage; with samples=3, 7 = 3 × 2 + 1; triage_calls is 1 both times
Evidence: TestE2E_JudgeSummaryCarriesCost (cmd/aguard/e2e_test.go); W1 red at compile time (e2e_test.go:315: j.PromptTokens undefined); with the fields added but runJudge not filling them (temporary probe, reverted) red at run time: tokens = 0 in / 0 out over 4 call(s); want 400 / 28, triage_calls = 0, want 1; green after W3: under quiet: true, tokens = 100 × calls / 7 × calls, triage_calls = the number of artifacts with static findings, retries = 0
Evidence: TestScanInbox_JudgeCostAddsUp (cmd/aguard/main_test.go); still red after W3 (tokens = 0 in / 0 out over 6 call(s); want 600 / 42, triage_calls = 0, want 2) → green after W4
Evidence: reverse assertion TestE2E_UnreportedUsageIsNotAZero (cmd/aguard/e2e_test.go): endpoint does not report usage → the JSON has no prompt_tokens / completion_tokens, and has retries / triage_calls
Evidence: reverse assertions green unchanged — TestScanEnv_JudgeRequestedButNotEnabled (main_test.go, Judge == nil without --llm), TestText_JudgeLineStates, TestHTML_JudgeSectionPlaceholder, TestConsensus_TriageIsNotSampled, TestConsensus_CostsWhatItSays, TestRun_RetriesOnlyRetryableErrors, TestHTTPClient_CountsTokens
Evidence: Out of scope — git diff --stat origin/main -- internal/report baselines internal/judge/judge.go internal/judge/openai.go internal/judge/run_test.go go.mod go.sum plugin is empty; the stderr line's format string is not in the diff
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 is go 1.23.5, no new dependencies
```
