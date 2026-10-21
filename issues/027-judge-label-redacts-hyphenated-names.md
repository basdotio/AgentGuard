<!-- SPDX-License-Identifier: MIT -->
# 027 — A long hyphenated skill name reads as a high-entropy secret: judge notes call two different skills `skill:<REDACTED>`

- **Category**: report defect (报告缺陷)
- **Severity**: low (低)
- **Status**: `已修复` — found 2026-10-20 (present in v0.28.1); fixed by P-085: the label is `kind:name` without `Redact`

## Problem

The judge names each artifact by a label, `detect.Redact(kind + ":" + name)` (`internal/judge/run.go`, `planFor`). The label
is never sent to the model. It names the artifact in three kinds of note:

- the `LLM-000` budget note (`artifactSpan`: "N planned call(s) covering … were NOT made");
- the `LLM-005` note listing quotes that did not ground;
- the notes about what an excerpt left out (`bounds.go`).

`Redact` ends with the entropy catch-all (`entropyTokenRE`, 24 or more of `[A-Za-z0-9+/_-]`, then `secretish`). A skill name is
one such run when it is long, hyphenated and carries digits, so the whole name becomes `<REDACTED>`:

- the report lists the artifact as `migrating-tailwindcss-v4`;
- the note reads `skill:<REDACTED>`.

Two such skills read the same in every note. The reader cannot tell which artifact went unchecked or which quote failed to
ground. That is the one fact these notes exist to give (invariant #5: an omission is never silent, and it must say what was
omitted).

The credential pass reaches names too. `sk-[A-Za-z0-9_-]{16,}` (the OpenAI-style key) matches any name that starts with `sk-`
and continues for 16 characters. None of the measured real names does.

The same catch-all applies, by design, to the text sent to the model (invariant #3: redact before egress). A description
that names the skill (`see migrating-tailwindcss-v4 docs`) reaches the model as `see <REDACTED> docs`. That is the price of
the catch-all and is not what this issue is about; it is recorded so that a fix to the label does not get mistaken for a fix
to the excerpts.

## Measured

`name:` from the frontmatter of the corpus skills (`../agent-artifact-corpus`, 2026-10-20), run through `redact.Secrets`:

| Class | Names | Changed by `Secrets` | Of which by the credential pass |
|---|---|---|---|
| benign | 2,794 | 6 (0.21%) | 0 |
| malicious | 392 | 0 | 0 |
| hard-negative | 3 | 0 | 0 |

The six: `gbp-audit-g3-area-scheme`, `hd2-bilingual-doc-verify`, `000-jeremy-content-consistency-validator`,
`migrating-tailwindcss-v4`, `sysml-v2-guide-validator`, `ln-110-project-docs-coordinator`.

The other name shapes the judge labels are not affected:

- memory (`projects/-Users-…/memory/MEMORY.md`), whose segments are separated by `/`;
- plugin skills (`… (plugin x@y)`);
- MCP servers, hooks (`PreToolUse[Bash]#0`), commands and instructions.

The corpus's own directory names carry source prefixes (`sk-…`, `md-…`) and redact far more often. They are not names a user
installs, so they are not counted.

## Reproduction

A directory `migrating-tailwindcss-v4/` holding a `SKILL.md` with a `curl … | sh` line, and a config with `llm.enabled: true`,
`samples: 3`, `max_calls: 1`. Then `aguard check migrating-tailwindcss-v4 --llm --json`:

- the artifact's `name` is `migrating-tailwindcss-v4`;
- the `LLM-000` note says `9 planned call(s) covering skill:<REDACTED> were NOT made`.

## Direction

Not decided; measure before choosing.

- **The label without `Redact`.** The report already prints names the scanner listed from disk as they are (invariant #3 leaves
  those names outside `Redact`, an engine-wide question still open), so the notes would match the report. The label is not sent to the model. Before choosing this, check that no other path copies the label into a request
  or a snippet.
- **Keep `Redact`, disambiguate.** When the label changed under `Redact`, add something the reader can match to the report,
  such as the artifact's index or the first digits of its content hash.

Either way, add a test that two skills whose names redact are told apart in an `LLM-000` budget note. Leave the catch-all's
threshold alone: it is shared with every snippet and excerpt, and lowering its reach to spare names would let real tokens
through.

## Fixed (P-085)

The first direction was taken: one helper, `artifactLabel` (`internal/judge/run.go`), builds the label as `kind:name`
without `Redact`, for `planFor` and `unaskedNote` alike, so the notes name an artifact as the report lists it. The label
is still never sent to the model, and the excerpts are unchanged: a description that names its skill still reaches the
model as `see <REDACTED> docs`. Pinned by:

- `TestRun_NotesNameArtifactsAsTheReportDoes`: two redaction-shaped skills, both named in the budget note;
- `TestUnaskedNote_NamesArtifactsAsTheReportDoes`;
- `TestPlan_ExcerptsStillRedactNames` (reverse: the payload still redacts the name).
