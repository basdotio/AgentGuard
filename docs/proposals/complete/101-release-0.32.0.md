<!-- SPDX-License-Identifier: MIT -->
# 101 — Release record v0.32.0: P-098, P-099

- **Source**: the maintainer said "发" (2026-10-23). A release does not go through a proposal; this file is only the record.
- **Includes**: P-098 (PR #124), P-099 (PR #125). Since v0.31.0 also merged and shipping nothing executable: P-097
  (PR #123) and P-100 (PR #126), measurement records; `issues/028`, a rejection record.
- **Branch**: `p/101-release-0.32.0`

## Why minor

Findings change: the judge's server pass weighs some `credential-read` findings medium, and `EXFIL-001`/`-003` fire on
fewer files. No flag, exit code or JSON field was added or removed.

`rules` moves `ecc901cb9f1f` → `717ada4c680b`; `judge-prompt` (`b851dfd5412b`) and `judge-excerpt` (9) are the same as in
v0.31.0.

## Changelog

`CHANGELOG.md`, entry **v0.32.0 (2026-10-23)**, written from P-098's and P-099's Problem and Initial direction.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.31.0` | `0.32.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.31.0` | `0.32.0` |
| `CHANGELOG.md` | range up to v0.31.0 | range up to v0.32.0, the entry above first |
| Index rows 098, 099 | `PR #…` | `PR #…; v0.32.0` |
| Index: a row for this record | — | added |
| git tag `v0.32.0` | — | on the `main` commit this PR's merge produces |

## After the tag

The release workflow publishes the six npm packages and the GitHub release. Check that all six reach `0.32.0` and that
the release lists `bas-guard-*`, `aguard-*` and `SHA256SUMS.txt`.

## Done

```
Merged: PR #127 (2026-10-23; find the sha with git log --grep P-101)
Released: v0.32.0
Evidence: make verify: all gates passed on the branch
Evidence: bas-guard version on main before this branch: rules=717ada4c680b judge-prompt=b851dfd5412b judge-excerpt=9
```
