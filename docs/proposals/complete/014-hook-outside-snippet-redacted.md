<!-- SPDX-License-Identifier: MIT -->
# 014 — In the evidence of the hook out-of-bounds finding, the resolved path after the arrow is not redacted

- **Source**: the snippet of `HOOK-002` is assembled as `redactClip(ref) + " → " + resolved`; the resolved path is not
  redacted, so what was redacted out of the first half appears in clear after the arrow; the registry address in the
  `Why` of `SUP-006` is the same. Ported from P-056 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/014-hook-outside-snippet-redacted`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

When a script referenced by a hook command resolves to outside HOME, the evidence snippet of `HOOK-002` is assembled
like this (`internal/detect/hooks.go:361`):

```go
Snippet: clip(redactClip(ref) + " → " + resolved)
```

The `ref` before the arrow is copied out of the hook command and passes through `redactClip`; the `resolved` after the
arrow is the same bytes after expanding `~`/`$HOME`/`$CLAUDE_PROJECT_DIR` or joining with home, and **not one byte of it
is redacted**. For an absolute-path reference, `resolved` equals `ref`. So what the redactor wipes from the first half
is printed in clear in the second half — invariant #3's "`detect.Redact` is the only way a snippet is produced" does not
hold here.

Reproduction (binary built from this repository's `main` dec64ca, `HOME` pointing at the home inside the fixture, two
hooks: `sh <outside-HOME>/opt/ghp_<36 chars>/hook.sh` and `sh <outside-HOME>/outside/guard.sh`,
`scan --json --inbox off`):

- The snippet of the `HOOK-002` with the token:
  `<REDACTED><REDACTED>/hook.sh → /tmp/aguard-scratch/p014fx/opt/ghp_<36 chars>/hook.sh` — the token appears in clear
  after the arrow
- The same reference in the `COV-000` note: `<REDACTED><REDACTED>/hook.sh` — the same bytes, redacted here
- The `HOOK-002` for the other, ordinary path: `<REDACTED>.sh → /tmp/aguard-scratch/p014fx/outside/guard.sh` — entropy
  detection swallows the whole first half of an ordinary long path too, while the second half is printed as is

This snippet goes into JSON, HTML, markdown (`check --md -` is meant for pasting into PR comments), SARIF (uploaded to
code scanning) and the terminal report. The judge's path redacts the evidence again in both places (`triageItems` in
`judge/run.go`, `judge/excerpt.go`), so the leak surface is the report, not the model endpoint.

**Sweep for the same class** (looking in `internal/detect`, `internal/collect` and `internal/permcheck` for "the same
bytes redacted in one place and in clear in another"): the `Why` of `SUP-006` (package source redirected) splices the
target taken from the file line into the sentence (`internal/detect/shape.go:194-204`), while the same line is redacted
in the snippet. When no host can be parsed from the target (a port written as `${PORT}`, a form `url.Parse` rejects; no
host after the userinfo; a broken default in `${VAR:-…}`), what gets spliced in is the raw target string. In the same
fixture a skill script writes `npm config set registry https://ci:ghp_<36 chars>@npm.corp:${PORT}/`: the snippet is
`npm config set registry https://ci:<REDACTED>@npm.corp:${PORT}/`, and the `Why` contains the full token. Across the
whole JSON the token appears 2 times (once after the `HOOK-002` arrow, once in the `SUP-006` `Why`).

Consequence: a user has put a secret into a path or URL (a directory name, a credential in a registry address); the
tool wipes it for them in one place and prints it for them in another place of the same finding; the more the report is
passed on (PR comments, SARIF upload), the less this one place should be an exception.

## Initial direction

`HOOK-002`: assemble the whole `ref → resolved` string first, then pass it through `redactClip` once as a whole (the
order redact-then-truncate is unchanged); the snippet for ordinary paths is identical to today's, character for
character. `SUP-006`: redact the target before splicing it into `Why`. `detect.Redact` itself does not change. Other
places of the same shape are listed one by one; places of a different shape are not fixed here.

