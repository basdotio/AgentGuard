<!-- SPDX-License-Identifier: MIT -->
# 055 — Release record v0.24.0: P-052, P-053 and P-054

- **Source**: the maintainer said "发吧 你来" after P-054 merged (2026-10-13). A release does not go through a proposal;
  this file is only the record.
- **Includes**: P-052 (PR #72), P-053 (PR #73) and P-054 (PR #74) — every proposal in `complete/` whose `Released:`
  line said "not yet".
- **Branch**: `p/055-release-0.24.0`

## Why minor

P-053 adds an artifact kind (`server`) and a judge rule (`LLM-010`): `check <dir>` on a Python MCP server reports kind
`server` where it reported `directory`, and `check <server> --llm --fail-on-llm` answers 1 or 0 where it answered 4.
`judge-prompt` moves `6572379d32b1` → `cc2cab18585d` (the new pass's shapes); `rules` (`d74bca61f0da`) and
`judge-excerpt` (6) are the same as in v0.23.0. P-052 lets a model that refuses the temperature member be the judge
and adds `temperature_omitted` to the judge block. P-054 changes nothing a user configures. No static rule, score
formula, flag or exit code was added or removed.

## Changelog

`CHANGELOG.md`, entry **v0.24.0 (2026-10-13)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.23.0` | `0.24.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.23.0` | `0.24.0` |
| `CHANGELOG.md` | range up to v0.23.0 | range up to v0.24.0, the entry above first |
| `complete/052`, `complete/053`, `complete/054`, `Released:` | not yet | v0.24.0 |
| Index rows 052–054 | `PR #NN` | `PR #NN; v0.24.0` |
| Index: a row for this record | — | added |
| git tag `v0.24.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #75 (2026-10-13; find the sha with git log --grep P-055)
Released: v0.24.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.24.0
Evidence: aguard version at v0.23.0 vs this branch: rules d74bca61f0da both, judge-excerpt 6 both, judge-prompt 6572379d32b1 → cc2cab18585d
```
