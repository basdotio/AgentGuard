<!-- SPDX-License-Identifier: MIT -->
# 037 — What the judge is sent is not always bounded, valid UTF-8 or a fixed point of redaction: grounding can compare against text the endpoint never received

- **Source**: new finding (2026-10-10), follow-up to P-005, P-006, P-020, P-027 and P-036: reading the payload builders
  after P-036 closed the one non-fixed-point case a corpus replay found
- **Depends on**: none (P-036 merged)
- **Branch**: `p/037-judge-payload-bounds`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

With `--llm`, a bounded, redacted excerpt of each artifact goes to the user's own endpoint, and a verdict survives only
if its quote can be found in what was sent (grounding, `internal/judge/ground.go`). Both promises rest on the fields of a
request being exactly the bytes the endpoint reads, within the caps the documentation states. Four places break that:

1. **Two fields have no cap that holds.** The deobfuscation pass joins up to 8 decoded payloads of up to 800 bytes each
   with `\n---\n` (`decodedPayloads`, `planFor`): up to 6,435 bytes, above the 6,000-byte excerpt cap
   `docs/llm-judge.md` states. The collusion pass sends `capabilityDigest`, one line per static capability finding, with
   no cap at all: a large skill sends every line it has.
2. **Byte-offset cuts can split a character.** `detect.clip` (every static snippet, which triage then sends),
   `capHeadTail`'s one-line case, `boundedRedact` (a hook's command) and `decodedPayloads` cut at a byte offset. A cut
   inside a multibyte character leaves invalid UTF-8, which `encoding/json` replaces with U+FFFD when the request is
   marshalled: the endpoint reads one text, grounding compares against another.
3. **Triage evidence is unbounded.** Each triage item is `file:line snippet`; the snippet is clipped, the path is not.
4. **Nothing makes an assembled field a fixed point of redaction.** Each part is redacted on its own and then joined,
   separated or cut; a pattern that spans the join, or a token that only looks secret once cut, is left for a second
   `Redact` pass to change. P-036 closed the one case a corpus replay found (an argv flag and its value); nothing
   guarantees the next.

### Measured on `origin/main` (6cab205)

A root built here (`/tmp/ag-scratch/037/fx`, five skills, a `CLAUDE.md` and a hook), planned the way
`aguard llm preview --root <fixture> --inbox off` plans it (`scanEnv` with the preview sink; nothing sent), each field
read from the planned payload and checked for length, `utf8.ValidString`, whether a JSON round trip (what the client's
`json.Encoder` does) gives the same text, and whether `detect.Redact` changes it:

| Fixture | Field | On main |
|---|---|---|
| skill, 8 base64 blobs decoding to 900 printable bytes each | deobfuscation behavior | **6,435 bytes** (cap stated: 6,000); nothing disclosed |
| skill with `EXFIL-002` and 60 capability findings in files two 60-byte directories deep | collusion behavior | **12,941 bytes**, every line; nothing disclosed |
| hook whose command is `echo ` + 2,100 × `中` | injection and capability behavior | 6,000 bytes, **invalid UTF-8**, changed by the JSON round trip |
| `CLAUDE.md` of one line, `x` + 2,100 × `中` | injection behavior | 6,000 bytes, **invalid UTF-8**, changed by the JSON round trip |
| skill with a blob decoding to 300 × `中` | deobfuscation behavior | 800 bytes, **invalid UTF-8**, changed by the JSON round trip |
| the same skill, `curl … \| bash # ` + 100 × `中` (fires a rule, snippet clipped at 200) | triage evidence | 223 bytes, **invalid UTF-8**, changed by the JSON round trip |
| skill with a script four 200-byte directories deep that fires a rule | triage evidence | **1,022 bytes** (no bound; a deeper path sends more) |
| skill whose `a.sh` ends `mytool login --token`, next file `b.sh` | intent behavior | **not a fixed point**: a second `Redact` turns the next header `# b.sh` into `<REDACTED> b.sh` |
| skill with decoded payloads `run the installer with --token` and `next step fetch the config file` | deobfuscation behavior | **not a fixed point**: a second `Redact` turns the `---` separator into `<REDACTED>` |

