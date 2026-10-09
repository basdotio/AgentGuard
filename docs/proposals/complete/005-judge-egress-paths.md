<!-- SPDX-License-Identifier: MIT -->
# 005 — The BYO judge sends the username, absolute paths and env values without their key names to the model vendor

- **Source**: new finding (2026-10-09) — with `--llm`, the excerpts sent to the model endpoint the user configured carry
  the absolute `$HOME` path and the username (hook commands, MCP arguments, memory content, file locations in triage);
  MCP env values go out without their key names, so key-based redaction never fires. Ported from P-045 in the former
  private repository agent-guard
- **Depends on**: none
- **Branch**: `p/005-judge-egress-paths`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

When `--llm` runs against an online endpoint, the user message in the request body (`chatRequest` in
`internal/judge/openai.go`: `model`, `messages[system,user]`, `temperature`) is assembled by `userPrompt` in
`prompt.go`. Its content is each pass's `Behavior` / `Declared`, plus triage's `[RULE] file:line snippet`.
`Request.Artifact` (the `kind:name` label, assigned in `planFor` in `run.go`) **never enters the request body** — that
part is right. But three other things do go out, and `detect.Redact` handles none of them:

| What leaks | How it enters the request body | Consequence |
|---|---|---|
| **The absolute home directory path, i.e. the username** | **Content**: the hook command (`excerpt.go` `hookExcerpt`), MCP command / args / env (`mcpExcerpt` via `detect.ConfigStrings`), `CLAUDE.md` and memory body text, skill scripts and descriptions, decoded blobs. **Triage**: `run.go` `triageItems` concatenates the static findings' `File:Line Snippet` as is; for `EXFIL-005`, `Evidence.File` is the importer's **absolute path** (`collect/imports.go` `importCredentialFinding`), and findings on `~/.claude.json` become **`<username>/.claude.json`** through the fallback in `detect.relPath` | The vendor gets "this machine's username + its directory layout" on every call. `Redact` only recognises secret shapes; its high-entropy class includes `/`, so **long temp paths containing digits** occasionally have their first half wiped, but the second half left behind is exactly the username (`<REDACTED>.d/alice/…`); on a real machine `/Users/alice` is only 12 characters and is not touched at all |
| **Claude Code's project directory encoding** | Static findings on memory files have `Evidence.File` = `projects/-Users-alice-work-x/memory/MEMORY.md`, which goes out through triage | Same as above, spelled differently; `-Users-alice-work-x` is under 24 characters and has no digits, so the entropy rule does not touch it either |
| **MCP env values, without key names** | `detect.collectStrings` collects only values, so `{"DB_PASS":"hunter2"}` goes out as a lone line `hunter2` | Key-based redaction (`assignRE` in `redact.go`) **structurally cannot fire**: it needs to see the key name. And `collectStrings` walks the map in random order, so the same config produces different request-body bytes on two runs |

One more place has no limit: a skill's `Declared` (the SKILL.md description) is sent whole, and `parse.ReadMarkdown`
reads up to 1 MiB; the intent and injection passes each send it once, and `samples: 3` multiplies that by three.

Measured (this repository's `origin/main`, dec64ca; the fixture is the one from the e2e test in the first item of Done
criteria below: the home directory ends in the digit-free marker segment `alicemarker` and contains one hook, one MCP
server, an `@~/.env` import in `CLAUDE.md`, one skill and one memory file; it was run locally first):
**11 of 13 request bodies carry this username, 1 carries `hunter2`, none carries `DB_PASS=<REDACTED>`**. The full home
directory never appeared — the entropy rule wiped the first half of the temp path (which contains digits) to
`<REDACTED>`, and what was left was exactly the username half. Planning the same MCP config twice gives a different
value order the second time; a description of 3,000 `é` sends 6,000 bytes in each of the intent and injection passes.

In one sentence: **the user picked an online endpoint and agreed to "send redacted excerpts"; what actually goes out
also includes who they are, what their directories look like, and a password that was not recognised.**

## Initial direction

