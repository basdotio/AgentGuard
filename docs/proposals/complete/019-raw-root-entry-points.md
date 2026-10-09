<!-- SPDX-License-Identifier: MIT -->
# 019 — The same directory spelled another way gets another answer: `clean --root .` refuses to undo, `check .` does not read the root layout, and under a relative spelling the gate lets plugin skills through unaudited

- **Source**: entry points with the same root cause left after P-012 was merged ((1)(2)(3) of the item in P-012's
  "Out of scope" that calls for one combined proposal, plus open questions 7 and 9); decided by the maintainer
  (2026-10-09) to combine them into one
- **Depends on**: P-012 (already in `main`)
- **Branch**: `p/019-raw-root-entry-points`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

P-012 anchors the root to an absolute path at the `collect.CollectAll` entry point, so the `scan` report no longer depends
on how `--root` is typed. Several other entry points still decide on **the string as typed**, so the answer for one
directory depends on how the path is spelled:

1. **`internal/clean`**: `withinDir` runs only `EvalSymlinks` on the base, not `Abs` (`internal/clean/clean.go:532`).
   `EvalSymlinks(".")` is still `.`, which cannot be `Rel`'d against the resolved absolute trash path, and an error means
   refusal — so `safeTrash` judges the root's own `.aguard-trash/` to be "outside the scanned root".
   The `clean` command still hands `--root` to `clean.*` as typed (the clean `RunE` in `cmd/aguard/main.go`).
2. **Routing in `check` / `hash`**: `collect.CollectTarget` uses `looksLikeRoot(path)` to decide "does this look like a
   root", and that checks `filepath.Base(dir) == ".claude"` (`internal/collect/collect.go:110`) — `Base(".")` is `.`, and
   `Base("<abs>/.claude/.")` is also `.`. When the working directory is a `.claude` without
   `plugins/installed_plugins.json`, `check .` takes the "any other directory" branch and reads the whole tree as **one**
   `directory` artifact; `check ../.claude` takes the root layout. `hash` uses the same `CollectTarget`, so the identity
   it prints changes too.
