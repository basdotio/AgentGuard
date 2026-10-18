<!-- SPDX-License-Identifier: MIT -->
# 071 — The judge's token count prices every prompt token at the full rate: the vendor's prompt-cache discount is never read

- **Source**: new finding (2026-10-19), reading what the judge records about a run against what an OpenAI-compatible
  endpoint returns
- **Depends on**: P-001 (the judge's usage in the JSON report), P-022 (the benchmark ledger reads it)
- **Branch**: `p/071-cached-prompt-tokens`

## Problem

Anyone who brings their own key and reads what a `--llm` run used gets two numbers: `prompt_tokens` and
`completion_tokens`. OpenAI-compatible endpoints report a third, `usage.prompt_tokens_details.cached_tokens`: the part
of the prompt tokens the vendor served from its prompt cache, which it bills at its own, lower rate.
`internal/judge/openai.go` decodes only the first two, so the JSON `judge` block, the stderr usage line and the
benchmark ledgers (`baselines/adapter/aguard/usage.go` → `ledger.jsonl` `judge_usage`, and `run.yaml` `judge_usage:`
through `baselines/run/judgeusage.go` `SumJudgeUsage`) carry a number from which the real cost of a run cannot be
worked out: every prompt token looks like a full-rate one.

Every judge call repeats the pass's system prompt (about 2 KB) ahead of the excerpt, and a question is asked `samples`
times, so the cached share of a run can be large. Anyone estimating what a run used from the report, and the benchmark
runs that record usage (P-001, P-022), overstate it by exactly the part the endpoint already told us about.

## Initial direction

Read the field where the other two are read, keep a third running total next to them under the same conditions, and
carry it everywhere the two go: the JSON block, the stderr line (only when there is something to say), the ledger row
and the run sum. It is a subset of `prompt_tokens`, never added to it. Response parsing only: no request byte moves.

## Done criteria

- [ ] `TestHTTPClient_CountsCachedPromptTokens` (`internal/judge`): a stub whose replies carry
  `prompt_tokens_details.cached_tokens` sums it next to the other two; absent, `null` or a `null` details object count 0;
  a non-200 reply or an undecodable envelope adds nothing, as for the other two
- [ ] `TestE2E_JudgeSummaryCarriesCachedPromptTokens` (`cmd/aguard`): at `samples: 3`, with the first request answered
  503 and retried, the JSON judge block's `cached_prompt_tokens` is the per-call figure times `calls` (the 503 adds
  nothing), and `prompt_tokens` / `completion_tokens` are what they were
- [ ] Reverse assertion, `TestE2E_UncachedEndpointBlockUnchanged`: a stub without the field leaves the key out
  (omitempty), and the marshalled block equals the same run's block with the new key deleted — every other byte as before
- [ ] `TestJudgeUsageLine` (`cmd/aguard`): the stderr line for an endpoint without the field is byte-identical to the old
  format string; with it, `(N cached)` follows the input count; `TestScanInbox_JudgeCostAddsUp` sums the Downloads items'
  cached tokens
- [ ] Baselines: the ledger row carries `judge_usage.cached_prompt_tokens` (omitempty), `run.yaml`'s sum carries it;
  a sample without it is not "tokens unreported" (`TestSumJudgeUsage_*`, `TestJudgeUsage_*`)
- [ ] Request bytes unchanged: `aguard llm preview --json` on a fixed fixture is byte-identical before and after, and
  `judge-prompt=` in `aguard version` does not move
- [ ] `docs/llm-judge.md` / `.zh-CN.md`, spec §8, `baselines/README.md`
- [ ] `make verify` green with `go version` go1.23.5 and no toolchain switch; `head -3 go.mod` second line `go 1.23.5`

## Out of scope

- Any money: the tool prices nothing and computes no cost. It records what the endpoint reported, under the endpoint's
  own field name.
- Other usage fields: reasoning tokens (`completion_tokens_details`), audio tokens, and cache fields some endpoints
  report under other names. Only the OpenAI-compatible `prompt_tokens_details.cached_tokens` is read.
- Making the cache hit more often (reordering the request so the shared prefix is longer, or a cache key): that changes
  request bytes and `PromptVersion`, and is its own proposal if it is worth one.
- The committed `baselines/results/` runs: they were made before the field was read and stay as they are.

## Must not claim

- Never that cached tokens "cost X", "are free", or are billed at any named fraction; never a vendor's prices. Only: the
  vendor reports them separately and bills them at its own (lower) rate.
- Never that `cached_prompt_tokens` absent means "nothing was cached": an endpoint that does not report the field and one
  that cached nothing look the same, as for the other two counts.
- Never that the tool's numbers are the vendor's own account of a run; they remain "what the endpoint reported".

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The proposal | `P-071: the proposal — the judge's token count never reads the vendor's cached prompt tokens` |
| 2 | The client decodes the field and keeps a third total; `Usage` returns a small struct | `P-071: the judge client reads prompt_tokens_details.cached_tokens and keeps it next to the other two` |
| 3 | The JSON block (environment, check, Downloads), the stderr line and `llm test` carry it | `P-071: the judge block carries cached_prompt_tokens; the usage line names it only when the endpoint reported some` |
| 4 | The ledger row and the run sum carry it | `P-071: the baseline ledger and usage sums carry cached_prompt_tokens` |
| 5 | Documentation | `P-071: docs — cached_prompt_tokens in the judge block and the ledger` |
| 6 | Done | `P-071: done — evidence, and the proposal moves to complete` |

## Open questions

1. A struct or a third positional return from `HTTPClient.Usage`? Recommended: a small struct (`judge.TokenUsage`):
   three ints of the same type in a row is how two get swapped, and there are two callers to update.
   **Decided (2026-10-19)**: as recommended.
2. A pointer in the ledger row (like `prompt_tokens`) or a plain `int` with `omitempty` (like `repaired`)? Recommended:
   a plain int. The binary leaves a zero out, and an endpoint that does not report the field means the same thing as one
   that cached nothing as far as any reader can tell, so "absent" and "0" carry no distinction a pointer could keep; and
   `tokens_unreported_samples` stays about prompt/completion only.
   **Decided (2026-10-19)**: as recommended.
3. The `run.yaml` sum when some sample did not report prompt/completion tokens? Recommended: no cached total either,
   as for the other two — a cached figure next to an absent prompt total is a subset of nothing.
   **Decided (2026-10-19)**: as recommended.
4. Does the stderr line show the cached share when the endpoint reported none? Recommended: no — the line stays
   byte-identical for every endpoint that does not report it, which is every local one.
   **Decided (2026-10-19)**: as recommended.
