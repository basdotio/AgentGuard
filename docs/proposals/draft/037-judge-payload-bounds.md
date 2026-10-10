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

## Initial direction

Drop whole decoded payloads and digest lines from the end until the field fits (with their grounding units), and
disclose it like the MCP excerpt's shortening (`LLM-000`). Cut on rune boundaries everywhere with the one helper that
already does it (`capBytes`), cap each triage item's evidence at 1,000 bytes after redaction, and make the last step of
building every field a whole-field `Redact` (re-capped if that lengthened it), pinned by a fuzz test: every built field
is a fixed point of `Redact`, valid UTF-8 and within its cap. Bumps `ExcerptVersion`.