In `internal/judge`, **at the moment the excerpt is built**, replace every spelling of the home directory (as is, after
`EvalSymlinks`, Claude Code's project directory encoding) with `~`; for triage's `<username>/x` fallback, change only
that one structural prefix. Render an MCP server as sorted `key=value` lines, so key-based redaction can fire and the
request body bytes are stable. Cap `Declared` at 1,000 bytes. **Not in `detect.Redact`**: it is the single exit for
every static snippet; changing it would change the text/JSON/SARIF output and recompute SARIF fingerprints.

## Done criteria

Some of the tests below were added by the two review rounds after the implementation in the former repository (reviews
1–7, re-reviews 1–4); on port the whole set came along with its labels kept, so it is easy to see which hole each one
pins.

- [x] `TestE2E_JudgeBodiesCarryNoHomeOrKeylessSecret` (`cmd/aguard/e2e_test.go`, new): using the request-capture
  approach of `TestE2E_CredentialImportNeverReachesTheJudge`, the fixture's home directory is
  `TempDir()/home.d/alicemarker` (**the marker segment has no digits**, so the entropy rule cannot turn this test green
  for the wrong reason), containing: one hook (the command contains the home directory, once in its `EvalSymlinks`
  form), one MCP server in `~/.claude.json` (args contain the home directory; env is
  `{"DB_PASS":"hunter2","NODE_OPTIONS":"--require <home>/…"}`, the latter makes `EXEC-010` land on
  `alicemarker/.claude.json` and enter triage), `projects/<encoded-home>/memory/MEMORY.md` (the body contains the home
  directory, plus an `INJ-001` sentence so the encoded path goes out through triage), `CLAUDE.md` (the body contains the
  home directory + `@~/.env`, so `EXFIL-005`'s absolute path enters triage), one skill (the home directory appears in
  the description, a script and a base64 blob). Asserts:
  **no request body** contains the home directory, its `EvalSymlinks` form, its encoded form or `alicemarker`; none
  contains `hunter2`; at least one contains `DB_PASS=<REDACTED>`; none contains any artifact's `kind:name` label;
  `~/notes`, `~/logs/audit.log`, `~/.claude/CLAUDE.md`, `~/.claude.json` are all present (replaced, not deleted). Today
  (`origin/main` dec64ca, W1 test run first): red, 11 of 13 request bodies carry `alicemarker`, 1 carries `hunter2`, 0
  carry `DB_PASS=<REDACTED>`, 0 carry those four `~/` paths
- [x] `TestScanInbox_JudgeBodiesCarryNoHome` (`cmd/aguard/e2e_test.go`, new; shares the capturing `capturingJudge` with
  the previous item): `HOME` points at `TempDir()/home.d/bobmarker`, a candidate skill in `~/Downloads` has a script
  that references the home directory, `scanInbox(…, llm: true)` → no request body contains `bobmarker`. Today: red (1
  request body carries `bobmarker`, no `~/notes`)
- [x] `TestEgress_*` (`internal/judge/egress_test.go`, new, table-driven): the raw / `EvalSymlinks` / encoded forms are
  all replaced with `~`;
  **reverse**: `/Users/alicemarker2/x`, `/data/Users/alicemarker/x` are untouched (boundaries), a bare username in prose
  is untouched, the three-segment `x/alice/.claude.json` is untouched, an empty home and `/` are the identity;
  `alice/.claude.json` becomes `~/.claude.json` only in the file position; the two forms the entropy rule ate half of,
  `<REDACTED>.d/alice/…` and `/home/first.<REDACTED>`, and `/Users/alic…`, cut inside the username by the 200-byte
  snippet cap, are completed (open question 7); `/Users/…` cut before the username is untouched. Today: compile red
  (`newEgress` undefined)
- [x] `TestPlan_MCPExcerptIsKeyedAndByteStable` (`internal/judge/plan_test.go`, new): the same MCP config planned 30
  times gives identical `Behavior` bytes; contains `command=npx`, `env.DB_PASS=<REDACTED>`, not `hunter2`; **reverse**:
  `env.LOG_LEVEL=debug` is present as is (a value whose key is not a credential name is still sent). Today: red (the
  bytes already differ on the 2nd planning, `hunter2` as is, without its key name). The key-name table itself is pinned
  row by row by `TestMaskCredentialValue` (`internal/judge/excerpt_test.go`, new); its reverse rows are that the values
  of `NODE_OPTIONS`, `API_BASE` and `args` are not masked
- [x] `TestPlan_DeclaredIsCappedOnARuneBoundary` (`internal/judge/plan_test.go`, new): a long description → `Declared` ≤
  1,000 bytes, valid UTF-8, and the text of the intent pass's SKILL.md unit equals `Declared` byte for byte (grounding
  compares against the bytes that were sent). Today: red (6,000 bytes in each of the intent / injection passes). Review
  2: the W1 version used 3,000 `é`; at two bytes each and with 1,000 being even, the cut already fell on a character
  start, so deleting the rune walk-back stayed green; W9 changed it to two rows, 2,000 × U+4E2D (a 3-byte CJK character)
  and `a` + 3,000 `é` (a precondition asserts that byte 1,000 falls inside a character), plus "walks back at most one
  character"
- [x] `TestConfigLines_SameLeavesAsConfigStrings` (`internal/detect/configlines_test.go`, new): the multiset of
  `ConfigLines` values with the key prefixes stripped equals `configStrings` — the judge and the static pass read the
  same leaves. Today: compile red (`ConfigLines` undefined)
- [x] Review 1 (a relative `--root` turned the replacement off; `CLAUDE_CONFIG_DIR` and `check --llm` were not covered):
  `TestE2E_RelativeRootStillStripsTheHome`, `TestE2E_ConfigDirUnderTheHomeStripsTheUserHome`,
  `TestCheckTarget_JudgeBodiesCarryNoHome` (`cmd/aguard/e2e_test.go`); `TestRun_EmptyHomeStillStripsTheUserHome`,
  `TestRun_ScanHomeAndUserHomeAreBothStripped`, `TestRun_ScanHomeInsideTheUserHomeKeepsItsPlace`,
  `TestEgress_RelativeHomeIsResolved`, `TestEgress_LaterHomeInsideAnEarlierIsDropped` (`internal/judge/egress_test.go`)
