<!-- SPDX-License-Identifier: MIT -->
# 026 — A build gated with `--fail-on-llm` passes when the judge could not run: the exit code is 0 exactly when the judge is blind

- **Source**: new finding (2026-10-09), verified on `main`; the maintainer's plan names a distinct exit code `4` = "gate not evaluable" for it
- **Depends on**: none (P-004 gave `check` the same `--llm` / `--fail-on-llm` pair as `scan`)
- **Branch**: `p/026-fail-on-llm-unevaluable`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`--fail-on-llm` is the one switch that lets the judge fail a build. It is opt-in twice (the flag and `llm.authority: escalate`),
and a missing grant is refused rather than ignored, because "a gate that silently never fires is worse than no gate".

But `failGate` in `cmd/aguard/main.go` only asks whether findings reach a threshold. When the judge could not evaluate — it did
not run at all (endpoint down, API key unavailable, endpoint refused, `llm.enabled: false`), or some of its calls failed or were
never made (`llm.max_calls` budget, `llm.total_timeout` deadline) — there are no LLM findings, so the effective set holds only the
deterministic findings and the command exits `0`. The report carries an `LLM-000` note and the JSON `judge` block says `ran:
false` or `failed: N`, but no CI step reads those: a pipeline that relies on `--fail-on-llm` goes green exactly when the judge is
blind, which is the shape the authority refusal exists to prevent.

### Measured on `origin/main` (155865b)

Binary built from the base; a benign two-file skill (`SKILL.md` + `add.sh`, no deterministic finding), `scan --root <fixture>/.claude
--inbox off` and `check <fixture>/.claude/skills/notes`, every config with `authority: escalate`, `max_retries: 0`, `samples: 1`,
`--json --quiet`; the fake endpoint is a local Python server answering "not flagged" (script and output in the scratch directory,
not committed). `scan` and `check` gave the same exit code on every row:

| Situation | `judge` block in the JSON | exit (`scan` / `check`) |
|---|---|---|
| working endpoint, `--llm --fail-on-llm high` | ran, 2 calls, 0 failed, 0 skipped | 0 / 0 |
| closed local port (`http://127.0.0.1:1`) | ran, 2 calls, **2 failed** | **0 / 0** |
| endpoint refused (`http://` on a remote host) | **ran: false** | **0 / 0** |
| `api_key_file` that does not exist | **ran: false** | **0 / 0** |
| `llm.enabled: false` with `--llm` | **ran: false** | **0 / 0** |
| `llm.max_calls: 1` (2 calls planned) | ran, 1 call, **1 skipped** | **0 / 0** |
| `llm.total_timeout: 1s`, endpoint answering after 5 s | ran, 2 calls, **2 failed** (cancelled in flight) | **0 / 0** |
| `--fail-on-llm high` without `--llm` | **no judge block** | **0 / 0** |
| `--fail-on high` only (no `--fail-on-llm`), closed port | ran, 2 failed | 0 / 0 |
| `check` on a skill with a deterministic high, `--fail-on critical --fail-on-llm high`, closed port or working endpoint | ran | 1 |

Every bold row is a pipeline that asked the judge to gate it and was told "pass" by a judge that saw nothing, or saw only part.
Exit `3` is taken (`clean` only: acted partially); nothing uses `4`.

## Initial direction

Give "the LLM gate could not be evaluated" its own exit code, `4`, decided in `failGate` from the run's `JudgeSummary`, below a
real gate hit (`1`) and a run error (`2`), and never reached when `--fail-on-llm` is not set. Update every place that documents
exit codes.

## Done criteria

- [x] `TestFailGate_LLMGateNotEvaluable` (`cmd/aguard/fail_on_llm_test.go`, new, unit, table): with `--fail-on-llm high` granted and
  no threshold met, `failGate` returns a `failExit` with code **4** when `Judge` is nil (no `--llm`), when `Judge.Ran` is false, when
  `Judge.Failed > 0`, and when `Judge.Skipped > 0`; the error text names which of the four it was (`--llm`, the summary's `Reason`,
  the failed count, the skipped count). Red on the base: every row returns nil (exit 0)
- [x] Precedence rows in the same table: a deterministic hit on `--fail-on` with a judge that did not run → code **1**; a qualified LLM
  finding at the threshold from a run where other calls failed → code **1** (a gate that fired is an answer, partial or not); a typo or
  a missing grant → still a plain error (exit 2), unchanged (`TestFailGate_DeterministicHitDoesNotMaskARefusal` stays green as is)
