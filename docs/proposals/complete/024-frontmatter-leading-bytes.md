<!-- SPDX-License-Identifier: MIT -->
# 024 — A rule file with a blank line or a BOM before its frontmatter is labelled (path-scoped) in the report, while Claude Code loads it every session

- **Source**: P-015 open question 5 (`docs/proposals/complete/015-rules-frontmatter-first.md`). On 2026-10-09 the maintainer
  approved opening a separate proposal, measuring `SKILL.md` first
- **Depends on**: none
- **Branch**: `p/024-frontmatter-leading-bytes`

<!-- No "Status" line: the directory the file is in is its status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Before looking for the opening `---`, `splitFrontmatter` in `internal/parse/frontmatter.go` first strips a UTF-8 BOM and
leading whitespace (spaces, tabs, blank lines). `collectRules` uses it to decide whether a `rules/**/*.md` has `paths:`,
and if it does, appends ` (path-scoped)` to the artifact name.

P-015 measured on Claude Code 2.1.107: for a rule file with one blank line or one BOM before `---`, `paths:` is ignored and
the file is loaded at the start of every session. So for these two shapes aguard says `(path-scoped)`, while the agent
actually reads the file every session. That name answers exactly "how much context does it take" — the comment on
`collectRules` says "loaded every session" and "loaded only when a matching file is opened" deserve different reactions
from the reader — and on this point the report understates.

The same function also reads `name`/`description` for `SKILL.md` (and for subagents and slash commands). Whether Claude
Code honours the frontmatter of a `SKILL.md` with a BOM or a blank line before `---` was not measured by P-015: if it does
not, the description aguard uses for context_bloat, duplicate descriptions and the judge's "declared purpose" is not the one
the agent actually sees.

### Measured (Claude Code 2.1.107, this machine, 2026-10-09)