- [x] Review 3 (replacing before redacting let 16–23-byte tokens under the home directory escape the entropy rule):
  `TestEgress_RedactsBeforeItStripsTheHome`, covering every builder that takes raw content and the file position in
  triage
- [x] Review 4 (MCP keys sorted + a head-only cut, so padding keys sorted first pushed env out every time):
  `TestPlan_MCPExcerptLeadsWithWhatTheServerRuns`, `TestRun_ShortenedMCPExcerptIsDisclosed` (with a reverse: a config
  that fits gets zero notes)
- [x] Review 5 (the project directory encoding was per byte): `TestEgress_NonASCIIHomeIsEncodedPerCharacter` (with a
  reverse: the per-byte spelling is untouched)
- [x] Review 6 (the cut repair ran on raw content): `TestEgress_ClipRepairIsForStaticSnippetsOnly` (with a reverse: the
  two static snippet paths are still repaired)
- [x] Re-review 1 (no test covered `capLine`'s rune walk-back: the padding was all ASCII, so the cut naturally fell on a
  character start): `TestPlan_MCPLineCapCutsOnARuneBoundary` (`internal/judge/plan_test.go`)
- [x] Re-review 2 (`LLM-000` had only been triggered by the "a line was cut" half; no test covered matching lead keys by
  exact equality): `TestRun_DroppedMCPLinesAreDisclosedWithoutACap`, `TestPlan_MCPLeadKeysMatchExactly`
  (`internal/judge/plan_test.go`)
- [x] Re-review 3 (the cut repair only recognised the raw spelling; the encoded spelling `projects/-Users-alic…` was
  sent as is): `TestEgress_RepairsAnEncodedHomeTheSnippetCapCut` (`internal/judge/egress_test.go`, with reverses: cut
  before the username, not cut, the tail of a longer name, raw content, and a one-segment home are all untouched)
- [x] Re-review 4 (the docs said "the given spelling" and "three spellings"; in fact the path is made absolute and
  cleaned, and a one-segment home does not get its encoded spelling replaced):
  `TestEgress_HomeIsReplacedInItsAbsoluteCleanedForm` (`internal/judge/egress_test.go`); the llm-judge and architecture
  pairs and the spec rewritten
- [x] Each item above marked "review / re-review" re-tested in this repository: those that add tests were run red by
  mutation, those that change code were run red on the pre-fix code; the numbers are recorded in "Done"
- [x] Reverse assertions, still green **with not a word of the assertions changed**:
  `TestE2E_CredentialImportNeverReachesTheJudge`, `TestRun_GroundedFindingGetsRealLineNumbers`,
  `TestRun_UngroundedFindingIsDroppedAndCounted`, `TestGround_ChecksRedactedTextNotDisk`,
  `TestPlan_MCPUsesTheSameViewTheScannerSees`, `TestPlan_EveryKindIsFencedAndRedacted`,
  `TestBehaviorExcerpt_PayloadBelowPaddingReachesTheModel`, `TestDecodedPayloads_RedactsSecret`. Of these, the 8 call
  sites that call `planFor` / `behaviorExcerpt` / `decodedPayloads` directly gain an `egress{}` argument (zero value =
  no replacement); apart from that, not a line changes
- [x] Reverse assertion: on the same fixture, `scan --json` **without `--llm`** gives byte-identical output from the
  `origin/main` binary and this branch's binary, apart from the version — the static output did not change
- [x] `make verify` green; `collect`/`detect` changed, so run a scan on a real machine and record the header

## Out of scope

- **`detect.Redact` is not changed** (`credKeys`, `assignRE` and the entropy rule all stay): it is the exit for every
  static snippet; changing it would change the text/JSON/SARIF/HTML/markdown output and recompute SARIF fingerprints.
  This proposal's replacement happens only when the judge builds an excerpt
- **`detect.relPath` is not changed, nor `EXFIL-005`'s `Evidence.File`**: the globs in the user's `.aguardignore` match
  against `e.File`
- **A bare username is not replaced**: only full home-directory path forms are, plus the one two-segment structural
  prefix `<username>/x` in triage; a username may be a common word
- No `ExcerptVersion` is added
- **The rendering of model output is not touched**: `finding()` / `barrierFinding()` in `judge.go` and `ground.go` stay
  as they are (P-006 changes them in parallel; this avoids conflicts)
- The `file` an LLM finding cites in the report does not change: `sourceUnit.file` is for the report, never leaves the
  machine, and is not changed
- No prompt changes, no change to the `judge.Client` interface, `openai.go` not touched (`NewHTTP` unchanged), no new
  dependencies, `go.mod` / `go.sum` unchanged

## Must not claim

- Do not say "request bodies no longer carry identifying information": bare usernames in prose, git author names, email
  addresses, absolute paths **outside** the home directory (`/opt`, `/srv`, temp directories), sibling directories of
  the home (`/Users/alice.bak`) are all still sent. What this proposal replaces is a few spellings of **two specific
  directories**: the OS user's home directory (`os.UserHomeDir()`, i.e. `$HOME`) and the scanned environment's home
  directory (the parent of the absolute `--root`). When `$HOME` is not the person (under `sudo`, under a service
  account), or another user's home directory appears in the content, it is still sent. (Before review 1 even this
  sentence was too strong: with a relative `--root` nothing was replaced, `check --llm` never replaced anything, and
  with `CLAUDE_CONFIG_DIR=~/.config/claude` only `~/.config` was replaced)
