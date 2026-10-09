<!-- SPDX-License-Identifier: MIT -->
# 028 — The restore preview of `clean --undo` can print the head of a secret that crosses its 100-byte column limit: it cuts first and redacts second

- **Source**: new finding, made while P-018 was implemented (P-018 moved `Redact` into `internal/redact` and listed
  `internal/clean` among the callers that stay unchanged; reading those callers showed the order is inverted there)
- **Depends on**: none
- **Branch**: `p/028-clean-preview-redact-first`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Before `clean --undo` (and its `--dry-run`) puts a quarantined skill back, `internal/clean/preview.go` prints what is in
the box: the first three non-empty lines of a file, the first five entry names of a directory, a symlink's target, and
the titles of the worst static findings. Every one of those four lines is written as `detect.Redact(clip(x))`: the text
is first cut to 100 bytes plus `…`, and only the cut piece is redacted.

Invariant #3 says the opposite order, everywhere: "redact first, then truncate" — `detect.redactClip` exists precisely so
that order cannot be got wrong at a call site, and its comment describes this exact failure. A secret that starts before
column 100 and ends after it is cut into a head that no longer matches its pattern (a GitHub token needs 20 characters
after `ghp_`, a Google key 30 after `AIza`) and is shorter than the entropy pass's 24-character floor, so the head of the
key — the useful half — reaches the terminal, and from there a CI log or a pasted transcript.

Who is affected: an operator restoring their own quarantined skill whose first lines (a long `curl` with a key in the
query string, a config line) or whose file names carry a credential past column 100. The preview's own comment says
content is redacted because a preview that leaked a credential "would be a self-inflicted version of the leak the
scanner reports"; for a secret that crosses the column limit, that is what it does today.

## Initial direction

Redact before cutting, through one local helper of the same shape as `detect.redactClip` (`clip(detect.Redact(s))`),
used by all four lines; the 100-byte width and the `…` mark stay as they are. Measure first which of the four lines can
actually carry text that was not written by the tool, and look for the same inversion elsewhere in the repository.

## What was measured

On `origin/main` (155865b), each preview line driven directly with `preview(&buf, path)` and once through
`Undo(…, "last", dryRun=true)`; the values are obviously fake (`ghp_` plus 36 letters and digits, the one the detect tests
use; `AIza` plus 35 characters), each placed so that the cut at byte 100 leaves its first 10 or 14 bytes:

| Preview line | Text it quotes | Today, secret crossing byte 100 |
|---|---|---|
| file line (`previewFile`) | the first three non-empty lines of the quarantined file | `… ghp_abcdefghij…` and `…&key=AIzaFAKE0F…` printed |
| directory entry (`previewDir`) | the first five relative entry names | `ddd…/ghp_abcdefghij…` printed, also through `clean --undo last --dry-run` |
| symlink target (`preview`) | `os.Readlink` of the quarantined path | `/opt/vault/xxx…/ghp_abcdefghij…` printed |
| worst findings (`previewRisk`) | `RuleID + " " + Title` of high/critical findings | cannot leak: every title is written in this repository; the longest of the 46 rules is 83 bytes with its ID and none is changed by `Redact`; the 16 literal `Title:` strings in detect are at most 82 bytes |

In each leaking case the head on its own is not redacted (too short for its prefix pattern, under the entropy pass's
24-character floor); the whole value is. A secret that ends inside the first 100 bytes is fully `<REDACTED>` today.

Looked for the same inversion elsewhere (`grep` of every non-test `Redact(`, `redact.Secrets(` and `redactClip(` call
site): the four lines above are the only places that redact a piece already cut. Nearby shapes that are not this one:
`judge`'s `capBytes(detect.Redact(…))` and `boundedRedact` redact first; `egress.snippet` re-redacts a detect snippet that
was redacted whole before detect clipped it (a defensive second pass, not the protection); `hookOwnedNote` in
`internal/detect/hooks.go` leaves its resolved path unredacted by a decision recorded in P-014, pinned by
`TestHookNotes_DoNotRedactTheScannerOwnPaths`.

## Done criteria

- [x] `TestPreview_SecretAcrossTheColumnLimitIsRedacted` (`internal/clean/preview_redact_test.go`, new): four subtests —
  a file line with a GitHub-shaped token, a file line with a Google-shaped key in a URL query, a directory entry, a
  symlink target — each with the secret straddling byte 100; the output contains no 8-byte head of the secret and does
  contain `<REDACTED>`. Each subtest first checks the fixture is the straddling case (`Redact` recognises the whole value
  and not the head a cut leaves). Red on `origin/main`, green after the fix
- [x] `TestUndoDryRun_PreviewDoesNotPrintASecretHead` (same file, new): the same property through `Undo(…, "last", true)`
  on a quarantined skill whose entry name carries the token past the limit. Red on `origin/main`, green after
