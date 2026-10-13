<!-- SPDX-License-Identifier: MIT -->
# 026 — `clean` quarantines a skill whose SKILL.md is a relative symlink, and then refuses to undo it

- **Category**: functional defect (功能缺陷)
- **Severity**: medium (中)
- **Status**: `未修复` — found while P-056 was designed (2026-10-13); present before and after P-056

## Problem

`clean --resolve <id> --keep <side>` moves the losing skill directory into `<root>/.aguard-trash/`. When that directory's
`SKILL.md` is a relative symlink (`skills/y/SKILL.md` → `../lib/y/SKILL.md`, the per-skill install layout P-056 measured), the
move breaks the link: from the trash, `../lib/y/SKILL.md` names nothing. `clean --undo` re-hashes the quarantined copy, finds a
different hash and refuses with "quarantined content changed since it was moved", so the move is not reversible, which is the
promise `clean` makes.

Before P-056 the recorded hash was the empty-input digest and the trash copy hashed with the unreadable mark; after P-056 the
recorded hash is the content hash and the trash copy has no hash (its `SKILL.md` is refused). Both differ, so undo fails either way.

## Reproduction

Two skills `x` and `y`, each a directory holding only a `SKILL.md` symlink to `skills/lib/<name>/SKILL.md` with identical
descriptions (a `duplicate_fn` pair): `clean --resolve <id> --keep x` quarantines `y`; `clean --undo last` prints
"refuse y: quarantined content changed since it was moved".

## Direction

Refuse the move up front when the source tree holds a relative symlink that resolves outside it (the move cannot preserve what
it points at), with a blocker the plan shows (`issues/019`'s lesson: "cannot be done" must be visible before `--apply`), or hash
the quarantined entry against its original location. Measure first.
