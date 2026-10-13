<!-- SPDX-License-Identifier: MIT -->
# 054 — Two readings the judge gets wrong: a tool signature with nested parentheses declares nothing, and a quote of the declared purpose passes as evidence of a behaviour

- **Source**: P-053's benign panel (the atproto example declared nothing) and the 2026-10-13 review of P-049's full-benign
  residue (a CTF-forensics skill flagged on its own description, "no behaviour found to confirm")
- **Depends on**: P-041, P-053
- **Branch**: `p/054-two-judge-readings`

## Problem

1. The server-source extractor (P-053) reads a tool as `@x.tool … def name(params) …: """docstring"""` and stops the
   parameter list at the first `)`. A FastMCP tool whose parameters carry `Annotated[str, Field(description="…")]` has
   parentheses inside the list, so none of such a server's tools is read; DECLARED is empty, the prompt reads every
   behaviour as undisclosed, and a login that reads its own credentials is a high. One of the four benign flags in
   P-053's panel was exactly this.
2. On the intent pass the declared purpose travels as a unit beside the behaviour units, so a verdict may quote it.
   A verdict that quotes only the declaration as evidence of an undisclosed behaviour contradicts itself: the CTF skill
   was flagged `credential-read` with its own description as the quote and the reason "purpose states recovery of
   credentials but no behaviour found to confirm; not disclosed". The server-source pass inherits the shape — its
   docstrings are its declaration.

## Initial direction

One level of nested parentheses in the signature pattern. On the intent and server passes, ground the quote against the
behaviour units first; a quote that lands only in the declaration is dropped like an unquotable one, and the `LLM-005`
note says which quote was the declared purpose. No prompt change (`judge-prompt` does not move).

## Done criteria

- [x] `TestServerDeclared_NestedParenthesesInSignature` (`internal/judge/server_test.go`): a tool with
  `Annotated[…, Field(description=…)]` parameters is read with its docstring. **Red on the base**: declared empty
- [x] `TestRun_DeclaredOnlyEvidenceIsDropped` (`internal/judge/severity_test.go`): on the intent pass a verdict quoting
  the description is dropped and the `LLM-005` note names the declared purpose; quoting a script line it is kept; the
  server pass the same with a docstring line against a code line. **Red on the base**: the declaration quote is kept
- [x] Reverse assertions green without a character changed: every grounding test (`ground_test.go`, padding,
  omission marker), every P-041 / P-049 / P-053 test, the `cmd/aguard` e2e; `judge-prompt` unchanged
- [x] Measured (the panel's draw band is wider than the effect; see Done): the 76-server benign panel at `samples: 3` against P-053's run (the atproto flag gone, no new flag); the
  224 subset at `samples: 3` against the same-prompt control (`s3-mini-224`: malicious 71, benign 0, hard negatives 1,
  targeted 2) within the noise band; offline on the recorded full-benign runs: the declared-only rule removes 1–2
  benign samples per run and no malicious one
- [x] `make verify` green; `internal/score`, `internal/gate`, `internal/detect`, `internal/collect` untouched

## Out of scope

- Any other quote rule: grounding stays exact-match per line (P-020, P-037); this only says which units count as behaviour
- Tools whose declaration the extractor still cannot read (decorators applied as functions, tools registered from
  other modules): a server with no readable declaration is still judged, with an empty declared side, as P-053 decided

## Must not claim

- Do not say the server pass's benign cost is solved: one of four was this; the other three are the model's readings
- Do not quote the 224 numbers as an improvement: the change can only remove a finding whose quote was the declaration,
  and the 224 subset's two runs are read for "nothing else moved"

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The two fixes and their tests | `judge: two readings fixed — a tool signature with nested parentheses is read, and a quote of the declared purpose is not evidence of a behaviour (P-054)` |
| 2 | Docs pair, judge rule, the two measurements recorded, this file, the index | `docs, baselines, proposals: P-054 (P-054)` |

## Open questions

1. **Drop the declaration-only verdict, or weigh it medium?** **Recommendation**: drop, as an unquotable claim — a
   finding needs evidence of a behaviour, and "the declaration says so" is none; the note keeps it visible.
   **Proceeded under the recommendation (2026-10-13)** on the maintainer's "1,2都做".

## Done

```
Merged: PR #74 (2026-10-13; find the sha with git log --grep P-054)
Released: not yet
Evidence: TestServerDeclared_NestedParenthesesInSignature, TestServerDeclared_ReadsRegisteredTools (extended: Tool(name=GitTools.STATUS, …) and a triple-quoted description), TestRun_DeclaredOnlyEvidenceIsDropped (internal/judge); the first and third red on the base; every grounding, P-041, P-049, P-053 test and the cmd/aguard e2e green without a character changed; judge-prompt unchanged
Evidence: offline on the recorded runs (no calls): the declared-only rule removes exactly one benign sample per full-benign run (ctf-forensics at samples 1, overnight-dev at samples 3 and on the 819 subset) and no malicious or hard-negative sample; on the two 224 runs of P-041 three malicious samples quoted the frontmatter beside another high and keep their flag
Evidence: 224 subset at samples 3 (…-p054-224-s3/compare.txt against …-p054-224-s3-control/, same prompt for these samples): random malicious 70 against 71, random benign 0 against 0, hard negatives 1 against 1 (socket-client-plain), targeted benign 1 against 2; weight decisions identical on 97 of 101 flagged questions (96.0%) — nothing moved beyond the draw
Evidence: 76-server benign panel at samples 3 (…-p054-benign-servers-s3/compare.txt, the final draw, binary b1e9fe3; two earlier draws in raw_archive): escalated 6, against P-053's 4, with the two earlier draws at 7 and 5 — on 76 samples the draw alone moves the count by three, so the gate "no new flag" cannot be read here and is recorded as not readable rather than met. What the fixes did is visible sample by sample: atproto_mcp now declares its four tools (was nothing) and is still flagged because the login's credential read is in no docstring, a defensible reading; fetch and git now declare their tools (fetch: "Fetches a URL from the internet…", STATUS: "Shows the working tree status") where both declared nothing; the declared-only rule fired once (an LLM-005 naming the declared purpose)
Evidence: Out of scope — git diff --stat main -- internal/score internal/gate internal/detect internal/collect: empty
Evidence: make verify: all gates passed on the branch; go test -count=1 green; go.mod line 2 unchanged
```