- [x] Reverse assertion: `TestPreview_OrdinaryTextIsUnchanged` (same file) — ordinary lines, entry names and a symlink
  target longer than 100 bytes render byte for byte as `line[:100] + "…"`, short lines unchanged, and the worst-finding
  line still reads `⚠   EXEC-001 <title>` for a `curl … | bash` payload. Green before and after, unchanged
- [x] Reverse assertion: `TestPreview_ShortSecretIsStillRedacted` (same file) — a token that ends inside the width is
  fully `<REDACTED>` on the file line, the directory entry and the symlink target. Green before and after, unchanged
- [x] `TestPreview_WorstFindingLineQuotesOnlyToolText` (same file): every rule's `ID + " " + Title` fits in the width and
  `Redact` leaves it unchanged — the reason that line moves to the same helper for consistency, not because it leaked.
  Green before and after
- [x] Existing `internal/clean` tests (among them `TestUndo_CoordinatedRewriteSucceedsButIsAnnounced`) green without a
  change; `make verify` green; `go version` does not switch toolchains, `go.mod` line 2 still `go 1.23.5`, no dependency

## Out of scope

- **No change to the width or the cut**: `previewWidth` (100), `previewLines` (3), `previewEntries` (5) and the `…` mark
  stay; `clip` keeps cutting at a byte offset (it can split a multi-byte character, exactly as `detect.clip` does — a
  separate, cosmetic question)
- **No change to `Redact`**: nothing in `internal/redact` or `internal/detect` changes; no pattern, threshold or pass order
- **No change to the other lines `clean` prints**: the plan and result lines (`would restore <name> → <path>`,
  `restoring <name> ← <path>`, `skip …`) print names and paths that are not redacted today; whether paths are redacted is
  the engine-wide question P-014 left open (its open question 2), not this one
- **No change to `hookOwnedNote`** (decided in P-014) nor to `egress.snippet`'s defensive re-redaction (not the inverted
  shape; see "What was measured")

## Must not claim

- Not that the worst-finding line ever leaked: it quotes only tool-written titles, measured above; it moves to the
  helper so the file has one order, and a test pins why
- Not that the preview now redacts every secret: `Redact` is best-effort by shape (`internal/redact` package doc); the
  fix restores the order invariant #3 states, nothing more
- Not that `clean`'s output as a whole is redacted: the plan lines' paths are untouched (Out of scope)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The red tests and the reverse assertions, run on this base | `clean: tests pin that the restore preview redacts a secret before cutting it to the column limit (P-028)` |
| 2 | A local `redactClip` of the same shape as `detect.redactClip`, used by the four preview lines | `clean: the restore preview redacts each quoted line whole before cutting it to 100 bytes (P-028)` |
| 3 | Done block, `git mv` to `complete/`, index row | `proposals: P-028 (P-028)` |

## Open questions

1. **Export `detect.redactClip` and call it from `clean`, or keep a local helper of the same shape?**
   **Recommendation**: a local `redactClip(s) = clip(detect.Redact(s))` in `preview.go`. `detect.redactClip` cuts at 200
   bytes, the preview at 100; exporting it would mean a width parameter on a helper whose point is that a call site
   cannot get anything wrong. The pattern is reused (same name, same one-line body, redaction still only through
   `detect.Redact`); no second redactor appears.
   **Decided (2026-10-09)**: as recommended.
2. **Should the worst-finding line move to the helper too, although it cannot leak today?**
   **Recommendation**: yes. All four lines then read the same way, and a future title that quotes file text (the test
   in the Done criteria fails first) gets the right order without anyone having to remember it. The output is unchanged
   for every current title (measured).
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #44 (2026-10-09; find the sha with git log --grep P-028)
Released: v0.20.0
Evidence: red → green — TestPreview_SecretAcrossTheColumnLimitIsRedacted (internal/clean/preview_redact_test.go), all four subtests FAIL on origin/main 155865b with the head printed before the cut mark (`… ghp_abcdefghij…` for the file line, the directory entry and the symlink target, `…&key=AIzaFAKE0F…` for the URL query), PASS on this branch; TestUndoDryRun_PreviewDoesNotPrintASecretHead FAIL → PASS the same way (`ddd…/ghp_abcdefghij…` under `would restore deadskill`)
Evidence: reverse assertions — TestPreview_OrdinaryTextIsUnchanged (four subtests: lines, entry names and a symlink target over 100 bytes render as `x[:100] + "…"`, the `⚠   EXEC-001 <title>` line unchanged), TestPreview_ShortSecretIsStillRedacted (three subtests) and TestPreview_WorstFindingLineQuotesOnlyToolText (46 rules, longest 83 bytes with its ID, none changed by Redact) PASS on origin/main and on this branch without a change; the existing internal/clean tests, TestUndo_CoordinatedRewriteSucceedsButIsAnnounced among them, PASS unchanged
Evidence: not doing — git diff --stat origin/main -- internal/detect internal/redact internal/judge internal/collect internal/clean/clean.go internal/clean/clean_test.go go.mod go.sum docs/rules.md is empty; internal/clean/preview.go is the only production file changed (the four call sites, the helper, one doc-comment sentence)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5, no new dependency
```
