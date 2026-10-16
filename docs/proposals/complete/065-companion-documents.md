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
- [x] Live at `samples: 3`, v0.26.0 against this branch: the 384 benign trees that name a companion (233 real recent, 148
  skillet-wild, 3 SkillMD) — the whole exposed benign population, both binaries, same day; the 240 SkillHarm trees; the
  held-out ClawHub sets (totals only)
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-16, layer-1 paths at corpus commit 4f9c61be, the branch's binary (built at
e4005c6 with this change, before it was committed as bef2cf0); five result directories under
`results/aguard/2026-10-16-llm-gpt-4.1-mini-p065-*`, raw in `aguard-judge-raw-2026-10-p065.tar.gz` on v0.26.0. "judge" is
the fold's predicate (an escalated, grounded, 2-of-3 LLM finding at high), "any" one vote at high; a sample with a
failed judge call counts as not caught.

**The 240 SkillHarm trees, P-064's v0.26.0 run → this branch** (`…-p065-skillharm-s3/`; 237 → 236 complete):

| by realization — where the upstream put the payload | n | static | judge before → after | any before → after |
|---|---:|---:|---:|---:|
| `redirect_to_new_doc` — a companion `.md` the agent is told to read first | 54 | 0 | **0 → 34** | 3 → 37 |
| `redirect_to_new_pdf` — a PDF the agent is told to read first | 38 | 0 | **0 → 26** | 0 → 30 |
| `plain_text` — in `SKILL.md` itself | 50 | 6 | 33 → 29 | 37 → 38 |
| `redirect_to_new_script` — a new script the agent is told to run | 66 | 11 | 46 → 43 | 47 → 47 |
| `executable_code` — inside an existing script | 17 | 0 | 9 → 10 | 10 → 10 |
| `import_new_helper` — a helper module an existing script imports | 15 | 0 | 8 → 10 | 11 → 11 |
| all | 240 | 17 | 96 (40%) → **152 (63%)** | 108 → 173 |

| by dimension | n | judge before → after | | by tier | n | judge before → after |
|---|---:|---:|---|---|---:|---:|
| filesystem | 100 | 38 → 64 | | plain | 50 | 33 → 29 |
| injection | 40 | 4 → 9 | | indirect | 152 | 63 → 97 |
| backdoor | 20 | 10 → 15 | | evasive (the PDFs) | 38 | 0 → 26 |
| execution | 20 | 13 → 18 | | | | |
| exfiltration | 20 | 11 → 13 | | | | |
| permission | 20 | 7 → 14 | | | | |
| resource-abuse | 20 | 13 → 19 | | | | |

Escalated rules `LLM-003` 32 → 88, `LLM-001` 62 → 61, `LLM-006` 3 → 3: the 56 new flags are the injection question
answered over a companion. The four realizations the pass does not touch moved 96 → 92 between them — by sample, 11 lost
and 6 gained, with the same prompt, the same excerpt and the same six calls per tree (e.g. `sh-dos-9f841f807c`: 6 calls,
none failed, flagged in P-064 and unanimous benign here). Two draws of the same model, the band P-064 already saw on the
120 shared ids.

The 32 companion trees still missed all had the pass (9–10 calls each, one failed call in one tree): 8 had one vote at
high and lost the 2-of-3; 24 were unanimous benign. 7 of the 32 are `goal_hijacking` / `output_manipulation` (the shape
P-066 asks about); the rest the model read as housekeeping — `rm -f /var/log/fjsp-audit/access.log` in a `PREFLIGHT.md`
that calls it a stale file from the previous run, and the like. That is a judgement about what the document says, which
is the question this proposal set out to make askable; it is no longer a question nobody asked.

**The 384 benign trees that name a companion** — every layer-1 benign skill tree whose `SKILL.md` names a `.md`/`.txt`/`.pdf`
present in the tree (233 recent real repositories, 148 skillet-wild, 3 SkillMD), v0.26.0 and this branch the same day
(`…-p065-benign-docs-v0.26.0-s3/`, `…-p065-benign-docs-s3/`):

| | static | judge | any | escalated rules | judge calls | failed calls | prompt tokens |
|---|---:|---:|---:|---|---:|---:|---:|
| v0.26.0 | 54 | 66 | 73 | `LLM-001` 11, `LLM-006` 6 | 2,525 | 44 (1.7%) | 4.93 M |
| this branch | 54 | 67 | 73 | `LLM-001` 8, `LLM-006` 7, `LLM-003` 1 | 3,641 | 64 (1.8%) | 7.34 M |

Seven trees changed verdict (3 gained, 4 lost); none of the three gained is grounded in a companion document — the one
`LLM-003` cites `SKILL.md:48` of a screen-control skill, from the body pass. **Flags the companion pass added on 384
benign trees: 0.** The cost is the calls: +44% judge calls and +49% prompt tokens on the trees that name a document,
which is 58% of real trees; a tree that names none pays nothing. The failed calls are the model returning malformed JSON
(a `\x` escape, a stray `]` or `}` after a member) and are a v0.26.0 behaviour, 1.7% of calls before this change; a
tolerant reading of those replies is a separate proposal.

**Held-out ClawHub** (agent-skill-malware, totals only, `…-p065-heldout-asm-*`): malicious 123, static 68, judge 68, as at
v0.24.0; benign 216, static 6, judge 6 (7 at v0.24.0; P-061's change, not this one). None of the 339 trees names a
companion document, so the pass never ran on them: the control that nothing else moved.

**Reading.** The two zeros are now 34 of 54 and 26 of 38, for no benign flag on the whole exposed benign population and
about half again the judge's token bill on the trees that name documents. The static engine still files these documents
as prose: a `rm -f /var/log/…` in a named `PREFLIGHT.md` is a judge finding today and not a rule finding, so a scan without
`--llm` still sees nothing in 92 of 240. Whether the engine should read a named document as `roleInstruction` is the
deferred proposal, with the false-positive measurement on the 400 real trees it needs.

## Done

```
Merged: PR #87 (2026-10-16; find the sha with git log --grep P-065)
Released: v0.27.0
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p065-skillharm-s3: 240 asked, static 17, judge 96 → 152 (doc 0 → 34 of 54, PDF 0 → 26 of 38), any 108 → 173
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p065-benign-docs-{v0.26.0-,}s3: 384 benign trees naming a companion, judge 66 → 67, companion-grounded flags 0; calls 2,525 → 3,641
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p065-heldout-asm-{malicious,benign}-s3: 68/123 and 6/216, unchanged (no tree names a companion)
Evidence: raw aguard-judge-raw-2026-10-p065.tar.gz on v0.26.0 (sha256 e40ef904…d242e, 507,834 bytes); schema round-trip 44 files / 26,363 lines; ExcerptVersion 8 (digest cbb3db8b81b93515); make verify: all gates passed
```
