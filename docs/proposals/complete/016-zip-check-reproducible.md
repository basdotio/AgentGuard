<!-- SPDX-License-Identifier: MIT -->
# 016 — The same zip checked twice gives different SARIF: the random extraction dir name gets into the uri, the artifact and the fingerprint, and Code Scanning opens a fresh batch of alerts on every run

- **Source**: the same zip checked twice gives different SARIF / JSON / text reports: the random extraction dir name
  leaks into the artifact name, the uri and `partialFingerprints`, so GitHub Code Scanning opens a fresh batch of alerts
  on every CI run; `aguard approve x.zip` records a temp path that has already been deleted; a root-shaped zip reads the
  shared `$TMPDIR` as home. Ported from P-048 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/016-zip-check-reproducible`

<!-- No "Status" line: the directory the file sits in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`aguard check x.zip` first unpacks the archive into `os.MkdirTemp("", "aguard-inbox-")` (`internal/inbox/archive.go`
`ExtractZip`), then checks that directory as the target (`cmd/aguard/main.go` `checkTarget`). Afterwards it only sets
`out.Root` back to the zip path; every other field **still points at the random temp dir**. With a binary built on
`main` (`dec64ca`, v0.18.0), running `--sarif` twice on the same zip and `diff`ing gives:

| zip shape | What changes in the SARIF on every run | Cause |
|---|---|---|
| `SKILL.md` at the archive root (flat) | `properties.artifact` = `skill:aguard-inbox-<random>`; on macOS also `uri` = `aguard-inbox-<random>/install.sh` and `partialFingerprints["aguard/v1"]` (measured: 3 lines change) | the artifact name is the base of the extraction dir; on macOS `$TMPDIR` sits under the `/var → /private/var` symlink, `detect.relPath` resolves only the root's symlink, `Rel` fails and it falls back to "the last two segments", which carries the random dir name into the evidence path, and the fingerprint is computed from the evidence path |
| `myskill/SKILL.md` (wrapped in one directory) | `properties.artifact` = `directory:aguard-inbox-<random>` (measured: 1 line changes) | same as above, the artifact name |

Consequences:

- **GitHub Code Scanning matches alerts across runs by `partialFingerprints`.** Each time CI checks the same zip, every
  fingerprint changes → each run opens a fresh batch of alerts and marks all the old ones "fixed"; alerts that were
  reviewed and dismissed come back the next time. The comment in `sarif.go` itself says the fingerprint "deliberately
  leaves out the line number, so as not to reopen alerts that were already reviewed" — the cause of the reopening here
  is coarser than a line number.
- **Not only SARIF.** The same random name also appears in: the artifact `name`/`path` of `--json`, the "Config root" in
  `locations`, the evidence `file` of a flat archive (macOS); the terminal report's "skill aguard-inbox-<random> — curl
  piped to shell"; the evidence `file` in the JSON of `scan`'s Downloads section (it also goes through `ExtractZip`).
- **The name and path that `aguard approve x.zip` records are a temp dir that has already been deleted**: measured, the
  approvals store holds `name: aguard-inbox-1441416832`, `path: /var/folders/…/T/aguard-inbox-1441416832`, and the
  terminal hint says ``Run `aguard check "/var/folders/…/T/aguard-inbox-1441416832"` `` — doing what it says necessarily
  gives "no such file". The approval **key** (the content tree hash) is not affected; it is the same both times.
- **The other side of the same root cause: the result depends on files outside the archive.** When the archive contains
  `plugins/installed_plugins.json`, `CollectTarget` treats the extraction dir as a root and goes through `CollectAll`,
  with `home` = the parent of the extraction dir = **the shared `$TMPDIR`**. Measured: put an MCP server in
  `$TMPDIR/.claude.json`, and `check root.zip` gains an `mcp:planted` artifact and its `EXEC-001`, and `locations` also
  lists three places, `$TMPDIR/.claude.json` among them — those files are not in the archive at all. On Linux `$TMPDIR`
  is usually the machine-wide shared `/tmp`, so any user on the same machine can add findings to someone else's zip
  check.

## Initial direction

Unpack into **a subdirectory named after the archive file inside a private temp dir** (`<MkdirTemp>/x.zip/`, with the
temp root's symlinks resolved first), so the artifact name = the archive name, the evidence path = the path inside the
archive, and `home` = a private empty directory that holds only this subdirectory; after checking, `checkTarget` and the
Downloads path rewrite the `path`/`locations` that point at the temp dir to the archive path. The output for directory
targets does not change by a single byte.

## Design

Fix it in the **data**, not in the SARIF writer: text / JSON / markdown / SARIF read the same `model.ScanResult`, so
changing only `sarif.go` would leave the other three leaking. Two changes:

1. **`inbox.ExtractZip`**: run `filepath.EvalSymlinks` on `os.MkdirTemp("", "aguard-inbox-")` first, create the
   subdirectory `filepath.Base(archivePath)` inside it (0700), and unpack the entries into the subdirectory; return the
   subdirectory, and `cleanup` deletes the whole temp root. Three things improve together:
   - the artifact name = the archive's file name (`skill:x.zip`), no longer random;
   - the temp root has its symlinks resolved, so `detect.relPath`'s `Rel` holds on macOS too, and the evidence path =
     the path inside the archive (`install.sh`), the same as on Linux;
   - when a root-shaped archive goes through `CollectAll`, `home` = the private temp root, which holds only this
     subdirectory — the `.claude.json` / `.mcp.json` / desktop app directories under the shared `$TMPDIR` are no longer
     read.
2. **`cmd/aguard` gains `archiveView`** (shared by the zip branch of `checkTarget` and the Downloads `checkCandidate`,
   via `checkExtracted`): after checking, it rewrites the absolute paths that still point at the temp root — artifact
   `path` → the archive path; in `locations`, "Config root" → the archive path, and the entries derived from `home` (all
   inside the private temp root, and by construction necessarily absent) are dropped; evidence `file` in absolute-path
   form (e.g. the `COV-000` of an empty root-shaped archive) → the relative path inside the archive; the extraction dir
   quoted in snippets and `Why` (verbatim I/O error text) → the archive file name.

**The SARIF uri takes the path inside the archive, not `x.zip!/install.sh`** (open question 1): for a directory target
the uri is already relative to the target being checked; a zip is checked as "the folder it unpacks to", so the uri is
likewise relative to the archive root, and `check x.zip` and `check x/` (the same tree, a path not under a symlink) have
exactly the same uri and fingerprint, so switching between the two does not reopen alerts. If the archive name went into
the fingerprint, `skill-1.2.zip → skill-1.3.zip` would reopen every alert on every release — the same disease, just a
little slower than today. Which archive it is is stated by `properties.artifact` (`skill:x.zip`) and the run-level
`aguard/root`.

**Hashes and approvals do not change**: a zip's artifact hash is the `TreeHash` of the unpacked tree, computed over
paths relative to the archive and independent of the directory name (the same both times); the approvals store is keyed
by the hash. What changes is only the human-facing `name` / `path` in the approval record (from a deleted temp dir to
the archive name / archive path). A Downloads entry's `hash` is still over the bytes of the archive itself.

## Done criteria

- [x] `TestCheckZip_TwiceIsByteIdentical` (`cmd/aguard/archive_test.go`, new): a flat archive and an archive wrapped in
  one directory, each run through `checkTarget` twice → SARIF and text are **byte-identical**, markdown and JSON are
  identical apart from the scan time (markdown prints it to the minute); none of the four outputs contains
  `aguard-inbox-`. Red today: both archives' `properties.artifact` carries the random name (all platforms), and for the
  flat archive on macOS the uri and fingerprint change too
- [x] `TestCheckZip_SameFindingsAsItsFolder` (same as above): the flat archive's SARIF results match, one by one, those
  from checking **the same tree as a directory** (uri `install.sh`, fingerprint, level, message); only `artifact`
  differs (`skill:flat.zip` vs `skill:<dir-name>`); the artifact hash is the same — the canonical hash is not affected
- [x] `TestCheckZip_DoesNotReadTheSharedTempDir` (same as above): put an MCP server in `$TMPDIR/.claude.json` and check
  a root-shaped archive → that artifact is absent, and `locations` holds only the archive-path entry. Red today
- [x] `TestApprove_ZipRecordsTheArchive` (same as above): `approvePath(x.zip)` records `name` = `x.zip`, `path` = the
  archive path, and a `hash` equal to the one from checking the same tree directly. Red today
- [x] `TestScanInbox_ZipEvidenceIsArchiveRelative` (same as above): for a flat archive in Downloads, the evidence `file`
  of the entry's finding = `install.sh`, and the JSON contains no `aguard-inbox-`. Red today on macOS
- [x] `TestExtractZip_FolderNamedAfterTheArchive` (`internal/inbox/inbox_test.go`, new): the returned directory's base =
  the archive file name, its symlinks are resolved, it is the only thing in its parent, and the parent is not the shared
  temp dir (both sides symlink-resolved before comparing); `cleanup` deletes the parent. Red today
- [x] `TestCheckZip_AbsoluteEvidenceNamesTheArchive` (same as above): the `COV-000` evidence of an empty root-shaped
  archive = `root.zip`, not the extraction dir's absolute path
- [x] `TestCheckZip_ErrorTextNamesTheArchive` (same as above): in a root-shaped archive `installed_plugins.json` is a
  directory, or `skills` is a file → the `IO-000` snippet quotes the I/O error text, that text contains the extraction
  dir's absolute path, and the snippet goes into the fingerprint. The two SARIF outputs are byte-identical, `IO-000` is
  still there, and the snippet is written as the path inside the archive
- [x] **Reverse assertion**: the SARIF of a directory target does not change — `TestCheckDir_SARIFUnchanged` pins the
  uri `install.sh`, the `artifact` `skill:myskill` and a fingerprint literal (value taken on `main`; green already at
  W1, still green after the change); `locations` still has four entries, "User MCP config" among them, and `root` = the
  path passed in
- [x] **Reverse assertion**: what should fire still fires — the `EXEC-001` in the archive is still at `error` level and
  `overall < 70`; the existing `TestCheckTarget_ZipIsCheckedAsItsFolder`, `TestExtractZip_RefusesWhatWouldEscapeOrBloat`
  and `TestScanInbox_*` stay green without a single change; an archive named `.claude.zip` with only `install.sh` at the
  top level is still read as a directory (`TestCheckZip_DotClaudeNameIsNotARoot`: guards against a change like "name the
  subdirectory after the archive without its extension" routing it into `CollectAll`, after which nobody reads the
  top-level files)
- [x] `make verify` green; the repro steps (build `bin/aguard`, check the same zip twice with `--sarif`, then `diff`):
  flat archive 3 changed lines, wrapped archive 1 line → both 0 lines

## Out of scope

- **No change to `internal/report/sarif.go`**: the formulas for the uri, the fingerprint and the `artifact` property do
  not change by a single character; once the data they read is right, the output is right
- **The directory-target fingerprint from `detect.relPath` does not change**
- **No output for directory or single-file targets changes by a single byte** (reverse assertion); the old issue of
  `scanLocations` also listing the parent-directory entries for a single target is not touched
- **No change to hashes, approval keys or reputation allowlist matching**; no change to the zip safety caps, the refusal
  rules, the 0700/0600 permissions, or deletion right after the check
- **`internal/collect`, `internal/detect`, `internal/gate` and `internal/model` are not touched**; no SARIF `uriBaseId`
  / `originalUriBaseIds` is added

## Must not claim

- Do not say "all of `check`'s output is byte-for-byte reproducible": JSON's `scanned_at` and markdown's "scanned … UTC"
  differ every time. Say "SARIF and the terminal report are byte-identical; markdown and JSON are identical apart from
  the scan time"
- Do not say "the uri of directory targets on macOS is now stable too": `relPath` was not changed (see Out of scope)
- Do not say a zip's alerts "can link to the source file on GitHub": the files in the archive are not in the repository;
  the uri is the path inside the archive
- Describe the `$TMPDIR` issue as it is: a planted file **can only add findings, not hide them** (one more artifact; the
  score can only go down, never up), and the worst case is a false block; do not present it as "can bypass the gate"

## Work items

| W | In one sentence | Commit message (no sha, a rebase changes it) |
|---|---|---|
| 1 | Seven tests + reverse assertions, run red | `cmd, inbox: tests — the same zip checked twice gives different reports, and a root-shaped zip reads the shared temp dir (P-016)` |
| 2 | `ExtractZip` unpacks into a subdirectory named after the archive inside a private, symlink-resolved temp root | `inbox: a zip unpacks into a folder named after it inside a private temp dir, so its name and paths stop being random (P-016)` |
| 3 | `archiveView`: `check` and Downloads rewrite the remaining paths that point at the temp root to the archive; tighten the temp-root location check, add the absolute-evidence test | `cmd: a checked zip is reported as the archive — path, locations and evidence no longer name the extraction dir (P-016)` |
| 4 | spec §4.1 item 3, the §8 `ArtifactReport.Path` comment; the Downloads entry in `.claude/rules/pipeline.md` | `docs: spec says a zip is reported by its own name and its member paths (P-016)` |
| 5 | Rename the variable `real` in `ExtractZip` to `resolved` (it shadowed the builtin function `real`) | `inbox: the resolved temp root no longer shadows the builtin real (P-016)` |
| 6 | Tests: the extraction dir in verbatim I/O error text; drop the scan time before comparing; resolve both sides of the temp-root check; run red | `cmd, inbox: tests — an I/O error quoted from inside a zip still names the extraction dir; compare without the scan time, resolve both sides of the temp-dir check (P-016)` |
| 7 | `archiveEvidence` replaces the extraction dir in snippets and `Why` with the archive file name | `cmd: an I/O error quoted from inside a zip names the archive, so that note's fingerprint stops changing (P-016)` |
| 8 | Spec wording: only SARIF and the terminal report are byte-identical; quoted I/O errors name the archive | `docs: spec — markdown carries the scan time, so only SARIF and the terminal report are byte-identical; quoted I/O errors name the archive (P-016)` |
| 9 | This file, the index | `proposals: P-016 (P-016)` |

