# Measurement: what Claude Code actually loads at startup

Every claim in this file was measured, not read. Where a measurement contradicts something we
previously wrote from documentation, the measurement wins and the contradiction is called out.

**Subject:** Claude Code `2.1.229`, Linux, Node v22.22.2.
**Date:** 2026-08-13.
**Mode caveat:** the harness drives `claude -p` and the debug log reports
`Fast mode is not available in the Agent SDK`, so these are **`-p` / Agent-SDK-path** results. The
interactive TUI may load strictly more (see the auto-memory row in §4). Treat every "not loaded"
verdict as scoped to this mode until re-measured interactively.

## 1. Method

Three independent signals, so no single one has to be trusted:

1. **What entered the model's context.** `ANTHROPIC_BASE_URL` pointed at a local HTTP server that
   dumps request bodies and answers `400`. The captured `POST /v1/messages?beta=true` body is the
   ground truth for "did this file reach the system prompt" — not the model's own report of what it
   can see, which is unreliable.
2. **What was read off disk.** `strace -f -e trace=openat,getdents64`. This distinguishes *never
   enumerated* from *enumerated and then filtered* — the second is a much weaker guarantee, since a
   filter is one refactor away from changing.
3. **What Claude Code says it did.** `--debug --debug-file`, quoted verbatim in §5.

Isolation: a purpose-built `CLAUDE_CONFIG_DIR` containing only probe files, each carrying a unique
`MARKER_*` token. The real `~/.claude` was never involved. Every probe is a no-op instruction.

## 2. Result: the quarantine address is safe

**Tier A1's safety premise holds.** Nothing under `<config-root>/.aguard-trash/` was opened, and
nothing reached the system prompt:

| probe | opened | in context |
|---|---|---|
| `.aguard-trash/trash-skill/SKILL.md` | no | no |
| `.aguard-trash/rules/tr-rule.md` | no | no |
| `.aguard-trash/agents/tr-agent.md` | no | no |
| `.aguard-trash/output-styles/tr-st.md` | no | no |
| `.aguard-trash/CLAUDE.md` | no | no |
| `.aguard-trash/MEMORY.md` | no | no |

`strace` shows `<cfg>/.aguard-trash` itself being stat'd once, as part of listing the config root,
and then never descended into. So the guarantee is "the config root is not a recursive scan target",
which is a structural property rather than a name-based filter.

## 3. Result: the address is safe, the *name* is not

This is the finding that changed the code. `rules/`, `agents/` and `commands/` are walked
**recursively**, and the walk **does not skip hidden directories**:

| probe | opened | in context | note |
|---|---|---|---|
| `rules/ctl-rule.md` | 3× | yes | control |
| `rules/a/b/c/deep-rule.md` | 3× | yes | recursion is ≥3 levels deep |
| `rules/.aguard-trash/nest-rule.md` | 3× | **yes** | a dot prefix does not hide it |
| `rules/aguard-trash/nest-rule2.md` | 3× | **yes** | nor is the dot what mattered |
| `agents/.aguard-trash/nest-agent.md` | 2× | **yes** | |
| `commands/.aguard-trash/tr-cmd.md` | 6× | **yes** | |

So `<root>/.aguard-trash` is inert **only because of where root is**. Point `--root` at
`~/.claude/rules` and the identical code parks quarantined content at
`~/.claude/rules/.aguard-trash/…`, where it keeps loading every session while the report says
"quarantined". That is worse than not cleaning at all: a miss leaves the operator looking, a false
assurance stops them.

`internal/collect.QuarantineUnsafe` now refuses that address, in dry-run as well as apply, and
`TestApply_RefusesRootInsideALoadedTree` locks it.

### 3.1 Only the extension stops the loader

Same directory (`rules/.aguard-trash/`, the worst case above), four extensions:

| probe | in context |
|---|---|
| `probe-a.md` | **yes** |
| `probe-b.md.quarantined` | no |
| `probe-c` (no extension) | no |
| `probe-d.markdown` | no |
| `agents/.aguard-trash/probe-e.md.quarantined` | no |

A rename scheme is therefore *available* as defence-in-depth, and was rejected for now: refusing an
address is verifiable in one function, whereas per-file renaming inside a quarantined tree fails
half-way and leaves a directory that is neither loadable nor restorable. Revisit if we ever support
in-place quarantine.

