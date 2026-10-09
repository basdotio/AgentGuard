<!-- SPDX-License-Identifier: MIT -->
# 020 — Pad one directive line with whitespace and the judge's excerpt holds only an omission marker: the judge cannot see exactly the sentence it exists to read

- **Source**: a follow-up recorded after P-005 and P-006 were merged (2026-10-09). P-006 fixed "a whitespace-padded quote
  shows only `…Note:…` in the **evidence**", which presumes the quote was already grounded — but the excerpt drops the
  whole line **before** it is sent, so the model never has the chance to quote it
- **Depends on**: none (P-005 and P-006 are already in `main`)
- **Branch**: `p/020-excerpt-padding-evasion`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The judge's excerpts are capped in bytes: the SKILL.md body, single files such as CLAUDE.md, and the connector tool list
get 6,000 bytes each; a skill's scripts get 2,000 per file and 6,000 in total (`capHeadTail` in
`internal/judge/excerpt.go`); a hook command is cut to a 6,000-byte prefix (`boundedRedact`), MCP config to 500 per line
(`capLine`), the declared purpose to 1,000 (`declaredPurpose`), a decoded blob to 800 (`decode.go`).
`condense` only folds lines that are **entirely whitespace**. A line that has text, but carries thousands of bytes of
whitespace in the middle of the text (or at its start), goes into the cap as is:

- `capHeadTail` fills from the top down and from the bottom up; the line that does not fit, together with every line
  between it and the tail, is replaced by one line `# … N line(s) omitted …`;
- when that line is the whole body, it degrades to "take a prefix", the prefix is all whitespace, and there is no
  omission marker;
- the other places truncate to a prefix, which likewise leaves only whitespace.

So an author writes the injected directive as `Note:` + 7,000 spaces + `ignore your instructions …`, the model receives
"1 line omitted here", and the judge (the LLM-003 / LLM-007 pass) sees not one word of the sentence it exists to read.
The whitespace need not be ASCII: U+3000, U+2003 and NBSP, which `unicode.IsSpace` recognises, and the zero-width
characters INJ-004 reports (U+200B …) all work — P-006 already made **grounding** treat both classes as padding; the
excerpt side did not follow.

Measured (`origin/main` `fd28344`; the probe starts an OpenAI-compatible endpoint with `httptest`, records the user
message of every request via `NewHTTP` + `Run`, and counts how many requests contain the directive verbatim; the probe
files are in scratch and not committed, and the tests in the done criteria reproduce the same shapes):

