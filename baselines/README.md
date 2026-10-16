<!-- SPDX-License-Identifier: MIT -->
# baselines/

Every scanner's measurement against [agent-artifact-corpus](https://github.com/basdotio/agent-artifact-corpus),
including this project's own. One tree, so a comparison is a diff of two files rather than two
prose paragraphs written a month apart.

Provenance: the baseline-rig proposal (P-015) in the internal archive.

## Read this before reading a number here

**These figures are produced by adapters we wrote, at thresholds we chose, with sample placement
we decided.** That is stated in every `run.yaml` and printed above every scorecard, and it is not
a formality — it is the only defence left after a deliberate decision.

The first design put this directory in a separate repository, so that "the contestant is writing
the opponent's answer sheet" could be answered by pointing at the location. That was overruled on
2026-09-21: results live here, in the repository of the tool being measured. Location now answers
nothing, so the answer had to become something that cannot be forgotten — a required field
(`run.Validate` refuses a run without `adapter` and `placement`) and a refutable artifact.

**Any third-party figure here is provisional.** The corpus is public and its own guide invites
any vendor to measure itself — no account, no registration, nothing to ask us for. We measure
other tools because waiting is not an option for a project that needs a comparator now, not
because our measurement is better than theirs would be. When a vendor publishes its own run,
theirs is the number.

## Running one

```bash
# aguard — its own binary, its own shipped default threshold
make build
go run ./baselines/cmd/baseline -tool aguard -aguard bin/aguard \
  -corpus ../agent-artifact-corpus -out baselines/results/aguard/$(date -u +%F) -score-tool aguard
# -score-tool makes the scorecard carry aguard's own out_of_scope section: what aguard declares it
# does not detect (in the corpus's taxonomy/tools.yaml), and recall over the full AND the in-scope
# denominator side by side. Only a tool that declares out_of_scope needs it (P-034).

# cc-audit — pinned release, and a config file it REFUSES to run without
cc-audit init                                   # writes .cc-audit.yaml
go run ./baselines/cmd/baseline -tool ccaudit -bin /path/to/cc-audit \
  -policy /path/to/.cc-audit.yaml \
  -corpus ../agent-artifact-corpus -out baselines/results/ccaudit/$(date -u +%F)

# cisco skill-scanner — a Python package, so a pinned venv; strict mode refuses non-skill trees
uv venv --python 3.12 /tmp/cisco-venv
uv pip install --python /tmp/cisco-venv/bin/python cisco-ai-skill-scanner==2.1.0   # verify the wheel sha256 first
/tmp/cisco-venv/bin/skill-scanner generate-policy -o /tmp/cisco-policy/balanced.yaml  # NOT inside the corpus checkout
go run ./baselines/cmd/baseline -tool skill-scanner -bin /tmp/cisco-venv/bin/skill-scanner \
  -policy /tmp/cisco-policy/balanced.yaml \
  -corpus ../agent-artifact-corpus -out baselines/results/skill-scanner/$(date -u +%F)
```

### Measuring the judge (local, never committed)

The aguard adapter runs the binary with four environment variables and the flags it chooses,
on purpose: nothing from the operator's machine reaches a sample. Measuring the LLM judge needs
three exceptions, each narrow and opt-in (P-021):

```bash
export ZHIPU_API_KEY=…                      # the key stays in the environment, never in a file
go run ./baselines/cmd/baseline -tool aguard -aguard bin/aguard -corpus ../agent-artifact-corpus \
  -samples subset.jsonl -out /tmp/judge-run -raw /tmp/judge-run/raw -j 2 \
  -aguard-extra-args "--llm --config /path/to/judge.yaml" -aguard-env ZHIPU_API_KEY -aguard-timeout 25m
```

