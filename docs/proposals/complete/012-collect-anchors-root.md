<!-- SPDX-License-Identifier: MIT -->
# 012 — With a relative --root, a symlink-installed skill is not collected at all, and `--root .` also misses the configuration under home; the CI template's `.mcp.json` is read only by accident

- **Source**: collect only calls `Clean`, not `Abs`: with a relative spelling a symlink-installed skill is dropped
  entirely, and `--root .` gets home wrong, so the user-level MCP config, the home `CLAUDE.md` and the desktop store are
  all missed; the published CI template `scan --root .` reads the repository's top-level `.mcp.json` only because home
  happens to equal root, and the absolute spelling never reads it. Ported from P-055 in the former private repository
  agent-guard
- **Depends on**: none (can be merged independently of P-010, see Open questions)
- **Branch**: `p/012-collect-anchors-root`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

At its entry `collect.CollectAll` only calls `filepath.Clean(root)`, not `Abs`, and then takes `home := filepath.Dir(root)`
(`internal/collect/collect.go:216-217`). `home` governs two things: where the user-level and project-level configuration
is looked up (`<home>/.claude.json`, `<home>/.mcp.json`, `<home>/CLAUDE.md`, the desktop store and session cache), and
where a symlink-installed skill must land once resolved (invariant #2: `withinDir(home, real)`, out of bounds yields
`SCOPE-001`). `Dir` only looks at the string, so:

- **Relative spelling** (`.claude`, `home/.claude`, `../home/.claude`): home is a relative path. A skill symlink with an
  absolute target (`skills/x → ~/.agents/skills/x`, the most common installation method) resolves to an absolute path,
  `filepath.Rel(relative, absolute)` inside `withinDir` returns an error, and fail-closed returns false — **the skill is
  not collected at all**, and it also gets a `SCOPE-001` that states the wrong reason,
  "Skill dir symlink points outside HOME" (it is inside HOME). A symlink with a relative target happens to survive.
- **`--root .`, `--root ./`** (the most natural spelling after `cd ~/.claude`): `Dir(".")` is still `.`, so
  home == root. All the home-level locations above are looked up **inside** root; the skill boundary shrinks to root, and
  every skill symlink that points out of root is rejected. The report's "Locations" section (`scanLocations` in
  `cmd/aguard/main.go`, likewise `Dir(Clean(root))`) also writes "User MCP config" as `.claude.json` with status
  `absent` — telling the reader it looked and found nothing.

Measured (`main` `dec64ca`, binary built from v0.18.0, `HOME` pointing at the fixture, `--inbox off`; fixture: one plain
skill inside root, one skill linked by absolute path to a location inside HOME, one skill linked by relative path to a
location inside HOME, one skill linked outside HOME, an MCP server `sh -c "curl … | bash"` in `<home>/.claude.json`, an
ordinary server in `<home>/.mcp.json`, and `<home>/CLAUDE.md`; every skill script is `curl … | bash`):

| `--root` spelling | Working directory | artifact | Risk findings | `SCOPE-001` |
|---|---|---|---|---|
| `<home>/.claude`, `<home>/.claude/`, `<home>/.claude/.` | any | 6 | 4 × `EXEC-001` | 1 (the one outside HOME, correct) |
| `.claude`, `.claude/` | `<home>` | 5 (missing the absolutely linked skill) | 3 | **2** (one extra false positive) |
| `home/.claude` | parent directory of `<home>` | 5 | 3 | **2** |
| `../home/.claude` | sibling directory of `<home>` | 5 | 3 | **2** |
| `.claude` | `<home>` reached through a symlink | 5 | 3 | **2** |
| `.`, `./` | `<home>/.claude` | **1** (only the plain skill left) | **1** | **3** (two false positives) |

The `curl | bash` server in `.claude.json` vanishes from the inventory entirely under `--root .` (under the absolute
spelling it carries a high `EXEC-001`). The overall score happens to be 69 on all these rows (capped by one high), but
the inventory and the findings differ — whether the gate lets it through depends on whether what was dropped holds
something heavier than what remains.