- Do not say redacting before replacing is free (review 3's fix): a long home directory with digits and no bytes outside
  the entropy character class in between (typically a temp directory) is eaten whole by the entropy rule, and goes out
  as `<REDACTED>.claude/…` — nothing leaks, but the judge does not see `~`, and part of the path information is lost
- Do not say the MCP excerpt always fits `env`: `command`, `args`, `env`, `url`, `headers` come first and each line is
  capped at 500 bytes, but with enough `args` elements (or top-level keys whose names start with `env.`), lead keys
  further down are still pushed out of the 6,000 bytes. When that happens the text says so and an `LLM-000` is emitted,
  but the model did not see it
- Do not say `Redact` now recognises paths or `DB_PASS`: when a rule in the static report matches the `DB_PASS=hunter2`
  line, the snippet is still plaintext, and triage sends that snippet as is. This proposal only changes the MCP excerpt
  the judge **builds itself**
- Do not say `~` is always the user's home: with `scan --root /proj/.claude`, `/proj` and the user's home directory are
  **both** replaced with `~`, and two `~` in the same request body do not point to the same place (when the scanned
  environment's home lies inside the user's home, only the outer one is replaced, and in that case `~` has only one
  meaning)
- Do not say the credential-name mask is complete: it is a key-name heuristic, used only for MCP configuration — where
  key names are structure, not prose. A password named `DB_CONN` still relies on `Redact` alone
- Do not say the project directory encoding rule is documented by Claude Code: "every **character** that is not an ASCII
  letter or digit becomes one `-`" was inferred from directory names on a real machine (before review 5 it was replaced
  per byte, and the encoded form of a non-ASCII home leaked as is); how characters outside the BMP (emoji) are encoded
  has not been observed, and they are treated as one `-`. If it changes, the encoded form leaks again
- Do not say every home-directory fragment in static snippets is completed: only three known cuts are repaired (the
  entropy rule eating the head, eating the tail, and the 200-byte cap cutting inside the username); other combinations
  of several cuts on the same home directory, or what is left after another `Redact` rule (key names, URL credentials)
  eats a path whole, are not covered. The third (the cap) **is repaired only in static snippets** (review 6): a
  `/Users/alic…` at the end of raw content was written by the author and is sent as is. The encoded spelling
  (`projects/-Users-alic…`) is repaired only for the cap (re-review 3): it lies entirely inside the entropy rule's
  character class, so the entropy rule either eats it whole or leaves it alone, and there is no half-eaten form; what
  another `Redact` rule leaves after cutting into the middle of the encoded spelling is still not covered
- Do not say all "three spellings" of each home are replaced (re-review 4): what is replaced is the spelling after
  `filepath.Abs` (a relative path resolved against the working directory, the path normalised by `Clean`); a spelling
  that only the caller's original bytes have (`/Users/./alice` in content) is still sent; for a one-segment home
  (`HOME=/root`) the encoded spelling is **deliberately** not replaced, so `-root` is still sent (open question 6),
  while the raw spelling `/root/…` is still replaced
- Do not say the judge's MCP verdicts are the same as before: the model now sees sorted lines with key names, no longer
  a list of values; the `LLM-009` judge runs committed in `baselines/results/` were measured on the old rendering

## Work items

W1–W8 are the first implementation round in the former repository, W9–W15 the fixes for reviews 1–7, W16–W20 the fixes
for re-reviews 1–4; on port, one commit per item, in the same order.

