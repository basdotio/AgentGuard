<!-- SPDX-License-Identifier: MIT -->
# 018 — A token in an import line, a plugin name or an in-tree entry name reaches the report verbatim through a few notes: those evidence snippets were never redacted

- **Source**: the places that P-014 (`docs/proposals/complete/014-hook-outside-snippet-redacted.md`) named in its Out of
  scope and open question 3 and left for a follow-up: collect's notes (the `"@" + ref` of the four notes in
  `imports.go`, plugin names, `err.Error()`, entry lists, the file names in `connectors.go`/`unowned.go`/`loaded.go`),
  `detect.unreadableNote`, and `missing hook command: <cmd>` in `internal/gate/status.go`
- **Depends on**: none
- **Branch**: `p/018-collect-notes-redacted`

<!-- No "Status" line: the directory the file sits in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Invariant #3 says "`detect.Redact` is the only way a snippet is produced". A few notes splice strings copied from a
file's body or from a config value straight into the evidence snippet or `Why`, without a single byte going through
`Redact`:

- `internal/collect/imports.go`: the snippet of `EXFIL-005` and of three `COV-000` (credential path refused, outside the
  scan boundary, beyond the import depth) is `"@" + ref`, where `ref` is the verbatim text of the `@…` line in the
  instruction file
- `internal/collect/plugins.go`: the snippet of `SCOPE-001` (plugin install path escapes HOME) is
  `"install path escapes HOME: " + name`, where `name` is the key in `installed_plugins.json`
- `internal/collect/hooks.go`: the snippet of `PARSE-000` (hook entry not understood) is `"hooks." + event`, where
  `event` is the key under `hooks` in settings.json
- `unreadableNote` in `internal/detect/detect.go`: the names of unreadable entries go verbatim into `Why` and the
  snippet; its same-shaped siblings in the same file, `nonRegularNote` and `skippedDirNote`, both use `redactClip(list)`
- `DeadRegistrationNote` in `internal/gate/status.go` (`GATE-001`, which `scan` attaches on every run): the snippet is
  `"missing hook command: " + cmd`, where `cmd` is the verbatim command registered in settings.json

Repro (a binary built from this repository's `main` fd28344, the fixture under `/tmp`, `HOME` pointing at the home
inside the fixture, the token an obviously fake value of `ghp_` plus 36 characters; `CLAUDE.md` has four `@` import
lines, pointing at `~/vault/<token>/.env`, `~/.ssh/<token>/config`, `…/<token>/notes.md` outside HOME, and an import
chain whose fifth hop lands on `d/<token>/d5.md`; a skill contains a `0111` directory `<token>/`;
`installed_plugins.json` has a `<token>@market` plugin installed outside HOME; settings.json has a broken hook entry
keyed `<token>`, and a gate registration pointing at a nonexistent `…/<token>/aguard hook`; `scan --json --inbox off`):

- The token appears **11 times** in the JSON, one by one: one copy each in the snippets of `EXFIL-005` ×2, the
  credential-path `COV-000` ×2, the out-of-bounds `COV-000` and the depth `COV-000`; one each in the `Why` and the
  snippet of `unreadableNote`; one each in the snippets of `PARSE-000`, `SCOPE-001` and `GATE-001`
- `scan --md -` and `scan --verbose` also 11 times; the default terminal report 2 times
- `check <skill-dir> --md -` (the path meant for pasting into PR comments) 2 times — the two copies from
  `unreadableNote`
- In the same fixture, the token is `<REDACTED>` in the lines hit by engine rules and in the `HOOK-002` quote: the leak
  is only in these notes

Why it was never fixed: collect cannot import detect (detect depends on collect), and the implementation of `Redact`
lives in detect, so collect could not use it even if it wanted to; P-014 therefore left the collect half to this
proposal (its open question 3).

Consequence: when a user has put a token into a path (a directory name, an import line, a plugin key), the tool blanks
it out for them in the lines a rule hits, and prints it verbatim for them in these notes; the more the report is
reposted (PR comments, SARIF uploads), the less these places should be an exception.

## Initial direction

