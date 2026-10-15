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
10. **The judge's direction, said the right way round** (added 2026-10-15 on the maintainer's side, found while writing the
    user guide): `aguard llm setup` said the judge "can only add findings, never remove one or lower the score", and the
    plugin's `references/llm.md` said it "never lowers the deterministic score". The score runs from 100 (safe) to 0, and the
    judge's second number can only go **down** (`OverallEffective = min(Overall, …)`); what it never does is remove a finding or
    make a result look safer. Both now say "the judge can only add findings or raise the risk — it never removes a finding or
    makes a result look safer"; the report's Judge's-view and Judge lines say nothing that points the other way.
11. **Markdown** follows the reading order with every datum: the Why line, the plain judge's view and `Next:` in each finding's
    details, the disclosure in the summary (with the note's text) instead of under Not checked, the target-only locations. The
    judge's identity line stays. HTML is untouched.

## Done criteria

Each new test was committed before the code and run on the base (`1d9ffcb`) with the new API stubbed (never committed); the
reasons are the base's own output.

- [x] `TestText_JudgeViewInPlainWords` (red: the view is not in plain words), `TestText_JudgeVersionsOnlyInVerbose` (red: no short
  Judge line; `prompt_version`, `excerpt_version`, `samples 3`, `model fake` in the default view), `TestText_JudgeLineSuffix` (red:
  no suffix), `TestText_DisclosureIsNotACoverageGap` (red: no disclosure line; `Not checked — 2 coverage note(s), highest medium
  [LLM-002 COV-000]`), `TestText_TargetListsOnlyWhatApplies` (red: `User MCP config`, `Claude Desktop store`, `Desktop session cache`
  listed for a single target in `--verbose` and markdown), `TestText_WhyFollowsTheFormula` (red: no line), `TestText_NextStep`
  (red: no next step, no approve command), `TestText_CleanIsShort` (red: 19-line clean report), `TestMarkdown_ReadingOrder` (red:
  no Why, judge's view, next step or disclosure; LLM-002 under Not checked) — `internal/report`; `TestExplain_IsApply`
  (`internal/score`, red: every row) — all green here
- [x] Real pipeline (`cmd/aguard`): `TestCheckTargetLocations` red on the base for the skill, directory and file targets (every
  root-derived row listed) and for the zip (no "the target only"); green here, `check <root>` keeps the rows and the JSON
  `locations` are unchanged. `TestDefaultReportShowsEveryGroup` (scan, scan `--llm`, check, check `--llm` on a loopback stand-in
  judge) is green on the base and here: every static and judge group keeps its header in the default view.
  `TestLLMSetupStatesTheJudgesDirection` red on the base (`llm setup` says "lower the score", the plugin reference "never
  lowers"), green here
- [x] `TestCleanSettingsReportIsUnchanged`: the two terminal goldens replaced on purpose (the reason is in the test's comment);
  the markdown golden byte for byte unchanged
- [x] **Reverse assertions**: `TestText_FindingsSurviveBothModes`, `TestWriteReport_DefaultStillShowsTheDangerousThing`,
  `TestText_CollapsedNoteCarriesHighestSeverity`, `TestBrokenSettingsIsNotReportedSafe`, `TestArtifactNoteReachesEveryHumanRenderer`,
  `TestMarkdown_AttackerTextIsInert`, every `TestZeroDial_*` and `TestGateCommandsPasteAsPrinted` green unchanged. Tests that
  pinned the old layout were updated deliberately, each with its reason in a comment: the summary helpers read the header block
  (`summaryOf`, `summaryPart`, `checkedOf`), the safe headline is "looks safe" or the ✅ score line, the default single-target view
  names its target in the title instead of a Checked line, `→ … start with` prints above three findings, the inventory, locations
  and judge identity are read from `--verbose`, the triage label in plain words
- [x] Measured, base `1d9ffcb` against this branch (the same binary flags; the judge rows on the loopback stand-in), see the table
  below: exit codes identical on every row; `--json` byte-identical on every row with `scanned_at` and `tool_version` masked; `--sarif`
  identical apart from the driver's `version` string (`v0.25.0` against an unstamped `dev` build); 95 of 95 static groups have a
  header in the default scan report, base and branch
- [x] `make verify` ends with "verify: all gates passed"; `go version` go1.23.5, no toolchain switch; `go.mod` line 2 `go 1.23.5`;
  no new dependency

| Run (real `~/.claude`; fixtures) | default lines, base → branch | `--verbose` lines, base → branch | exit |
|---|---|---|---|
| `check` of a skill with one high | 28 → 16 | 28 → 21 | 1, 1 |
| the same, `--llm` | 31 → 19 | 31 → 26 | 1, 1 |
| `check` of a clean skill | 19 → 2 | 19 → 12 | 0, 0 |
| the same, `--llm` | 21 → 3 | 21 → 16 | 0, 0 |
| `check` of a plain directory with a high | 27 → 14 | 27 → 19 | 1, 1 |
| `check` of a clean file | 21 → 2 | 21 → 12 | 0, 0 |
| `check` of a `.zip` with a high | 24 → 15 | 24 → 20 | 1, 1 |
| `check` of a root-shaped directory | 27 → 15 | 27 → 23 | 1, 1 |
| `scan --root ~/.claude --inbox off` | 665 → 640 | 696 → 699 | 0, 0 |

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
| 5 | `llm setup` and the plugin's judge reference say the judge's direction the right way round (design 10) | `llm setup, plugin: the judge can only raise the risk, never make a result look safer (P-058)` |
| 6 | `.claude/rules/report.md` and `gate.md`, architecture pair, README pair, judge docs pair, spec §9 | `docs: the default report's reading order, the judge's view in plain words, LLM-002 as a disclosure (P-058)` |
| 7 | "Done" in this file, the index | `proposals: P-058 (P-058)` |

## Open questions

1. **Vote share for the judge's view.** The decision asked for "3 of 3"; triage is one call at temperature 0 and its label has no
   count. **Recommendation**: print "(one reading)" now; asking triage `samples` times is a judge change with its own cost and belongs
   in its own proposal.
2. **`check`'s JSON `locations`.** The same root-derived rows are in `check --json` (`model.ScanResult.Locations` says "empty for a
   single-target check", the code fills it). This proposal keeps JSON byte-identical as decided. **Recommendation**: drop them from
   the data in a follow-up that says it changes `check --json`.

## Done

```
Merged: PR <pending> (2026-10-15; find the sha with git log --grep P-058 after the merge)
Released: (fill in after stage 5)
Evidence: TestText_JudgeViewInPlainWords, TestText_NextStep, TestText_WhyFollowsTheFormula, TestText_CleanIsShort,
  TestText_DisclosureIsNotACoverageGap, TestText_TargetListsOnlyWhatApplies (internal/report/reader_test.go);
  TestExplain_IsApply (internal/score/explain_test.go); TestDefaultReportShowsEveryGroup, TestCheckTargetLocations,
  TestLLMSetupStatesTheJudgesDirection (cmd/aguard/reader_test.go); check of a skill with one high 28 → 16 lines, a clean
  skill 19 → 2; JSON byte-identical, exit codes identical; reverse: TestText_FindingsSurviveBothModes,
  TestWriteReport_DefaultStillShowsTheDangerousThing, 95 of 95 groups in the default scan report
```