In the four invalid-UTF-8 rows the endpoint reads U+FFFD where the text grounding compares against holds the broken
bytes, so the text of a verdict's quote and the sent text are not the same string.

**The corpus, replayed here** (as P-036 did: every one of the 3,539 corpus samples placed by
`baselines/adapter/aguard.Stage` and run through `aguard llm preview --json`; this set contains every sample of the four
committed judge runs under `baselines/results/aguard/*-llm-*`): 8,770 planned payloads, of which **none** has a field
over its cap, a U+FFFD, or a field that a second `redact.Secrets` changes; 2 deobfuscation and 4 collusion payloads in
all. The four defects are reachable by content written to reach them, and not by any sample there — which is what makes
a byte-identical replay the reverse assertion for the fix.

## Initial direction

Drop whole decoded payloads and digest lines from the end until the field fits (with their grounding units), and
disclose it like the MCP excerpt's shortening (`LLM-000`). Cut on rune boundaries everywhere with the one helper that
already does it (`capBytes`), cap each triage item's evidence at 1,000 bytes after redaction, and make the last step of
building every field a whole-field `Redact` (re-capped if that lengthened it), pinned by a fuzz test: every built field
is a fixed point of `Redact`, valid UTF-8 and within its cap. Bumps `ExcerptVersion`.

## Done criteria

- [x] `TestPlan_ExplainAndDigestFitTheExcerpt` (`internal/judge/bounds_test.go`): the two oversize fixtures above give a
  deobfuscation and a collusion behavior of at most 6,000 bytes, made of whole payloads / whole digest lines (each unit's
  text appears in the field, and no unit is left for text that was not sent), a `shortened` naming how many were left
  out, and an `LLM-000` from `schedule` naming the artifact. **Red on the base**: 6,435 and 12,941 bytes, nothing shortened
- [x] `TestPlan_CutsFallOnCharacterBoundaries` (`internal/judge/bounds_test.go`, table-driven): `中` (3 bytes) and `😀`
  (4 bytes) placed so each cap falls on every byte of a character — the hook command (6,000), a one-line `CLAUDE.md`
  (6,000), a one-line skill script (2,000 per file), a decoded payload (800), a static snippet (200, through triage) —
  give fields and unit texts that are valid UTF-8 and unchanged by a JSON round trip, and a quote of the text as the
  endpoint reads it grounds. **Red on the base** for every cap at a split offset
- [x] `TestClip_CutsOnACharacterBoundary` (`internal/detect`): `clip` never ends inside a character, and returns ASCII
  input exactly as before (`s[:200] + "…"`)
- [x] `TestTriageItems_EvidenceIsBounded` (`internal/judge/bounds_test.go`): a finding whose file position is 3,000 bytes
  (ASCII and CJK) gives evidence of at most 1,000 bytes, valid UTF-8, and a token that straddles the cut leaves no head
  behind (redacted first). **Red on the base**: 3,000+ bytes
- [x] `TestPlan_FieldsAreRedactionFixedPoints` (`internal/judge/bounds_test.go`): the two non-fixed-point fixtures above
  give fields that `detect.Redact` leaves unchanged, with units that hold the sent bytes. **Red on the base**
- [x] `FuzzPlanFields` (`internal/judge/bounds_test.go`): for an artifact set built from the fuzz input (a skill with its
  description, body, script, blobs and static findings; a `CLAUDE.md`; a hook; an MCP entry; a connector), every
  planned field `f` — declared, behavior, triage evidence — satisfies `detect.Redact(f) == f`, `utf8.ValidString(f)` and
  `len(f) <= ` its cap (1,000 / 6,000 / 1,000), and every unit's text is a substring of the field it was built for. Its
  seeds include every fixture above, so it is **red on the base**
- [x] **Reverse assertion, corpus level** (scratch replay, not committed): over the 3,539 corpus samples, every one of the
  8,770 planned payloads (and every artifact hash) is byte-identical between the base binary and this branch's
- [x] **Reverse assertion, unit level**: `TestExcerptVersion_IsPinnedWithItsGolden` stays green on digest
  `270922b0e0fc4223` through every code commit before the golden fixture is extended — an ASCII fixture under every cap
  is planned byte for byte as before — and every existing judge test passes; the only edits to existing tests are one
  call's arity (`egress_test.go`: `capabilityDigest` now also returns what it left out) and the golden fixture's extension