- `-aguard-extra-args` is appended to `scan`, and to `check` on one route only, and only when
  asked: `-aguard-check-llm` hands the flags to `check` for a tree that places nowhere but is MCP
  server source by aguard's own recogniser (the route `ledger.ServerSource` marks, P-059), so the
  judge's server pass (P-053, `LLM-010`) is measured on the corpus's server trees. Every other
  `check`-routed sample stays static, as in every judge run before P-059. `check` takes `--llm`
  on any tree (P-004); passing it everywhere would change what a rerun measures, which is a
  measurement decision of its own, not a rerun. A run that asked records `check_llm: true` in
  its `run.yaml`, and its fold needs `judgefold -check-llm` so a server-source row with no judge
  summary is counted as a judge that did not run rather than a static row by design.
- `-aguard-env` adds the named variables and nothing else; a bare `NAME` is copied from this
  process so the key never appears on a command line.
- `-aguard-timeout` replaces the per-sample limit; a judged sample takes minutes, not seconds.

Three consequences to know before quoting anything from such a run. **The verdicts do not
change**: the fold reads deterministic findings only, so `verdicts.jsonl` and the scorecard from a
`--llm` run are the static numbers. What the judge found exists only in `-raw`, and
`baselines/cmd/judgefold` folds it (see "Folding what the judge found"); what it cost is
folded by the driver (below). **`run.yaml` says
the content left the machine**: `uploads_samples` flips to true with a basis written for that
run, and `tool_extra_args` records the flags. **A judge run goes under `results/` in its own
directory, never as the baseline**: `results/aguard/<date>-llm-<model>/`, with `verdicts.jsonl`
folded at the judge's predicate (static ≥ high, or an escalated LLM finding ≥ high; see the
`fold:` line of its `run.yaml`), `judge.jsonl` with the per-sample static / judge / judge-any
split and the escalated rule ids, and `samples.jsonl` because the subset was a random draw.
`raw/` is not in the tree: it is the only primary evidence and cannot be regenerated, so it ships
as a release asset instead, named with its sha256 in the run's `run.yaml` under `raw_archive:` —
6.2 MB of model output quoting malicious samples does not belong in a source checkout (P-039).
**The four 2026-09 judge runs below have no published archive:** their `raw_archive:` names one
and its sha256, but no release carries it (checked 2026-10-10). Their `raw/` survives only in the
history of the former repository, before its P-039, so a reader of this repository cannot re-fold
them. Attach the archive to a release before a judge run's directory is committed, not after.
A judge number depends on the model, the
day and the sampling, so its `run.yaml` says so at the top, and it is reported beside the static
column with model, `samples`, subset size and date, never pooled with it. Two such
directories exist, on the same 819 samples: `results/aguard/2026-09-25-llm-glm-5.3-flash/` and
`results/aguard/2026-09-28-llm-gpt-4.1-mini/`; their `judge.jsonl` files line up sample by sample.
A third, `results/aguard/2026-09-29-llm-gpt-4.1-mini-s3/`, is the `samples: 3` counterpart on 224
of those 819 (a 200-sample random draw plus a 24-sample targeted stratum, listed in its
`strata.json`); its `judge.jsonl` adds the per-question vote counts. A fourth,
`results/aguard/2026-09-29-llm-gpt-4.1-mini-s3-votes/`, repeats that run on the same inputs with a binary
that records every vote's severity (P-035); its `compare.txt` sets the first vote against the majority on
one raw, and the two runs against each other. Four more, under `results/aguard/2026-10-12-llm-gpt-4.1-mini-p041-*/`,
measure P-041 (the judge's severity set from a category table): the 224 subset twice at `samples: 3`, the 819 subset at
`samples: 1` and, for the first time, every benign sample (3,220) at `samples: 1`; their `judge.jsonl` votes carry the
`categories` the model named, and their raw/ is one archive, `aguard-judge-raw-2026-10-p041.tar.gz`, whose sha256 each
`run.yaml` names. Four more, `results/aguard/2026-10-12-llm-gpt-4.1-mini-p048-*/`, belong to a withdrawn proposal and
are kept for what they show about the method: two `samples: 1` runs of nearly the same prompt on all 3,220 benign samples
swap about a third of their escalated set (`…-p048-benign-s1/compare.txt`), and a pair at `samples: 3` on the 541 benign
hooks and MCP catalogues (`…-p048-hm-s3-old/`, `…-p048-hm-s3-new/`) is the shape a wording change has to be measured in;
their raw/ is `aguard-judge-raw-2026-10-p048.tar.gz`. Four more, `results/aguard/2026-10-12-llm-gpt-4.1-mini-p049-*/`,
measure P-049 (a category the text does not show weighs medium) with the prompt unchanged: a second same-prompt draw of
the 541-sample panel as the control (`…-p049-hm-s3-control/`), the check on the panel, on the 819 subset and on every
benign sample; their raw/ is `aguard-judge-raw-2026-10-p049.tar.gz`. Five more,
`results/aguard/2026-10-13-llm-gpt-4.1-mini-p050-*/`, measure P-050 (the injection pass names sandbox escape) the way a
prompt change has to be measured after P-048: the current prompt and the clause side by side on 28 samples (9
skill-safety-bench + 19 hard negatives) and on 100 random benign skills at `samples: 10`, where each question's k/10 is
its flag probability, plus the 819 subset at `samples: 1`; their raw/ is `aguard-judge-raw-2026-10-p050.tar.gz`, with
the two wordings the proposal dropped. One more, `results/aguard/2026-10-13-llm-gpt-4.1-mini-p049-benign-s3/`, is the
full-benign denominator at `samples: 3` — the mode a gate is told to run — on main at e951123 (P-049 in): 30 of 3,220 =
0.93% [0.65, 1.33] against 35 at `samples: 1` on the same binary, so what is left is not the draw; its raw/ is
`aguard-judge-raw-2026-10-benign-s3.tar.gz` on v0.23.0. Two more, `results/aguard/2026-10-13-llm-gpt-4.1-mini-p053-*/`,
measure P-053 (the server-source pass, kind `server`, `LLM-010`) through `aguard check <dir> --llm` rather than the driver,
which never passes `--llm` to `check`: the 127 Cisco servers of the corpus, and 76 benign servers that are NOT in the corpus
(real source at pinned commits, named in `samples.jsonl` and `run.yaml`; the trees travel in the archive under
`benign-servers/`); `judge.jsonl` for the benign panel was written in judgefold's row shape by the run's own script,
since judgefold stages from the corpus. Their raw/ is `aguard-judge-raw-2026-10-p053.tar.gz`. Three more, `results/aguard/2026-10-13-llm-gpt-4.1-mini-p054-*/`,
measure P-054 (two readings fixed, no prompt change): the 224 subset beside a same-prompt control, and the 76-server benign
panel again — whose four draws under the P-054 binaries escalated 7, 5 and 6 against P-053's 4, the band a panel that
size moves in; the two earlier draws and a gpt-6.1-sol probe on the 224 subset are in `aguard-judge-raw-2026-10-p054.tar.gz`
under `earlier-draws/` and `model-probe/`. Two more things apply to a
judge run: the driver isolates aguard's environment, so a proxy the endpoint needs is passed by
name with the key (`-aguard-env OPENAI_API_KEY,HTTPS_PROXY,HTTP_PROXY,NO_PROXY`), and Go does not
read the macOS system proxy. Without that, a run whose endpoint needs a proxy answers nothing and
still exits 0, every call an `LLM-000`. Check a few `raw/` files for `"failed": 0` before waiting
on the rest.
Nine more, `results/aguard/2026-10-15-llm-gpt-4.1-mini-p059-*/`, belong to P-059 and are the first runs through the
server-source route with the judge (`check_llm: true` in their `run.yaml`; fold them with `judgefold -check-llm`).
`…-benign-servers-s3/` is the server pass's false-positive denominator: every benign server tree the corpus holds after
agent-artifact-corpus PR #2 (547: 89 derived from four pinned upstreams, 268 third-party servers by code search, 190 from
repositories created on or after 2026-09-20), three driver invocations merged into one fold. The eight
`…-heldout-*` directories are one work list each run with two binaries, v0.21.0 (the judge before P-041) and v0.24.0:
the malicious and the benign skills of `agent-skill-malware` (layer 2, paths under the corpus's `cache/`, written by
`scripts/prep-agent-skill-malware.py`), a seeded SkillHarm subset (`scripts/prep-skillharm.py`), and the corpus's 400
`gh-` skills from repositories created on or after the cutoff. Their per-sample rows were not read when they were made,
by design — a held-out set stays one only while nobody writes a prompt against its residue — so cite their totals
(`per-kind-rule.txt`, the decisions document) and nothing finer. All nine share one archive,
`aguard-judge-raw-2026-10-p059.tar.gz` on v0.24.0, each run's raw/ under the directory name used here.

