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

Measured with a binary built from `main` (`155865b`) against a temporary root:

```
skill directory of 212 characters, one EXEC-001 (curl … | bash), PreToolUse[Skill] → ask
  the reason's "aguard check …" argument, expanded by sh: the first 160 characters of the path and "…"
  aguard check <that argument>  → error: stat <W>/home/.claude/skills/desktop-skill-xxx…: no such file   exit 2
skill directory named x$(touch pwned), aguard approve <full path> → the note prints
  aguard check "<W>/home/.claude/skills/x$(touch pwned)"
  pasted into sh in an empty directory → the argument is "<W>/home/.claude/skills/x", and a file named pwned appears
```

Consequence: the gate prints a command to copy that fails when copied, for long paths; and for a path holding shell
syntax, pasting it runs that syntax. The operator who wants the full
report or wants to accept the risk is handed a broken instruction at exactly the moment the gate stopped a load.

## Initial direction

Keep the clipped path for the text a human reads, and give every copyable command the full path, quoted for a POSIX
shell. Keep the gate's existing bounds on what a message may contain (no evidence snippets, length not sized by the
artifact, control and bidi characters never printed raw).

## Done criteria

- [x] `TestGateCommandsPasteAsPrinted` (`cmd/aguard/gate_paste_test.go`, new): two real fixture skills — one whose
  directory path is longer than 160 runes, one whose path holds shell syntax (a space, `'`, `"`, `$HOME`, `$(touch
  pwned)`, a backtick, `\`, `!`) — each with one high finding. For every copyable command the gate and `approve` print
  (`Reason()` under `default`: `aguard check` and `aguard approve`; the deny under `auto`: `aguard approve`;
  `UnrememberedLine()`: `aguard approve`; `approvePath`'s note: `aguard check`), the argument as printed is expanded by
  a real `/bin/sh` in an empty working directory and must be exactly one word equal to the full skill path; the real
  command function (`checkTarget` / `approvePath`) then succeeds on it; the working directory stays empty. Red on `main`
  for the reason stated in the Problem: the long path is the clipped one (`stat …: no such file`), and the shell-syntax
  path is changed by the shell and creates `pwned`
- [x] `TestApproveNoHashHintPastesAsPrinted` (same file, added with open question 4): `approve`'s refusal for a root
  with no content hash prints `aguard check <target> --json`; pasted the same way it is one word equal to the target, the
  real `checkTarget` runs on it, nothing is created. Red before W3: `pwned` and `pwned2` appear and `$HOME` is expanded
- [x] `TestShellQuoteRoundTripsThroughAShell` / `TestShellQuoteNeverPrintsAnInvisibleCharacter`
  (`internal/gate/command_test.go`, new with the fix): the quoting round-trips through `/bin/sh` for printable paths,
  and through `bash` and `zsh` (skipped when absent) for paths holding control, bidi and zero-width characters and
  bytes that are not UTF-8; no quoted form holds a raw control or invisible character (invariant #7)
- [x] Reverse assertion: for an ordinary path (printable, no `"` `\` `$` `` ` `` `!`), the command argument is
  byte-identical to today's `%q` form — pinned by a literal `Reason()` / `UnrememberedLine()`
  (`TestCommandsForAnOrdinaryPathAreUnchanged`) and the deny reply (`TestDenyCommandForAnOrdinaryPathIsUnchanged`) for
  a short path, green on `main` and after the fix, and by `shellQuote(p) == fmt.Sprintf("%q", p)` over a table of such
  paths (`TestShellQuoteIsPercentQForOrdinaryText`)
- [x] Reverse assertion: the display stays shortened where it is display only — for the long fixture, `Verdict.Path`,
  the path line of `Reason()`, and the `Path` recorded in the approvals store remain the 160-rune clip plus `…`
  (`TestGateDisplayStaysClipped`, `cmd/aguard/gate_paste_test.go`)
- [x] Bound: a command argument longer than 4096 bytes once quoted is not printed; a placeholder names its length
  instead (`TestCommandArgBound`). `TestAttackerControlledNameIsSanitizedAndBounded` and
  `TestReasonCarriesNoEvidenceSnippets` stay green unchanged, and a 100 000-byte path keeps `Reason()` and
  `UnrememberedLine()` under 4000 bytes (`TestCommandsCannotSizeTheMessage`)
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency

## Out of scope

- The 160-rune display clip, `Verdict.Path`, the `Path` stored in approvals and pending entries, and the approvals file
  format do not change
- The path line of `Reason()` prints the clipped path without `report.Sanitize`, so a control or bidi character in a
  path reaches it raw (measured here: an ESC and a U+202E survive into `Reason()`); a separate follow-up, not fixed here
- Names (the 80-rune clip): no command carries a name
- SessionStart and `AdditionalContext`: no command carries a path there; the nonce fence is unchanged
- `aguard approvals`, `aguard hook status`, the `clean` undo hint (its batch ID is generated by the tool, not a path):
  measured, none puts a shortened string into a command
- Shells outside the POSIX family (fish, PowerShell, cmd.exe), and a relative target that starts with `-`

## Must not claim

- Do not say the commands paste into every shell: the double- and single-quoted forms are POSIX; the `$'…'` form used
  only for paths holding control or invisible characters needs bash, zsh, ksh or a shell implementing POSIX.1-2024
- Do not say anyone pasted a hostile command, or that this was exploited: the Problem is a measured shape on a fixture
- Do not say the display path is complete or sanitized now: the display is unchanged on purpose (see Out of scope)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The paste test and the short-path pins, run red | `cmd: tests — the commands the gate prints to copy fail for a long path and run shell syntax in a path (P-030)` |
| 2 | The gate's commands carry the full path, quoted for a POSIX shell and bounded; the display stays clipped | `gate: a copyable command carries the full path, quoted for a POSIX shell (P-030)` |
| 3 | The no-hash refusal's paste test, run red; then `approve`'s two notes use the same quoting (two commits) | `cmd: test — approve's no-hash refusal quotes its check command with %q, and a shell runs what the path holds (P-030)` · `cmd: approve's notes print commands that paste as printed (P-030)` |
| 4 | The install-gate pair and the gate rules say what a copyable command carries | `docs: install-gate says the gate's commands carry the full path, quoted for a shell (P-030)` |
| 5 | This file, the index | `proposals: P-030 (P-030)` |

## Open questions

1. **How is a path holding a control or invisible character quoted?**
   **Recommendation**: `$'…'` with every byte outside a conservative safe set written as a three-digit octal escape.
   Printing those characters raw would break invariant #7; Go's `%q` escapes (`\u202e`) are not shell syntax and fail
   today as well; a placeholder would leave the operator without a command for exactly the paths most worth checking.
   Three-digit octal never absorbs a following digit, unlike `\x`. This form needs bash, zsh, ksh or a POSIX.1-2024 shell.
   **Decided (2026-10-09)**: as recommended.
2. **Should ordinary paths keep the double-quoted form?**
   **Recommendation**: yes. When every character is printable and none of `"` `\` `$` `` ` `` `!` occurs, double quotes
   are exact in a POSIX shell (and in interactive bash and zsh, where `!` is history expansion), and the output stays
   byte-identical to today; otherwise single quotes, with `'` written as `'\''`.
   **Decided (2026-10-09)**: as recommended.
3. **What bounds a command's length, now that it is no longer clipped?**
   **Recommendation**: 4096 bytes of quoted argument (Linux's `PATH_MAX`; macOS's is 1024), above which the command is
   replaced by a placeholder naming the path's length. Every path the OS can open in one call fits unless it is mostly
   escaped characters, and an artifact still cannot size the message beyond a fixed bound.
   **Decided (2026-10-09)**: as recommended.
4. **Does `approve`'s no-hash error (`aguard check %q --json`, which repeats the operator's own argument and is not
   clipped) switch too?**
   **Recommendation**: yes, to the same helper: it is a command printed to be copied, and `%q` is not shell quoting.
   **Decided (2026-10-09)**: as recommended.
