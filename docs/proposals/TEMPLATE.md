<!-- SPDX-License-Identifier: MIT -->
# NNN — <one-line title, stated as the consequence>

- **Source**: issues/NNN · or "new finding" · or the date of an audit/conversation
- **Depends on**: none · or P-NNN
- **Branch**: `p/NNN-slug` (fill in once stage 2 has created it)

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

<!-- ===== draft part: in draft/, write only up to here. Once the maintainer says it is worth designing, git mv it to design/ and fill in the rest below. ===== -->

## Problem

Who is affected, what the current behaviour is, and what is wrong with it. Describe the symptoms and the consequences,
not the fix.

## Initial direction

One or two sentences: roughly how to do it and what it will touch. This is for the maintainer to judge "is it worth
designing"; it is not a design.

<!-- ===== design part: fill in after moving to design/. Ask all open questions at once; once the maintainer has answered, it is accepted. ===== -->

## Done criteria

At least two, each specific enough for an AI to verify on its own: which command, which test, which number. For a code
proposal, one of them must be a reverse assertion (after the fix, what should still fire still fires).

- [ ] `TestXxx` pins: …
- [ ] Reverse assertion: … still fires
- [ ] `make verify` green

## Out of scope

The scope boundary. Leave it out and the AI will certainly overstep. For each item, the review package must be able to
show proof that it was indeed not touched.

## Must not claim

This repository's disclosure discipline: which words must not appear in reports/docs, and which conclusions must not be
drawn. If it does not apply, write "not applicable" and say why.

## Work items

One commit per row:

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | | |

## Open questions

Ask them all at once during design, each with a recommended answer; questions that come up during stage 2 are appended
after them. When the maintainer answers, write "**Decided (date)**: …"; do not delete the question.

## Done

Fill in at stage 4, when the maintainer says ship it and the file is `git mv`ed to `complete/` (in the PR, not after
the merge):

```
Merged: PR <number or link> (<date>; find the sha with git log --grep P-NNN after the merge)
Released: vX.Y.Z (fill in after stage 5)
Evidence: <test name> (<file>); <number before → after = explanation>; <line holding the reverse assertion>
```