- [x] **Reverse assertion, static**: `scan --json` over the corpus and over `~/.claude` gives the same report on both
  binaries (`scanned_at` aside), except snippets whose 200-byte cut fell inside a character (counted, each listed)
- [x] `judge.PromptVersion()` unchanged (`aguard version` prints the same `judge-prompt=` on both binaries);
  `ExcerptVersion` is 3, pinned with the digest of the golden fixture extended to hit a multibyte cut, the triage bound
  and a flag before a file header
- [x] `.claude/rules/judge.md` carries the guard: a change under `internal/redact` changes excerpt bytes and bumps
  `ExcerptVersion` in the same commit; the file stays at or under 200 lines
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency

## Out of scope

- **No redaction pattern changes**: `internal/redact` has zero diff; `Secrets` and `Credentials` return what they did for
  every string. What changes is where the judge's builders call `Redact` and how they cut
- **The content hash does not change**: `internal/collect/hash.go`, `internal/detect/contenthash.go`, `internal/reputation`
  and `internal/gate` have zero diff; no approval or reputation entry is invalidated (`clip` is not on the hash path)
- **No pass, prompt or trigger changes**: `planFor` asks the same questions of the same artifacts; `PromptVersion` is
  unchanged; the per-file 2,000 and per-payload 800 caps, the 8-payload and 40-item limits and the MCP line cap stay
- **Report-side cuts are not touched**: the LLM-005 quote clip (`clipQuote`), the `clean` preview clip and SARIF's
  `clipMessage` bound text for a reader, not a request; they are named as a follow-up
- **The judge's reply handling, grounding rules, consensus and rendering do not change**
- `baselines/results/` does not change by a byte; no run is re-folded

## Must not claim

- Not "what the judge is sent is redacted completely": redaction stays best-effort (spec §16.3). The fixed point says a
  second pass finds nothing more, not that the first found everything
- Not that the whole request body is a fixed point of `Redact`: the guarantee is per field. The labels and the fence
  around fields are ours, and a field ending in `--token` can still make `Redact` read the next label as its value
  (measured: no corpus payload does)
- Not that the corpus result says anything about content outside it: it says this branch changes no byte of what the
  corpus sends, while the fixtures above show what it fixes
