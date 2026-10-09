<!-- SPDX-License-Identifier: MIT -->
# 029 — A plugin's MCP servers written without the `mcpServers` wrapper are invisible to the scan: Claude Code starts them, aguard counts zero and says nothing

- **Source**: follow-up recorded in P-021 open question 4 ([021](../complete/021-plugin-mcp-unscanned.md)); one plugin
  installed on the maintainer's machine writes its `.mcp.json` this way
- **Depends on**: P-021 (`detect.MCPServerKey`), P-009 (`ArtifactReport.MCPServer`, MCP content hash)
- **Branch**: `p/029-plugin-mcp-unwrapped`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`collect.collectPluginMCP` reads a plugin's `.mcp.json` / `mcp.json` through `mcpServersFrom`, which decodes only the
`mcpServers` member. A plugin file that lists its servers directly at the top level (`{"name": {"command": …}}`, no
wrapper) decodes to an empty map: zero MCP artifacts, `mcp_servers` does not count them, no note says the file was not
understood, and the plugin scores what its tree scores. The rules, the content hash and the judge's MCP config pass never
see those servers.

### What Claude Code does with such a file (measured, Claude Code 2.1.107, 2026-10-09)

**Source** (the JS embedded in `~/.local/share/claude/versions/2.1.107`, read with `strings`): the plugin MCP loader reads
`<plugin root>/.mcp.json` (and any file the plugin manifest's `mcpServers` names), takes `servers = doc.mcpServers || doc`,
and validates each entry against the MCP server schema; an entry that fails is skipped with an
`Invalid MCP server config for <key>` error line. Project-level `.mcp.json` goes through a different reader whose schema
**requires** `mcpServers`. Every valid schema branch requires either a `command` member (stdio) or a `type` member.

**Runtime**: isolated `CLAUDE_CONFIG_DIR` and `HOME`, `env -i`, `ANTHROPIC_BASE_URL` pointing at a capture server on
`127.0.0.1` that answers 400 (`ANTHROPIC_API_KEY` is a placeholder), `claude -p hi`. Each server is a tiny stdio probe
(written for this measurement, in the scratch directory) that appends its label to a log when started and answers
`initialize` / `tools/list` with one tool. Plugins loaded with `--plugin-dir`; the wrapped and the flat plugin were also
installed through a local marketplace (`claude plugin marketplace add` + `claude plugin install`) with the same result.

| Plugin MCP file | Started by Claude Code | Tool in the request to the model |
|---|---|---|
| `.mcp.json` `{"mcpServers": {"wrapped": …}}` | `wrapped` | `mcp__plugin_pwrap_wrapped__probe_wrapped` |
| `.mcp.json` `{"flat": …}` (no wrapper) | **`flat`** | **`mcp__plugin_pflat_flat__probe_flat`** |
| `.mcp.json` `{"flathttp": {"type": "http", "url": …}}` | connection attempted to the url (`plugin:phttp:flathttp`) | — (nothing listens) |
| `.mcp.json` `{"$schema": "…", "version": 2, "notaserver": {"description": "x"}, "junkok": …}` | `junkok` only; the other three logged as invalid | `…junkok…` |
| `.mcp.json` `{"mcpServers": {"mixedinner": …}, "mixedouter": …}` | `mixedinner` only | `…mixedinner…` |
| `.mcp.json` `{"mcpServers": {}, "emptyouter": …}` | none | — |
| `.mcp.json` `{"mcpServers": null, "nullouter": …}` | `nullouter` | `…nullouter…` |
| `.mcp.json` `{"mcpServers": false, "falseouter": …}` | `falseouter` | — (connection seen in the debug log) |
| `.mcp.json` `{"mcpServers": 0, "zeroouter": …}` / `{"mcpServers": "", "strouter": …}` | `zeroouter` / `strouter` | — (probe log) |
| `.mcp.json` `{"MCPSERVERS": {}, "caseouter": …}` | `caseouter` (`MCPSERVERS` logged as an invalid server) | — (probe log) |
| `.mcp.json` `{"McpServers": {"caseinner": …}}` | none (`McpServers` logged as an invalid server) | — |
| `.mcp.json` `{"": …}` | the server keyed `""` (`plugin:pemptykey:`) | — (connection seen in the debug log) |
| `.mcp.json` `{"mcpServers": [ … ], "arrayouter": …}` | a server named `0` (the array element); `arrayouter` not | — (connection seen in the debug log) |
| `mcp.json` (no dot) `{"mcpServers": {"bare": …}}`, not named in the manifest | none | — |
| manifest `plugin.json` `"mcpServers": {"inline": …}` | `inline` | — (probe log) |
| manifest `plugin.json` `"mcpServers": "./servers.json"`, that file flat `{"pathflat": …}` | `pathflat` | — (probe log) |
| project `.mcp.json` `{"projflat": …}` (no wrapper), `enableAllProjectMcpServers: true` | none: `Does not adhere to MCP server configuration schema` | — |
| project `.mcp.json` `{"mcpServers": {"projwrap": …}}` (control) | `projwrap` | `mcp__projwrap__probe_projwrap` |

So a plugin's servers without the wrapper are started exactly like wrapped ones, under the same `plugin:<plugin>:<key>`
name. The rule is JavaScript's `doc.mcpServers || doc`: the wrapper wins whenever it is present and truthy (an empty
object included), and the whole document is the server map when it is absent or falsy (`null`, `false`, `0`, `""`).
A project-level `.mcp.json` does not accept the flat form (measured). The user-level `~/.claude.json` was not measured
with a flat layout: its top level is the CLI's own settings, and nothing in it is a server map.