- [x] `TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind` (same file, drives the built binary, `scan` and `check`): closed local port, an
  `http://` remote endpoint (refused by `CheckEndpoint`, nothing dialled), a missing `api_key_file`, `llm.enabled: false`,
  `llm.max_calls: 1`, and `--fail-on-llm` without `--llm` all exit **4**, with one stderr line carrying `(exit 4)` and the reason, also
  under `--quiet`. Red on the base: every row exits 0 with an empty stderr
- [x] **Reverse assertions** (same test): a judge that ran over everything and found nothing exits **0** with an empty stderr; the
  same broken configurations with `--fail-on` and **no** `--fail-on-llm` exit exactly what the base exits (0 below the threshold, 1
  at it) — the deterministic gate never sees the judge's state; a deterministic high with a closed port and `--fail-on-llm high`
  still exits 1
- [x] The load-time gate never runs the judge: `TestGateScannerNeverEnablesLLM`, `TestHookRunnerNeverFails` and the existing
  `TestFailGate*` / `TestE2E_*` stay green, the latter with one fixture change only (`llmResult` gains the judge summary a real run
  with LLM findings always has)
- [x] Every place that lists exit codes lists `4`: README pair, CLAUDE.md command section, `docs/llm-judge*.md` gating section,
  spec §3, the two plugin skills that list exit codes (paraphrased), the npm launcher's comment, and the `--fail-on-llm` flag help.
  README's "coverage notes never change … the exit code" sentence, which this makes wrong, is corrected. `./bin/aguard check plugin
  --fail-on low` still exits 0
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no toolchain switch; no new dependency

## Out of scope

- **`--fail-on` alone never changes**: `failGate` reads the judge's state only when `--fail-on-llm` is set
- **The Downloads (inbox) judge**: inbox items never gate (`hasAtLeast` walks `Artifacts` only), so `Inbox.Judge` shortfalls never
  produce exit 4
- **No change to which LLM findings qualify** (grounding, consensus, `advisoryOnly` for the MCP pass), to `JudgeSummary`'s schema, or
  to the JSON / SARIF / HTML / markdown / terminal report content
- **A shortened excerpt is not "not evaluated"**: the head/tail cap, the shortened MCP configuration note and `LLM-005` discarded
  verdicts all describe a judge that answered; they keep their `LLM-000` / `LLM-005` disclosure and do not reach the exit code
- **The load-time gate** (`internal/gate`, `cmd/aguard/gate*.go`): it never consults a model and never calls `failGate`
- **`clean`'s exit 3**, `hack/github-action.yml` (it does not pass `--fail-on-llm`, and its `outcome == 'failure'` already covers any
  non-zero code), the baselines adapter (never passes `--fail-on-llm`), and `CHANGELOG.md` (written at release)
- No change to `internal/collect`, `internal/detect`, `internal/judge`, `internal/score`, `internal/report`

## Must not claim

- **Exit 0 under `--fail-on-llm` does not mean the judge read every byte**: excerpts are capped and the disclosure for that stays an
  `LLM-000`. What exit 0 now means is "the judge was asked every question it planned and answered each one"
- **Exit 4 is not a finding** and says nothing about the artifact's risk; do not describe it as "unsafe" or "blocked for risk". It
  says the build asked for an answer the run could not give
- Do not say the judge "found nothing" on a 4, and do not say a manipulation was ruled out on a 0 ("silence proves nothing" stays
  true)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Unit and binary tests for exit 4, its precedence and its reverse rows, run red on the base | `cmd: tests — --fail-on-llm exits 0 when the judge did not run or ran short (P-026)` |
| 2 | `failGate` returns exit 4 from the run's `JudgeSummary` when `--fail-on-llm` is set and no gate fired; `main` prints its one-line reason; the flag help says so | `cmd: --fail-on-llm exits 4 when the judge could not evaluate every artifact (P-026)` |
| 3 | README pair, CLAUDE.md, `docs/llm-judge*.md`, spec §3, plugin skills, npm launcher comment list exit 4; the README coverage-note sentence is corrected; a guard note in `.claude/rules/pipeline.md` | `docs: exit code 4 is listed wherever exit codes are (P-026)` |
| 4 | "Done" in this file, the index | `proposals: P-026 (P-026)` |

## Open questions

1. **Which code?** `3` is `clean`'s "acted partially"; reusing it would give one number two meanings across commands, and a script
   that wraps both could not tell them apart.
   **Recommendation**: `4`, as the maintainer's plan names it.
   **Decided (2026-10-09)**: as recommended.
2. **Does partial coverage count, or only "did not run at all"?** A run where some calls failed or the budget/deadline left some
   unmade has judged part of the artifacts. The `--fail-on-llm` answer is a statement about every artifact, and for the ones the
   missing calls covered the run cannot make it — and the missing one may be exactly the one that matters.
   **Recommendation**: any `Failed > 0` or `Skipped > 0` is "not evaluable" (exit 4). No tolerance threshold: any number would be one
   nobody can justify, and the existing `LLM-000` already says which artifacts were missed.
   **Decided (2026-10-09)**: as recommended.
3. **`--fail-on-llm` without `--llm`**: refuse up front with exit 2 (like a missing grant), or scan and exit 4?
   **Recommendation**: exit 4, after the scan, like every other way the judge can fail to run. An up-front exit 2 would make the
   judge's state block the deterministic answer — no report, and `--fail-on` never evaluated — while exit 4 keeps the report and
   lets a deterministic hit still exit 1. It also keeps one rule for one condition ("the judge did not run"), where splitting it
   by "knowable before the scan" would put a missing key under 2 and an unreachable endpoint under 4.
   **Decided (2026-10-09)**: as recommended.
4. **Precedence.** **Recommendation**: `2` (refused or run error, unchanged: thresholds and grant are still checked before
   anything is collected) > `1` (any gate fired — the deterministic one, or a qualified LLM finding from the part of the run that did
   complete; a fired gate is an answer, partial or not) > `4` > `0`. `--fail-on` alone never reaches 4.
   **Decided (2026-10-09)**: as recommended.
5. **What counts as "the judge answered"?** **Recommendation**: only the summary's own counts — `Ran`, `Failed`, `Skipped`. A
   shortened excerpt, an `LLM-005` discarded verdict and the advisory-only MCP pass are all answers; counting them would make exit 4
   fire on any large artifact and teach users to ignore it. A summary that ran over zero planned calls (nothing the judge asks
   about) is evaluable.
   **Decided (2026-10-09)**: as recommended.
6. **Say why on stderr?** Exit 1 prints nothing (the report shows the finding). Exit 4's reason is in the report too, but under
   `--json` or `--quiet` a CI log would show a bare non-zero code — the silence this item removes.
   **Recommendation**: one stderr line, also under `--quiet` (which means "errors only", and this is the reason for a failing exit),
   naming the reason; nothing on stdout, so `--json` stays parseable and the report content is unchanged.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #46 (2026-10-10; find the sha with git log --grep P-026)
Released: v0.20.0
Evidence: TestFailGate_LLMGateNotEvaluable and TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind (cmd/aguard/fail_on_llm_test.go): red on the base
  (re-run on origin/main 0cc9391 after the rebase) on exactly the 16 not-evaluable rows — 4 unit rows return nil, 12 binary rows
  (scan and check × closed port, http remote refused, missing api_key_file, llm.enabled false, max_calls 1, no --llm) exit 0 with an
  empty stderr — and green after
Evidence: the measured table, re-run with the fixed binary: every bold row 0 → 4 on scan and on check (also llm.total_timeout 1s against
  a slow endpoint, 0 → 4), with one stderr line "--fail-on-llm could not be evaluated (exit 4): …" naming the reason; working endpoint
  0 → 0; --fail-on high / low only with a closed port 0 → 0; deterministic high with --fail-on-llm 1 → 1
Evidence (reverse assertion): the 18 reverse and precedence rows pass on the base and after — "ran fully, found nothing" exits 0 with an
  empty stderr, "--fail-on high only, closed port" 0 and "--fail-on medium only, closed port" 1 on both commands, a fired gate exits 1
  over a blind judge, a typo still exits 2; TestFailGate_DeterministicHitDoesNotMaskARefusal, TestFailGateFlags_RefusedBeforeTheJudge,
  TestGateScannerNeverEnablesLLM and TestHookRunnerNeverFails green unchanged; the only edit to an existing test is the llmResult
  fixture gaining its judge summary
Evidence (not done): git diff --stat origin/main...HEAD -- internal/ hack/ baselines/ CHANGELOG.md cmd/aguard/gate.go cmd/aguard/inbox.go
  go.mod go.sum → empty; ./bin/aguard check plugin --fail-on low → exit 0, 100/100
Verify: make verify → "verify: all gates passed"; go.mod line 2 go 1.23.5; go version go1.23.5, no toolchain switch
```