- Not that any of the four defects let a secret out on the measured inputs: the measured effects are an excerpt larger
  than documented, text grounding compares against that the endpoint never read, and a second redaction pass that
  still changes what was sent

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The failing tests: oversize fields, character-boundary cuts, triage bound, fixed points, the fuzz property, `clip` | `judge: tests — the judge's fields can exceed their caps, split a character, or not be a redaction fixed point (P-037)` |
| 2 | One rune-boundary cut (`detect.RunePrefix`, the loop `capBytes` had) for `clip`, `capBytes`, `capLine`, `declaredPurpose`, `capHeadTail`, `boundedRedact` and the decoded payloads; invalid input bytes become U+FFFD on the way in, as `encoding/json` would write them | `detect, judge: cut on a character boundary everywhere, and send invalid bytes as the endpoint reads them (P-037)` |
| 3 | Triage evidence capped at 1,000 bytes after redaction | `judge: bound each triage item's evidence at 1,000 bytes, redacted first (P-037)` |
| 4 | Decoded payloads and digest lines dropped whole from the end to fit 6,000 bytes, with their units, disclosed as `shortened` and in an `LLM-000` | `judge: fit the deobfuscation and collusion fields to the excerpt cap, and say what was left out (P-037)` |
| 5 | The last step of every field is a whole-field `Redact`, re-capped if that lengthened it; `ExcerptVersion` 3 with the extended golden | `judge: make every payload field a redaction fixed point, and bump ExcerptVersion to 3 (P-037)` |
| 6 | The `docs/llm-judge` pair, the `docs/architecture` pair, spec §16.3, the invariant #3 note and the `judge.md` guard | `docs: every judge field is bounded, valid UTF-8 and a redaction fixed point (P-037)` |
| 7 | The comments that document a call's `shortened` name the fitted fields too | `judge: name the fitted fields where a call's shortened is documented (P-037)` |

## Open questions

1. **Where does the one rune-boundary cut live?** `capBytes` (judge) has the loop, and so do `capLine` and
   `declaredPurpose`; `detect.clip` cannot reach judge (judge imports detect). **Recommendation**: move the loop down one
   package as `detect.RunePrefix(s, max)` — the longest prefix of at most `max` bytes that does not end inside a
   character — and make `capBytes`, `capLine`, `declaredPurpose`, `clip`, `capHeadTail`, `boundedRedact` and the decoded
   payloads call it; no second copy. It backs off at most three bytes, so a run of invalid bytes is not eaten whole.
   **Decided (2026-10-10)**: as recommended
2. **Bytes that are invalid in the input, not made invalid by a cut?** A Latin-1 script reaches the endpoint as U+FFFD
   too, and grounding compared against the raw byte. **Recommendation**: replace each such byte with U+FFFD exactly as
   `encoding/json` does (`sendable`), first thing in `egress.redact` — so caps measure what is sent — and again in the
   final step; a no-op for valid UTF-8. **Decided (2026-10-10)**: as recommended
3. **How is triage evidence cut?** **Recommendation**: the whole `file:line snippet` item, after redaction and the home
   scrub, to at most 1,000 bytes on a character boundary with `…` at the cut (the mark static snippets already use). A
   path long enough to fill the item leaves no room for the snippet; such a path is not an ordinary one, and the label is
   display-only. **Decided (2026-10-10)**: as recommended
4. **Is a digest line bounded on its own?** It is the same `file:line [RULE] snippet` shape, and with no per-line bound a
   single line could exceed the field, leaving nothing to send. **Recommendation**: the same 1,000-byte item bound as
   triage, then whole lines dropped from the end. **Decided (2026-10-10)**: as recommended
5. **One disclosure or two?** The MCP shortening has its own `LLM-000` whose last sentence is about configurations.
   **Recommendation**: leave that note's text byte for byte, and give decoded payloads and digest lines one more
   `LLM-000` in the same style, built from the same `task.shortened` the preview already shows.
   **Decided (2026-10-10)**: as recommended
6. **What if the final `Redact` lengthens a field past its cap?** **Recommendation**: re-apply that field's own cap — a
   prefix cut, the head/tail cut with its line map, or dropping the last unit (file, payload, digest line) — at a limit
   that tightens each round, so a cut that itself ends in a shape `Redact` rewrites cannot loop; for the MCP excerpt the
   re-cut is added to its `shortened`. **Decided (2026-10-10)**: as recommended
7. **How does a field assembled from several units stay grounded after a whole-field `Redact`?** **Recommendation**:
   `Redact` replaces within a line and never adds or removes one, so after the pass each unit's text is read back from
   the same lines of the field; the units then hold exactly the sent bytes, separators and headers included in no unit.
   **Decided (2026-10-10)**: as recommended
8. **`ExcerptVersion`?** **Recommendation**: 3, bumped once in W5 with the golden fixture extended to exercise the change;
   W2–W4 keep the existing golden green, which is the unit-level reverse assertion. **Decided (2026-10-10)**: as recommended
9. **SARIF fingerprints.** A fingerprint is (rule, file, snippet); a snippet whose 200-byte cut fell inside a character
   gets a different (valid) snippet, so that alert is re-keyed once. **Recommendation**: accept it — the old snippet held
   invalid UTF-8 — and count such snippets over the corpus and `~/.claude`. **Decided (2026-10-10)**: as recommended

## Done

```
Merged: PR #58 (2026-10-10; find the sha with git log --grep P-037 after the merge)
Released: pending release
Evidence: red on the base with the tests alone (commit "judge: tests — …"): TestPlan_ExplainAndDigestFitTheExcerpt 6,435 and
  11,934 bytes, nothing shortened, no LLM-000; TestPlan_CutsFallOnCharacterBoundaries 29 of 40 (surface, character, offset)
  rows — every one whose cut fell inside a character — send invalid UTF-8, and on 29 calls a quote of what the endpoint read
  does not ground; TestClip_CutsOnACharacterBoundary 6 of 8 offsets end inside a character; TestTriageItems_EvidenceIsBounded
  3,049 / 2,849 / 1,040 bytes; TestPlan_FieldsAreRedactionFixedPoints the header `# b.sh` and the `---` separator rewritten by
  a second Redact; FuzzPlanFields red on 3 of its 4 seeds. Green as the fix landed: W2 the boundary and clip tests, W3 triage,
  W4 deobfuscation and collusion, W5 the fixed points and the fuzz seeds