## Done criteria

All fixtures are built on the fly in `t.TempDir()`; the token is an obviously fake `ghp_` plus 36 characters
(`redact_test.go` already uses the same form), which `Redact`'s known-prefix table recognises.

- [x] `TestHookOutside_SecretInPathIsRedactedOnBothSides` (`internal/detect/snippet_redact_test.go`, new): the hook
  `sh <outside-HOME>/opt/<token>/hook.sh` goes through `Engine.Run`, and the token is absent from the Title, Why,
  Evidence File and Snippet of both `HOOK-002` and `COV-000`; the `HOOK-002` snippet still has the `… → …` shape, both
  sides end in `/hook.sh`, and there is a `<REDACTED>` after the arrow. Then `hookOutsideFinding` is called directly to
  pin two literal values: an absolute reference (`/opt/vault/<token>/hook.sh`, the same on both sides) and a reference
  that differs between the sides (`$HOME/../vault/<token>/hook.sh → /home/vault/<token>/hook.sh`); after the fix they
  are `/opt/vault/<REDACTED>/hook.sh → /opt/vault/<REDACTED>/hook.sh` and
  `$HOME/../vault/<REDACTED>/hook.sh → /home/vault/<REDACTED>/hook.sh` respectively. Red today: the token is in clear
  after the arrow
- [x] `TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts` (same file, new): three forms where "no host can be read
  from the target" (a `${PORT}` port, no host after the userinfo, a broken `${VAR:-…}` default) plus a token-shaped host
  name; the token is absent from the `Why` of `SUP-006` in all of them, and the snippet never had it. Red today
- [x] `TestScan_PathSecretsNeverReachARendering` (`cmd/aguard/redact_render_test.go`, new): the same fixture (the hook
  above, plus `npm config set registry https://ci:<token>@npm.corp:${PORT}/` in a skill script) goes through `scanEnv`,
  and the token is absent from all six renderings — JSON (encoded with the same indentation as the CLI), terminal
  (plain and `--verbose`), markdown, SARIF, HTML — while `HOOK-002` and `SUP-006` are both present. Red today
- [x] Reverse assertion: the `HOOK-002` snippet for ordinary paths is **character for character the same** as today —
  `TestHookOutside_OrdinaryPathSnippetUnchanged` (same detect file) pins literal values with a table
  (`/opt/acme/hooks/guard.sh`, `/Applications/… 3.app/…/unibase-hook.js` with spaces (the real shape in the header
  comment of `hooks.go`), a `~/…` expansion that differs between the sides, and one longer than the 200-byte truncation
  cap), and every row equals the result computed in the test with today's formula
  `clip(redactClip(ref) + " → " + resolved)`; green before and after the fix
