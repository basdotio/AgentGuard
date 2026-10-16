<!-- SPDX-License-Identifier: MIT -->
# 067 — Release record v0.27.0: P-058, P-064 and P-065

- **Source**: the maintainer said "release v0.27.0" after P-058 merged (2026-10-16). A release does not go through a
  proposal; this file is only the record.
- **Includes**: P-058 (PR #78), P-064 (PR #86) and P-065 (PR #87) — every proposal in `complete/` whose `Released:` line
  said it was not released yet. P-066 is an open branch and is not included.
- **Branch**: `p/067-release-0.27.0`

## Why minor

P-058 changes the default terminal report's content and order (every finding still printed; `--verbose`, JSON, SARIF and
HTML unchanged) and the `aguard llm setup` text. P-065 changes what the judge is shown: `judge-excerpt` moves 7 → 8, and a
skill that names companion documents sends them to the judge, which adds judge calls. P-064 is measurement (corpus and
results). `rules` (`8fc8772b2951`) and `judge-prompt` (`cc2cab18585d`) are the same as in v0.26.0. No rule, flag or exit
code was added or removed; dependencies are unchanged.

## Changelog

`CHANGELOG.md`, entry **v0.27.0 (2026-10-16)**, written from the proposals' Problem sections rather than from commit
messages. Numbers stay in the proposals and in `baselines/results/`.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.26.0` | `0.27.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.26.0` | `0.27.0` |
| `CHANGELOG.md` | range up to v0.26.0 | range up to v0.27.0, the entry above first |
| `complete/058`, `Released:` | (fill in after stage 5) | v0.27.0 |
| `complete/064`, `complete/065` | no `Released:` line | `Released: v0.27.0` |
| Index rows 058, 064, 065 | `PR #78; pending release`, `PR (open)`, `PR #87` | `PR #NN; v0.27.0` |
| Index: a row for this record | — | added |
| git tag `v0.27.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (2026-10-16; find the sha with git log --grep P-067 after the merge)
Released: v0.27.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 3 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.27.0
Evidence: aguard version at v0.26.0 vs this branch: rules 8fc8772b2951 both, judge-prompt cc2cab18585d both, judge-excerpt 7 → 8
```