3. **The gate and `version` take home from the root as typed**: `gateOptions` in `cmd/aguard/gate.go` has
   `Home: filepath.Dir(root)`, and `pluginVersionLine` in `cmd/aguard/version.go` has
   `filepath.Dir(filepath.Clean(root))`. Home goes to `collect.PluginInstalls`, which runs `withinDir(home, real)` on every
   plugin (invariant #2) — a relative home cannot be related to an absolute `real`, an error means refusal, and **every
   plugin installed by the CLI is dropped**; under an absolute spelling with a trailing slash or trailing dot
   (`gateOptions` does not `Clean`), home == root, and none of the plugins installed by the desktop app (under
   `<home>/Library/…`) are found. When the gate cannot find a skill it fails open by design: it allows the load and
   emits `GATE-000`.

Measured (binary built from this repo's `main` `fd28344`; `HOME` points at the fixture. Fixture A: under `<home>/.claude`,
`skills/plain` (`curl … | bash` in `scripts/run.sh`), `skills/linked → <home>/agents-store/linked` (absolute target,
inside HOME), one hook in `settings.json`, `sh ~/.claude/hooks/pre.sh` (`curl … | bash`), `history.jsonl`, an empty
`.aguard-trash/`, and **no** `plugins/installed_plugins.json`. Fixture B: `<home>/.claude/plugins/installed_plugins.json`
holds `myplug@mk` (skill `hello`, `curl … | bash`) and `aguard@AgentGuard` 0.18.0, and the desktop app's store holds
`dplug` (skill `dhello`, `curl … | bash`). The gate is fed `PreToolUse[Skill]`, `permission_mode: default`):

| Spelling of `--root` / target | Working directory | `clean --undo last --dry-run` | `clean --zombie --apply --dry-run` | `check` (A) | `hash` (A) | `version` plugin line (B) | Gate `myplug:hello` (B) | Gate `dplug:dhello` (B) |
|---|---|---|---|---|---|---|---|---|
| `<abs>` | any | Nothing to undo, exit 0 | would quarantine plain, exit 3 | 2 skills + 1 hook | 3 lines | "matches this binary" | `ask` (EXEC-001) | `ask` |
| `<abs>/` | any | same | same | same | same | same | `ask` | **`GATE-000`** |
| `<abs>/.` | any | same | same | **1 `directory`** | **`directory:.`** | same | `ask` | **`GATE-000`** |
| `<base>/via/.claude` (absolute spelling through a symlink) | any | same | same | 2 skills + 1 hook | 3 lines | same | `ask` | `ask` |
| `.claude` | `<home>` | **refused, exit 2** | **refused, exit 2** | 2 skills + 1 hook | 3 lines | **none** | **`GATE-000`** | `ask` (by chance) |
| `.claude/.` | `<home>` | **refused** | **refused** | **1 `directory`** | **`directory:.`** | **none** | **`GATE-000`** | **`GATE-000`** |
| `.`, `./` | root | **refused** | **refused** | **1 `directory`** | **`directory:.`** | **none** | **`GATE-000`** | **`GATE-000`** |
| `../.claude` | root | **refused** | **refused** | 2 skills + 1 hook | 3 lines | **none** | **`GATE-000`** | `ask` (by chance) |
| `..` | `<root>/skills` | **refused** | **refused** | **1 `directory`** | **`directory:.`** | **none** | **`GATE-000`** | **`GATE-000`** |
| `home/.claude` | `<base>` | **refused** | **refused** | 2 skills + 1 hook | 3 lines | **none** | **`GATE-000`** | `ask` (by chance) |
| `../home/.claude` | `<base>/sibling` | **refused** | **refused** | 2 skills + 1 hook | 3 lines | **none** | **`GATE-000`** | `ask` (by chance) |
| `.claude` | `<home>` reached through a symlink | **refused** | **refused** | 2 skills + 1 hook | 3 lines | **none** | **`GATE-000`** | `ask` (by chance) |

"By chance": home is the relative `.`/`..`, the desktop store path joined from the working directory is relative as well,
and `Rel` happens to work. The refusal reads
"refusing to use .aguard-trash: it resolves to …/.aguard-trash, outside the scanned root" (`.claude/.aguard-trash` etc.
varies with the spelling). Under a relative spelling the gate does not drop the skill in the root (`plain`), but the path
in the reason and the `aguard approve …` command it gives are relative (`skills/plain`), and copying them from another
working directory fails. `clean --json` (plan listing only) goes through `scanEnv` and has been independent of the
spelling since P-012.

Consequences:

- `clean`'s undo and quarantine **all fail** under the most natural spelling after `cd ~/.claude`, and `--undo` is
  exactly the one needed after something has gone wrong.
- Inside a `.claude`, `check .` gives a different artifact inventory, score composition and hash than `check "$PWD"`;
  `aguard approve .` records a different identity.
- The gate: a plugin skill with a high finding gets `ask` under an absolute spelling and is **allowed unaudited** under a
  relative one, leaving only a `GATE-000`. The hook command registered in settings.json carries no `--root` (it uses
  `defaultRoot`, usually an absolute path), so everyday loads do not reach this; what does reach it is feeding events by
  hand while debugging the gate (the fastest loop listed in `CLAUDE.md`), a relative `CLAUDE_CONFIG_DIR`, and spellings
  produced by completion such as `--root ~/.claude/`.
- Under a relative spelling `version` reports no plugin version, and the "plugin is newer/older than the binary" hint
  disappears.

The spot in P-012 open question 9 (some evidence paths shrink to two segments when the working directory is reached
through a symlink) is a separate matter: measured, a relative spelling gives the same output as **the same absolute
spelling through the symlink** (`settings.json` → `.claude/settings.json`, `hooks/deep/x/pre.sh` → `x/pre.sh`, under both
spellings). It is not a spelling dependency but detect `relPath`'s existing display rule for absolute paths through a
symlink.

## Initial direction

Export one anchoring function, `collect.AnchorRoot` (exactly the lines `CollectAll` has inline today: `filepath.Abs`,
falling back to `Clean` on failure, **no** `EvalSymlinks`), and have `CollectAll` use it; `looksLikeRoot` decides on the
anchored path; `clean` anchors once at the entry points of its write paths (`quarantine`, `Undo`, `KeepBoth`,
`QuarantineRefusal`); `gateOptions` and `pluginVersionLine` anchor first, then take `Dir`. The synonymous `anchorRoot`
(P-010) and `absRoot` (P-009) in detect are changed to call it (a mechanical replacement; both are already pinned by
spelling-matrix tests). Single-target `check ./skill` is unchanged; `relPath` is unchanged (it would change the output of
absolute spellings).

## Done criteria

All fixtures are built on the spot in `t.TempDir()`, with the temp directory run through `EvalSymlinks` first (macOS
`/var → /private/var`); the working directory is set with `os.Chdir` plus `PWD` and restored in `t.Cleanup` (reusing
P-012's `anchorChdir`/`anchoredChdir`; these packages have no `t.Parallel`). The spelling matrix is, throughout: `<abs>`,
`<abs>/`, `<abs>/.`, `.claude`, `.claude/.` (working directory `<home>`), `.`, `./`, `../.claude` (working directory
root), `..` (working directory `<root>/skills`), `home/.claude` (working directory `<base>`), `../home/.claude` (working
directory `<base>/sibling`), plus "working directory through a symlink" (`<base>/via → home`, spelled `.claude`).

- [x] `TestClean_RootSpellingIsTheAbsoluteRun` (`internal/clean/rootspelling_test.go`, new): one zombie skill under the
  root, with artifact paths the absolute paths collect gives. For each spelling, in order: the output and `Result` of an
  `Apply` dry run equal the absolute spelling's; a real `Apply` succeeds; then the output of an `Undo(…, "last")` dry run
  equals the absolute spelling's dry run for the same batch; a real `Undo` puts the skill back where it was; the output
  of a `KeepBoth` dry run equals the absolute spelling's; the answer of `QuarantineRefusal` equals the absolute
  spelling's (empty, i.e. it may be moved). The paths in the "through a symlink" row are in the link's coordinate system
  and are compared after mapping the link back to home. Expected red at W1: the nine rows other than `<abs>`, `<abs>/`,
  `<abs>/.` report "outside the scanned root" at the first step (the `Apply` dry run returns an error and the test stops
  there)
- [x] Reverse assertion (same file): when `.aguard-trash` is a symlink pointing outside the root, **every spelling**
  refuses (the error says "it is a symlink") and nothing new appears in the directory outside the root; a `--root` inside
  `rules/` (working directory `<root>/rules`, spelled `.`) is still refused with "inside a \"rules\" directory".
  Green already at W1, still green after the fix
- [x] `TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget` (`internal/collect/targetspelling_test.go`, new):
  P-012's "install-shaped" fixture (no `plugins/installed_plugins.json`); under every spelling `CollectTarget` takes the
  root layout — `Result.Root` equals the absolute root, and the inventory (kind, name, hash, path) and notes equal the
  absolute spelling's (the "through a symlink" row compares only kind, name, hash and note rules, P-012 open question 5).
  Expected red at W1 in five rows: for `<abs>/.`, `.claude/.`, `.`, `./`, `..` the `Root` is empty and the inventory is a
  single `directory`
- [x] Reverse assertion (same file): single targets stay as typed — with the working directory at the root,
  `CollectTarget("skills/plain")` and `CollectTarget("./skills/plain")` have an empty `Root`, one `skill`, and a `Path`
  that is exactly the typed string; a directory **not** named `.claude` that has `skills/` and `settings.json` is still
  one `directory` artifact, spelled `.` from inside it or as an absolute path from outside (`looksLikeRoot` is not
  loosened). Green already at W1, still green after the fix
- [x] `TestCheck_RootSpellingInsideAConfigRootIsTheAbsoluteReport` (`cmd/aguard/rawroot_test.go`, new): the same fixture
  through `checkTarget`; the JSON (without `scanned_at`) is **byte-identical** to the absolute target's (the "through a
  symlink" row uses P-012's path-stripped view). Expected red at W1 in five rows (same as above)
- [x] `TestHashCommand_RootSpellingPrintsTheAbsoluteHashes` (same file): the real binary runs `aguard hash <spelling>`
  in each working directory, and stdout equals that of `aguard hash <abs>`. Expected red at W1 in five rows (one
  `directory:.` line printed)
- [x] `TestGate_RootSpellingResolvesTheSamePlugins` (same file): fixture B's shape — `myplug@mk` installed by the CLI
  (skill `hello`, `curl … | bash`), `dplug` in the desktop store (skill `dhello`, likewise), and skill `plain` in the
  root. For each spelling: `gateOptions`'s `Root` equals the absolute root and `Home` equals its parent; `runHook`'s
  replies to the three `PreToolUse[Skill]` events for `myplug:hello`, `dplug:dhello` and `plain` are **byte-identical**
  to the absolute spelling's, and all are `ask`. Expected red at W1: in the nine relative rows `Root`/`Home` differ and
  `myplug:hello` is `GATE-000`; in the two rows `<abs>/` and `<abs>/.`, `Home` equals the root and `dplug:dhello` is
  `GATE-000`; under relative spellings the path in `plain`'s reason is relative. The fixture lives in a short temp
  directory: the gate truncates paths longer than 160 characters before writing them into the reason, and `t.TempDir()`
  carries the subtest name, long enough that in the "through a symlink" row `via` and `home` would be cut at different
  characters
- [x] Reverse assertion (same test): a plugin `outplug` whose `installPath` resolves outside HOME is not resolved under
  **any spelling** (`outplug:x` is `GATE-000`, `collect.PluginInstalls` does not contain it) — anchoring does not loosen
  invariant #2's plugin boundary. Green already at W1, still green after the fix
- [x] `TestPluginVersionLine_RootSpelling` (same file): `pluginVersionLine(<spelling>, "v0.18.0")` equals the absolute
  spelling's "plugin aguard 0.18.0 matches this binary" in every row. Expected red at W1 in nine rows (relative spellings
  return an empty string)
- [x] Reverse assertion: the absolute spelling's results are first pinned with literal values (the target path in the
  clean dry-run line, `check`'s artifact inventory, the gate's two `ask` replies carrying `EXEC-001`, the version line);
  green already at W1, and still green after the fix without a single character changed
