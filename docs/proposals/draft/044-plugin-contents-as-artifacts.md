<!-- SPDX-License-Identifier: MIT -->
# 044 — The judge never reads the skills, commands and agents a plugin installs: a CLI-installed plugin is one tree artifact, and the judge has no question for that kind

- **Source**: `issues/023` (direction A for plugins, D for quarantined, chosen by the maintainer on 2026-10-11); follow-up to P-038
- **Depends on**: P-038 (the `LLM-000` note naming the artifacts no pass covers, exit 4 on a `check` target)
- **Branch**: `p/044-plugin-contents-as-artifacts`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The judge exists for the rewritten instruction no regex matches, and a plugin is the channel that installs instructions in
bulk. Yet `scan --llm` and `check <plugin> --llm` never put one sentence of a plugin's `skills/*/SKILL.md`, `commands/*.md` or
`agents/*.md` to the model. `collectPlugins` emits a CLI-installed plugin as ONE `plugin` artifact (the whole tree, read by the
static rules) plus its hooks and MCP servers as artifacts of their own; `judge.planFor` has no pass for the `plugin` kind, so the
only call a plugin can get is triage of its static findings, and with no static finding it gets no call at all. P-038 made this
visible (one `LLM-000` per run, exit 4 when the target of a `check` is a plugin) but gave the judge no question.

The same `SKILL.md` checked on its own gets the intent and injection questions; installed through a plugin it gets none. Measured
in P-038 on one real `~/.claude`: 11 plugins, 7 with no call at all and 4 with triage only.

`quarantined` entries (`clean` moved them out of the load path) have no pass either; they no longer load, and the run says
nothing specific about them beyond the shared `LLM-000` count.

## Initial direction

**A for plugins**: collect each plugin's loadable skills, commands and agents as artifacts of their own — in `scan` and in
`check <plugin>` — the way its hooks and MCP servers already are, so the existing `skill` / `command` / `subagent` passes apply
unchanged. The plugin tree artifact and its hash stay as they are. This moves the inventory, the averaged environment score,
what a gate approval or a reputation entry covers, and can report a static finding twice; each needs a measurement and a decision
before code. **D for quarantined**: disclose only, no judging. Plain directories: no change.
