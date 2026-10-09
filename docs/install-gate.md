<!-- SPDX-License-Identifier: MIT -->
# Load-time gate

`aguard hook` audits a skill **before an agent loads it**, and asks you about anything that
carries a finding. It is the same static scan `aguard check` runs without `--llm` — no model, no
network, nothing executed — delivered at the moment the answer is worth something.

Chinese version: [install-gate.zh-CN.md](install-gate.zh-CN.md).

## Install

```bash
aguard hook install          # merges into ~/.claude/settings.json, backing it up first
aguard hook install --dry-run   # see the change without making it
aguard hook uninstall        # removes exactly its own entries
```

Restart Claude Code — hooks are read when a session starts.

The install merges rather than replaces. Unknown top-level keys, your permissions, and hooks
you wrote by hand are carried through untouched, and re-running it is a no-op. It registers
**one command** for all three events (the runner dispatches on `hook_event_name`), so there is
no shell pipeline in your settings file — which also means the gate's own installation does
not trip `HOOK-001`.

## What it does

| Event | What happens |
|---|---|
| `PreToolUse[Skill]` | Resolve the skill, scan it, decide. Already approved → silence. Clean and new → one line, then remembered. **Below the threshold but with a finding at medium or above → one line on every load, not remembered** (until the content is clean, or you `aguard approve` it). Findings at or above the threshold → **the load stops and you are asked**. |
| `PostToolUse[Skill]` | If you approved at the prompt, record it — but only after re-reading the target and confirming the bytes still hash to what the prompt described. |
| `SessionStart` | Audit the whole root and report unapproved artifacts carrying findings. Informs only; it cannot block. When nothing needs raising it still states what the gate does **not** cover, because a quiet environment is the one whose owner is most likely to assume it covers everything. |

To confirm it is actually wired up:

```bash
aguard hook status
```

Three states, stated plainly: **active**, **not installed**, or **registered but dead**. The
third is why this command exists — the registration bakes in an absolute path, and a path can
stop resolving (the binary moves, a build directory is cleaned, dotfiles land on a machine that
never had it). Then Claude Code runs a command that is not there, **every skill loads unaudited,
and the silence looks exactly like a clean environment**. Exit code: `0` when active, `1`
otherwise.

## Why load time, and not install time

Claude Code exposes nine hook events and **none of them fire when a plugin or skill is
installed**. Even if one did, it would not be complete: a skill also arrives by `git clone`,
by `cp`, and by hand in a terminal no session ever sees.

Loading is the boundary that can actually be held, and it is the one that matters. **A skill
on disk is inert.** It does nothing until an agent reads it into context — which is the same
argument `collect/unowned.go` already rests on: what makes something dangerous is whether the
agent has a load path in, not whether the bytes are present.

So the gate does not try to stop a download. It stops the artifact from speaking.

## Approvals are keyed by content, not by name

```bash
aguard approve ./some-skill        # trust these exact bytes
aguard approvals                   # list what has been trusted
aguard approvals forget <hash>     # withdraw one; a prefix works, including the short hash a gate message printed
aguard approvals forget all
```

The key is the artifact's **canonical hash** — the same tree hash the reputation library uses,
stable across machines and checkouts. Approving `pdf-export` does not approve "whatever is
called pdf-export from now on"; it approves those bytes. An update, a re-install, or a single
edited character produces a different hash, and the gate asks again **by itself**: no expiry to
tune, no cache to invalidate, nothing to remember to re-run.

Content with no hash cannot be approved. A config file that did not parse, or a file the scanner
could not open, has no hash for an approval to be keyed on, so `aguard approve` refuses it — it
names the artifact and the reason, exits 2, and leaves the store untouched.
`aguard check <path> --json` lists the notes that say what could not be read.

The `aguard check` and `aguard approve` commands in the gate's messages carry the artifact's
**full path**, quoted for a POSIX shell, so they work pasted exactly as printed. The path shown
for reading is shortened past 160 characters (a skill installed through the desktop app is usually
longer than that); the command is not. A path holding a control or invisible character is spelled
with `$'…'` escapes, which bash, zsh and ksh read; a path longer than 4096 bytes once quoted gets a
placeholder instead of a command.

