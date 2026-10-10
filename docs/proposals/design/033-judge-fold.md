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

## What the committed files mean, measured

The `fold:` prose leaves four definitions open. Each was read off the committed files and checked against the raw/ of
all four runs (extracted, not committed, from the former repository's tree just before its P-039, the copy P-022 used),
2,086 rows:

| Field | Definition that reproduces every committed row |
|---|---|
| `static` | a deterministic finding (`score.Deterministic`) at or above `high` |
| `judge` | `static`, or an escalated LLM finding at or above `high` whose rule is not `LLM-009` (on the glm run, whose binary predates P-019, `LLM-009` escalated 12 times and never counted) |
| `judge_any` | `static`, or any vote-carrying LLM finding at **`medium`** or above, escalated or not (`low` does not count: 8 glm rows) |
| `escalated_rules` | sorted rule ids of escalated LLM findings at or above `high`, `LLM-009` included when it escalated |
| `llm_notes` | the scan-level `notes` by rule id, `COV-000` included, in first-appearance order (s3 runs); the two `samples: 1` runs wrote the same counts key-sorted (20 rows each differ only in key order) |
| `votes` | every LLM finding with a dimension, in report order; `k`/`n` from the `[k of n samples agreed]` the judge appends, `severities` from `[severities: …]` when the binary wrote it |
| `triage_calls`, `questions` | the ledger's `triage_calls` (P-022); `(calls − triage_calls) / samples` |
| verdict severity | the highest severity among deterministic findings and escalated non-`LLM-009` LLM findings, flagged or not |
| verdict dimensions | the corpus dimensions of the findings that carried the flag, static and escalated alike |

The committed `verdicts.jsonl` files are in work-list order; the driver's `corpus.WriteVerdicts` sorts by sample. The
corpus scorer reads either.

## Done criteria

- [ ] `TestSchema_RoundTripsTheCommittedJudgeFiles` (`baselines/judgefold/schema_test.go`): every line of the four
  committed `judge.jsonl` (2,086) decodes into the tool's row type and re-encodes to the same bytes; a vote with no
  `kind` stays without one, `votes`/`triage_calls`/`questions` stay absent on the two `samples: 1` runs, `llm_notes`
  keeps its key order
- [ ] `TestJudgefold_Golden` (`baselines/cmd/judgefold/golden_test.go`): the aguard binary built from this tree, driven
  through `aguard.Adapter.Scan` against a scripted endpoint on 127.0.0.1 (three passes per question, answers chosen per
  pass), then folded with the binary deleted: a question flagged 3 of 3 escalates and flips `judge`; 1 of 3 is a vote
  with `escalates: false` that changes no verdict; an `LLM-009` at `high` sets `judge_any` and never `judge`; a sample
  whose calls failed is incomplete in the first directory and the retry directory's complete answer replaces its whole
  row; a sample cut by `max_calls` stays incomplete, is counted in the table header and listed in `incomplete.jsonl`; a
  `check`-routed sample is scored without a judge. `judge.jsonl`, `verdicts.jsonl`, `ledger.jsonl` and
  `per-kind-rule.txt` equal the golden bytes
- [ ] `TestRebuild_EqualsScan` (`baselines/adapter/aguard/rebuild_test.go`): for a `scan`-routed and a `check`-routed
  sample, the row `Rebuild` makes from raw/ equals the row `Scan` returned, field for field, `judge_usage` included
- [ ] `TestSelect_*` (`baselines/judgefold/select_test.go`): the first complete answer wins in argument order; an
  incomplete first attempt is replaced as a whole (no vote of it survives); with no complete attempt the first one is
  kept and marked; a sample with no raw/ takes a `no-verdict` row from a directory's `ledger.jsonl`, and with none at all
  `ledger.Check` reports it, only `incomplete.jsonl` is written and the exit code is 1
- [ ] `TestWilson_MatchesTheCorpus`, `TestCell_*` (`baselines/judgefold/table_test.go`): Wilson 9/10 is
  [0.596, 0.982] like the corpus's own test; a cell prints a rate only when the half-width is at most 15 points (0/22
  prints a rate, 0/21 and 3/12 print the count alone); the header names the corpus/aguard kind swap and the incomplete count
- [ ] `TestSource_*` (`baselines/corpus/source_test.go`): each arm of the corpus's source definition; with
  `AGUARD_CORPUS` set, every source the port assigns equals the sample set `corpus samples --source <s>` prints
