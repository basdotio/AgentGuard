<!-- SPDX-License-Identifier: MIT -->
# 056 — A skill whose SKILL.md is a symlink hashes as empty input and scores a clean 100: the instructions it loads are never read, and one approval trusts every such skill

- **Source**: new finding (2026-10-13), reproduced on the maintainer's real `~/.claude` with v0.21.0 and v0.24.0
- **Depends on**: none
- **Branch**: `p/056-symlinked-skill-file`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

A common way to install a skill pack is one directory per skill under `~/.claude/skills/`, each holding only a `SKILL.md` that
is a symlink into the pack's own directory (`skills/<s>/SKILL.md` → `skills/<pack>/<s>/SKILL.md`). Claude Code loads each of those
skills and reads the linked file as its instructions. aguard does not:

- `TreeHash` skips every file whose target resolves outside the skill's own directory, without a trace. A skill whose only file is
  such a link hashes **no entries**, so its hash is `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` — the
  SHA-256 of empty input. `aguard hash` of the pack's own copy of the same skill prints a real hash.
- `detect.readTextTree` skips the same file the same silent way: no rule runs on the instructions that load, the skill scores
  100 and carries no finding and **no note** (invariant #5).
- The judge does the opposite: `planFor` reads `SKILL.md` through `parse.ReadSkill`, which follows the link wherever it points,
  so `aguard llm preview` of the same skill shows its text in the intent and injection payloads. Two paths, two readings of one
  file.

Consequences, on the real `~/.claude` (41 skill directories with this layout, 363 artifacts in `scan --json`):

- every one of the 41 skills has the same content hash, the empty-input digest;
- the load-time gate keys approvals by that hash, so the first clean load of any one of them records "trusted from now on for
  content e3b0c44298fc…", and from then on all 41 load with no output — whatever their linked files say, and whatever is later
  written into them; the gate never re-asks;
- a reputation entry or an `aguard approve` for one of them covers all of them;
- the static rules never read what these skills load, while the judge does.

A fixture shows the worst shape: a skill whose `SKILL.md` links to a file outside `$HOME` is collected, scores 100 with no note,
and the judge's payload carries that file's text — the judge reads content invariant #2 says nothing may read.

## Initial direction

Decide which files are a skill's content when its `SKILL.md` (or another file in it) is a symlink out of the skill directory,
driven by what Claude Code's loader reads, and give the hash, the static rules and the judge one reading of that file set: follow
`SKILL.md` into a declared boundary, refuse anything else loudly (a scored finding, never a clean score, never the empty-input
digest). This touches `collect` (the hash walk), `detect` (the tree read), `judge` and `hygiene` (how they open `SKILL.md`),
invariant #2's wording and the hash rules; existing approvals and reputation entries re-key only for the affected skills.
