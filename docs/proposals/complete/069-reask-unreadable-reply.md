<!-- SPDX-License-Identifier: MIT -->
# 068 — A judge reply the reader refuses is asked once more, not counted as a question never asked

- **Source**: the maintainer, 2026-10-16 ("我们主打的是 llm 判官" → "1": the judge's reliability before its next question)
- **Depends on**: P-034 (the one repair the reader makes), P-033 (a sample is complete only when every call succeeded)
- **Branch**: `p/069-reask-unreadable-reply`

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
- [x] Live at `samples: 3`, the P-068 binary against this one on the 384 benign trees (the highest failure rate seen): failed
  calls, incomplete trees, re-asks, and the judge totals unchanged within the run band; the 240 SkillHarm trees and the
  held-out ClawHub benign 216 for the same
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-16, the P-068 binary (second wording, `judge-prompt ee01c8aa0cab`) against this
branch (`b851dfd5412b`), the same three lists the same day; three result directories under
`results/aguard/2026-10-16-llm-gpt-4.1-mini-p069-*`, raw in `aguard-judge-raw-2026-10-p068.tar.gz` on v0.26.0 (named before this proposal was renumbered
from 068 to 069, so the asset and the directories inside it carry the old number). The calls
per list are identical by construction (a re-ask is the same call asked again, like a retry); "trees short" is the
fold's incomplete count, a tree with at least one failed call.

| | calls | failed calls P-068 → P-069 | trees short P-068 → P-069 | re-asked (recovered) | judge / any P-068 → P-069 |
|---|---:|---:|---:|---:|---:|
| benign trees that name a companion | 4,790 | 58 (1.21%) → **4 (0.08%)** | 48 of 384 (12.5%) → **4 (1.0%)** | 65 (61) | 65 / 75 → 62 / 73 |
| SkillHarm | 2,594 | 11 (0.42%) → **1 (0.04%)** | 11 of 240 → **1** | 10 (10) | 175 / 197 → 178 / 196 |
| held-out ClawHub benign (totals) | 1,969 | 6 (0.30%) → **5 (0.25%)** | 3 of 216 → **1** | 5 (0) | 9 / 15 → 9 / 13 |

Prompt tokens on the benign list +1.6% (the repeats). What was failing, by the first error each tree reported under
P-068: a stray `]` or `}` or a `+`, `"`, `#`, `.`, `,` after a member (29 trees), an invalid escape — `\x` inside a quoted
line, `\u` followed by text (20 trees). Under P-069 every one of those shapes on the 384 benign trees was answered on the
repeat, including all 18 invalid-escape trees; what is left is one timeout, and three trees whose repeat was also
malformed (a `]`, a `+`, a `]`). The held-out tree that stays short is the other case: five of its nine calls produce an
invalid escape and all five repeats do too — content that makes this model write `\x` every time, which a format rule
does not reach. One tree in 840 across the three lists.

The judge totals moved 65 → 62, 175 → 178, 9 → 9: the run band (P-065 measured 11 lost / 6 gained on 148 untouched
trees), and the direction a recovered question could move them is up, not down, so the −3 on the benign list is the
draw, not the repeat.

**Reading.** A question that counted as never asked in one tree of eight on real skill trees now does so in one of a
hundred, for 1.6% more tokens and no change to what a verdict can be. The remaining shape — a tree whose content makes
the model write an invalid escape on every draw — is a reader's problem, not a prompt's: reading a backslash the JSON
grammar rejects as a literal backslash changes no boolean, no category and no severity, only a quoted string that still
has to ground. That is a repair of one shape like P-034's, measured here at one tree in 840, and it is its own proposal
if it is worth one.

## Done

```
Merged: PR #90 (2026-10-16; find the sha with git log --grep P-069)
Released: v0.28.0
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p069-benign-docs-s3: 4,790 calls, failed 58 → 4, trees short 48 → 4 of 384, re-asked 65, judge 65 → 62
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p069-skillharm-s3: failed 11 → 1 (a timeout), re-asked 10, judge 175 → 178; …-p069-heldout-asm-benign-s3: trees short 3 → 1 of 216, judge 9 → 9
Evidence: raw aguard-judge-raw-2026-10-p068.tar.gz on v0.26.0 (sha256 ca450655…4313, 327,858 bytes); schema round-trip 61 files / 30,524 lines; PromptVersion b851dfd5412b; ExcerptVersion 9; make verify: all gates passed
```
