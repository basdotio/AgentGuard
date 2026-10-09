<!-- SPDX-License-Identifier: MIT -->
# 017 — The summary contradicts itself: checking a file lists findings while saying "Nothing was found to check"; parts of loaded content that were not read leave "looks safe" untouched

- **Source**: follow-ups recorded by P-013 (its Out of scope (a)(b), the last item of Must not claim, open question 8;
  decided by the maintainer on 2026-10-09 to combine them into one, opened separately after P-013 merged)
- **Depends on**: P-013 (merged into `main`)
- **Branch**: `p/017-checked-line-and-scan-notes`

<!-- No "Status" line: the directory the file sits in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The report's Summary is the half that a reader who does not write code actually reads. It has two sentences: the
headline (one sentence derived from the level band + counts) and the Checked line (what was checked). Measured with a
binary built from this repository's `main` (`fd28344`), both sentences can contradict what **the same report** lists
below them. All fixtures are built on the spot in temp directories.

**1. The Checked line is derived only from the collector's inventory counts (`EnvSummary`), and several kinds of scanned
things are not in the inventory.** So:

| Target | Also in the report | The Summary's second sentence |
|---|---|---|
| `check install.sh` (`curl … \| bash`) | `EXEC-001` high, "There are problems you should fix…" | `Nothing was found to check under this root.` |
| `check hello.sh` (clean) | "Your Claude Code setup looks safe." | `Nothing was found to check under this root.` |
| `check tool/` (an ordinary directory, with the same `install.sh`) | `EXEC-001` high | `Nothing was found to check under this root.` |
| `scan --root` on a `.claude` holding only `CLAUDE.md` (with `curl … \| bash`) | `EXEC-001`, 69/100 Elevated | `Nothing was found to check under this root.` |
| `scan --root` on a `.claude` holding only `settings.json`, which holds only an `env` block | a `permission:settings env` artifact | `Nothing was found to check under this root.` |
| `scan --root` on a `.claude` whose `settings.json` is `chmod 000` | headline already hedged (P-013), Not checked has an `IO-000` pointing at `settings.json` | `Nothing was found to check under this root.` |

All four renderers — terminal default, `--verbose`, `--md`, HTML (`scan --html`) — say exactly the same. Cause:
`check <single-file>` produces an `instruction` (or `command`) artifact and `check <plain-directory>` produces a
`directory` artifact, and `CollectTarget` gives both an empty `EnvSummary`; a `CLAUDE.md` in a root is of kind
`instruction`, and the `env` block of `settings.json` is of kind `permission`, which is not counted — none of them is in
the inventory, so `checkedParts` counts nothing and `checkedWithGaps` falls through to "Nothing was found". An
unreadable `settings.json` is a scan-level `IO-000` with no artifact, and P-013's "found, not fully checked" names only
notes attached to an artifact, so it falls through to the same sentence too.

A sentence saying "nothing was checked" right next to a high that the same check found leaves the reader to disbelieve
one of the two.