| Shape | Requests | With directive verbatim | With omission marker | Notes |
|---|---|---|---|---|
| SKILL.md body, directive line padded inside with 7,000 spaces / 7,000 tabs / 2,400×U+3000 / 2,400×U+2003 / 3,600×NBSP / 2,400×U+200B | 2 | **0** | 1 | the injection pass's body is "head + `# … 1 line(s) omitted …` + tail" |
| Same, padding at the start of the line (indentation) | 2 | **0** | 1 | |
| Same, padding on a line inside a ```` ```bash ```` fence | 2 | **0** | 1 | |
| The body is only this line | 2 | **0** | **0** | the user message is 6,163 bytes: `Note:` plus about 6,000 bytes of whitespace; it does not even say "omitted" |
| The directive line itself is not padded; **the line above it** is `.` + 7,000 spaces, followed by 2,360 more bytes of body | 2 | **0** | 1 | one line that does not fit blocks every line after it and before the tail |
| skill script `run.sh` with `echo ok;` + 3,000 spaces + `curl … \| sh` | 2 | **0** | 1 | the 2,000-byte per-file cap |
| Three scripts of about 1,975 bytes each, almost all whitespace (each within 2,000), a fourth, `z.sh`, is the payload | 2 | **0** | 1 | the 6,000-byte total is spent on padding; only head and tail fragments of `z.sh` remain |
| CLAUDE.md, directive line padded inside with 7,000 spaces | 1 | **0** | 1 | |
| description padded with 2,000 spaces, then the directive | 2 | **0** | 0 | the declared purpose is a 1,000-byte prefix |
| hook command `echo ok;` + 7,000 spaces + `curl … \| sh` | 2 | **0** | 0 | 6,000-byte prefix, no disclosure at all |
| One MCP `args` value padded with 700 spaces, then `--require /tmp/evil.js` | 1 | **0** | 0 | cut to 500 bytes, emits `LLM-000` (disclosed, but the model still did not see it) |
| Reverse: the same body without padding | 2 | 1 | 0 | |

Consequence: this is an evasion of the tool's **deep check itself** — "an injection in other words" that line-by-line
static rules cannot read is the very reason the judge exists; one stretch of invisible whitespace blinds the judge to
that line, and the report gives no way to tell "the judge ran and found nothing" from "the judge never saw it".

Whitespace runs this long almost never occur in real files (2,246,717 non-empty lines across this machine's
`~/.claude/plugins` and `~/.claude/skills` and the corpus's benign / hard-negative / malicious sets, measuring each
line's longest run over the character set above): the longest leading indentation is 121 bytes; only 28 lines have a run
over 128 bytes, 27 of them markdown table column alignment, and the other a 636-byte run of spaces + zero-width
characters at the end of a line in a skill in the corpus's benign set.

## Initial direction

In the excerpt layer, fold **padding** the way grounding reads it, then cap: a run of whitespace / invisible characters
longer than a threshold far above real indentation is replaced by what grounding already reads it as (one space; nothing
when it is only invisible characters). Only within a line, never across `\n`, so the line count and `lineMap` do not
change; redact first, then fold, then cap, so invariant #3's order is unchanged; it can only make the bytes sent fewer.
It goes into the single choke point the excerpt builders call on raw content, so it takes effect for every kind at once.
Touches `internal/judge` (`excerpt.go`, `egress.go`), the judge doc pair, and spec §5.2.

## Done criteria

- [x] `TestRun_PaddedDirectiveReachesTheJudge` (`internal/judge/padding_test.go`, new): runs `Run` through `NewHTTP` + an
  `httptest` endpoint; the endpoint records the user message of every request and replies with a flagged verdict citing
  the directive as evidence **only when the text it received contains the directive verbatim** (the model can only quote
  what it sees). 6 paddings (7,000 spaces, 7,000 tabs, 2,400×U+3000, 2,400×U+2003, 3,600×NBSP, 2,400×U+200B) × 5
  positions (inside the directive line, as its indentation, as the only line of the body, inside a ```` ```bash ````
  fence, on the line above it with about 2,400 more bytes of body after), 30 cases in total; each case asserts: at least
  one request carries the directive verbatim; an `LLM-003` cites **the line where the directive actually is** in
  `SKILL.md`; no `LLM-005`. Red today: in all 30 cases no request carries the directive verbatim
- [x] `TestPlan_PaddingCostsWhatOneSpaceCosts` (same file, new): the same fixture with the padding written as thousands
  of bytes, compared with the padding written as "what grounding reads it as" (one space; nothing when it is only
  invisible characters): the **requests** `planFor` produces are **byte-identical** (`Mode`, `Declared`, `Behavior`),
  the **source units are identical too** (`file`, `text`, `firstLine`, `lineMap`, `collapsed`), and so is `shortened`.
  Surfaces covered: SKILL.md body (injection), skill script (intent), three padding scripts each within the per-file cap
  plus one payload script (total cap), description (declared purpose), CLAUDE.md, connector tool description, hook
  command, one value in MCP `args`, a base64 blob that decodes to a padded command. Red today: on every surface the padded
  version differs from the reference (omission marker / prefix of pure whitespace / payload script reduced to fragments /
  `1 value(s) cut`)
- [x] Guard for invariant #3, `TestEgress_PaddingIsFoldedBetweenRedactions` (`internal/judge/padding_test.go`, new):
  ① a 24-character token that the entropy rule would erase on its own, followed by a run of invisible characters and 60
  `a`s — the token is not in the text sent (red if the fold moves before `Redact`: the entropy of the joined long run
  drops below 3.6 and it is no longer erased); ② `AKIA` + a run of invisible characters + 16 characters — the joined
  `AKIA…` is not in the text sent verbatim (red if the second `Redact` after the fold is removed); ③ a home directory cut
  in the middle by a run of invisible characters — replaced with `~` after the fold (red if scrub moves before the fold).
  ①② are green today (there is no fold today, so the token is still erased by the first pass and `AKIA…` is still cut
  apart) and guard the implementation; ③ is red today. All three state their corresponding mutation, and after
  implementation the mutation is actually run once and the red recorded