- [x] Reverse assertion: `HOOK-002` stays dim 4 medium for ordinary events and dim 4 high for `PermissionRequest` (the
  previous item's table runs once for each event); `SUP-006` stays dim 5 high, and the `Why` for an ordinary
  `${CORP_REGISTRY}` and for a known host `https://npm.evil.example/` is unchanged character for character — pinned as
  literal values by `TestRegistryRedirect_OrdinaryWhyUnchanged` (same detect file); green before and after the fix
- [x] Reverse assertion: existing tests stay green without a single change — `internal/detect/hooks_test.go` (including
  `TestHookNotes_DoNotRedactTheScannerOwnPaths`, which pins that the resolved path of `hookOwnedNote` is **not**
  redacted), `shape_test.go`, `shape_severity_test.go`, `redact_test.go`; `git diff --stat origin/main` is empty for
  these four files
- [x] On a real machine, `scan --root ~/.claude`: the JSON before and after the fix, with `scanned_at` and
  `tool_version` removed, is byte-for-byte identical, or differs only in the two fields this proposal changes
- [x] `.claude/rules/detect.md` still has no more than 200 lines (`maxRuleLines` in `cmd/aguard/claude_rules_test.go`)
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **Do not change `detect.Redact`, `redactClip` or `clip`**, nor the character class of entropy detection (the `/` in
  paths makes a whole long path count as one token; that is a property of `Redact`, see open question 1)
- **Do not change `hookOwnedNote`** (`internal/detect/hooks.go:325`): literally the same shape, but its resolved path is
  a file path **inside** the plugin tree, which by the engine's convention "paths the scanner located are only
  truncated" is deliberately not redacted, and a test pins that (open question 2). Only the comment on
  `hookOutsideFinding` states why the two differ
- **Do not change collect's notes**: the `"@" + ref` of the four notes in `imports.go`, the plugin name at
  `plugins.go:192`, the `err.Error()` and entry lists in `collect.go`, the file names in
  `connectors.go`/`unowned.go`/`loaded.go` — each is entirely in clear, without the "half redacted" shape, and collect
  cannot import detect (open question 3)
- **Do not change `unreadableNote`** (`internal/detect/detect.go:870`): entry names go into Why and the snippet in
  clear, while the similar `nonRegularNote` redacts them — inconsistent, but entirely in clear, not half and half; the
  entry names are paths inside the tree
- **Do not change `missing hook command: <cmd>` in `internal/gate/status.go:234`** (not in the three packages swept;
  entirely in clear)
- Do not change the severity, dimension, title or Why of `HOOK-002`, nor the `COV-000` note; the rule text is untouched
  and `docs/rules.md` does not change
- The spec does not change: invariant #3 already requires this, and this proposal brings the implementation back to it;
  the data model does not change
- No new dependencies; `go.mod` unchanged

## Must not claim

- Do not say "the secret was sent to the judge": both of the judge's paths redact the evidence again (`triageItems` in
  `judge/run.go`, `judge/excerpt.go`), and `Why` does not go to the judge; the leak surface is the report generated on
  the local machine, and the copies users repost or upload
- Do not say "every finding field is now redacted": collect's notes, `unreadableNote`, the resolved path of
  `hookOwnedNote`, and every `Evidence.File` are still in clear (see Out of scope)
- Do not say the `HOOK-002` evidence is always easier to read after the fix: when entropy detection treats the resolved
  path itself as a token (the `…/outside/guard.sh` one in "Problem"), before the fix the path was still visible after
  the arrow, and after the fix both sides are `<REDACTED>.sh`. This trades readability for "nothing leaks out of the
  other half", open question 1
- Do not say SARIF fingerprints are unchanged: `partialFingerprints` includes the snippet, so the fingerprints of the
  `HOOK-002`s whose resolved path the redaction changes will change (in code scanning the old alerts close and new ones
  open); those for ordinary paths do not change
- Do not say a `HOOK-002` was fixed on a real machine: the local `~/.claude` has 0 `HOOK-002`; the target of its only
  `SUP-006` is not changed by `Redact`

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Three new tests plus reverse assertions (two packages), run red | `detect, cmd: tests — a secret in a hook's out-of-home script path or a registry URL reaches the report in clear beside its redacted copy (P-014)` |
| 2 | `HOOK-002` assembles the whole string, then `redactClip`; a comment states why it differs from `hookOwnedNote` | `detect: HOOK-002 redacts the resolved path after the arrow the same way it redacts the reference before it (P-014)` |
| 3 | `SUP-006` passes the target through `Redact` before splicing it into `Why` | `detect: SUP-006 names the registry target in its explanation only after redacting it, as the snippet already does (P-014)` |
| 4 | The hook paragraph of `.claude/rules/detect.md` gains one guard-point sentence (no new line: the file has a 200-line cap, this repository is at 198 lines now, and P-010 adds two lines to the same paragraph) | `rules: detect.md says the HOOK-002 snippet is redacted whole and why the plugin attribution note is not (P-014)` |
| 5 | This file, the index | `proposals: P-014 (P-014)` |

