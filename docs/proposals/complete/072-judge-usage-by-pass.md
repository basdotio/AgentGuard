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

- [x] `TestRun_UsageByPass` (httptest stub answering each pass with known token counts): the JSON judge block's
  `usage_by_pass` has one entry per pass that was called, with `calls`, `prompt_tokens`, `completion_tokens` (and
  `cached_prompt_tokens` when reported), and the entries sum exactly to the block's totals — reasked calls (P-069) and
  retries included in their pass
- [x] Reverse assertion: the totals (`calls`, `prompt_tokens`, `completion_tokens`, `cached_prompt_tokens`) are
  byte-identical to `main` for the same stub run; a run with no call has no `usage_by_pass` key (omitempty)
- [x] The ledger row's `judge_usage` carries `model` and `usage_by_pass`; `SumJudgeUsage` writes the per-pass sums into
  `run.yaml`, and `cmd/judgefold` keeps reading ledgers without them
- [x] Request bytes unchanged: `aguard llm preview --json` output identical to `main` on the corpus fixtures used by
  P-027's tests
- [x] The usage log line (P-073) carries `usage_by_pass` as the block does, and its equality test holds it to the block
- [x] Docs: `docs/llm-judge.md` / `.zh-CN.md` (the judge block, the usage log's field list and line size), spec §3 and
  §8, `baselines/README.md`
- [x] `make verify` green, `go version` go1.23.5 with no toolchain switch

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
4. Which spelling of the pass names? The list above (`explain`, `mcp_config`, `steering`) follows the Go constants; no
   output uses them. `aguard llm preview` already names each call's pass (`pass` in its JSON) as `deobfuscation`,
   `mcp-config` and `result`, and the docs' pass table and every proposal since P-068 say the same. **Recommended**: the
   preview's names (`modeInfo(…).pass`, and `triage`), through one function both use (`task.pass`), so a planned call
   and the tokens it spent are read under one name and a second vocabulary for the same nine questions never exists.
   **Decided (2026-10-19)**: as recommended.
5. How are per-call tokens carried from the client to `Run` without changing the `Client` interface (every test double
   implements it)? **Recommended**: `execute` puts one counter per task in the call's context (`withCallUsage`), and the
   HTTP client adds each decoded reply's usage to it in the same place it adds to the run totals (`HTTPClient.count`):
   the per-pass figures are a second copy of the numbers the totals are made of, never a recount, so they cannot drift
   apart; a client that keeps no counter (a test double) leaves the tokens 0 and the calls counted.
   **Decided (2026-10-19)**: as recommended.
6. The per-pass `reasked` in the JSON block: always present or omitted when 0? **Recommended**: always present in the
   block and the usage log, as the block's own `reasked` is (the judge's own counts are always present, the endpoint's
   tokens are omitted when not reported); omitted when 0 in the ledger row and `run.yaml`, as the ledger's own
   `reasked` is. **Decided (2026-10-19)**: as recommended.
7. `run.yaml` when some sample made calls without a breakdown (a binary older than it)? **Recommended**: no
   `usage_by_pass` at all, as the token totals refuse a sum over part of the samples; per-pass token counts only beside
   the run's token totals. **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR <number> (find the sha with git log --grep P-072 after the merge)
Evidence: TestRun_UsageByPass (internal/judge/usage_by_pass_test.go): every pass of the preview's fixture plus a server, samples 3, 41 calls; one entry per pass (9), each equal to what the stub reported for that pass; the first capability call answered 503 and retried (counted once, the 503 adds nothing), the first injection call unreadable and re-asked (its two replies counted, reasked 1 under injection); the entries sum to calls, reasked and the client's three token totals
Evidence: TestRun_UsageByPassNamesThePreviewsPasses: the keys are the preview's pass names; TestRun_UsageByPassReverse: no call → no map, a client reporting no tokens → calls only
Evidence: TestE2E_JudgeBlockUsageByPass, TestScanInbox_JudgeCostAddsUp (cmd/aguard): the JSON block and the Downloads block sum to their totals, 100 / 7 / 64 per call, the retried call once; TestUsageLog_* hold the line's usage_by_pass equal to the block's, none on llm test
Evidence: reverse — TestE2E_UncachedEndpointBlockUnchanged: the block without usage_by_pass is byte-equal to the pre-P-071 key set; TestE2E_NoCallNoUsageByPass: a block with no call has no key
Evidence: baselines — TestJudgeUsage_ModelAndUsageByPassReachTheRow, TestSumJudgeUsage_UsageByPassIsSummed, TestJudgefold_ReadsLedgersBeforeAndAfterUsageByPass (all 75 committed aguard ledgers read); the judgefold golden ledger gains model and usage_by_pass on its 7 judged rows and no other byte (checked by stripping the two keys)
Evidence: scan --llm (samples 3) over a fixture with a skill, a chained skill, CLAUDE.md, a hook and an MCP server against a local stub, origin/main binary vs this branch: the judge block minus usage_by_pass is equal (38 calls, 37457 / 266 tokens, 2432 cached), the stderr line reads "37457 tokens in (2432 cached) / 266 out" on both; usage_by_pass: injection 12 calls / 13011, intent 6 / 6906, result 6 / 7047, capability 3 / 2811, collusion 3 / 2388, deobfuscation 3 / 2316, mcp-config 3 / 1746, triage 2 / 1232 — sum 38 / 37457 / 266 / 2432; check --llm on a server: server 3 / 3708, triage 1 / 574, sum 4 / 4282
Evidence: request bytes — aguard llm preview --json, origin/main vs this branch, cmp identical on three fixtures: the environment (21,810 bytes; intent, injection, result, deobfuscation, collusion, capability, mcp-config, triage), one skill (12,955), a server source (4,948; server, triage); judge-prompt=b851dfd5412b, judge-excerpt=9 unchanged
Evidence: a usage log line for one skill checked by five passes measures 929 bytes (was about 400 without the breakdown); the docs' line-size sentence says so
Evidence: make verify: all gates passed; go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 3 "go 1.23.5"
```
