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

## Measured (2026-10-13, base `18114a1` = v0.24.0, Claude Code 2.1.107)

A scratch build (never committed) followed `SKILL.md` into a wider boundary and refused everything else with a scored
`SCOPE-001`, behind an environment switch, so every "after" number below comes from the same binary as its base number.
`llm preview --json` counted the judge calls; nothing was sent.

### What Claude Code 2.1.107 loads

Read from the skill and markdown loaders in the installed 2.1.107 binary:

| Surface | What is read | Containment | Dedupe |
|---|---|---|---|
| user, project, policy and `--add-dir` skills | each entry of `<dir>/skills` that is a directory **or a symlink**; `<entry>/SKILL.md` read with `readFile`, which follows a symlink wherever it points | **none** | by `realpath` of `SKILL.md` across all sources ("same file already loaded") |
| plugin skills | `<dir>/SKILL.md` read with `readFile` | none | by resolved path, per plugin |
| commands, agents, output styles, workflows | a recursive walk that follows symlinked files **and** symlinked directories (a directory already visited is skipped), every `.md` read | none | by the link's own inode |

- The base directory handed to the model ("Base directory for this skill", `${CLAUDE_SKILL_DIR}`) is `<entry>`, the unresolved
  per-skill directory, not the link's target.
- An unreadable `SKILL.md` is skipped: `ENOENT` silently, other errors (a loop) with a warning in the debug log. A dangling or
  looping `SKILL.md` therefore loads nothing.
- No loader reads a skill's supporting files; the agent reaches them at run time through the base directory, so a supporting-file
  symlink is followed whenever the skill tells the agent to run or read it.

So what loads is the linked file's bytes, wherever they are. The boundary question is only about what **aguard** is willing to read.

### Three readings of one file today

| Path | How it opens a skill's files | A `SKILL.md` symlinked out of the skill directory |
|---|---|---|
| hash (`collect.TreeHash`) | `withinDir(skillRoot, p)`; a dangling link is placed by its parent and counts as **inside** | skipped, no entry, no mark |
| static (`detect.readTextTree`) | `inBoundary(skillRoot, p)`; a dangling link counts as **outside** | skipped, no note |
| judge (`planFor`) | `SKILL.md` through `parse.ReadSkill` — **no boundary at all**; every other file `lstat`-skipped if it is a symlink, even one inside the skill | read and sent, from anywhere, including outside `$HOME` |
| hygiene | `parse.ReadSkill`, no boundary (local only) | read |

Two more disagreements: `docs/rules.md` already describes `SCOPE-001` as raised when "a skill's internal file … resolved outside
the allowed root and was NOT read" — no code raises it for a skill's internal file; and spec §16 item 2 names `--root` (and the
project root) as the boundary, while `.claude/rules/invariants.md` #2 and the code use the skill's own root.

### The real `~/.claude` (counts only; `scan --json --inbox off`)

