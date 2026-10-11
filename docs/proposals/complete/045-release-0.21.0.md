<!-- SPDX-License-Identifier: MIT -->
# 045 — Release record v0.21.0: P-036 to P-040 and P-042 to P-044

- **Source**: the maintainer said "release v0.21.0" (2026-10-11). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-036 (PR #55), P-037 (PR #58), P-038 (PR #57), P-039 (PR #56), P-040 (PR #59), P-043 (PR #60),
  P-044 (PR #61) and P-042 (PR #62) — every proposal in `complete/` whose `Released:` line said "pending release". P-041
  is not merged and is not part of this release.
- **Branch**: `p/045-release-0.21.0`

## Why minor

P-044 changes what a scan collects: a plugin's loadable skills, commands and agents become artifacts of their own, the
environment score becomes an average of units, and the JSON report gains `plugin` on each child and `bundled_commands` /
`bundled_agents` in the summary (additive; nothing existing changes shape). P-038 adds a case to exit 4 under
`--fail-on-llm`. P-036, P-037, P-039, P-040, P-042 and P-043 change what is redacted, so what the judge is shown
(`judge-excerpt` 1 → 6), some snippets and some content hashes change. `rules` on the `aguard version` line moves with
P-044's rules epoch. No flag or rule was added or removed and no rule's severity changed; `judge-prompt` is the same as in
v0.20.1.

## Changelog

`CHANGELOG.md`, entry **v0.21.0 (2026-10-11)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.20.1` | `0.21.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.20.1` | `0.21.0` |
| `CHANGELOG.md` | range up to v0.20.1 | range up to v0.21.0, the entry above first |
| `complete/036` … `complete/040`, `complete/042` … `complete/044`, `Released:` | pending release | v0.21.0 |
| Index rows 036–040, 042–044 | `PR #NN; pending release` | `PR #NN; v0.21.0` |
| Index: a row for this record | — | added |
| git tag `v0.21.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (2026-10-11; find the sha with git log --grep P-045 after the merge)
Released: v0.21.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.21.0
```
