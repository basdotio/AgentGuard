<!-- SPDX-License-Identifier: MIT -->
# 032 — Release record v0.20.0: P-026 to P-031

- **Source**: the maintainer said "release v0.20.0" (2026-10-10). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-026 to P-031 — every proposal in `complete/` whose `Released:` line said "pending release" (PRs #44 to
  #49, merged 2026-10-10).
- **Branch**: `p/032-release-0.20.0`

## Why minor

New user-visible capabilities, all backward compatible: the `aguard llm preview` command (P-027), and the
`prompt_version` / `excerpt_version` / `model` / `samples` fields in the JSON `judge` block plus two more fields on the
`aguard version` line, with `$2` still the version (P-031). The behaviour changes are corrections of something that used
to be missed or misreported: `--fail-on-llm` exits 4 instead of 0 when the judge could not answer (P-026), a plugin's MCP
servers listed without the `mcpServers` wrapper are scanned (P-029), the gate's copyable commands paste as printed
(P-030), and the `clean --undo` preview redacts before it cuts (P-028). Exit 4 is a new code, but it replaces a 0 that
reported a gate which never looked, the same kind of correction as `aguard approve` exiting 2 in v0.19.0. No flag, rule
or exit code was removed and no rule's severity changed, so it is not major; there are new outputs, so it is not a
patch.

## Changelog

`CHANGELOG.md`, entry **v0.20.0 (2026-10-10)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.19.0` | `0.20.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.19.0` | `0.20.0` |
| `CHANGELOG.md` | range up to v0.19.0 | range up to v0.20.0, the entry above first |
| `complete/026` … `complete/031`, `Released:` | pending release | v0.20.0 |
| Index rows 026–031 | `PR #NN; pending release` | `PR #NN; v0.20.0` |
| Index: a row for this record | — | added |
| git tag `v0.20.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-032)
Released: v0.20.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.20.0
```
