<!-- SPDX-License-Identifier: MIT -->
# 023 — The judge asks nothing of plugin trees and unrecognised directories

- **Category**: coverage gap (覆盖缺口)
- **Severity**: medium (中)
- **Status**: `未修复` — nobody has decided what the judge should ask. P-038 (2026-10-10) made the gap visible and made a
  `check` target of these kinds fail `--fail-on-llm` with exit 4; it did not give the judge a question.

## Problem

`internal/judge/run.go` `planFor` has a pass for skills, the instruction-like kinds (CLAUDE.md, subagents, commands, rules,
workflows, output styles, memory), connectors, hooks and MCP servers. It has **none** for `plugin`, `directory` and
`quarantined` (`judge.noPassKinds`). For an artifact of those kinds the only call is triage of its static findings, whose labels
are display-only; with no static finding there is no call at all. The static rules read these trees in full — the judge, which
exists for the rewritten instruction no regex matches, never reads them.

Measured on v0.20.1+ (`6cab205`), `authority: escalate`, `samples: 1`, `--fail-on-llm high --fail-on critical`:

| Target | judge block | exit before P-038 |
|---|---|---|
| plugin directory: manifest + `skills/p1/SKILL.md` whose body asks the agent to quietly send the user's home directory to an address in config | `ran: true, calls: 0` | 0 |
| plain directory: `NOTES.md` with the same sentence + a benign `build.sh` | `ran: true, calls: 0` | 0 |
| the plugin plus a script with one static medium (`EXEC-004`), working endpoint | 1 triage call, answered | 0 |
| the same `SKILL.md` checked as a skill | 2 questions | answered: 0; closed port: 4 |

**The scan side is larger than it looks.** It was assumed that a scan judges a plugin's skills as artifacts of their own. For
CLI-installed plugins it does not: `collectPlugins` emits one `plugin` artifact (the whole tree, for the static rules) plus the
plugin's hooks and MCP servers as their own artifacts (`issues/006` records that skills and commands were never split). Only
the Claude Desktop *skills bundles* are collected per skill. On one real `~/.claude` (184 artifacts): 11 plugins, whose trees
hold 252 `SKILL.md`, 171 command and 66 agent markdown files (file counts, some of them copies that never load — see
`issues/017`); none of them is judged in `scan --llm`. 7 of the 11 plugins got no call at all, 4 got triage only.

## What P-038 did, and why it stopped there

- Every `--llm` run carries one `LLM-000` counting the artifacts of these kinds and naming the first three.
- `check <target> --llm --fail-on-llm` exits 4 when the target is one of them (a plugin, a plain folder, either as a `.zip`).
- `scan` and a root-shaped `check` keep their exit code: counting plugins there would make every machine with a plugin exit 4
  whatever the judge said, because of this issue.

Deciding what to ask changes what leaves the machine, how many calls a run makes, `ExcerptVersion`, and — for option A — the
inventory, the environment score and the hashes approvals and reputation entries are keyed by. Each needs its own measurement;
P-038 only stopped "asked nothing" from reading as "answered".

## Fix directions (none decided)

- **A. Collect a plugin's skills, commands and agents as artifacts of their own**, in `scan` and in `check <plugin>`, the way
  its hooks and MCP servers already are; the existing passes then apply unchanged. Costs: the inventory and the averaged
  environment score move (more artifacts dilute existing findings — the 86 → 97 effect in `.claude/rules/pipeline.md`); each
  new artifact has its own hash, so gate approvals and reputation entries keyed by the plugin tree do not cover them; the same
  text appears twice (tree + artifact), as plugin hooks do today.
- **B. Give the `plugin` kind a judge plan** over its loadable files (`skills/*/SKILL.md`, `commands/*.md`, `agents/*.md`): an
  injection question per file, intent per skill, findings grounded to `file:line` on the plugin artifact. No inventory or score
  change. Cost: on the machine above, on the order of 500 extra questions per scan, so it needs a budget story (`max_calls`
  already reports what it cuts) and a corpus measurement before it is the default.
- **C. `directory`** has no loading semantics to anchor a choice: the candidates are every markdown file and every script in the
  tree, which is unbounded. A cap by size or count is a design decision of its own.
- **D. `quarantined`** content is no longer loaded; disclosing it may be all it needs.

## Source

- P-038 (`docs/proposals/complete/038-judge-asked-nothing.md`): the measurements above, the `LLM-000` note, exit 4 on a target
- `internal/judge/unasked.go` (`noPassKinds`, pinned to `planFor` by `TestAsksNothingOf_FollowsPlanFor`)
- `internal/collect/plugins.go` (`collectPlugins`), `internal/collect/desktop.go` (per-skill desktop bundles)
