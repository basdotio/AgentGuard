<!-- SPDX-License-Identifier: MIT -->
# 068 — A judge reply the reader refuses is asked once more, not counted as a question never asked

- **Source**: the maintainer, 2026-10-16 ("我们主打的是 llm 判官" → "1": the judge's reliability before its next question)
- **Depends on**: P-034 (the one repair the reader makes), P-033 (a sample is complete only when every call succeeded)
- **Branch**: `p/068-reask-unreadable-reply`

## Problem

On the 384 benign skill trees of P-065's control, 1.7% of judge calls failed with a parse error — `invalid escape
sequence \x in string`, `invalid character ']' after object key:value pair`, `'}' after object key` — the model having
written a quoted line with a raw backslash escape or closed a member wrongly. P-034 repairs exactly one of these shapes.
Every other one is a question that counted as never asked: an `LLM-000` note, the sample incomplete in the fold, and
in one run 48 of 384 trees (12%) short at least one question. For a tool whose offer is the judge, an unasked question
in one tree of eight is a defect, not a footnote.

## Initial direction

A reply the reader refuses is a `ReplyError`, distinct from a transport error. `execute` asks that question ONCE more:
the same request under a fresh nonce with `Request.Reask` set, which appends a fixed `reaskRule` to the system message
(one JSON object and nothing else; escape backslashes and quotes; no `\x`, no raw control characters). The reply's own
text never goes back. The repeat is another draw of a sampled question — it decides nothing the first call could not
have — so invariant #4 is untouched; a second unreadable reply fails the call as before and lands in `LLM-000`. The
repeats are counted (`Stats.Reasked`, `judge.reasked` in the JSON, the summary line) so the recovery is never silent.
`PromptVersion` moves (the re-ask shape is a call shape); `ExcerptVersion` does not.

Not this: a generic JSON "fixer" (P-034's reasoning — a misread must fail visibly, not decide), or the endpoint's
JSON mode (`response_format`), which local endpoints handle differently and which changes every request; the second
is a possible follow-up once this is measured.

## Done criteria

- [x] `TestParseVerdict_UnreadableRepliesAreReplyErrors`, `TestRun_ReasksAnUnreadableReplyOnce`,
  `TestRun_SecondUnreadableReplyFailsVisibly` (and: a transport error is not re-asked), `TestHTTP_ReaskCarriesTheRuleUnderAFreshNonce`
- [ ] Live at `samples: 3`, the P-067 binary against this one on the 384 benign trees (the highest failure rate seen): failed
  calls, incomplete trees, re-asks, and the judge totals unchanged within the run band; the 240 SkillHarm trees and the
  held-out ClawHub benign 216 for the same
- [ ] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

(pending)

## Done

```
(pending)
```