## Open questions

1. **When entropy detection swallows the resolved path itself, both sides of the arrow are `<REDACTED>` after the fix;
   is that acceptable?**
   **Recommendation**: accept it. `Redact` looks only at shape and cannot tell "a long path" from "a secret with no
   prefix"; making that distinction for it here in `HOOK-002` would grow a second exit outside `Redact`, exactly what
   invariant #3 forbids. The remedy can only be in `Redact` itself (for example the `/` in the entropy-detection
   character class), which needs a separate proposal and a fresh measurement of false positives; this proposal does not
   touch it. The cost is bounded: a real shape like `/Applications/… 3.app/…/unibase-hook.js` with spaces is not changed
   by `Redact` and stays readable after the fix (pinned by the reverse table).
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
2. **`hookOwnedNote` has literally the same shape; change it too?**
   **Recommendation**: do not change it; state the difference in the comment on `hookOutsideFinding`. The resolved path
   of `hookOwnedNote` is a file inside the plugin tree; that whole tree is scanned, and the `Evidence.File` of any
   finding in it prints the same path in clear — the engine's convention is "paths the scanner located are only
   truncated", `TestHookNotes_DoNotRedactTheScannerOwnPaths` pins it, and the test's comment records the plugin path on
   a real report that entropy detection mangled into `<REDACTED>.5.<REDACTED>.cjs`. The resolved path of `HOOK-002` is
   not in any scanned tree; its bytes come only from the hook command, which puts it on the snippet side. Redacting
   paths inside trees as well is the engine-wide question "should `Evidence.File` be redacted", for a separate
   proposal.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
3. **Should collect's in-clear notes (`@import` references, plugin names, error strings, file names) get a separate
   proposal?**
   **Recommendation**: this proposal only lists them and does not fix them; a fix gets its own proposal. The fix must
   first decide where `Redact` lives: collect cannot import detect (detect depends on collect), so either `Redact` moves
   to a leaf package both can use, or detect passes collect's notes through it when merging them — both routes change
   how notes are produced, which is not the same task as this proposal's "bring one splice that slipped through back to
   the single exit". The closest to a real leak among them is the `@ref` in `imports.go` (instruction file body).
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
4. **`Redact` or `redactClip` for the `SUP-006` target?**
   **Recommendation**: `Redact`, no truncation. The target in `Why` has no length cap today; adding truncation would
   shorten, in `Why`, an overlong target that holds no secret, so the ordinary case would no longer be identical character for
   character. Redacting without truncating has no ordering question (invariant #3 governs the order when both steps are
   done); permcheck's snippet also uses only `Redact` (`permcheck.go:104`).
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
5. **Does `SUP-006` go in the same PR as the hook, or is it split out?** (the title mentions only the hook)
   **Recommendation**: the same PR. It was found by the same-class sweep this proposal requires, and it is the same fix
   (bring the half that slipped through back to `Redact`); it stands alone as one commit, W3, with its own red/green
   tests, so reverting it alone means reverting that one commit.
   **Decided (2026-10-09)**: as recommended (raised and decided during implementation in the former repo, kept on
   port).
6. **"Redacting the whole string once = redacting each half once" is an argument, not an exhaustive check; is that
   enough?**
   The basis for ordinary paths staying identical character for character: no `Redact` pattern can cross ` → ` (`→` is
   not in any value character class; `flagSecretRE`/`flagUserPassRE`, which could swallow it as a value, require a flag
   such as `--token` or `-u` immediately before it, and `ref` always ends in a script extension).
   **Recommendation**: enough. If a pattern that can cross whitespace is added in future, the whole-string form only
   redacts more (the safe direction); the character-for-character stability of ordinary paths is backstopped by
   `TestHookOutside_OrdinaryPathSnippetUnchanged` comparing row by row against the old formula, which turns red the day
   it stops holding.
   **Decided (2026-10-09)**: as recommended (raised and decided during implementation in the former repo, kept on
   port).

