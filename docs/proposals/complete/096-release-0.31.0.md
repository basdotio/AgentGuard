<!-- SPDX-License-Identifier: MIT -->
# 096 — Release record v0.31.0: P-087, P-089..P-095

- **Source**: the maintainer said "发31" (2026-10-22). A release does not go through a proposal; this file is only the record.
- **Includes**: the judge's three weightings — P-087 (PR #113), P-089 (PR #115), P-090 (PR #116); the static rules —
  P-091 (PR #117), P-092 (PR #118), P-093 (PR #119), P-094 (PR #120), P-095 (PR #121). P-088 (PR #114) was rejected and
  ships nothing.
- **Branch**: `p/096-release-0.31.0`

## Why minor

New rule IDs (`FS-005`, `EXEC-013`, `SUP-007`) and new findings from existing ones on documents a `SKILL.md` names: a
scan can report more than it did, and fewer `EXFIL-001`/`-003`. No flag, exit code or JSON field was added or removed.

`rules` moves `8fc8772b2951` → `ecc901cb9f1f`; `judge-prompt` (`b851dfd5412b`) and `judge-excerpt` (9) are the same as in
v0.30.0.

## Changelog

`CHANGELOG.md`, entry **v0.31.0 (2026-10-22)**, written from each included proposal's Problem and Initial direction.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.30.0` | `0.31.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.30.0` | `0.31.0` |
| `CHANGELOG.md` | range up to v0.30.0 | range up to v0.31.0, the entry above first |
| Index rows 087, 089..095 | `PR #…` | `PR #…; v0.31.0` |
| Index: a row for this record | — | added |
| git tag `v0.31.0` | — | on the `main` commit this PR's merge produces |

## After the tag

The release workflow publishes the six npm packages and the GitHub release. Check that all six reach `0.31.0` and that
the release lists `bas-guard-*`, `aguard-*` and `SHA256SUMS.txt`.

## Done

```
Merged: PR #122 (2026-10-22; find the sha with git log --grep P-096)
Released: v0.31.0
Evidence: make verify: all gates passed on the branch
Evidence: bas-guard version on main before this branch: rules=ecc901cb9f1f judge-prompt=b851dfd5412b judge-excerpt=9
```
