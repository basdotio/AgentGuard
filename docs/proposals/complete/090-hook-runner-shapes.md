<!-- SPDX-License-Identifier: MIT -->
# 090 — Three hook shapes weigh medium on the capability pass: a package runner on a moving dist-tag, a formatter through a package runner, and a git pull

- **Source**: the maintainer, 2026-10-22 ("按你建议在": the rulings proposed after the benign-label review)
- **Depends on**: P-049 (`categoryShown`, where a hook category must be shown by the command), the benign-label review of 2026-10-22
- **Branch**: `p/090-hook-runner-shapes`

## Problem

Reading the benign samples the judge flagged high (2026-10-13, re-run 2026-10-21) left four hooks whose `LLM-008` high
rested on one of three shapes: `npx claude-flow@alpha` on every command (claude-flow's own init output), `npx oxfmt` and
`npx prettier --write` after an edit, and a SessionStart `git pull` of the author's own repository. The maintainer ruled
the first and the third medium — the supply-chain exposure is real, the intent is not hostile — and a formatter run
through a package runner medium at most. P-049 had pinned `git pull` and `npx claude-flow@alpha` as showing `network`;
those two test rows pinned the old answer and now pin the new one.

## Initial direction

On the capability pass, before the tokens of `install` and `network` are looked for, three shapes are stripped from the
command: a package runner (`npx`, `bunx`, `pnpm dlx`/`exec`, `yarn dlx`/`exec`) on a moving dist-tag (`@alpha`, `@beta`,
`@next`, `@latest`, `@canary`, `@rc`); a package runner running a well-known formatter or linter (prettier, oxfmt,
oxlint, eslint, biome, dprint, stylelint, markdownlint); and `git pull`/`git fetch`. Anything else in the command — a
URL, curl, an install, an unknown package run through npx, pinned or not — still shows the category. No prompt change;
`PromptVersion` and `ExcerptVersion` do not move. The corpus has no malicious hook that uses these shapes, so the
recall cost cannot be measured on it; the rows that must stay high are pinned in the test instead.

## Done criteria

- [x] `TestCategoryShown`: the three shapes are not shown (medium); an unknown package through npx, pinned or not, a
  `git pull` beside curl, and a formatter beside `pip install` still are
- [x] Live at `samples: 3`, main against the branch, the same day, every hook sample of the corpus
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-22, main (`80a670c`) against the branch (`79c380f`), corpus `073df281`, 196 hook
samples (188 benign, 5 malicious, 3 hard negatives); `results/aguard/2026-10-22-llm-gpt-4.1-mini-p090-{main,branch}-s3/`,
raw in `aguard-judge-raw-2026-10-p090.tar.gz` on v0.30.0.

| | main | branch |
|---|---:|---:|
| benign hooks with an escalated `LLM-008` at high | 8 | **1** |
| malicious hooks with an escalated `LLM-008` at high | 2 | 2 |
| benign hooks judged high by any rule (of 188) | 13 | 10 |
| malicious hooks judged high by any rule (of 5) | 4 | 4 |

The three hooks the review named (claude-flow@alpha, oxfmt, the SessionStart git pull) go to medium; prettier was medium
in both runs. The one benign hook still at high is `pnpm format` read as an install — a project script, not one of the
three shapes, and not flagged in the main run (the run band); left for its own question.

**Reading.** The ruling, as written: seven benign hook highs gone, no malicious one moved. Its cost is outside what the
corpus can show — a malicious hook would now be medium if it ran its payload as a formatter's name or behind a moving
dist-tag — and the rows that keep everything else high are in the test.

## Done

```
Merged: PR #116 (2026-10-22; find the sha with git log --grep P-090)
Evidence: results/aguard/2026-10-22-llm-gpt-4.1-mini-p090-{main,branch}-s3: benign LLM-008 high 8 → 1 of 188, malicious LLM-008 high 2 → 2 of 5
Evidence: raw aguard-judge-raw-2026-10-p090.tar.gz on v0.30.0 (sha256 a4380ed2…c786, 325,997 bytes); schema round-trip 78 files / 34,386 lines; make verify: all gates passed
```
