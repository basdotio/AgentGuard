<!-- SPDX-License-Identifier: MIT -->
# 092 — Deleting or emptying a log, or clearing the shell history, is FS-005 at high

- **Source**: the maintainer, 2026-10-22 ("后续这几个都做": the static shapes left after P-091, starting with erased traces)
- **Depends on**: P-091 (a named document's code runs the rules — most of these payloads sit there)
- **Branch**: `p/092-erases-its-own-traces`

## Problem

Without the judge, BAS Guard caught 21 of the 240 SkillHarm development trees. One of the twelve risk categories,
anti-forensics, was 0 of 20: its payload is erasing a record of what ran — `rm -f /var/log/<tool>/audit.log`,
`: > /root/.bash_history` plus `history -c`, `rm -f /root/.audit/usgs_access.log` — presented as "clear the stale audit
cache before the first request". No rule names that shape, in any file.

## Initial direction

One line rule, `FS-005` (dimension 9, high): a log erased under the home directory or `/var/log` — deleted (`rm`,
`shred`, `unlink`), truncated to zero, or emptied by a redirect with nothing (or `:`, `true`, `cat /dev/null`,
`echo -n`) in front of it — or the shell history switched off or cleared (`history -c`, `unset HISTFILE`,
`HISTFILE=/dev/null`, `HISTSIZE=0`, `set +o history`). Under the home directory a path counts only when it names a
`.log` or `*_history` file; `/var/log` is logs by definition. Appending (`>>`) is never this, and neither is a command's
own output redirected onto a log (`app > /var/log/app.log`, `app &> …`) — the prefix of the emptying redirect is what
separates the two.

And a file-level half, the PERM-007 pattern: a log path assigned to a name on one line and the name erased on a later
line (`AUDIT_LOG="/root/…/audit.log"` … `rm -f "${AUDIT_LOG}"`; `LOG = Path("/root/.agent_audit.log")` … `LOG.unlink()`;
`os.remove(LOG)`; `open(LOG, "w")`). One layer, same file, like EXFIL-001's destination: a path built by a join or passed
as a parameter is not followed. Both lines are cited. It runs where FS-005 runs (not prose, tool descriptions or
config data) and only when the line rule did not fire in that file. `rulesEpoch` 5 → 6.

## Done criteria

- [x] `TestDetect_ErasesTraces` (13 forms fire at high / dim 9; 10 quiet: cron `>>`, a command logging, `&>`, an echo
  with content, `tail -f`, a log under `/tmp` or `./`, a cache directory, a non-log file under home);
  `TestDetect_ErasesTraces_InADocumentTheSkillNames` (fires in a named document's code block, not in an unnamed doc);
  `TestDetect_ErasesTraces_ThroughAName` (shell, pathlib and `os.remove` forms fire citing both lines; a non-log path,
  a read, an append, a different file and a `/tmp` log stay quiet)
- [x] Static, over every skill tree, the held-out sets, 200 freshly sampled benign skills and every non-skill sample,
  main against the branch
- [x] `make verify`; `docs/rules.md` regenerated; `.claude/rules/detect.md`

## Out of scope

The judge (no prompt or excerpt change). The other static gaps found in the same pass — a script written and sourced
from a shell startup file (P-093), a download saved and then run (P-094). Paths built by `os.path.join` or a glob loop,
which is how the remaining held-out anti-forensics trees are written.

## Must not claim

Not "detects anti-forensics": it sees one family of spellings. Most held-out anti-forensics trees (7 of 10) are still
missed statically — a joined path, a glob loop, a PDF.

## Measured

Static only (no judge; deterministic, so no raw archive), corpus `6d7aa48e`, main `b4c1dba` against the branch, both built
the same day; `results/aguard/2026-10-22-p092-{skills,heldout,fresh200,other}-{main,changed}/`. The main binary's
version string reads `-dirty` because the untracked test file was present when it was built; the binary is main's.

| | n | main | branch |
|---|---:|---:|---:|
| malicious skill trees (development) | 393 | 98 | **108** |
| held-out malicious (ClawHub 123 + SkillHarm fresh 120) | 243 | 73 | **76** |
| benign skill trees (development) | 2,879 | 162 | 162 |
| held-out ClawHub benign | 216 | 6 | 6 |
| freshly sampled benign skills | 200 | 38 | 38 |
| every non-skill sample: malicious | 156 | 38 | 39 |
| every non-skill sample: benign (MCP servers, hooks, configs, …) | 1,627 | 245 | 245 |

The ten development trees gained are all anti-forensics (0 → 10 of 20). Held-out: three anti-forensics trees (1 of 10 →
3 of 10 counting the one the line rule catches on its own: two of the three are the assigned-name form). Non-skill: one
MCP sample, `log_tampering_evidence_removal`. No benign sample anywhere gained a flag.

**Reading.** A clean rule — 0 benign in 4,922 — for a shape that had no rule. The held-out gain is smaller than the
development gain because the generator writes the same payload many ways; the line rule plus one layer of names covers
the spellings a regex can, and the rest is the judge's.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The line rule and its tests | P-092: deleting or emptying a log under home or /var/log, or clearing the shell history, is FS-005 at high |
| 2 | The assigned-name half, measured | P-092: FS-005 follows a log path assigned to a name to the line that erases it; measured |
