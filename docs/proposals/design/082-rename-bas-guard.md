<!-- SPDX-License-Identifier: MIT -->
# 082 — The tool shares its name and its command with other projects, so users find and install the wrong one

- **Source**: maintainer decision (2026-10-20) after a name search (2026-10-16 to 2026-10-20)
- **Depends on**: none
- **Branch**: `p/082-rename-bas-guard`

## Problem

"AgentGuard" and the command `aguard` are both taken by others:

- Another MIT-licensed project called AgentGuard also targets Claude Code, so a search for "AgentGuard Claude Code"
  often finds it first.
- AGENTGUARD is a registered US trademark (class 042), and another application is pending.
- The unscoped npm package `aguard` belongs to a different project and installs a command that is also called
  `aguard`. `npx aguard` runs someone else's tool. PyPI `aguard` is yet another project.

A user who reads about this tool and types the obvious install command can end up running a different program with
access to their shell. For a security tool that is the worst possible failure.

The maintainer chose the name **BAS Guard**:

| Identifier | Today | New |
|---|---|---|
| Name in prose, reports, help | AgentGuard | **BAS Guard** |
| Repository | `basdotio/AgentGuard` | **`basdotio/bas-guard`** (`basdotio/guard` is the old distribution repo and stays as it is) |
| Go module path | `github.com/basdotio/AgentGuard` | **`github.com/basdotio/bas-guard`** |
| Command | `aguard` | **`bas-guard`**, with `aguard` kept as an alias during the transition |
| npm | `@bas.io/guard`, bin `aguard` | `@bas.io/guard` unchanged, bin `bas-guard` (plus `aguard`); unscoped **`bas-guard`** published as a thin alias, so `npx bas-guard` works and the name cannot be squatted |
| Plugin | marketplace `AgentGuard`, plugin `aguard`, commands `/aguard-*`, skills `agentguard-*` | marketplace and plugin **`bas-guard`**, commands `/bas-guard-*`, skills `bas-guard-*` |

## Initial direction

Two releases. **Stage 1 (compatibility release)** teaches the current code to answer to both names and to read both
the old and new locations, without changing any identifier users already depend on. **Stage 2 (rename release)**
renames the repository, the module path, the plugin, the binary and the prose. After stage 2 the old names keep working
for the transition period (Open question Q3), and the tool says how to switch.

Some identifiers do not change at all, because changing them would cost users data or alerts: see "Out of scope".

## Done criteria

Stage 1 (compatibility release):

- [ ] A binary called `bas-guard` and the same binary called `aguard` behave identically.
  `TestCommandNameAgnostic` runs both and diffs `version`, `scan --json`, `check --json` and `hook` replies.
- [ ] The gate recognises its own entries under both names (`looksLikeOurs`, `internal/gate/status.go`).
  `TestLooksLikeOurs` covers both bare names, quoted paths with spaces, the release-asset names
  (`aguard-linux-amd64`, `bas-guard-darwin-arm64`) and the npm platform-package paths. Reverse assertion:
  `/x/guard hook`, `/x/basguard hook`, `/x/other-tool hook` and `/x/bas-guard scan` are **not** ours.
  (`hook install` matches the exact command, as it always has; telling an `aguard` registration how to switch is
  stage 2, Q4.)
- [ ] Config directory: the new directory is `$XDG_CONFIG_HOME/bas-guard`, else `~/.config/bas-guard`. When it is
  absent and the old `aguard` directory exists, config, the key file and reports are read from the old one, and
  nothing is moved without the operator. `TestConfigDirFallback` has four rows: only new, only old, both (new wins),
  neither. With both present, `llm status` names the old one as ignored (`TestLLMStatus_NamesIgnoredLegacyDir`).