**The opposite direction is inconsistent too**: when home == root, `<home>/.mcp.json` reads the `.mcp.json` **inside
root**. With the repository itself as root (the CI template that `hack/github-action.yml` ships to users is exactly
`aguard scan --root . --fail-on high`), the `.` spelling reads the repository's top-level `.mcp.json` and the absolute
spelling does not — measured with the same binary, a `curl | bash` server in the repository's top-level `.mcp.json`:
`--root .` gives overall 69, one `EXEC-001`, and `--fail-on high` exits 1; `--root "$PWD"` gives overall 100, zero
findings, zero notes, exit 0 (`rootOwned` in `unowned.go` records `.mcp.json` as "already read by someone", so there is
not even a disclosure). Under `--root .` the repository's top-level `CLAUDE.md` is also collected twice (`CLAUDE.md` and
`CLAUDE.md (project)` are the same file).

On a real machine, `~/.claude` (same binary, `--inbox off`): the absolute spelling gives overall 69 / artifacts 175 /
findings 806 / notes 10; `--root .claude` after `cd ~` gives 69 / 137 / 643 / 15; `--root .` after `cd ~/.claude` gives
69 / 83 / 574 / 15.

Consequence: for the same environment, the inventory depends on how root is typed; what gets dropped is skills installed
in a **legitimate way** and the user-level MCP config, both prime targets of this tool's audit, and the drop comes with a
note that states the wrong reason.

## Initial direction

Anchor root once at the entry of `CollectAll`: `filepath.Abs` (which includes `Clean`), **not** `EvalSymlinks` (resolving
root's own symlink would move home); take home from the anchored root. `scanEnv` hands the same anchored root to
`analyze`, so detect, Locations, the baseline path and collect share one frame. Under a relative spelling the `path` in
JSON becomes an absolute path; the output of the absolute spelling must stay byte-for-byte the same, and tree hashes do
not change (the hash covers content, not the root path). Do not touch `internal/detect` (that half is P-010).

## Done criteria

All fixtures are built on the fly in `t.TempDir()`, with the temporary directory passed through `EvalSymlinks` first
(macOS's `/var → /private/var`). The "install shape" fixture: root is `<base>/home/.claude`; `skills/plain` (a plain
skill, `curl … | bash` in `scripts/run.sh`), `skills/linked → <home>/agents-store/linked` (absolute target, inside HOME),
`skills/rel-linked → ../../agents-store/rel` (relative target, inside HOME), `skills/escaper → <base>/outside/evil`
(outside HOME); one `sh -c "curl … | bash"` MCP server in `<home>/.claude.json`; one server in `<home>/.mcp.json`;
`<home>/CLAUDE.md`. Spellings: `<abs>`, `<abs>/`, `<abs>/.`, `.claude`, `.claude/` (working directory `<home>`), `.`,
`./` (working directory root), `home/.claude` (working directory `<base>`), `../home/.claude` (working directory
`<base>/sibling`), plus "working directory through a symlink" (`<base>/via → home`, working directory `<base>/via`,
`PWD` pointing at the link, spelling `.claude`). The working directory is set with `os.Chdir` and restored in
`t.Cleanup` (Go 1.23 has no `t.Chdir`; neither package uses `t.Parallel`). The "repository as root" fixture: root is
`<base>/work/repo` (`skills/demo`, one `sh -c "curl … | bash"` server in a top-level `.mcp.json`, one server in a
top-level `.claude.json`), and its home `<base>/work` also has its own `.mcp.json` and `.claude.json`.

- [x] `TestCollectAll_RootSpellingKeepsTheInventory` (`internal/collect/rootspelling_test.go`, new): for every row of the
  "install shape" fixture, the `CollectAll` inventory of (kind, name, hash, path), `Env`, and the notes' (rule, evidence
  file) all equal the absolute spelling; on the "through a symlink" row the paths are in the symlink's frame, so it
  compares only kind/name/hash, `Env` and the note rules. Expected red at W1 on seven rows: `.claude`, `.claude/`,
  `home/.claude`, `../home/.claude` and the through-a-symlink row lack `linked` and have one extra `SCOPE-001`; the `.`
  and `./` rows have only `plain` left (consistent with the binary measurement in "Problem"). The `<abs>/` and
  `<abs>/.` rows are green already at W1 (`CollectAll` already calls `Clean`)
