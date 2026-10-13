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

## Design (as recommended in the open questions)

One predicate in `collect` answers "may this file of this artifact be read?", and the hash walk, the static tree read, the judge
and hygiene all ask it; nothing else opens a skill's files.

1. **`SKILL.md` at the root of a skill** (the one file Claude Code's loader reads) is read when it resolves inside the skill root,
   **or** inside the skills directory that holds the skill (open question 1; for a plugin's child, the plugin root, question 7),
   and its resolved path is not a credential path by the predicates `@import` already uses (`hasSensitiveComponent`,
   `credentialFile`). It is hashed under the name `SKILL.md` with the target's bytes — the same hash a plain copy gets (question 3).
2. **A refused `SKILL.md`** (outside that boundary, a credential, or unresolvable while its lexical target is outside) is not
   opened by anything: `SCOPE-001` (medium, scored) on the artifact — `EXFIL-005`-style high when the target is a credential
   (question 4) — and the artifact's hash is `""`, never the empty-input digest; P-011 then makes `approve` refuse and the gate
   say "not remembered".
3. **Every other file** keeps invariant #2's skill-root boundary (question 2), but a refused one is no longer silent: one
   `SCOPE-001` (medium, scored) per artifact naming the entries (through `Redact`), and each entry folded into the tree hash as a
   marker over its link text, so retargeting a link re-keys. This applies to every tree the walk hashes (skill, plugin, directory,
   server, quarantined).
4. **A link that cannot be resolved** (dangling, a loop) is classified by its lexical target: inside → present and unread (the
   existing unreadable mark and `COV-000`), outside → item 3 (question 6). `withinDir` and `inBoundary` stop disagreeing.
5. **The judge reads exactly that file set**: `SKILL.md` through the predicate (no more reading outside the boundary), supporting
   files the static walk reads, including symlinks inside the skill root (question 5). `ExcerptVersion` is bumped.
6. `detect.rulesEpoch` 2 → 3 (deterministic findings change for an input); `make docs`.

## Done criteria

- [ ] `TestSkillFiles_OneReading` (`internal/collect`, `internal/detect`, `internal/judge`; one `t.TempDir()` fixture with rows
  a–i, z, the P-044 plugin child and a corpus-shaped dangling supporting link): per row, the hash class (equal to the plain copy's
  hash / `""` / changed by the marker / unchanged), the static finding set, `SCOPE-001` present or absent, and that the files the
  judge's planned units name are exactly the files the hash folded in and the static walk read. Red on the base for a–f, h, i and
  the plugin child
- [ ] `TestNoSkillHasTheEmptyDigest` (`internal/collect`, invariant): over every row and over `CollectAll` / `CollectTarget`, no
  `skill` artifact and no plugin child hashes to `e3b0c442…b855`. Red on the base (a, b, c, d, i, the child)
- [ ] `TestOutsideSkillMDNeverLeaves` (`cmd/aguard`, invariant #2 on the egress path): a `SKILL.md` symlinked to a file outside
  the boundary that holds a marker string; the marker appears in no JSON, SARIF, markdown, HTML or terminal report and in no
  `llm preview --json` payload. Red on the base (the judge payload carries it)
- [ ] `TestGate_LinkedSkillsShareNoApproval` (`cmd/aguard`, two `runHook` processes as `nextProcess` does): after the clean load
  of a linked benign skill, a linked malicious skill asks; after appending a byte to a remembered skill's link target, the gate
  audits it again. Red on the base (no output for both)
- [ ] `TestCrossRootSymlinkIgnored` rewritten to the property it was meant to pin: changing the out-of-root target's bytes leaves
  the hash unchanged and the content absent from every output; removing the link changes the hash; `SCOPE-001` names it
- [ ] Real `~/.claude`, counts only: 41 → 0 skills with the empty-input digest; exactly those 41 hashes move; `overall` 69 → 69;
  judge calls 589 → 593 with intent and injection unchanged; hygiene still 17 items
- [ ] **Reverse assertions**: rows g (directory symlink into `$HOME`) and z (plain file) byte-identical to the base in
  `scan --json` and `check --json`; a tree with no symlink keeps its hash (`TestHashGolden`, `TestContentHashGolden` unchanged);
  the three scan-level `COV-000` notes for symlinked commands, agents and rules unchanged; `check --fail-on high` still exits 1
  for z; `TestZeroDial_*`, `TestAsksNothingOf_FollowsPlanFor` green unchanged
- [ ] Docs: `.claude/rules/invariants.md` #2 and `hash.md`, `docs/architecture*.md` (invariant 2), spec §16 item 2 and §13 test ②,
  §8 (the tree hash), `docs/rules.md` via `make docs` (`SCOPE-001` now true as written), `docs/llm-judge*.md` (what the judge reads
  of a skill), `ROADMAP.md` known limitations if a question is decided against reading
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no toolchain switch; no new dependency

## Out of scope

- **Symlinked commands, agents, rules, workflows and output styles** that resolve outside the root: they are disclosed today (one
  scan-level `COV-000` medium per tree) but not scored, while Claude Code loads them. Aligning them with `HOOK-002`'s "score the
  refusal" is a follow-up (question 11); none exists on the real `~/.claude`
- **Claude Code's realpath dedupe**: two links to one target are two artifacts with one hash, as today; Claude Code loads one
- **The skill directory install guard** (a directory symlink must resolve inside `$HOME`, `SCOPE-001`): unchanged
- **`clean`**: quarantining or restoring a skill whose `SKILL.md` is a relative link (`issues/019`); its identity check simply
  gets a real hash instead of the empty-input digest
- **`credentialFile` coverage** (`.credentials.json` is not matched, nor for `@import`s): a follow-up, it moves `EXFIL-005`
- **Zip targets**: extraction never recreates a symlink (spec §4.1), so nothing changes there
- **The gate's code**: no change; it consumes the hash and the findings the scanner produces
- No rule text change other than `SCOPE-001`'s and `EXFIL-005`'s descriptions; no Go dependency; no `go` directive change

## Must not claim

- **Not "symlinked skills are covered"**: a `SKILL.md` is read inside the declared boundary; one outside it, and every supporting
  file outside its skill root, is refused — loudly, scored, unhashed or marked — and still not read
- **Not that the judge "now sees" these skills**: it already read every one of them (measured, 589 calls before and after for the
  questions); the change for the judge is that it stops reading outside the boundary and reads the same set as the rules
- **Not that the loader table is Claude Code's contract**: it was read from one installed build (2.1.107)
- **No recall claim from the corpus**: one sample moves (100 → 88, medium against a high truth label); one sample is not a rate
- **Not "approvals are migrated"**: approvals of the affected skills stop matching and are asked again once; nothing rewrites
  the store

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The collect, detect, judge and cmd tests above, with their reverse rows, run red on the base; `TestCrossRootSymlinkIgnored` rewritten | `collect: tests — …`, `detect: tests — …`, `judge: tests — …`, `cmd: tests — …` (P-056) |
| 2 | The one predicate in `collect`; `TreeHash` follows `SKILL.md` into the boundary, returns `""` for a refused one, and folds a marker for every other refused entry; dangling links classified by their lexical target | `collect: a skill's SKILL.md is read where it loads, and nothing is refused in silence (P-056)` |
| 3 | `readTextTree` asks the same predicate; `SCOPE-001` on the artifact for a refused file; the credential case; `rulesEpoch` 2 → 3; `make docs` | `detect: a symlinked SKILL.md is scanned, a refused file is scored (P-056)` |
| 4 | `planFor`, `behaviorExcerpt`, `decodedPayloads` and hygiene open a skill's files through the predicate; `ExcerptVersion` bumped | `judge, hygiene: one reading of a skill's files (P-056)` |
| 5 | (if question 10 is decided as recommended) `CLAUDE.md` / `CLAUDE.local.md` resolving outside home: `SCOPE-001` instead of nothing | `collect: an instruction file outside home is refused out loud (P-056)` |
| 6 | Invariant #2 wording in `.claude/rules/invariants.md`, `hash.md`, architecture pair, spec §16 / §13 / §8, judge docs pair | `docs: invariant #2 names the file a skill loads (P-056)` |
| 7 | "Done" in this file, the index | `proposals: P-056 (P-056)` |

## Open questions

1. **Which boundary may a `SKILL.md` symlink resolve into and still be read?** Options, with what each reads on the real
   `~/.claude` (41 links) and the fixture:
   - A — the skill root only (today's rule, made loud): reads 0 of 41; all 41 get `SCOPE-001`, no hash, can never be approved;
     the rules still never read what loads.
   - B — the skills directory that holds the skill (the resolved skill root's parent when it is named `skills`): 41 of 41; c and i
     refused. Reads nothing in the config root outside `skills/` (`history.jsonl`, settings, `.credentials.json`).
   - C — the config root (spec §16 item 2's literal wording): 41 of 41; also reads links into the rest of the root.
   - D — home (`Dir(root)`, the directory-symlink guard's boundary): 41 of 41; c and i read.
   **Recommendation**: B. It covers every measured link, is narrower than the spec's ceiling (so it satisfies it), needs no root
   plumbing (the gate and `scan` hash the same directory with the same rule, so their hashes stay equal), and a link it refuses
   is loud, scored and unapprovable rather than silent. A `check` of a lone directory outside a `skills/` directory gets no
   widening.
2. **Do supporting files get the same widening?**
   **Recommendation**: no — keep invariant #2's skill root for every file except the root `SKILL.md`. No loader reads them;
   measured 0 supporting links leaving their skill on the real `~/.claude`; and the corpus's one symlink sample escapes its own
   directory into its `skills/` directory, so widening to B would turn its attack into a read. They become loud instead (Design 3).
3. **What is the hash?**
   **Recommendation**: a followed `SKILL.md` is hashed under its own name with the target's bytes and no link text — identical to
   a plain copy (measured: `a552389f…` both ways), so identity stays content, a reputation entry for the bytes covers both
   layouts, and retargeting to other bytes re-keys. A refused `SKILL.md` gives `""`: what loads was not read, so there is no
   identity to approve (a constant marker would recreate the collision). A refused supporting entry gives a marker over its link
   text: presence and target re-key, the target's bytes never enter the digest. Re-keying: only artifacts with a symlink leaving
   their root move — 41 skills on the real `~/.claude`, none of the 18 reputation entries carries the empty-input digest, the
   official marketplace mirror has no symlink; the pinned-repo entries are confirmed by `make reputation-refresh` in W2's PR.
4. **What severity is a refusal?**
   **Recommendation**: `SCOPE-001` medium on the artifact, scored — `HOOK-002`'s precedent: an unread file that loads or runs is a
   risk, not only a coverage gap; medium means the gate re-audits on every load (P-005) without blocking a legitimate dotfiles
   layout. A `SKILL.md` that resolves to a credential path is high (`EXFIL-005`'s reasoning: Claude Code would put the secret in
   context; with a body-derived description, in every session's skill list).
5. **Does the judge follow the same predicate?**
   **Recommendation**: yes, for every file: it stops reading a `SKILL.md` outside the boundary (fixture d sends an out-of-`$HOME`
   file today), and it reads supporting-file symlinks inside the skill root that the rules already read (2 on the real
   `~/.claude`). One reading is the point; `ExcerptVersion` moves.
6. **Dangling and looping links.**
   **Recommendation**: classify by the lexical target (`Join(Dir(p), readlink)`): inside the root → present and unread, as the hash
   already marks it; outside → refused as in Design 3. Today the two primitives disagree (the hash says inside, the walk says
   outside). `desktop.go` treats a dangling directory link as "nothing loads, no note"; that stays, since it is about a directory
   Claude Code does not load. A dangling `SKILL.md` still makes no skill, as in Claude Code.
7. **A plugin child's boundary** (P-044).
   **Recommendation**: the plugin root, the container P-044 already bounds children by; Claude Code reads plugin skills with no
   containment. Measured: no installed plugin has a symlinked `SKILL.md` (two in a third-party marketplace mirror, which never loads).
8. **Approvals already stored under the empty-input digest.**
   **Recommendation**: leave them. After the change no skill can hash to it, so they match nothing; `aguard approvals` still lists
   them and `forget` removes them. Dropping them silently on load would be a second rule about which keys are valid.
9. **Findings shown twice** (on the pack, whose tree holds the target, and on the linked skill).
   **Recommendation**: keep both, no fold. They are two skills that load separately, and the gate's verdict for the linked skill
   must carry its own findings; measured 9 duplicates on 4 skills.
10. **`CLAUDE.md` symlinked outside home** is dropped with no note today, and the run says "Nothing to audit under this root".
    **Recommendation**: include it (W5): `SCOPE-001` on the refusal, the same one-line shape as the skills guard. Same class (a
    loaded file refused in silence), no file on the real `~/.claude` is affected.
11. **Symlinked commands, agents and rules outside the root.**
    **Recommendation**: not here (Out of scope). They are disclosed, not silent; scoring them changes `check`'s answer for a
    different surface and deserves its own measurement. Record it as a follow-up in `issues/` when this merges.
12. **Invariant #2's wording.**
    **Recommendation**: "A skill's internal files must not resolve outside the skill root; the one exception is its `SKILL.md`,
    the file Claude Code loads, which may resolve inside the skills directory that holds the skill (a plugin's root for a plugin's
    skill) unless it names a credential. A skill directory that is itself a symlink must resolve inside `$HOME`. Resolution
    failures are classified by the lexical target. Every refusal is scored (`SCOPE-001`), and the hash, the rules and the judge
    read the same files." Spec §16 item 2 then names both boundaries instead of `--root` alone.
