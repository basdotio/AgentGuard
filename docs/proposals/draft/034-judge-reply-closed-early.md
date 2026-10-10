<!-- SPDX-License-Identifier: MIT -->
# 034 — A judge reply whose object closes one member early fails the call: an answered check is reported as not run

- **Source**: new finding (2026-10-10), measured on a judge benchmark run; follow-up to P-001 (the judge block's counts) and P-033 (a sample is a complete judge measurement only when every call succeeded)
- **Depends on**: none
- **Branch**: `p/034-judge-reply-closed-early`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`parseVerdict` (`internal/judge/openai.go`) reads a model reply by slicing it from the first `{` to the last `}` and
handing that slice to `json.Unmarshal`. A reply in which the model closed the object one member early with a stray `}`
and then went on writing the last member has this shape:

    {"flagged": false, "severity": "low", "summary": "…", "evidence": "…"}, "barrier_evidence": ""}

Every member is there and the answer is unambiguous, but the slice is not one JSON value, so the call fails with
`parse judge verdict: invalid character ',' after top-level value`. It is not retried (a parse error is not a transport
error), it counts in the judge block's `failed`, and the report's `LLM-000` note says "those checks did not run".

Measured on 2026-10-10 against an OpenAI-compatible endpoint (`gpt-4.1-mini-2025-04-14`, temperature 0.8, the
`samples: 3` path) over corpus samples: 5 of 1,482 calls (0.34%) failed with exactly that error, and the one captured
reply had the shape above (its `evidence` was 2,944 characters copied verbatim from the data block).

Consequences:

- **A BYO user loses checks the model did answer.** The `LLM-000` note tells them the judge was partly blind on an
  artifact when it was not; with `--fail-on-llm`, a run that ran short exits 4 (P-026), so a formatting slip in one
  reply fails a pipeline.
- **A benchmark run loses whole samples.** A sample is a complete judge measurement only when all its calls succeeded
  (P-033). At 0.3–0.6% per call, 3–6% of skill samples (about 6 calls each) and most large hook samples (30–200 calls)
  are incomplete because of this shape alone.

A related hazard sits in the same function: when the continuation has no closing `}` at all
(`{"flagged": false, …}, "flagged": true`), the slice ends at the first object's `}` and today's code accepts the first
half alone. The second half can carry a different `flagged` or a `barrier_evidence`, and taking the first half silently
turns a judge answer into a different one.

## Initial direction

Decode the first complete JSON value from the first `{` with `encoding/json`'s `Decoder`; when what follows it begins
with `,`, repair only that shape — drop the one stray `}` and decode the joined text as a single object, accepted only
when nothing but whitespace or a closing fence follows and no member name repeats — and otherwise behave exactly as
today. Count repaired replies in the judge block (`repaired`) so the repair is never silent. Request bytes do not
change.