Evidence: the measured fixtures, planned again on the branch (scanEnv with the preview sink): deobfuscation 6,435 → 5,630 bytes,
  shortened "1 of 8 decoded payload(s) past the 6000-byte excerpt not sent"; collusion 12,941 → 5,943, "34 of 63 capability
  line(s) past the 6000-byte excerpt not sent"; the four invalid-UTF-8 fields valid and unchanged by a JSON round trip (hook
  6,000 → 5,999 bytes, CLAUDE.md 6,000 → 5,998, decoded payload 800 → 798, triage item ending on a whole character); the deep
  path's evidence 1,022 → 1,000; both non-fixed points fixed (the header goes as `<REDACTED> b.sh`, the separator as
  `<REDACTED>` — what a second pass made of them)
Evidence: reverse, corpus — 3,539 samples placed by baselines/adapter/aguard.Stage (a superset of the four committed judge runs'
  samples), `llm preview --json` on the base binary and the branch's: 8,770 payloads and 4,704 artifact hashes byte-identical
  (0 differing lines); before and after, 0 fields over a cap, 0 U+FFFD, 0 fields a second redact.Secrets changes, 0 payloads it
  changes. `scan --json` (or `check --json` for a sample Stage cannot place) on both binaries: 3,539 reports identical with
  scanned_at removed, and none holds a U+FFFD — no snippet in the corpus is cut inside a character, so no SARIF fingerprint
  there moves
Evidence: reverse, unit — TestExcerptVersion_IsPinnedWithItsGolden green on digest 270922b0e0fc4223 at W2, W3, W4 and with W5's
  code before the fixture was extended; the extended fixture digests 6f976e938511cce8 on the base, b5a7dc70f28922c2 at W4 and
  870e91b426266cd3 with W5, pinned as {3, 870e91b426266cd3}; every other existing judge, detect and cmd test green
Evidence: ~/.claude — `scan --root ~/.claude --json --inbox off` identical on both binaries run back to back (184 artifacts,
  overall 69, scanned_at aside); `scan --root ~/.claude --quiet` prints nothing and exits 0 on both
Evidence: `aguard version` — judge-prompt=e863a9dc881c on both binaries (PromptVersion unchanged), judge-excerpt=2 → 3
Evidence: fuzz — FuzzPlanFields 180 s (15,642 execs) during W5 and 300 s (25,782 execs) on the final code, no failure; FuzzSendable 20 s
  (484,334 execs), no failure
Evidence: not done — `git diff --stat origin/main -- internal/redact internal/collect internal/detect/contenthash.go
  internal/detect/contenthash_test.go internal/reputation internal/gate baselines go.mod go.sum docs/rules.md
  internal/judge/prompt.go internal/judge/prompt_version.go internal/judge/triage.go internal/judge/reply.go` is empty;
  internal/judge/ground.go differs by the ExcerptVersion line only
Verify: make verify → "verify: all gates passed" (go1.23.5, no toolchain switch; go.mod line 2 `go 1.23.5`)
```

Follow-ups left out on purpose:

- Report-side cuts that can still split a character: the `LLM-005` quote clip (`clipQuote`, 120 bytes), the `clean`
  restore preview's clip (100 bytes) and SARIF's `clipMessage` (300 bytes, at a space when there is one). They bound text
  for a reader, not a request.
- Two judge limits are still silent, as on the base: the intent pass leaves out files past its 6,000-byte budget, and the
  deobfuscation pass blobs past its eighth. Disclosing them adds a note to many large skills, so it is its own proposal.
- The request body as a whole is not a fixed point of `Redact` (a field ending in a flag makes it read the label after it
  as the flag's value); every field is. No corpus payload is affected.