**2. The headline hedges only on "a coverage note attached to an artifact, or an `IO-000` / `PARSE-000` anywhere" (the
set decided by the maintainer in P-013), while detect's coverage notes about loaded content are all scan-level
`COV-000`** (`detect.Engine.Run` gathers each artifact's dimension-0 results into the scan-level notes). Measured:

| Fixture | What was not read | Score / headline |
|---|---|---|
| a skill's `SKILL.md` says "Run sh sub/inner.sh", `sub/` is `chmod 0111`, and inside it is `curl … \| bash` | `COV-000` medium "Entries in this artifact could not be read" | 100 / "Your Claude Code setup looks safe." |
| a 1.1 MB `big.sh` in a skill, ending in `curl … \| bash` | `COV-000` "File too large, content scan skipped" | 100 / "looks safe" |
| a skill's `SKILL.md` says "Run node node_modules/dep/setup.js" | `SUP-004` medium (points into a directory the scan does not read) + `COV-000` "Third-party / VCS trees not read" | 88 / "looks safe. 1 finding needs a look" |
| a hook registered in `settings.json` runs `sh ~/.claude/hooks/missing.sh` | `COV-000` "Hook script not followed" | 100 / "looks safe" |
| `.claude/rules` is a symlink pointing outside the root (a common dotfiles layout) | `COV-000` medium "Entries resolve outside the scanned root, not read" (Why: "They are loaded by Claude Code but were NOT scanned") | 100 / "looks safe" + "Nothing was found to check" |

The first two rows are exactly the two evasion shapes, W-001 and the oversized file: the payload sits where the scanner
cannot read it, the report's headline still says safe, and only the collapsed "Not checked — 1 coverage note(s), highest
medium [COV-000]" line knows. All four renderers have the same headline. Invariant #5 requires "no silent gaps", and
these notes do go into Not checked; but P-013 already defined "looks safe" as the claim "everything Claude Code loads
was read in full", and in every row above something loaded was not read in full — the headline says the wrong thing.

`collect` also has several scan-level `COV-000` of the same kind (unresolvable entries in a load namespace, `rules/` and
the like resolving outside the root, the directory depth cap, the managed-policy instruction file, quarantine escapes,
`@import` out of bounds / refused credential reads / depth cap, the desktop session-cache cap), and they leave the
headline untouched as well.

## Initial direction

Change only the plain-language layer of `internal/report` (`plain.go`) and the places where the three human-facing
renderers call it, deriving from data already in `ScanResult`, adding no thresholds and moving no data: when the
inventory counts nothing, the Checked line counts the artifacts actually scanned instead, and it also names scan-level
`IO-000` / `PARSE-000` as "not fully checked"; the headline's set widens to scan-level coverage notes meaning "loaded
content not read in full", while the few notes for deliberate skips still leave the headline untouched. JSON / SARIF,
the score and the exit code do not change.

## Done criteria

All fixtures are built on the spot in `t.TempDir()` and go through the real pipeline (`checkTarget` / `scanEnv`); for
each of the four human-facing renderers (terminal default, `--verbose`, markdown, HTML) the Summary section is taken
(`summaryOf`, the same cut as in P-013).

- [x] `TestCheckedLineCountsWhatWasScanned` (`cmd/aguard/checked_line_test.go`, new):
  `check <install.sh with curl|bash>`, `check <clean hello.sh>`, `check <commands/deploy.md>`,
  `check <plain-directory>`, `scan` of a root holding only `CLAUDE.md`, `scan` of a root whose `settings.json` holds
  only an `env` block — in all four renderers the Summary does **not** contain `Nothing was found to check`, and
  contains, respectively, `Checked 1 file.` / `Checked 1 file.` / `Checked 1 command.` / `Checked 1 directory.` /
  `Checked 1 file.` / `Checked 1 settings block.`. Red today: all 6 × 4 places say "Nothing was found"
- [x] `TestUnreadableSettingsIsNamedNotFullyChecked` (same file, new): a `chmod 000` `settings.json` (scan-level
  `IO-000`, no artifact) → in all four renderers the Summary contains `Not fully checked:`, `settings.json` and
  `IO-000`, and does not contain `Nothing was found to check`. Red today. Skipped when running as root makes it readable
- [x] `TestLoadedContentLeftUnreadHedgesTheHeadline` (same file, new): five Low-band fixtures — a `chmod 0111`
  subdirectory in a skill (skipped when running as root), a script over 1 MiB in a skill, a `SKILL.md` pointing at a
  file in `node_modules/` (`SUP-004`), a hook running a script that does not exist, `rules/` as a symlink pointing
  outside the root — in all four renderers the Summary contains `Low risk in what was read, but coverage is incomplete.`
  and does not contain `looks safe`. Red today: all 5 × 4 places say "looks safe"
- [x] `TestScanLevelNotesThatHedge` (`internal/report/scan_gaps_test.go`, new): the same clean Low-band result, each
  time with one scan-level note added. Hedges: `IO-000`, `PARSE-000`, every kind of detect and collect `COV-000` about
  loaded content (unreadable entries, oversized file, non-regular file, hook script not followed, permission-grant
  script not followed, resolves outside the root, managed-policy file), an artifact carrying `SUP-004`. Does **not**
  hedge (reverse): the four deliberate-skip notes (unclaimed top-level entries, empty root, third-party / VCS trees,
  hook script attributed to its plugin), `LLM-000`, `LLM-002`, `LLM-005`, `GATE-001`, `REP-GOOD`, `IGN-000`. Red today
  on the hedging half
- [x] Reverse assertion `TestDeliberateSkipsKeepTheHeadline` (`cmd/aguard/checked_line_test.go`, new, green today): real
  pipeline; a `node_modules/` in a skill that nothing points to, a plugin's hook script found in the plugin tree
  (`Hook script attributed to its plugin`), an empty root, a clean skill — all four renderers still say
  `Your Claude Code setup looks safe.`; the notes of the first two are still in Not checked (P-013's
  `notCheckedMarker`); the clean skill still reads `Checked 1 skill.`, and the empty root still reads
  `Nothing was found to check under this root.`. Still green after the fix without a single change
- [x] Reverse assertion: P-013's `TestCleanSettingsReportIsUnchanged` (byte-for-byte golden of a clean config in
  terminal default / `--verbose` / markdown), `TestUnownedEntriesKeepTheHeadline`,
  `TestUnreadableSettingsHedgesTheHeadline`, `TestSummaryDoesNotCallIncompleteCoverageSafe`, `TestCoverageVerdict`,
  `TestCheckedWithGaps`, and `TestCheckedLine`, `TestVerdictSentence` stay green without a single change
  (`git diff origin/main -- <these-test-files>` is empty)
