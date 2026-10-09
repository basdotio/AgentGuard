<!-- SPDX-License-Identifier: MIT -->
# 002 — A report does not say which version of the rules produced it, so two reports cannot tell whether the rules changed or the input did

- **Source**: new finding (2026-10-09): when two scans of the same content score differently, nothing in the report says
  whether the rule table changed; `rules_version` is the precondition for recomputing `overall` offline. Ported from
  P-043 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/002-rules-version`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

On "what produced it", a `scan --json` has a single field: `tool_version`, holding the build-time version
(`make build` injects `git describe`, such as `v0.17.0-8-g4eb7716` or `-dirty`; `go install …@latest` takes the module
version from the build info; a plain `go build` in a checkout gives `dev`). It labels **a commit**, not **a set of
rules**, and it is wrong in both directions:

| Case | Measured (this repository's origin/main dec64ca, `git rev-list` / `git describe`) | Consequence |
|---|---|---|
| The version changed, the rules did not | v0.16.0 → v0.17.0: 4 commits, **0** of them touch `internal/detect/` | When two reports differ in `tool_version` and in findings, the reader cannot rule out "the rules changed"; the only option is to dig through the changelog |
| The rules changed, the rule-table files did not | v0.17.0 → v0.18.0: of 10 commits, `a882e42` makes slash commands run the full rule set — it changes the role decision in `detect.go` / `collect.go`, and `rules.go` / `rules_data.go` are untouched; only `4eb7716` (`EXEC-012`) touches the rule table | Judging by "did the rule-table files change" misses the former |
| The rules changed, the version looks the same | `git describe`: `a882e42` (slash commands run the full rule set) and `5a60282` (reputation allowlist renewal, does not touch detection) are both `v0.17.0-1-g…`; `881c417` is `v0.17.0-7-g881c417`, and the next commit `4eb7716` adds `EXEC-012` and is `v0.17.0-8-g4eb7716` | With two versions that differ only in a suffix, the reader cannot know whether a rule lies between them |
| A build that does not go through `make` | `go build ./cmd/aguard` in a checkout of this repository → `aguard dev (commit none, built unknown)`, `tool_version: "dev"` | No information at all |

Neither `aguard version` nor the `docs/rules.md` header says which version the rule table is. As a result:

- the scorecards in `baselines/results/` need to be attributed to a rule set (`docs/corpus-benchmark.zh-CN.md` §6: "the
  benchmark header must pin the rule-set hash, otherwise the numbers cannot be attributed"; the sample scorecard header
  in the same file has long had a `rules_version: <first 12 hex digits of the sha256>` field), but today they can only be
  pinned to a commit;
- when a user brings two reports and asks "why did this finding disappear", the first step of the answer — "are the
  rules the same" — has no answer;
- to recompute a report's `overall` offline against the rules that produced it, you first need to know which rules, and
  today the report does not carry that information.

## Initial direction

Compute `RulesVersion()` in `internal/detect`: over `builtinRules()` in the engine's traversal order, take a canonical
hash of each rule's ID, dimension, severity, advisory and the `*Only` flags, and regex source (`re` / `except`), then fold
in a manually incremented `rulesEpoch` (bumped by one whenever detection logic outside the rule table changes). The regex
source fields are unexported, so it can only be computed in the `detect` package; `hack/gen-rules` cannot reach them.

It appears in three places: `ScanResult.RulesVersion` (`scan --json` / `check --json`), the end of the `aguard version`
line, and the `docs/rules.md` header (so the existing drift check also stops "changed a rule but did not run
`make docs`"). Only the single field `rules_version` is delivered; the input digest (`inputs_digest`) and the offline
verification command (`aguard verify`) are not part of this proposal.

## Design

Code read (origin/main dec64ca): `internal/detect/rules.go` (the 13 fields of `Rule`; `re` / `except` are unexported),
`rules_data.go` (`builtinRules()` is one slice literal, 46 rules, no duplicate IDs), `detect.go:321` (each line is matched
against the rules one by one in the order of `e.rules`, and each match is appended — **the order of findings on one line
is the rule order**), `detect.go:225` `byID` (takes the first match), `detect.go:695` `roleAllows`
(the three `*Only` flags and `Dimension` decide which rules run on which kind of file), `model.go:457` `ScanResult`,
`cmd/aguard/main.go:464` (the only place `ScanResult` is constructed; both `scan` and `check` go through `analyze`),
`main.go:805` (the version line), `cmd/aguard/buildinfo.go` (fills in version / commit / date from the build info when
`-ldflags` did not inject them), `cmd/aguard/version.go` (**there is already a `versionLine`**, the comparison function
for the plugin line), `hack/gen-rules/main.go` (the count block in the header), spec §5.1 / §8.

- **What is hashed**: in the order of `builtinRules()`, each rule's `ID`, `Dimension`, `Severity`, `Advisory`,
  `HookOnly`, `ConnectorOnly`, `RawOnly`, `ScriptOnly`, `re.String()`, `except.String()` (empty string when absent), plus
  the rule count and an unexported `rulesEpoch` integer. Each field is written into the sha256 with a length prefix (a
  field can contain any byte, so a separator is not reliable), and the first 12 hex digits are taken (the definition in
  the corpus-benchmark sample scorecard header).
  **`Title` / `Why` / `Ref` are not hashed**: they are explanations for people to read and do not decide whether a
  finding fires or how severe it is.
- **`rulesEpoch`**: as soon as **deterministic** detection code outside the rule table (the structural/shape/
  exfiltration-chain checks, `permcheck`, the lexical layer, the role gate, which files are read, `collect`'s credential
  import check `EXFIL-005`) changes "which deterministic findings an input produces, or a finding's ID / dimension /
  severity / advisory", it is bumped by one in the same commit. `a882e42` in the "Problem" table above is exactly this
  kind: the rule table is untouched, and the findings on slash commands changed.
  **The judge is out of scope** (decided by the maintainer): the purpose of `rules_version` is recomputing `overall`
  offline, and the judge only moves `overall_effective`.
  **Today a report identifies the judge's code only through `tool_version`**: the `judge` summary records only whether
  it ran, how much it ran and which endpoint it connected to, not the model, the prompt version, `samples` or
  `authority`; the `llm` configuration does not go into the report. Listing "the judge's rule mapping" under the epoch
  would leave every commit that changes `internal/judge` owing an epoch bump that cannot be seen from `internal/judge`.
  This discipline is written in the constant's doc comment and in spec §5.1, plus one line in
  `.claude/rules/pipeline.md` (it loads under `internal/**` and `cmd/aguard/**`, so whoever edits detection code outside
  the table sees it; written only beside the constant, someone editing `permcheck` would never open that file);
  **it is not written into `.claude/rules/detect.md`** (198/200 lines; `claude_rules_test.go` caps it at 200).
- **A new field must declare itself, and the declaration is verified**: when `Rule` gains a field later,
  `TestRulesVersion_EveryRuleFieldIsDecided` uses reflection to force it onto either the hashed list or the "just text"
  list, **and then changes each field once**: changing a hashed field must move the version, changing a text field must
  not. Otherwise a new behaviour flag (say another `DocOnly`) would quietly stay out of the version — if only the lists
  were checked, a field registered but not hashed would also be green.
- **Pure function**: `rulesVersion(rules []Rule, epoch int) string` is there for tests to mutate; the exported
  `RulesVersion()` computes it only once with `sync.OnceValue` (the gate runs `analyze` on every skill load and does not
  need to recompile 46 regexes each time).
- **Three outputs**: `ScanResult.RulesVersion` (`json:"rules_version"`, right next to `tool_version`, **always present** —
  a scan always has a rule table; old reports do not have the key);
  ` · rules=<v>` appended at the end of the `aguard version` line, with **`$2` still the version**
  (`.github/workflows/release.yml:60` uses `awk '{print $2}'`, and `baselines/adapter/aguard` takes the whole first line
  as `tool_version`, so from now on it naturally carries `rules=`); the `docs/rules.md` header.
  `InboxReport` carries no `tool_version`; it is embedded in `ScanResult`, the same field covers it, and nothing is
  added.
- **The version-line function is called `binaryVersionLine`**: this repository's `version.go` already has
  `versionLine(collect.PluginInstall, string)` (the plugin line), so the new function takes another name and the old
  function is not touched. The three variables `version` / `commit` / `date` are injected by `-ldflags` as before, and
  filled in by `init()` in `buildinfo.go` when not injected; `binaryVersionLine` only does the layout — values from both
  sources contain no spaces, so the `$2` contract is not affected.
- **With the header in rules.md, the drift check covers more**: before, changing only a regex and not the title left
  `rules.md` byte-for-byte unchanged; now the version in the header changes, and the drift step in `make verify` / CI
  requires rerunning `make docs`. This is intended, not a side effect.

## Done criteria

- [ ] `TestRulesVersion_MovesWithWhatDecidesAFinding` (`internal/detect/rules_version_test.go`, new): mutate a copy of
  `builtinRules()` one item at a time — change one rule's regex, add/remove `except`, change the severity, change the
  dimension, change the ID, flip `Advisory` and the four `*Only`, swap the order of two rules, delete one, `epoch + 1` —
  and each one moves `rulesVersion`. Today the function does not exist, so it is red at compile time
- [ ] `TestRulesVersion_IsStable` (same file, new): two calls of `RulesVersion()` are equal, the value is 12 lowercase hex
  digits, and it equals `rulesVersion(builtinRules(), rulesEpoch)`
- [ ] **Reverse assertion** `TestRulesVersion_IgnoresProse` (same file, new): replace `Title`, `Why` and `Ref` of **every**
  rule → the version does not change
- [ ] `TestRulesVersion_EveryRuleFieldIsDecided` (same file, new): every field of `Rule` is on one of the two lists
  "hashed" or "just text", **and** each field is changed once on a copy of one rule (exported fields by reflection: bool
  negated, integer +1, string / `Severity` with one byte appended; `re` / `except` cannot be set by reflection and are
  mutated explicitly) — a hashed field must move the version, a text field must not. **Mutation**: move `Title` onto the
  hashed list, or add a `DocOnly bool` to `Rule` and only register it without hashing it → red; a version that only
  checks the lists is green under both mutations
- [ ] `TestRulesEpoch_ScopeIsDeterministicOnly` (same file, new): the doc comment of `rulesEpoch` states "covers only
  deterministic detection" and "the judge is outside the rules version", and does not list `clampSeverity`, `LLM-007` or
  "the judge's rule mapping" as code the epoch covers
- [ ] `TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion` (`hack/gen-rules/main_test.go`, new): the header states
  "covers deterministic detection only", "**Outside `rules_version` entirely:** every `LLM-` ID", and "today a report
  identifies the judge's code only through `tool_version`"; "how it ran" does not appear (no field in the report says
  how the judge ran)
- [ ] `TestRulesDocHeaderSaysWhatTheHashCovers` (same file, new): the header puts every ID a report can carry into
  **exactly one class**, and states each class once — **Hashed** (N engine rules), **Covered only by the epoch**
  (structural, permission, scan notes that are not `LLM-`, `GATE-001`), **Outside `rules_version`**
  (every `LLM-` ID); the count of each class is taken from the generator's own tables, not written by hand. The test
  builds the classes from the tables, and every ID on the page must fall into exactly one class (except `GATE-000`, which
  goes into no report); "It hashes what decides a finding", "scan-note and gate entries", "notes included" and
  `GATE-000` do not appear. **Mutation**: add a `GATE-002` to `gateNotes`, or put an `LLM-` ID into `structural` → red
- [ ] `TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion` (`internal/detect/rules_version_docs_test.go`, new): the
  six passages — the `rulesEpoch` comment, the `ScanResult.RulesVersion` comment, the architecture pair, spec §5.1 and
  §8 — all say "only through `tool_version`", and none says "how it ran", its Chinese equivalent, or the Chinese for
  "Judge describes"
- [ ] `TestArchitectureEpochNotesExcludeLLM` (same file, new): the architecture pair says that what the epoch alone
  covers is "the notes that are not `LLM-` IDs", and does not say "permission and note checks" or its Chinese form
  "permission and note"
- [ ] `TestEpochTriggerIsStatedAlike` (same file, new): the architecture pair, the epoch discipline in spec §5.1 and the
  `rulesEpoch` comment all count advisory among the triggers, and name `collect`'s credential import check
  (`EXFIL-005`)
- [ ] `TestEpochRuleLoadsWhereCoveredCodeIsEdited` (`cmd/aguard/claude_rules_test.go`, new): `.claude/rules/pipeline.md`
  loads whenever any of the seven detection files outside the table
  (shape / hooks / logical / imports / permcheck / gate status / main) is edited — with no leading frontmatter it is
  always loaded (the way `frontmatterPaths` reads it), and with one, `paths:` must cover these seven — and one line names
  `detect.rulesEpoch`, `builtinRules()` and `make docs` together.
  **Mutation**: with the frontmatter in effect, narrow `paths` to `internal/detect/**` → red
- [ ] `TestE2E_ReportNamesItsRules` (`cmd/aguard/e2e_test.go`, new): the results of `scanEnv` and `checkTarget` have
  `RulesVersion == detect.RulesVersion()`, and the `--json` serialisation has a `"rules_version"` key. Today
  `ScanResult` has no such field, so it is red at compile time
- [ ] `TestBinaryVersionLine_RulesComeAfterTheExistingFields` (`cmd/aguard/main_test.go`, new): `strings.Fields()[1]` of
  `binaryVersionLine(...)` is the version, the prefix is word for word today's format, and the line ends with
  ` · rules=<v>`. **Reverse**: the `$2` contract (`release.yml:60`) does not change
- [ ] `TestRulesDocHeaderCarriesRulesVersion` (`hack/gen-rules/main_test.go`, new): the committed `docs/rules.md`
  contains the current `RulesVersion()`
- [ ] **Reverse assertion**: `TestEveryRuleIDIsDocumented` and `TestDocumentedMetadataMatchesEngine` stay green unchanged —
  not one rule in the rule set itself changed
- [ ] **Reverse assertion**: `TestHashGolden` (`internal/collect/hash_test.go`) stays green unchanged — `rules_version` is
  a different hash; none of the artifact canonical hash, the reputation allowlist or the keys of gate approvals changes
- [ ] **Reverse assertion**: `TestPluginVersionLine` and `cmd/aguard/buildinfo_test.go` stay green unchanged — neither
  the plugin line nor the build-info fallback changes
- [ ] `make verify` green; the summary line of a `scan` on a real machine is the same as before the change (this
  proposal does not change detection)

## Out of scope

- **No `inputs_digest`, no `aguard verify`**: the input digest and offline verification are two other pieces of work;
  this proposal delivers only `rules_version`
- **Nothing is added to the human-readable reports**: text / markdown / html / sarif do not change by a single byte (zero
  changes to `internal/report`). The markdown footer already has `AgentGuard <version>`; whether to add the rules version
  there is a separate question
- **The artifact canonical hash, the reputation allowlist and the gate do not change**: zero changes to
  `internal/collect`, `internal/reputation` and `internal/gate`; approval records still record only `tool_version`
- **`clean --json` does not change**: `CleanPlan` gains no field and `CleanPlanSchema` is unchanged — cleanup items are
  not products of the rules
- **baselines do not change**: zero changes to `baselines/`, and the committed `results/` do not change by a single byte.
  The adapter reads the first line of `aguard version`, so future `run.yaml` files naturally carry `rules=`
- **`plugin/` does not change**: the sentence "prints version, commit, build date, and the reputation-list entry count"
  in `references/install.md` is still true, and no plugin release is made for a suffix
- **`buildinfo.go` and the existing `versionLine` do not change**: the sources of version / commit / date stay the same,
  and the plugin line stays the same
- **No rule changes**: not one entry of `builtinRules()` changes, and `docs/rules.md` gains only a header section
- **`.claude/rules/detect.md` does not change** (198/200 lines); in `.claude/rules/` only `pipeline.md` gains one line
- No dependency added; `go.mod` / `go.sum` do not change

## Must not claim

- **Do not say "same `rules_version` ⇒ same detection logic"**. The rule-table half is mechanical; the detection code
  outside the table relies only on the manual `rulesEpoch` discipline, and a forgotten bump means two reports with "the
  same rules version" actually came from different detection code. The `docs/rules.md` header says only the two
  sentences that can be guaranteed mechanically: different ⇒ the rules changed; same ⇒ the engine rule table is the same
- **Do not say `rules_version` covers the judge, and do not say "same `rules_version` ⇒ same judge"**: none of the
  prompts, evidence grounding, consensus or severity clamping in `internal/judge` is in it, and changing them does not
  bump the epoch
- **Do not say anything else in the report identifies the judge**: `JudgeSummary` has only ran / reason / artifacts /
  calls / failed / skipped / findings / endpoint, no model, prompt version, `samples` or `authority`; the `llm`
  configuration does not go into the report at all. Today the **only** thing in a report that identifies the judge's code
  is `tool_version`, and that is all that can be said
- **Do not say "`rules_version` hashes what decides a finding", and do not say it covers every entry on
  `docs/rules.md`**: only the 46 entries of `builtinRules()` go into the hash; the 13 structural checks, the 6 permission
  checks, the 6 scan notes that are not `LLM-` IDs, and `GATE-001` rely on the epoch discipline alone; **every `LLM-` ID**
  (7 judge findings + 3 `LLM-` notes `LLM-000/002/005`) is entirely outside. A sentence "It hashes what decides a
  finding — each rule's ID, …" at the top of a page of 83 IDs reads as full coverage; with "scan notes and gate entries
  rely on the epoch alone" next to "`LLM-` entries (notes included) are outside", the `LLM-` notes fall on both sides at
  once, which reads as a contradiction
- **Do not say `GATE-000` is covered by the epoch**: it is the gate's reply to the hook, goes into no report, and the
  header makes no statement about it
- **Do not say the epoch covers the outcome of `REP-BAD` / `REP-GOOD`**: the header counts them in "13 structural" and
  "6 scan notes" respectively, and the epoch governs the **code** that produces them; whether they fire depends on the
  reputation allowlist, and the allowlist is not in `rules_version` (open question 3)
- **Do not say "same `rules_version` and same input ⇒ byte-identical report"**: the `Title`/`Why` text, `tool_version`,
  `scanned_at`, the scoring weights and the reputation allowlist are all outside it
- **Do not say "the report can now prove itself / be verified by recomputation"**: `inputs_digest` and `verify` were not
  done
- Do not say Title/Why changes "do not affect the report": they still appear in the findings in the JSON; they just do
  not go into the version

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | New tests, run red | `detect, cmd, gen-rules: tests — no report, version line or rules.md header says which rule table produced it (P-002)` |
| 2 | `detect.RulesVersion` + `rulesVersion` + `rulesEpoch` | `detect: RulesVersion hashes what decides a finding, plus an epoch for detection code outside the table (P-002)` |
| 3 | `ScanResult.RulesVersion` filled in `analyze`; `binaryVersionLine` extracted, appending `rules=` | `model, cmd: scan/check --json and aguard version name the rule table that produced them (P-002)` |
| 4 | The `gen-rules` header; `make docs` | `gen-rules: the docs/rules.md header carries the rules version, so the drift check now catches a pattern change too (P-002)` |
| 5 | spec §5.1 / §8; README and its zh pair; architecture and its zh pair | `docs: spec, README and architecture pairs say what rules_version covers and when the epoch moves (P-002)` |
| 6 | The judge moves out of the scope of `rules_version` (decided by the maintainer) — the `rulesEpoch` / `RulesVersion` comments, spec §5.1 / §8, the architecture pair, the `model.go` comment, the rules.md header; two tests | `detect, gen-rules, docs: rules_version covers deterministic detection only, so a judge change no longer obliges an epoch bump nobody would see (P-002)` |
| 7 | The rules.md header makes clear that the hash covers the N engine rules and the other entries rely on the epoch alone; the same sentence in the architecture pair is narrowed with it; one test | `gen-rules, docs: the rules.md header names the engine rules it hashes and says the rest of the page rides on the epoch alone (P-002)` |
| 8 | One line of epoch discipline added to `.claude/rules/pipeline.md`; one test | `rules, cmd: the epoch rule loads wherever deterministic detection outside the rule table is edited, not only beside the constant (P-002)` |
| 9 | `TestRulesVersion_EveryRuleFieldIsDecided` mutates field by field | `detect: a Rule field listed as hashed must move the rules version when changed, so a list entry can no longer stand in for the hash (P-002)` |
| 10 | Only `tool_version` identifies the judge's code in a report — spec §5.1 / §8, the rules.md header (through gen-rules), the architecture pair, and the `rulesEpoch` and `ScanResult.RulesVersion` comments do not promise that the `judge` summary and the `llm` configuration describe the judge; one new test, two tightened | `detect, gen-rules, docs: a report identifies the judge's code only through tool_version, and no passage promises the judge summary does more (P-002)` |
| 11 | The rules.md header states each ID class once (engine rules in the hash; structural, permission, non-`LLM-` notes and `GATE-001` on the epoch; every `LLM-` ID outside) and does not mention `GATE-000`; `make docs`; one test rewritten | `gen-rules: the rules.md header states each ID class once — engine rules hashed, the other deterministic IDs on the epoch, every LLM- ID outside — so its sentences no longer contradict (P-002)` |
| 12 | In the architecture pair, the notes covered only by the epoch are written as "the notes that are not `LLM-` IDs"; one test | `docs: the architecture pair puts only the notes that are not LLM- IDs on the epoch, so it no longer contradicts its own next sentence that puts the judge outside (P-002)` |
| 13 | The epoch trigger in the architecture pair gains advisory, and the code outside the table names `collect`'s `EXFIL-005` check; one test, which also pins spec §5.1 and the `rulesEpoch` comment | `docs: the architecture pair's epoch trigger reads like spec §5.1 — the advisory flag counts and collect's EXFIL-005 check is named — so neither change looks exempt from a bump (P-002)` |
| 14 | This file, the index | `proposals: P-002 (P-002)` |

W1–W13 were done in the former repository in the same order and went through two rounds of review; this repository
replays them one by one, resolving conflicts and differences from this repository in the corresponding commits, with the
evidence re-measured here:
`versionLine` is already taken by the plugin line → W3 uses `binaryVersionLine`, and its commit message states that
version / commit / date still come from `-ldflags` or `applyBuildInfo`; `EXEC-012` brings the engine rules to 46 → from
W4 on, `docs/rules.md` is regenerated by this repository's `make docs`; in this repository the first line of the rules
files with `paths:` is an SPDX comment → the W8 test, reading as `frontmatterPaths` does, counts "no leading frontmatter"
as always loaded (see "Done").

## Open questions

1. **Should the version be pinned to a literal, as `TestHashGolden` does?**
   **Recommendation**: no. This value **should** change with every rule change; with a literal pinned, every commit that
   changes a rule would have to update it as well — a test whose only correct response is "change the number" protects
   nothing (the same reason the head of `e2e_test.go` refuses to pin score literals). `TestHashGolden` is different:
   reputation allowlist entries and gate approvals **are stored with it as the key**, and a change of definition
   invalidates all of them; nothing is stored with `rules_version` as the key. The published value is in the
   `docs/rules.md` header, and CI's drift check is the assertion "red when the code and the published value disagree";
   `TestRulesDocHeaderCarriesRulesVersion` pins it once more in `go test`.
   **Decided (2026-10-08)**: as recommended.
2. **Hash in engine order, or sort by ID first?**
   **Recommendation**: engine order. Findings on one line are appended in rule order (`detect.go:321`) and `byID` takes
   the first match, so merely swapping two rules can change the report; sorting would hide such a change. The cost is
   that a pure reordering also moves the version — that errs towards "the rules changed", the safe side.
   **Decided (2026-10-08)**: as recommended.
3. **Do the scoring weights and the reputation allowlist go into `rules_version`?**
   **Recommendation**: no. The reasons "why two reports differ" fall into three kinds — the rules changed / the input
   changed / the score is not reproducible; scoring belongs to the third, and mixing it in would make them inseparable;
   the reputation allowlist already has its own count on the version line (`reputation entries=N`), and
   `--no-reputation` can turn it off.
   **Decided (2026-10-08)**: as recommended.
4. **Should `rulesEpoch` be enforced mechanically (for example by hashing the source of `internal/detect`)?**
   **Recommendation**: no. Any source hash would be moved by a commit that only changes comments, and a version that
   changes when a comment changes is no version at all — this proposal's own W6, W10, W12 and W13 change only comments,
   docs and tests, and `rules_version` should not move by a single character. The cost is written into the first item of
   "Must not claim".
   **Decided (2026-10-08)**: as recommended.
5. **Is the field always present, or `omitempty`?**
   **Recommendation**: always present. A scan always has a rule table, so an empty value has no legitimate meaning; a
   consumer that reads "no such key" knows the report predates this proposal.
   **Decided (2026-10-08)**: as recommended.
6. **Is the judge within the scope of `rules_version` / the epoch?** (raised during review in the former repository)
   **Recommendation**: no. The purpose of `rules_version` is recomputing `overall` offline, and the judge only moves
   `overall_effective`; with the judge's rule mapping listed under the epoch, whoever edits `internal/judge` cannot see
   that obligation.
   **Decided (2026-10-08, by the maintainer)**: as recommended; the judge moves out of scope (W6).

**Scan header on a real machine (recorded during implementation, not a question)**: `scan --root ~/.claude --quiet --json`,
a build of origin/main dec64ca vs this branch (after W13), summary lines only:

```
before: overall 69 · overall_effective 69 · artifacts 175 · scoring findings high 162 / medium 451 / low 193 · notes 10 · rules_version (absent)
after:  overall 69 · overall_effective 69 · artifacts 175 · scoring findings high 162 / medium 451 / low 193 · notes 10 · rules_version 43f245966105
JSON identical key by key except for the three keys scanned_at / tool_version / rules_version; finding counts per (rule_id × severity) identical
```

## Done

```
Merged: PR #21 (2026-10-09; find the sha with git log --grep P-002)
Released: pending release
Evidence: W1 red at compile time on this repository's origin/main dec64ca, for the reasons the criteria give: internal/detect has no RulesVersion / rulesVersion / rulesEpoch; cmd/aguard has no detect.RulesVersion, model.ScanResult has no RulesVersion field, there is no binaryVersionLine; hack/gen-rules has no detect.RulesVersion
Evidence: TestRulesVersion_MovesWithWhatDecidesAFinding (internal/detect/rules_version_test.go); green after W2: 14 mutations (regex, add except, remove except, severity, dimension, ID, Advisory, the four *Only, swapping the order of two rules, deleting one, epoch + 1) each move the version and are pairwise distinct; manual negative: temporarily removing the except field from the hash → the two "except added / removed" cases red, reverted
Evidence: TestRulesVersion_IsStable (same file); green after W2: two calls equal, 12 lowercase hex digits, equal to rulesVersion(builtinRules(), rulesEpoch); current value in this repository 43f245966105 (46 engine rules, EXEC-012 included)
Evidence: reverse assertion TestRulesVersion_IgnoresProse (same file): Title / Why / Ref of all 46 rules rewritten → version unchanged
Evidence: TestRulesVersion_EveryRuleFieldIsDecided (same file, W9): the 13 fields of Rule = 10 hashed + 3 just text; each field changed once on a copy of INJ-001, every hashed one moves the version and every text one does not. W9's red came from mutation: ① moving Title from the text list onto the hashed list — the pre-W9 test PASS, the W9 test FAIL "Rule.Title is listed as hashed, but changing it does not move the rules version"; ② adding DocOnly bool to Rule and registering it only on the hashed list — FAIL "Rule.DocOnly is listed as hashed, but changing it does not move the rules version"; both mutations reverted
Evidence: TestRulesEpoch_ScopeIsDeterministicOnly (internal/detect/rules_version_test.go) and TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion (hack/gen-rules/main_test.go), W6: on the W5 tree red 5 + 2 (missing "DETERMINISTIC detection only", missing "The LLM judge is outside the rules version", listing clampSeverity / LLM-007 / "the judge's rule mapping"; header missing "It covers deterministic detection only." and the sentence putting LLM entries outside) → green after W6
Evidence: TestRulesDocHeaderSaysWhatTheHashCovers (hack/gen-rules/main_test.go), W7: on the W6 tree red 3 (missing "It hashes the 46 engine rules' ID, dimension, severity, flags and pattern", missing "The structural, permission, scan-note and gate entries on this page are covered only by that epoch", still has "what decides a finding") → green after W7; 46 taken from len(detect.Rules())
Evidence: TestEpochRuleLoadsWhereCoveredCodeIsEdited (cmd/aguard/claude_rules_test.go), W8: the former repository's test as is, on the W7 tree, red 8 — 1 for the missing line, plus 7 "does not load … (paths: [])", and on the W8 tree those 7 are still red. They are not paths being too narrow: in this repository the first line of every rules file with paths is an SPDX comment, frontmatterPaths cannot read a leading --- block and treats it as "no frontmatter = always loaded" (TestClaudeRulesAreScopedToExistingPaths reads it the same way). The test was changed to read it the same way: no frontmatter counts as loaded, and with one it must cover the seven files; the changed test on the W7 tree is red only 1 (the missing line) → green after W8. Mutations: deleting that line from pipeline.md → red 1; removing the SPDX line so the frontmatter takes effect, paths unchanged → green; then narrowing internal/** to internal/detect/** → red 3 (imports.go, permcheck.go, gate/status.go); all reverted. pipeline.md 151 → 152 lines (limit 200)
Evidence: TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion (internal/detect/rules_version_docs_test.go) and the tightened TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion, W10: on the W9 tree red 12 + 2 (the six passages — the rulesEpoch comment, the ScanResult.RulesVersion comment, the architecture pair, spec §5.1, spec §8 — each missing "only through tool_version" or its Chinese equivalent, each still having "how it ran" / its Chinese equivalent / the Chinese for "Judge describes"; header missing "today a report identifies the judge's code only through `tool_version`" and still saying how the judge ran) → green after W10
Evidence: TestRulesDocHeaderSaysWhatTheHashCovers (W11 rewrite): on the W10 tree red 5, and at the same time TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion red 1 → green after the gen-rules header change + make docs; the class assertion proved by mutation: ① adding a GATE-002 to gateNotes → red "GATE-002 is in 0 header classes"; ② putting LLM-010 into structural → red "LLM-010 is in 2 header classes [epoch outside]" (the count sentence red as well); both reverted
Evidence: TestArchitectureEpochNotesExcludeLLM (internal/detect/rules_version_docs_test.go), W12: on the W11 tree red 4 (each side of the pair missing "the notes that are not LLM- IDs" or its Chinese equivalent, each still having "permission and note checks" or its Chinese equivalent) → green after W12
Evidence: TestEpochTriggerIsStatedAlike (same file), W13: on the W12 tree red 4, all in the architecture pair (each side missing the advisory half and the EXFIL-005 sentence) → green after W13; mutations: deleting "or advisory flag" from the rulesEpoch comment → red 1; deleting "/advisory" from the epoch discipline in spec §5.1 → red 1; both reverted
Evidence: rules_version was 43f245966105 throughout from W4 to W13 — W6–W13 change only comments, docs, the generator's header text and tests, and rulesEpoch is still 1; after every commit that changed the generator, make docs was committed, with no drift
Evidence: TestE2E_ReportNamesItsRules (cmd/aguard/e2e_test.go); W1 red at compile time → green after W3: for both entry points, scan and check, rules_version == detect.RulesVersion(), --json has the rules_version key, tool_version is still there
Evidence: TestBinaryVersionLine_RulesComeAfterTheExistingFields (cmd/aguard/main_test.go); W1 red at compile time → green after W3. On a real machine: the first line of aguard version from make build "aguard v0.18.0-15-gc2d0a4f (commit c2d0a4f, built …) · reputation entries=18 · rules=43f245966105", awk '{print $2}' = v0.18.0-15-gc2d0a4f (the release.yml:60 contract unchanged); the first line from a go build without ldflags "aguard dev (commit none, built unknown) · reputation entries=18 · rules=43f245966105" — a dev build can name its rule table too
Evidence: TestRulesDocHeaderCarriesRulesVersion (hack/gen-rules/main_test.go); after W2 red on origin/main's rules.md ("does not name rules version 43f245966105") → green after W4 make docs
Evidence: reverse assertions green unchanged — TestEveryRuleIDIsDocumented, TestDocumentedMetadataMatchesEngine (hack/gen-rules/main_test.go), TestHashGolden (internal/collect/hash_test.go), TestPluginVersionLine, TestPluginVersionLine_LegacyName, TestApplyBuildInfo (cmd/aguard)
Evidence: real-machine scan, origin/main build → this branch: overall 69 → 69, overall_effective 69 → 69, artifacts 175 → 175, scoring findings high 162 / medium 451 / low 193 identical on both sides, notes 10 → 10; JSON identical key by key except scanned_at / tool_version / rules_version; counts per (rule_id × severity) identical
Evidence: Out of scope — git diff --stat origin/main -- internal/report internal/collect internal/gate internal/reputation internal/judge internal/permcheck internal/clean baselines plugin .claude/rules go.mod go.sum internal/detect/rules_data.go internal/detect/rules.go cmd/aguard/buildinfo.go cmd/aguard/buildinfo_test.go leaves only one line in .claude/rules/pipeline.md (detect.md untouched); docs/rules.md gains only 17 header lines; cmd/aguard/version.go gains only the one function binaryVersionLine, and the existing versionLine is untouched
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); go.mod line 2 is go 1.23.5, module github.com/basdotio/AgentGuard, no new dependencies
```