- [x] Reverse assertion (same test): `escaper` is rejected on every row — there is a `SCOPE-001` with evidence pointing
  at `skills/escaper`, and no artifact's path lies under `<base>/outside`. "Rejected" is green at W1 and stays green
  after the fix; "exactly one `SCOPE-001`" is red at W1 on the same seven rows (the extra ones are false positives)
- [x] Reverse assertion (same test): the tree hashes of the three skills are the same on every row and equal the
  `TreeHash` computed directly on the resolved directories — the hash covers content, not how root is spelled;
  `TestHashGolden` stays green without a single change
- [x] `TestScan_RootSpellingIsTheAbsoluteReport` (`cmd/aguard/collectroot_test.go`, new): every spelling of the
  "install shape" fixture goes through `scanEnv`, and the JSON (without `scanned_at`) is **byte-for-byte identical** to
  the absolute spelling — including `root`, `locations`, the artifacts' `path`, all findings and evidence (the "through a
  symlink" row compares only a view with the path fields removed). Expected red at W1 on nine rows: seven rows already
  differ in inventory (as above), and the `<abs>/` and `<abs>/.` rows differ only in the echoed `root`
- [x] `TestCollectAll_RootLevelMCPConfigIsRead` (`internal/collect/rootspelling_test.go`, new): under both the `<abs>`
  and `.` spellings, the "repository as root" fixture collects the servers in the top-level `.mcp.json` and
  `.claude.json` **and** the servers in home's two files, and the two rows have the same inventory. Expected red at W1
  on two rows: the absolute spelling lacks the two at the top of root (never read), `.` lacks home's two
  (home == root). Reverse assertion (same test): when the top-level `.mcp.json` of root is a symlink to
  `<home>/.mcp.json`, that file's server appears only once (the same file is not read twice)
- [x] `TestScan_CITemplateShapeBlocksUnderEverySpelling` (`cmd/aguard/collectroot_test.go`, new): the CI template's
  shape (`hack/github-action.yml` on this repository's `main` is still
  `aguard scan --root . --fail-on high --sarif aguard.sarif`): a `curl | bash` server in the repository's top-level
  `.mcp.json`; `--root .` and `--root "$PWD"` **both** carry `EXEC-001` and **both** block under `--fail-on high`
  (`failGate(out, "high", "", false)` returns exit 1). Expected red at W1 on the `"$PWD"` row (before the fix overall
  100, zero findings, zero notes); the `.` row is green at W1 and stays green after the fix — what blocked before still
  blocks
- [x] Reverse assertions (added during implementation, W6): `TestCollectAll_LinkedRootKeepsItsHome`
  (`internal/collect/rootspelling_test.go`): when root itself is a symlink (`~/.claude → ~/dotfiles/claude`), the
  inventory on the four rows `<abs>`, `<abs>/`, `.` (working directory inside the link) and `.claude` equals that of a
  plain root with the same content, and `Result.Root` is the link itself, not where it points — pinning "only `Abs`, not
  `EvalSymlinks`"; `TestCheck_RelativeRootShapedTargetIsTheAbsoluteReport` (`cmd/aguard/collectroot_test.go`): the JSON
  of `check .claude`, `check ./.claude` and `check ../home/.claude` is byte-for-byte identical to the absolute target —
  pinning that `checkTarget` uses `Result.Root`
- [x] Reverse assertion: when the top of root does **not** have these two files, the absolute spelling's `scan --json`
  is byte-for-byte identical before and after the fix (without `scanned_at`, `tool_version`): on the "install shape"
  fixture (binary before and after), and on a real machine with `scan --root ~/.claude` (neither file exists under
  `~/.claude` on the real machine, confirmed); the review package posts only overall and the artifact/finding/note
  counts
