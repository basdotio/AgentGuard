<!-- SPDX-License-Identifier: MIT -->
# 046 — `--fail-on-llm` can fail a build on one draw: `authority: escalate` is accepted with `samples: 1`

- **Source**: the LLM-judge precision plan (2026-10-10), item B4; measured under P-041 (2026-10-12)
- **Depends on**: P-041 (the numbers below are its)
- **Branch**: `p/046-escalate-needs-samples`

## Problem

`--fail-on-llm` is opt-in twice: the flag, and `llm.authority: escalate` in the config. Nothing asks how many times
each question is put to the model. With the default `samples: 1`, a build fails on a single draw from a probabilistic
process, and P-041 measured what a single draw costs now that the finding's severity follows the category the model
names (one draw in a high category is high, with no second opinion):

| Run (gpt-4.1-mini, binary dd8001b) | `samples` | benign escalated at high |
|---|---|---|
| 224 subset, random benign 100, two runs | 3 | 0 / 0 (the 2026-09-29 run: 1) |
| 819 subset, benign 500 | 1 | 11 (the 2026-09-28 run: 6) |
| every benign sample, 3,220 | 1 | 53 = 1.65% [1.26, 2.15] |

The same two 224 runs at `samples: 3` agreed on the weight-at-high decision for 95.1% of the questions either run
flagged; `judge-comparison-2026-09-25.zh-CN.md` recorded 9.2% first-vote disagreement between two `samples: 1` runs
of the same prompt. A pipeline that gates on the judge at `samples: 1` fails builds on that noise, and nothing in the
tool says so: `docs/llm-judge.md` recommends `samples: 3` in a sentence, the gate accepts anything.

## Initial direction

Refuse `--fail-on-llm` unless the configuration asks each question at least three times, exactly as it is refused
without the authority grant today: exit 2 before any request is sent, an error that names `llm.samples: 3`. The
check sits where authority is checked (`validateFailGates`, called before collection and again in `failGate`), not in
`config.Load`: a configuration whose judge knobs are inconsistent must not stop `hook`, `scan` or `approve`, which
never read the judge's authority.

## Done criteria

- [x] `TestLLMConfig_GateReady` (`internal/config/config_test.go`, new): `GateReady` is nil only with
  `authority: escalate` and `samples ≥ 3`; advisory names `llm.authority: escalate`, escalate with 0 / 1 / 2 names
  `llm.samples: 3`; 5 is accepted; the constant is pinned at 3
- [x] `TestFailGate_LLMGateNeedsSamples` (`cmd/aguard/main_test.go`, new): `failGate` with authority granted and
  `samples` 0 / 1 / 2 returns a configuration error (not the exit-1 sentinel) naming `llm.samples: 3`; with 3 or 5 a
  qualified LLM high trips the gate; `--fail-on high` alone with `samples: 1` is untouched. **Red on the base**: the
  bool signature cannot express it; the first case passed the gate
- [x] `TestCheckCmd_FailOnLLMNeedsSamples` (`cmd/aguard/check_llm_test.go`, new): the built binary, `check --llm
  --fail-on-llm high` with `escalate` and `samples: 1` → exit 2, stderr names `llm.samples: 3`, stdout empty, the
  judge endpoint saw **0** requests; the same config without `--fail-on-llm` runs the judge and exits 0
- [x] Reverse assertions, green without a character changed in what they assert: `TestFailGate_LLMGateNeedsAuthority`,
  `TestCheckCmd_FailOnLLMNeedsAuthority`, `TestFailGateFlags_RefusedBeforeTheJudge` (advisory rows still refused for
  authority, valid rows still exit 1 with the judge reached), `TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind` and
  `TestFailOnLLM_TargetTheJudgeAsksNothingAbout` (exit 4 cases unchanged), `TestE2E_JudgeEscalatesWithoutTouchingTheDeterministicScore`,
  `TestE2E_AdvisoryAuthorityRefusesTheGate`; their fixtures that gate now ask three times
  (`writeJudgeConfigSamples`, `writeGateJudgeConfig`), everything else keeps `samples: 1`
- [x] Docs: `docs/llm-judge.md` + zh (gating section, the `authority` row, the flag table), spec §3 gate paragraph,
  `.claude/rules/invariants.md` item 4, `docs/architecture.md` + zh; `docs/rules.md` unchanged (`make docs` no-op)
- [x] `make verify` green; `go.mod` line 2 unchanged; `internal/judge`, `internal/score`, `internal/gate`,
  `internal/detect` untouched

