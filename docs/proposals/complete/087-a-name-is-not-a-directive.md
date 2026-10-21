<!-- SPDX-License-Identifier: MIT -->
# 087 — A one-word quote is a name, not a directive

- **Source**: the maintainer, 2026-10-21 ("你先修1类": the judge false positives the benign-label review sheet named)
- **Depends on**: P-049 (`categoryShown`, the place a category must be shown by the text), the review of the 30 benign samples judged high on 2026-10-13
- **Branch**: `p/087-a-name-is-not-a-directive`

## Problem

Re-running main on the 30 benign samples the judge flagged high on 2026-10-13 left 21 still high. One is a hook whose
whole command is `claude-ignore`. The model read the name as "ignore previous instructions": a `concealed-directive` on
the injection pass and an attempt on the analyzer (`LLM-007`, whose severity the tool fixes at high), three votes
each, both quoting the one word. The quote grounds — a whole short document may be quoted, so a hook whose command is
shorter than the 16-character floor can be cited at all — but a word is not something anyone can be told to obey.

## Initial direction

On the injection and result passes, a category resting on a quote of one token (no whitespace once trimmed) is not
shown by the text: medium, and the finding says so. A barrier quote of one token raises no `LLM-007`; the uncounted
quote is said on the verdict beside it, or is an `LLM-005` when there is none. Sentences are untouched: the check is
about a single token, not about short text. No prompt change; `PromptVersion` and `ExcerptVersion` do not move.

## Done criteria

- [x] `TestBareName_IsNotADirective` (the hook: no `LLM-007`, `LLM-003` at medium with the reason, the lone-quote path
  noted in `LLM-005`), `TestBareName_ASentenceStillCounts` (a sentence keeps both)
- [x] Live at `samples: 3`, main against the branch, the same day: every hook sample of the corpus plus the 30 review samples
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-21, main (`540b641`, v0.30.0) against the branch, corpus `aceef06e`, 217
samples (every hook — 188 benign, 5 malicious, the hard negatives — and the 30 review samples);
`results/aguard/2026-10-21-llm-gpt-4.1-mini-p087-{main,branch}-s3/`, raw in `aguard-judge-raw-2026-10-p087.tar.gz` on v0.30.0.

| | n | main | branch |
|---|---:|---:|---:|
| benign, judge-on high | 209 | 32 | 30 |
| malicious, judge-on high | 5 | 4 | 5 |
| findings resting on a one-word quote, at high | 217 | 2 (one tree) | 0 |

The case: `ben-hook-li-zhixin-claude-ignore-settings` goes from `LLM-003` high and `LLM-007` high to one `LLM-003` at
medium that says the quote is a name. No other tree in the 217 had a high finding on a one-word quote; the other
twelve trees whose verdict changed between the two runs did so on rules the change does not touch (the run band).

**Reading.** Correct and narrow: one tree of 3,220 benign, where a name weighed two highs, one of them the severity the
tool reserves for content that argues with the analyzer. Nothing malicious rested on a single word.

## Done

```
Merged: PR #113 (2026-10-22; find the sha with git log --grep P-087)
Evidence: results/aguard/2026-10-21-llm-gpt-4.1-mini-p087-{main,branch}-s3: the claude-ignore hook LLM-003 high + LLM-007 high → LLM-003 medium; one-word high findings 2 → 0 of 217; malicious hooks 4 → 5 of 5
Evidence: raw aguard-judge-raw-2026-10-p087.tar.gz on v0.30.0 (sha256 5aedfcc7…5d1d, 360,571 bytes); schema round-trip 72 files / 33,286 lines; make verify: all gates passed
```