### What aguard does with it today (`origin/main` `155865b`, 2026-10-09)

The same four servers P-021 used (`evil` curl into bash, `leak` key read and posted, `preload` `NODE_OPTIONS=--require`,
`fs` an ordinary npx server), in the `.mcp.json` of one plugin installed by the CLI, `scan --json --inbox off`:

| `.mcp.json` | `mcp_servers` | MCP artifacts | Overall | `--fail-on high` |
|---|---|---|---|---|
| wrapped, four servers | 4 | evil 75 `EXEC-001`, fs 100, leak 50 `EXFIL-001` `FS-001`, preload 75 `EXEC-010` | 65 | exit 1 |
| flat, four servers | **0** | **none** | 25 (the plugin tree alone) | exit 1 (the tree's `EXEC-001`) |
| wrapped, `preload` only | 1 | preload 75 `EXEC-010` | 69 | exit 1 |
| flat, `preload` only | **0** | **none** | **100**, "Your Claude Code setup looks safe. No findings. Checked 1 plugin." | **exit 0** |
| `{"MCPSERVERS": {}, "preload": …}` (Claude Code starts `preload`) | **0** | **none** | **100** | exit 0 |
| `{"McpServers": {"preload": …}}` (Claude Code starts nothing) | 1 | preload 100, no units, no hash | 100 | exit 0 |

The last two rows come from Go's struct decoder matching `json:"mcpServers"` case-insensitively, while lookups in detect
use the exact key: a case-variant wrapper is read as the wrapper by collect and found by nobody.

No note in any flat row. The plugin tree reads the raw JSON as text, which is why curl-into-bash still surfaces there; the
preload does not, because `EXEC-010` matches the `KEY=VALUE` shape only the MCP artifact's env unit renders (P-021). For Claude Code
the two layouts are the same servers (previous section; measured with probe servers, not with these).

On this machine (`~/.claude`, same binary): 2 CLI-installed plugins ship a `.mcp.json`, one wrapped and **one flat** (1
server, not collected); the 2 desktop-installed plugin MCP files are both wrapped. The scan reports 27 MCP artifacts, 26
of them from plugins, overall 69.

Consequence: a server a plugin starts in every session, outside the load-time gate, is absent from the inventory, from the
rules, from the content hash the gate and the allowlist key on, and from the judge — and nothing in the report says so.

## Initial direction

Claude Code starts the unwrapped servers, so collect them exactly like wrapped ones: same artifact naming, `MCPServer`
key, content hash, rules and judge pass. Only plugin files; user-level and project-level configs stay as they are.

## Design

**Collect decides the form once and records it; every lookup follows the record.**

- `collectPluginMCP` reads each plugin MCP file through a plugin variant of `mcpServersFrom` that applies Claude Code's
  rule: the top-level member `mcpServers` (exact case, as in JavaScript) is the server map when present and truthy —
  today's path, unchanged; otherwise the document itself is the server map. JavaScript truthiness of a JSON value: `null`,
  `false`, a numeric zero and `""` are falsy, everything else (an empty object included) is truthy.
- In the unwrapped form, a top-level entry is a server when its value is an object with a `command` or a `type` member.
  Every branch of Claude Code's server schema requires one of the two, so this is a superset of what Claude Code starts,
  never a subset: a mismatch costs one extra scanned artifact, never a server that runs unscanned. `"$schema"`,
  `"version"`, a description object and the falsy `mcpServers` member itself are not servers, and are not artifacts.
- Each server becomes the artifact a wrapped one becomes: Name `<key><suffix>`, `MCPServer` = the key, counted in
  `mcp_servers`, names sorted. One new scan-input field, `ArtifactReport.MCPUnwrapped` (`json:"-"`), is `true` for these;
  its zero value is "under `mcpServers`", so every artifact built today, in collect or in a test, reads as before.
- User-level (`~/.claude.json`) and project-level (`.mcp.json`) configs keep today's reader: Claude Code rejects the flat
  form in a project file (measured above), and reading `~/.claude.json`'s top level as servers would invent them.
- detect: one helper picks the server map (`mcpServers`, or the document when `MCPUnwrapped`), and the three readers go
  through it — the rule units (strings + env), `MCPServerKey`'s `""` check, and the content hash (`mcpHashInput`). The
  judge's `mcpExcerpt` takes the artifact and reads `detect.MCPConfigLines(a)`, the same entry. The canonical form hashed
  is unchanged (the entry, redacted, under `domainMCP`), so an unwrapped server's hash equals the hash of the same entry
  wrapped.

Exact case closes a cheap way out: `{"MCPSERVERS": {}, "evil": …}` — Go's struct decoder matches the field
case-insensitively and read it as an empty wrapper, while Claude Code reads `doc.mcpServers` as undefined and starts
`evil`. A plugin file whose wrapper key differs only in case is therefore unwrapped now; its inner servers are not
collected unless they themselves look like a server, which matches what Claude Code starts.

## Done criteria

- [x] `TestCollect_PluginMCPWithoutWrapper` (`internal/collect`, new), table-driven over the shapes measured above, each
  asserting the set of (Name, `MCPServer`, `MCPUnwrapped`) and `mcp_servers`: flat → its servers, suffixed like wrapped
  ones; flat with `$schema` / `version` / a description object → only the server-shaped entries; `mcpServers` `null`,
  `false`, `0`, `""` beside a server → that server; `MCPSERVERS: {}` beside a server → that server; `McpServers` holding
  servers → none; flat key `""` → a server with `MCPServer` `""`; wrapped + a top-level sibling → only the wrapped one;
  `mcpServers: {}` + a sibling → none; a repeated `mcpServers` key → the last one (as `JSON.parse`; added in W2). Red on main
  for every row that should yield an unwrapped server, for the case-variant row and for the repeated-key row
- [x] `TestScan_UnwrappedPluginMCPServerGetsTheRulesAWrappedOneGets` (`cmd/aguard/plugin_mcp_test.go`): P-021's four
  servers written flat, in each of the three plugin channels (CLI, Claude Desktop, Cowork synced) → per server, score,
  scoring rule IDs and `hash` equal the same servers wrapped in the same channel, and the hashes are non-empty. Red on
  main: zero MCP artifacts
- [x] `TestScan_UnwrappedPluginPreloadIsNotAClean100` (same file): a flat plugin file with only `preload` → overall 69,
  `failGate(…, "high")` returns `failExit`. Red on main: 100, `nil`
- [x] `TestDetect_UnwrappedPluginMCPServerIsFoundByItsKey` (`internal/detect/detect_test.go`): an artifact with
  `MCPUnwrapped` over a flat file and the same entry wrapped have equal findings one by one (rule, severity, line,
  snippet), including a server keyed `""` beside a benign decoy keyed by its Name
- [x] `TestPlan_UnwrappedPluginMCPServerGetsTheConfigPass` (`internal/judge/plan_test.go`): a flat plugin server gets a
  planned `ModeMCPConfig` whose Behavior is byte-identical to the wrapped one's
- [x] Reverse assertion: a benign server written flat (`TestScan_BenignPluginMCPServersStayClean`'s four shapes, also in
  the unwrapped form) scores 100 with zero scoring findings in every plugin channel
- [x] Reverse assertion: wrapped plugin files and user-level / project-level configs are byte-identical to today — every
  existing P-021 test, `TestContentHashGolden`, `TestContentHash_SameConfigTwoMachines`, `TestHashGolden`,
  `TestPlan_PerKindDispatch`, `TestScan_ProjectMCPIsActuallyScanned` stay green without a character changed; the main and
  branch binaries give byte-identical `scan --json` (after removing `scanned_at` / `tool_version`) on the wrapped fixtures
  and on a user-level `~/.claude.json` with no `mcpServers` member, and on a project `.mcp.json` written flat
- [x] Real machine: `scan --root ~/.claude --json` before and after; the one flat plugin server appears as an MCP artifact
  with a hash and units; record the changes in artifacts, MCP count, findings, scores and notes as numbers, no names
- [x] `make verify` green; `go version` with no toolchain switch, `go.mod` second line `go 1.23.5`

## Out of scope

- **User-level and project-level MCP configs**: `mcpServersFrom` keeps its reader for them (the diff touches only the
  plugin path); Claude Code rejects the flat form there
- **MCP servers declared in the plugin manifest** (`plugin.json` `mcpServers` inline, a path, an array or an MCPB):
  Claude Code loads them, aguard does not collect them, today or after this change — a separate follow-up
- **`mcpServers` as an array**: Claude Code starts the elements as servers named `0`, `1`, …; aguard keeps today's
  parse-error artifact for it (not silent) — a separate follow-up
- **The bare `mcp.json`**: aguard reads it at the plugin root, Claude Code 2.1.107 does not unless the manifest names it;
  whether to keep reading it is not changed here (both files go through the same reader, as today)
- **No hash definition change**: `internal/collect/hash.go` / `hash_test.go` diff empty; `domainMCP` and the canonical
  form of an entry unchanged
- **No rule, severity or scoring change**: `internal/detect/rules_data.go`, `internal/score`, `docs/rules.md` diff empty
- `check <plugin-dir>` still does not split out servers (P-021 open question 2); the gate, reputation and report
  packages are untouched; no dependency, `go.mod` / `go.sum` untouched

## Must not claim

- **Not "every MCP server a plugin ships is now collected"**: manifest-declared servers and the array form are not (Out of
  scope); this covers the plugin MCP file without the wrapper
- **Not "aguard validates server entries like Claude Code"**: the unwrapped filter is a necessary condition (`command` or
  `type`), deliberately looser than Claude Code's schema; an entry Claude Code rejects can still be scanned
- **Not "the server is audited"**: as in P-021, the rules read the config entry, not the server's code
- **Not "the gate covers it"**: plugin MCP servers have no load event; `SessionStart` lists them, it does not block them
- **Not "the score on this machine got worse / better because of risk"**: the extra artifact enters the environment average;
  a clean one raises the average and a risky one lowers it, which is the averaging rule, not a new judgement

## Work items

| W | In one sentence | Commit message (no sha; rebase changes it) |
|---|---|---|
| 1 | Red tests in collect, detect, judge and cmd for the unwrapped form, plus the benign reverse assertion | `collect, detect, judge, cmd: tests — a plugin's MCP servers listed without the mcpServers wrapper are never collected (P-029)` |
| 2 | collect reads plugin MCP files with Claude Code's `doc.mcpServers \|\| doc` rule and records `MCPUnwrapped`; detect's units, key and content hash read the entry from the map it names | `collect, detect: a plugin's MCP servers listed without the mcpServers wrapper are collected and scanned like wrapped ones (P-029)` |
| 3 | The judge's MCP config pass reads the same entry through `detect.MCPConfigLines` | `judge: the MCP config pass reads an unwrapped plugin server's entry instead of finding nothing (P-029)` |
| 4 | spec §4 and the data model, `detect.md` (net zero lines), ROADMAP | `docs: spec, detect.md and ROADMAP say a plugin's unwrapped MCP servers are collected, and what still is not (P-029)` |
| 5 | This file, the index | `proposals: P-029 (P-029)` |

## Open questions

1. **Collect the unwrapped servers, or only disclose the file?**
   **Recommendation**: collect. Claude Code starts them (measured: process started, tool offered to the model, installed
   and `--plugin-dir` alike), so a note would describe a live server as "not understood" while scoring nothing.
   **Decided (2026-10-10)**: as recommended.
2. **Which top-level entries count as servers?**
   **Recommendation**: an object with a `command` or `type` member — the necessary condition of Claude Code's schema. A
   full schema copy would drift with every Claude Code release, and any place it is stricter than Claude Code is a server
   that runs unscanned; a looser filter costs at most a scanned artifact Claude Code rejects.
   **Decided (2026-10-10)**: as recommended.
3. **Entries skipped in the unwrapped form — note or not?**
   **Recommendation**: no note. They cannot start (no `command`, no `type`), so nothing is missed; a note on every
   `$schema` line would teach operators to skip notes. The file is still read as text in the plugin tree.
   **Decided (2026-10-10)**: as recommended.
4. **Exact-case wrapper key, although Go's decoder accepted `McpServers` until now?**
   **Recommendation**: exact case, as Claude Code reads it. The case-insensitive read was an evasion
   (`MCPSERVERS: {}` hid a server Claude Code starts) and invented a server Claude Code never starts (scored 100 with no
   units and no hash). Only plugin files change; user-level and project-level reading is untouched.
   **Decided (2026-10-10)**: as recommended.
5. **A field on the artifact, or re-derive the form from the file at each lookup?**
   **Recommendation**: a field (`MCPUnwrapped`, not serialized), set where the form is decided, as `MCPServer` is. Three
   readers re-deriving it would be three copies of the rule; the zero value keeps every existing artifact as it is.
   **Decided (2026-10-10)**: as recommended.

## Done

Run by hand (the fixtures of "Problem", one binary built each from `origin/main` `155865b` and from this branch, one
process per step, 2026-10-10):

```
                                               Before (main 155865b)                        After (this branch)
flat, four servers (CLI plugin)                mcp_servers 0, no MCP artifact, overall 25   mcp_servers 4: evil 75 EXEC-001, fs 100, leak 50 EXFIL-001 FS-001,
                                                                                            preload 75 EXEC-010 — equal to the wrapped file, hashes included; overall 65
flat, preload only                             100, "looks safe", --fail-on high exit 0     69, --fail-on high exit 1
{"MCPSERVERS": {}, "preload": …}               0 servers, 100                               preload 75 EXEC-010, overall 69
{"McpServers": {"preload": …}}                 1 server, 100, no units, no hash             0 servers (Claude Code starts none)
probe plugins installed for Claude Code        mcp_servers 1                                mcp_servers 2 — the two servers `claude mcp list` shows
wrapped plugin, user-level ~/.claude.json      —                                            JSON byte-identical to before after removing scanned_at / tool_version
(with and without mcpServers), project .mcp.json (flat and wrapped)
```

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-029 after the merge)
Released: pending release
Evidence: measured Claude Code 2.1.107 (isolated CLAUDE_CONFIG_DIR and HOME, env -i, loopback capture server answering 400, probe stdio servers): a plugin .mcp.json without the wrapper starts its servers as plugin:<plugin>:<key>, through --plugin-dir and through a marketplace install alike; the table in "Problem" lists the 19 layouts and what each started
Evidence: TestCollect_PluginMCPWithoutWrapper (internal/collect/pluginmcp_unwrapped_test.go); red on the W1 commit in 12 rows (flat, non-server members, mcpServers null / false / 0 / 0.0 / "", MCPSERVERS {}, McpServers holding servers, key "", bare mcp.json, repeated wrapper) → W2 green; the four wrapped rows green before and after
Evidence: TestScan_UnwrappedPluginMCPServerGetsTheRulesAWrappedOneGets (cmd/aguard/plugin_mcp_test.go); red on W1 "0 MCP artifacts from the unwrapped file, want 4" in all three plugin channels → W2 green, every server's score, rules and non-empty hash equal to the wrapped file's
Evidence: TestScan_UnwrappedPluginPreloadIsNotAClean100 (same file); red "overall = 100, want 69" and "--fail-on high passed …" → W2 green
Evidence: TestDetect_UnwrappedPluginMCPServerIsFoundByItsKey (internal/detect/unwrapped_mcp_test.go); red in 4 rows, got: null and hash "" → W2 green
Evidence: TestPlan_UnwrappedPluginMCPServerGetsTheConfigPass (internal/judge/plan_test.go); red "no config pass planned" for "weather (plugin p@mkt)" and " (plugin p@mkt)" → W3 green
Evidence: mutation check (changed temporarily, run, reverted, not committed): mcpServerShaped always true → 3 collect rows red; jsTruthy treating null and numbers as truthy → the null / 0 / 0.0 rows red; MCPServerKey's "" check on the wrapped map → the detect "" row and the judge "" row red; mcpHashInput on the wrapped map → all 4 detect hash checks red
Evidence: reverse assertion TestScan_BenignUnwrappedPluginMCPServersStayClean (cmd/aguard/plugin_mcp_test.go): the four benign shapes listed without the wrapper are 100 with no scoring finding in all three plugin channels (red on W1 only because nothing was collected); TestCollect_UserAndProjectMCPAreNeverReadUnwrapped green before and after; green without a character changed: every P-021 test, TestContentHashGolden, TestContentHash_SameConfigTwoMachines, TestHashGolden, TestPlan_PerKindDispatch, TestScan_ProjectMCPIsActuallyScanned, TestDetect_MCPEnvInjectsCode, TestDetect_MCPConfigURLIsNotEgress; the only edited test line is the egress test's mcpExcerpt call, whose signature now takes the artifact
Evidence: main vs branch binary, scan --json --inbox off, after removing scanned_at / tool_version: byte-identical on the wrapped plugin fixtures (four servers, preload only), a user-level ~/.claude.json with and without mcpServers, and project .mcp.json files flat and wrapped
Evidence: on a real machine ~/.claude (the main and branch binaries back to back, scan --json): artifacts 181 → 182, MCP 27 → 28, the added one is the flat plugin's server, 100, no finding, with a hash; temporary probe (not committed) counting units: 28/28 MCP artifacts have units, the unwrapped one included; every other artifact, the notes and the overall (69 → 69) unchanged; --quiet prints nothing and exits 0 both times (a first pair an hour apart differed also in 3 memory hashes: those files changed between the runs, the back-to-back pair shows 0)
Evidence: Out of scope — git diff --stat origin/main -- internal/collect/hash.go internal/collect/hash_test.go internal/detect/rules_data.go internal/score internal/gate internal/reputation internal/report docs/rules.md go.mod go.sum is empty; collect's user-level and project-level reading (collectMCP, RootMCPConfigs, mcpServersFrom's decoder) is unchanged
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); go.mod second line go 1.23.5
```