## Done

```
Merged: PR #25 (2026-10-09; find the sha with git log --grep P-014)
Released: pending release
Evidence: TestHookOutside_SecretInPathIsRedactedOnBothSides (internal/detect/snippet_redact_test.go); W1 red on this repository's main (dec64ca): under Engine.Run the HOOK-002 snippet is <REDACTED><REDACTED>/hook.sh → /var/folders/…/opt/ghp_<36 chars>/hook.sh, and in both literal-value subtests the token is in clear after the arrow → green after W2, both sides of the arrow are …/<REDACTED>/hook.sh
Evidence: TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts (same file); W1 red, all four subtests (${PORT} port, no host after userinfo, broken ${R:-…} default, token-shaped host name) have the full token in Why → still red after W2 (not its concern) → green after W3
Evidence: TestScan_PathSecretsNeverReachARendering (cmd/aguard/redact_render_test.go); W1 red: 2 occurrences each in JSON, terminal, --verbose, markdown, HTML, 3 in SARIF → after W2 1 each, 2 in SARIF (the SUP-006 Why remains) → after W3 0 in all six; HOOK-002 and SUP-006 both still present
Evidence: reverse assertion TestHookOutside_OrdinaryPathSnippetUnchanged (same detect file): four ordinary paths × two events, 8 subtests in total, snippet character for character equal to the value computed by the old formula, HOOK-002 dim 4 medium/high, green already at W1, still green after the fix without a single change; mutation (temporarily replacing the fix with redactClip(ref) + " → " + redactClip(resolved), reverted after the run, not committed) → the two rows over 200 bytes red (one each for PreToolUse and PermissionRequest)
Evidence: reverse assertion TestRegistryRedirect_OrdinaryWhyUnchanged (same file): the literal Why values for ${CORP_REGISTRY} and npm.evil.example, SUP-006 dim 5 high, green already at W1, still green after the fix
Evidence: reverse assertion TestHookNotes_DoNotRedactTheScannerOwnPaths (internal/detect/hooks_test.go, unchanged) green at every step W1–W4
Evidence: binary before/after (dec64ca vs this branch, fixture under /tmp, three outside-HOME hooks + one npm registry rewrite script, HOME pointing at the fixture, --inbox off): scan --json with scanned_at, tool_version removed differs in only 3 of 178 lines — in the SUP-006 Why ghp_<36 chars> → <REDACTED>; after the arrow of the HOOK-002 with the token ghp_<36 chars> → <REDACTED>; the one whose whole path entropy detection swallowed, <REDACTED>.sh → /tmp/aguard-scratch/…/outside/guard.sh, becomes <REDACTED>.sh → <REDACTED>.sh (the cost accepted in open question 1). Token occurrences 2 → 0; the ordinary short path /tmp/p014h/guard.sh → /tmp/p014h/guard.sh identical character for character; overall 69 → 69
Evidence: real machine ~/.claude, the two binaries run back to back: before and after the fix overall 69 / artifacts 175 / findings 806 / notes 10 / HOOK-002 0 / SUP-006 1, JSON with scanned_at, tool_version removed byte-for-byte identical (15678 lines)
Evidence: Out of scope — git diff --stat origin/main -- internal/detect/redact.go internal/detect/detect.go internal/collect internal/gate internal/permcheck docs/rules.md docs/spec go.mod go.sum internal/detect/hooks_test.go internal/detect/shape_test.go internal/detect/shape_severity_test.go internal/detect/redact_test.go is empty; both hunks in hooks.go are in hookOutsideFinding (the comment above it and the function body), hookOwnedNote untouched
Evidence: .claude/rules/detect.md 198 → 198 lines (sentence added within a line, no new line), TestClaudeRulesAreScopedToExistingPaths green
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 go 1.23.5
```