| W | In one sentence | Commit message (no sha; rebase changes it) |
|---|---|---|
| 1 | Six new test groups, run red | `judge, detect, cmd: tests — judge request bodies carry the home path, the username and a keyless env password (P-005)` |
| 2 | `judge.egress`: the home directory's three forms + two half forms → `~`, the `<username>/x` structural prefix; `Options.Home`; in every excerpt, triage and the capability summary, replace first, then `Redact` | `judge: every excerpt replaces the home directory with ~ before redaction, so no request body names the user (P-005)` |
| 3 | `scanOpts.home`: the environment scan passes `filepath.Dir(root)`, Downloads passes `os.UserHomeDir()`, into `Options.Home` via `runJudge` | `cmd: the scan and Downloads judges are told which home to strip (P-005)` |
| 4 | `detect.ConfigLines`: the same string leaves, rendered as `key=value` lines sorted by key; `configEntry`, one function that reads an entry; `configStrings`, the package-internal version (the exported name stays for one commit as a transitional wrapper, since the judge still uses it) | `detect: ConfigLines renders a config entry as sorted key=value lines from the same leaves the scanner reads (P-005)` |
| 5 | The MCP excerpt switches to `ConfigLines`; a value whose key is a credential name is not sent; the `ConfigStrings` transitional wrapper is removed | `judge: the MCP excerpt is sorted key=value lines and a value whose key names a credential is not sent (P-005)` |
| 6 | `Declared` capped at 1,000 bytes, on a rune boundary | `judge: a declared purpose is capped at 1,000 bytes on a rune boundary (P-005)` |
| 7 | spec §5.2 / §16 invariant 3, Privacy in `docs/llm-judge.md` and its zh pair, `docs/architecture.md` and its zh pair | `docs: spec, llm-judge and architecture pairs say the judge strips the home directory and sends MCP config by key (P-005)` |
| 8 | The form where the static snippet's 200-byte cap cuts inside the username is also completed (open question 7, found during implementation) | `judge: a home the static snippet cap cut inside the username is completed before triage sends it (P-005)` |
| 9 | Review 2: the rune-boundary test switches to a 3-byte character and a 2-byte character offset by one byte, so deleting the walk-back turns it red | `judge: the declared-purpose cap test cuts inside a character, so deleting the rune walk-back turns it red (P-005)` |
| 10 | Review 5: `projectDirName` encodes per character | `judge: a non-ASCII home is encoded one '-' per character, as Claude Code names the project directory (P-005)` |
| 11 | Review 1: `Run` always adds the OS user's home; homes are made absolute; a spelling of a later home that lies inside an earlier one is dropped; `scanEnv` takes the parent of the absolute root; the Downloads override is removed | `judge, cmd: the judge always strips the OS user's home as well as an absolute scan home, so an empty or relative home no longer sends paths unchanged (P-005)` |
| 12 | Review 3: `Redact` first, then replace (the same for file positions) | `judge: excerpts are redacted before the home is stripped, so a short token under the home is judged as the run the report saw (P-005)` |
| 13 | Review 6: the cut repair only for static snippets (`egress.snippet`, triage and the collusion summary) | `judge: only static snippets get the clipped-home repair, so raw text ending in a path fragment is sent as written (P-005)` |
| 14 | Review 4: the MCP excerpt puts lead keys first, caps each line at 500 bytes, fills the budget line by line, and emits `LLM-000` when cut | `judge: the MCP excerpt leads with command, args, env, url and headers, caps each line at 500 bytes and says when it was cut (P-005)` |
| 15 | Review 7: the three doc pairs llm-judge, spec and architecture rewritten to the actual guarantees | `docs: llm-judge, spec and architecture pairs say both homes are replaced, after redaction, and nothing beyond them (P-005)` |
| 16 | Re-review 1: the MCP line-cap test cuts inside a 3-byte character, so deleting the rune walk-back turns it red | `judge: the MCP line-cap test cuts inside a 3-byte character, so deleting the rune walk-back turns it red (P-005)` |
| 17 | Re-review 2: an MCP excerpt that only drops lines, with no line capped, gets its own `LLM-000` test | `judge: an MCP excerpt that drops lines without capping one is disclosed by a test of its own, so losing that half of the note turns it red (P-005)` |
| 18 | Re-review 2: lead keys matched by exact equality; top-level keys such as `command.x` rank with the rest | `judge: a top-level MCP key that only starts with command, args or url is pinned to rank with the rest, so matching lead keys by prefix turns a test red (P-005)` |
| 19 | Re-review 3: the cut repair extended to the encoded spelling, the username's start computed from the encoded offset; spec updated to match | `judge: a static snippet the cap cut inside the encoded home is completed like the raw one, so projects/-Users-alic… no longer goes out with the username's head (P-005)` |
| 20 | Re-review 4: the llm-judge and architecture pairs and the spec state "made absolute and cleaned" and "a one-segment home does not get its encoded spelling replaced"; the `newEgress` comment updated to match, plus a test pinning the cleaned form | `docs, judge: llm-judge, architecture and spec say a home is replaced made absolute and cleaned, not as given, and a one-segment home such as /root keeps its encoded form (P-005)` |
| 21 | This file, the index | `proposals: P-005 (P-005)` |

## Open questions

1. **`DB_PASS` is not in `Redact`'s credential key table** (`credKeys` only has `password|passwd|…`; the comment on the
   old test in `e2e_test.go` literally says "`pass` is not in the redaction key list"). Rendered as `key=value`,
   `DB_PASS=hunter2` still goes out as is — the rendering alone cannot save it. What to do?
   **Recommendation**: only in the judge's MCP excerpt, when the key name (its last segment) contains `pass` `pwd`
   `secret` `token` `key` `auth` `cred` `private` `cookie`, or some segment is exactly `pw`, replace the value with
   `<REDACTED>`, then pass the whole thing through `Redact` as usual. Do **not** add `pass` to `credKeys`: that table
   runs on prose and code, so `bypass=` and `compass:` would be wiped, and it changes static output (the first item of
   Out of scope). In MCP configuration key names are structure, not prose, so a wider table here has no problem of
   hitting prose; the cost of a false hit is the model seeing one value fewer. Static snippets in triage do not get this
   table (that is the static view; see the fourth item of Must not claim).
   **Decided (2026-10-08)**: as recommended.
2. **Which home does the Downloads path use?** The environment scan's convention is `filepath.Dir(root)`, but a
   Downloads candidate's root is the candidate itself, whose parent is `~/Downloads`.
   **Recommendation**: `scanInbox` uses `os.UserHomeDir()` (it already uses it to expand `~/`); the environment scan
   keeps `filepath.Dir(root)`.
   **Decided (2026-10-08)**: as recommended. (After review 1, `Run` always replaces the OS user's home, and the separate
   Downloads override was removed with W11; the conclusion stands)