## Open questions

The six questions below were settled as recommended by the AI in the former repository and are unchanged on port; any of
them can be overturned before merge.

1. **Should the SARIF uri be the path inside the archive (`install.sh`) or `x.zip!/install.sh`?**
   **Recommendation: the path inside the archive.** The same convention as directory targets (relative to the thing
   being checked); `check x.zip` and `check x/` have the same fingerprint; a new archive version does not reopen alerts;
   SARIF has no `!/` syntax, so GitHub would treat it as a literal path. Cost: checking two different archives in the
   same Code Scanning category, hitting the same line with the same rule, collides on the fingerprint — the same as
   checking two directory targets today; separate them with a SARIF category.
   **Decided (2026-10-08)**: as recommended.
2. **Should the artifact name be the archive file name (`x.zip`) or have the extension stripped (`x`)?**
   **Recommendation: `x.zip`.** The name is the file the person passed in; also, `collect.looksLikeRoot` recognises the
   directory name `.claude`, so with the extension stripped `.claude.zip` would be routed into `CollectAll` and nobody
   would read the top-level `install.sh` — a bypass by renaming a file. A name that keeps its extension never equals
   `.claude` (`IsZip` requires a `.zip` ending).
   **Decided (2026-10-08)**: as recommended.
3. **For artifacts in a root-shaped archive, should `path` be the archive path or a composed path like
   `x.zip/skills/foo`?**
   **Recommendation: the archive path.** `path` answers "where on disk can I find it", and for something inside an
   archive the answer is the archive; a composed path looks like a real path, and following the gate's hint
   ``aguard check "<path>"`` would fail. The location inside the archive is in `name` and in the evidence.
   **Decided (2026-10-08)**: as recommended.
