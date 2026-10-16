<!-- SPDX-License-Identifier: MIT -->
# 065 — A document `SKILL.md` tells the agent to read is an instruction file: the judge reads the companions a skill names, PDFs included

- **Source**: the maintainer, 2026-10-16 ("那我们的判官模式,还有可以优化的吗" → "做1、2"), after P-064 found 92 of 240 SkillHarm trees unreachable by construction
- **Depends on**: P-064 (the measurement), agent-artifact-corpus PR #3 (the 240 trees), P-037 (`fitUnits` and `boundedNote`, which the new pass reuses)
- **Branch**: `p/065-companion-documents`

## Problem

"Read `PREFLIGHT.md` in full before starting." An agent follows that line, and the file it names is as much an
instruction file as the one that named it. Nothing in the tool read it: the static engine files any `.md` that is not
`SKILL.md` / `CLAUDE.md` as a prose document and runs injection rules only on it; the judge's behaviour excerpt lists
scripts and config extensions and no `.md`; nobody reads a PDF. P-064 measured the consequence on the 240 SkillHarm trees:
54 put the attack in a companion `.md`, 38 in a PDF, and the judge caught 0 of the 92 — 38% of the set, and the single
largest hole on the skill surface. It also reread P-059: that run's 43 of 117 was 43 of the 73 the tool could see.

The companion shape is not exotic. Of 400 real skill trees from recent repositories, 233 name at least one document in
their `SKILL.md` (median three, the longest 69), so whatever reads companions must read what is named, not what exists,
or it reads `references/` wholesale and pays for it in false positives and tokens.

## Initial direction

A companion pass, judge-side only: the `.md`, `.txt` and `.pdf` files the `SKILL.md` body names — by path from the skill
root or by bare name anywhere in the tree, through the rules' own reach predicate, `SKILL.md` and licence files excluded,
in the order named, the first eight — go to the injection question beside the declared purpose, as one more pass per
skill, with the behaviour pass's budgets (2000 bytes per file, 6000 in all, head and tail) and its unit grounding, so a
verdict cites `PREFLIGHT.md:7`. What does not fit is disclosed through `boundedNote` like the other fitted fields. One
level only: a document naming a document is not followed.

A PDF contributes its text layer through a reader of about a hundred lines in the standard library: each stream decoded
through the filters its dictionary names (ASCII85 and Flate, in order; any other filter means "cannot read"), the text
operators' strings, a line per positioning operator, and a noise gate that returns nothing when the result is mostly
unprintable (a font with a private encoding). Nothing is executed, no object graph is followed, output is bounded like
any file. All 38 SkillHarm PDFs are ReportLab output and decode. A PDF this cannot read is counted in the note, not ignored.

`ExcerptVersion` 7 → 8; the prompt is unchanged, so `PromptVersion` is not. The static side (a named document as
`roleInstruction`) is not in this proposal: it changes deterministic findings and the `--fail-on` answer, and wants its own
false-positive measurement on the 400 trees.

## Done criteria

- [x] `TestCompanionDocs_NamedDocumentsOnlyInOrder`, `TestCompanionDocs_CapAndEscapes`, `TestCompanionExcerpt_TextLayerAndGrounding`,
  `TestRun_CompanionDocumentGetsTheInjectionPass`, `TestPDFText_*` (three filter chains, six refusals, escapes and Latin-1)
- [ ] Live at `samples: 3`, v0.26.0 against this branch: the 384 benign trees that name a companion (233 real recent, 148
  skillet-wild, 3 SkillMD) — the whole exposed benign population, both binaries, same day; the 240 SkillHarm trees; the
  held-out ClawHub sets (totals only)
- [ ] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

(pending)

## Done

```
(pending)
```
