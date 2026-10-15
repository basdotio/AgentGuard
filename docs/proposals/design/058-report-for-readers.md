<!-- SPDX-License-Identifier: MIT -->
# 058 — The default terminal report is written for the auditor: a reader who does not write code has to find the one thing to do under scan internals, judge versions and places the scan never looked

- **Source**: the maintainer's review of real reports (2026-10-15), decided as suggested ("start with your suggestions")
- **Depends on**: none
- **Branch**: `p/058-report-for-readers`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The default terminal report (`report.Text`) is the one most people read, and most of them do not write code. Measured on
`v0.25.0` (`1d9ffcb`) against the maintainer's real `~/.claude` (counts only):

- `check` of a skill with one high finding prints 28 lines; the finding is said three times (the verdict line, the
  "what to look at" list, the findings list), and 11 of the 28 lines are scan details. A clean skill prints 19 lines to say
  "nothing to fix".
- `check <skill>` lists four "Where the scan looked" rows that do not apply to a single target: the target is treated as a
  config root, so the report says "Claude Desktop store absent …/skills/<x>/Library/Application Support/…" and "User MCP config
  absent …/skills/.claude.json". `check <file>` adds two "MCP config in the root" rows under the file's own path. None of these
  places is read for a single target.
- With `--llm`, the judge's opinion on a static finding is printed as `⚖ triage (LLM, advisory): likely-benign — …`: jargon,
  and nothing says that it leaves the finding and the score alone. Two lines of judge internals (`prompt_version`,
  `excerpt_version`, model, samples) sit in the summary.
- `LLM-002` — the disclosure that excerpts left the machine for a non-local judge endpoint — is folded into "Not checked … coverage
  is incomplete" as a medium coverage note. It is not a coverage gap, and the line that says where the content went is hidden.
- Nothing says why the score is the number it is (a single high caps the score at 69, the environment number is an average),
  and no finding says what the reader can do next with this tool (which file and line to read, the `aguard approve` command).

## Initial direction

Reorder and shorten the default terminal view only: one block per static finding with the judge's view and a concrete next
step, a derived "Why N" line, a short "Judge:" line, the `LLM-002` disclosure on its own line, scan details behind `--verbose`, a
three-line clean report, and no root-derived locations for a single target. `--verbose`, JSON, SARIF, HTML and markdown keep
every datum; markdown follows the same reading order. JSON and SARIF stay byte-identical.

## Measured (2026-10-15, base `1d9ffcb` = v0.25.0)

Counts and line counts only; no content of the real `~/.claude` enters the repository. The judge runs below used a loopback
stand-in endpoint (labels every triaged rule `likely-benign`, flags nothing), so nothing left the machine.

| Run | default lines | `--verbose` lines | exit |
|---|---|---|---|
| `scan --root ~/.claude --inbox off` (185 items, 95 static groups, 17 hygiene items, 9 coverage notes) | 665 | 696 | 0 |
| `check` of a skill with one high finding | 28 | 28 | 1 |
| the same with `--llm` | 30 | 30 | 1 |
| `check` of a clean skill | 19 | 19 | 0 |
| the same with `--llm` | 21 | 21 | 0 |

What a single target prints under "Where the scan looked" (fixtures, `check <x>`):

| Target | Rows | Rows that apply |
|---|---|---|
| a skill directory | Config root (the skill), User MCP config `<parent>/.claude.json`, Claude Desktop store and session cache under `<parent>/Library/…` | none beyond the target |
| a plain directory | the same four, under the directory's parent | none beyond the target |
| a file | the same four plus two "MCP config in the root" rows at `<file>/.claude.json` and `<file>/.mcp.json` | none beyond the target |
| a `.zip` | Config root (the archive) — `archiveView` already drops the rest | none beyond the target |
| a directory laid out as a root (`check ~/.claude`) | the four rows of a scan | all four: `CollectAll` reads them |

The judge's view on a static finding: the terminal **does** print it today, as `⚖ triage (LLM, advisory): likely-benign — <reason>`,
and so do markdown and HTML. What is missing is plain words and the statement that the view changes nothing. There is **no vote
share to show**: triage is asked once per artifact at temperature 0 (`judge.Triage`), and the label carries no count.

## Design

All of it is in `internal/report` (the plain layer, `plain.go`, and the terminal writer, `text.go`), plus one formula breakdown
exported by `internal/score` and the shell quoting moved to a leaf package. No `model` field changes, so JSON and SARIF do not move.