## 4. Result: two corrections to what we believed

**`skills/` — a dot-prefixed directory is still a live skill.** `skills/.dotskill/SKILL.md` was
opened 9× and its description reached the system prompt. The debug log says only that a dot-prefixed
directory `is never adopted as a plugin` — a *different* rule that reads like protection and is not.
The collector must therefore keep walking hidden directories under `skills/`; the tidy-looking
"skip hidden dirs" refactor would blind the scan to exactly the layout an author picks when they want
a skill to look like it isn't there. Locked by
`TestCollectSkills_DotPrefixedDirIsStillLoaded`.

`skills/` is walked **one level only**: `skills/.aguard-trash/nest-skill/SKILL.md` and
`skills/aguard-trash/nest-skill2/SKILL.md` were both ignored, while `strace` shows repeated failed
opens of `skills/.aguard-trash/SKILL.md` — each subdirectory is treated as one skill candidate, so a
nested layout is missed rather than protected. The margin here is one directory level.

**`workflows/` are NOT auto-loaded.** The directory is enumerated at startup (so `/<name>`
completes) but `workflows/ctl-wf.md` was never opened and never reached context. We had been
counting workflows on the report's `Auto-loaded:` line, which overstated an environment's standing
context cost. They now print on a separate `On-demand:` line, and `model.KindWorkflow` carries the
correction.

**`output-styles/` load only when selected.** With no `outputStyle` in settings the control style
was read from disk but did not reach the system prompt; after `{"outputStyle":"ctl-style"}` it did.
Note the consequence for cleanup: quarantining the *selected* style leaves a dangling reference in
settings, which is a config-integrity check we do not yet have.

**Auto-memory: NOT REPRODUCED, still open.** `projects/<project>/memory/MEMORY.md` was never opened
— no `openat`, no `stat`, no debug line mentioning memory — even though `--bare` advertises skipping
"auto-memory", implying the normal path has it. Either it needs a setting we did not set, a project
with prior sessions, or the interactive TUI. **`issues/012`'s claim that auto-memory is the one
unbounded surface is therefore NOT yet measured** and must not be cited as measured.

## 5. Incidental findings worth acting on

Quoted from the debug log:

- `Loading skills from: managed=/etc/claude-code/.claude/skills, user=<cfg>/skills, project=[/home/claude/.claude/skills]`
  — with cwd `/home/claude/trashlab/proj`, the *project* skills path resolved to `/home/claude/.claude/skills`.
  Project scope walks **up** to an ancestor `.claude`. A `--root <project>/.claude` audit can
  therefore miss skills that session will load from an ancestor directory. Not yet handled.
- `Watching for changes in skill/command directories: <cfg>/skills, <cfg>/commands, <cfg>/agents` —
  Claude Code **watches these directories live**. `clean --apply` during an active session is
  observable by that session mid-conversation. Our lock protects two `aguard` runs from each other,
  not `aguard` from a running agent.
- `Extracted ZIP to <cfg>/skills/synced/.staging/…` — `skills/synced/` is sync-owned and repopulated
  automatically (`[skills] skipping reserved dir name 'synced' … 'synced' is the sync-owned root`).
  Quarantining anything under it will be undone by the next sync; cleanup there should be refused,
  not attempted.
- `getSkills returning: … 41 bundled skills` — a large share of any environment's skill surface ships
  with Claude Code and is not the operator's to clean. Counting bundled skills as user inventory
  would misreport every environment.

## 6. Reproducing

The harness lives outside the repo (it needs a throwaway `CLAUDE_CONFIG_DIR` and a local endpoint):
build a config root of `MARKER_*`-tagged probes, run

```
CLAUDE_CONFIG_DIR=<probe-root> ANTHROPIC_BASE_URL=http://127.0.0.1:8787 \
  ANTHROPIC_API_KEY=probe strace -f -e trace=openat,getdents64 -o trace.log \
  claude -p ok --dangerously-skip-permissions --debug --debug-file debug.log
```

then grep the captured request body for each marker. A probe that appears in the body was loaded
into context; one that appears in `trace.log` but not the body was read and discarded; one absent
from both was never touched.
