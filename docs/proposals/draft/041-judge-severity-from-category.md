<!-- SPDX-License-Identifier: MIT -->
# 041 — A judge finding's severity is the model's one-word self-rating: a hidden directive the judge found can sit at medium and never reach the gate, while a hook that writes a file sits at high

- **Source**: new finding (2026-10-10), measured on the committed vote-level judge run
  `baselines/results/aguard/2026-09-29-llm-gpt-4.1-mini-s3-votes/` (gpt-4.1-mini, `samples: 3`, 224 samples) and the
  two `samples: 1` runs on the 819 subset (`2026-09-25-llm-glm-5.3-flash/`, `2026-09-28-llm-gpt-4.1-mini/`); follow-up to
  the judge comparison of 2026-09-25 (`docs/decisions/judge-comparison-2026-09-25.zh-CN.md`) and to P-031 (a report
  names the judge that produced its findings, so a severity change is a `prompt_version` change)
- **Depends on**: none (P-033 `judgefold` is what re-folds the measurements below; P-037 touches the same payload
  builders and should merge first so the two do not rebase over each other)
- **Branch**: `p/041-judge-severity-from-category`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The judge asks the model for `"severity": "low|medium|high"` and `clampSeverity` (`internal/judge/judge.go`) reads
that word back: `high`/`critical` → high, `low` → low, anything else → medium. Nothing in the prompt says what each word
means for the pass being asked, and nothing in the tool checks the word against what the model said it found. The one
exception is `LLM-007`, whose severity the tool fixes at high because "a manipulation attempt would happily rate itself
low" — that reasoning applies to every other pass as well, and was never extended to them.

The severity decides whether an escalated finding counts. In the tool, an escalated finding (grounded, majority,
rule not `LLM-009`) lowers `overall_effective` by its severity's penalty (medium 12, high 25) and trips
`--fail-on-llm <level>` only at or above that level; in the benchmark, the fold's predicate is "an escalated LLM finding
at or above `high`". So a verdict the model phrases as medium is, for the gate and for every published recall number,
the same as no verdict.

### Measured

Vote-level run (`2026-09-29-llm-gpt-4.1-mini-s3-votes`, read from its `judge.jsonl`; a majority is 2 of 3 votes; `LLM-009`
excluded everywhere because it is advisory-only by design):

**Malicious, random stratum (n=100), the 50 samples the static rules missed:**

| What the judge reached | Samples |
|---|---|
| Majority at high → escalated, counted | 26 |
| Majority, but every winning vote medium → not counted | **5** |
| Flagged by one vote of three only | 3 |
| Never flagged | 16 |

The five are `mal-hook-pretooluse-autoapprove` (`LLM-008`, medium ×3), `mal-skill-sg-clean-manifest-nested` (`LLM-001`,
medium ×3), `mal-skill-sg-definition-of-done-push` (`LLM-003`, medium, high, medium), `mal-skill-sg-missing-required-fields`
(`LLM-003`, medium ×3), `mal-skill-sg-overbroad-permissions` (`LLM-003`, medium ×3). Three of them are the injection pass
agreeing three times that it found a hidden directive — the thing `LLM-003` exists to find — and rating it medium.
The same shape in the `samples: 1` runs: of the 38 (glm-5.3-flash) / 49 (gpt-4.1-mini) static-missed reachable
malicious samples, 16 / 14 were flagged at medium or above by some rule and escalated by none.

**Majority verdicts by rule and severity, every class** (counts are questions, i.e. sample × rule):

| Class | `LLM-001` | `LLM-003` | `LLM-004` | `LLM-007` | `LLM-008` |
|---|---|---|---|---|---|
| malicious, random (100) | high 21 · medium 5 | high 44 · medium 5 | high 1 | high 4 | medium 2 |
| benign, random (100) | medium 1 | medium 1 | — | high 1 | **medium 18** |
| benign, targeted (5; the five a `samples: 1` run had flagged) | high 1 | high 2 | — | — | high 11 · medium 1 |
| hard negative (19) | — | — | — | — | medium 6 (2 samples) |

Two things follow. First, the word is not stable: in 24 of 213 majority groups the agreeing votes disagree on severity
(6 malicious, 16 benign, 2 hard negative), and in the earlier comparison the first vote's severity differed from the
majority's in 2 of 124 findings — the majority rule (P-035 of the former repository, rejected) cannot repair a scale the
prompt never defined. Second, the meaning of "medium" differs by pass: on `LLM-003` and `LLM-001` a medium majority is
malicious 10 times and benign 2 times in this run; on `LLM-008` it is malicious 2 times, benign 18 and hard negative 6.
A hook that kills processes or writes a file outside its event is what "medium" means there, and it is the single
largest source of judge noise on benign input once `LLM-009` is set aside.

Consequences:

- **A user with `--fail-on-llm high` is not protected by a verdict the judge reached three times out of three.** The
  report shows the finding, with `[3 of 3 samples agreed] [severities: medium, medium, medium]`, as advisory text under
  a score that did not move enough to matter; the pipeline is green.
- **The published recall of the judge is held down by its own scale.** 5 of the 50 static-missed malicious samples in
  this run (10%) are counted as misses while the judge's own answer, read by category, is a hit; the two `samples: 1`
  runs put the same bucket at 16 of 38 and 14 of 49.
- **Raising the bar or lowering it is the wrong axis.** Counting medium everywhere would add 18 benign hooks and 6 hard
  negatives for 5 malicious samples; keeping high everywhere keeps the 5 lost. The signal that separates the two is
  *which pass and what kind of behaviour*, and that is exactly what the tool does not ask the model to say in a form it
  can act on.

## Initial direction

Make the severity the tool's, derived from what the model says it found, the way `LLM-007` already is. Each verdict
pass gets a closed `category` field in its JSON schema (injection: `exfiltration | credential-read | remote-execution |
safety-disable | concealed-directive | other-directive`; intent: the same list plus `software-source |
file-deletion | privilege`; capability: `network | credential | install | file-write | process-control | other`) and a
`disclosed` boolean on the intent pass. A per-(pass, category) table in `internal/judge` maps the category to a
severity; the model's own `severity` word is kept in the reply and shown in the vote list, but no longer sets the
finding's severity. Categories the table does not know map to medium, never high, so a model that invents a category
cannot raise its own weight. Grounding, consensus, `advisoryOnly`, the score formula and `--fail-on` are untouched;
`prompt_version` moves because the system prompt and reply schema change (P-031), which is how a report from before
and after stays distinguishable.

Measured before merging, with the gates the judge comparison set: the 819 subset at `samples: 1` and the 224 subset at
`samples: 3` twice (stability), per source with n; the full 3,220 benign once (the denominator the +0.4% figure never
had); hard negatives +0. The table is tuned on the malicious set only and read off the benign set once.

Left for their own proposals, named here so they are not folded in: a focused second question for a static-clean
artifact whose only verdict is medium; the excerpt budget (6,000 / 2,000 bytes) that the large-file skillsgoat misses run
into; running the 127 server-source samples through `check --llm` (P-004) so they enter the judge's denominator.
