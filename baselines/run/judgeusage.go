// SPDX-License-Identifier: MIT

package run

import (
	"fmt"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// UsageMixed is a run whose judged samples do not all rest on the same basis. One run measures one
// binary, so it should not happen; it is named rather than assumed away, because a total over part
// of the samples printed as the run's total is the misreading JudgeUsage exists to prevent.
const UsageMixed = "mixed"

// JudgeUsage is what the tool's judge cost over a whole run, summed from the ledger rows, and the
// basis those rows rest on. It used to be typed into a hand-written run.yaml, with triage_calls
// inferred; now the inference is the labelled fallback for a binary that does not count them.
//
// A total is written only when every judged sample supplied its part. Retries and tokens that some
// samples did not report have no total — an absent line, never a sum over the rest — and the
// samples that made calls without reporting tokens are counted instead.
type JudgeUsage struct {
	Basis     string `yaml:"basis"`
	BasisNote string `yaml:"basis_note"`
	// ReportedSamples and DerivedSamples count the samples whose output carried a judge summary,
	// by basis. A sample with no summary (routed where the judge does not run) is in neither.
	ReportedSamples  int  `yaml:"reported_samples"`
	DerivedSamples   int  `yaml:"derived_samples"`
	Calls            int  `yaml:"calls"`
	Failed           int  `yaml:"failed"`
	Skipped          int  `yaml:"skipped"`
	TriageCalls      int  `yaml:"triage_calls"`
	Retries          *int `yaml:"retries,omitempty"`
	PromptTokens     *int `yaml:"prompt_tokens,omitempty"`
	CompletionTokens *int `yaml:"completion_tokens,omitempty"`
	TokensUnreported int  `yaml:"tokens_unreported_samples,omitempty"`
}

// SumJudgeUsage totals the ledger's judge usage, or returns nil when no row carries any — every
// static run — so run.yaml gains no block.
func SumJudgeUsage(rows []ledger.Row) *JudgeUsage {
	var t JudgeUsage
	retries, prompt, completion := 0, 0, 0
	retriesKnown := true
	for _, r := range rows {
		u := r.JudgeUsage
		if u == nil {
			continue
		}
		if u.Basis == ledger.UsageReported {
			t.ReportedSamples++
		} else {
			t.DerivedSamples++ // an unknown basis is not a report
		}
		t.Calls += u.Calls
		t.Failed += u.Failed
		t.Skipped += u.Skipped
		t.TriageCalls += u.TriageCalls
		if u.Retries == nil {
			retriesKnown = false
		} else {
			retries += *u.Retries
		}
		// A sample that made no call has no tokens to report, and the binary leaves a zero out.
		if u.PromptTokens == nil || u.CompletionTokens == nil {
			if u.Calls > 0 {
				t.TokensUnreported++
			}
			continue
		}
		prompt += *u.PromptTokens
		completion += *u.CompletionTokens
	}
	if t.ReportedSamples+t.DerivedSamples == 0 {
		return nil
	}
	if retriesKnown {
		t.Retries = &retries
	}
	if t.TokensUnreported == 0 {
		t.PromptTokens, t.CompletionTokens = &prompt, &completion
	}
	t.Basis, t.BasisNote = usageBasis(t.ReportedSamples, t.DerivedSamples)
	return &t
}

func usageBasis(reported, derived int) (string, string) {
	const derivation = "one per artifact with a deterministic finding, exact only when skipped is 0"
	switch {
	case derived == 0:
		return ledger.UsageReported, "summed from the tool's own counts: every sample with a judge " +
			"summary reported triage_calls and retries. Questions asked = (calls - triage_calls) / " +
			"the judge's samples setting, exact only when skipped is 0"
	case reported == 0:
		return ledger.UsageDerived, "derived: no sample's judge summary reported triage_calls (a " +
			"binary that predates those counts), so triage_calls is " + derivation +
			"; retries and tokens were not reported and have no total"
	}
	return UsageMixed, fmt.Sprintf("mixed: %d sample(s) reported triage_calls and %d did not; "+
		"those were derived as %s. Retries and tokens have a total only when every sample "+
		"reported them", reported, derived, derivation)
}