- [ ] `TestJudgefold_ImportsNoNetwork`: `go list -deps ./baselines/cmd/judgefold` contains no `net` or `net/…` package
- [ ] Reverse assertion: every existing test in `baselines/adapter/aguard`, `baselines/cmd/baseline`, `baselines/run` and
  `baselines/ledger` passes without a character changed; the driver's static output on a corpus subset is byte-identical
  before and after (`ledger.jsonl`, `verdicts.jsonl`; `run.yaml` apart from `started_at`)
- [ ] Measured, not committed: folded from the former repository's raw/ of the four committed runs, `judge.jsonl` equals
  the committed rows 2,086/2,086 apart from the new `kind` and the 40 key-order rows above, and `verdicts.jsonl` equals
  the committed rows once both are sorted by sample
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`, no new dependency

## Out of scope

- **No change to `internal/`, `cmd/aguard/`, the canonical hash, `docs/rules.md`, `go.mod`/`go.sum`**, and no new rule:
  the binary already prints everything the fold reads
- **Nothing under `baselines/results/` changes**, and no committed run is re-folded or rewritten: the four `run.yaml`
  files and their `raw_archive:` block are the maintainer's to correct (a separate change), and the reproduction above
  is evidence, not a new committed file
- **The driver's run loop does not change**: no `-resume` flag. Resuming is "fold what is there, run the driver on
  `incomplete.jsonl`, fold both directories"; the driver already writes each raw/ file as its sample finishes. The only
  driver change is the sentence in its upload notice that says the judge's findings are "folded by hand"
- **No new verdict predicate and no re-weighting**: the fold applies the committed `fold:` predicate word for word; the
  table is descriptive and decides nothing
- **No model call, no re-asking, no network**: the fold never executes aguard and imports no networking package
- No change to the ccaudit / cisco / sarif adapters; `fill`, `FlaggingRules`, `judgeUsage` and `DimensionMap` keep
  their logic (the aguard adapter only gains `Rebuild`, which calls them)
- `docs/corpus-benchmark.zh-CN.md`: no sentence there becomes false, so it is not touched

## Must not claim

- Do not call any number in `per-kind-rule.txt` a gate, a tier, a threshold for a decision, or a cost; it is a
  description of one run, on one model, one day, one sampling, like every judge number here
- Do not say the four committed `judge.jsonl` files were regenerated or verified in this repository: the reproduction
  ran on a copy of raw/ that is not in this repository and is not committed
- Do not say the per-(kind, rule) denominator counts the questions actually asked of each artifact: raw/ records the
  questions per sample, not per artifact, so a kind's rules share one denominator (open question 4)
- Do not say a resumed run equals an uninterrupted one: a retried sample is a second draw from the model
- `FP` here means "a benign sample the judge flagged", the corpus's own caution: a benign label is `assumed`, not reviewed

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The failing tests: schema round trip, fold predicates, selection, table cells, source, `Rebuild`, no-network | `baselines: tests — nothing folds what the judge found, and raw/ cannot be turned back into a ledger row (P-033)` |
| 2 | `aguard.Adapter.Rebuild`: the row `Scan` returned, rebuilt from raw/ by the same code (`scanned`/`checked` shared with `Scan`) | `baselines: the aguard adapter rebuilds a sample's ledger row from the bytes it kept in raw/ (P-033)` |
| 3 | `corpus.Source`: the corpus's own definition of a sample's source, read from its annotation | `baselines: the corpus's source of a sample, read from its annotation by the corpus's own rule (P-033)` |
| 4 | `baselines/judgefold`: the committed schema plus `kind`, the fold of one answer, and the one selection rule | `baselines: fold one sample's judge answer into the committed judge.jsonl schema, and pick one answer per sample (P-033)` |
| 5 | `per-kind-rule.txt`: FP, FP per artifact, FP_any, recall, Δrecall, hard negatives and per-source rows, with Wilson intervals by the corpus's rule | `baselines: a generated per-(kind, rule) table of what the judge flagged, with the corpus's interval rule (P-033)` |
| 6 | `baselines/cmd/judgefold`: inputs, outputs, `ledger.Check`, `run.SumJudgeUsage`, exit codes; the golden end-to-end test | `baselines: judgefold — fold, merge and resume judge runs from raw/ with no model and no network (P-033)` |
| 7 | `baselines/README.md` says how to fold and resume; the driver's upload notice stops saying "folded by hand" | `docs: baselines README says how to fold and resume a judge run (P-033)` |
| 8 | This file, the index | `proposals: P-033 (P-033)` |

## Open questions

1. **Where does the fold live?**
   **Recommendation**: a command `baselines/cmd/judgefold` over a package `baselines/judgefold`, reusing
   `baselines/adapter/aguard` (row rebuild), `ledger`, `run` and `corpus`. Not a mode of the driver: the driver runs a
   tool, the fold must never run one, and keeping them apart is what lets a test prove the second.
   **Decided (2026-10-10)**: as recommended.
2. **What is a sample's "source"?** The annotation's `origin.source` is free text, a per-sample URL for most samples.
   **Recommendation**: the corpus's own definition (`populationOf` in its `harness/cmd/corpus/basis.go`, which its
   `samples --source` and its scorecard use), ported to `corpus.Source` and kept in step by hand like `DimensionMap`,
   with a test that, given a corpus checkout, compares the port with `corpus samples --source` for every source. A
   per-URL grouping could not be compared with any scorecard.
   **Decided (2026-10-10)**: as recommended.
3. **Which small-denominator rule?** The plan assumed "n < 22: counts, no rate".
   **Recommendation**: the corpus's actual rule (`FigureThresholdPoints = 15` in its `harness/internal/score`): a rate
   only when the Wilson 95% half-width is at most 15 points, otherwise the count alone with the half-width. "n < 22" is
   that rule for an all-zero result only (`tripwire.MinimumN`); 3/12 has n < 22 and 3/40 has not, and both are counts.
   **Decided (2026-10-10)**: as recommended.
4. **What does "an artifact of that kind on which that question was asked" count, when raw/ records questions per
   sample and not per artifact?**
   **Recommendation**: a complete sample that has at least one artifact of the kind and `questions > 0`; a row only for
   a (kind, rule) pair that carries at least one vote in the run, since a vote proves the question was asked of that
   kind. Every rule of a kind therefore shares the kind's denominator, which overstates it for the conditional questions
   (`LLM-004` asks only when a payload decodes, `LLM-006` only after a static cross-file chain); the header says so.
   Inferring each artifact's plan would copy `internal/judge`'s planner into the rig.
   **Decided (2026-10-10)**: as recommended.
5. **What is a complete answer?**
   **Recommendation**: a scored row whose judge summary says `failed: 0` and `skipped: 0`; on a `scan`-routed sample the
   summary must also exist and say `ran: true` (a judge that did not run answered nothing). A `check`-routed sample is
   complete without a judge, which never runs on that path; so is a `no-verdict` row. Incomplete rows stay in
   `judge.jsonl` with `incomplete` saying why, are left out of every table row and counted in its header.
   **Decided (2026-10-10)**: as recommended.
6. **How does a died run resume?**
   **Recommendation**: the fold always writes `incomplete.jsonl`, the work-list lines of every sample without a complete
   answer; `baseline -samples incomplete.jsonl -out <dir2> -raw <dir2>/raw` runs only those (its own tripwire may refuse
   to publish a subset, which leaves raw/ in place), and folding `<dir1> <dir2>` merges them. When `ledger.Check` fails
   against the full work list, the fold writes only `incomplete.jsonl` and exits 1, the driver's "nothing is written
   until it holds".
   **Decided (2026-10-10)**: as recommended.
7. **`llm_notes` key order**: the s3 runs kept first-appearance order, the `samples: 1` runs sorted keys.
   **Recommendation**: first-appearance order, the schema the task names (s3); the round trip keeps whatever order a
   file has. Key order carries nothing for a reader of a JSON object.
   **Decided (2026-10-10)**: as recommended.
8. **`judge_any` counts `medium`, the table's `FP_any` asks for `high`.**
   **Recommendation**: keep both as measured and stated: `judge_any` is the committed field (static, or any vote at
   `medium` or above), `FP_any` is the table's column (any vote at `high` or above, of that rule on that kind). Changing
   the field would break the round trip with every committed row.
   **Decided (2026-10-10)**: as recommended.
9. **Where does `samples` (n) come from when raw/ predates P-031?**
   **Recommendation**: the judge summary's `samples` when present; otherwise `-judge-samples N`; neither, on a sample
   that made judge calls, is an error, and so is a summary that disagrees with the flag. A vote without its
   `[k of n samples agreed]` is read as 1 of 1 only when n is 1; otherwise the fold refuses rather than guess.
   **Decided (2026-10-10)**: as recommended.
10. **Does the fold write `ledger.jsonl` and the usage totals?**
    **Recommendation**: yes to the merged `ledger.jsonl` (the rebuilt rows, sorted like the driver's) and the
    `run.SumJudgeUsage` totals in the table header; no `run.yaml`, whose attribution is the operator's (or the
    driver's) to write.
    **Decided (2026-10-10)**: as recommended.