4. **What happens to the entries in `locations` derived from `home` (User MCP config, Desktop store, etc.)?**
   **Recommendation: drop them.** They now land inside the private temp root, empty by construction and deleted right
   after the check; rewriting them to the archive path would be a lie, and keeping them is noise plus a random path.
   "Config root" is kept, rewritten to the archive path.
   **Decided (2026-10-08)**: as recommended.
5. **Fix `detect.relPath` while at it (resolve symlinks in the file path too)?**
   **Recommendation: no.** That would change the uri and fingerprint of directory targets under a macOS symlink prefix,
   and this proposal explicitly requires directory targets to stay unchanged; touching `detect` would also require a
   scan on a real machine.
   **Decided (2026-10-08)**: as recommended.
6. **A long archive name + a deep path hits the path limit; handle it?** With the extra directory level named after the
   archive, a very long archive name plus a very deep entry path hits macOS's 1024-byte path limit, `check` exits 2, and
   that Downloads entry becomes "archive could not be opened".
   **Recommendation: do not handle it; record it as a known limitation.** It fails closed (an error, not a pass); an
   attacker could already make extraction fail with deep-path entries alone, and the only new case is "a benign archive
   with both a long name and a deep path"; truncating the directory name for it would make the artifact name and the
   archive name disagree, and would need yet another rewrite of the name. Deal with it if someone actually reports it;
   the approach then would be "use a short name when the directory name exceeds N bytes, and have `archiveView` change
   the artifact name back to the archive name".
   **Decided (2026-10-08)**: as recommended.