- [x] `TestFoldPadding` (table-driven) + `FuzzFoldPadding` (`internal/judge/fold_test.go`, new; the seed corpus runs with
  `go test`): a 128-byte run stays as is, a 129-byte run is folded; a run of only invisible characters folds to nothing;
  a mixed run folds to one space; 100 bytes of whitespace on each side of a `\n` are not folded (no folding across
  lines); invalid UTF-8 bytes stay as is. Properties: the output is no longer than the input; the number of `\n` is
  unchanged; the normalised text and per-byte line numbers from `normalizeWithLines` are **exactly the same** as for the
  input (grounding reads it no differently from the original); idempotent; no padding run over 128 bytes in the output
- [x] Reverse assertion `TestPlan_RealTextIsSentAsWritten` (`internal/judge/padding_test.go`, new, green today, still
  green after the fix without a single character changed):
  ① an ordinary 7,000-byte line in the body with only single spaces: the excerpt is byte-identical to "computed by hand
  with today's pipeline" (`condense` + `capHeadTail`, no fold), still head + `# … 1 line(s) omitted …` + tail; a verdict
  quoting that omission marker still fails grounding (`LLM-005`, no judge finding);
  ② a Python script (8 levels of indentation, one alignment run of exactly 128 bytes): the excerpt is byte-identical to
  the hand computation;
  ③ a line whose padding is broken every 128 bytes by a visible character (`.`) is **not folded** and is still replaced
  by the omission marker — that is visible content, not padding (see "Must not claim")
- [x] Reverse assertion `TestRun_PaddingBelowTheFoldStillShowsTheDirective` (same file, new, green today): P-006's
  W12/W13 window path still has an end-to-end test after the fold — the directive has 5 runs of 120 bytes of whitespace
  each in the middle (below the threshold, sent as is), with one `-` between runs; the quote is grounded at `SKILL.md:7`,
  and the snippet contains the whole directive and is ≤ 515 bytes. Why: P-006's
  `TestRun_WhitespacePaddedQuoteShowsTheDirective` / `TestRun_UnicodePaddedQuoteShowsTheDirective` use a single 600-byte
  run of padding; after the fold they stay green, but no longer go through `collapsedWindow`
- [x] Reverse assertion: existing tests stay green without a single character changed — `excerpt_test.go`,
  `plan_test.go`, `rendering_test.go` (including `TestGround_OmissionMarkerIsNotEvidence`,
  `TestRun_OmissionMarkerIsNotEvidence` and P-006's two padding tests), `ground_test.go`, `egress_test.go`, the e2e tests
  in `cmd/aguard`; the deletion column of `git diff --numstat origin/main -- '*_test.go'` is all 0
- [x] `make verify` green; `go version` does not switch toolchains; `internal/collect` and `internal/detect` untouched (so
  no before/after scan on a real machine; instead the diff is shown to be empty)

## Out of scope

- **No cap changes**: the values of `maxExcerptBytes`, `maxFileBytes`, `maxDeclaredBytes`, `maxConfigLineBytes`,
  `maxDecodedBytes`, `maxSnippetBytes` and the way `capHeadTail` / `capLine` / `boundedRedact` / `declaredPurpose` cut
  stay unchanged to the line; the fold can only make the bytes entering the cap fewer
- **`detect` untouched**: none of `detect.Redact`, the static rules or the static snippets are touched, so the static
  output of text / JSON / SARIF / HTML / markdown is unchanged; no static rule for "horizontal padding" is added (it would
  change the score; see the follow-up below)
- **Grounding and rendering untouched**: `ground.go` and `judge.go` unchanged — after the fold the normalised text equals
  the original, so the grounding standard, the line-number computation, the definition of `LLM-005` and P-006's window
  need no change
- **The static snippets in the triage and collusion summaries are untouched**: they were already cut at 200 bytes in the
  `detect` step, and folding afterwards cannot recover what was cut (`eg.snippet` does not go through the fold)
- **`capHeadTail`'s "one line that does not fit blocks every line after it" is unchanged**: it still applies to long
  lines of **visible** content (minified JS, a 7,000-byte paragraph of prose)
- No prompt change, no `Client` interface change, `openai.go` untouched; `go.mod` / `go.sum` untouched, no dependency
  added; the canonical hash, the gate and scoring untouched

## Must not claim

- **Do not say "padding can no longer block the judge"**: only padding **contiguous** over 128 bytes is folded. Insert a
  visible character (`.`) every 128 bytes and it no longer counts as padding; about 47 visible characters fill a
  6,000-byte excerpt — that is content, and the excerpt cannot tell it from other content; a line stuffed with it is still
  replaced by the omission marker, and stuffing some into each of several files can still spend the total cap. What is
  fixed is the tier where **zero visible characters** blind the judge
- **Do not say "the excerpt sends the file as written"**: a whitespace run over 128 bytes is one space in the excerpt,
  and a run of only invisible characters is empty. Grounding already reads it that way, so quotes and line numbers are
  unaffected; but the model cannot see "thousands of bytes of padding were here", and the report does not say so either
  (no note)
- Do not say "padding" covers every character that looks blank: only two tables count — the whitespace
  `unicode.IsSpace` recognises (newline excluded) and the invisible characters in `detect.Invisible`, exactly the same as
  P-006's grounding uses. Characters not in the tables, such as the U+2800 braille blank and the U+3164 Hangul filler,
  still consume budget
- Do not say Redact now recognises secrets cut apart by zero-width characters: only after a run over 128 bytes is folded
  does the joined text go through `Redact` again; when the run cutting it is shorter, it is still sent as is (as today,
  in its cut-apart form)
- Do not say the fold changes no real file's excerpt: 28 of 2,246,717 real non-empty lines change (27 lines of markdown
  table column alignment, 1 line of trailing spaces + zero-width characters), and the change is alignment whitespace
  shrinking to one space