- [x] Reverse assertion: the binary's output under the absolute spelling is byte-identical before and after the fix
  (without `scanned_at`, `tool_version`): fixture A's `scan --json`, `check --json`,
  `hash`, `clean --undo last --dry-run`, `clean --zombie --apply --dry-run`, and fixture B's second `version` line and
  the two gate replies; `check ./skills/plain` and `check skills/plain` (single targets) are byte-identical before and
  after; on a real machine the `scan --root ~/.claude` JSON is byte-identical before and after (the review package only
  posts overall and the artifact/finding/note counts)
- [x] After W5 (detect deduplication): `func absRoot` does not exist, detect's `anchorRoot` is a single line forwarding to
  `collect.AnchorRoot` (the name stays; P-010's test comments and `detect.md` refer to it), and the only
  `filepath.Abs(root)` left in the repository is the one in `AnchorRoot` itself; mutation (change temporarily, run,
  revert, do not commit) of `collect.AnchorRoot` to only `Clean` without `Abs` → the spelling matrices of all four
  packages collect, detect, clean and cmd go red; changing it to `EvalSymlinks(Abs(root))` → the rows where the root
  itself is a symlink go red, proving that one function pins every entry point
- [x] Existing tests stay green without a single character changed:
  `git diff --stat origin/main -- internal/clean/clean_test.go internal/collect/collect_test.go
  internal/collect/rootspelling_test.go internal/collect/hash_test.go internal/detect/rootspelling_test.go
  cmd/aguard/rootspelling_test.go cmd/aguard/collectroot_test.go cmd/aguard/main_test.go cmd/aguard/gate_e2e_test.go`
  is empty (it covers `TestHashGolden`, the P-009/P-010/P-012 spelling matrices and invariant #2's boundary tests)
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **Do not change detect's `relPath` (P-012 open question 9)**: measured, a relative spelling already gives the same
  output as the same absolute spelling through the symlink (last paragraph of "Problem"), so the criterion "equals the
  absolute spelling" holds; fixing it (resolving the directory for absolute paths too) would change the output of
  **absolute spellings through a symlink** and of "`~/.claude` itself is a symlink" — SARIF `uri`, fingerprints and
  `.aguardignore` globs would all change with it — which is beyond this proposal. A separate proposal
- **Do not change single-target `check` output**: `check ./skills/plain` still echoes the typed path; inside a skill
  directory `check .` still names the artifact `.` (the absolute spelling names it `plain`) — renaming would change
  single-target output, which this proposal pins as unchanged. A separate proposal
- Do not loosen `looksLikeRoot`: the markers are still only "the directory is named `.claude`" and "it has
  `plugins/installed_plugins.json`", now decided on the anchored path (`.claude/rules/pipeline.md` explains why)
- Do not `EvalSymlinks` the root (the same reason as P-010 open question 3 and P-012 open question 2); do not change the
  decision logic of `clean`'s `withinDir`/`resolved`/`protected`/`quarantinable`, and do not change `collect.withinDir`
  or the boundary of `PluginInstalls` — change only which coordinate system the root and home fed to them are in
- Do not change the path echo of `hook install`/`uninstall`/`status`, `approvals`, `forget`: reading and writing by the
  typed root, they reach the same file; the path echoed in their output varies with the spelling, but the answer does
  not (measured: `hook install --dry-run` and `hook status` give the same output under both spellings, and `approvals`
  differs only in the echoed path)
- Do not change the hash definition (`internal/collect/hash.go` untouched), the JSON schema, the rules or the scores; the
  spec is unchanged (the spec does not say how home is derived from the root)
- Do not change `hack/`, add no dependency, `go.mod` untouched

## Must not claim

- Do not say "the result is the same however `--root` is spelled": when the working directory is reached through a
  symlink, paths stay in the symlink's coordinate system (P-012 open question 5); through a symlink some evidence shrinks
  to a two-segment tail (first item of "Out of scope"); single-target `check` echoes the typed path
- Do not say the old gate "let malicious plugins through": what can be said is that under relative spellings and
  spellings with a trailing slash/dot the gate **could not find** installed plugin skills, and by design let them load
  and emitted `GATE-000`; the hook command registered in settings.json carries no `--root`, and everyday loads use
  `defaultRoot` (usually an absolute path)
- **Must say**: inside a `.claude`, `check .` (and `check <abs>/.claude/.`, `check ..`) changes from "the whole tree as
  one `directory`" to the root layout — the same as `check "$PWD"`: top-level directories of the root that nothing
  references are **disclosed by name, not read** (`COV-000`), and the artifact inventory, the score composition, the
  identity `hash` prints and the hash `approve` records all change with it. This aligns with the absolute spelling; it
  does not loosen routing
- **Must say**: under relative spellings, `clean`'s output, `check`'s JSON, the paths in the gate's reason and the
  `aguard check`/`aguard approve` commands it gives all become anchored absolute paths
- Do not say the output on a real machine changed: the default root on a real machine is an absolute path, and this
  proposal changes none of its output (as measured)

## Work items

| W | In one sentence | Commit message (no sha; rebase changes it) |
|---|---|---|
| 1 | Spelling matrices in clean, collect and cmd, run red | `clean, collect, cmd: tests — a relative --root makes clean refuse its own trash, check . inside a config root reads one directory, and the gate and version lose the installed plugins, so one directory gets a different answer per spelling (P-019)` |
| 2 | Export `collect.AnchorRoot`, `CollectAll` uses it, `looksLikeRoot` decides on the anchored path | `collect: one exported root anchor, and check routes on the anchored path, so check . inside a config root takes the root layout (P-019)` |
| 3 | `clean` anchors at the write-path entry points | `clean: the root is anchored where every move, restore and baseline write starts, so a relative --root no longer refuses its own trash (P-019)` |
| 4 | `gateOptions` and `pluginVersionLine` anchor first, then take home | `cmd: the gate and version take home from the anchored root, so a relative or slash-ended --root finds the installed plugins (P-019)` |
| 5 | detect's `anchorRoot` and `absRoot` replaced by `collect.AnchorRoot` | `detect: the engine and the content hashes anchor with collect.AnchorRoot instead of two private copies of it (P-019)` |
| 6 | `.claude/rules/pipeline.md` updates the P-012 guard point, `detect.md` updates the function name (net zero lines) | `rules: pipeline.md says every entry point that routes on the root or derives home from it anchors with collect.AnchorRoot (P-019)` |
| 7 | This file, the index | `proposals: P-019 (P-019)` |

## Open questions

1. **Where does the anchoring function live, and what is it called?**
   **Recommendation**: `collect.AnchorRoot`, whose body is exactly the lines `CollectAll` has inline today
   (`filepath.Abs`, falling back to `Clean` on failure, no `EvalSymlinks`). collect is where P-012 put the anchor; clean,
   gate, detect and cmd all import collect already, so no import edge is added.
   **Decided (2026-10-09)**: as recommended.
2. **At which layer does clean anchor?**
   **Recommendation**: the package's write-path entry points — `quarantine` (the single move path shared by `Apply`,
   `Resolve` and `ResolveInteractive`), `Undo`, `KeepBoth`, `QuarantineRefusal` — not the clean `RunE` in `main.go`. The
   same convention as P-010 (`Engine.Run`) and P-012 (`CollectAll`): every caller of the package is covered at once.
   `withinDir`'s decision is unchanged — from now on it only receives an absolute base; changing it alone without
   anchoring the entry points would get the decision right, but the paths echoed in the output would still vary with the
   spelling.
   **Decided (2026-10-09)**: as recommended.
3. **Where does `check`'s routing anchor?**
   **Recommendation**: `looksLikeRoot` itself anchors first, then decides — "the directory is named `.claude`" is a
   property of the directory, not of the string; the three single-target branches still use the typed `path`, so
   `check ./skill` is unchanged. `CollectAll(path)` still anchors on its own (idempotent).
   **Decided (2026-10-09)**: as recommended.
4. **Inside `.claude`, `check .` switches to the root layout and reads less (top-level directories nothing references
   are only disclosed, not read). Is that acceptable?**
   **Recommendation**: accept it, and write it into "Must not claim". The criterion is "equals the absolute spelling",
   and `check "$PWD"` and `check ../.claude` already take the root layout today; `looksLikeRoot`'s comment and
   `pipeline.md` say why a root-shaped tree is rightly read by the root's rules. The opposite direction (making the
   absolute spelling read the whole tree) would change the output for every `check ~/.claude` user.
   **Decided (2026-10-09)**: as recommended.
