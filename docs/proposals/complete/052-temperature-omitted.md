<!-- SPDX-License-Identifier: MIT -->
# 052 — A model that allows only its default temperature cannot be the judge: every call is a 400, including `llm test`

- **Source**: new finding (2026-10-13), trying `gpt-6.1-sol` as the judge for a model comparison: `aguard llm test`
  answered `judge endpoint returned 400: … 'temperature' does not support 0 with this model. Only the default (1)
  value is supported`
- **Depends on**: none
- **Branch**: `p/052-temperature-omitted`

## Problem

The judge client sends `temperature` on every call — 0 for a single call, 0.8 under `samples > 1` — and some models
(reasoning-class ones among them) accept only their default and answer the member with a 400 that names it. The 4xx
path is right not to retry a bad request, so the model cannot be used at all: `llm test` fails, `scan --llm` reports
every call failed, and a comparison of judges cannot include it. The error says exactly what is wrong, which is what
makes it answerable by the client rather than by a knob.

## Initial direction

On a 400 whose body names `param: temperature` with `code: unsupported_value`, the client sends the same call once more
without the member and remembers to omit it for the rest of its life; a consensus run then samples at the model's
default, which is what such a model offers. The run's summary says `temperature_omitted: true` so a report never
hides it. Nothing else changes: the request body of a model that takes the member is byte-identical (`PromptVersion`
does not move), and any other 400 is the answer it always was.

## Done criteria

- [x] `TestHTTP_TemperatureRefusedIsOmittedThenRemembered` (`internal/judge/temperature_test.go`, new): an endpoint
  that refuses the member sees three requests for two calls — refused, re-sent without it, the next clean — and the
  client reports the omission. **Red on the base**: the first call returns the 400
- [x] `TestHTTP_OtherBadRequestsAreNotRetried` (same file): a `model_not_found` 400 is one POST, returned, nothing
  remembered
- [x] `TestRun_TemperatureOmittedReachesTheStats` (same file): `Run` carries the client's report into `Stats`, no call
  counted as failed; a scripted client reports nothing
- [x] `TestPromptVersion_HashesWhatTheClientSends` and the prompt-version goldens unchanged: the body of a model that
  takes the member is byte-identical (`temperature` became a pointer with `omitempty`)
- [x] `aguard llm test` against the real endpoint with `model: gpt-6.1-sol` answers OK; with `gpt-4.1-mini` as before
- [x] Docs: `docs/llm-judge.md` + zh (sampling bullet, the judge block's fields), spec §16 `JudgeSummary`
- [x] `make verify` green and `go test -count=1` green; `internal/score`, `internal/gate`, `internal/detect`,
  `internal/collect` untouched

## Out of scope

- A config knob for the temperature: the endpoint says what it takes; a knob would be a second place to be wrong
- Counting the re-sent call as a retry: `retries` counts "not now" answers (429, 5xx, transport); this is "not that
  shape", answered once and remembered
- Whether sampling at a model's default temperature gives a consensus worth the same as at 0.8: the summary records
  that it happened; a comparison that includes such a model reads its vote agreement with that in mind

## Must not claim

- Do not say such models are "supported" beyond this: their replies have not been measured; this proposal only lets
  the measurement start
- Do not say consensus at the default temperature equals consensus at 0.8

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The fallback in the client, the flag in `Stats` and `JudgeSummary`, the tests | `judge: a model that refuses the temperature member gets the same call without it, and the summary says so (P-052)` |
| 2 | Docs pair, spec | `docs: temperature_omitted in the judge block (P-052)` |
| 3 | This file, the index | `proposals: P-052 (P-052)` |

## Open questions

1. **Retry once in the client, or surface the refusal to the run loop?** **Recommendation**: in the client — the
   refusal is about the request's shape, the client owns the shape, and `Ping` (`llm test`) has no run loop.
   **Proceeded under the recommendation (2026-10-13)** on the maintainer's "用 GPT-6.1 Sol 试试".

## Done

```
Merged: PR #72 (2026-10-13; find the sha with git log --grep P-052)
Released: v0.24.0
Evidence: TestHTTP_TemperatureRefusedIsOmittedThenRemembered, TestHTTP_OtherBadRequestsAreNotRetried, TestRun_TemperatureOmittedReachesTheStats (internal/judge/temperature_test.go); the first red on the base
Evidence: prompt-version tests green and judge-prompt unchanged (6572379d32b1 before and after): the request body of a model that takes the member did not change
Evidence: aguard llm test, real endpoint: "OK · gpt-6.1-sol answered in 1.85s via https://api.openai.com/v1 (23 tokens in / 4 out)" where the base answered "judge endpoint returned 400 … 'temperature' does not support 0 with this model"; gpt-4.1-mini still OK
Evidence: TestHTTP_ConcurrentFirstCallsAreAllResent (same file), added after the first real run: with concurrency 4 the first calls are refused together, the one that set the flag was the only one re-sent and the others failed — 573 of 1,769 calls on the 224 subset, about three per sample; the re-send now rests on whether THIS call carried the member, not on the flag
Evidence: make verify: all gates passed on the branch; go test -count=1 -race ./internal/judge/ ./cmd/aguard/ green; go.mod line 2 unchanged
```