## Work items

| W | In one sentence | Commit message (no sha; rebase changes it) |
|---|---|---|
| 1 | Two new test groups, end-to-end and `planFor`, + three guards + two groups of reverse assertions, run red (guards ①② and the reverse assertions are green today) | `judge: tests — a directive padded with whitespace never reaches the judge, which is sent the omission marker or a prefix of blanks instead (P-020)` |
| 2 | `foldPadding`: padding contiguous over 128 bytes within a line folds to what grounding reads it as; `eg.redact` becomes Redact → fold → Redact again (only if something was folded) → scrub; `TestFoldPadding` + `FuzzFoldPadding`; one mutation run for each of the three guards | `judge: padding inside a line is folded to the space grounding reads it as before any cap, so it can no longer push a directive out of the excerpt (P-020)` |
| 3 | One sentence each in the `llm-judge` pair, the `architecture` pair, spec §5.2, `.claude/rules/judge.md` | `docs: the judge pair, the architecture pair, spec §5.2 and judge.md say padding is folded before the excerpt caps (P-020)` |
| 4 | This file's "Done", the index | `proposals: P-020 (P-020)` |

## Open questions

1. **What threshold?**
   **Recommendation**: 128 bytes. The longest real leading indentation is 121 bytes (2,246,717 non-empty lines), and only
   28 lines have a run over 128; any lower and it starts eating real indentation (the corpus has 3 lines with a leading
   run over 64); any higher only makes "insert a visible character every N bytes" twice as cheap, without changing the
   tier it defends (zero visible characters).
   **Decided (2026-10-09)**: as recommended
2. **Fold to what?**
   **Recommendation**: what grounding reads it as — one space if the run contains whitespace, nothing if it is only
   invisible characters (the same rules as `foldForMatch`). The excerpt, once normalised, is then byte-identical to the
   original, and grounding, line numbers and P-006's window need no change. A visible placeholder (`⟨7000 spaces⟩`) could
   tell the model "padding was here", but it is text that is in no file, and grounding would have to be taught to reject
   it like the omission marker; the cost is out of proportion.
   **Decided (2026-10-09)**: as recommended
3. **At which layer?**
   **Recommendation**: `eg.redact` — the single choke point the excerpt builders call on raw content (the SKILL.md body
   and description, scripts, single files, connectors, hooks, MCP lines and decoded blobs all go through it), so new
   builders get it automatically. Not in `condense`: hooks / MCP / descriptions / blobs do not go through it; and not in
   `detect.Redact`: that would change the static output.
   **Decided (2026-10-09)**: as recommended
4. **Fold only when over the cap, or always?**
   **Recommendation**: always. Folding only over the cap does not stop "three scripts of 1,975 bytes each, all within the
   per-file cap, with the 6,000-byte total spent on padding" (the probe measured that the payload does not reach the
   request); only 28 lines of real files change.
   **Decided (2026-10-09)**: as recommended