Move the implementation of `Redact` unchanged into a leaf package that both collect and detect can import, with
`detect.Redact` only delegating and its behaviour unchanged to the character (the existing redaction tests stay green
without a single change); in the five kinds of notes above, **the part copied from a file's body or a config value**
goes through it first. Measure each place: where the string is also that finding's `Evidence.File` (that is the
engine-wide question P-014 left open, not in this proposal), and where it is an identifier the application generates
itself (`Redact` would blank out every real value); leave those alone and write down why.

## Where the line falls, measured place by place

One rule in three sentences, drawn by **which field of the finding the string lands in**, not by where it comes from:

- (a) **Text that goes into `Evidence.Snippet` and was not written by the tool itself** (a piece copied from a file's
  body or a config value) always goes through `Redact` — the literal wording of invariant #3;
- (b) when the same text **also goes into `Why`**, `Why` prints the same redacted bytes — no half-redacted, half-not
  (P-014's `SUP-006` precedent);
- (c) **name lists that go only into `Why`, and paths that are that finding's `Evidence.File`**, are not in this
  proposal: that is the engine-wide question "should `Evidence.File` be redacted" left by P-014's open question 2, and
  deciding a few of those places here would grow a second set of judgements outside `Redact`.

What was measured on a real machine is "does `Redact` change today's real value at this place" (this machine's
`~/.claude` and the desktop app store, counted only, contents not printed; the synthetic values are common install
shapes):

| Place | Lands in | Measured on a real machine | Conclusion |
|---|---|---|---|
| `"@" + ref` in the four `imports.go` notes | snippet | 18 `@` references in CLAUDE.md/AGENTS.md under `~/work` on this machine, `Redact` changes 0; of 8 synthetic ones it changes 2 (long paths with digits) | change (a) |
| the plugin key in `plugins.go` `SCOPE-001` | snippet | 8 keys in `installed_plugins.json`, 0 changed | change (a) |
| `hooks.<key>` in `hooks.go` `PARSE-000` | snippet | the only such note on this machine is this one, `hooks.hooks`, unchanged; 11 standard event names, 0 changed | change (a) (found while checking for similar cases) |
| the entry list in `detect.unreadableNote` | snippet + Why | inside the skill trees on this machine (skipping the scan's excluded directories), 80 of 1948 in-tree relative paths changed (4.1%; long paths with digits like `browse/test/pair-agent-e2e.test.ts` get eaten by the entropy check into `<REDACTED>.test.ts`) | change (a)(b), same shape as `nonRegularNote`/`skippedDirNote` |
| the command in `gate/status.go` `GATE-001` | snippet | no gate registered on this machine; of 11 synthetic install paths, 4 changed (npm global, npx cache, a checkout with digits, mise's go version directory) | change (a); for the cost see open question 6 |
| `gate` `Describe` (terminal output of `aguard hook status`) | not a finding | same as above, 4/11; the `this binary:` line on the same screen prints the same path verbatim | no change, open question 5 |
| the entry list in `collect.go` `unresolvedNote` | only in Why (the snippet is a fixed string) | 46 entries in `skills/`, 0 changed, plugin keys 0/8; the desktop app store's entries are application-generated IDs (`plugin_<id>` 3/3 and account UUIDs 2/2 changed) | no change (c) |
| session file names in `connectors.go` | only in Why | all 13 `local_<uuid>.json` changed to `<REDACTED>.json` | no change (c) |
| entry names in `unowned.go` | Why + each name is an `Evidence.File` | 30 top-level names, 0 changed | no change (c) |
| `err.Error()` in `collect.go` `ioNote` | snippet, but the path in it is that finding's `Evidence.File` | at every call site the error is an `os.PathError` or `safeio`'s `"<path>: …"`, path = `Evidence.File` | no change (c): redact the snippet and File still holds the original |
| notes in `loaded.go` | — | `Why`/snippet hold only fixed strings, counts and the `TrashDir` constant; paths are only in File | nothing to change |

## Done criteria

All fixtures are built on the spot in `t.TempDir()`; the token is an obviously fake value of `ghp_` plus 36 characters
(`redact_test.go` already does the same), which `Redact`'s known-prefix table recognises.

- [x] `TestImportNotes_SecretInReferenceIsRedacted` (`internal/collect/notes_redact_test.go`, new): four import lines in
  `CLAUDE.md` (`@~/vault/<token>/.env`, `@~/.ssh/<token>/config`, `@../../outside/<token>/notes.md`, fifth hop
  `@<token>/d5.md`) go through `CollectAll`; the Title, Why and Evidence of `EXFIL-005` ×2 and of the three kinds of
  `COV-000` (credential path / out of bounds / depth) contain no token, and the snippet carries `<REDACTED>`, still
  starts with `@` and still ends with the original tail (`/.env is a credential path`,
  `/notes.md escapes the scan boundary`, `/d5.md beyond depth 4`…). Red today
- [x] `TestConfigNamesInNotesAreRedacted` (same file, new): a `<token>@market` plugin installed outside HOME →
  `SCOPE-001`; a broken entry keyed `<token>` under `hooks` in settings.json → `PARSE-000`: the snippet contains no
  token, carries `<REDACTED>`, and its fixed prefix is unchanged. Red today
- [x] `TestUnreadableNote_SecretInEntryNameIsRedacted` (`internal/detect/note_redact_test.go`, new): a `0111` directory
  in a skill named with the token, through `Engine.Run`; that `COV-000`'s Why and snippet both contain no token, both
  carry `<REDACTED>`, and the two hold the same list. Red today
- [x] `TestDeadRegistrationNote_SecretInCommandIsRedacted` (`internal/gate/status_redact_test.go`, new): for the
  registered command `/opt/<token>/aguard hook`, the `GATE-001` snippet equals
  `missing hook command: /opt/<REDACTED>/aguard hook`. Red today
- [x] `TestScan_NoteSecretsNeverReachARendering` (`cmd/aguard/note_redact_render_test.go`, new): all of the above in one
  fixture, through `scanEnv`, with `gateLivenessNote` attached the way the `scan` command does it; none of the six
  renderings — JSON (encoded with the same indentation as the CLI), terminal (plain and `--verbose`), markdown, SARIF,
  HTML — contains the token, and `EXFIL-005`, the three import `COV-000`, `SCOPE-001`, `PARSE-000`, the
  unreadable-entries `COV-000` and `GATE-001` are all still there; the markdown of the same skill through `checkTarget`
  (the path for pasting into PR comments) contains no token either. Red today
- [x] Reverse assertion: ordinary values **unchanged to the character** — `TestImportNotes_OrdinaryReferenceUnchanged`,
  `TestConfigNamesInNotes_OrdinaryUnchanged`, `TestUnreadableNote_OrdinaryNamesUnchanged`,
  `TestDeadRegistrationNote_OrdinaryCommandUnchanged` pin them with literals (`@~/.env is a credential path`,
  `@../../outside/notes.md escapes the scan boundary`, `@h5.md beyond depth 4`,
  `install path escapes HOME: figma@claude-plugins-official`, `hooks.PreToolUse`, `unreadable: lib/helper.sh, sub`,
  `… (N more)` for more than 10 names, `missing hook command: /usr/local/bin/aguard hook`, the quoted path with a space
  `"/Applications/Some Tool/aguard" hook`), and each one equals the value computed by today's concatenation; green
  before and after the fix
- [x] Reverse assertion: `aguard hook status` still prints the registered command as written —
  `TestStatusDescribe_ShowsTheRegisteredCommandVerbatim` (same gate file) uses an npx-cache-shaped path that `Redact`
  would change and asserts it appears verbatim in the `Describe` output; green before and after the fix (pins open
  question 5)
- [x] The move does not change behaviour: `internal/detect/redact_test.go`, `contenthash_test.go`,
  `snippet_redact_test.go`, `internal/judge/*_test.go`, `internal/permcheck/permcheck_test.go`,
  `cmd/aguard/redact_render_test.go` stay green without a single change (`git diff --stat origin/main` is empty for
  them); the moved patterns, the two-pass function and the entropy check are byte-for-byte equal to that section of
  `internal/detect/redact.go` on `origin/main` (`diff` is empty after removing the package name and package comment)
- [x] Fixture binaries before and after (fd28344 vs this branch, same fixture, `--json --inbox off`): token 11 → 0, the
  counts of the findings and notes above unchanged, overall unchanged
- [x] `scan --root ~/.claude` on a real machine: the JSON before and after the fix is byte-identical once `scanned_at`
  and `tool_version` are removed (the only note on this machine that falls at a changed place is `hooks.hooks`, which
  `Redact` does not change)
- [x] None of `.claude/rules/*.md` exceeds 200 lines; `make verify` green; `go version` does not switch toolchains, line
  2 of `go.mod` is still `go 1.23.5`, no dependencies added

## Out of scope

- **No change to what `Redact` matches**: the patterns, the entropy check's character classes, the thresholds and the
  order of the two passes move over byte for byte; no change to `redactClip`/`clip`; no truncation is added to any of
  the notes this proposal changes
- **No change to the places under line (c)**: the Why-only name lists of `unresolvedNote` and `connectors.go`, the entry
  names in `unowned.go`, the `err.Error()` of `ioNote`, every `Evidence.File`. The reasons are in the table above; they
  are one problem, and changing them means a separate proposal for the whole of it
- **No change to the output of `aguard hook status` (`Describe`)** (open question 5)
- **No change to the name of a followed import artifact**, `"@" + ref` (the `artifact(...)` line in `imports.go`): an
  artifact name is a different category (the gate, the reputation allowlist and the report all use it to identify
  things), and it belongs to the same engine-wide question above
- **No change to `hookOwnedNote`** (decided in P-014), nor to any rule's severity, dimension, title or fixed Why text;
  `docs/rules.md` does not change
- **The canonical hash is not touched** (`internal/collect/hash.go`); the credential pass that the content hash
  (`contenthash.go`) calls only moved, and its golden test does not change by a character
- No dependencies added, `go.mod` does not change; the new package uses only the standard library

## Must not claim

- Do not say "a token in a path no longer reaches the report": the places under line (c), artifact names (including
  followed `@` imports) and the output of `hook status` are all still verbatim
- Do not say `Redact` got stronger: what it recognises and what it cannot recognise (things like `MYSQL_PASS=…`) has not
  changed by a word; it is still best-effort
- Do not say these notes are as readable after the fix as before: ordinary names that `Redact` hits by mistake (4.1% of
  in-tree paths on a real machine, 4/11 common install paths) become `<REDACTED>` in these notes (open questions 4, 6)
- Do not say SARIF fingerprints are unchanged: for findings whose snippet `Redact` changes, `partialFingerprints`
  changes; for ordinary values it does not
- Do not say a leak was fixed on a real machine: only one note on this machine falls at a changed place, and `Redact`
  does not change it
- Do not say the judge received these: collect's notes and `GATE-001` do not go into the judge's requests; the leak
  surface is the report generated on the machine, and the copies the user reposts or uploads

## Work items

| W | In one sentence | Commit message (no sha, a rebase changes it) |
|---|---|---|
| 1 | Five new test files (four packages) plus reverse assertions, run red | `collect, detect, gate, cmd: tests — a token in an @import line, a plugin or hook key, an unreadable entry name or the gate's registered command reaches the report in clear (P-018)` |
| 2 | Move the implementation of `Redact` unchanged into the leaf package `internal/redact`; `detect.Redact`/`redactCredentials` only delegate | `redact: the redactor moves to a leaf package so collect can call the one implementation; detect delegates unchanged (P-018)` |
| 3 | collect: the part that the four import notes, `SCOPE-001` and `PARSE-000` splice into the snippet goes through `redact.Secrets` first | `collect: import, plugin-install and hook-shape notes redact the text they copy from a file or a config key (P-018)` |
| 4 | detect: the `unreadableNote` list goes through `Redact` first, and Why and the snippet use the same copy | `detect: the unreadable-entries note redacts its list the way its non-regular sibling does (P-018)` |
| 5 | gate: the `GATE-001` command goes through `detect.Redact` first; `Describe` unchanged | `gate: GATE-001 redacts the registered command it quotes; hook status still prints it as written (P-018)` |
| 6 | Docs: invariant #3 (`.claude/rules/invariants.md`, `docs/architecture*.md` in both languages), spec §16.3 and §12, the location of `redact.go` in `hash.md` | `docs: invariant #3 — one redactor in internal/redact, detect delegates, collect and the gate call it (P-018)` |
| 7 | This file, the index | `proposals: P-018 (P-018)` |

## Open questions

1. **Where does the implementation go: a leaf package, or a single pass by detect when it merges collect's notes?** (the
   two paths offered by P-014's open question 3)
   **Recommendation**: the leaf package `internal/redact`. A single pass would mean changing the path along which notes
   are produced (collect's notes converge through several paths — `main.analyze`, `inbox`, `check` — and the `EXFIL-005`
   attached to an artifact goes through `Engine.Run` as well), and it would also feed whole paragraphs of the tool's own
   Why prose to `Redact` — `looseAssignRE` recognises "a credential key name + a space + 12 characters", so English
   prose would get changed (measured: `the secret configuration is here` → `the secret <REDACTED> is here`); the leaf
   package is called only at the concatenation points, and no other bytes go through it.
   **Decided (2026-10-09)**: as recommended.
2. **What is the new package's API called, and does `detect.Redact` stay?**
   **Recommendation**: `redact.Secrets` (both passes) and `redact.Credentials` (only the credential pass, used by the
   content hash); `detect.Redact` and `detect.redactCredentials` stay as one-line delegations, so the dozen-plus call
   sites in judge, permcheck, hygiene and clean and `redact_test.go` do not change by a character. gate calls
   `detect.Redact`: gate already depends on detect transitively through report, so no new dependency direction is added;
   only collect (below detect) calls the new package directly. "There is only one implementation" is guaranteed by "the
   patterns and the two-pass function appear only in `internal/redact`".
   **Decided (2026-10-09)**: as recommended.
3. **Why are the places under line (c) not changed along with the rest?**
   **Recommendation**: do not change them; write them into Out of scope, and open a separate proposal to change them.
   They are the same problem as `Evidence.File`: the names are ones the scanner itself listed on disk, or they are that
   finding's location; redacting only Why while File is still printed is the "half and half" P-014 fixed, in reverse;
   and the names in the desktop app store and the session cache are application-generated IDs, which `Redact` blanks out
   entirely (13/13, 3/3, 2/2) — that would delete every real value for the sake of a shape the application never writes.
   **Decided (2026-10-09)**: as recommended.
4. **Should `unreadableNote` also be clipped to 200 bytes, like `nonRegularNote`?**
   **Recommendation**: no clipping, only `Redact`. It is not clipped today, adding clipping would change ordinary long
   lists (up to 10 names), and the done criteria require ordinary values unchanged to the character; with redaction and
   no clipping there is no ordering question (invariant #3 governs the order when both steps are done; likewise P-014's
   open question 4).
   **Decided (2026-10-09)**: as recommended.
5. **Should the output of `aguard hook status` be redacted too?**
   **Recommendation**: no. It is not a finding and does not go into a report; it is the operator asking, in their own
   terminal, "which command is registered"; on the `⚠ … THAT FILE DOES NOT EXIST` line the path is the answer, and
   `Redact` would change 4/11 common install paths, including the npx cache path that `ephemeralExeWarning` predicts
   will stop working; the `this binary:` line on the same screen prints the same path verbatim, so redacting only one
   line would be "half and half". The copy that gets reposted (`GATE-001` in the `scan` report) is redacted by this
   proposal.
   **Decided (2026-10-09)**: as recommended.
6. **After `GATE-001` is redacted, paths like the npx cache cannot be read in full in the report; acceptable?**
   **Recommendation**: acceptable. The report is the copy that gets reposted and uploaded; the fix that `Why` gives
   (`aguard hook install`) does not need the old path; the old path can still be looked up verbatim in
   `aguard hook status`. Telling "install path" from "secret" on `Redact`'s behalf would be a second exit (likewise
   P-014's open question 1).
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #41 (2026-10-09; find the sha with git log --grep P-018)
Released: pending release
Evidence: W1 red on this repository's main (fd28344), for the reasons in the criteria — TestImportNotes_SecretInReferenceIsRedacted: all six snippets carry the token verbatim (e.g. @~/vault/ghp_<36 chars>/.env is a credential path); TestConfigNamesInNotesAreRedacted: two (install path escapes HOME: ghp_<36 chars>@market, hooks.ghp_<36 chars>); TestUnreadableNote_SecretInEntryNameIsRedacted: the Why and snippet (unreadable: ghp_<36 chars>); TestDeadRegistrationNote_SecretInCommandIsRedacted (missing hook command: /opt/ghp_<36 chars>/aguard hook); TestScan_NoteSecretsNeverReachARendering: all seven subtests red → all green after W5
Evidence: TestScan_NoteSecretsNeverReachARendering step by step (token count, json/text/verbose/markdown/sarif/html/check --md): W1 11/2/11/11/4/3/2 → W2 (move only) unchanged → W3 (collect) 3/0/3/3/1/1/2 → W4 (unreadableNote) 1/0/1/1/0/0/0 → W5 (GATE-001) all 0; the counts of the eight kinds of findings and notes unchanged at every step
Evidence: reverse assertions TestImportNotes_OrdinaryReferenceUnchanged, TestConfigNamesInNotes_OrdinaryUnchanged, TestUnreadableNote_OrdinaryNamesUnchanged (three subtests, Why equal to the old formula character for character), TestDeadRegistrationNote_OrdinaryCommandUnchanged (four install commands), TestStatusDescribe_ShowsTheRegisteredCommandVerbatim (npx cache path, first asserting that Redact would change it) green already at W1, still green after the fix without a single change
Evidence: the move does not change behaviour — after W2, internal/detect, judge, permcheck, hygiene and clean are all green, only the one red test from W1 remains; git diff --stat origin/main -- internal/detect/redact_test.go internal/detect/contenthash_test.go internal/detect/contenthash.go internal/detect/snippet_redact_test.go internal/judge internal/permcheck internal/hygiene internal/clean cmd/aguard/redact_render_test.go is empty; diff of lines 18–213 of internal/detect/redact.go on origin/main against the corresponding section of internal/redact/redact.go: the only differences are the two function headers Redact→Secrets and redactCredentials→Credentials with their comments, and redactClip, which stays in detect; every pattern, the entropy check and the two-pass function bodies are byte-for-byte identical
Evidence: binaries before and after (fd28344 vs this branch, same fixture, HOME pointing at the fixture, scan --json --inbox off): token 11 → 0; of the 299 JSON lines only 11 differ, exactly those 11 places; per-rule counts unchanged (EXFIL-005 2, COV-000 6, SCOPE-001 1, PARSE-000 1, GATE-001 1), overall 69 → 69. The two paths outside HOME (/tmp/ag-scratch/018/…, long paths with digits) are eaten whole by the entropy check into <REDACTED><REDACTED>/notes.md and <REDACTED><REDACTED>/aguard hook — the cost accepted in open questions 4 and 6
Evidence: ~/.claude on a real machine (with Downloads): before and after the fix, overall 69 / artifacts 180 / findings 806 / notes 10; with scanned_at and tool_version removed, only 3 of 15730 JSON lines differ, all of them the path of two desktop connector artifacts and one evidence file — the "latest tool list" in the session cache moved to a different file between the two runs (the desktop app keeps writing session files during this session; connectors.go is not touched by this proposal), and with those two fields masked the output is byte-identical. Another back-to-back run of each (--inbox off): with scanned_at and tool_version removed, 15724 lines byte-identical
Evidence: Out of scope — git diff --stat origin/main -- internal/detect/hooks.go internal/report internal/collect/hash.go internal/collect/connectors.go internal/collect/unowned.go internal/collect/loaded.go internal/collect/desktop.go docs/rules.md go.mod go.sum is empty; internal/collect/collect.go has a single hunk, in the package comment (unresolvedNote and ioNote untouched); internal/gate/status.go has three hunks: the import, the DeadRegistrationNote comment and the snippet line, Describe untouched; internal/detect/detect.go has two hunks, both in unreadableNote
Evidence: .claude/rules/invariants.md 84 → 90 lines, hash.md 62 → 63 lines, detect.md 200 lines untouched; TestClaudeRulesAreScopedToExistingPaths green
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5, no new dependencies
```