- [x] Reverse assertion: the existing tests for invariant #2 stay green without a single change —
  `TestEscapingSymlinkSkillNoted`, `TestCrossRootSymlinkIgnored`, `TestInstalledSymlinkSkillFound`, `TestWithinRoot`,
  `TestCollect_UnresolvableSkillEntryIsDisclosed`, `TestCollect_DanglingSkillSymlinkIsNotReportedAsAGap`;
  `git diff --stat origin/main -- internal/collect/collect_test.go internal/collect/hash_test.go internal/collect/pathsafe.go internal/collect/hash.go internal/detect`
  is empty
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **Do not touch `internal/detect`**: following hook and permission-grant scripts relative to home is P-010. `scanEnv`
  hands the anchored root to `analyze` (see open question 1), so on the `scan`/`clean` path detect also receives an
  absolute root — this follows necessarily from "collect and detect share one frame", it is not a detect fix made here;
  `check` on a single target, the gate, and `clean`'s restore preview still depend on P-010
- Do not change the boundary logic of `withinDir`, the install-symlink guard, `collectNestedSkills` or `collectPlugins`:
  not one line of invariant #2's "resolve first, then judge; reject on error" changes, only the frame of the root and
  home fed to them
- Do not `EvalSymlinks` root (open question 2)
- **Three other places with the same root cause are not fixed here; decided by the maintainer (2026-10-09) to combine
  them into one proposal, which the lead opens after this one merges**:
  (1) `internal/clean`: measured on this repository's `main`, when the fixture's root has `.aguard-trash/`,
  `cd <root> && aguard clean --root . --undo last --dry-run` reports
  "refusing to use .aguard-trash: it resolves to …/.aguard-trash, outside the scanned root" and exits 2
  (`clean.withinDir` only calls `EvalSymlinks`, not `Abs`, on a relative root), while the absolute spelling of the same
  root works (`Nothing to undo`, exit 0); the `clean` command still hands `--root` unchanged to `clean.*`.
  (2) `CollectTarget` routing: `looksLikeRoot` looks at the directory name in the string as typed — measured: when the
  working directory is a `.claude` without `plugins/installed_plugins.json`, `check .` reads the whole tree as "other
  directory" into 1 `directory` artifact, while `check ../.claude` takes the root layout and yields 5 artifacts.
  (3) `Home: filepath.Dir(root)` in `cmd/aguard/gate.go` and `pluginVersionLine` in `cmd/aguard/version.go` take home
  from the root as typed
- `scanLocations` only gains rows for the two files at the top of root (only when they exist, W4); the two lines that
  take home do not change — the root it receives from `analyze` is already anchored
- Do not change `rootOwned` in `unowned.go`: it says `.mcp.json` and `.claude.json` are already read by someone, and
  after W4 that statement becomes true
- Do not change the hash definition, do not change the JSON schema (no fields added or removed), do not change the spec
  (the spec does not say how home is derived from root; this proposal fixes the implementation's departure from the
  existing statement "home is the parent directory of root", see the comment on `CollectAll`)
- Do not change `hack/` (the CI template keeps `--root .`)
- No new dependencies; `go.mod` unchanged

## Must not claim

- Do not say "the result is the same however `--root` is spelled": `check` routing still looks at the string (item (2)
  of the combined proposal in Out of scope); when the working directory is reached through a symlink, artifact `path`s
  stay in the symlink's frame (same inventory, hashes and findings, different path strings); until P-010 merges,
  following hook scripts in `check` and the gate still varies with the spelling
- Do not say earlier scans "missed malicious skills": what can be said is that the relative spelling dropped skills
  installed by absolute-path symlinks, and `.`/`./` also dropped the home-level configuration and the desktop store; what
  reading them would have produced depends on their content
- **Must say**: for relative spellings and spellings with a trailing slash, the `root` in JSON/SARIF/HTML/markdown,
  artifact `path`s, and the paths in collect note evidence all become the anchored absolute path — an intentional output
  change that affects only users of these spellings. The `uri` of a SARIF result is taken from the evidence's relative
  path and does not change for files inside root (fingerprints unchanged); the `aguard/root` property changes with
  `root`
- **Must say**: in environments with a `.mcp.json` or `.claude.json` at the top of root, the output of the **absolute
  spelling** changes too — the MCP servers in these two files are added (possibly with findings, so a repository running
  CI with an absolute path may newly turn red). This is the absolute spelling's own false negative being fixed, not a
  regression: `rootOwned` has always claimed they were read, and `--root .` has always read them. When the top of root
  has neither file, the absolute spelling is byte-for-byte unchanged; neither exists under `~/.claude` on the real
  machine
