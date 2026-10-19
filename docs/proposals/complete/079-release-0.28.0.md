<!-- SPDX-License-Identifier: MIT -->
# 079 — Release record v0.28.0: P-068, P-069, P-071, P-072, P-073, P-075, P-076, P-077 and P-078

- **Source**: the maintainer said "release v0.28.0" (2026-10-19). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-068 (PR #89), P-069 (PR #90), P-071 (PR #92), P-072 (PR #99), P-073 (PR #93), P-075 (PR #95), P-076
  (PR #96), P-077 (PR #98) and P-078 (PR #97) — every proposal in `complete/` merged since v0.27.0. P-066, P-070 and
  P-074 are rejected records and ship nothing.
- **Branch**: `p/079-release-0.28.0`

## Why minor

P-068 adds a judge question (the result pass, `LLM-011`), so a skill judged with `--llm` costs more calls, and P-069
re-asks an unreadable reply; `judge-prompt` moves `cc2cab18585d` → `b851dfd5412b` and `judge-excerpt` 8 → 9. P-078 changes
the request for a `base_url` that has a query string or a fragment (it never worked before). P-071, P-072 and P-076 add or
change fields of the JSON judge block and the baseline ledger; P-073 adds the opt-in `AGUARD_USAGE_LOG`. `rules`
(`8fc8772b2951`) is the same as in v0.27.0. No static rule, flag or exit code was added or removed; dependencies are
unchanged (`go.mod` and `go.sum` identical to v0.27.0).

## Changelog

`CHANGELOG.md`, entry **v0.28.0 (2026-10-19)**, written from the proposals' Problem sections rather than from commit
messages. Numbers stay in the proposals and in `baselines/results/`.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.27.0` | `0.28.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.27.0` | `0.28.0` |
| `CHANGELOG.md` | range up to v0.27.0 | range up to v0.28.0, the entry above first |
| `complete/071`, `Released:` | `pending release` | `v0.28.0` |
| `complete/068`, `069`, `072`, `073`, `075`, `076`, `077`, `078` | no `Released:` line | `Released: v0.28.0` |
| Index rows 068, 069, 071, 072, 073, 075, 076, 077, 078 | `PR #NN` (071: `PR #92; pending release`) | `PR #NN; v0.28.0` |
| Index: a row for this record | — | added |
| git tag `v0.28.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (find the sha with git log --grep P-079)
Released: v0.28.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 3 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.28.0
Evidence: aguard version at v0.27.0 vs this branch: rules 8fc8772b2951 both, judge-prompt cc2cab18585d → b851dfd5412b, judge-excerpt 8 → 9
```