Layout of `skills/` (46 entries): 41 directories whose only entry is a `SKILL.md` symlink into one third-party pack's directory
inside `skills/` (40 distinct targets; two links share one); the pack itself (a skill with its own `SKILL.md`, whose tree contains
the 40 targets); one more plain skill; two directory symlinks into `$HOME` (allowed by invariant #2, unchanged here); one empty
directory. No supporting-file symlink leaves its skill (2 links stay inside their skill; 11 sit under `node_modules`, which no walk
enters). No command, agent or rule is a symlink; `CLAUDE.md` is not a symlink. The approvals store does not exist.

| | base | after (scratch build) |
|---|---|---|
| artifacts | 363 | 363 |
| artifacts hashed as the empty-input digest | 42 (41 skills + the empty directory) | 1 (the empty directory) |
| hashes that change | — | 41, all skills, all of them the 41 above; 40 distinct values. No plugin, directory, desktop or other skill hash moves |
| scoring findings on the 41 | 0 | 9 on 4 skills (1 `EXFIL-001` high, 8 `EXEC-003` low) — the same findings the pack's tree already carries for those bytes |
| skills below 100 | 21 | 25 |
| `overall` / `overall_effective` | 69 / 69 | 69 / 69 |
| mean artifact score | 97.32 | 97.21 |
| `check <the skill with the high>` | exit 0 | exit 1 |
| notes | unchanged (8 `COV-000` low, 1 `PARSE-000`, 1 `REP-GOOD`) | unchanged |
| judge calls (`llm preview`) | 589: intent 171, injection 321, triage 31, other 66 | 593: the same plus 4 triage calls for the 4 skills that gained findings |
| judge payload | 1,578 KiB | 1,579 KiB |
| hygiene | 17 items; 2 `duplicate_fn` items carry the empty-input digest in 3 locators (the identity `clean` checks before a move) | 17 items; those locators carry the content hash |

The judge already read all 41 `SKILL.md` files; only the hash and the static rules change.

### Fixtures (`t.TempDir()`-style scratch tree, one row per layout)

Each "bad" file holds a `curl … | bash` line and an instruction to send `~/.ssh/id_rsa` out. "After" = the scratch build with the
boundary at the config root; every row reads the same with the skills-directory rule recommended in open question 1, because no
target sits in the config root outside `skills/`.

| Row | Layout | base | after |
|---|---|---|---|
| a | `skills/a/SKILL.md` → `skills/pack/a/SKILL.md` (pack has its own `SKILL.md`) | skill 100, empty-input digest, no note; judge 2 calls carry the text | 0/100, 4 high; hash = `aguard hash skills/pack/a` (`a552389f…`) |
| b | → `skills/bundle/b/SKILL.md` (bundle has no `SKILL.md`, collected as a directory) | 100, empty-input digest | 0/100, 4 high |
| c | → `~/packs/c/SKILL.md` (in `$HOME`, outside the config root) | 100, empty-input digest | `SCOPE-001` medium, 88, no hash, judge 0 calls (read and 0/100 with a `$HOME` boundary) |
| d | → a file outside `$HOME` | 100, empty-input digest, no note; **the judge's payload carries the file's text** | `SCOPE-001`, 88, no hash, judge 0 calls |
| e, f | dangling `SKILL.md`, a `SKILL.md` that links to itself | collected as `directory`, 100, both the same hash (`2301f581…`, the present-unread mark), no note | a low `COV-000` "not read" |
| g | the whole skill directory a symlink into `$HOME` | 0/100, read | unchanged |
| h | real `SKILL.md`, `run.sh` → `~/packs/hlib/run.sh` | 100, hash ignores `run.sh`, no note; the judge skips it too | `SCOPE-001` medium, 88, hash changes |
| i | benign `SKILL.md` → `~/packs/i/SKILL.md` | 100, empty-input digest — **the same hash as a** | `SCOPE-001`, 88, no hash |
| z | the bytes of a as a plain file | 0/100, 4 high | unchanged |
| — | `commands/`, `agents/`, `rules/` file → `~/packs/md/*.md` | not collected; one scan-level `COV-000` medium each ("resolve outside the scanned root, not read") | unchanged |
| — | `CLAUDE.md` → a file outside `$HOME` | not collected, and the only note is "Nothing to audit under this root" | unchanged (open question 10) |
| — | `check <plugin>` with `skills/x/SKILL.md` → `../../shared/x.md` (P-044 child) | plugin row 0/100 with the findings; child `p:x (plugin p)` 100, empty-input digest | follows open question 7 |

The gate on the same fixture (`PreToolUse[Skill]`, `fail_on: high`):

- base: loading `i` prints "trusted from now on for content e3b0c44298fc…" and stores it; then `a` loads with **no output**;
  appending to `skills/pack/a/SKILL.md` and loading `a` again: still no output. `aguard approve skills/i` stores the same digest.
- after: `i`, `c`, `d`, `h` are allowed with "not recorded as trusted … SCOPE-001 (medium)" (P-005: re-audited on every load);
  `a` asks (0/100); `aguard approve skills/i` refuses with "has no content hash", exit 2 (P-011).

### The corpus (`../agent-artifact-corpus`, read only)

One symlink among all samples outside `cache/`: `mal-skill-sg-symlink-escape`, a supporting file that is a deliberately dangling
link out of its skill directory. base: `check` 100, no note (the hash places the dangling link inside and folds the present-unread
mark; the static walk calls it outside and skips it silently). after: `SCOPE-001` medium, 88. No benign sample has a symlink, so
this change cannot add a benign false positive on the corpus.

### Tests on the scratch build

The whole suite stays green except `TestCrossRootSymlinkIgnored` (`internal/collect`), which asserts that deleting an
out-of-root link leaves the hash unchanged — it pins the silent skip. `TestHashGolden` stays green. No existing test has a
symlinked `SKILL.md`, and none asserts what the judge reads of one.
