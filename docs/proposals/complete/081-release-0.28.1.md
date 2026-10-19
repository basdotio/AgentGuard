<!-- SPDX-License-Identifier: MIT -->
# 081 — Release record v0.28.1: P-080

- **Source**: the maintainer said "release v0.28.1" (2026-10-19). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-080 (PR #101), the one proposal in `complete/` merged since v0.28.0.
- **Branch**: `p/081-release-0.28.1`

## Why patch

P-080 changes when the judge draws its nonce (once per question instead of once per call) and which ready call a free
worker takes next. Nothing a user configures or invokes is new, no output field was added, and no request changed shape:
`judge-prompt` (`b851dfd5412b`) and `judge-excerpt` (9) are the same as in v0.28.0, and `aguard llm preview --json` is
byte-identical. `rules` (`8fc8772b2951`) is unchanged. No flag, rule or exit code was added or removed; dependencies are
unchanged (`go.mod` and `go.sum` identical to v0.28.0).

## Changelog

`CHANGELOG.md`, entry **v0.28.1 (2026-10-19)**, written from the proposal's Problem section rather than from commit
messages. Numbers stay in the proposal.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.28.0` | `0.28.1` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.28.0` | `0.28.1` |
| `CHANGELOG.md` | range up to v0.28.0 | range up to v0.28.1, the entry above first |
| `complete/080`, `Released:` | `vX.Y.Z (fill in after stage 5)` | `v0.28.1` |
| Index row 080 | `PR #101` | `PR #101; v0.28.1` |
| Index: a row for this record | — | added |
| git tag `v0.28.1` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #102 (2026-10-19; find the sha with git log --grep P-081)
Released: v0.28.1
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 3 unchanged; go.mod and go.sum identical to v0.28.0
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.28.1
Evidence: aguard version on this branch: rules=8fc8772b2951 judge-prompt=b851dfd5412b judge-excerpt=9, all three as in v0.28.0
```
