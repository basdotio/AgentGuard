<!-- SPDX-License-Identifier: MIT -->
# 080 — Every sample of a judge question goes out under its own nonce: the k requests of one question never share a prefix, and an endpoint's prompt cache never serves one of them

- **Source**: new finding (2026-10-19), reading `cached_prompt_tokens` (P-071) on real `--llm` runs: it is 0 on every run
- **Depends on**: P-071 (the judge block reports the cached prompt tokens the endpoint returns)
- **Branch**: `p/080-shared-nonce-per-question`

## Problem

With `samples: 3` (the setting a gate on the judge requires) each question is sent three times. The three requests
carry the same pass, the same excerpt and the same temperature; the only bytes that differ are the nonce. `HTTPClient.Judge`
draws a fresh one for every call, and the nonce sits in the first sentence of the system message and on the first line of
the user message. So the three requests share no prefix longer than about a hundred tokens.

OpenAI-compatible endpoints that cache prompts (OpenAI's documented minimum is a 1,024-token identical prefix) can
therefore never serve samples 2 and 3 from the prefix sample 1 just paid for. Since P-071 the judge block shows this
directly: `cached_prompt_tokens` is absent on every `--llm` run we made against an endpoint that reports the field.

Measured with `aguard llm preview --json` over 150 benign and 150 malicious corpus skills, a skill question is a pass
system prompt of about 600–700 tokens plus the excerpt (injection ≈1,090, result ≈1,035, intent ≈70 tokens on average).
Two consequences:

- Moving the nonce later inside the system message gains nothing: the static part is under the 1,024-token minimum, and the
  user message still opens with the nonce fence.
- What could be cached is the whole request of samples 2..k, which is identical except for the nonce. On the benign set 61%
  of questions reach the minimum; 51% of all input tokens are in a repeat sample of one of those.

A pilot on 20 randomly chosen benign skills (gpt-4.1-mini-2025-04-14, `samples: 3`, 197 calls each way) with an
experimental build that shares the nonce within a question and sends the first sample before the others:
`cached_prompt_tokens` 0 → 153,088 of 334,350 prompt tokens (45.8%), 18 of 20 skills with a hit, the same findings. The cost of
that build is time. A single `check` waited for every question's first sample before sending the rest, and its median wall
time went from 11.7 s to 21.7 s: 9 calls under `concurrency: 8` used to go out at once.

## Initial direction

Draw the nonce once per question when the run starts, give it to the question's k samples (`Request.Nonce`), and let a
sample other than the first wait for its first only while there is other work for the worker to do. When a worker would
otherwise sit idle (a `check` of one skill), it does not wait. A re-ask (P-069) keeps drawing a fresh nonce.

Why this keeps the barrier: the nonce protects the verdict against content that forges the fence line. The content is fixed
before the nonce is drawn, it never sees the nonce, and no model reply is fed back into another sample's request. A nonce
shared by the k identical requests of one question is exactly as unguessable to that content as k different ones.

## Done criteria

- [x] `TestRun_SamplesOfAQuestionShareOneRequest` (`internal/judge`, real `NewHTTP` over `httptest`, `samples: 3`): the
  three request bodies of each question are byte-identical; each nonce is 32 lowercase hex digits; no two questions share a nonce
- [x] `TestHTTP_JudgeNonce`: `Judge` uses `Request.Nonce` when it is 32 lowercase hex digits, and draws its own when it is
  empty, malformed, or the request is a re-ask
- [x] `TestHTTP_ReaskCarriesTheRuleUnderAFreshNonce` additionally pins that, at `samples: 3`, every re-ask's nonce differs
  from the nonce its question shares and from every other re-ask's
- [x] `TestSchedule_*` (table-driven, no clock, no goroutines): at one worker the dispatch order is the table order; a
  sample waits for its first while other work is ready, and goes to the front when its first finishes; with more free
  workers than ready work, a waiting sample is released at once (the earliest question first), so a run with
  `concurrency` ≥ its call count dispatches every call before any finishes; a first that failed releases its samples like
  one that answered; triage tasks and `samples: 1` questions never wait
- [x] `TestRun_FewQuestionsAreNotSerialized`: a stub that answers only once every planned call is in flight (bounded
  wait) completes a `samples: 3` skill run with `concurrency` ≥ the call count
- [x] Reverse: `TestRun_ConcurrencyDoesNotChangeOutput`, `TestRun_DeadlineSkipsAreReported`,
  `TestRun_ProgressIsReportedAndSerialized` and every `Concurrency: 1` test that scripts replies in call order pass unchanged;
  `aguard llm preview --json` on a fixed fixture is byte-identical before and after, and `judge-prompt=` / `judge-excerpt=`
  in `aguard version` do not move
- [x] Measured on the pilot's 20 skills with the final build: `cached_prompt_tokens` share, skills with a hit, findings, and
  the median wall time of one `check` against the 11.7 s baseline
- [x] Docs: `docs/llm-judge.md` / `.zh-CN.md`, `docs/architecture.md` / `.zh-CN.md`, spec §5.2, `.claude/rules/judge.md`
- [x] `make verify` green with `go version` go1.23.5 and no toolchain switch; `head -3 go.mod` second line `go 1.23.5`

## Out of scope

- `prompt_cache_key` or any other new request field (open question 1). No request member is added; `chatRequest` is unchanged.
- Reordering the prompt (the nonce moved later in the system message, the fence moved): measured above to gain nothing at
  today's prompt sizes, and it would move `PromptVersion`.
- Triage: one call per artifact, never sampled; its nonce stays per call.
- The `--llm` output token count, the excerpt caps, and which passes run: they set what a question costs, not whether its
  repeats can be cached.
- Anything priced: the tool records what the endpoint reported (P-071) and computes no cost.

## Must not claim

- Never that a run "costs less", by how much, or at what rate cached tokens are billed. Only: an endpoint that caches prompts
  can now serve the repeat samples of a question from its cache, and `cached_prompt_tokens` reports whether it did.
- Never that a cache hit is guaranteed. The cache is the endpoint's and best-effort; OpenAI documents a 1,024-token
  minimum, so short questions (most hooks, the intent pass) are not cached at all. Endpoints without a prompt cache, every
  local one included, see no difference.
- Never that the barrier is weaker or "shared across calls" in general. One nonce is shared by the k identical requests of
  one question and by nothing else; a re-ask and every other question draw their own.
- Never that the samples are less independent: they were identical questions before, differing only in the nonce, and the
  sampling temperature above zero is what makes each one its own draw.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The proposal | `P-080: the proposal — the samples of a judge question never share a nonce, so none can be served from a prompt cache` |
| 2 | The client uses `Request.Nonce` when valid, otherwise draws as before | `P-080: the judge client fences a request with the nonce it is given, and draws its own when there is none or it is a re-ask` |
| 3 | `runTasks` draws one nonce per question and dispatches through a schedule where a sample waits for its first only while other work is ready | `P-080: one nonce per question, and a repeat sample waits for its first only while there is other work` |
| 4 | Documentation | `P-080: docs — the nonce is drawn per question, and repeat samples can be served from a prompt cache` |
| 5 | Done | `P-080: done — evidence, and the proposal moves to complete` |

## Open questions

1. Also send OpenAI's `prompt_cache_key`? Recommended: no. The k requests of a question are byte-identical, so the
   endpoint's own routing (a hash over the start of the prompt) already sends them to the same place; the key helps when many
   different requests share a prefix, which ours do not. It is a member some OpenAI-compatible servers do not know, and an
   unknown member is a refusal we would have to detect and retry without, as P-052 does for `temperature`. The final
   build's measurement (done criteria) is the check that it was not needed.
   **Decided (2026-10-19)**: as recommended.
2. When should a repeat sample wait for its first? Recommended: only while there is other ready work. Waiting is free when
   every worker is busy anyway (a scan of many artifacts) and costs a full call's latency when workers would sit idle (a
   `check` of one skill: the pilot's 11.7 s → 21.7 s). The rule never leaves a worker idle that the old schedule would
   have kept busy; it only changes which ready call a worker takes next.
   **Decided (2026-10-19)**: the maintainer asked that a `check` with few questions not wait for the first; the work-conserving
   rule is that.
3. Where is the nonce drawn? Recommended: in `runTasks`, when the run starts, not in the plan (`schedule`/`Plan`). The preview
   shares the plan and must not draw or show a nonce, and a nonce drawn at plan time would sit in a table that outlives
   the run.
   **Decided (2026-10-19)**: as recommended.
4. What if `crypto/rand` fails when the question's nonce is drawn? Recommended: leave `Request.Nonce` empty. The client then
   draws per call as before and fails the call if it cannot (fail closed, `LLM-000`); there is never a guessable barrier.
   **Decided (2026-10-19)**: as recommended.
5. Does `PromptVersion` move? Recommended: no. No request body changes shape; which nonce a body carries was always stood in,
   and a relation between two bodies' nonces is not in either body. `tool_version` identifies the build, as for every other
   change to how the client schedules calls.
   **Decided (2026-10-19)**: as recommended.
6. Does the client check the nonce it is given? Recommended: yes. Anything but 32 lowercase hex digits makes it draw its own,
   so a caller that fills the field wrongly gets today's behaviour and never a short or predictable fence.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR <number> (<date>; find the sha with git log --grep P-080 after the merge)
Released: vX.Y.Z (fill in after stage 5)
Evidence: TestRun_SamplesOfAQuestionShareOneRequest (internal/judge/dispatch_test.go): samples 3, concurrency 4, 9 calls → 3 nonces, each on 3 byte-identical request bodies, all 32 lowercase hex
Evidence: TestHTTP_JudgeNonce (internal/judge/nonce_test.go): a well-formed Request.Nonce fences both messages; empty, short, upper-case, non-hex, fence-character and re-ask cases each draw a fresh one
Evidence: TestHTTP_ReaskCarriesTheRuleUnderAFreshNonce now at samples 3: 18 calls, 9 re-asks; each question's three first calls share its nonce, all 9 repeats carry nonces of their own
Evidence: TestSchedule_DispatchQueue, 6 cases: one worker = table order; a sample waits while other work is ready; free workers never wait; the tail releases what waits; a failed first releases; triage and single questions never wait. TestRunTasks_NoncePerQuestion: one nonce per group, none on triage, the caller's table untouched
Evidence: TestRun_FewQuestionsAreNotSerialized: the stub answers only once all 9 calls are in flight → 9 calls, 0 failed. Mutation (the idle-worker release removed): it fails after its 5 s bound, as do two TestSchedule cases
Evidence: reverse — TestRun_ConcurrencyDoesNotChangeOutput, TestRun_DeadlineSkipsAreReported, TestRun_ProgressIsReportedAndSerialized and every Concurrency: 1 scripted test unchanged and green; llm preview --json on a corpus skill: 19,661 bytes, cmp identical between main and this branch; judge-prompt=b851dfd5412b, judge-excerpt=9 on both
Evidence: measured, 20 random benign skills (seed 2026), gpt-4.1-mini-2025-04-14, samples 3, concurrency 8, run side by side with main: 197 calls each, 0 failed; cached_prompt_tokens 0 → 148,736 of 334,107 (44.5%); 18 of 20 skills with a hit; median wall time of one check 12.8 s (main) → 12.2 s. The experimental build that always waited and sent prompt_cache_key: 45.8%, 21.7 s
Evidence: make verify: all gates passed; go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 3 "go 1.23.5"
```