- [x] Binaries on a real machine: one built each from this repository's `main` and this branch; for every fixture above,
  the `diff` of the `--json` (without `scanned_at`) and `--sarif` output is 0 bytes (the data does not move)
- [x] `~/.claude` on a real machine (`scan --inbox off`): the terminal output differs from `main` only in the Checked
  line (the scan-level `PARSE-000` is named); the headline does not change (not in the Low band)
- [x] `make verify` green; `go version` does not switch toolchains, line 2 of `go.mod` is still `go 1.23.5`

## Out of scope

- **No data moves**: `ScanResult`, `model`, and the JSON / SARIF bytes do not change. detect still puts its coverage
  notes at scan level, and the coalescer still merges them; collect still produces the same notes. `collect` / `detect`
  only turn **four existing titles** from package-internal constants / literals into exported constants (the strings
  unchanged to the character), and not a line beyond that changes
- **No change to the score, the exit code, `--fail-on` / `--fail-on-llm`**: dimension 0 never gates
  (`score.Deterministic` and `report.HasAtLeast` do not change); the severity of `SUP-004` does not change
- **No change to the Not checked line**: the count, the highest severity and the rule ID still count all coverage notes
- **No change to the verdict sentence of the other three bands** (Watch / Elevated / Critical), nor to the count clause
- **When the inventory count is not empty, the Checked line does not change by a character**: things the inventory does
  not count, such as `CLAUDE.md`, are not added to a line that has counts (that would change the line on almost every
  real machine, and that line is not wrong today)
- **The Downloads section is not touched** (`inboxAdvice` not looking at an entry's own coverage notes is a separate
  matter, see the follow-ups in Done), **the gate is not touched** (`internal/gate`)
- **collect's empty-root note is not fixed**: when the only loaded content in a root resolves outside the root, collect
  still emits `Nothing to audit under this root`, and the Checked line follows it in saying "Nothing was found to check"
  (the headline does hedge), see open question 7
- `go.mod` / `go.sum` do not change; no dependencies added

## Must not claim

- Do not say "content that cannot be read in full now makes `check` fail": what is fixed is the summary's wording. The
  score, the exit code and `--fail-on` are all unchanged; a skill hiding `curl | bash` in a `chmod 0111` subdirectory is
  still 100/100 and exits 0 — the report now **says** it was not read in full; it does not **block** it
- Do not say "everything not read hedges the headline": the four deliberate-skip notes (unclaimed top-level entries,
  empty root, third-party / VCS trees, hook script attributed to its plugin) leave the headline untouched; so do the
  judge's `LLM-*`, the gate's `GATE-001` and the suppression notes. The headline and the Not checked line are still
  **not** the same set
- Do not say "`node_modules/` now hedges the headline": it does not when nothing points to it; it does only when an
  artifact points the agent into it (`SUP-004`)
- Do not say "the Summary now names everything not read in full": the Checked line names the coverage notes attached to
  an artifact and the scan-level `IO-000` / `PARSE-000`; the scan-level `COV-000` from detect and collect only make the
  headline hedge, and their files are in Not checked
- Do not say "'Nothing was found to check' will not appear any more": an empty root still gets it; so does a root whose
  only loaded content resolves outside the root (open question 7)
- Do not say "the inventory now lists `CLAUDE.md`": only when the inventory counts nothing does the Checked line count
  the scanned artifacts instead

## Work items