The same four skill populations were then run through the two comparison tools, static, with their existing adapters
(`results/ccaudit/2026-10-16-p059-*/`, `results/skill-scanner/2026-10-16-p059-*/`, P-060). Three of the four work lists are
layer-2 samples under the corpus's `cache/`, which `corpus score` does not know: their `scorecard.txt` says so ("verdict(s)
named a sample not in the corpus") and is kept verbatim as the scorer's own statement; the figures are read from
`ledger.jsonl`. `results/mcp-scanner/2026-10-16-behavioral-*/` is Cisco mcp-scanner's LLM analyzer on the 547 benign server
trees and, as a probe, on the 127 Cisco servers, run by the script beside each directory because no adapter exists; the
decisions document says why that column is not a false-positive figure.

**What a judge run cost is folded by the driver, not by hand.** Each judged sample's ledger row
carries `judge_usage` — `calls`, `failed`, `skipped`, `triage_calls`, `retries`, `prompt_tokens`,
`completion_tokens` — read from the binary's own `--json` judge summary, and `run.yaml` sums them
under `judge_usage:` with the basis they rest on. `basis: reported` means the binary counted its
triage calls and retries itself. `basis: derived` means its output carried no such count (a
binary that predates them) and `triage_calls` was inferred as one per artifact with a
deterministic finding — the inference the four committed judge runs used, and exact only when
`skipped` is 0: a `max_calls` budget or the run deadline cuts the tail of the plan, which is where
each artifact's triage call sits. A total is written only when every judged sample supplied its
part; retries or tokens some sample did not report have no total, and `tokens_unreported_samples`
counts the samples that made calls without reporting tokens. `repaired` is the part of `calls` whose
reply the judge read only by dropping the stray `}` of an object closed one member early (P-034): an
answered call, not a failed one. It appears in a row and in `run.yaml` only when above 0; a binary that
does not print it predates the repair, so its count is 0. Questions asked are
`(calls − triage_calls) / samples`, with `samples` from the judge config (the summary does not
carry it), exact only when `skipped` is 0; a `judge.jsonl` folded from a new run takes
`triage_calls` from the ledger rather than inferring it. `raw/` holds the bytes the binary
printed, whitespace removed, not the rig's re-encoding of them: the re-encoding wrote
`"triage_calls":0` for a binary that never counted it, which is the one thing the basis is read
from.