- [ ] Environment: every `AGUARD_*` name the tool or its scripts read also has a `BAS_GUARD_*` form, and the new
  form wins when both are set. Today that is three names: `AGUARD_USAGE_LOG` (`cmd/aguard/usagelog.go`),
  `AGUARD_LLM_KEY` (the default key variable in `internal/config/config.go`), and `AGUARD_FAIL_ON` (`hack/pre-commit`).
  `TestEnvAliases`, `TestAPIKeyFromRenamedEnv` and `TestUsageLog_RenamedEnv` cover the Go readers. The pre-commit
  hook has no test harness, so the review package shows its three cases (neither set, old only, both set).
- [ ] On-disk names in the scan root keep their names. They are **not** renamed in this proposal (Out of scope), so
  every approval store, baseline, quarantine and settings backup already on disk is still found.
  `TestOnDiskNamesKeptAcrossRename` (gate: `.aguard-approvals.json`, `.aguard-bak`, `.aguard-bak.prev`),
  `TestTrashDirKeptAcrossRename` (collect) and `TestIgnoreFileKeptAcrossRename` (clean) pin them.
- [ ] `make verify` is green. The zero-dial tables gain no row: renaming adds no network path.

Stage 2 (rename release):

- [ ] `go.mod` declares `github.com/basdotio/bas-guard`, and every import and `hack/github-action.yml` agrees (the
  three places CLAUDE.md names). `go version` does not switch toolchains, and `go.mod` line 2 is still `go 1.23.5`.
- [ ] `cmd/bas-guard` is the main package. `make build` produces `bin/bas-guard`; `make dist` and the release produce
  `bas-guard-<os>-<arch>` **and** byte-identical `aguard-<os>-<arch>` copies for the transition. Old `latest/download/aguard-*` links in
  the wild keep working, and `SHA256SUMS.txt` lists both.
- [ ] npm: `@bas.io/guard` installs both `bas-guard` and `aguard`, and the launcher's stdio and exit-code contract is
  unchanged (its existing tests pass). The unscoped `bas-guard` package depends on `@bas.io/guard` at the same version
  and only forwards to it.
- [ ] Plugin: `.claude-plugin/marketplace.json` declares `bas-guard` with one plugin `bas-guard`. Commands and skills
  are renamed, and their text uses `bas-guard`. `version` tells an install under the old plugin name `aguard` (or the
  older `agentguard`) to switch, using the mechanism `legacyBundleName` already has (`cmd/aguard/version.go`).
  `TestMarketplaceEntryVersionMatchesPlugin` and the plugin self-scan pass.
- [ ] Prose: README pair, docs pairs, CONTRIBUTING, NOTICE, report headers (`internal/report/reader.go`,
  `markdown.go`), `--help`, the `version` line and the generated `docs/rules.md` say BAS Guard and `bas-guard`. The
  `version` line keeps its field order: the release workflow reads `$2` as the version.
- [ ] Reverse assertions: the content-hash goldens (`TestHashGolden`) are unchanged; every reputation entry still
  matches (18/18); an approval written by v0.28.x is still honoured; the SARIF `tool.driver.name` is still `aguard`, so
  existing code-scanning alerts are not closed and reopened; `judge.prompt_version` is unchanged.
- [ ] `make verify` is green on Linux CI.

## Out of scope

Each of these is checked by the review package (a grep over the diff, or a golden that did not move):

- **Content-hash domains and markers**: `aguard:hook:v1`, `aguard:mcp:v1`, `aguard:permission:v1`,
  `aguard:settings-env:v1`, `aguard:unreadable-entry`, `aguard:outside-link`. Changing them would re-key every approval
  and every reputation entry.
- **The judge's fence** `===AGUARD:<nonce>===`. It is part of `judge.prompt_version`; changing it would invalidate
  every judge cache keyed on that version.
- **On-disk file names in the scan root**: `.aguard-approvals.json`, `.aguardignore`, `.aguard-trash`,
  `settings.json.aguard-bak(.prev)`. Renaming them needs a migration that can lose approvals or misplace quarantined
  content; a later proposal may add new names and read both.
- **SARIF `tool.driver.name`** stays `aguard`. GitHub code scanning keys alerts by tool name.
- **The `baselines/` tool id** `aguard` and everything under `baselines/results/`. The corpus ledgers key on it.
- **History**: existing CHANGELOG entries, proposals, issues and release records keep the old names. New entries use
  the new ones.
