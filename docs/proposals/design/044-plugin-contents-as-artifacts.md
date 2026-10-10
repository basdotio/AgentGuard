<!-- SPDX-License-Identifier: MIT -->
# 044 — The judge never reads the skills, commands and agents a plugin installs: a CLI-installed plugin is one tree artifact, and the judge has no question for that kind

- **Source**: `issues/023` (direction A for plugins, D for quarantined, no change for plain directories; chosen by the maintainer
  on 2026-10-11); follow-up to P-038
- **Depends on**: P-038 (the `LLM-000` note naming the artifacts no pass covers, exit 4 on a `check` target)
- **Branch**: `p/044-plugin-contents-as-artifacts`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The judge exists for the rewritten instruction no regex matches, and a plugin is the channel that installs instructions in
bulk. Yet `scan --llm` and `check <plugin> --llm` never put one sentence of a plugin's `skills/*/SKILL.md`, `commands/*.md` or
`agents/*.md` to the model. `collectPlugins` emits a CLI-installed plugin as ONE `plugin` artifact (the whole tree, read by the
static rules) plus, in `scan` only, its hooks and MCP servers as artifacts of their own; `judge.planFor` has no pass for the
`plugin` kind, so the only call a plugin can get is triage of its static findings, and with no static finding it gets no call at
all. P-038 made this visible (one `LLM-000` per run, exit 4 when the target of a `check` is a plugin) but gave the judge no
question.

The same `SKILL.md` checked on its own gets the intent and injection questions; installed through a plugin it gets none. Spec §4
already asks for a plugin's bundled skills and commands to be "expanded and rescanned under their own kinds (B4)"; only the hooks
and MCP servers ever were.

`quarantined` entries (`clean` moved them out of the load path) have no pass either; they no longer load, and the run says nothing
specific about them beyond the shared `LLM-000` count.

## Initial direction

**A for plugins**: collect each plugin's loadable skills, commands and agents as artifacts of their own — in `scan` and in
`check <plugin>` — so the existing `skill` / `command` / `subagent` passes apply unchanged. The plugin tree artifact and its hash
stay as they are. This moves the inventory, the averaged environment score, what a gate approval or a reputation entry covers, and
can report a static finding twice; each is measured below and has a decision. **D for quarantined**: disclose only, no judging.
Plain directories: no change.

## Measured (2026-10-11, base `78678a8`, Claude Code 2.1.107)

A scratch build (never committed) added the children described under "Design" behind an environment switch, so every "A" number
below comes from the same binary as its base number. `llm preview --json` counted the calls; nothing was sent.

### What Claude Code 2.1.107 loads from a plugin

Read from the plugin loader in the installed 2.1.107 binary (the skill, command and agent loaders and the manifest reader):

| Component | Loaded | Name |
|---|---|---|
| skills | `skills/SKILL.md` (the directory itself is one skill), otherwise every `skills/<dir>/SKILL.md` one level deep, `<dir>` a directory **or a symlink** | `<plugin>:<dir>` |
| commands | every `.md` under `commands/`, **recursively**; a directory holding a `SKILL.md` (any case) is one skill-style command and is not descended; symlinked entries are skipped (directory-entry types) | `<plugin>:<sub>:<file>` |
| agents | every `.md` under `agents/`, recursively; symlinked entries skipped | `<plugin>:<sub>:<file>` (or the frontmatter `name`) |
| manifest | `skills`, `commands`, `agents` in `.claude-plugin/plugin.json`: a path or a list of paths relative to the plugin root, **in addition to** the default directories; `commands` may also be an object whose entries carry a `source` path or inline `content`; a path escaping the plugin root is refused | as above |

The same real path loads once per plugin. Never loaded: `docs/<locale>/skills/…`, `.agents/skills/…` and other mirror copies,
`tests/` (`issues/017`). The same reader also lists `output-styles/` and a manifest `outputStyles`, which this proposal does not
touch (Out of scope).

### The real `~/.claude` (counts only; `scan --inbox off`)

| | base | A, children averaged like hooks (S2) | A, plugin and children one unit (S1) |
|---|---|---|---|
| artifacts | 184 | 362: +105 skills, +51 commands, +22 agents | 362 |
| `overall` (published) | 69 | 69 | 69 |
| average before the high cap | 95 | 97 | 95 |
| scoring findings in JSON | 806 | 834 (+28) | 834 |
| judge calls (`llm preview`) | 300 | 598 | 583 (no second triage of a finding the plugin carries) |
| bytes in the judge payloads | 592 KiB | 1,409 KiB | 1,407 KiB |
| `scan` CPU (user) | 137.6 s | 140.5 s | — |

- 11 plugins, 0 manifest-declared component paths. Their trees hold 252 `SKILL.md`, 171 `commands/**.md` and 66 `agents/**.md`
  (P-038's figures); **178 of those 489 load** — 311 are copies that never load and are not collected.
- The 298 added calls are 105 intent + 105 injection (skills), 51 + 22 injection (commands, agents) and 15 triage calls about
  static findings the plugin's own triage already covers.
- **All 28 added scoring findings are already on the plugin** (same rule, same line, byte-identical file): 22 on the same path, 6
  on a mirror copy the tree walk met first — the plugin row names `.agents/skills/…` or `docs/zh-CN/…`, the child names the copy
  that loads. No child finding is new.
- Hygiene gains one `context_bloat` item on a plugin skill. On the fixture below it gains a `duplicate_fn` whose blocker reads
  "lives outside the config root (installed elsewhere, e.g. by Claude Desktop)" — wrong for a plugin child.
- No CLI plugin here matches a reputation entry (the installed versions are not the pinned ones), and the approvals store is empty,
  so the reputation and gate effects are measured on the data and on a fixture instead.
