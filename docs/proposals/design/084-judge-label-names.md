<!-- SPDX-License-Identifier: MIT -->
# 084 — The judge's notes name an artifact through `Redact`: a long hyphenated skill name becomes `skill:<REDACTED>`, and two such skills cannot be told apart

- **Source**: [issues/027](../../../issues/027-judge-label-redacts-hyphenated-names.md) (2026-10-20)
- **Depends on**: none
- **Branch**: `p/084-judge-label-names`

## Problem

The judge names each artifact by a label, `detect.Redact(kind + ":" + name)`. It is built in two places: `planFor` in
`internal/judge/run.go`, and `unaskedNote` in `internal/judge/unasked.go`. The label is never sent to the model. It is what
these notes say:

- the `LLM-000` budget note (`artifactSpan`);
- the `LLM-005` note about quotes that did not ground;
- the excerpt-shortening notes (`bounds.go`, `run.go`);
- the note on artifacts the judge asks nothing of (`unaskedNote`);
- the placeholder evidence of a verdict before grounding replaces it.

`Redact` ends with the entropy catch-all, so a long hyphenated name with digits is removed whole. The report lists the
skill as `migrating-tailwindcss-v4`, and the note says `skill:<REDACTED>`.

On the corpus skills' frontmatter names, 6 of 2,794 benign names (0.21%) change this way; the measurement is in
issues/027. Two such skills read the same. `artifactSpan` also de-duplicates by label, so a budget note that skipped both
names one `skill:<REDACTED>`. The reader cannot tell which artifact went unchecked, which is the fact the note is for
(invariant #5).

Every other place that names an artifact prints its name as it is:

- the terminal, Markdown and HTML reports, through `Sanitize` only;
- the JSON `artifacts[].name`;
- `aguard llm preview`.

Invariant #3 leaves the names the scanner lists outside `Redact` (an engine-wide question still open) and says not to draw a
line of one's own per note. The judge's label is the one note that drew one.

## Initial direction

Build the label in one helper, `kind + ":" + name` without `Redact`, and use it in both places. The notes then name the
artifact as the rest of the report does. Nothing sent to the model changes: the label is not in any request body, and
excerpts keep going through the egress transform.

## Done criteria

- [ ] `TestRun_NotesNameArtifactsAsTheReportDoes` (`internal/judge`):
  - two skills named `migrating-tailwindcss-v4` and `sysml-v2-guide-validator`, a `max_calls` budget that skips both, a stub
    client;
  - the `LLM-000` budget note names both, and neither as `<REDACTED>`.
- [ ] `TestUnaskedNote_NamesArtifactsAsTheReportDoes`: the note on artifacts asked nothing names a redaction-shaped name as is.
- [ ] Reverse: what is sent does not move.
  - A skill whose description repeats its redaction-shaped name still reaches the model with `<REDACTED>` in place of the name
    (the `Plan` payload, the same code the client sends).
  - `TestPromptVersion_EveryInputIsDecided` still lists `Request.Artifact` as never sent.
  - `judge-prompt=` and `judge-excerpt=` do not move.
- [ ] Reverse: a name that does not redact gives the same label as before (`skill:route-tester`), byte for byte.
- [ ] issues/027 → `已修复`, with the test names; docs where they describe the label.
- [ ] `make verify` green with `go version` go1.23.5 and no toolchain switch.

## Out of scope

- The entropy catch-all and the credential patterns. Their thresholds are shared with every snippet and excerpt, and narrowing
  them to spare names would let real tokens through.
- The text sent to the model. A description that names its skill keeps reaching the model as `see <REDACTED> docs`: the price
  of the catch-all, recorded in issues/027.
- Invariant #3's engine-wide question (whether names listed from disk should go through `Redact` anywhere). This proposal makes
  the judge's notes agree with the report, whichever way that question is later decided.

## Must not claim

- Never that names are "safe" or "contain no secrets". The label now prints the name as the artifact list in the same report
  already does: no more and no less.
- Never that the excerpts changed.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The proposal | `P-084: the proposal — the judge's notes name an artifact through Redact, so a hyphenated skill name reads <REDACTED>` |
| 2 | Tests, then one label helper without `Redact` in both places | `P-084: the judge names an artifact in its notes as the report does` |
| 3 | issues/027, docs | `P-084: docs — issues/027 fixed; the judge's label is the artifact's name` |
| 4 | Done | `P-084: done — evidence, and the proposal moves to complete` |

## Open questions

1. Drop `Redact` from the label, or keep it and add a disambiguator (the artifact's index, a hash prefix)? Recommended: drop
   it. Every renderer already prints the name as is, so a disambiguator would be a second way of naming the same artifact for
   the reader to match by hand. Keeping `Redact` protects nothing the same report does not already print.
   **Decided (2026-10-21)**: as recommended.
2. Should a hook's name, whose matcher is a configuration value, keep `Redact` in the label? Recommended: no, for the same
   reason: the artifact list prints `PreToolUse[<matcher>]#0` as is. Whether configuration-derived names should go through
   `Redact` in every renderer is invariant #3's open question, and it is answered once, for all renderers, not per note.
   **Decided (2026-10-21)**: as recommended.

## Done

```
Merged: PR <number> (<date>; find the sha with git log --grep P-084 after the merge)
Released: vX.Y.Z (fill in after stage 5)
Evidence:
```
