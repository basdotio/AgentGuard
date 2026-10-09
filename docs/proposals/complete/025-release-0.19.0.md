<!-- SPDX-License-Identifier: MIT -->
# 025 — Release record v0.19.0: P-001 to P-024

- **Source**: the maintainer said "release v0.19.0" (2026-10-09). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-001 to P-024 — every proposal in `complete/` whose `Released:` line said "pending release", plus the
  translation of the proposals into English (PR #42).
- **Branch**: `p/025-release-0.19.0`

## Why minor

New user-visible capabilities, all backward compatible: `check --llm` (P-004), the `rules_version` field and the extra
field on the `aguard version` line (P-002), the judge usage fields in the JSON `judge` block (P-001). Behaviour changes are
corrections of something that used to be missed or misreported (plugin MCP servers now scanned, P-021; relative root
spellings, P-010 / P-012 / P-019; headline hedging, P-013 / P-017; frontmatter labels, P-024). `aguard approve` exiting 2 for
content without a hash (P-011) replaces a success message that stored nothing. No flag, rule or exit code was removed and
no rule's severity changed, so it is not major; there are new outputs, so it is not a patch.

## Changelog

`CHANGELOG.md`, entry **v0.19.0 (2026-10-09)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals and in `baselines/results/`.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.18.0` | `0.19.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.18.0` | `0.19.0` |
| `CHANGELOG.md` | range up to v0.18.0 | range up to v0.19.0, the entry above first |
| `complete/001` … `complete/024`, `Released:` | pending release | v0.19.0 |
| Index rows 001–024 | `PR #NN; pending release` | `PR #NN; v0.19.0` |
| Index: a row for this record | — | added |
| git tag `v0.19.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #43 (2026-10-09; find the sha with git log --grep P-025)
Released: v0.19.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.19.0
```
