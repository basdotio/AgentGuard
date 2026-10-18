<!-- SPDX-License-Identifier: MIT -->
# 072 — A judge run's tokens are one total: which question spent them cannot be told, and a ledger row does not say which model answered

- **Source**: new finding (2026-10-18), while adding up the judge usage of the committed benchmark runs: 46 runs, about 162M prompt and 40M completion tokens, and no way to say how much of it the intent, injection, steering or triage questions took
- **Depends on**: P-071 (both change the judge's usage accounting in `internal/judge/openai.go`; this one starts from its three totals)
- **Branch**: `p/072-judge-usage-by-pass`

## Problem

`HTTPClient` keeps two running totals (`prompt`, `completion`; three after P-071) for the whole run. The JSON `judge`
block, the stderr line and the benchmark ledger therefore say how many tokens a run took, never which question took
them. The judge asks up to nine kinds of call — intent, injection, explain, capability, mcp_config, collusion,
server, steering (P-068), and triage — with very different prompt and reply sizes: on the 2026-10-10 pilot an MCP
config call returned about 994 completion tokens against 64–81 for connector and hook calls. Whoever tunes a prompt,
drops a pass or decides `samples` has to guess where the tokens go, and a change that doubles one pass's replies is
invisible in a total that moves by a few percent.

Separately, a ledger row's `judge_usage` does not name the model: the model is in the run's `run.yaml`, so a row
copied or folded out of its run cannot be attributed.

## Initial direction

Count usage per call and attribute it to the call's pass; report a `usage_by_pass` map in the judge block and the
ledger, and write the model into each ledger row's `judge_usage`.

## Done criteria

- [ ] `TestRun_UsageByPass` (httptest stub answering each pass with known token counts): the JSON judge block's
  `usage_by_pass` has one entry per pass that was called, with `calls`, `prompt_tokens`, `completion_tokens` (and
  `cached_prompt_tokens` when reported), and the entries sum exactly to the block's totals — reasked calls (P-069) and
  retries included in their pass
- [ ] Reverse assertion: the totals (`calls`, `prompt_tokens`, `completion_tokens`, `cached_prompt_tokens`) are
  byte-identical to `main` for the same stub run; a run with no call has no `usage_by_pass` key (omitempty)
- [ ] The ledger row's `judge_usage` carries `model` and `usage_by_pass`; `SumJudgeUsage` writes the per-pass sums into
  `run.yaml`, and `cmd/judgefold` keeps reading ledgers without them
- [ ] Request bytes unchanged: `aguard llm preview --json` output identical to `main` on the corpus fixtures used by
  P-027's tests
- [ ] `make verify` green, `go version` go1.23.5 with no toolchain switch

## Out of scope

- Money, or any per-model rate: the tool reports what the endpoint said.
- Per-artifact usage in the report (a later question; the per-sample ledger row already gives it for benchmarks).
- The stderr line keeps its form (totals only); the breakdown is in the JSON and the ledger.
- No change to which calls are made, their prompts, or `samples`.

## Must not claim

That a pass "costs" an amount, or that one pass is "too expensive": the numbers are token counts as reported by the
endpoint, and their price depends on the vendor. Do not compare passes across models.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Tests first: per-pass counts sum to the totals, reasks and retries land in their pass | `P-072: tests — per-pass usage sums to the totals` |
| 2 | The client returns usage per call; `Run` attributes it to the call's pass | `P-072: the judge counts usage per pass` |
| 3 | `usage_by_pass` in the JSON judge block, the Downloads block and the spec's data model | `P-072: the judge block reports usage by pass` |
| 4 | The ledger row names the model and carries the per-pass usage; run.yaml sums it | `P-072: a ledger row names its model and its usage by pass` |
| 5 | Docs pair (llm-judge.md / .zh-CN.md), baselines README | `P-072: docs — usage by pass` |

## Open questions

1. Key the map by the wire mode name (`intent`, `injection`, …, `triage`) or by rule ID? **Recommended**: by mode
   name: a pass is a question, and one question can raise several rules. **Decided (2026-10-19)**: as recommended.
2. Count a reasked call (P-069) under its own key or under its pass? **Recommended**: under its pass, with the pass's
   `reasked` count beside it, so the map still sums to the totals. **Decided (2026-10-19)**: as recommended.
3. Implement before P-071 merges? **Recommended**: no — the two change the same accounting; this branch starts from
   P-071's merge. **Decided (2026-10-19)**: as recommended.