5. **Where does the gate anchor?**
   **Recommendation**: `gateOptions` — this is where home is derived from the root, and the `Root`/`Home` of
   `gate.Options` are supplied by the caller. The `gate.ApprovalsPath(root)` that `runHook` reads approvals through is
   unchanged: the relative path and the anchored path point to the same file.
   **Decided (2026-10-09)**: as recommended.
6. **Should detect's `anchorRoot` (P-010) and `absRoot` (P-009) be merged into one here?**
   **Recommendation**: merge them, in a commit of their own (W5): the two function bodies are identical character for
   character, and changing them to call `collect.AnchorRoot` is a mechanical replacement; P-009's `hash` spelling test,
   P-010's detect/cmd spelling matrices and P-012's matrix all pin it, and the mutation check proves that one function
   pins every entry point.
   **Decided (2026-10-09)**: as recommended.
7. **Is P-012 open question 9 (`relPath`) folded in?**
   **Recommendation**: no. Measured, it is not a spelling dependency (a relative spelling gives the same output as the
   same absolute spelling through the symlink); the fix would change the output of absolute spellings through a symlink
   and of "`~/.claude` itself is a symlink", and this proposal's reverse assertion is precisely "absolute spellings do not
   change". To be raised separately as a follow-up.
   **Decided (2026-10-09)**: as recommended.