- **Must say**: when the working directory is reached through a symlink and `--root` uses a relative spelling, the
  evidence path of files inside root that collect did not resolve (such as `settings.json`) changes from the full
  relative path to a two-segment tail (`settings.json` → `.claude/settings.json`) — the same as today's output for the
  **absolute** spelling of the same path through the symlink; skill evidence is unchanged (open question 9)
- Do not say the score on a real machine changed: the default root on a real machine is an absolute path with neither
  file at its top, and this proposal changes none of its output

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Spelling matrix for both packages and "repository as root", run red | `collect, cmd: tests — a relative --root drops a symlink-installed skill, --root . loses the user-level MCP config, and an absolute root never reads its own .mcp.json, so no two spellings report the same inventory (P-012)` |
| 2 | `CollectAll` anchors root at its entry; `Result.Root` hands out the anchored root | `collect: CollectAll anchors the root once, so a relative --root keeps symlink-installed skills and finds the user-level config (P-012)` |
| 3 | `scanEnv` and `checkTarget` call `analyze` with `Result.Root` | `cmd: scan and check analyse with the root collect anchored, so evidence, locations and the baseline share collect's frame (P-012)` |
| 4 | `.mcp.json` and `.claude.json` at the top of root are read (the same file is not read twice); `scanLocations` lists them | `collect, cmd: a .mcp.json or .claude.json at the top of the root is read under every spelling, so the CI template's --root . keeps blocking a poisoned repository config (P-012)` |
| 5 | `.claude/rules/pipeline.md` gains one guard point (200-line cap, enforced by `TestClaudeRulesAreScopedToExistingPaths`) | `rules: pipeline.md says the root is anchored once in CollectAll and callers analyse with Result.Root (P-012)` |
| 6 | Two reverse tests: root itself is a symlink; `check` on a relative root-shaped target | `collect, cmd: tests — a root that is itself a symlink keeps its home, and check of a relative root-shaped target reports what the absolute one does, so resolving the root or dropping Result.Root turns them red (P-012)` |
| 7 | This file, the index | `proposals: P-012 (P-012)` |

## Open questions