**Collected during implementation and review (collected in the former repository on 2026-10-08, re-measured in this
repository)**

- **W1's temp-root location check originally compared against the unresolved `TMPDIR` prefix**: after W2 the extraction
  path is symlink-resolved (`/private/var/…` on macOS), so that W1 assertion could no longer catch a location still
  inside the temp root. Tightened in the W3 commit (compare both spellings, and assert that only the archive-path entry
  remains), which also adds `TestCheckZip_AbsoluteEvidenceNamesTheArchive` (the `COV-000` evidence of an empty
  root-shaped archive used to be the extraction dir's absolute path). The mutation check in this repository: replace
  `archiveView` with a pass-through that only changes `Root`, and the location check reports four entries ("Config root"
  "User MCP config" "Claude Desktop store" "Desktop session cache"), and the three tests for `COV-000`, approval and the
  two-run comparison also go red; all green after restoring.
- **On Linux the uri and fingerprint were already stable before the change** (`/tmp` is not a symlink); only the
  `artifact` property and the JSON name / path changed from run to run; the new tests still go red on Linux through
  those, and CI runs on Linux.
- **W5**: the variable `real` in `ExtractZip` shadowed the builtin function `real`; lint does not enable that check;
  renamed to `resolved`.
- **(review) The extraction dir also leaked through snippets**: `collect.ioNote` uses `err.Error()` verbatim as the
  snippet, the I/O error text carries an absolute path, and the snippet goes into the fingerprint. So when `skills` is a
  file or `installed_plugins.json` is a directory in a root-shaped archive, the same archive still got different
  fingerprints on two runs. W6 first writes `TestCheckZip_ErrorTextNamesTheArchive` and runs it red; W7 has
  `archiveEvidence` replace the extraction dir in the snippet and `Why` with the archive file name
  (`fdopendir skillsfile.zip/skills: not a directory`); it replaces only the prefix the tool composed itself, brings in
  nothing from the archive's content, and redaction has already run.
- **(review) markdown prints "scanned … UTC" to the minute**, and `renderAll` originally zeroed the time only before
  JSON, so a run crossing a minute boundary would go falsely red. W6 fixes that too, and it shows the first done
  criterion was originally wrong — markdown is not byte-identical; the criterion, "Must not claim" and the spec wording
  (W8) are all changed to "SARIF and the terminal report are byte-identical".
- **(review) The "the parent is not the shared temp dir" assertion in `TestExtractZip_FolderNamedAfterTheArchive` could
  originally never fire on macOS** (one side was symlink-resolved, the other was not). W6 resolves both sides.
- **(review) A long archive name + a deep path hits the path limit**: see open question 6; measured in this repository
  with a 200-byte archive name + an 816-byte entry path, the `main` binary exits 0 and this branch exits 2
  (`mkdir …/aguard-inbox-<random>/<archive-name>/…: file name too long`). Recorded as a known limitation per open
  question 6, not fixed.

## Done

All the red evidence was measured in this repository: the W1 tests were applied on top of `main` at `dec64ca` (v0.18.0);
the W6 tests on top of W5 (W1–W5 applied). The 8 commits from the former repository (module path changed to AgentGuard,
P number changed to P-016) were applied in order with `git am -3`, **with no conflicts and no manual edits**; the only
change on port was "Taken on origin/dev" → "Taken on main" in a W1 test comment. Since the export, this repository has
not touched `internal/inbox`, `cmd/aguard/inbox.go` or the zip path in `cmd/aguard/main.go`, so no part of this had
already been fixed.

```
Merged: PR #31 (2026-10-09; find the sha with git log --grep P-016)
Released: pending release
Evidence: W1 on top of main (dec64ca): 6 red, 5 reverse assertions green: TestCheckZip_TwiceIsByteIdentical (flat: uri aguard-inbox-<random>/install.sh, artifact skill:aguard-inbox-<random>, all four outputs contain aguard-inbox; nested: artifact directory:aguard-inbox-<random>), TestCheckZip_SameFindingsAsItsFolder (zip uri aguard-inbox-<random>/install.sh vs directory install.sh), TestCheckZip_DoesNotReadTheSharedTempDir (picks up mcp:planted from $TMPDIR/.claude.json, four locations point at the temp dir), TestApprove_ZipRecordsTheArchive (name aguard-inbox-<random>, path a deleted temp dir), TestScanInbox_ZipEvidenceIsArchiveRelative (evidence aguard-inbox-<random>/install.sh), TestExtractZip_FolderNamedAfterTheArchive (dir name random, symlinks unresolved, directly in the shared temp dir)
Evidence: after W2 the SARIF is already the same on both runs, and TestCheckZip_SameFindingsAsItsFolder / DoesNotReadTheSharedTempDir / ScanInbox_ZipEvidenceIsArchiveRelative / ExtractZip_FolderNamedAfterTheArchive turn green; TestCheckZip_TwiceIsByteIdentical is still red on text / markdown / JSON, TestApprove_ZipRecordsTheArchive still red because path is <temp-root>/flat.zip; all green after W3
Evidence: TestCheckZip_SameFindingsAsItsFolder (cmd/aguard/archive_test.go); the zip and the same tree as a directory: uri install.sh, fingerprint 77848c2e390ecd00, level and message identical result by result, only artifact differs (skill:flat.zip vs skill:myskill); artifact hash identical
Evidence: TestCheckZip_DoesNotReadTheSharedTempDir; only the archive-path location remains. Mutation (archiveView replaced by a pass-through that only changes Root) reports four locations, and TestCheckZip_AbsoluteEvidenceNamesTheArchive, TestApprove_ZipRecordsTheArchive and TestCheckZip_TwiceIsByteIdentical go red at the same time; restored
Evidence: TestCheckZip_ErrorTextNamesTheArchive; W6 on top of W5 red (two cases, manifest_is_a_directory and skills_is_a_file: the IO-000 aguard/v1 fingerprint differs between the two runs, text / markdown / JSON contain aguard-inbox) → green after W7, IO-000 still present
Evidence: TestExtractZip_FolderNamedAfterTheArchive (internal/inbox/inbox_test.go); the W6 version of the test run against main's ExtractZip fires all three assertions (random name, symlinks unresolved, "sits directly in the shared temp dir")
Evidence: reverse assertion TestCheckDir_SARIFUnchanged — fingerprint literal 77848c2e390ecd00 green on main, still green after the change; TestCheckZip_DotClaudeNameIsNotARoot, TestCheckTarget_ZipIsCheckedAsItsFolder, TestExtractZip_RefusesWhatWouldEscapeOrBloat, TestScanInbox_ChecksDownloadsWithoutTouchingTheScore unchanged and green before and after
Evidence: repro (one binary each from main and this branch with the same -ldflags; check the same zip twice with --sarif, then diff): flat archive 3 changed lines → 0, wrapped archive 1 line → 0; ten archive shapes (flat, wrapped, empty root-shaped, root-shaped with a skill, plugin, a single CLAUDE.md, .claude.zip, with refused entries, installed_plugins.json is a directory, skills is a file) × four outputs (--json, --sarif, --md -, --verbose): aguard-inbox occurrences 136 → 0, lines changed between the two SARIF runs 22 → 0, exit codes identical case by case
Evidence: $TMPDIR pointed at a directory holding a .claude.json (with one MCP server), check root.zip: main picks up mcp:planted + EXEC-001 and lists four locations → this branch 0 artifacts, the only location is the archive path; approve flat.zip: main records name aguard-inbox-<random>, path /var/folders/…/aguard-inbox-<random> → this branch flat.zip / the archive path; hash key 43db58ad… the same before and after
Evidence: reverse assertion, directory and single-file targets byte-for-byte unchanged — 5 targets (flat directory, skill directory, wrapped directory, a single install.sh, root-shaped directory) × 4 outputs (text including the exit code, --json without scanned_at, --sarif, --md - without the scanned line), main vs this branch: 20 cases, 0 differences
Evidence: on a real machine, scan --root ~/.claude --json: 69/100, 180 artifacts, main and this branch identical apart from scanned_at / tool_version (this machine has no candidates in ~/Downloads, so the evidence for the Downloads path comes only from the tests and the shape sweep); internal/collect and internal/detect are untouched, this check is only for the Downloads path going through ExtractZip
Evidence: Out of scope — git diff --stat origin/main -- internal/report internal/collect internal/detect internal/gate internal/model .claude/rules/invariants.md go.mod go.sum is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); go.mod line 2 go 1.23.5, no new dependencies; coverage cmd/aguard 51.4%, internal/inbox 74.0%
```