3. **Before a static snippet enters triage, `Redact` has already eaten the first half of the home directory** (measured:
   `<REDACTED>.d/alicemarker/…`: a segment of the temp path containing digits is wiped by the entropy rule, the break is
   at `.`, and the username half remains), so the judge side never sees the full home directory.
   **Recommendation**: repair exactly two adjacent forms — `<REDACTED>` immediately followed by the home directory's
   suffix after some break character (a character not in the entropy character class, such as `.`) becomes `~`; the home
   directory's prefix up to some break character immediately followed by `<REDACTED>` becomes `~/<REDACTED>`. No fuzzy
   matching.
   **Decided (2026-10-08)**: as recommended.
4. **The reason `detect.ConfigStrings` is exported ("so the judge sends the same view") no longer holds; keep it or make
   it package-internal?**
   **Recommendation**: demote it to `configStrings`. The judge switches to `ConfigLines`; the two share one function
   that reads an entry, and `TestConfigLines_SameLeavesAsConfigStrings` pins that the leaves are the same.
   **Decided (2026-10-08)**: as recommended.
5. **How are arrays rendered: one line per element, `args=-y`, or joined into one line?**
   **Recommendation**: one line per element. The same granularity as the static view (one string leaf per line), and a
   model quoting one element can still be grounded.
   **Decided (2026-10-08)**: as recommended.
6. **Is the encoded form `-root` of a one-segment home (`/root`) replaced too?**
   **Recommendation**: no. `-root` looks too much like a command-line option (`tool -root-dir x`); replacing it would
   corrupt the command under review; the raw path `/root/…` is still replaced.
   **Decided (2026-10-08)**: as recommended.
7. **(Added during implementation) The static snippet's 200-byte cap also cuts the home directory in half.** Found while
   looking at the actual request bodies against the e2e fixture: a `HOOK-001` snippet ends in `-d @/…/home…` —
   `detect`'s `clip` cuts at 200 bytes and appends a `…`, regardless of where the cut lands in the content; on a long
   hook command the cut can easily land in the middle of the username (`/Users/alic…`). Open question 3 only repaired
   the entropy rule's two cuts.
   **Recommendation**: repair it just as exactly: look only at text **ending in `…`** (the cut marker only appears at
   the end of a snippet); when a prefix of the home directory (or some tail left after the entropy rule ate the head) is
   immediately followed by `…` and **already reaches into the username segment**, replace it with `~…`; a `/Users/…`
   that stops at the parent directory does not count — it points at nobody, and replacing it would be guessing. It does
   not cross Out of scope and changes no user-visible contract (report bytes unchanged); it is a third cut of the same
   thing as open question 3.
   **Decided (2026-10-08)**: as recommended.
8. **Real-machine scan (a record, not a question)**: `detect` changed (`ConfigLines`, `configEntry`, `configStrings`),
   so per the port convention `aguard scan --root ~/.claude` was run in this repository (read-only, without `--llm`),
   the `origin/main` (dec64ca) and this branch's binaries back to back; only the summary lines are recorded:

   ```
   Risk score 69/100 (Elevated)
   The score above is an average over 175 items (149 of them at 100)
   66 findings need a look (medium or above); 19 more are informational.
   66 of 85 finding(s) are medium or above
   ```

   The two binaries' terminal output is byte-identical, and `--json` is byte-identical once `scanned_at` and
   `tool_version` are removed. (`--quiet` prints nothing on this machine, so the summary lines come from a run without
   `--quiet`.)
9. **(Added during the review fixes) Which one is replaced when the two homes are nested?** Review 1 requires always
   replacing both the OS user's home and the parent of `--root`; with `CLAUDE_CONFIG_DIR=~/.config/claude` the latter is
   a subdirectory of the former, and replacing both "longest first" turns `~/.config/claude/x` into `~/claude/x` — the
   judge would read it as a different location.
   **Recommendation**: process them in order (the OS user's first); when some spelling of a later home lies inside an
   earlier one, drop that spelling, since the outer replacement already covers it; **only in this direction**: when the
   scanned environment's home **contains** the user's home (`--root /Users/.claude`), keep both, otherwise
   `/Users/alice/x` would become `~/alice/x`. `TestEgress_LaterHomeInsideAnEarlierIsDropped` pins both directions.
   **Implemented as recommended. Decided (2026-10-09, by the maintainer): accepted (the maintainer confirmed these four
   points during the review in the former repository).**
10. **(Added during the review fixes) Review 6's literal fix conflicts with review 3.** Review 6 says "`repairHalves` is
    only for static snippets; the raw-content builders do not do it"; but once review 3 changed raw content to `Redact`
    first and replace second, raw content can also show the entropy rule's eaten-head / eaten-tail forms (the e2e
    fixture's temp home `<REDACTED>.d/alicemarker`), and without the repair the username goes out — the e2e goes red,
    and rightly so. **Recommendation**: split `repairHalves` in two: the entropy rule's two cuts (which any text that
    went through `Redact` first can have) are repaired on every path; the 200-byte cap cut (which only detect's `clip`
    produces) only in `egress.snippet`, i.e. triage and the collusion summary. This is the correct form, after review 3,
    of review 6's intent (do not guess a cap on text that was never cut). **Implemented as recommended. Decided
    (2026-10-09, by the maintainer): accepted (the maintainer confirmed these four points during the review in the
    former repository).**