1. **Header.** `AgentGuard · <name> (<kind>)` when the result is about one target — an artifact whose `Path` is the result's
   `Root` and which is not a plugin's child — else `AgentGuard · <root>`. Then the score line, which keeps its anchor:
   `Risk score N/100 (Level) — <clause>`, the clause derived from the counts already printed (`N finding(s) to look at`,
   `nothing at medium or above, N informational finding(s)`, `no findings in what was read` when coverage is incomplete,
   `nothing to fix`, the last one prefixed `✅`). The level's sentence follows (`coverageVerdict`'s lead, with the coverage hedge),
   then the existing worst-item, sandbox and effective-score lines. The Checked line prints for an environment, and for a single
   target only when something was not fully checked (the title already names what was checked).
2. **One block per finding, numbered.** "What to look at" is the static findings list itself, numbered in report order; the
   separate top-3 list and its `→ … start with` line print only above more than three findings, where an index helps. Each block:
   severity and the dimension's plain phrase; who · what `[RULE]` with `×N in M files` and `[advisory: not confirmed]`; the rule's
   Why; the judge's view (item 3); the next step (item 4); the evidence lines. `in plain terms:` and `what to do:` are folded into
   the first line and into `Next:` — no datum is dropped. The judge's own findings keep their section and anchor.
3. **The judge's view in plain words** (decision 1): `Judge's view (advice; the finding and the score stand): likely a false alarm
   — <reason> (one reading).` — `likely-real` reads `likely real`. "one reading" is the vote share the data holds (open question 1).
4. **Next step** (decision 8), fixed phrases: `read <file> around line N.` for a line-anchored finding, `look at the entry quoted
   below.` for a whole-entry one; then the dimension's existing `actionHint` sentence. `REP-BAD` reads `remove it — this exact
   content is on the known-malicious list.` A line `If you decide to keep it, tell the load-time gate: aguard approve <path>` is
   added once per artifact, under its first medium-or-above static finding, only when every one of these holds: the artifact is a
   skill (the only kind `PreToolUse[Skill]` stops), it has a content hash (P-011 refuses otherwise), none of its findings is
   critical, and it is the target of a `check` or sits inside the scanned root (a Claude Desktop skill is not loaded through the
   gate). The path is quoted for a shell by the gate's own quoting (P-030), moved to `internal/cmdline` so the report can call it
   without importing the gate; for a scan the command carries `--root <root>`, the store the gate reads for that root.
5. **Why N** (decision 7): one line under the list, built from `score.Explain` — the same unit split, mean and cap `score.Apply`
   uses, so the sentence cannot disagree with the number — and from the printed groups (`#k`). One unit: the cap when it bound
   (`a high finding caps the score at 69 (#1); by points alone it would be 75`), else `100 − 25 (#1, high) − 12 (#3, medium); only
   the worst finding of each kind counts`. Several units: the mean, and the cap with the `#` range of the findings at that
   severity. No line at 100.
6. **Judge line** (decision 2): `Judge: checked 1 skill, nothing to add` / `added N lead(s) (below)` / `did not run — <reason>`,
   with failed and never-sent calls kept in the default line (`… ; 2 of 7 calls failed, so its answer is incomplete`). Model,
   samples, `prompt_version`, `excerpt_version` and the long line move to `--verbose`. `report.JudgeLineSuffix` is the one
   extension point: nil here, its text appended to that line when set.
7. **`LLM-002` is a disclosure** (decision 5): `Sent to <host>: redacted excerpts in N call(s).` (host from the judge block's
   endpoint), or `… no call was made` when the judge never sent one. It leaves the Not checked line and the `--verbose` warnings
   block; `--verbose` prints the note's own text under the disclosure. The note stays where it is in JSON and SARIF, and the
   headline never counted it (it is an `LLM-*` note).
8. **Scan details** — locations, inventory, OWASP line, the static-analysis statement — print with `--verbose` only; the default
   ends with one line naming what `--verbose` adds. For a single target the locations are replaced by `the target only` (decision
   6); a root-shaped `check` and `scan` keep every row.
9. **Clean** (decision 9): no static or judge finding, no note other than an `LLM-002`, no trust decision, no Downloads item, no
   sandbox and no judge escalation prints the title, the `✅` score line, the Checked line for an environment, the Judge line and
   the disclosure — two to five lines. Low findings print in full; incomplete coverage keeps its collapsed line with the highest
   severity; a trust decision keeps its block. The hygiene list folds to one line naming `aguard clean` (`--verbose` prints it).