- **Removing the `aguard` alias.** That is a later release, after the transition period (Q3).

## Must not claim

- Do not claim the rename changes what is detected, how scores are computed, or what is approved. It changes names
  only, and the hash and score goldens prove it.
- Do not say or imply that BAS Guard is affiliated with, or is a successor to, any other product called AgentGuard.
- Do not call the old names "deprecated" until stage 2 ships. Stage 1 only adds the new ones.

## Work items

Stage 1, one PR (proposed v0.29.0):

| W | In one sentence | Commit message |
|---|---|---|
| 1 | Tests first: both command names, gate ownership, config-dir fallback, env aliases, on-disk names kept | `test: the tool answers to bas-guard and aguard alike (P-082)` |
| 2 | Gate recognises `bas-guard` and `aguard` entries | `gate: a registration under either name is ours (P-082)` |
| 3 | Config dir `bas-guard` with fallback to `aguard`, `llm status` note, and `BAS_GUARD_*` environment names | `config: ~/.config/bas-guard and BAS_GUARD_* names, the aguard ones still read (P-082)` |
| 4 | Docs: the new directory and variable names, nothing called deprecated yet | `docs: the bas-guard directory and BAS_GUARD_ variables (P-082)` |

Stage 2, one PR (proposed v0.30.0), after the maintainer renames the repository on GitHub:

| W | In one sentence | Commit message |
|---|---|---|
| 6 | Module path `github.com/basdotio/bas-guard` everywhere, `go 1.23.5` kept | `chore: module path github.com/basdotio/bas-guard (P-082)` |
| 7 | `cmd/bas-guard`, build and release produce both binary names | `build: bas-guard binaries, aguard copies for the transition (P-082)` |
| 8 | npm launcher installs both bins; unscoped `bas-guard` alias package | `npm: bas-guard bin and an unscoped bas-guard alias package (P-082)` |
| 9 | Plugin, marketplace, commands and skills renamed; legacy-name hint extended | `plugin: bas-guard, and a hint for installs under aguard (P-082)` |
| 10 | Prose, report headers, help, version line, generated rules doc | `docs: BAS Guard (P-082)` |
| 11 | Release record and CHANGELOG with the migration steps | `proposals: P-082 release record (P-082)` |

## Open questions

1. **Two PRs and two releases, or one?** **Decided (2026-10-20)**: two, as recommended. Recommended: two. Stage 1 can ship before the GitHub rename and is fully
   reversible. Stage 2 needs the rename to have happened (the module path must equal the repository path).
2. **Copyright holder** in LICENSE and NOTICE: "AgentGuard authors" → "BAS Guard authors"? **Decided (2026-10-20)**: yes, in stage 2, as recommended. Recommended: yes, as
   "BAS Guard authors (formerly AgentGuard authors)". A holder name is not a licence change.
3. **How long do the old names last?** **Decided (2026-10-20)**: as recommended. Recommended: the `aguard` alias, the `aguard-*` release assets, the old config
   directory and the `AGUARD_*` variables stay for at least two minor releases **and** three months after stage 2,
   whichever is later. Removal is its own proposal with a release note one version ahead.
4. **Should `hook install` move existing registrations to the new command automatically?** **Decided (2026-10-20)**: no, as recommended; the `hook status` line ships in stage 2. Recommended: no. The gate
   keeps working through the alias. `hook status` adds one line ("registered as aguard; `bas-guard hook install`
   switches it"), and the operator decides.
5. **On-disk names** (`.aguard-*`): leave them for good, or plan new names later? **Decided (2026-10-20)**: leave them, as recommended. Recommended: leave them. They are
   implementation details in the operator's own directory, and renaming them buys nothing but migration risk.

## Done

```
Merged: (stage 1) PR #104 (2026-10-20) · (stage 2) …
Released: (stage 1) v0.29.0 · (stage 2) …
Evidence: …
```
