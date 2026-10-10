<!-- SPDX-License-Identifier: MIT -->
# 033 — A judge benchmark run has no tool that folds what the judge found: judge.jsonl cannot be regenerated, no per-(kind, rule) table exists, and a run that died cannot be resumed

- **Source**: new finding (2026-10-10), follow-up to P-022 ("No folding of the judge's findings in the rig" in its Out of scope)
- **Depends on**: P-022 (`ledger.JudgeUsage`, `run.SumJudgeUsage`, raw/ keeps the tool's own bytes), P-031 (`samples` in the judge summary)
- **Branch**: `p/033-judge-fold`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`baselines/cmd/baseline` drives aguard over the corpus and writes, per run, `verdicts.jsonl` (folded from deterministic
findings only), `ledger.jsonl` (each judged row carries `judge_usage`, P-022), `run.yaml`, `scorecard.txt` and, with
`-raw`, one `raw/<sample>.json` per sample: the JSON aguard printed. What the judge found exists only in `raw/`, and
`baselines/README.md` says folding it "is yours to do and to describe". Who is affected: anyone who measures the judge,
or tries to check a judge number this repository already publishes.

| Symptom | Consequence |
|---|---|
| The four committed judge runs (`baselines/results/aguard/*-llm-*`) carry a `judge.jsonl` and a judge-predicate `verdicts.jsonl` that a one-off script produced; their `run.yaml` says "Written BY HAND" and describes the predicate in prose (`fold:`) | Nothing in this repository can regenerate either file. The next judge run is folded by another one-off script, whose definitions (what `judge_any` means, which notes `llm_notes` counts, which severity a verdict carries) nobody can compare with the last one |
| No per-(artifact kind, rule) table exists; the per-rule split was never computed for any run | Which judge question produces the benign flags on which kind of artifact can only be answered by hand, from `raw/`, every time |
| The driver writes `ledger.jsonl` and `run.yaml` only after every sample finished | A judge run that dies after an hour (deadline, crash, a laptop lid) leaves `raw/` for the samples it finished and nothing else: no ledger, no usage totals, and no way to run only the rest and merge |
| A sample whose judge calls failed or were skipped is folded like any other | An incomplete answer is indistinguishable from a complete "nothing found" unless someone reads its `judge_failed` by hand; a retried sample has two raw files and no rule for which one counts |

The four committed runs cannot simply be re-folded to check them: their `run.yaml` names a release asset
`aguard-judge-raw-2026-09.tar.gz` (sha256 `10a7ba6e…faa4eafd`) "attached as a release asset", and no release of this
repository or of the corpus carries it (measured 2026-10-10). The only copy of their `raw/` is the one P-022 measured
against: the former repository's tree just before its P-039 moved `raw/` out. So a fold tool's committed regression
cannot rest on those runs; it has to rest on fixtures produced by the real binary, plus a proof that the tool's schema
is exactly the committed one.

## Initial direction

A fold command, `baselines/cmd/judgefold`, that never calls a model and makes no network connection: it reads one or
more run (or shard, or retry) directories' `raw/`, the work list and the corpus checkout, rebuilds each sample's ledger
row through the aguard adapter's own code, picks one answer per sample by one rule (the first complete answer wins),
and writes `judge.jsonl` (the committed schema plus the artifact `kind` on each vote), a judge-predicate
`verdicts.jsonl`, a generated `per-kind-rule.txt`, the merged `ledger.jsonl` and the list of samples still to run.
Touches `baselines/` and its README only.
