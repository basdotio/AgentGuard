<!-- SPDX-License-Identifier: MIT -->
# 030 — For a skill whose path is longer than 160 characters, the command the gate prints to copy fails when copied: the path in it is the shortened display form

- **Source**: new finding, found while P-019 was implemented (its fixture had to move to a short temporary directory
  because the gate clips paths longer than 160 characters); the same class as P-008 (a command printed for copying that
  carries a display-only shortening)
- **Depends on**: none
- **Branch**: `p/030-gate-command-not-truncated`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`gate.Summarize` stores the artifact path already clipped for display: `Path: clip(a.Path, maxPathLen)`
(`internal/gate/gate.go`), 160 runes and a `…`. Every command the gate builds from the verdict then takes that clipped
value as its argument:

- `Verdict.Reason()` — the text of an ask/deny decision: `Full report: aguard check %q · trust these exact bytes:
  aguard approve %q`
- `Verdict.UnrememberedLine()` — a pass that is not remembered: `… or you accept it with: aguard approve %q`
- `handlePre` in an auto-answering permission mode: `To load it anyway, decide outside the session: aguard approve %q`
- `approvePath` (`cmd/aguard/gate.go`), after accepting a risk from the terminal: ``Run `aguard check %q` ``

For a skill whose path is longer than 160 characters, each of these commands names a path that does not exist, so
pasting it fails (`check` and `approve` refuse a target that cannot be read). Paths that long are ordinary: a skill
installed through the desktop app sits under `~/Library/Application Support/Claude/local-agent-mode-sessions/<uuid>/<uuid>/…`;
on the machine this was measured on, all 41 such skill directories are between 170 and 235 characters long.
The same commands also quote the path with Go's `%q`, which is not shell quoting: inside double quotes a POSIX shell still
expands `$`, backticks and `\`, so a path containing those characters is changed by the shell before `aguard` sees it.

Consequence: the gate prints a command to copy that fails when copied, for long paths. The operator who wants the full
report or wants to accept the risk is handed a broken instruction at exactly the moment the gate stopped a load.

## Initial direction

Keep the clipped path for the text a human reads, and give every copyable command the full path, quoted for a POSIX
shell. Keep the gate's existing bounds on what a message may contain (no evidence snippets, length not sized by the
artifact, control and bidi characters never printed raw).
