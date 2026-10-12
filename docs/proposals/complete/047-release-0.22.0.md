<!-- SPDX-License-Identifier: MIT -->
# 047 — Release record v0.22.0: P-041 and P-046

- **Source**: the maintainer said "release v0.22.0" (2026-10-12). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-041 (PR #64) and P-046 (PR #65) — every proposal in `complete/` whose `Released:` line said "not yet".
- **Branch**: `p/047-release-0.22.0`

## Why minor

P-041 changes what the judge is asked and how a judge finding's severity is set: `judge-prompt` moves
`e863a9dc881c` → `ec4382a9226a`, the vote text gains `[categories: …] [tool: …]`, and the severities of `LLM-001`,
`LLM-003`, `LLM-004`, `LLM-006` and `LLM-008` findings come from a table rather than the model's word. P-046 refuses a
configuration that was accepted before: `--fail-on-llm` with `llm.samples` below 3 exits 2. No static rule, score
formula, flag or exit code was added or removed; `rules` (`d74bca61f0da`) and `judge-excerpt` (6) are the same as in
v0.21.0. The refusal is a behaviour change for a pipeline that gated on the judge at `samples: 1`; the changelog entry
says what to set.

## Changelog

`CHANGELOG.md`, entry **v0.22.0 (2026-10-12)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.21.0` | `0.22.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.21.0` | `0.22.0` |
| `CHANGELOG.md` | range up to v0.21.0 | range up to v0.22.0, the entry above first |
| `complete/041`, `complete/046`, `Released:` | not yet | v0.22.0 |
| Index rows 041, 046 | `PR #NN` | `PR #NN; v0.22.0` |
| Index: a row for this record | — | added |
| git tag `v0.22.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #66 (2026-10-12; find the sha with git log --grep P-047)
Released: v0.22.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.22.0
Evidence: aguard version at v0.21.0 vs this branch: rules d74bca61f0da both, judge-excerpt 6 both, judge-prompt e863a9dc881c → ec4382a9226a
```
