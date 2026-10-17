<!-- SPDX-License-Identifier: MIT -->
# 070 — What the SKILL.md names goes first: named scripts lead the behaviour excerpt, named documents are followed one level further

- **Source**: the maintainer, 2026-10-16 ("好" to: measure first whether the judge's misses are judgement or visibility, then decide); the measurement said visibility for 29 of 62
- **Depends on**: P-065 (the companion pass), P-068 (the result pass), P-064 (the 240 SkillHarm trees as the development set)
- **Branch**: `p/070-named-files-first` (the code is reverted in the same branch; the measurement and this record stay)
- **Rejected**: 2026-10-17 — measured on four lists in two variants: no recall gain beyond the run band, development benign up; the maintainer agreed ("好,按你说的来") to record it and move to a stronger model. See Measured.

## Problem

After P-069 the judge misses 62 of the 240 SkillHarm trees. Before asking the model anything differently, the question
was whether it had even seen the payload. Using the upstream's own `metadata.json` (`added_artifacts` names the file
each attack adds) against `aguard llm preview --json` (the exact text each pass sends): 33 of the 62 had the payload
in front of the model in full, 16 in part, **13 not at all**. The 13 are two shapes, neither a judgement:

- The document the body names names another — `ooxml.md` says "read `workspace.md` first", and the payload is in
  `workspace.md`. The companion pass followed one level. 4 trees.
- The script the body says to run is behind the budget — `python scripts/preflight_env.py` in a tree of nine to
  fourteen scripts and 31–197 KB, where the behaviour excerpt's 6,000 bytes were spent in walk order on files the
  body never mentions. 9 trees; most of the 16 "in part" are the same files cut to head and tail.

## Initial direction

Two small changes to what is sent, nothing to how it is asked. `behaviorExcerpt` takes the files the instruction text
names (`namedFiles`: a path-ish token with a script or config extension, by path from the skill root or by bare name)
and sends them first, in the order named, before the rest in walk order (`namedFirst`); the budgets do not change, so
a tree that names nothing sends exactly what it sent. `companionDocs` reads the documents the named documents name —
two levels, not one; the documents named by those are not followed — under the same cap of eight. `ExcerptVersion`
9 → 10; the prompt is unchanged, so `PromptVersion` is not.

Not this: a larger budget (every tree pays, this costs nothing), or following references from scripts (imports are
the static chain's business, P-061).

## Done criteria

- [x] `TestBehaviorExcerpt_NamedScriptsComeFirst`, `TestNamedFiles_ByExtensionInOrder`, `TestCompanionDocs_FollowsOneMoreLevel`;
  the excerpt fixture names a script and a second-level document
- [x] Live at `samples: 3`, `main` (P-069) against this branch: the 240 SkillHarm trees (the 13 unseen and 16 partly seen
  payloads are the cells to watch), the 384 benign trees that name a companion, the held-out ClawHub benign 216, the fresh
  held-out benign 216 — in two variants
- [x] The decisions document (the judge docs and rules are unchanged: the code is reverted)

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-16/17, `main` at P-069 (`33a3f0b`, `judge-excerpt=9`) against this branch
(`judge-excerpt=10`), layer-1 paths at corpus `6c7eef52`; nine result directories under
`results/aguard/2026-10-17-llm-gpt-4.1-mini-p070-*`, raw in `aguard-judge-raw-2026-10-p070.tar.gz` on v0.26.0. Main's
SkillHarm, benign-384 and ClawHub figures are P-069's runs of the day before (same binary); the fresh held-out has a
same-day main run.

**First variant** (one shared 6,000-byte budget, the named files first): the named scripts got in, and the unnamed
files that carry SkillHarm's `executable_code` and `import_new_helper` payloads were pushed out — 10 → 6 and 10 → 7
caught. **Second variant** (the named files get 4,000 bytes of their own; one past it falls back to its place in the
walk): checked without a judge call first, the 15 trees the first variant lost see their payloads exactly as on main,
and of the 62 misses the unseen go 13 → 7.

| | n | main | first variant | second variant |
|---|---:|---:|---:|---:|
| SkillHarm, judge | 240 | 178 | 174 | 173 |
| … of the 62 main missed, caught | 62 | — | 11 | 8 |
| … flips against main (gained / lost) | 240 | — | 11 / 15 | 8 / 13 |
| benign trees that name a companion | 384 | 62 | 65 | 68 (`LLM-001` 7 → 12) |
| held-out ClawHub benign (totals) | 216 | 9 | 8 | 8 |
| fresh held-out benign (totals; main same day) | 216 | 11 | 10 | 10 |

The run band, measured: two SkillHarm runs with the same question and the same excerpt (P-068's and P-069's) flip 9
gained / 6 lost. Both variants sit inside it on recall. The development benign list moves up in both, all of it the
intent pass reading the scripts real skills name and flagging a few more; the two held-out benign lists do not move.
Prompt tokens on the benign list +2.5%.

**Reading.** The premise was that the misses were visibility: 13 payloads never in front of the model, 16 only partly.
The second variant puts six of the thirteen in view without taking anything from anyone, and the model catches about
as many of the newly visible as the run band would hand it anyway. For gpt-4.1-mini at three votes the binding
constraint on SkillHarm is judgement, not what it is shown — the same conclusion P-068's misses suggested from the other
side (a forced value with an engineering rationale, in plain view, three votes benign). The next measurement is a
stronger model on the same lists, not a larger excerpt. The visibility tool (`payload2.py`: the upstream's
`added_artifacts` against `aguard llm preview --json`) stays in the scratch notes; the cell it names, a payload in a
helper module nothing names, is real and is the static import chain's (P-061), not the judge's.

## Done

```
Rejected: 2026-10-17 — no recall gain beyond the run band in either variant; development benign 62 → 68; code reverted, runs kept
Evidence: results/aguard/2026-10-17-llm-gpt-4.1-mini-p070-skillharm-s3: 173 (main 178); …-first-variant-skillharm-s3: 174
Evidence: results/aguard/2026-10-17-llm-gpt-4.1-mini-p070-benign-docs-s3: 68 (main 62); …-heldout-asm-benign-s3: 8 (main 9); …-heldout-fresh-benign-{,main-}s3: 10 / 11
Evidence: raw aguard-judge-raw-2026-10-p070.tar.gz on v0.26.0 (sha256 6dc0d69d…4f08, 792,203 bytes); schema round-trip 70 files / 32,852 lines
```