5. **Is the raw (unsanitized) path line in `Reason()` fixed here?**
   **Recommendation**: no. It is display text, which this proposal keeps unchanged by its own reverse assertion; it is a
   separate invariant #7 follow-up.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #45 (2026-10-10; find the sha with git log --grep P-030)
Released: v0.20.0
Evidence: TestGateCommandsPasteAsPrinted (cmd/aguard/gate_paste_test.go); W1 red on this base (155865b): for the long path (272 runes on this machine) all five commands expand to the first 160 runes and "…", and the pasted checkTarget / approvePath fail with stat …: no such file or directory; for the shell-syntax path each of the five pastes creates pwned and pwned2 and expands $HOME → after W2 the four commands the gate prints are green and approve's note is still red (W2 changes only the gate) → after W3 all green
Evidence: TestApproveNoHashHintPastesAsPrinted (same file); red on W2's tree (pwned and pwned2 created, $HOME expanded) → green after W3
Evidence: TestShellQuoteRoundTripsThroughAShell and TestShellQuoteNeverPrintsAnInvisibleCharacter (internal/gate/command_test.go) green; the round trip ran through /bin/sh, bash and zsh here, the $'…' form through bash and zsh
Evidence: reverse assertions TestCommandsForAnOrdinaryPathAreUnchanged, TestDenyCommandForAnOrdinaryPathIsUnchanged (literal pins), TestGateDisplayStaysClipped and TestCommandsCannotSizeTheMessage green at W1 on the base and green after the fix without a character changed; TestShellQuoteIsPercentQForOrdinaryText green; internal/gate/gate_test.go and hook_test.go unchanged and green
Evidence: binary run by hand against a temporary root — main (155865b): the reason's aguard check pasted exits 2 with stat …: no such file; approve's note for a skill named x$(touch pwned), pasted into sh, expands to …/skills/x and creates pwned. This branch: the reason's aguard check pasted runs the audit (exit 1, the EXEC-001 at the default --fail-on high); the note prints aguard check '<W>/home/.claude/skills/x$(touch pwned)', which pastes to the exact path with nothing created
Evidence: Out of scope — git diff --stat origin/main -- internal/gate/approvals.go internal/collect internal/detect internal/report internal/clean go.mod go.sum internal/gate/gate_test.go internal/gate/hook_test.go docs/spec is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), the go directive in go.mod is still go 1.23.5
```
