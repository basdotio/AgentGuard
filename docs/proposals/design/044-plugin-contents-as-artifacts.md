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

### A fixture plugin

A plugin with a manifest, three skills, two commands (one nested), one agent, one hook, `scripts/build.py` with one static
medium (`EXEC-004`), and a `docs/ja/` copy of one skill. Variant 1 puts `curl … | bash` in `skills/s1/SKILL.md` and in the hook's
script; variant 2 leaves only the medium.

| Variant 2 (medium only) | base | S2 | S1 |
|---|---|---|---|
| `scan` `overall` (plugin + its hook) | 94 | 98 | 94 |
| `check <plugin>` `overall` | 88 | 98 | 88 |
| `check <plugin>` artifacts / judge calls | 1 / 1 | 7 / 11 | 7 / 11 |

- Variant 1, S2, terminal report: the same `EXEC-001` at `s1/SKILL.md:6` is printed twice — once on the plugin row, once on the
  `skill p:s1` row — and counted twice in "N findings need a look". The `docs/ja/` copy produces no child.
- **The gate already resolves a namespaced skill to the skill directory**: `PreToolUse[Skill]` with `p:s1` audits
  `<installPath>/skills/s1` (75/100, `EXEC-001`) under content hash `eca3989e…` — the hash the child artifact carries in the
  scratch scan, and what `aguard hash <installPath>/skills/s1` prints. Commands and agents hash like `aguard hash` on the file.
  So A puts in the scan report, under the same hash, exactly the bytes the gate audits and approves.