11. **(Added during the review fixes) Is the MCP 500-byte cap per value or per line?** Review 4 says "each value". Key
    names are just as much under the config author's control, and a 6 KB key name can fill the budget just the same.
    **Recommendation**: cap per line (the whole `key=value` line), bounding value and key together; on a rune boundary,
    with the marker ` … (N bytes omitted)`. **Implemented as recommended. Decided (2026-10-09, by the maintainer):
    accepted (the maintainer confirmed these four points during the review in the former repository).**
12. **(Added during the review fixes) How are lead keys recognised?** `ConfigLines` joins paths with `.`, so a top-level
    key `command.x` cannot be told apart from a nested key under `command`; matching by prefix, a run of `command.a…`
    top-level padding keys would rank in the command group and push env out. **Recommendation**: `command`, `args`,
    `url` are strings or string arrays, matched only when the key name is **exactly** equal; `env`, `headers` are
    objects, matched by the `env.` / `headers.` prefix — and `ConfigLines`'s sort order always puts the real `env`
    object's lines before any `env.<anything>` top-level keys. `detect.ConfigLines` is not changed (that would touch
    `detect` and require a real-machine run). **Implemented as recommended. Decided (2026-10-09, by the maintainer):
    accepted (the maintainer confirmed these four points during the review in the former repository).** From re-review 2
    on, `TestPlan_MCPLeadKeysMatchExactly` pins this: switch command, args, url to prefix matching and it goes red.
13. **(Added on port; a record, not a question) Overlap with the parallel P-001 / P-003.** P-001
    (`p/001-judge-usage-in-json`) also changes the body of `runJudge` in `cmd/aguard/main.go`, `internal/judge/run.go`
    and the llm-judge pair; this proposal adds a `home` parameter to it. P-003 (`p/003-zero-dial-test`) adds a test seam
    to `NewHTTP` in `openai.go` and calls `scanEnv` / `scanInbox` / `checkTarget` with `scanOpts{…}` named fields. This
    proposal does not touch `NewHTTP` and only adds one named field to `scanOpts`, so there is no semantic conflict with
    P-003. Whoever merges later rebases: the `runJudge` spot is merged by both sides' intent; if P-003's zero-dial
    source check requires a particular shape of the client literal in `NewHTTP`, the side that merges later adapts.
    **At merge (2026-10-09, this proposal after P-001, P-003, P-004)**: `runJudge` merged automatically with both sides
    present (P-001's usage goes into the summary, this proposal's `home` into `judge.Options`); `NewHTTP` unchanged, and
    the zero-dial source check passes as usual. P-004 left the assertion "what `check --llm` (directory and `.zip`)
    sends has the home scrubbed" to whichever merged later, i.e. this proposal: `TestCheckTarget_JudgeBodiesCarryNoHome`
    now runs once for a directory and once for a `.zip`; under mutation (after building the zip, point `HOME` at another
    directory, so the egress step no longer knows which home to scrub) both rows go red, and green again when restored.

## Done