### Folding what the judge found

```bash
go run ./baselines/cmd/judgefold -samples subset.jsonl -corpus ../agent-artifact-corpus \
  -out /tmp/judge-fold /tmp/judge-run [/tmp/judge-run-2 ...]
```

`judgefold` never runs aguard, never calls a model and links no networking package: it reads
the bytes a run already wrote. Each argument is a run directory holding the `raw/` the driver
kept with `-raw` (and its `ledger.jsonl`, when the run got that far); `-samples` is the run's
whole work list, and `-corpus` the checkout it read, which decides each sample's route (staged
for `scan`, or handed to `check`) and its corpus source. A `raw/` whose judge summary predates
`samples` (P-031) needs `-judge-samples N`, the judge config's value. It writes into `-out`:

| file | what it is |
|---|---|
| `judge.jsonl` | one row per work-list sample, in work-list order: the schema of the committed judge runs, plus each vote's `kind` (the aguard artifact kind it sits on) and `incomplete` when the answer is not complete |
| `verdicts.jsonl` | what `corpus score` reads, folded at the judge's predicate: malicious when a deterministic finding is at or above the threshold, or an escalated (grounded and k of n) LLM finding is and its rule is not `LLM-009` |
| `ledger.jsonl` | the merged ledger, every row rebuilt from `raw/` by the aguard adapter's own code (`Rebuild`) and checked with `ledger.Check` against the whole work list |
| `per-kind-rule.txt` | generated, never hand-edited: FP, FP per artifact, FP_any and recall per (aguard kind, rule), hard negatives on their own lines, Δrecall per kind, and rows per corpus source; the `run.SumJudgeUsage` totals in its header |
| `incomplete.jsonl` | the work-list lines of every sample without a complete answer |