- The gate resolves only `skills/`: fed `p:c1` (a plugin command's name), it answers `GATE-000 … no plugin bundle or project
  directory "p" provides it`, although the bundle exists.
- **`check <plugin>` splits nothing today**: the fixture is 2 artifacts in `scan` (plugin + its hook) and 1 in `check`. The
  premise that `check` collects a plugin's hooks and MCP servers "the way it already does" holds for `scan` only.

### The corpus

`agent-artifact-corpus` (`97e5af0`, read-only): 0 samples route as a plugin. Two samples carry `.claude-plugin/plugin.json`, both
with a `SKILL.md` at their root, so `check` reads them as skills, before and after. A changes nothing there, and the corpus cannot
say how the judge does on plugin contents.

### Reputation and approvals

The 13 plugin entries in `internal/reputation/data/reputation.json` keep matching (the plugin tree hash does not move). But a GOOD
match suppresses the findings of the artifact whose hash matched, and a child has its own hash: **18 of the 253 reviewed findings
of those entries sit in `skills/<x>/` files** (superpowers 6, plugin-dev 3, receipts 2, skill-creator 5, hookify 2; none in
`commands/` or `agents/`) and would reappear, unsuppressed, on the children of a matched plugin. The 5 Claude Desktop entries are
per skill already and do not move. An approval of a plugin tree hash keeps covering the plugin artifact and covers no child; an
approval the gate recorded for `p:s1` already sits under the child's hash.

### How plugin hooks are handled today (the precedent)

- The hook **command** is read twice: as text in the plugin tree, and as the hook artifact, where hook-specific rules run. On the
  real `~/.claude`, 29 plugin hooks and 27 plugin MCP servers carry 9 scoring findings, all `HOOK-001`, none duplicated on a tree.
- The hook's **script** is not read a second time for the hook: the hook gets a `COV-000` "Hook script attributed to its plugin,
  not to the hook" and the findings live once, on the plugin.
- Plugin hooks and MCP servers count in the environment average as artifacts of their own (56 here). Folding them into a plugin
  unit would move today's numbers (real average before the cap 95 → 94, fixture `scan` 94 → 88), so this proposal does not.

## Design (as recommended in the open questions)

1. **Collect.** One helper, `collect.pluginContents(pluginRoot, bundle, suffix)`, called by all three plugin channels
   (`installed_plugins.json`, `plugins/synced/<uuid>/`, the Claude Desktop store) and by `CollectTarget`'s plugin branch — one
   builder, as for hooks, because two would drift. It follows the 2.1.107 table above and nothing else: no `docs/`, no mirror
   copies. Containment is checked per entry against the plugin root after resolving (invariant #2); an entry that exists and cannot
   be resolved or read joins the existing aggregate `COV-000` shapes, a dangling one stays silent (`hidesContent`). Kinds and
   hashes: `skill` with `TreeHash` of the skill directory, `command` and `subagent` with `FileHash` of the file — what
   `aguard hash` and the gate compute. Name: `<bundle>:<leaf> (plugin <name@marketplace>)`, the leaf exactly as Claude Code and the
   gate resolve it (`<dir>` for a skill; `<sub>:<file>` for a command or an agent), the suffix as for plugin hooks and MCP servers.
   A new field `ArtifactReport.Plugin` (`json:"plugin,omitempty"`) names the parent plugin artifact. `skills=` and the other
   inventory counters do not move; the "Inside plugins" line gains commands and agents (`EnvSummary.BundledCommands`,
   `BundledAgents`, next to `BundledSkills`).
2. **Detect and hashes** run on a child as on any artifact of its kind, so a child's score and hash are what `check <its path>`
   and the gate say about the same bytes.
3. **Reputation.** A child of a plugin that matched a GOOD entry inherits the match: its deterministic findings are suppressed and
   counted in the plugin's `REP-GOOD` note. The child's bytes are inside the reviewed tree (hash-exact), and its findings are in the
   entry's reviewed `findings` list (the 18 above). No entry is added, none changes.
4. **Score.** The environment average runs over **units**: a plugin and its children are one entry, scored with the existing
   per-artifact formula over the union of their findings (within a dimension the highest hit counts, so a duplicate adds nothing);
   the effective number does the same with `score.Escalating`. Per-artifact `score` / `score_effective` keep their definition.
   While a child's deterministic findings are a subset of its plugin's (all 28 measured), `overall` is byte-identical to today; a
   qualified LLM finding on a child lowers its unit's effective score, and a high one caps `overall_effective` at 69 like any
   other.
5. **Report.** Children are listed under their plugin. A child's deterministic finding that its plugin also carries (same rule,
   same evidence file and line) is not printed again — terminal, markdown, HTML, SARIF — and is not counted again in the headline
   counts; JSON carries every finding of every artifact. One exported predicate decides "the plugin row already shows this",
   used by the renderers and by the judge (item 6), so the two cannot disagree. The 6 mirror-copy cases are printed on the child:
   its row names the copy that loads.
6. **Judge.** No new pass; `planFor` and `ExcerptVersion` do not move. Children get the passes of their kind (a skill: intent,
   injection, and explain or collusion when their triggers fire; a command or an agent: injection). Triage on a child skips the
   findings the plugin row already shows (the plugin's own triage asks about them): 598 → 583 calls here.
7. **P-038's note and exit 4.** A plugin counts as "asked nothing about" only when none of its children got a question; the same
   predicate drives the `LLM-000` count and `check <plugin> --fail-on-llm`'s exit 4, so they cannot disagree. A plugin with no
   loadable skill, command or agent keeps both.
8. **Gate, hygiene, approve.** No change in `internal/gate`'s resolution. `SessionStart`, `Summarize` (hence `aguard approve`) and
   hygiene skip children: every blocking rule of a child is on its plugin's row, approvals of a skill loaded by the gate already
   key on the child's hash, and `clean` never moves anything outside `skills/` anyway. Their outputs stay as today.
9. **`rules_version`.** The deterministic findings an input produces change (new artifacts carry findings), so `detect.rulesEpoch`
   is bumped and `make docs` run (pipeline.md, P-002).
10. **Quarantined (D).** P-038's note already counts them; its kind label becomes `quarantined (N, no longer loaded)` so the count
    does not read as a loaded gap. No judging, no triage change.

## Done criteria

- [ ] `TestPluginContents_FollowClaudeCodesLoader` (`internal/collect`, new, `t.TempDir()` fixture with one row per loader case:
  `skills/SKILL.md`, `skills/<dir>`, a symlinked skill directory, nested `commands/<sub>/<file>.md`, `commands/<dir>/SKILL.md`,
  a symlinked command file, nested agents, manifest `skills` / `commands` / `agents` as a string, a list and a `source` object, a
  manifest path escaping the root, a `docs/ja/skills/` copy, an `.agents/skills/` copy): for `CollectAll` (installed, synced and
  Claude Desktop channels) and `CollectTarget(<plugin>)`, exactly the expected children — kind, name, `Plugin`, and a hash equal to
  `TreeHash` / `FileHash` of that path — and none for the copies, the escaping path or the symlinked command. Red on the base: no
  child artifacts at all
- [ ] `TestGate_NamespacedSkillHashIsTheChildsHash` (`cmd/aguard`): `PreToolUse[Skill]` `p:s1` over a fixture root audits the
  directory whose hash the child `p:s1` carries in `scan --json` of the same root. Red on the base: no such child
- [ ] `TestApply_PluginAndChildrenAreOneUnit` (`internal/score`): a plugin with one medium and six clean children scores the
  environment exactly as the plugin alone (94 for the fixture `scan`, 88 for `check <plugin>`); a child with a qualified high LLM
  finding lowers the unit's effective score and caps `overall_effective` at 69 while `overall` does not move; the
  `TestApply_EffectiveNeverExceedsOverall` property still holds. Red on the base: children averaged per artifact give 98 and 98 (the S2 rows)
- [ ] `TestRender_ChildDuplicatesAreShownOnce` (`internal/report`): the variant-1 fixture prints `EXEC-001` at `s1/SKILL.md:6`
  once in the terminal, markdown and HTML reports and once in SARIF, counts it once in "N findings need a look", and keeps it on
  both artifacts in JSON; a child finding on a file the plugin row does not name (the mirror case) is printed on the child
- [ ] `TestReputation_ChildrenInheritAGoodMatch` (`cmd/aguard`): a fixture plugin whose tree hash is a GOOD entry has every
  child's deterministic findings suppressed and counted in the one `REP-GOOD` note; the same tree with one byte changed suppresses
  nothing on the plugin or its children
- [ ] `llm preview --json` over the fixture plugin plans the passes of each child's kind and no triage of a finding the plugin row
  shows; over the real `~/.claude`, 300 → 583 calls (recorded with the build that ships, not this design's scratch build)
- [ ] P-038's rows move as decided: `check <plugin with skills/p1/SKILL.md> --llm --fail-on-llm high` answers from the judge (stub
  answering: exit 0 and no `LLM-000` "asked nothing"; closed port: exit 4 with P-026's reason, the calls failed); a plugin with no
  loadable skill, command or agent still exits 4 and is still counted; plain directories still exit 4
- [ ] **Reverse assertions**: the plugin tree artifact's entry in `scan --json` and `check --json` is byte-identical to the base
  (name, path, hash, findings, score) on the fixtures and on the real `~/.claude`; `overall` 69 → 69 on the real `~/.claude`, 94 → 94
  and 88 → 88 on the fixture; `check <plugin> --fail-on {critical,high,medium,low}` exit codes unchanged on both variants;
  `SessionStart`, `aguard approve <plugin>` and hygiene outputs unchanged on the fixtures; `TestZeroDial_*`,
  `TestHashGolden`, `TestContentHashGolden`, `TestAsksNothingOf_FollowsPlanFor` green unchanged
- [ ] Docs: README pair (inventory line), `docs/architecture*.md`, `docs/llm-judge*.md` (what the judge reads of a plugin),
  `docs/install-gate*.md` if a sentence about plugin skills moves, `docs/rules.md` via `make docs`, spec §4 (the B4 row and its
  "still not split" note), §5.3 (units), §8 (`plugin` field), `issues/023` (A done; C open; D), `issues/006`/`008`/`017` index lines,
  and the `.claude/rules/pipeline.md` bullet that says splitting dilutes (it now has a unit rule to point at)
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no toolchain switch; no new dependency

## Out of scope

- **The plugin tree artifact and the canonical hash**: same name, path, `TreeHash`, findings and score as today;
  `internal/collect/hash.go` and `internal/detect/contenthash.go` untouched (P-043 works there); `internal/redact` untouched
- **Plain directories (C)**: no change; they stay counted in P-038's note and exit 4 on a `check` target
- **No new judge pass**: `planFor`'s switch, the payloads and `ExcerptVersion` do not change; the `plugin` kind keeps no pass of
  its own, so `noPassKinds` and `TestAsksNothingOf_FollowsPlanFor` do not change
- **Plugin hooks and MCP servers**: not folded into the plugin unit (that would move today's `overall`, measured above), and not
  split in `check <plugin>` (that would change `check`'s `--fail-on` answer for a plugin whose hook command trips a hook-only rule
  such as `HOOK-001`). Both are follow-ups (open question 4)
- **The gate**: no resolution change. A plugin command passed to the Skill tool still answers `GATE-000`; that, and its wording
  ("no plugin bundle … provides it" when the bundle exists), are a follow-up
- **What A still does not collect**: `output-styles/` and a manifest `outputStyles`, inline `content` commands in the manifest,
  manifest-declared `mcpServers`. They stay read as text in the plugin tree by the static rules, and the judge reads none of them
  (Must not claim says so); measured: none of the 11 plugins on the real `~/.claude` has any of them
- **Pruning the tree read to the loading surface** (`issues/017`): the plugin tree keeps reading mirror copies and `tests/`
- **Enabled or disabled plugins**: every installed plugin is collected, as today
- **`check <file>` of an agent** still routes as `instruction` (only `commands/` is recognised by `singleFileKind`); a child agent
  is a `subagent` with the same `FileHash`. Not changed here
- **The Downloads (inbox) judge**: a plugin found there gets its children like any `check` target and never enters `overall`, as
  today; nothing else changes there
- No Go dependency, no `go` directive change, no rule severity change

## Must not claim

- **Not "the judge covers plugins"**: it reads the skills, commands and agents a plugin loads, as Claude Code 2.1.107 lists them.
  It still reads nothing of a plugin's scripts outside its skill directories, its docs, its output styles or its inline manifest
  commands, and plugin hooks and MCP servers only through their own passes
- **Not that the loader table is Claude Code's contract**: it was read from one installed build (2.1.107) and can move with an
  update; the fixture test pins this repository's reading of it, not Claude Code
- **Not "the score did not change" as a law**: `overall` is unchanged while a child's deterministic findings are a subset of its
  plugin's (measured: all of them). A child-only finding lowers its unit — that is the point, not a regression
- **No precision claim from the corpus**: it has no plugin-shaped sample, so nothing here measures how often the judge is right
  about plugin contents
- **Not "approving a plugin approves its skills"** or the reverse: approvals stay keyed by the hash of the bytes approved
- Exit 0 from `check <plugin> --llm --fail-on-llm` means P-026's "every planned question was asked and answered", over the
  children; it does not mean every file of the plugin was read by the model

## Work items

Implementation waits for the maintainer's answers to the open questions marked "needs the maintainer".

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The collect, gate-alignment, score, render, reputation, preview and P-038 tests above, with their reverse rows, run red on the base | `collect, score, report, cmd: tests — a plugin's skills, commands and agents are never artifacts of their own (P-044)` |
| 2 | `pluginContents` following the 2.1.107 loader, called by the three plugin channels and `CollectTarget`; `ArtifactReport.Plugin`; `BundledCommands` / `BundledAgents` | `collect: a plugin's skills, commands and agents become artifacts of their own (P-044)` |
| 3 | Children inherit their plugin's GOOD reputation match | `cmd: a plugin's children inherit its reputation match (P-044)` |
| 4 | The environment average runs over units: a plugin with its children is one entry | `score: a plugin and its children are one unit of the environment average (P-044)` |
| 5 | One "the plugin row already shows this" predicate; renderers list children under their plugin and print a duplicate once; SARIF the same | `report: a child finding its plugin already shows is printed once (P-044)` |
| 6 | Triage skips what the plugin row shows; the `LLM-000` count and `check`'s exit 4 count a plugin only when no child got a question; the quarantined label | `judge, cmd: a plugin whose contents were judged is not "asked nothing about" (P-044)` |
| 7 | `SessionStart`, `Summarize` and hygiene skip children | `gate, hygiene: children of a plugin are not listed twice (P-044)` |
| 8 | `detect.rulesEpoch` + 1 and `make docs` | `detect: bump rulesEpoch — plugin children carry findings (P-044)` |
| 9 | The docs listed in Done criteria; `issues/023` and the issue index | `docs: a plugin's skills, commands and agents are judged as artifacts of their own (P-044)` |
| 10 | "Done" in this file, the index | `proposals: P-044 (P-044)` |
