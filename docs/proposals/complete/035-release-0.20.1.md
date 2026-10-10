<!-- SPDX-License-Identifier: MIT -->
# 035 — Release record v0.20.1: P-033 and P-034

- **Source**: the maintainer said "release v0.20.1" (2026-10-10). A release does not go through a proposal; this file is
  only the record.
- **Includes**: P-033 (PR #52) and P-034 (PR #53), every proposal in `complete/` whose `Released:` line said "pending
  release", plus the correction of the four 2026-09 judge runs' `raw_archive:` text (PR #51).
- **Branch**: `p/035-release-0.20.1`

## Why patch

P-034 is a bug fix: a judge reply that closes its object one member early used to fail the call, and one that also lost
its last brace was read silently as its first half. The one new output, the `repaired` count in the JSON `judge` block,
on the report's judge line and in a benchmark ledger row, only reports that fix at work; nothing a user configures or
invokes is new, and nothing sent to the judge changed (`judge-prompt` and `judge-excerpt` are the same as in v0.20.0).
P-033 adds a command under `baselines/`, which is benchmark tooling: it is not built into the `aguard` binary and is not
in the npm packages. PR #51 changes text only. No flag, rule or exit code was added or removed and no rule's severity
changed.

## Changelog

`CHANGELOG.md`, entry **v0.20.1 (2026-10-10)**, written from each proposal's Problem section rather than from commit
messages. Numbers stay in the proposals.

## What changed where

| Place | Before | After |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.20.0` | `0.20.1` |
| `.claude-plugin/marketplace.json` plugin entry `version` | `0.20.0` | `0.20.1` |
| `CHANGELOG.md` | range up to v0.20.0 | range up to v0.20.1, the entry above first |
| `complete/033`, `complete/034`, `Released:` | pending release | v0.20.1 |
| Index rows 033–034 | `PR #NN; pending release` | `PR #NN; v0.20.1` |
| Index: a row for this record | — | added |
| git tag `v0.20.1` | — | on the `main` commit this PR's merge produces |

## Done

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-035)
Released: v0.20.1
Evidence: make verify: all gates passed on the branch; go1.23.5, go.mod line 2 unchanged
Evidence: TestMarketplaceEntryVersionMatchesPlugin green with both files at 0.20.1
```