**One answer per sample, by one rule.** Several directories are one run in parts (shards, a
resumed tail, whole-sample retries), and the first complete answer in argument order wins. An
answer is complete when the judge summary says `failed: 0` and `skipped: 0` and, on a sample
staged for `scan`, says the judge ran; a sample routed to `check` is complete without a judge,
which never runs there. A retry replaces the whole row, so votes from two attempts are never
mixed. A sample with no complete answer keeps its first attempt, marked `incomplete`, and is left
out of every table row and counted in the table's header.

**Resuming a run that died.** The driver writes each `raw/` file as its sample finishes, but its
ledger only at the end. Fold what is there: when the ledger cannot account for every sample, the
fold writes only `incomplete.jsonl` and exits 1. Run the driver on that list into a new directory
(`-samples incomplete.jsonl -out <dir2> -raw <dir2>/raw -no-fixtures`; on a subset its tripwire
may refuse to publish, which leaves `raw/` in place) and fold again with both directories. A
retried sample is a second draw from the model, not the answer the first run would have given.

**Reading the table.** Its kinds are aguard's, read from `raw/`, and they are not the corpus's
surface names: corpus surface `mcp` (tool catalogues) is aguard kind `connector`, and corpus
surface `connector` (`.mcp.json`) is aguard kind `mcp`. `raw/` records how many questions the
judge asked per sample, not per artifact, so a (kind, rule) row's denominator is every complete
sample with an artifact of that kind and at least one question asked, shared by all of that
kind's rules; a row exists only for a pair the run holds a vote for. A cell prints the rate and
its Wilson 95% interval only when the half-width is at most 15 points, the corpus's own rule, and
the bare count otherwise. A benign label is the corpus's assumption, so a flag on one is not a
confirmed error.

Three things about skill-scanner, measured 2026-09-24 (P-022):

- **Exit 1 means two things.** The gate firing AND every runtime failure — missing directory,
  taxonomy load error, `SkillLoadError` — all return 1 (`cli.py` returns only 0 and 1). The
  adapter never trusts the code; it reads stdout (SARIF or not) and stderr (`Error loading
  skill` vs any other `Error`). Do not "fix" this by treating 1 as success.
- **Strict mode requires `SKILL.md`, and that is kept on purpose.** Every mcp / hooks / permission
  / instruction / connector sample is refused with `Error loading skill: SKILL.md not found` and
  recorded as `no-verdict / unsupported-input`. `--lenient` would make them scannable — and would
  measure a mode users do not get by default. If you run it, it goes in a separate directory.
- **127 of the 300 malicious samples are its own eval set** (`cisco-mcp-scanner-evals`). Run
  twice: once on the full work list, once on `corpus samples --exclude-source
  cisco-mcp-scanner-evals`. Its column in any comparison cites only the second; its MCP row is
  "not measurable" (clean n=3), not blank.

Two things about cc-audit that cost an afternoon each if you meet them by surprise:

- **It exits 2 with "Configuration file not found" if no config is in play.** The config is also
  policy — it sets per-rule error/warn/ignore, so it decides what counts as a finding — which is
  why it goes through `-policy` and gets hashed into `run.yaml` rather than being treated as
  setup. It is passed explicitly rather than left to discovery, because discovery searches upward
  from the scanned path: a stray `.cc-audit.yaml` anywhere above the corpus checkout would
  silently change 3,539 answers.
- **Pin the version and record the hash.** It has shipped 146 releases and went v1.0.0 → v3.0.0
  inside 24 hours. Take the GitHub release tarball and verify its `.sha256`; `brew` and
  `cargo install` give you no hash to put in `run.yaml`, and "recomputable offline" is exactly
  the property these runs exist to test.

## Reading three columns

`baselines/results/` now carries aguard, cc-audit and skill-scanner. The columns are NOT one
ladder with three rungs; each was measured at its own shipped default, and two of them refuse
whole surfaces:

| | aguard | cc-audit | skill-scanner |
|---|---|---|---|
| reads | every corpus surface | every corpus surface | **skills only** — strict mode refuses any tree without `SKILL.md` |
| malicious flagged | 87/300 | 185/300 | 27/300 (27 of the 143 read-basis skills it scored) |
| benign flagged | 147/3220 | 1191/3220 | 76/3220 (of 2480 skills scored) |
| mcp row | measured | measured | **not measurable**: 0 of 487 mcp trees carry `SKILL.md`; declared in `tools.yaml` |
| attribution | 59% [48,68], map by lift | 39% [32,46], map by lift | 41% [25,59], **map by name** — 23 samples were too few to measure |

Three cautions that travel with the skill-scanner column:

- **Its zeros on mcp / hooks / connector / permission are refusals, not misses.** The ledger says
  `no-verdict / unsupported-input` for each, with the loader's own line. Counting them as misses
  would give it 9% recall; counting them as passes would give it a perfect false-positive rate
  on 900 samples it never read. Neither is the number.
- **`cisco-mcp-scanner-evals` is its own eval set and it never opened it.** All 127 are
  `.mcp.json` and were refused. "Grading its own exam" turned out not to arise in strict mode; the
  second results directory (`-excl-cisco`) exists to show that removing the source changed no
  verdict.
- **Its `dimension_map` is the weakest of the three.** Five categories mapped by name with a
  per-line note, twelve left null as "unmeasured"; the lift method that fixed two wrong guesses
  for cc-audit had 23 samples here and could not decide. Recompute when the skills surface grows.

## Layout

```
tools.yaml                        the facts about each scanner that change how to read it
adapter/                          the boundary; Scan must return a row for every sample
ledger/                           the partition invariant: nothing may vanish
tripwire/                         a whole surface coming back clean needs a declaration
corpus/                           the wire format, and nothing tool-specific
results/<tool>/<YYYY-MM-DD>/
  verdicts.jsonl                  what `corpus score` reads
  ledger.jsonl                    one row per test point, with the reason when there is no verdict
                                  and, on a judge run, what the judge cost on that sample
  scorecard.txt                   `corpus score` output, verbatim, never paraphrased
  run.yaml                        version, threshold, corpus commit, and the attribution
  judge.jsonl                     a judge run only: what the judge found, per sample (cmd/judgefold)
  per-kind-rule.txt               a judge run only: the generated per-(kind, rule) table
  raw/                            NOT committed
```

**What is committed and what is not.** Committed: verdicts, ledger, scorecard, `run.yaml`
— together about 850 KB per run. Not committed: `raw/` (measured at 14 MB per run per threshold,
regenerable from the corpus commit in `run.yaml`, and it carries third-party report text about
malicious samples) and `samples.jsonl` (622 KB, regenerable). And not every run: only the ones
something cites — a baseline, or a before/after pair. A directory of every experiment is a
directory with no baseline in it.