10. **Markdown** follows the reading order with every datum: the Why line, the plain judge's view and `Next:` in each finding's
    details, the disclosure in the summary (with the note's text) instead of under Not checked, the target-only locations. The
    judge's identity line stays. HTML is untouched.

## Done criteria

- [ ] `TestText_JudgeViewInPlainWords`, `TestText_JudgeVersionsOnlyInVerbose`, `TestText_JudgeLineSuffix`,
  `TestText_DisclosureIsNotACoverageGap`, `TestText_TargetListsOnlyWhatApplies`, `TestText_WhyFollowsTheFormula`,
  `TestText_NextStep`, `TestText_CleanIsShort` (`internal/report`), `TestMarkdown_ReadingOrder`, `TestExplain_IsApply`
  (`internal/score`) red on the base, green here
- [ ] Real pipeline: `TestDefaultReportShowsEveryGroup` (`cmd/aguard`) — every static and judge group `report.Aggregate` makes from
  `scanEnv` and `checkTarget` results (a fixture with every severity, a judge run on a loopback stand-in, notes and a trust
  decision) has its header in the default report; `TestCheckTargetLocations` — `check` of a skill, a file, a directory and a zip
  prints no root-derived location in `--verbose` or markdown, while `check <root>` keeps them and the JSON `locations` key is unchanged
- [ ] `TestCleanSettingsReportIsUnchanged`: the terminal goldens are replaced deliberately (default and `--verbose` now differ); the
  markdown golden stays byte for byte
- [ ] **Reverse assertions**: `TestText_FindingsSurviveBothModes`, `TestWriteReport_DefaultStillShowsTheDangerousThing`,
  `TestText_CollapsedNoteCarriesHighestSeverity`, `TestBrokenSettingsIsNotReportedSafe`, the hedge tests and every `TestZeroDial_*`
  green; `TestGateCommandsPasteAsPrinted` green with the quoting moved
- [ ] Measured on the real `~/.claude` and the fixtures, base against branch: the line counts above; the number of static and judge
  group headers in the default report equals the number of groups; exit codes unchanged; `--json` and `--sarif` byte-identical
  apart from `scanned_at` (and `tool_version`'s commit)
- [ ] `make verify` ends with "verify: all gates passed"; `go version` go1.23.5, no toolchain switch; `go.mod` line 2 `go 1.23.5`

## Out of scope

- **JSON, SARIF, HTML**: no change. `check`'s JSON keeps the root-derived `locations` rows (open question 2); HTML keeps `LLM-002`
  under Not checked and its current order
- **Asking triage more than once**: the judge, its prompts and its call count are untouched (open question 1)
- **The Downloads section, `clean`, the gate's messages**: unchanged; the gate's quoting moves, its output does not
- **Rule text**: no rule, title or Why changes; `make docs` has nothing to regenerate
- **Anything appended to the Judge line**: this proposal adds the extension point (`report.JudgeLineSuffix`) and sets nothing on it

## Must not claim

- Not that the judge "cleared", "dismissed", "lowered" or "downgraded" a finding: its view is printed beside a finding that still
  stands and still counts (invariant #4)
- Not a vote share the data does not hold: triage is one reading
- Not "the report is shorter" for a scan with many findings: findings are never folded, so a long list stays long; the shortening
  is in the clean case, a single target, and the moved details
- Not that `aguard approve` makes an artifact safe: it records a decision for the load-time gate and changes no score

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The tests above, red on the base | `report: tests — …`, `score: tests — …`, `cmd: tests — …` (P-058) |
| 2 | Shell quoting moved to `internal/cmdline`, the gate delegating to it | `cmdline: the gate's shell quoting, callable from the report (P-058)` |
| 3 | `score.Explain` shares `overallScore`'s working | `score: the formula's working, for a report to say why (P-058)` |
| 4 | The default terminal report and markdown as designed; goldens replaced | `report: the default terminal report is written for the reader who does not write code (P-058)` |
| 5 | `.claude/rules/report.md`, architecture pair, README pair where they show the report | `docs: the default report's reading order (P-058)` |
| 6 | "Done" in this file, the index | `proposals: P-058 (P-058)` |

## Open questions

1. **Vote share for the judge's view.** The decision asked for "3 of 3"; triage is one call at temperature 0 and its label has no
   count. **Recommendation**: print "(one reading)" now; asking triage `samples` times is a judge change with its own cost and belongs
   in its own proposal.
2. **`check`'s JSON `locations`.** The same root-derived rows are in `check --json` (`model.ScanResult.Locations` says "empty for a
   single-target check", the code fills it). This proposal keeps JSON byte-identical as decided. **Recommendation**: drop them from
   the data in a follow-up that says it changes `check --json`.
