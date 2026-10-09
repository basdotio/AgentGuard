// SPDX-License-Identifier: MIT

package aguard

import (
	"encoding/json"

	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// judgeUsage folds what the judge cost on one sample from the bytes aguard printed, or returns
// nil when they carry no judge summary.
//
// The committed judge runs folded this by hand, and inferred two of its numbers: triage_calls as
// "one per artifact with a static finding", and questions = (calls − triage) / samples from it.
// The binary counts triage calls and retries itself now, and the inference is exact only when no
// call was skipped: a max_calls budget cuts the tail of the plan, which is where each artifact's
// triage sits, and the inferred count then includes calls that were never made — measured, two
// skills at samples: 3 under max_calls: 13 report 1 triage call where the inference says 2. So
// what the tool reported wins, and the inference stays only for output that does not carry the
// counts, labelled as derived.
//
// Presence is read from the printed bytes, not from res: decoding into model.ScanResult turns a
// missing triage_calls into 0, which is also a legitimate count, and the whole decision here is
// whether the tool said anything at all.
func judgeUsage(out []byte, res model.ScanResult) *ledger.JudgeUsage {
	j := res.Judge
	if j == nil {
		return nil
	}
	var printed struct {
		Judge map[string]json.RawMessage `json:"judge"`
	}
	// These bytes decoded into res a moment ago. If reading them a second time failed anyway, no
	// key counts as present, which takes the derivation and labels the row so.
	if err := json.Unmarshal(out, &printed); err != nil {
		printed.Judge = nil
	}
	reported := func(key string, n int) *int {
		if _, ok := printed.Judge[key]; !ok {
			return nil
		}
		return &n
	}

	u := &ledger.JudgeUsage{
		Basis: ledger.UsageReported, Calls: j.Calls, Failed: j.Failed, Skipped: j.Skipped,
		TriageCalls:      j.TriageCalls,
		Retries:          reported("retries", j.Retries),
		PromptTokens:     reported("prompt_tokens", j.PromptTokens),
		CompletionTokens: reported("completion_tokens", j.CompletionTokens),
	}
	if reported("triage_calls", j.TriageCalls) == nil {
		u.Basis = ledger.UsageDerived
		u.TriageCalls = derivedTriage(res.Artifacts, j.Calls)
	}
	return u
}

// derivedTriage is the documented fallback. judge.Run plans one triage call for each artifact
// with a deterministic finding — its predicate is score.Deterministic's — so with nothing skipped
// that is how many it made. Two guards the hand fold never needed on the committed runs, and which
// change none of their rows: no call at all means no triage call, and triage cannot outnumber the
// calls.
func derivedTriage(arts []model.ArtifactReport, calls int) int {
	n := 0
	for _, a := range arts {
		for _, f := range a.Findings {
			if score.Deterministic(f) {
				n++
				break
			}
		}
	}
	return min(n, calls)
}
