<!-- SPDX-License-Identifier: MIT -->
# 013 — When settings.json fails to parse, the terminal and markdown reports say "looks safe": an artifact's own dim-0 note is never rendered

- **Source**: collect attaches `PARSE-000` to an artifact rather than as a scan-level note, and the terminal, markdown
  and HTML render only scan-level notes, so a `settings.json` that does not parse is reported as
  "looks safe … Nothing was found to check" (invariant #5: no omission may be silent).
  Ported from P-054 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/013-artifact-notes-rendered`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

A `.claude` with a broken `settings.json` (content `{"hooks": {"PreToolUse": [ broken`), measured with a binary built
from `main` (`dec64ca`, v0.18.0):

```
$ aguard check …/broken/.claude
AgentGuard scan · root=…/broken/.claude
Risk score 100/100 (Low)

Summary
  Your Claude Code setup looks safe. No findings.
  Nothing was found to check under this root.


✅ No risk findings (static).

Scan details
  …
  Inventory: skills=0 mcp=0 hooks=0 permissions=0 subagents=0 commands=0 plugins=0 connectors=0
$ echo $?
0
```

`--verbose` is identical to the default output word for word; `--md -` likewise says "looks safe" + "Nothing was found
to check", with no "Not checked" section; `scan --root … --html` (`check` has no `--html`) is the same, and `PARSE-000`
appears 0 times on the page. Yet the note is right there in `check --json`:

```
artifacts: [("hook", "settings.json", hash "", score 100, findings [("PARSE-000", dimension 0, "Parse failed, artifact not fully covered")])]
notes: []
```

`check --sarif` carries it too (`PARSE-000`, `properties.artifact = "hook:settings.json"`).

Cause: `collect.withParseError` (`internal/collect/collect.go:580`) attaches `PARSE-000` **to the artifact it creates**,
and does not put it in the scan-level `notes`. The three human-read renderers take dimension-0 notes only from
`ScanResult.Notes` (`writeNotes(w, r.Notes, …)` at `text.go:167`, `splitNotes(r.Notes)` at `markdown.go:79` and
`html.go:264`), and `report.Aggregate` (`aggregate.go:86`) skips every dimension-0 finding — so the artifact's own note
is **printed nowhere**. SARIF prints it (it walks each artifact's findings), JSON prints it (serialised as is), and none
of the three human-facing ones do.

That one config file holds hooks, permissions and env, the surface this tool cares about most; it was not read, yet the
report says "looks safe" and "Nothing was found to check" (it was found, it just could not be read). This is exactly
what invariant #5 ("no omission may be silent") is there to prevent: the note was produced, and the reader cannot see
it. The README's "`--json` / `--html` / `--md` always carry everything" (`README.md:186`) does not hold for html and md
either.

`withParseError` has three call sites: `settings.json` / `settings.local.json` (hook-kind artifact, `collect.go:540`),
MCP configs (`~/.claude.json` and others, mcp kind, `collect.go:476`), and `plugins/installed_plugins.json` (plugin
kind, `plugins.go:144`). Measured on `main`, all three are the same gap: a broken `~/.claude.json` (`{"mcpServers": {`)
and a broken `installed_plugins.json` likewise report "looks safe … Nothing was found to check", and the JSON has one
artifact carrying `PARSE-000` each, with `notes` empty.

Exit code 0 itself is not what this changes: dimension-0 notes never take part in `--fail-on` (`score.Deterministic`);
that is part of invariant #4.

## Initial direction

Change only the three human-read renderers (text / markdown / html in `internal/report`): an artifact's own
dimension-0 notes and the scan-level notes go into the same "Not checked" channel (the same folded line, the same
`--verbose` section, the same `<details>`, the same HTML block). The data does not move — JSON / SARIF already carry
it, and their bytes do not change. The two sentences in the Summary ("looks safe", "Nothing was found to check") need to
know coverage is incomplete, using the existing wording "coverage is incomplete". Score, exit code and `--fail-on` all
stay as they are.

## Done criteria

The fixture is the same `.claude` with two versions of `settings.json`: the **broken** one
`{"hooks": {"PreToolUse": [ broken`, and the **good** one, the same opening written out completely, registering one
`echo ok` hook (on `main` 100/100, zero findings, zero notes, output "looks safe" + "Checked 1 hook.").

- [x] `TestBrokenSettingsIsNotReportedSafe` (`cmd/aguard/artifact_notes_test.go`, new): the broken fixture goes through
  `scanEnv` and through `checkTarget`, and each of the four human-read renderers (terminal default, `--verbose`,
  markdown, HTML) contains `PARSE-000` and the path of `settings.json`, and **does not** contain `looks safe` or
  `Nothing was found to check`. Red today: none of the four renderers has `PARSE-000`
- [x] `TestCheckBrokenSettingsCLI` (same file, new): the stdout of the real binary's `aguard check <broken-fixture>`
  contains `PARSE-000` and not `looks safe`, with **exit code 0**. Red today on the stdout half; the exit-code half is 0
  today and is still 0 after the fix
- [x] `TestArtifactNoteReachesEveryHumanRenderer` (`internal/report/artifact_notes_test.go`, new): a hand-built result
  where an artifact carries its own dim-0 note, with U+202E and ESC in the file name; all four renderers show the note,
  and the output contains **no** U+202E / ESC (invariant #7, the file name comes from the scanned directory). Red today:
  the note is not shown
- [x] `TestSummaryDoesNotCallIncompleteCoverageSafe` (same file, new): Low band + **something Claude Code loads was not
  fully read** (the set decided by the maintainer, see open question 2: a coverage note attached to an artifact, or an
  `IO-000` / `PARSE-000` anywhere) → the Summary of all three renderers says `coverage is incomplete` and not
  `looks safe`. Red today. Three more rows: only a scan-level "top-level entries not read" `COV-000` → still says
  `looks safe`; only `LLM-002` → still says `looks safe`; a scan-level `PARSE-000` (a hook entry not understood) → hedges
- [x] `TestUnownedEntriesKeepTheHeadline` (`cmd/aguard/artifact_notes_test.go`, new): the real pipeline, good fixture +
  a `sessions/a.jsonl` → exactly one scan-level `COV-000`, Low band; all four renderers still say
  `Your Claude Code setup looks safe.`, and the note is still disclosed (the terminal's "Not checked —" line,
  `--verbose`'s "⚠ Scan warnings", markdown's "## Not checked", HTML's `notchecked` block)
- [x] Reverse assertion `TestUnreadableSettingsHedgesTheHeadline` (same file, new): a `settings.json` with `chmod 000` →
  scan-level `IO-000`, no artifact → the headline of all four renderers hedges (skipped when running as root, where the
  file is readable)
- [x] Reverse assertion `TestCleanSettingsReportIsUnchanged` (`cmd/aguard/artifact_notes_test.go`, new, green today): the
  good fixture's terminal default, `--verbose` and markdown output are **byte-for-byte identical** to goldens
  **recorded on this repository's `main`** (only the temporary directory replaced by a placeholder, time and version
  fixed); HTML says `looks safe`, `Checked 1 hook.`, and has no "Not checked" block. Stays green after the fix without a
  single change
- [x] Reverse assertion `TestBrokenSettingsMachineOutputUnchanged` (same file, new, green today): in the broken fixture's
  JSON, `PARSE-000` is still in `artifacts[0].findings` and `notes` is still empty, and SARIF still has this result
  attributed to `hook:settings.json` — the data did not move; the score stays 100; `failGate` passes under both
  `--fail-on high` and `--fail-on low` (dim-0 does not gate). Stays green after the fix without a single change
- [x] Reverse assertion: Low band, no coverage note → still `Your Claude Code setup looks safe.`; only suppression notes
  (`REP-GOOD`/`IGN-000`) → still `looks safe` (a suppression is not a coverage gap, and the Summary already has its own
  line for it); `TestVerdictSentence` and `TestCheckedLine` stay green without a single change
- [x] On a real machine: for the broken and good fixtures, `diff` of the `check --json` / `check --sarif` output (without
  `scanned_at`) between `main` and this branch is 0 bytes; the terminal output for a fixture with only "top-level
  entries not read" and for an empty root is byte-for-byte identical to `main`; the output for the real machine's
  `~/.claude` does not change (if it is not in the Low band or has no note carried by an artifact)
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **Do not move the data**: `collect.withParseError` still attaches `PARSE-000` to the artifact; not one line of `model`,
  `collect`, `detect`, `score` or `analyze` in `cmd/aguard/main.go` changes. JSON / SARIF rendering
  (`internal/report/sarif.go`, the JSON encoding in `main`) does not change
- **Do not change exit codes or the semantics of `--fail-on` / `--fail-on-llm`**: dim-0 notes never gate
  (`score.Deterministic` and `report.HasAtLeast` unchanged)
- **Do not change the score**: the broken fixture is still 100/100 (this artifact was not read, so there are no
  findings; making it cost points is a different contract)
- **Do not change the severity of `PARSE-000`** (still low): changing it would change the JSON
- **Do not change the verdict sentences of the other three bands** (Watch / Elevated / Critical): they are not claims
  of "safe"
- **Do not fix the two wording defects in the Checked sentence; they are combined into one separate proposal, opened
  after this one merges** (decided by the maintainer, 2026-10-09):
  (a) `check <single-script>` / `check <plain-directory>` reports findings while saying
  "Nothing was found to check under this root." (the inventory counts exclude file / directory artifacts, unrelated to
  dim-0 notes);
  (b) for an unreadable `settings.json` (scan-level `IO-000`) the headline already hedges, but the Checked sentence
  still says "Nothing was found to check" (open question 8).
  Both are "the Checked sentence is derived only from the counts the collectors extract", fixed in the same place.
  Coverage notes that detect itself puts at scan level about loaded content not hedging the headline (see "Must not
  claim") also belong to that proposal
- **Do not change how the scan-level notes for things not read by design are shown**: the "top-level entries not read"
  `COV-000` and `LLM-002` are still in the "Not checked" line, with count, highest severity and rule IDs as before; the
  narrowing only changes them so they do **not** affect the headline
- **Do not touch the Downloads section** (`cmd/aguard/inbox.go` already sorts the dim-0 notes of an entry's artifacts
  into `it.Notes`) or **the gate** (`internal/gate`)
- `go.mod` / `go.sum` unchanged; no new dependencies

## Must not claim

- Do not say "a broken config now makes `check` fail": what is fixed is **visibility**, not **blocking**. With the
  report showing the note, `check` still exits 0 under the default `--fail-on high` (and under any `--fail-on`), and CI
  stays green
- Do not say "a broken config now costs points": it is still 100/100; the headline says coverage is incomplete, not that
  there is risk
- Do not say "every case of incomplete reading now names the file": the Summary names only the items whose artifact
  carries its own note; scan-level notes are still folded into the "Not checked" line, with the files in `--verbose` /
  markdown / HTML
- Do not say "the terminal report used to miss scan-level notes": scan-level ones have always been in the "Not checked"
  line; only the kind attached to an artifact was missing
- Do not say `--json` / SARIF used to be incomplete: they have always carried this note
- Do not say "the headline and the 'Not checked' line talk about the same set" (narrowing decided by the maintainer):
  the headline only looks at whether what Claude Code loads was fully read; the
  "Not checked — … coverage is incomplete" line still counts every coverage note. So a Low-band report can show both
  `looks safe` and that line (for example with only a "top-level entries not read" `COV-000`, which is nearly always the
  case on a real machine)
- Do not say "whenever loaded content was not fully read the headline hedges": detect puts its own coverage notes
  (unreadable entries in an artifact, oversized files, hook scripts not followed) at **scan level** with rule ID
  `COV-000`, and under the set decided by the maintainer (notes attached to an artifact + an `IO-000` / `PARSE-000`
  anywhere) they leave the headline alone and go only into "Not checked"

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | End-to-end tests on the broken / good fixtures, renderer unit tests, headline tests, run red; correct one old assertion that pinned the defect as passing; goldens recorded from this repository's `main` | `report, cmd: tests — a settings.json that does not parse renders as "looks safe" with no note in any human report (P-013)` |
| 2 | The three human-read renderers put an artifact's own dim-0 notes and the scan-level notes into one channel; HTML shows the file for a single-evidence note | `report: an artifact's own coverage note reaches the terminal, markdown and HTML reports (P-013)` |
| 3 | Summary: with a coverage note the Low band does not say looks safe; the Checked sentence names the items not fully checked | `report: the summary stops calling a scan safe when coverage is incomplete, and names what was not fully checked (P-013)` |
| 4 | One sentence each in `.claude/rules/report.md`, spec §9, and the README and architecture pairs | `docs: report rules, spec §9 and the README and architecture pairs say artifact-level notes render and the headline follows coverage (P-013)` |
| 5 | Narrowing decided by the maintainer: a result with only a "top-level entries not read" `COV-000` / only `LLM-002` should still say looks safe, run red; an unreadable `settings.json` should still hedge (reverse) | `report, cmd: tests — an unowned top-level entry or the judge's privacy notice takes "looks safe" away from the headline (P-013)` |
| 6 | `coverageVerdict` narrowed: set = coverage notes attached to an artifact + `IO-000` / `PARSE-000` anywhere, defined only in this one place, selected by rule ID and attachment point | `report: only what Claude Code loads and did not fully read takes "looks safe" away; unowned entries and the privacy notice stay in Not checked (P-013)` |
| 7 | `.claude/rules/report.md`, spec §9, and the README and architecture pairs rewritten for the narrowing | `docs: report rules, spec §9 and the README and architecture pairs say only unread loaded content hedges the headline (P-013)` |
| 8 | This file, the index | `proposals: P-013 (P-013)` |

W2–W4 are the former repository's first-round wide set, W5–W7 the narrowing decided by the maintainer; both stretches
are committed in their original order, one commit per W.

## Open questions

1. **At which layer are an artifact's own notes merged in?**
   **Recommendation**: give the three human-read renderers one shared accessor in `internal/report` (scan-level notes
   first, then each artifact's own in artifact order), without moving the data. Moving the data (moving them into
   `ScanResult.Notes` in `analyze`) would change the JSON bytes and the SARIF artifact attribution, and both of those are
   already correct; the Downloads section already works this way (`checkCandidate` sorts dim-0 into the entry notes when
   it builds an entry), the same approach.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
2. **What does the headline say when coverage is incomplete?**
   **Recommendation**: the Low band's `Your Claude Code setup looks safe.` becomes
   `Low risk in what was read, but coverage is incomplete.` when **there is any coverage note**, with the count clause as
   before. "Any coverage note" is **the same set** the "Not checked — N coverage note(s) … coverage is incomplete" line
   counts (the coverage half of `splitNotes`); suppression notes do not count (they already have their own line in the
   Summary). The other three bands are unchanged.
   **Rejected alternative**: change it only when an artifact carries its own note — the same `settings.json` being
   unreadable (`IO-000`, scan level) and unparseable (`PARSE-000`, artifact level) would get opposite headlines.
   **Cost**: a real machine's `~/.claude` nearly always carries a "top-level directories not read" `COV-000`, so the
   headline of a real-machine report in the Low band would always be this sentence; so would an empty root's.
   **Decided (2026-10-09)**: as recommended (former repo).
   **Decided (2026-10-09, by the maintainer)**: narrow it. Only when **something Claude Code actually loads was not
   fully read** does the Low band's "looks safe" become the hedged sentence: `PARSE-000`, `IO-000` (scan-level ones
   count too, for example an unreadable `settings.json`), and coverage notes attached to an artifact (`COV-000` /
   `SCOPE-001` when attached to an artifact; `SCOPE-001` is today a scored dimension-9 finding, not a note, and would
   trigger the same way if it were ever attached to an artifact as dimension 0). The scan-level "top-level entries not
   read" `COV-000` (unowned non-config entries, not read by design) and `LLM-002` (privacy notice) **do not change the
   headline**, and still appear as before in the "Not checked" line. Reason: on a real machine that `COV-000` is nearly
   always present, and the wide rule would put the hedged sentence on nearly every Low-band report, at which point it no
   longer means anything. Implementation: the set is defined only in `coverageVerdict`, selected by rule ID and
   attachment point (artifact / scan), not by title matching; the `IO-000` / `PARSE-000` headline inconsistency feared in
   the "Rejected alternative" above does not arise, because `IO-000` is in the set. W5–W7.
3. **What about "Nothing was found to check under this root."?**
   **Recommendation**: that sentence is derived from the inventory. An artifact carrying its own coverage note means
   "found, but not fully checked", so name it in that sentence: `Not fully checked: <short-path> [<rule-ID>]`, at most 3
   items with the rest counted; when there are inventory counts it follows `Checked …`. When the inventory is empty and
   there is no such artifact the original sentence stays. Scan-level notes are not named here — they are not items in
   the inventory, and the line below already counts them. This is also where the **file name** appears in the default
   view.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
4. **Should the default terminal view expand an artifact's own notes separately?**
   **Recommendation**: no. Fold them into the same line as the scan-level notes (count, highest severity and rule IDs all
   include them); the file name is already in the Summary; the full text is `--verbose`'s job. A separate block would
   amount to inventing a second channel.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
5. **Should HTML's "Not checked" block show the file?**
   **Recommendation**: yes — a note with exactly one piece of evidence shows `— <file>`, the same rule markdown and
   `--verbose` already have (`len(Evidence)==1`). Otherwise in HTML this note is left with only the title
   "Parse failed, artifact not fully covered" and cannot say which file. Side effect: single-evidence scan-level notes in
   HTML also gain the file name; that brings the three renderers into line, it is not a new rule.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
6. **Exit code / `--fail-on`?**
   **Recommendation**: leave them. dim-0 never gates (invariant #4, `score.Deterministic`); after the fix the report
   shows the note and still exits 0 at the default threshold, written into "Must not claim". Making a broken config fail
   the gate would change the `--fail-on` contract, not in this proposal.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
7. **An existing test pinned the defect as passing; what to do?** The sample in `TestText_AggregatesAndLabels`
   (`internal/report/text_test.go`) has a `COV-000` carried by an artifact, and the assertion is "no `COV-000` anywhere
   in the output" — the intent was "does not appear as a risk line", but as written it also asserted "is not shown at
   all".
   **Recommendation**: change it to "absent from the Findings section, present in the Not checked line", changed and
   disclosed in W1.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
8. **What about the unreadable `settings.json` half (`IO-000`, scan level)?** After the fix its headline also says
   `coverage is incomplete`, but the Checked sentence is still "Nothing was found to check under this root." — it has no
   artifact, so it is not in the "found, but not fully checked" list.
   **Recommendation**: not in this proposal; open it separately together with the `check <single-script>` item in "Out
   of scope": both are "the Checked sentence is derived only from the counts the collectors extract", fixed in the same
   place; this proposal's contract ("an artifact's own notes are rendered by someone, and the headline no longer says
   looks safe") is met.
   **Decided (2026-10-09, by the maintainer)**: the two wording defects (this one, and `check <single-script>` /
   `check <plain-directory>` still saying "Nothing was found to check" while reporting findings) are combined into
   **one** separate proposal, opened after this one merges; this proposal does not fix them. After the narrowing
   `IO-000` is still in the headline's set, so the headline half is unchanged.

## Done

```
Merged: PR #28 (2026-10-09; find the sha with git log --grep P-013)
Released: pending release
Evidence: TestBrokenSettingsIsNotReportedSafe (cmd/aguard/artifact_notes_test.go); W1 red on this repository's main renderers: on both the scanEnv and checkTarget paths, all four of terminal default / --verbose / markdown / HTML "does not name the parse failure [PARSE-000]", and all say "looks safe" and "Nothing was found to check" (24 places) → after W2 PARSE-000 reaches the four renderers → after W3 all four green: the Summary is "Low risk in what was read, but coverage is incomplete. No findings." + "Not fully checked: …/.claude/settings.json [PARSE-000]."
Evidence: TestCheckBrokenSettingsCLI (same file); W1 red on stdout (no PARSE-000, has looks safe) → green after W3; exit code 0 before and after the fix
Evidence: TestArtifactNoteReachesEveryHumanRenderer (internal/report/artifact_notes_test.go); W1 red: all four renderers "does not show the artifact's own note" → after W2 only the terminal default red (file name not in the default view, no U+FFFD) → green after W3; U+202E and ESC occur 0 times in all four outputs, U+FFFD is present
Evidence: TestSummaryDoesNotCallIncompleteCoverageSafe (same file); W1 red in 16 places (scan-level IO-000 / artifact-level PARSE-000 × 4 renderers × 2 assertions) → green after W3; the "no note" and "trust decision only" rows green from W1 on, and still green afterwards without a single change
Evidence: narrowing decided by the maintainer — three new rows in the same test; W5 red in 8 places ("unowned top-level entries only" and "judge privacy notice only" × 4 renderers, looks safe = false, want true) → green after W6; the "scan-level PARSE-000" row green before and after; the original four rows not changed at all in W5 / W6, still green. TestCoverageVerdict (added by W3 itself) changed its arguments as coverageVerdict changed to take the whole result; the expected sentence on every row is unchanged
Evidence: TestUnownedEntriesKeepTheHeadline (cmd/aguard/artifact_notes_test.go); W5 red: all four renderers "an unowned top-level entry changed the headline" → green after W6, all four say Your Claude Code setup looks safe., and the Not checked line / Scan warnings / ## Not checked / notchecked block are still present
Evidence: reverse assertion TestUnreadableSettingsHedgesTheHeadline (same file); green already at W5, still green after W6 without a single change; mutation (IO-000 removed from coverageVerdict's scan-level set) → all four renderers red "an unreadable settings.json must hedge the headline", green after revert
Evidence: reverse assertion TestCleanSettingsReportIsUnchanged (same file); goldens freshly recorded at W1 with this repository's main renderers (temporary dump test, not committed), differing from the former repository's goldens only in the rules link at the end of the markdown, blob/main ← blob/dev; green from W1, still green after W2–W7 without a single change (terminal default = --verbose, markdown byte for byte)
Evidence: reverse assertion TestBrokenSettingsMachineOutputUnchanged (same file); green from W1, still green afterwards without a single change: in the JSON PARSE-000 is still in artifacts[0].findings, notes is [], SARIF attributes it to hook:settings.json, overall 100, neither --fail-on high nor low triggers
Evidence: reverse assertion — TestVerdictSentence, TestCheckedLine (internal/report/plain_test.go) unchanged, still green; text_test.go changed only one assertion in TestText_AggregatesAndLabels (open question 7), W1 red "must fold into the Not checked line, not vanish" → green after W2
Evidence: real-machine binaries — one built each from main (dec64ca) and this branch (same -ldflags version string), 7 fixtures (good settings.json, broken settings.json, broken ~/.claude.json, broken installed_plugins.json, top-level entries only, empty root, settings.json with chmod 000) × check --json (scanned_at removed) / check --sarif, 14 files in total, 0-byte difference; the good fixture's terminal default / --verbose / --md (time line removed) / scan --html (meta line removed) 0-byte difference; the top-level-entries-only fixture and the empty root: terminal default / --verbose / --md / scan output 0-byte difference, and their HTML only gains the file name of single-evidence notes (open question 5)
Evidence: real-machine binaries — the three withParseError call sites, broken settings.json, broken ~/.claude.json and broken installed_plugins.json, change from "looks safe … Nothing was found to check" to "Low risk in what was read, but coverage is incomplete." + "Not fully checked: <short-path> [PARSE-000].", and [PARSE-000] appears in the Not checked line; markdown gains "## Not checked", HTML gains the notchecked block and prints the file; for the chmod 000 settings.json the headline hedges and the Checked sentence is still "Nothing was found to check" (Out of scope (b)); exit code 0 for all 7 fixtures before and after
Evidence: real machine ~/.claude (scan --inbox off, 175 items, Elevated, 605 lines): 0-byte difference between main and this branch output — not in the Low band, and no note carried by an artifact
Evidence: Out of scope — git diff --stat origin/main -- internal/collect internal/detect internal/score internal/model internal/report/sarif.go internal/report/sanitize.go cmd/aguard/main.go cmd/aguard/inbox.go internal/gate go.mod go.sum is empty
Evidence: port — all 7 patches apply with git am -3 on this repository's main without conflict; this repository's internal/report differs from the former repository's export base in one place only (the branch name of the rules link in the markdown, blob/main ← blob/dev, already reflected in the re-recorded goldens), and v0.16–v0.18 did not touch internal/report again (git log 2b4c2a7..origin/main -- internal/report is empty)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5
```
