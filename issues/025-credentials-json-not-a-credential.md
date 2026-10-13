<!-- SPDX-License-Identifier: MIT -->
# 025 — `.credentials.json` is not recognised as a credential: an import of Claude Code's own credentials file is read

- **Category**: security defect (安全缺陷)
- **Severity**: medium (中)
- **Status**: `未修复` — found while P-056 was designed (2026-10-13)

## Problem

`collect.credentialFile` matches `credentials` and `credentials.json` by exact lower-cased base name, so `.credentials.json` —
the file Claude Code keeps its OAuth tokens in under the config root on Linux — is not a credential to it. `CredentialPath`
(P-056) is the same list, used for `@import` targets and for a skill's `SKILL.md` symlink.

## Reproduction

A root holding `.credentials.json` and a `CLAUDE.md` with the line `@~/.claude/.credentials.json`: `scan --json` collects
`@~/.claude/.credentials.json` as an `instruction` artifact (read, hashed, and judged under `--llm`), with no `EXFIL-005` and no
refusal note. `@~/.claude/credentials.json` is refused with `EXFIL-005` high.

## Direction

Add the dotted name to the high tier and measure the corpus for benign files it would refuse. Changing the list moves
`EXFIL-005`, so it bumps `rulesEpoch`.
