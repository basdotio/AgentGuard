<!-- SPDX-License-Identifier: MIT -->
# 063 — Release record v0.26.0: P-059, P-060, P-061 and P-062

- **Source**: the maintainer said "发26" after P-062 merged (2026-10-16). A release does not go through a proposal; this
  file is only the record.
- **Includes**: P-059 (PR #80), P-060 (PR #81), P-061 (PR #83), P-062 (PR #84) — every proposal in `complete/` whose
  `Released:` line said it was not released yet.
- **Branch**: `p/063-release-0.26.0`

## Why minor

P-061 changes what a deterministic finding says: `EXFIL-001` drops to low + advisory where a named credential's every
destination is its own service after one level of same-file resolution, where it was high; `rules` moves `629837fddcba`
→ `8fc8772b2951` (the rules epoch and the rule's description). P-062 changes a judge finding's severity on one pass and
category; `judge-prompt` (`cc2cab18585d`) and `judge-excerpt` (7) are the same as in v0.25.0. P-059 and P-060 are
measurement: the baselines driver, result directories, the tool registry. Content hashes do not change; `check` can exit
0 where it exited 1 on a script that reads its own service's key. No flag or exit code was added or removed in the
scanner; the driver gained `-aguard-check-llm` and judgefold `-check-llm`.

## Changelog

`CHANGELOG.md`, entry **v0.26.0 (2026-10-16)**, written from the proposals' Problem sections rather than from commit
messages. Numbers stay in the proposals and in `baselines/results/`.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.25.0` | `0.26.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.25.0` | `0.26.0` |
| `CHANGELOG.md` | range up to v0.25.0 | range up to v0.26.0, the entry above first |
| `complete/059` … `complete/062`, `Done` | no release line | `Released: v0.26.0` |
| Index rows 059–062 | PR only | `; v0.26.0` |
| Index: a row for this record | — | added |
| git tag `v0.26.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR #TBD (2026-10-16; find the sha with git log --grep P-063)
Released: v0.26.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.26.0
Evidence: aguard version at v0.25.0 vs this branch: rules 629837fddcba → 8fc8772b2951, judge-prompt cc2cab18585d and judge-excerpt 7 both
```