**Which static runs are kept (P-039).** Two: `results/aguard/2026-09-24/` is the "before" of the
v0.12–v0.15 rule work and the static baseline the judge subsets were drawn against;
`results/aguard/2026-09-29e/` is the current baseline. The six intermediate runs each proposal
took as its "after" were removed: every one of them is a before/after pair cited only by a
completed proposal, and one was byte-identical to `29e`. The README's rule stands — a baseline,
or a pair something cites — and a directory of every experiment is what it warns against.

## Paths in results are placeholders

Nothing under `results/` names a machine. Before anything is written, the driver replaces four
prefixes in every free-text field (`detail` in the ledger and in `fixtures.jsonl`), and the
aguard adapter does the same in `raw/`:

| placeholder | what it stood for |
|---|---|
| `<corpus>` | the corpus checkout the run read from |
| `<work>` | the staging directory the driver created under the temp directory (one per run) |
| `<home>` | the operator's home directory |
| `<tmp>` | the temp directory |

The substitution is by prefix, symlink-resolved forms included, so `<work>/<sample>/home/.claude`
still says exactly where in the staged tree a thing was; only whose machine staged it is gone.
Paths quoted from inside a third-party sample (a hook that hard-codes its author's home) are
sample content, not ours, and stay as they are. This was retrofitted to every committed run in
P-037 after one ledger was found carrying 1,669 copies of a maintainer's home directory.

## The two invariants this directory enforces

**Every test point ends somewhere.** The set is exactly the corpus: 3,539 samples from
`corpus samples` plus the 6 injected-fault fixtures from `corpus fixtures`, and nothing from
anywhere else. Each sample lands in exactly one of `scored`, `no-verdict` or `error`. There is
deliberately **no `skipped`** — no row may say we did not run it — and a `no-verdict` row must
carry `attempted: true`, so a reason cannot be written for a scanner that was never invoked.

What this replaces: `hack/corpus-runner` emits no line at all for a sample it cannot place, so
`corpus score` finds the hole by subtraction and calls it "uncovered". That says *that* something
is missing, never *why*, and a hole found by subtraction does not announce when it grows.

**A silent surface stops the run.** If a load-path surface had enough malicious samples for a
zero to be meaningful and nothing was flagged, the run halts and asks for a one-time declaration.
The corpus's guide names this as the failure that produces a wrong number about someone else's
scanner: a whole column of zeros is almost always placement, not a real miss. With a declaration
the zero is a disclosed coverage boundary and the run continues — penalising a disclosure would
invert what this project rewards. Without one, nothing is published.

"Enough samples" is the corpus's own line, not ours: a proportion stops being a figure once its
Wilson 95% half-width passes 15 points, which for an all-zero result is `n < 22`.

## Two things a number here cannot mean

**`no-verdict` is not `benign`.** A scanner that read nothing is not a scanner that found nothing
wrong. Counting it as a correct benign call would score a disclosed product boundary as a right
answer.

**`no-load-path` is a fact about one tool, never about the corpus.** aguard has no load path for
the 127 Python MCP server implementations; NVIDIA SkillSpector accepts single files and may well
score them. One tool's input model must not decide how much of the corpus another tool is
measured on — which is why the decision is made after the scanner has been invoked, not before.

## Contamination is disclosed, not excluded

Samples derived from a measured tool's own test suite are **still measured**; what they change is
the published figure, which then owes two denominators. Both known cases:

- **cisco-ai-defense** — 127 of the 130 malicious MCP samples derive from its scanner's own
  fixtures. There is consequently no clean MCP denominator for anyone: keep them and Cisco is
  graded on its own tests, drop them and three samples remain.
- **ourselves** — three samples cite agent-guard work items (W-008, W-009, W-027), and two of
  them are hard negatives. The hard-negative census is 19, so 10.5% of the denominator behind
  this project's own `hard negative <= 1/19` gate is our own fixture. aguard's hard-negative
  figure is therefore published over **both 19 and 17**. A disclosure rule we would not apply to
  ourselves is not a rule.