| W | In one sentence | Commit message (no sha, a rebase changes it) |
|---|---|---|
| 1 | End-to-end and unit tests, run red; the reverse assertions are green today | `report, cmd: tests — a checked file or directory reads "Nothing was found to check", and loaded content left unread still reads "looks safe" (P-017)` |
| 2 | The Checked line: count the scanned artifacts when the inventory counts nothing; name scan-level `IO-000` / `PARSE-000` as not fully checked | `report: the Checked line counts what was scanned when the inventory counts nothing, and names a file that could not be read or parsed (P-017)` |
| 3 | collect / detect export the title constants of the four deliberate-skip notes, strings unchanged | `collect, detect: export the titles of the four notes that disclose a deliberate skip, so the report can tell them from a gap (P-017)` |
| 4 | The headline set: every scan-level `COV-000` hedges except the four deliberate skips; an artifact carrying `SUP-004` hedges | `report: loaded content the scan did not read takes "looks safe" away wherever its note sits; the four deliberate skips do not (P-017)` |
| 5 | `.claude/rules/report.md`, spec §9, the README and architecture pairs | `docs: report rules, spec §9 and the README and architecture pairs say what the Checked line counts and which scan-level notes hedge the headline (P-017)` |
| 6 | Finishing W4: a single predicate for `IO-000` / `PARSE-000` (shared by the headline and the Checked line); the four exceptions go from a mutable map to a switch, behaviour unchanged | `report: one predicate for a file found and not read, shared by the headline and the Checked line; the deliberate skips are a switch, not a mutable map (P-017)` |
| 7 | This file, the index | `proposals: P-017 (P-017)` |

W6 was added during a self-review after W4 and W5: the two literals `IO-000` / `PARSE-000` were written once each in
`unreadLoaded` and in `scanGaps`, which is exactly the "two copies of a condition kept in sync by a comment" that this
repository keeps rejecting. It is only a refactor; the W1 tests and the mutation check pass the same way before and
after it.

## Open questions

1. **What does the Checked line say when the inventory counts nothing?**
   **Recommendation**: count the artifacts actually scanned (not counting P-013's "not fully checked" items), with one
   fixed noun per kind: `instruction` → file, `directory` → directory, `permission` → settings block, `quarantined` →
   quarantined item, and the rest keep the inventory's own nouns (skill, hook, command…); the order of kinds is fixed
   and independent of artifact order. Do this only when the inventory count is empty, so the line in every report whose
   inventory has counts stays byte-for-byte the same.
   **Rejected alternatives**: (a) always add the kinds the inventory does not count (write "1 file" even when there is a
   skill next to `CLAUDE.md`) — almost every real machine has a `CLAUDE.md`, so it would change that line on every real
   machine, and that line is not wrong today; (b) name the files — each renderer would have to quote a name supplied by
   the scanned directory, and naming is the job of the findings list below; (c) always write "N items" —
   `check install.sh` saying "Checked 1 item." is less understandable than "Checked 1 file.".
   **Decided (2026-10-09)**: as recommended.
2. **Should scan-level `IO-000` / `PARSE-000` be named in the Checked line?**
   **Recommendation**: yes, unconditionally, listed together with the artifacts' own notes (the artifacts' first, the
   scan-level ones after, under the same 3-item cap). They are the other half of P-013's "twin" argument: today the same
   `settings.json` is named when it cannot be parsed (`PARSE-000` on the artifact), but falls through to "Nothing was
   found" when it cannot be read (scan-level `IO-000`); collect's evidence for both notes is a file path. **Cost**:
   `~/.claude` on a real machine has one scan-level `PARSE-000` (a hook entry in a plugin's `hooks.json` was not
   understood), so the Checked line of that Elevated report gains `Not fully checked: hooks.json [PARSE-000].` — it is
   true, and it is recorded in Done. (At design time this was mistakenly written as `settings.json`; measured, it is
   `hooks.json`, and collect's evidence for this note is only the file's base name.) Scan-level `COV-000` are **not**
   named: detect's ones describe part of an item already counted in the inventory (a subdirectory of a skill, a large
   file in a plugin, the second segment of a hook), their evidence paths are relative to the artifact (the
   unreadable-subdirectory one is `.`), and naming them would be noise; they have their own line in Not checked.
   **Decided (2026-10-09)**: as recommended.
