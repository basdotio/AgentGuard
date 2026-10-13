<!-- SPDX-License-Identifier: MIT -->
# 024 — A symlinked command, agent, rule, workflow or output style outside the root is disclosed but not scored

- **Category**: coverage gap (覆盖缺口)
- **Severity**: medium (中)
- **Status**: `未修复` — recorded by P-056 (open question 11, decided 2026-10-13: a follow-up, not part of P-056)

## Problem

`collect.collectTree` walks `commands/`, `agents/`, `rules/`, `workflows/` and `output-styles/` with the config root as the
boundary. A file there that is a symlink resolving outside the root is not collected; the tree gets one scan-level `COV-000`
(medium, "Entries resolve outside the scanned root, not read"). Claude Code 2.1.107's markdown loader follows symlinked files and
directories there with no containment and loads every `.md` it reaches, so the file loads while the scan only notes that it did
not look.

P-056 made the same shape for a skill's files a **scored** refusal (`SCOPE-001` on the artifact, `HOOK-002`'s reasoning: an
unread file that loads is a risk, not only a gap). These kinds still have only the note: it does not move the score, `--fail-on`
or the gate.

## Where it was measured

P-056's fixture: `commands/cmd.md`, `agents/ag.md`, `rules/rl.md` each linked to a file in `$HOME` outside `~/.claude` — three
`COV-000` notes, no artifact, no finding. The maintainer's real `~/.claude`: no such link (counts only).

## Direction

Score the refusal the way P-056 does for skills (one finding per tree naming the entries), or widen the boundary for these kinds,
and measure what `check` and the environment score do before choosing. There is no artifact to carry a finding today, so a scored
form needs a carrier (a scan-level scored finding, or a placeholder artifact like `withParseError`'s).
