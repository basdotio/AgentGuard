<!-- SPDX-License-Identifier: MIT -->
# 083 — Release record v0.29.0: P-082 stage 1

- **Source**: the maintainer said "release v0.29.0" (2026-10-20). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-082 stage 1, the compatibility half of the rename to BAS Guard (PR #104). P-082 itself stays in
  `design/` until stage 2 ships; PR #103 (issues/027) touched only `issues/`.
- **Branch**: `p/083-release-0.29.0`

## Why minor

P-082 stage 1 adds names a user can configure: the config directory `~/.config/bas-guard` for a first setup and three
`BAS_GUARD_*` environment variables, and the gate recognises a registration under `bas-guard`. Nothing that exists is
removed or renamed: the binary is still `aguard`, `~/.config/aguard` is still read when it is the only directory, every
`AGUARD_*` variable still works, and the names on disk (`.aguard-approvals.json`, `.aguardignore`, `.aguard-trash`, the
settings backups) are unchanged. `rules` (`8fc8772b2951`), `judge-prompt` (`b851dfd5412b`) and `judge-excerpt` (9) are the
same as in v0.28.1; no flag, rule, exit code or JSON field was added or removed; `go.mod` and `go.sum` are identical to
v0.28.1.

## Changelog

`CHANGELOG.md`, entry **v0.29.0 (2026-10-20)**, written from P-082's Problem section and stage-1 criteria.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.28.1` | `0.29.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.28.1` | `0.29.0` |
| `CHANGELOG.md` | range up to v0.28.1 | range up to v0.29.0, the entry above first |
| `design/082`, `Released:` | — | stage 1 in v0.29.0 |
| Index row 082 | `PR #104 (stage 1)` | `PR #104 (stage 1); v0.29.0` |
| Index: a row for this record | — | added |
| git tag `v0.29.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #105 (2026-10-20; find the sha with git log --grep P-083)
Released: v0.29.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod and go.sum identical to v0.28.1
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.29.0
Evidence: aguard version on this branch: rules=8fc8772b2951 judge-prompt=b851dfd5412b judge-excerpt=9, all three as in v0.28.1
```
