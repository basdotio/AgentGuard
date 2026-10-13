<!-- SPDX-License-Identifier: MIT -->
# 057 — Release record v0.25.0: P-056

- **Source**: the maintainer said "release v0.25.0" after P-056 merged (2026-10-13). A release does not go through a
  proposal; this file is only the record.
- **Includes**: P-056 (PR #76) — every proposal in `complete/` whose `Released:` line said it was not released yet.
- **Branch**: `p/057-release-0.25.0`

## Why minor

P-056 changes what a scan reads, scores and hashes: a skill whose SKILL.md is a symlink into the skills directory that
holds it is read by the static rules and the judge and hashed like a plain copy, where it hashed as the empty-input
digest and scored 100; `SCOPE-001` becomes a scored finding (medium) on the artifact; a SKILL.md that links to a
credential raises `EXFIL-005` and leaves the skill with no content hash. `rules` moves `d74bca61f0da` → `629837fddcba`
(the rules epoch, and `SCOPE-001`'s and `EXFIL-005`'s descriptions), `judge-excerpt` 6 → 7 (the judge reads the files
the rules read), `judge-prompt` (`cc2cab18585d`) is the same as in v0.24.0. Content hashes change for skills whose
SKILL.md is a symlink, so their approvals are asked again once; `check` can exit 1 where it exited 0. No flag or exit
code was added or removed.

## Changelog

`CHANGELOG.md`, entry **v0.25.0 (2026-10-13)**, written from the proposal's Problem section rather than from commit
messages. Numbers stay in the proposal.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.24.0` | `0.25.0` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.24.0` | `0.25.0` |
| `CHANGELOG.md` | range up to v0.24.0 | range up to v0.25.0, the entry above first |
| `complete/056`, `Released:` | pending | v0.25.0 |
| Index row 056 | `PR #76; pending release` | `PR #76; v0.25.0` |
| Index: a row for this record | — | added |
| git tag `v0.25.0` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (2026-10-13; find the sha with git log --grep P-057 after the merge)
Released: v0.25.0
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.25.0
Evidence: aguard version at v0.24.0 vs this branch: rules d74bca61f0da → 629837fddcba, judge-excerpt 6 → 7, judge-prompt cc2cab18585d both
```