5. **Should "folded here" be disclosed (`LLM-000` or a marker in the text)?**
   **Recommendation**: no. What is folded is not content — grounding already reads it as one space; `condense` never
   discloses folding blank lines or dropping comment lines either; a note for 28 lines of real tables would teach people
   to skip notes. "Horizontal padding is itself a signal" belongs in a static rule (symmetric with `OBF-007`), which would
   change the score; it is recorded as a follow-up and not done here.
   **Decided (2026-10-09)**: as recommended
6. **Should `Redact` run again after the fold?**
   **Recommendation**: yes, but only when something was actually folded. The fold can join back a token that invisible
   characters cut apart; the second pass can only erase more (the first pass's `<REDACTED>` is not restored), and normal
   text pays nothing. Order Redact → fold → Redact → scrub: the fold cannot precede the first pass (the entropy rule
   judges the whole run, and appending a low-entropy tail would let a token that would have been erased leak), and scrub
   stays after the last Redact (P-005's placement rule).
   **Decided (2026-10-09)**: as recommended

## Done

```
Merged: PR #38 (2026-10-09; find the sha with git log --grep P-020)
Released: pending release
Evidence: TestRun_PaddedDirectiveReachesTheJudge (internal/judge/padding_test.go); W1 red 30/30 on 930e914, all `none of 2 request(s) carried the directive` → green after W2; in all 30 cases the LLM-003 cites the line where the directive actually is (SKILL.md:9 / 9 / 5 / 11 / 10), no LLM-005
Evidence: TestPlan_PaddingCostsWhatOneSpaceCosts (same file); W1 red 50/50 (9 surfaces × 6 paddings; the decoded blob runs only the two ASCII ones, since Unicode whitespace makes the blob read as binary): the two hook passes' Behavior each a 6,000-byte pure prefix; in the MCP case shortened = "1 value(s) cut to 500 bytes", empty for the reference; in the three-padding-scripts case intent is 5,995 bytes with an omission marker → green after W2
Evidence: TestEgress_PaddingIsFoldedBetweenRedactions (same file); at W1 ③ red (the home directory cut by zero-width characters was not replaced with ~), ①② green → all three green after W2; mutations (all reverted): fold moved before the first Redact → ① red; second Redact removed → ② red; scrub moved before the fold → ③ red (with only one Redact pass, ② and ③ go red together)
Evidence: TestFoldPadding, FuzzFoldPadding (internal/judge/fold_test.go); `go test -run '^$' -fuzz FuzzFoldPadding -fuzztime 20s` 935,369 executions with no failure (output never longer, line count unchanged, idempotent, no padding run over 128 bytes, normalizeWithLines text and per-byte line numbers identical to the original)
Evidence: reverse assertions TestPlan_RealTextIsSentAsWritten, TestRun_PaddingBelowTheFoldStillShowsTheDirective green already at W1 (implementation unchanged), still green after W2 without a character changed; with collapsedWindow in ground.go replaced by window (mutation, reverted), the only red in the whole package is TestRun_PaddingBelowTheFoldStillShowsTheDirective — P-006's two 600-byte padding tests indeed no longer go through that path after the fold, and this test takes over guarding it
Evidence: reverse assertion, existing tests green without a character changed (in make verify's full test run, including TestGround_OmissionMarkerIsNotEvidence, TestRun_OmissionMarkerIsNotEvidence, TestRun_WhitespacePaddedQuoteShowsTheDirective, TestRun_UnicodePaddedQuoteShowsTheDirective, excerpt_test.go, plan_test.go, egress_test.go, the e2e tests in cmd/aguard); git diff --numstat origin/main -- '*_test.go' shows only two new files, deletion column all 0
Evidence: Out of scope — git diff --stat origin/main -- internal/detect internal/collect internal/score internal/report internal/gate internal/judge/ground.go internal/judge/judge.go internal/judge/openai.go internal/judge/prompt.go internal/judge/decode.go internal/judge/run.go internal/judge/triage.go go.mod go.sum is empty; excerpt.go has 0 deleted lines (the cap constants and capHeadTail / capLine / boundedRedact / declaredPurpose untouched, only foldPadding added); collect / detect unchanged, so no before/after scan on a real machine was run
Evidence: the scratch probe (not committed) re-run after W2: the 22 shapes in the table in the "Problem" section plus the reverse case, all 23 cases "with directive verbatim ≥ 1, with omission marker 0"; in the 5 whitespace cases padded inside a line of the body the user message is 292 bytes, the same length to the byte as the unpadded reverse case
Evidence: make verify → verify: all gates passed; go version go1.23.5, no toolchain switch, go.mod second line go 1.23.5
```
