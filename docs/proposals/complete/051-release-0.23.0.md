<!-- SPDX-License-Identifier: MIT -->
# 051 — Release record v0.23.0: P-049 and P-050

- **Source**: the maintainer said "合并后你去发版" after P-050 merged (2026-10-13). A release does not go through a
  proposal; this file is only the record.
- **Includes**: P-049 (PR #68) and P-050 (PR #69) — every proposal in `complete/` whose `Released:` line said "not yet".
  P-048 (PR #67) was rejected and changed no code.
- **Branch**: `p/051-release-0.23.0`

## Why minor

P-050 changes what the judge is asked: `judge-prompt` moves `6572379d32b1` from v0.22.0's `ec4382a9226a`. P-049 changes
how a judge finding's severity is read: a hook or connector category the text does not show weighs medium, so fewer LLM
findings weigh high than in v0.22.0 and a `--fail-on-llm` gate may pass where v0.22.0 failed it. No static rule, score
formula, flag or exit code was added or removed; `rules` (`d74bca61f0da`) and `judge-excerpt` (6) are the same as in
v0.22.0.

## Changelog

`CHANGELOG.md`, entry **v0.23.0 (2026-10-13)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.22.0` | `0.23.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.22.0` | `0.23.0` |
| `CHANGELOG.md` | range up to v0.22.0 | range up to v0.23.0, the entry above first |
| `complete/049`, `complete/050`, `Released:` | not yet | v0.23.0 |
| Index rows 049, 050 | `PR #NN` | `PR #NN; v0.23.0` |
| Index: a row for this record | — | added |
| git tag `v0.23.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #70 (2026-10-13; find the sha with git log --grep P-051)
Released: v0.23.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.23.0
Evidence: aguard version at v0.22.0 vs this branch: rules d74bca61f0da both, judge-excerpt 6 both, judge-prompt ec4382a9226a → 6572379d32b1
```
