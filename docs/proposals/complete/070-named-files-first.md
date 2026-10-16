<!-- SPDX-License-Identifier: MIT -->
# 070 — What the SKILL.md names goes first: named scripts lead the behaviour excerpt, named documents are followed one level further

- **Source**: the maintainer, 2026-10-16 ("好" to: measure first whether the judge's misses are judgement or visibility, then decide); the measurement said visibility for 29 of 62
- **Depends on**: P-065 (the companion pass), P-068 (the result pass), P-064 (the 240 SkillHarm trees as the development set)
- **Branch**: `p/070-named-files-first`

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
- [ ] Live at `samples: 3`, `main` (P-069) against this branch: the 240 SkillHarm trees (the 13 unseen and 16 partly seen
  payloads are the cells to watch), the 384 benign trees that name a companion, the held-out ClawHub benign 216, the fresh
  held-out benign 216
- [ ] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

(pending)

## Done

```
(pending)
```