8. **Does `check .` naming the artifact `.` inside a skill directory belong to this proposal?**
   **Recommendation**: no; a separate proposal. It is single-target naming, and changing it would change output of the
   `check ./skill` kind; this proposal pins single targets as unchanged.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #40 (2026-10-09; find the sha with git log --grep P-019)
Released: pending release
Evidence: TestClean_RootSpellingIsTheAbsoluteRun (internal/clean/rootspelling_test.go); W1 red in nine rows on this repo's main (fd28344) (.claude, .claude/., ., ./, ../.claude, .., home/.claude, ../home/.claude, .claude through a symlink, all reporting "refusing to use <spelling>/.aguard-trash: it resolves to …/.aguard-trash, outside the scanned root" at the Apply dry run) → all twelve rows green after W3; the three rows <abs>, <abs>/, <abs>/. green already at W1
Evidence: reverse assertion TestClean_RootSpellingKeepsTheRefusals (same file) — in all twelve rows a symlinked .aguard-trash is refused with "it is a symlink", the directory outside the root is empty and the skill is untouched; with the working directory in rules/ and spelled ., it is still refused with inside a "rules" directory; green already at W1, still green after the fix
Evidence: TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget (internal/collect/targetspelling_test.go); W1 red in five rows (<abs>/., ., ./, .claude/., ..: Root empty, inventory a single directory) → all thirteen rows green after W2; reverse assertion TestCollectTarget_SingleTargetsKeepTheirSpelling (same file; skills/plain and ./skills/plain are still a single skill with Path as typed; a directory not named .claude is still one directory under five spellings) green already at W1, still green after the fix
Evidence: TestCheck_RootSpellingInsideAConfigRootIsTheAbsoluteReport, TestHashCommand_RootSpellingPrintsTheAbsoluteHashes (cmd/aguard/rawroot_test.go); W1 red in five rows each (the same five spellings as above) → all green after W2; check's JSON is byte-identical to the absolute target's, hash's stdout is identical
Evidence: TestGate_RootSpellingResolvesTheSamePlugins (same file); W1 red in eleven rows (in the nine relative rows gateOptions's Root/Home are relative and myplug:hello replies GATE-000; in the two rows <abs>/, <abs>/., Home equals root and dplug:dhello replies GATE-000) → all twelve rows green after W4, the three replies byte-identical to the absolute spelling's, all ask with EXEC-001; after the fixture moved to a short temp directory, gate.go and version.go were temporarily reverted to W3 and the run repeated: still red (20 subtests across the two tests)
Evidence: reverse assertion in the same test — outplug, whose installPath is outside HOME, is not resolved by PluginInstalls in any of the twelve rows, and outplug:x always replies GATE-000; the literal assertions on the absolute spelling's three ask replies green already at W1
Evidence: TestPluginVersionLine_RootSpelling (same file); W1 red in nine rows (relative spellings return an empty string) → after W4 all twelve rows are "plugin aguard 0.18.0 matches this binary"
Evidence: mutation (changed temporarily, run, reverted, not committed): collect.AnchorRoot only Clean without Abs → red in collect 4 tests (including P-012's three), detect 2 (TestContentHash_SameConfigTwoMachines, TestRun_RootSpellingKeepsTheBoundary), clean 1, cmd 7 (run filtered by RootSpelling|CITemplate|RelativeRootShaped; including P-010's and P-012's spelling tests); changed to EvalSymlinks(Abs(root)) → TestCollectAll_LinkedRootKeepsItsHome, TestImports_DoNotDuplicateCollectedFiles, TestRun_RootSpellingKeepsTheBoundary red; after each revert git status showed only the changes uncommitted at the time
Evidence: binary before/after (main fd28344 vs this branch, fixture under /private/tmp, HOME pointing at the fixture): after the fix, the thirteen spellings in the "Problem" table are byte-identical to the absolute spelling on clean --undo last --dry-run, clean --zombie --apply --dry-run, check --json, hash, the second version line, the two gate replies and clean --json (in the two through-a-symlink rows, check/clean --json equal the absolute spelling through the symlink); for <abs> and the absolute spelling through a symlink, these eight outputs are byte-identical before and after the fix; fixture A's absolute-spelling scan --json (without scanned_at, tool_version) is byte-identical before and after (4587 bytes); single-target check ./skills/plain, check skills/plain, and check . inside a skill directory are byte-identical before and after
Evidence: on a real machine ~/.claude (--inbox off): main and this branch under the absolute spelling both give overall 69 / artifacts 180 / findings 806 / notes 10, and the JSON (without scanned_at, tool_version) is byte-identical (15724 lines)
Evidence: Out of scope — git diff --stat origin/main -- internal/collect/hash.go internal/collect/pathsafe.go internal/collect/plugins.go internal/collect/desktop.go internal/gate internal/detect/detect.go internal/report hack docs/spec docs/rules.md go.mod go.sum is empty; existing tests git diff --stat origin/main -- internal/clean/clean_test.go internal/collect/collect_test.go internal/collect/rootspelling_test.go internal/collect/hash_test.go internal/detect/rootspelling_test.go cmd/aguard/rootspelling_test.go cmd/aguard/collectroot_test.go cmd/aguard/main_test.go cmd/aguard/gate_e2e_test.go is empty
Evidence: open question 7 measured (working directory through a symlink; the fixture gains a hook with an inline curl … | bash and hooks/deep/x/pre.sh): on main, the evidence for both --root .claude and --root <base>/via/.claude is .claude/settings.json, x/pre.sh; without the symlink both spellings give settings.json, hooks/deep/x/pre.sh — not a spelling dependency, not changed
Evidence: .claude/rules/pipeline.md 159 → 162 lines, detect.md still 200 lines; TestClaudeRulesAreScopedToExistingPaths green
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod second line go 1.23.5, module line github.com/basdotio/AgentGuard, no new dependency
```
