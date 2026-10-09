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

## Initial direction

Give "the LLM gate could not be evaluated" its own exit code, `4`, decided in `failGate` from the run's `JudgeSummary`, below a
real gate hit (`1`) and a run error (`2`), and never reached when `--fail-on-llm` is not set. Update every place that documents
exit codes.