3. **Which scan-level `COV-000` hedge the headline?**
   **Recommendation**: all of them, **except** the four notes that disclose a deliberate skip, marked by their producer
   with an exported title constant: unclaimed top-level entries (`collect.UnownedNoteTitle`, decided by the maintainer
   in P-013), empty root (`collect.EmptyRootNoteTitle`: nothing was collected, so there is nothing "loaded but not
   read"), third-party / VCS trees (`detect.GeneratedDirNoteTitle`: not read by design, present on real machines; when
   an artifact points the agent into one, `SUP-004` takes over), hook script attributed to its plugin
   (`detect.HookOwnedNoteTitle`: the content **was read**, only the attribution is incomplete; on a real machine one
   note merged 50 places). Any scan-level `COV-000` added later hedges by default — when it is wrong, the error is
   "saying safe once too few times", not "once too many".
   **Rejected alternatives**: (a) list the hedging titles positively (a dozen-plus constants) — then gaps added later
   would not hedge by default, which is exactly the shape being fixed here; (b) have detect attach its coverage notes to
   artifacts — the attribution changes in both JSON and SARIF, and it would also break up the coalescer's merging (one
   note becomes 50 lines); (c) guess from the shape of the evidence path (relative path = detect's) — collect emits
   relative paths too (the evidence of the `PARSE-000` for a hook entry that was not understood is just
   `settings.json`), and the evidence path is for humans, not a contract.
   **Decided (2026-10-09)**: as recommended.
4. **Does this conflict with what P-013 wrote into `report.md`, "select by rule ID and attachment point, not by matching
   titles"?**
   **Recommendation**: that rule guards against a renderer writing its own title literal, so that the set silently
   drifts as soon as the producer changes the wording. Among scan-level `COV-000`, the rule ID, the attachment point and
   the source are all identical — nothing in the data other than the title can tell "the user's own session records were
   not read" from "a subdirectory in a skill could not be read". Comparing against the producer's **exported constant**
   is what detect's own coalescer already does (the comment on `generatedDirNoteTitle`: shared by the producer and the
   coalescer, so the two sides cannot drift); the renderer still contains no title literal at all, and whoever changes
   the wording changes that same constant. That rule in `report.md` is rewritten to say this.
   **Decided (2026-10-09)**: as recommended.
5. **Should `SUP-004` hedge?** It is a scored finding, not a coverage note.
   **Recommendation**: yes. It is the only scored rule whose meaning is exactly "something loaded was not read" (the
   agent is pointed into a directory that is not read because of its name), and the note next to it is the
   third-party-tree deliberate skip, which does not hedge; without it, the "pointed into `node_modules/`" case would
   still say looks safe. `HOOK-002` and `EXFIL-005` do not need it: each comes with a `COV-000` that hedges. Selected by
   rule ID, static source only.
   **Decided (2026-10-09)**: as recommended.