The method follows P-015, plus a capture server that listens only on a loopback address. An isolated `CLAUDE_CONFIG_DIR`
(the user's configuration is not touched), a clean environment started with `env -i`, and `claude -p "hi"` run in a
freshly built project directory:

- **Rules**: an `InstructionsLoaded` hook registered in the user-level settings records each load's `file_path` and
  `load_reason`. Rules are placed both in the user-level `<config>/rules/` and in the project-level `.claude/rules/`, each
  with a `paths:` pointing to a directory that does not exist.
- **`SKILL.md`, slash commands, subagents**: `ANTHROPIC_BASE_URL` points to the capture server on `127.0.0.1`
  (`ANTHROPIC_API_KEY` is a placeholder string, not any real credential; the server records the request body and answers
  400, and the CLI then exits). In the request the CLI sends to the model, the first user message carries the skill list
  (`- <name>: <description>`), and the description of the `Agent` tool carries the subagent list — this is what the model
  sees, without going through the model.

**Leading bytes** (the frontmatter itself is valid; rules give the same result at both levels):

| What is before `---` | Rule: loaded at `session_start`? | `SKILL.md`: description in the list | Command: description in the list | Subagent: in the list? | aguard fd28344 |
|---|---|---|---|---|---|
| nothing, `---` at byte 1 (LF) | no | the frontmatter description | the frontmatter description | yes | reads the frontmatter, labels the rule `(path-scoped)` — matches |
| nothing, CRLF line endings | no | the frontmatter description | — (not measured) | — (not measured) | matches |
| one blank line | **yes** | `---` | `---` | **no** | reads the frontmatter, labels it `(path-scoped)` — **mismatch** |
| UTF-8 BOM | **yes** | `---` | `---` | **no** | same as above — **mismatch** |
| a line of spaces | **yes** | `---` | — (not measured) | — (not measured) | same as above — **mismatch** |
| a line with an HTML comment | yes | that comment line | that comment line | no | does not read the frontmatter — matches |
| a `# Title` line | yes | `Title` (the heading text) | — (not measured) | — (not measured) | does not read the frontmatter — matches |

In all seven shapes the skill is in the list, always named after its directory; when the frontmatter is not honoured,
Claude Code uses the first non-empty line of text in the body as the description.

**The value of `paths:`** (frontmatter at byte 1, user-level rule):

| `paths:` | Loaded at `session_start`? | aguard fd28344 |
|---|---|---|
| list `- "no-such-d/**"`, scalar string, `"a/**, b/**"`, nested list, `"no-such-{e,f}/**"` | no | `(path-scoped)` — matches |
| `paths:` (empty value) | yes | not labelled — matches |
| `[]`, `""`, `- ""`, `- "**"`, `- "**/**"`, `"/**"`, `"**, **"`, `"{**,**}"`, `5`, a mapping | **yes** | `(path-scoped)` — **mismatch** |
| unquoted `**/no-such-g/*.ts` (YAML cannot parse it) | no | not labelled — the opposite direction (the report overcounts context), see open question 6 |

**Source cross-check** (the embedded JS read with `strings`): rules, skills, commands, subagents, output styles and memory
all go through the same function, which matches `/^---\s*\n([\s\S]*?)---\s*\n?/` (without the `m` flag) against the text
read as utf-8 **with the BOM kept**; the rule loader then processes `paths` by "split on depth-0 commas, trim surrounding
whitespace, expand braces, drop a trailing `/**`, discard empty strings", and if what remains is empty or all `**`, treats
the rule as having no `paths`. Both tables above match this code entry for entry.

**aguard fd28344 on the same directories**: `aguard scan --json --inbox off` names the three rules (blank line, BOM,
spaces) and the ten `paths` values in the table above all `(path-scoped)`. Two skills that start with a BOM, with identical
and overlong descriptions, each get a `context_bloat`, and there is also a `duplicate_fn` between them — while the
description Claude Code lists for both is `---`.

## Initial direction

Make the way `parse` decides on frontmatter match the measurement: `---` must be the first three bytes of the file;
`(path-scoped)` is added only when a rule's `paths:`, normalised the way Claude Code normalises it, still has at least one
glob that is not `**`. Measured, `SKILL.md`, commands and subagents likewise do not honour frontmatter after leading bytes,
so the shared parser changes for all of them, and the way these kinds of artifact read frontmatter then matches Claude
Code. The hash only looks at bytes and is not affected.

## Done criteria

All fixtures are built on the spot in `t.TempDir()`, the same way as the existing tests.

- [x] `TestPathScoped_MatchesClaudeCode` (`internal/parse/frontmatter_test.go`, new): one row per rule shape in the two
  tables above (7 leading-byte rows + 16 `paths`-value rows), with the measured result as the expected value. Red on
  fd28344, and the rows that are red are **exactly** the 13 marked "mismatch" in the tables
- [x] Reverse assertion (same test): the LF and CRLF rows with `---` at byte 1, and the five values list, scalar string,
  comma string, nested list and real braces, are `true` before and after the fix; the HTML comment, `# Title` and empty
  value rows are `false` before and after the fix
- [x] `TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt` (`internal/collect/loaded_test.go`, new): through
  `CollectAll`, the names of the rules that start with a blank line, start with a BOM, or have `paths: []` do not carry
  `(path-scoped)`; the one starting at line 1 still does (reverse). The existing `TestCollectRules_RecursiveAndPathScoped`
  is unchanged and still green
- [x] `TestReadSkill_FrontmatterMustStartAtTheFirstByte` (`internal/parse/frontmatter_test.go`, new): a `SKILL.md` that
  starts with a BOM, a blank line, or a line of spaces reads `Name == ""`, `Description == ""`, `Body` contains the
  frontmatter lines (when Claude Code invokes the skill, they are in the body), and `BodyLine` points at the `---` line;
  reverse: LF and CRLF at byte 1 still read the name and description (the existing `TestReadSkill` is unchanged and still
  green)
- [x] The `leading blank + bom` row of `TestSplitFrontmatter` now expects "no frontmatter": the old expectation pinned
  exactly this discrepancy
- [x] `TestContextBloat_OnlyForADescriptionClaudeCodeLists` (`internal/hygiene/hygiene_test.go`, new): a skill that starts
  with a BOM and has an overlong description gets no `context_bloat`; the same content with `---` at byte 1 still does
  (reverse)
- [x] `TestPathScoped_BraceExpansionIsBounded` (new, with a deadline): 40 groups of `{,}` return `false` within 2 seconds
  (every expansion is the empty string), 40 groups of `{x,y}` return `true`; a regression shows up as a hang rather than an
  assertion failure, so the test times itself
- [x] Hash unchanged: `TestHashGolden` green, `git diff --stat origin/main -- internal/collect/hash.go` empty
- [x] Rescan of the measured directories: `aguard scan --json --inbox off` built from this branch gives, on the three
  measured directories above, `(path-scoped)` labels that match the measured tables entry for entry (except the unquoted
  row, open question 6); the two BOM skills no longer get `context_bloat` and `duplicate_fn`, the two that start at byte 1
  still do
- [x] On a real machine: `make build && ./bin/aguard scan --root ~/.claude --quiet --json` compared before and after the
  fix, each difference explained
- [x] `make verify` green; `go version` does not switch toolchains, the second line of `go.mod` is still `go 1.23.5`, no
  dependency added

## Out of scope

- **No change to the canonical hash**: `internal/collect/hash.go` does not change by a byte. The hash only looks at bytes,
  and this change does not touch bytes
- **No change to any detection rule, severity or score formula**: `internal/detect`, `internal/score` untouched. `parse`
  is not on the scoring path (it is used only by the collection labels, hygiene and the judge), so the score and the
  `--fail-on` answer do not change
- **No emulation of Claude Code's description fallback**: without frontmatter it uses the first line of the body as the
  description; aguard's `Description` stays empty (open question 3)
- **No alignment of the closing delimiter and the tail of the opening line**: Claude Code ends the frontmatter at the first
  `---` that appears (even in the middle of a line), and allows the opening `---` to be followed by whitespace before the
  newline; aguard ends it at the first line that starts with `---`. These shapes were not measured and are not in this item
  (open question 6)
- **No port of Claude Code's YAML repair**: when parsing fails it quotes the values of `key: value` lines and parses again,
  so it can read an unquoted `paths: **/x` and a description containing `: `, which aguard cannot. The direction is aguard
  reading less, which is not this item's problem (open question 6)
- **No new note or finding** to report "this frontmatter is ignored by Claude Code" (open question 5)
- **No change to `cmd/aguard/claude_rules_test.go`**: that is P-015's check on this repository's own rule files, and it
  already reports these leading shapes

## Must not claim

- Do not say "every version of Claude Code behaves this way": only 2.1.107 on this machine was measured
- Do not say a rule labelled `(path-scoped)` "loads when a matching file is opened": what was measured is only that it is
  **not** loaded at `session_start` (as in P-015)
- Do not say the frontmatter of output styles, memory or workflows was measured too: the measurement covers only rules,
  `SKILL.md`, slash commands and subagents; that output styles and memory go through the same function comes from reading
  the source, not from a behavioural measurement
- Do not say a `SKILL.md` that starts with a BOM "is not loaded": it is still in the list and can still be invoked, only its
  frontmatter is ignored; what is not in the list is the subagent
- Do not say the score changed: this change does not touch the scoring path, and the score of the scan on a real machine
  is the same before and after

## Work items

| W | In one sentence | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | Tests: leading bytes, `paths` values, how `SKILL.md` is read, context_bloat, the brace-expansion bound; run red | `parse, collect, hygiene: tests — a rule whose frontmatter does not start at the first byte, or whose paths keep no glob Claude Code uses, is labelled path-scoped, and a skill description after a BOM is read (P-024)` |
| 2 | `splitFrontmatter`: `---` must be the first three bytes | `parse: frontmatter is read only when the file starts with ---, as Claude Code reads it, so a blank line or BOM above it no longer makes a rule path-scoped or a skill description count (P-024)` |
| 3 | `PathScoped`: normalise the `paths` value the way Claude Code does | `parse: a rule is path-scoped only when its paths keep a glob Claude Code uses; empty, ** and non-string values load every session (P-024)` |
| 4 | The row in the spec §4 collection table, one guard in `pipeline.md` | `docs: spec and pipeline rules say a rule is path-scoped only on the frontmatter and globs Claude Code honours (P-024)` |
| 5 | This file, the index | `proposals: P-024 (P-024)` |

## Open questions

1. **Are the `paths:` values (`[]`, `""`, `**`, numbers, mappings…) part of this item?**
   **Recommendation**: yes. It is the same problem as the leading bytes — the report says the rule loads by path, and the
   agent reads it every session — and the task's criterion is also written as "label only when Claude Code will honour
   `paths:`", and all ten of these values were measured. A separate proposal would only mean changing the same function
   twice.
   **Decided (2026-10-09)**: as recommended.
2. **Copy the brace expansion as is?**
   **Recommendation**: copy the semantics but not the enumeration: depth first, returning on the first valid glob; cap the
   total number of expansions (1024), and beyond the cap answer "loaded every session". Copying the enumeration would let a
   line with `{a,b}` repeated 40 times drag the scanner into 2^40 expansions — the scanned object must not decide whether
   the scanner stops (the same reasoning as for FIFOs and oversized files in `detect.md`). Over the cap the answer leans to
   "loaded every time": the worst case is the report overcounting context, never undercounting it again. Normal patterns
   return on the first expansion.
   **Decided (2026-10-09)**: as recommended.
3. **Emulate Claude Code's description fallback (the first line of the body as the description when there is no
   frontmatter)?**
   **Recommendation**: no. aguard has always given an empty description for a file without frontmatter; this item only
   makes files whose "frontmatter is ignored" be treated the same as files with "no frontmatter". The fallback text is at
   most 100 characters and cannot trigger context_bloat; emulating it would change how files starting with an HTML comment
   or a heading are read, and those already match today.
   **Decided (2026-10-09)**: as recommended.
