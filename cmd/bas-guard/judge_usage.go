// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"time"

	"github.com/basdotio/bas-guard/internal/judge"
)

// The two places a person reads what the judge used — the stderr line after a run and `bas-guard llm
// test` — print the endpoint's token counts with tokensInOut, so both say the same thing.
//
// The cached share (P-071) is the part of the prompt tokens the endpoint reported serving from its
// prompt cache, which the vendor bills at its own rate. It is named only when the endpoint reported
// some: for every endpoint that does not — every local one — both lines read exactly as before.

// tokensInOut is "N tokens in / M out", with "(K cached)" after the input count when K > 0. K is a
// subset of N, never in addition to it.
func tokensInOut(u judge.TokenUsage) string {
	if u.CachedPrompt > 0 {
		return fmt.Sprintf("%d tokens in (%d cached) / %d out", u.Prompt, u.CachedPrompt, u.Completion)
	}
	return fmt.Sprintf("%d tokens in / %d out", u.Prompt, u.Completion)
}

// judgeUsageLine is the stderr line after a judge run: the same numbers as the JSON summary, for
// the operator at the terminal; the human report deliberately carries no cost line.
func judgeUsageLine(stats judge.Stats, elapsed time.Duration, u judge.TokenUsage) string {
	return fmt.Sprintf("LLM judge: %d call(s) in %s (p50 %s, p95 %s) · %d retry · %d failed · %s\n",
		stats.Calls, elapsed.Round(time.Millisecond),
		stats.Percentile(0.5).Round(time.Millisecond), stats.Percentile(0.95).Round(time.Millisecond),
		stats.Retries, stats.Failed, tokensInOut(u))
}