1. **Where does the anchor point go?**
   **Recommendation**: the entry of `CollectAll` (`filepath.Abs`), handed out through a new field `collect.Result.Root`;
   `scanEnv` and `checkTarget` call `analyze` with it (under `check` only targets that take the root layout have a
   `Root`; targets that are a single skill, directory or file stay as typed, and the output of `check ./skill` does not
   change). There is only one anchor point: the alternative is a second `Abs` in main, which is two copies of the same
   code kept in sync by a comment. Keeping the as-typed root away from detect is required — when collect hands out
   absolute paths and detect gets a relative root, `relPath` cannot relate the two frames and evidence degrades to a
   two-segment tail (the CI template's SARIF `uri` and fingerprints change with it). `main.go` changes in three places
   (`scanEnv`, `checkTarget`, and the lines W4 adds to `scanLocations`). There will be one textual conflict with P-005
   (which adds a few lines before the `return` in `scanEnv`); the resolution is to keep both sides; there is no conflict
   with P-010 (it does not touch `main.go`).
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
2. **`Abs` or `EvalSymlinks`?**
   **Recommendation**: only `Abs` (which includes `Clean`). Resolving would move the home of
   `~/.claude → ~/dotfiles/claude` to `~/dotfiles` (the same reason as P-010's open question 3); symlinks are still
   resolved only when `withinDir` checks. If `Abs` fails (the working directory has been deleted), fall back to `Clean`,
   the same as today.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
3. **Under a relative spelling the paths in the output become absolute; is that acceptable?**
   **Recommendation**: accept it, and write it into Must not claim. "The report does not vary with the spelling" is
   exactly the criterion; `.aguardignore` globs on the evidence's relative `file`, evidence for files inside root does not
   change, and existing baselines are unaffected.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
4. **Relationship to P-010?**
   **Recommendation**: do not touch detect; either can merge first. P-010's `anchorRoot` and P-009's `absRoot` are both
   in the detect package; this proposal does not add a function of the same name in the collect package (it only uses
   `filepath.Abs`), so no duplicate definition arises; the cmd test file and helper names avoid P-010's
   `cmd/aguard/rootspelling_test.go` (`spellingTempDir`, `withWorkingDir` and so on) — this proposal uses
   `collectroot_test.go` and `anchored*`. Once both merge, `Engine.Run` already receives an absolute root and
   `anchorRoot` is idempotent.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
5. **Should the test matrix include "working directory through a symlink"?**
   **Recommendation**: yes, but compare only the inventory, hashes and note rules: `Abs` uses `$PWD`, so paths stay in
   the symlink's frame; that is the definition of `Abs`, not a defect; the boundary is judged by `withinDir` after
   resolution, so on this row `linked` is still collected and `escaper` is still rejected.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
6. **The CI template's `--root .` would lose the repository's top-level `.mcp.json` as a result.**
   When home == root, `<home>/.mcp.json` reads exactly the `.mcp.json` at the top of root; the absolute spelling never
   reads it, and `rootOwned` in `unowned.go` records `.mcp.json` and `.claude.json` as "already read by someone", so
   under the absolute spelling there is not even a note (already a silent gap that violates invariant #5). The CI
   template `hack/github-action.yml` ships to users is exactly `aguard scan --root . --fail-on high` (the repository
   itself as root). Measured on this repository's `main` (a `sh -c "curl … | bash"` server in the repository's top-level
   `.mcp.json`): `--root .` overall 69, `EXEC-001`, `--fail-on high` exits 1; `--root "$PWD"` overall 100, zero
   findings, zero notes, exit 0. **With anchoring alone, `--root .` becomes the latter: the CI template goes from
   blocking such a repository to silently letting it through.**
   - **A (recommended)**: this proposal also reads `.mcp.json` and `.claude.json` at the top of root (when present; not
     read twice when they are the same file as home's two), producing one artifact per server, which may share a name
     with a home-level one but has a different `path` (existing precedent, see the comment on `mcpServersFrom`).
     `rootOwned`'s statement becomes true from then on. Cost: the absolute spelling's output changes — only when the top
     of root actually has these two files; neither exists under `~/.claude` on the real machine.
   - **B**: anchor only; open a separate proposal for these two files at the top of root. Between this one merging and
     that one merging, the CI template is silently green on a repository's top-level `.mcp.json`.
   - **C**: B, plus removing `.mcp.json` and `.claude.json` from `rootOwned` so the unowned `COV-000` names them as "not
     read". The gate still lets them through, but at least not silently.
   **Recommendation**: A.
   **Decided (2026-10-09, by the maintainer)**: A. The reason is the measurement above: the CI template's `--root .`
   blocks a `curl | bash` in a repository's top-level `.mcp.json` today, and anchoring alone would let it through
   silently. The absolute spelling's output therefore changes when the top of root has these two files; this goes into
   Must not claim: it is a false negative being fixed, not a regression.
7. **`check .` routing also varies with the spelling; fix it together?**
   **Recommendation**: no, open it separately (see Out of scope): it changes the shape of `check`'s artifacts (one
   directory for the whole tree → several artifacts under the root layout) and needs its own before/after comparison;
   this proposal's criteria look only at `scan --root` and `CollectAll`.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port); the maintainer separately
   decided to combine it with `clean` and the two places in `gate.go`/`version.go` into one proposal, opened after this
   one merges.
8. **Once both this and P-010 merge, P-010's `TestScan_RootSpellingDoesNotChangeTheResult` turns red; who changes it?**
   The reports of the two spellings are byte-for-byte identical; the cause of the red is `spellingView` in P-010's test:
   it strips the prefix of collect note evidence by the root **as typed** (`filepath.Clean(root)`); after this proposal
   those paths are anchored absolute paths, so the prefix strips from the absolute spelling's copy but not from the
   relative spelling's. Stripping by the report's own root turns it green: measured on this repository,
   `filepath.Clean(root)` in the **two lines** at the top of `spellingView` (the line computing the prefix and the line
   testing for `"."`) must become `filepath.Clean(out.Root)`; changing only the first line leaves the `dot` and
   `dot slash` rows red; on P-010's own branch these two lines behave the same (there `out.Root` is the root as typed).
   **Recommendation**: whichever PR merges second carries these two lines when it rebases: if P-010 merges first, this
   one changes them on rebase; if this one merges first, P-010 changes them on rebase. State it in the PR description.
   **At merge (2026-10-09)**: P-010 merged first, and this one carries the two lines. After the rebase P-010's test was
   red on all six relative-spelling rows, and turned green after the change.
   There is one more overlap, with P-005: in `scanEnv` P-005 computes the judge's home from the absolute root, and this
   proposal changes `analyze` to be called with collect's anchored `res.Root`; after the merge both are present —
   `analyze` is called with `res.Root`, and the judge's home is also computed from `res.Root` (still passed through
   `filepath.Abs` once). The combined measurement on this repository is in "Done".
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
9. **When the working directory is reached through a symlink and `--root` uses a relative spelling, the evidence paths
   of some files inside root get shorter; does that count as overstepping?**
   collect's paths are in `$PWD`'s frame (open question 5), and detect's `relPath` resolves root but not absolute file
   paths, so files inside root that collect did not resolve (`settings.json`, things like `commands/ns/foo.md` three or
   more levels deep) degrade to a two-segment tail: evidence on `settings.json` changes from `settings.json` (today's
   relative spelling) to `.claude/settings.json`. That is exactly today's output when `--root` is written as **the same
   absolute path through the symlink**, so the criterion "equals the absolute spelling" holds; but for this small class
   of users it is a regression in evidence paths (a glob written as `settings.json` in `.aguardignore` stops matching, in
   the direction of reporting more, not less). Skill evidence is unaffected (collect provides the resolved path). The fix
   belongs in detect's `relPath` (resolve the directory of absolute paths too); it would change the output of the
   absolute spelling through a symlink, and is outside this proposal's scope.
   **Recommendation**: accept it, and write it into Must not claim; whether the `relPath` change joins the combined
   proposal is left to the lead. The measurement on this repository is in "Done".
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).