4. **Subagents and slash commands use the same parser; change them together?**
   **Recommendation**: yes. Measured, Claude Code does not honour them either (the command's description becomes `---`, the
   subagent does not enter the list at all); the judge's "declared purpose" for them becomes `(not declared)` accordingly,
   matching what the agent sees. Keeping a lenient old parser for them would mean keeping a known discrepancy after it has
   been measured.
   **Decided (2026-10-09)**: as recommended.
5. **Emit a note or finding for "frontmatter ignored by Claude Code"?** (for example a `SKILL.md` that starts with a BOM,
   whose `disable-model-invocation` and `allowed-tools` then do not take effect; a subagent that is not loaded at all)
   **Recommendation**: not in this item. That is a new rule, whose dimension, severity and hit count on real installs all
   need to be decided and measured separately; this item only makes the existing labels and the existing reading tell the
   truth.
   **Decided (2026-10-09)**: as recommended.
6. **And the two discrepancies, the YAML repair and the closing delimiter?**
   **Recommendation**: not in this item, recorded as follow-ups. The former goes in the opposite direction (aguard reads
   less), the latter was not measured; neither has anything to do with "leading bytes".
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #36 (2026-10-09; find the sha with git log --grep P-024)
Released: v0.19.0
Evidence: measured (Claude Code 2.1.107 on this machine, isolated CLAUDE_CONFIG_DIR, claude -p exits after the loopback capture server answers 400): rules via InstructionsLoaded, 7 leading shapes at each of the two levels + 17 paths values at user level; skill list 7 leading shapes, commands and subagents 4 each. The results are the two tables in the "Problem" section
Evidence: W1 red on fd28344, for the reasons the criteria give: TestPathScoped_MatchesClaudeCode red on 13 of 23 rows, exactly the 3 leading shapes + 10 paths values marked "mismatch" in the tables, all "PathScoped = true, want false"; TestPathScoped_BraceExpansionIsBounded's "every expansion empty" red (no expansion, answers true); TestReadSkill_FrontmatterMustStartAtTheFirstByte red for each of the three leading shapes (reads probe/probe description, Body is only "\n# Body\n", BodyLine 4/5/5); TestSplitFrontmatter's leading blank + bom red; TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt red on 3 (blank, bom, nothing all carry (path-scoped)); TestContextBloat_OnlyForADescriptionClaudeCodeLists red (targets = [listed ignored]) → after W2 everything related to leading bytes green, after W3 all green
Evidence: reverse assertions: in TestPathScoped_MatchesClaudeCode the 7 rows LF/CRLF at byte 1, list, scalar string, comma string, nested list, real braces are true before and after the fix, the 3 rows HTML comment, heading, empty value are false before and after the fix; in TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt line1 carries (path-scoped) before and after the fix; TestReadSkill_FrontmatterMustStartAtTheFirstByte's first byte LF/CRLF and the existing TestReadSkill, TestContextBloat, TestCollectRules_RecursiveAndPathScoped unchanged and still green
Evidence: mutation confirms the tests bite (reverted after the run, not committed): remove the expansion cap → BraceExpansionIsBounded times out at 2 seconds "brace expansion is unbounded"; no brace expansion → "braces that expand to **" and "every expansion empty" red; trailing /** not dropped → "**/**", "/**" red; no split on commas → "** twice in one string" red
Evidence: rescan of the measured directories (aguard scan --json --inbox off, 31 rules): labels from the fd28344 build match the measurement 14/31 → this branch 30/31, the remaining one is the unquoted **/no-such-g/*.ts (YAML repair, opposite direction, open question 6); the two skills with overlong descriptions that start with a BOM: fd28344 gives context_bloat ×2 + duplicate_fn ×1 → this branch 0, the two that start at byte 1 still give context_bloat ×2 + duplicate_fn ×1
Evidence: on a real machine, make build && ./bin/aguard scan --root ~/.claude --quiet: no output before or after, exit code 0; the same command with --json compared before and after, identical key by key except scanned_at / tool_version: overall 69 · overall_effective 69 · artifacts 180 · rules 24 · path-scoped 15 · scored findings high 162 / medium 451 / low 193 · notes 10 · hygiene context_bloat 6 / duplicate_fn 11 · inbox 4. Why there is no difference: no .md under ~/.claude has a BOM or whitespace before --- (headers checked file by file, 0 files), and the paths of all 15 path-scoped rules keep a valid glob
Evidence: hash unchanged: TestHashGolden PASS; Out of scope — git diff --stat origin/main -- internal/collect/hash.go internal/detect internal/score go.mod go.sum cmd/aguard/claude_rules_test.go is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod second line go 1.23.5, no new dependencies
```