```
Merged: PR #24 (2026-10-09; find the sha with git log --grep P-005)
Released: pending release
Evidence: TestE2E_JudgeBodiesCarryNoHomeOrKeylessSecret (cmd/aguard/e2e_test.go); W1 red on origin/main (dec64ca): 11 of 13 request bodies carry alicemarker, 1 carries hunter2, 0 carry DB_PASS=<REDACTED>, 0 carry the four ~/ paths such as ~/notes → after W3 only the two MCP checks are still red (request 4 carries hunter2, no DB_PASS=<REDACTED>) → green after W5: 0 carry the home / its EvalSymlinks form / its encoded form / alicemarker / hunter2 / any kind:name label, all four ~/ paths present
Evidence: TestScanInbox_JudgeBodiesCarryNoHome (cmd/aguard/e2e_test.go); W1 red (request 0 carries bobmarker, no ~/notes) → green after W3
Evidence: TestPlan_MCPExcerptIsKeyedAndByteStable (internal/judge/plan_test.go); W1 red (bytes already differ on the 2nd planning, values without keys, hunter2 as is) → still red after W2 → green after W5; reverse: env.LOG_LEVEL=debug present as is
Evidence: TestPlan_DeclaredIsCappedOnARuneBoundary (internal/judge/plan_test.go); W1 red (6,000 bytes in each of the intent / injection passes) → green after W6; review 2: after W9, deleting declaredPurpose's RuneStart walk-back (mutation, restored) is red in 4 places (2 rows × two passes, 1,000 bytes, invalid UTF-8) → green when restored
Evidence: TestEgress_* (internal/judge/egress_test.go), TestMaskCredentialValue (internal/judge/excerpt_test.go); W1 compile red (egress_test.go:52: undefined: newEgress) → green after W2 / W5; TestEgress_RepairsAHomeTheSnippetCapCut red on W7's egress.go / excerpt.go (/Users/alic… and <REDACTED>.d/alicem… sent as is) → green after W8
Evidence: TestConfigLines_SameLeavesAsConfigStrings (internal/detect/configlines_test.go); W1 compile red (configlines_test.go:26: undefined: ConfigLines) → green after W4
Evidence: review 5 — TestEgress_NonASCIIHomeIsEncodedPerCharacter; red in 6 places on W9's egress.go (for /Users/josémarker and /home/\u674e\u96f7marker (two 3-byte CJK characters) the per-character encodings -Users-jos-marker and -home---marker were sent as is, while the per-byte -Users-jos--marker and -home-------marker were replaced instead) → green after W10
Evidence: review 1 — TestE2E_RelativeRootStillStripsTheHome, TestE2E_ConfigDirUnderTheHomeStripsTheUserHome, TestCheckTarget_JudgeBodiesCarryNoHome red on W10's main.go / inbox.go / egress.go / run.go: request bodies carrying the home or the marker 6/6, 5/5, 2/2 → green after W11; the five in the judge package (TestRun_EmptyHomeStillStripsTheUserHome etc.) compile red under W10's newEgress signature → green after W11; mutating the within drop check to never drop (restored) turns TestEgress_LaterHomeInsideAnEarlierIsDropped, TestRun_ScanHomeInsideTheUserHomeKeepsItsPlace and TestE2E_ConfigDirUnderTheHomeStripsTheUserHome red together
Evidence: review 3 — TestEgress_RedactsBeforeItStripsTheHome; red 7/7 on W11's code (redact, declared, hook, instruction file, skill tree, mcp, triage file all send ~/Xk9mQ2vL8pR4tZ7wB3n) → green after W12
Evidence: review 6 — TestEgress_ClipRepairIsForStaticSnippetsOnly; on W12's code (with a temporary one-line probe snippet = scrub so it compiles, since deleted) red 4/4 (declared, hook, instruction file, skill tree rewrite the trailing /Users/alic… to ~…) → green after W13
Evidence: review 4 — TestPlan_MCPExcerptLeadsWithWhatTheServerRuns, TestRun_ShortenedMCPExcerptIsDisclosed; on W13's excerpt.go (a temporary probe supplied the three-return-value signature and maxConfigLineBytes, since deleted) red: command=node, args, env.NODE_OPTIONS, url, headers, zzz all missing, a 6,000-byte line not cut, no LLM-000 → green after W14
Evidence: re-review 1 — TestPlan_MCPLineCapCutsOnARuneBoundary; deleting capLine's RuneStart walk-back (mutation, restored) turns only it red: 500 bytes kept, invalid UTF-8
Evidence: re-review 2 — TestRun_DroppedMCPLinesAreDisclosedWithoutACap; removing the dropped half of the note (mutation, restored) turns only it red: no LLM-000. TestPlan_MCPLeadKeysMatchExactly; matching lead keys by a key+"." prefix, or by a bare strings.HasPrefix (two mutations, restored), turns only it red: env.NODE_OPTIONS, url, headers pushed out, command.x0=pad out of place
Evidence: re-review 3 — TestEgress_RepairsAnEncodedHomeTheSnippetCapCut; red in 5 places on W18's egress.go (the four snippets -Users-alicem…, -Users-a…, -home---a…, -home-first-l… plus the triage evidence) → green after W19
Evidence: re-review 4 — TestEgress_HomeIsReplacedInItsAbsoluteCleanedForm; changing homeSpellings to use the absolute path as given (mutation, restored) red 3/3; encoding a one-segment home too (mutation, restored) turns the /root reverse cases of TestEgress_NoHomeIsIdentity and TestEgress_RepairsAnEncodedHomeTheSnippetCapCut red together
Evidence: reverse assertions still green with not a word of the assertions changed — TestE2E_CredentialImportNeverReachesTheJudge, TestRun_GroundedFindingGetsRealLineNumbers, TestRun_UngroundedFindingIsDroppedAndCounted, TestGround_ChecksRedactedTextNotDisk, TestPlan_MCPUsesTheSameViewTheScannerSees, TestPlan_EveryKindIsFencedAndRedacted, TestBehaviorExcerpt_PayloadBelowPaddingReachesTheModel, TestDecodedPayloads_RedactsSecret; in git diff origin/main the only deleted lines of existing tests are 8 call sites (planFor ×3, behaviorExcerpt ×2, decodedPayloads ×3, each gaining an egress{} argument)
Evidence: static output unchanged — a temporary fixture (home home.d/alicemarker; hook, MCP, CLAUDE.md's @~/.env, skill, memory; hits EXEC-010 EXFIL-001 EXFIL-005 FS-001 HOOK-001 INJ-001 COV-000) without --llm: scan --json, --sarif and terminal output byte-identical between the origin/main binary and this branch's; on the real machine's ~/.claude the terminal output is identical and --json byte-identical once scanned_at / tool_version are removed (open question 8)
Evidence: Out of scope — git diff --stat origin/main -- internal/judge/judge.go internal/judge/ground.go internal/judge/prompt.go internal/judge/triage.go internal/judge/openai.go internal/detect/redact.go internal/collect internal/report go.mod go.sum is empty; the change to detect.go does not include relPath
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5, no new dependencies
```
