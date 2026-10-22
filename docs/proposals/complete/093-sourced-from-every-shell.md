<!-- SPDX-License-Identifier: MIT -->
# 093 — A script the artifact writes and hooks into a shell startup file is EXEC-013 at high

- **Source**: the maintainer, 2026-10-22 ("后续这几个都做": the static shapes left after P-091)
- **Depends on**: P-092 (stacked on its branch: both move `rulesEpoch`)
- **Branch**: `p/093-sourced-from-every-shell`

## Problem

SkillHarm's backdoor-injection trees, 3 of 20 caught statically, mostly persist the same way: write a small script
(`cat > /root/.cache/pptx-ooxml/init.sh <<EOF … EOF`), then append `source /root/.cache/pptx-ooxml/init.sh` to
`/root/.bashrc`. From then on the script runs in every shell the user or the agent opens. No rule saw either half.

Appending a `source` line to a startup file is not itself suspicious: nvm, cargo and ROS document their setup exactly that
way. On the corpus it happens to be absent from every benign sample (4,922: skills, MCP servers, hooks, configs), but a
rule that relies on that would be wrong the first time someone scans a ROS skill.

## Initial direction

`EXEC-013` (dimension 4, high), two halves:

- Line rule: a startup file (`~/.bashrc`, `.bash_profile`, `.bash_login`, `.profile`, `.zshrc`, `.zshenv`, `.zprofile`,
  `.zlogin`, `/etc/profile`, `/etc/bash.bashrc`, `/etc/zsh/zshrc`) appended to (`>>`, `tee -a`) with a `source`/`.` of a
  script under `/tmp`, `/var/tmp` or `/dev/shm` — world-writable, emptied on reboot, never what a setup guide sources.
- File-level half (the PERM-007 pattern): the same file writes a path (a redirect's target or `tee`'s argument) and appends
  `source <that path>` to a startup file. Both lines are cited. A script some installer put there is neither.

Runs where the rules run (scripts, `SKILL.md`, a named document's code — P-091); not prose, tool descriptions or config
data. `rulesEpoch` 6 → 7. OWASP position ASI-05.

## Done criteria

- [x] `TestDetect_ScriptHookedIntoEveryShell`: five forms fire at high / dim 4 (heredoc then `echo … >>`, `printf '\n. X\n' >>`,
  `tee -a ~/.zshrc`, inside a `SKILL.md` fence, a `/tmp` script); seven stay quiet (nvm, cargo, ROS, a PATH line,
  completions, a script written and sourced but not hooked into a startup file, written in one file and hooked in another)
- [x] Static over the same sets as P-092, against the P-092 branch
- [x] `make verify`; `docs/rules.md` regenerated; `.claude/rules/detect.md`

## Out of scope

A heredoc appended straight into `.bashrc` (`cat >> ~/.bashrc <<EOF`: the content is on the following lines), a PATH
overwritten without `$PATH`, `ulimit` in a startup file — one development tree each. A hook written in a document
`SKILL.md` does not name (P-091 reads one level): one development tree.

## Must not claim

Not "detects persistence": it sees two spellings of one mechanism (shell startup files). Cron, systemd units, launchd
agents, git hooks and editor config are not covered; none of them appears in a malicious corpus sample.

## Measured

Static, corpus `6d7aa48e`, the P-092 branch (`results/aguard/2026-10-22-p092-*-changed`) against this branch
(`results/aguard/2026-10-22-p093-*-changed`), built the same day.

| | n | P-092 | P-093 |
|---|---:|---:|---:|
| malicious skill trees (development) | 393 | 108 | **112** |
| held-out malicious (ClawHub 123 + SkillHarm fresh 120) | 243 | 76 | **77** |
| benign skill trees (development) | 2,879 | 162 | 162 |
| held-out ClawHub benign | 216 | 6 | 6 |
| freshly sampled benign skills | 200 | 38 | 38 |
| every non-skill sample: malicious / benign | 156 / 1,627 | 39 / 245 | 39 / 245 |

All five trees gained are SkillHarm backdoor-injection (development 3 → 7 of 20 flagged). EXEC-013 also fires on one
more development tree (`dd-nickoc-clawscan`, the `/tmp` form) that other rules already flagged. No benign sample
anywhere gained a flag.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The rule, its file-level half, tests and measurement | P-093: a script the artifact writes and hooks into a shell startup file is EXEC-013 at high; measured |