## Out of scope

- Any change to how `samples` is used once the run is allowed: majority, temperature, vote text, `judgefold`
- `config.Load` validation of `authority`/`samples` consistency (deliberately not: see Initial direction)
- `llm setup` / `llm status` / `llm preview` wording about the bar (preview already prints calls × samples)
- The hosted-endpoint tier the spec reserves (`authority` / `samples` asserted server-side): not implemented, not touched

## Must not claim

- Do not say three samples make the judge "reliable": they make the gate fail on a majority rather than one draw;
  the 224 runs still disagreed on 5 of 102 questions
- Do not say `samples: 1` is unsafe for `scan --llm`: without the gate the judge informs, and `overall_effective` is
  shown either way; the bar is on consequences
- Do not present the benign numbers as rates of the world: n and source are in P-041's Done

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | `config.MinSamplesToEscalate` and `LLMConfig.GateReady`; `validateFailGates` / `failGate` take the config and ask it; tests red then green; fixtures that gate ask three times | `gate: --fail-on-llm needs llm.samples 3 or more as well as the authority grant (P-046)` |
| 2 | Docs pair, spec, invariants, architecture pair | `docs: the LLM gate needs a vote — samples 3 or more — beside the authority grant (P-046)` |
| 3 | This file's Done, the index | `proposals: P-046 (P-046)` |

## Open questions

1. **Where is the bar enforced?** **Recommendation**: at the gate (`validateFailGates`), not at `config.Load`. Load
   validates every command's config, and `hook`, `scan` and `approve` never read the judge's authority; a judge knob
   that is merely inconsistent must not take the load-time gate down. The same place already refuses the missing
   grant, so the two refusals share one message shape and one test file.
   **Proceeded under the recommendation (2026-10-12)** on the maintainer's "do B4"; a different answer before merge
   moves one call.
2. **Which bar?** **Recommendation**: `samples ≥ 3`, as `config.MinSamplesToEscalate`. Two is unanimity of two, not a
   majority bar, and nothing was measured at two; three is where P-041's numbers were taken. Larger odd counts are
   accepted; even counts above three are accepted too (the majority is `n/2 + 1`, as before).
   **Proceeded under the recommendation (2026-10-12).**
3. **Should the run warn when `authority: escalate` is configured with `samples: 1` but `--fail-on-llm` is not
   passed?** **Recommendation**: no. The configuration is not wrong without the flag; the moment the flag is passed the
   refusal says exactly what to change.
   **Proceeded under the recommendation (2026-10-12).**

## Done

```
Merged: PR #65 (2026-10-12; find the sha with git log --grep P-046)
Released: v0.22.0
Evidence: TestLLMConfig_GateReady (internal/config/config_test.go): advisory → "llm.authority: escalate"; escalate with 0 / 1 / 2 → "llm.samples: 3"; 3 and 5 ready; MinSamplesToEscalate pinned at 3
Evidence: TestFailGate_LLMGateNeedsSamples (cmd/aguard/main_test.go): escalate + samples 0 / 1 / 2 → configuration error naming llm.samples: 3, not *failExit; 3 and 5 → *failExit on a qualified high; --fail-on high alone with samples 1 → nil. On the base the gate took a bool and passed the samples-1 case
Evidence: TestCheckCmd_FailOnLLMNeedsSamples (cmd/aguard/check_llm_test.go): built binary, escalate + samples 1 + --fail-on-llm high → exit 2, stderr names llm.samples: 3, stdout empty, 0 requests at the endpoint; the same config without the flag → exit 0 with the judge reached
Evidence: reverse assertions green: TestFailGate_LLMGateNeedsAuthority, TestCheckCmd_FailOnLLMNeedsAuthority, TestFailGateFlags_RefusedBeforeTheJudge (advisory rows refused for authority, valid rows exit 1 with the judge reached), TestFailGate_DeterministicHitDoesNotMaskARefusal (gains a "no vote" row), TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind, TestFailOnLLM_TargetTheJudgeAsksNothingAbout, TestE2E_JudgeEscalatesWithoutTouchingTheDeterministicScore, TestE2E_AdvisoryAuthorityRefusesTheGate — fixtures that gate ask three times (writeJudgeConfigSamples, writeGateJudgeConfig)
Evidence: Out of scope — git diff --stat main -- internal/judge internal/score internal/gate internal/detect: empty; docs/rules.md unchanged
Evidence: make verify: all gates passed on the branch; go.mod line 2 unchanged
```
