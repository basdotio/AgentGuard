<!-- SPDX-License-Identifier: MIT -->
# 038 — `check --llm --fail-on-llm` passes a plugin or a plain directory the judge asked nothing about: zero questions read as "looked and found nothing"

- **Source**: new finding (2026-10-10), measured on v0.20.1+ (`6cab205`); follow-up to P-026
- **Depends on**: P-026 (exit 4 = "the LLM gate could not be evaluated")
- **Branch**: `p/038-judge-asked-nothing`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`--fail-on-llm` exits 4 when the judge could not answer (P-026): it did not run, a call failed, or a call was never made. It
does not see a judge that was never asked. `judge.planFor` has a pass for skills, the instruction-like kinds, connectors, hooks
and MCP servers, and **none** for `plugin`, `directory` or `quarantined` (nor `permission`, by design): for those the only
call is triage of their static findings, and with no static finding there is no call at all. `judgeGap` reads only `Ran`,
`Failed` and `Skipped`, so a run that planned zero questions about the target is "the judge answered everything" — exit 0,
and the report carries no note saying the judge never looked.

Measured with `authority: escalate`, `samples: 1`, `base_url` on a closed local port, `--fail-on-llm high --fail-on critical`:

| Target | `judge` block | exit |
|---|---|---|
| a plugin directory (`.claude-plugin/plugin.json` + `skills/p1/SKILL.md` whose body asks the agent to quietly send the user's home directory to an address in config) | `ran: true, calls: 0, failed: 0` | **0** |
| a plain directory (`NOTES.md` with the same sentence + a benign `build.sh`) | `ran: true, calls: 0, failed: 0` | **0** |
| the same `SKILL.md` checked as a skill (control) | 2 calls, 2 failed | 4 |

A pull-request job that runs `aguard check ./plugin --llm --fail-on-llm high` goes green on exactly the content the judge
exists to read: a rewritten instruction no regex matches.

## Initial direction

Disclose, on every `--llm` run, the artifacts whose kind the judge has no question for (one `LLM-000` note), and make the
`--fail-on-llm` gate not evaluable (exit 4, P-026's stderr line) when the target of a `check` is one of them. Record what the
judge *should* ask about a plugin tree or an unrecognised directory as an issue; that is a design question, not decided here.

### Measured on the real `~/.claude` (same binary, `scan --root ~/.claude --inbox off --llm`)

184 artifacts. `aguard llm preview` gives the plan per artifact: every skill (66), MCP server (28), rule (24), memory file (21),
hook (29) and connector (2) gets at least one judge question; **8 artifacts get no call at all** (7 plugins, 1 directory under
`skills/` without a `SKILL.md`) and **6 get triage only** (4 plugins, 2 permission blocks). The premise that a scan judges a
plugin's skills as their own artifacts does not hold for CLI-installed plugins: `collectPlugins` emits the plugin as one
`plugin` artifact (the whole tree, read by the static rules) plus its hooks and MCP servers; the 11 plugin trees hold 252
`SKILL.md`, 171 command and 66 agent markdown files, and none of them is an artifact of its own (only the Claude Desktop skills
bundles are collected per skill). With a working local stub endpoint, `scan … --llm --fail-on-llm critical` makes 300 calls and
exits 0; with the closed port, 300 calls fail.

A related shape: a plugin whose tree has one static finding below the threshold gets one triage call, and with a working
endpoint `check` exits 0 — the triage label answered, the sentence was never put to the model (measured with a
`subprocess.run` → `EXEC-004` medium beside the same `SKILL.md`).

## Done criteria

- [x] `TestFailOnLLM_TargetTheJudgeAsksNothingAbout` (`cmd/aguard/fail_on_llm_unasked_test.go`, new, drives the built binary,
  `check … --llm --fail-on-llm high --fail-on critical --json --quiet`): a plugin directory, the same with a trailing slash, the
  same as a `.zip`, a plain directory, and a plugin whose only static finding is one medium (triage only) exit **4** with one
  stderr line carrying `(exit 4)`, `asked nothing about the target` and the kind, against a working stub endpoint and (plugin
  directory) a closed port; each report carries one `LLM-000` naming the kind. Red on the base: exit 0, empty stderr, no
  `LLM-000`
- [x] `TestRun_NamesTheArtifactsNoPassCovers` (`internal/judge/unasked_test.go`): `judge.Run` over a skill, a plugin, a
  directory, a quarantined entry and a permission block returns exactly one `LLM-000` counting `plugin`, `directory` and
  `quarantined` (and not `permission`); over judged kinds only it returns none. Red on the base: no such note
- [x] `TestAsksNothingOf_FollowsPlanFor` (same file): for **every** `ArtifactKind` constant declared in `internal/model/model.go`
  (read from the source, so a new kind without a row fails), `planFor` over a fixture of that kind plans at least one judge
  question exactly when `judge.AsksNothingOf(kind)` is false — `permission` being the one kind with neither
- [x] `TestFailGate_TargetTheJudgeAsksNothingAbout` (unit, same cmd file): a result whose artifact at the checked path is a plugin,
  judge ran fully → 4; the same plugin in a scan-shaped result (no artifact at the root path) → 0; a deterministic hit → 1;
  `--fail-on` alone → 0; a judge that did not run → 4 with P-026's reason, not this one
- [x] **Reverse assertions** (same binary test): a skill target with a working stub and no finding exits **0** with an empty
  stderr and no `LLM-000`; the plugin directory under `--llm --fail-on high` with no `--fail-on-llm` exits 0 (base: 0), closed
  port or working stub; the triage-only plugin under `--fail-on-llm medium` exits 1 (a fired gate beats 4); `scan --llm
  --fail-on-llm high` over a root holding an installed plugin and a skill exits **0** with the note (the scan boundary, decided
  below), and over a root holding only judged kinds exits 0 with no `LLM-000`; `TestZeroDial_*`, `TestFailGate_LLMGateNotEvaluable`
  and `TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind` green unchanged
- [x] The real `~/.claude`, before and after (closed port and stub): exit code unchanged, one new `LLM-000` counting 11 plugins
  and 1 directory
- [x] Docs say it: `docs/llm-judge*.md` (what the judge does not ask about; exit 4 on a `check` target), the `LLM-000` row of
  `docs/rules.md` via `make docs`, the README pair's exit-code line, the vet skill's exit-4 line, spec (§5.2 trigger table, exit
  codes, test list, the plugin note in §4); `issues/023` records the design question. No guard note in `.claude/rules/`: the kind
  table is pinned by a test that fails on drift in either direction, and the scan boundary is stated where it is enforced
  (`unaskedTarget`'s comment)
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no toolchain switch; no new dependency

## Out of scope

- **No new judge pass**: what to ask about a plugin tree or an unrecognised directory is `issues/023`. `planFor` keeps its
  switch, so `ExcerptVersion` does not move and no payload changes
- **`scan` and a root-shaped `check` never exit 4 for an unasked artifact**; they disclose it (open question 2)
- **`permission` artifacts**: no judge question by design; not counted, not disclosed by this note
- **No schema change**: `JudgeSummary`, the JSON, SARIF and HTML shapes and `aguard llm preview` output are unchanged
- **The Downloads (inbox) judge** never gates (P-026); its items get the note like any `--llm` run, nothing more
- No change to `internal/collect`, `internal/detect`, `internal/score`, `internal/report`, nor to `internal/judge/excerpt.go`,
  `decode.go` (P-037 works there)

## Must not claim

- **Exit 4 is not a finding** and says nothing about the target's risk; it says the build asked the judge about something the
  judge has no question for
- **`scan --fail-on-llm` exit 0 does not mean a plugin's skills, commands or agents were judged**: they were not, and the
  `LLM-000` note says so. Do not write that the judge "covers plugins" or "covers every artifact"
- Exit 0 on a `check` target still means only what P-026 says: every planned question was asked and answered, not that every
  byte was read

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Binary, gate and judge tests for the unasked target and the note, with their reverse rows, run red on the base | `cmd, judge: tests — a check target the judge asks nothing about passes --fail-on-llm (P-038)` |
| 2 | `judge.AsksNothingOf` and one `LLM-000` from `schedule` naming the artifacts no pass covers; the kind table pinned against `planFor` | `judge: one LLM-000 names the artifacts no judge pass covers (P-038)` |
| 3 | `failGate` exits 4 when the artifact at the checked path is of a kind the judge asks nothing about | `cmd: --fail-on-llm exits 4 when the judge asked nothing about the check target (P-038)` |
| 3b | The note and the exit-4 line say no question was asked, not that no text was sent (triage still sends finding snippets) | `judge, cmd: say no question was asked, not that no text was sent (P-038)` |
| 4 | `docs/llm-judge*.md`, the `LLM-000` rule row (`make docs`), README pair, vet skill, spec | `docs: what the judge asks nothing about, and exit 4 on a check target (P-038)` |
| 5 | `issues/023` and its index row | `issues: 023 — the judge asks nothing of plugin trees and unrecognised directories (P-038)` |
| 6 | "Done" in this file, the index | `proposals: P-038 (P-038)` |

## Open questions

1. **What counts as "the judge asked nothing about it"?**
   **Recommendation**: an artifact whose **kind** has no judge pass in `planFor` — today `plugin`, `directory` and `quarantined`.
   Triage does not count as a question: its labels are display-only and can never reach `--fail-on-llm`, and counting it would
   let one static medium turn exit 4 back into exit 0 with the same sentence unjudged (measured above). `permission` is excluded:
   an allow list is configuration whose risk permcheck decides, and the scripts it names are followed statically (pipeline.md);
   there is no question the judge is meant to ask of it, and a note that always names it teaches the reader to skip the note. An
   artifact of a judged kind with nothing to send (an empty `CLAUDE.md`) is not counted: P-026's decision 5 stands for it.
   **Decided (2026-10-10)**: as recommended.
2. **Where does it make the gate not evaluable?** The task's suggested boundary was "the target itself (for `check`), or an
   artifact whose kind has no pass and whose contents are not judged elsewhere in the same run". Measured, the second half holds
   for every installed plugin: its skills, commands and agents are judged nowhere in a scan (only its hooks and MCP servers are),
   so that rule would make `scan --fail-on-llm` exit 4 on every machine with a plugin — this one included — whatever the judge
   said, a constant that carries no information and teaches people to drop the flag.
   **Recommendation**: exit 4 only for the **target of a `check`**: the artifact whose path is the checked path (a plugin
   directory, an unrecognised directory, or a `.zip` that unpacks to one). That is the question a pull-request job asks — "is
   this thing safe to merge" — and for that thing the run has no answer. `scan` and a root-shaped `check` (an environment) keep
   their exit code and disclose the unasked artifacts in the `LLM-000` note. When `issues/023` gives plugin contents their own
   questions, the scan side shrinks to what is genuinely unjudged and can be revisited.
   **Decided (2026-10-10)**: as recommended.
3. **Which note?** **Recommendation**: reuse `LLM-000` (the judge's coverage note, low, dimension 0), one per run, counting the
   artifacts per kind and naming up to three by their redacted `kind:name` label, "and N more" after that — the shape of the
   shortened-MCP note. No new rule ID; the generated `LLM-000` description says it now also covers this.
   **Decided (2026-10-10)**: as recommended.
4. **How does the gate know?** **Recommendation**: through one exported predicate, `judge.AsksNothingOf(kind)`, that the note
   uses too, so the note and the exit code cannot disagree; no field is added to `JudgeSummary`. The predicate is a table that must
   follow `planFor`'s switch, so a test runs `planFor` over a fixture of every kind declared in `model.go` and fails on a kind the
   table and the switch disagree about — including a kind added later without a row.
   **Decided (2026-10-10)**: as recommended.
5. **Precedence and wording.** **Recommendation**: unchanged order 2 > 1 > 4 > 0; among the reasons for 4, P-026's (did not run,
   ran short) are reported first, since they are true of the whole run. The stderr line keeps P-026's form:
   `--fail-on-llm could not be evaluated (exit 4): the judge asked nothing about the target: it has no pass for a plugin (1
   artifact), so no question about its content was put to the model (the report's LLM-000 note has the detail)`.
   **Decided (2026-10-10)**: as recommended.
6. **Does this overturn P-026's decision 5** ("a summary that ran over zero planned calls is evaluable")?
   **Recommendation**: it narrows it, for the target of a `check` only. Zero calls over a target of a judged kind with nothing to
   send stays evaluable; zero calls because no pass exists for the target's kind is not an answer.
   **Decided (2026-10-10)**: as recommended.

## Done

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-038 after the merge)
Released: pending release
Evidence: TestFailOnLLM_TargetTheJudgeAsksNothingAbout, TestFailOnLLM_ScanKeepsItsCodeAndNamesWhatWasNotAsked,
  TestFailGate_TargetTheJudgeAsksNothingAbout (cmd/aguard/fail_on_llm_unasked_test.go) and TestRun_NamesTheArtifactsNoPassCovers
  (internal/judge/unasked_test.go), committed first and run on the base code (6cab205): red on exactly the not-evaluable and
  note rows — check of a plugin directory (working stub, closed port, trailing slash, .zip), a plain directory and a triage-only
  plugin exit 0 with an empty stderr and no LLM-000; the --fail-on-only and --fail-on-llm-medium plugin rows and the scan with
  an installed plugin have no LLM-000; failGate returns nil for a plugin or directory target; judge.Run returns no note — and
  green after
Evidence: the measured table, re-run with the fixed binary against a local stub and the closed port: plugin directory 0 -> 4,
  plain directory 0 -> 4, plugin as a .zip 0 -> 4, plugin with one static medium (one triage call, answered) 0 -> 4, each with
  one stderr line "--fail-on-llm could not be evaluated (exit 4): the judge asked nothing about the target: it has no pass for a
  plugin (1 artifact), ..." and one LLM-000; the same SKILL.md as a skill target 0 -> 0 (stub) and 4 -> 4 (closed port)
Evidence: the real ~/.claude, scan --inbox off --llm --fail-on-llm critical, 184 artifacts: closed port 4 -> 4 (300 of 300
  calls failed, P-026's reason), local stub 0 -> 0 (300 calls answered); both now carry one more LLM-000: "the judge asked
  nothing about 12 artifact(s) of a kind it has no pass for — plugin (11), directory (1)"
Evidence (reverse assertion): green on the base and after — a skill target answered by a working stub exits 0 with an empty
  stderr and no LLM-000; a plugin under --llm --fail-on high only exits 0 (stub and closed port); a scan of a root holding only
  judged kinds exits 0 with no LLM-000; a scan of a root holding an installed plugin exits 0; in failGate a skill target and a
  scan-shaped plugin give 0, a deterministic hit 1, --fail-on alone 0, a judge that did not run P-026's 4;
  TestFailGate_LLMGateNotEvaluable, TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind and TestZeroDial_* green unchanged
Evidence (pin): TestAsksNothingOf_FollowsPlanFor over the 15 ArtifactKind constants read from model.go; with quarantined taken
  out of noPassKinds it fails ("planFor asks nothing about a quarantined, yet noPassKinds does not list it")
Evidence (not done): git diff --stat origin/main...HEAD -- internal/collect internal/detect internal/score internal/report
  internal/model internal/judge/excerpt.go internal/judge/decode.go internal/judge/ground.go go.mod go.sum .claude CLAUDE.md
  CHANGELOG.md -> empty; internal/judge/run.go changes by the three lines that append the note in schedule (planFor untouched,
  ExcerptVersion 2); ./bin/aguard check plugin --fail-on low -> exit 0
Verify: make verify -> "verify: all gates passed"; go.mod still go 1.23.5; go version go1.23.5, no toolchain switch; no new dependency
```