## Done

```
Merged: PR #30 (2026-10-09; find the sha with git log --grep P-012)
Released: pending release
Evidence: TestCollectAll_RootSpellingKeepsTheInventory (internal/collect/rootspelling_test.go); W1 on this repository's main (dec64ca) red on seven rows (.claude, .claude/, home/.claude, ../home/.claude and the through-a-symlink row: env Skills 2 vs 3, two SCOPE-001 [skills/escaper, skills/linked]; the ., ./ rows: Skills 1, MCPServers 0, three SCOPE-001 [escaper, linked, rel-linked]) → after W2 all ten rows green; the <abs>/, <abs>/. rows green already at W1
Evidence: reverse assertion, same test — on all ten rows escaper is rejected and only it (exactly one SCOPE-001, no artifact under outside); the tree hashes of the three skills on every row equal the TreeHash computed directly on the resolved directories; TestHashGolden unchanged and still green
Evidence: TestScan_RootSpellingIsTheAbsoluteReport (cmd/aguard/collectroot_test.go); W1 red on nine rows (<abs>/, <abs>/. diverge at JSON line 2, the root echo; seven rows have a different inventory) → after W3 all nine rows green, JSON byte-for-byte identical to the absolute spelling
Evidence: TestCollectAll_RootLevelMCPConfigIsRead (internal/collect/rootspelling_test.go); W1 red on two rows (the absolute spelling has only home-proj, home-user, missing repo-pwn, repo-user; . has only repo-pwn, repo-user with relative paths, missing home's two) → green after W4; the "same file read only once" subtest green already at W1, still green after the fix
Evidence: TestScan_CITemplateShapeBlocksUnderEverySpelling (cmd/aguard/collectroot_test.go); W1: . green, "$PWD" red (overall 100, no EXEC-001, failGate returns nil) → after W3 (anchoring only) . red too (overall 100) — exactly the consequence open question 6 predicted → after W4 both rows carry EXEC-001, failGate exits 1
Evidence: W6 TestCollectAll_LinkedRootKeepsItsHome, TestCheck_RelativeRootShapedTargetIsTheAbsoluteReport; mutation (temporary change, run, revert, not committed): CollectAll changed to EvalSymlinks(Abs(root)) → the former red on four rows; checkTarget not using Result.Root → the latter red on three rows
Evidence: mutation: removing the SameFile dedup → the "read only once" subtest red; CollectAll not reading RootMCPConfigs → TestCollectAll_RootLevelMCPConfigIsRead red on two rows, the CI template test red on two rows; scanEnv calling analyze with the root as typed → TestScan_RootSpellingIsTheAbsoluteReport red on eight rows; only Clean, no Abs → the inventory matrix red on seven rows; git status empty after each revert
Evidence: binary before/after (main dec64ca vs this branch, fixture under /private/tmp, HOME pointing at the fixture, --inbox off): absolute spelling scan --json with scanned_at, tool_version removed shows no cmp difference (5494 bytes); after the fix eight spellings are byte-for-byte identical to the absolute spelling, before the fix they differed by 2/2/68/68/66/66/140/140 lines respectively (<abs>/, <abs>/., .claude, .claude/, home/.claude, ../home/.claude, ., ./); CI repository shape after the fix: . and "$PWD" byte-for-byte identical, both overall 69, EXEC-001, --fail-on high exits 1 (before the fix "$PWD" scored 100, zero findings, zero notes, exit 0)
Evidence: open question 9 measured (fixture root plus a settings.json whose hook is curl … | bash): with the working directory through a symlink and --root .claude, the EXEC-001/HOOK-001 evidence is settings.json on main and .claude/settings.json on this branch, the same as the output of main and this branch for the same absolute path through the symlink; with the working directory not through a symlink both sides give settings.json
Evidence: real machine ~/.claude (--inbox off): main absolute spelling overall 69 / artifacts 175 / findings 806 / notes 10, this branch's absolute spelling the same; a rerun of main's absolute spelling and this branch's absolute spelling give byte-for-byte identical JSON (scanned_at, tool_version removed) (15678 lines)
Evidence: real machine, relative spellings (this branch): the two JSONs from --root .claude after cd ~ and --root . after cd ~/.claude are byte-for-byte identical to the absolute spelling above; on main the same two spellings give .claude artifacts 137 / findings 643 / notes 15, . artifacts 83 / findings 574 / notes 15
Evidence: Out of scope — git diff --stat origin/main -- internal/detect internal/clean cmd/aguard/gate.go cmd/aguard/version.go internal/collect/pathsafe.go internal/collect/hash.go internal/collect/unowned.go internal/collect/plugins.go internal/collect/collect_test.go internal/collect/hash_test.go docs/spec go.mod go.sum hack is empty; hack/github-action.yml is still aguard scan --root . --fail-on high --sarif aguard.sarif
Evidence: Out of scope (current state of the separate proposal, measured on main): with .aguard-trash/ under root, cd <root> && clean --root . --undo last --dry-run exits 2 (refusing to use .aguard-trash … outside the scanned root), the absolute spelling gives Nothing to undo, exit 0; check . inside a .claude without plugins/installed_plugins.json yields 1 directory artifact, check ../.claude yields 5
Evidence: combination with P-010 (temporary worktree merging origin/p/010-relative-root-hook-scripts, not committed, deleted): no code conflict (index line only); detect, collect all green; in cmd only P-010's TestScan_RootSpellingDoesNotChangeTheResult red on six rows (relative, relative trailing slash, dot, dot slash, relative through the parent, relative from a sibling under every kind of second stage), all green after changing the two filepath.Clean(root) in spellingView to filepath.Clean(out.Root) (changing only the prefix line leaves dot, dot slash red); the same two-line change alone on the P-010 branch keeps that test green (open question 8)
Evidence: combination with P-005 (as above, temporary merge): the only conflict is at the return in scanEnv; after keeping both sides (P-005's o.home computation first, this proposal's res := collect.CollectAll(root); return analyze(res.Root, res, o) after) go test -race ./cmd/aguard/ ./internal/judge/ is all green; git merge-tree with P-001, P-002, P-003, P-004, P-009 conflicts only on the index line
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5, module line github.com/basdotio/AgentGuard, no new dependencies
```
