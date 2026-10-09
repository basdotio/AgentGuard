<!-- SPDX-License-Identifier: MIT -->
# 031 — A `--llm` report cannot say which judge produced its LLM findings: only `tool_version` names it, and it moves when the judge did not and stays put when the judge changed

- **Source**: maintainer decision (2026-10-10) to overturn the second of P-002's two judge decisions — "today a report
  identifies the judge's code only through `tool_version`" — while keeping the first (the judge stays outside
  `rules_version`)
- **Depends on**: P-002 (`rules_version`, merged); coordinates with P-027 (PR #47, not merged), which refactors the same
  `internal/judge` files
- **Branch**: `p/031-judge-versions-in-report`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Two `--llm` reports, or two committed judge benchmark runs under `baselines/results/aguard/*-llm-*`, differ in their LLM
findings. The first question — "was it the same judge?" — has only one field to answer it: `tool_version`. P-002 measured
that field for the rule table and found it wrong in both directions; it is wrong in the same ways for the judge:

- the version moves while the judge's code does not (a release that touches nothing in `internal/judge/`);
- the judge changes while the version looks the same (two `git describe` strings one suffix apart, with a rewrite of
  what is sent to the model between them);
- a plain `go build` says `dev`, which names nothing.

And `tool_version` names a whole commit, so even when it does change it cannot say *what* changed: the prompts, or the
way an artifact is cut into the excerpt the model reads, or only something that cannot change an answer at all.

P-002 wrote this limit down as a fact in six passages, the `docs/rules.md` header and two tests. The maintainer has now
decided that the report should name the judge instead.

## Initial direction

Two versions, owned by `internal/judge`: a **computed** `judge.PromptVersion()` over everything the client wraps around
the scanned content (system prompts, fence and section layout, response format, temperatures, request envelope), and a
**hand-bumped** `judge.ExcerptVersion` for how an artifact becomes the excerpt and how a quote is grounded against it,
guarded by a golden test. Both, plus the model and `samples`, go into the report's `judge` block; `aguard version` prints
the two versions after `rules=`. `rules_version` does not change.
