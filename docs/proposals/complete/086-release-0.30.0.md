<!-- SPDX-License-Identifier: MIT -->
# 086 — Release record v0.30.0: P-082 stage 2 and P-085

- **Source**: P-082 stage 2 merged (PR #106, 2026-10-21); the tag waits for the maintainer's word. A release does not go
  through a proposal; this file is only the record.
- **Includes**:
  - P-082 stage 2, the rename to BAS Guard: module path, binary, npm, plugin, prose (PR #106);
  - its follow-up, found while preparing this record (PR #110): the npm platform packages keep the binary under its old
    name, the SARIF keys keep their names, and the old-name texts say what actually happens;
  - P-085, the judge's notes name an artifact as the report does (PR #109).

  P-084 (PR #107) is not merged and not included.
- **Branch**: `p/086-release-0.30.0`

## Why minor

The command, the module path, the repository and the plugin get new names. What a user set up keeps working through
the old names: the `aguard` command (npm installs both, and each platform package carries the binary as `aguard` too),
the `aguard-*` release assets, `~/.config/aguard` and the `AGUARD_*` variables. Two things do not, and the changelog
says so with the fix: an install of the plugin under `aguard` stops loading once its marketplace refreshes, and
`go install` of the old module path fails. Before 1.0 that is a minor release.

`rules` (`8fc8772b2951`), `judge-prompt` (`b851dfd5412b`) and `judge-excerpt` (9) are the same as in v0.29.0; no flag,
rule, exit code or JSON field was added or removed. Against v0.29.0, `go.mod` differs in its module line only and
`go.sum` is identical.

## Changelog

`CHANGELOG.md`, entry **v0.30.0 (2026-10-21)**, written from P-082's Problem section, its stage-2 criteria and the
follow-up's findings, and from P-085's Problem section. It ends with the steps to switch.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.29.0` | `0.30.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.29.0` | `0.30.0` |
| `CHANGELOG.md` | range up to v0.29.0 | range up to v0.30.0, the entry above first |
| `complete/082`, `Released:` | `(stage 2) v0.30.0 (fill in after the release)` | `(stage 2) v0.30.0` |
| `complete/085`, `Released:` | `vX.Y.Z (fill in after stage 5)` | `v0.30.0` |
| Index row 082 | `… PR #106 (stage 2), PR #110` | `… PR #106 (stage 2), PR #110; v0.30.0` |
| Index row 085 | `PR #109` | `PR #109; v0.30.0` |
| Index: a row for this record | — | added |
| git tag `v0.30.0` | — | on the `main` commit this PR's merge produces |

## After the tag

The release workflow publishes six npm packages for the first time from the renamed repository: the four platform
packages, `@bas.io/guard`, then `bas-guard`. Each trusts `basdotio/bas-guard` and `release.yml` and has not published
through it yet. Check that all six reach `0.30.0` and that the GitHub release lists `bas-guard-*`, `aguard-*` and
`SHA256SUMS.txt`.

## Done

```
Merged: PR #111 (2026-10-21; find the sha with git log --grep P-086)
Released: v0.30.0
Evidence: make verify: all gates passed on the branch; go1.23.5, no toolchain switch; against v0.29.0 go.mod differs in the module line only, go.sum identical
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.30.0
Evidence: bas-guard version on this branch: rules=8fc8772b2951 judge-prompt=b851dfd5412b judge-excerpt=9, all three as in v0.29.0
```