6. **Do collect's scan-level `COV-000` (unresolvable load-namespace entries, resolving outside the root, the depth cap,
   the managed-policy file, quarantine escapes, the three `@import` ones, the desktop session-cache cap) count?**
   **Recommendation**: yes. They say the same thing (the Why of quite a few of them literally says "loaded by Claude
   Code but were NOT scanned"), they are covered by the rule from question 3 as is, and no extra code is needed. The
   managed-policy file is always there on managed machines, so a Low-band headline there will always hedge — that file
   is loaded first in every session and this scan did not read it, so the hedge is true.
   **Decided (2026-10-09)**: as recommended.
7. **When a root holds only a `rules/` symlink resolving outside the root, is the Checked line still "Nothing was found
   to check"?**
   **Recommendation**: not changed in this proposal. In that case collect collects no artifact at all, its own
   `Nothing to audit under this root` note is still emitted, and the Checked line agrees with it; the headline already
   hedges. What would need to change is when collect emits the empty-root note, which is not in the renderer; recorded
   as a follow-up.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #39 (2026-10-09; find the sha with git log --grep P-017)
Released: v0.19.0
Evidence: TestCheckedLineCountsWhatWasScanned (cmd/aguard/checked_line_test.go); W1 red on the renderers of this repository's main: 6 subtests × terminal default / --verbose / markdown / HTML, all 24 places say "Nothing was found to check" (check install.sh, check hello.sh, check commands/deploy.md, check a plain directory, a root with only CLAUDE.md, a settings.json with only an env block) → green after W2: Checked 1 file. / 1 file. / 1 command. / 1 directory. / 1 file. / 1 settings block.
Evidence: TestUnreadableSettingsIsNamedNotFullyChecked (same file); W1 red: all four renderers lack "Not fully checked:", "settings.json", "IO-000" and say "Nothing was found to check" (16 places) → green after W2: "Not fully checked: …/.claude/settings.json [IO-000]." (a code span in markdown)
Evidence: TestLoadedContentLeftUnreadHedgesTheHeadline (same file); W1 red: 5 Low-band fixtures × 4 renderers, 20 places say "looks safe" (curl|bash in a chmod 0111 subdirectory, curl|bash at the end of a 1.1 MB script, SUP-004 pointing into node_modules, a hook running a script that does not exist, a rules/ symlink pointing outside the root) → still red after W2 (only the Checked line changed) → green after W4
Evidence: TestScanLevelNotesThatHedge (internal/report/scan_gaps_test.go); W1 red on 10 rows × 4 renderers (8 kinds of detect / collect scan-level COV-000 + managed policy + SUP-004); the two IO-000 / PARSE-000 rows and the 10 non-hedging rows (the four deliberate skips, LLM-000/002/005, GATE-001, REP-GOOD, IGN-000) green from W1 on → all green after W4
Evidence: TestCheckedLineDerivesFromWhatWasScanned (same file); W1 red on 6 / 9 rows (a single file, the various uncounted kinds in fixed order, a gap does not count as "checked", scan-level IO-000 after the counts / on its own, artifact gaps before scan-level ones) → green after W2; the three rows "scan-level COV-000 not named", "the line unchanged when the inventory has counts" and "nothing at all" green from W1 on
Evidence: reverse assertion TestDeliberateSkipsKeepTheHeadline (cmd/aguard/checked_line_test.go); green from W1 on, still green after W2–W6 without a single change: a node_modules nothing points to, a hook script found in the plugin tree, an empty root, a clean skill — all four renderers say "Your Claude Code setup looks safe.", and the notes of the first two are still in Not checked. Mutation: remove HookOwnedNoteTitle from deliberateSkip → the plugin-hook subtest goes red, and that row of the unit test goes red ×4; remove GeneratedDirNoteTitle → the node_modules subtest goes red; green after restoring
Evidence: reverse assertion — git diff origin/main -- internal/report/artifact_notes_test.go internal/report/plain_test.go internal/report/text_test.go cmd/aguard/artifact_notes_test.go is empty: P-013's TestCleanSettingsReportIsUnchanged (byte-for-byte golden), TestUnownedEntriesKeepTheHeadline, TestUnreadableSettingsHedgesTheHeadline, TestSummaryDoesNotCallIncompleteCoverageSafe, TestCoverageVerdict, TestCheckedWithGaps, and TestCheckedLine, TestVerdictSentence still green without a single change
Evidence: binaries on a real machine — one built each from main (fd28344) and this branch, 19 fixtures × (--json without scanned_at / tool_version, --sarif with the version string normalised), 38 files in all, 0 bytes of difference; the Summary sections (terminal default / --verbose / --md / scan --html) match the criteria above, and the two fixtures with an unreferenced node_modules and a clean skill differ by 0 bytes
Evidence: ~/.claude on a real machine (scan --inbox off, 605 lines, 69/100 Elevated): main and this branch differ only on line 7 — the Checked line gains "Not fully checked: hooks.json [PARSE-000]." (a hook entry in a plugin's hooks.json was not understood, open question 2); the headline does not change. scan --quiet exits 0 with 0 lines of output both before and after
Evidence: Out of scope — git diff --stat origin/main -- internal/model internal/score internal/gate internal/inbox cmd/aguard/main.go cmd/aguard/inbox.go internal/report/sarif.go internal/report/sanitize.go go.mod go.sum is empty; internal/collect and internal/detect have only 6 files, 26+/15-: the four titles turned into exported constants (strings unchanged), plus their comments and references
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5
Follow-ups (not done, named only): (1) collect's empty-root note is still emitted when "the only loaded content resolves outside the root", and the Checked line follows it in saying Nothing was found to check (open question 7); (2) the inboxAdvice of the Downloads section does not look at an entry's own coverage notes — measured (this branch's binary): a skill in Downloads with a chmod 0111 subdirectory containing curl|bash gets 100/100, the advice sentence is "Nothing flagged by the static rules. Install it if you know where it came from.", and that COV-000 is printed on the next line
```