The store is `~/.claude/.aguard-approvals.json`, mode 0600, written atomically. Besides the
approvals it holds, for at most an hour, the verdict behind each prompt still waiting for your
answer: the answer arrives in a separate hook call, and this file is the only thing the two
calls share. A store that
cannot be parsed degrades to *asking about everything* rather than to allowing it — the two
failure directions are not symmetric, and `aguard hook` will not overwrite a file it could not
read.

## Your permission mode changes what it does

Claude Code's `auto`, `acceptEdits`, `bypassPermissions` and `dontAsk` modes **answer prompts
automatically**. Under those, returning `ask` is not a question — it is a yes on your behalf,
and you see nothing at all.

So the gate reads the event's `permission_mode`, and when the mode would not actually put the
question in front of a human it **returns `deny` instead of `ask`**, naming the mode in the
reason. You have to know why you were not asked; otherwise the gate looks stricter than you
configured it, and the setting you would need to change is not the gate's.

```
This session runs in permission mode "auto", which answers prompts automatically.
An "ask" would have been accepted without you ever seeing it, so this was REFUSED instead.
To load it anyway, decide outside the session: aguard approve "<path>"
```

`default` and `plan` are unaffected — both show the question. An **unrecognised** mode does not
escalate either: turning every future Claude Code release into a wall of denials is its own way
of getting the gate uninstalled.

This was found on a real machine. A skill scoring 51/100 with a complete credential-exfiltration
chain returned `ask`, the session accepted it automatically, the skill loaded, and nothing was
displayed. A gate whose decision is swallowed has failed open in silence — the one failure this
package does not allow itself.

## What it does not cover

Stated plainly, because a gate you believe covers everything is worse than one you know the
edges of.

- **Only skills pass through the load-time gate.** A plugin's hooks and MCP servers are live
  from the first turn of a session — there is no "load" event to intercept. `CLAUDE.md` is the
  same. Those surfaces are covered by the `SessionStart` audit, which can only *tell* you.
- **The hook payload carries a skill NAME, not a path**, so the gate redoes the lookup
  (project → user root → installed plugins). A name it cannot place, or that two plugins both
  provide, is reported as **`GATE-000`: loaded without an audit** — never treated as clean.
- **It fails open, loudly.** An internal error, an unresolvable name or an unwritable store
  allows the load and says so. A security tool that bricks the editor when its own scanner
  breaks gets uninstalled, and then it protects nothing. What it must never do is stay quiet:
  "I could not audit this" and "I audited this and it is fine" are the two answers this whole
  tool exists to keep apart.
- **The clean-path notice may be invisible.** It travels in the hook protocol's
  `systemMessage` field, which does not render in the VS Code extension. Blocking is
  unaffected (that goes through `permissionDecision` and always takes effect), but it means
  "audited, nothing found" may reach you as silence. `aguard hook status` and
  `aguard approvals` are where to look instead — the latter lists every piece of content the
  gate has audited and trusted.
- **Static limits still apply.** Everything in the README's capability boundary holds here —
  the gate cannot prove malice, decode an obfuscated payload's intent, or catch a zero-day.

## Configuration

Both knobs are about noise, not capability; neither can make the gate call a model or reach
the network.

```yaml
gate:
  fail_on: high     # low | medium | high | critical — same default as `check --fail-on`
  action: ask       # ask (default) — you decide at the prompt
                    # deny          — refuse outright, for machines nobody is sitting at
```

## Registering it by hand

```json
{
  "hooks": {
    "PreToolUse":  [{"matcher": "Skill", "hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}],
    "PostToolUse": [{"matcher": "Skill", "hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}]
  }
}
```

Use an **absolute path**. The editor's `PATH` is not your shell's, and a hook that cannot be
found fails silently — which is a gate that is not there.
