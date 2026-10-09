// SPDX-License-Identifier: MIT

package run

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

func intp(n int) *int { return &n }

func judged(sample string, u ledger.JudgeUsage) ledger.Row {
	return ledger.Row{Sample: sample, Outcome: ledger.Scored, Attempted: true, Verdict: "benign", JudgeUsage: &u}
}

// TestSumJudgeUsage_ReportedRunSumsTheToolsOwnCounts: every judged sample reported its counts,
// so the totals are sums of what the tool said. A sample that made no call reports no tokens
// and that is not a gap; a sample with no judge summary (routed to `check`, or a static run) is
// not counted at all.
func TestSumJudgeUsage_ReportedRunSumsTheToolsOwnCounts(t *testing.T) {
	rows := []ledger.Row{
		judged("a", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 7, Failed: 1, TriageCalls: 1,
			Retries: intp(2), PromptTokens: intp(700), CompletionTokens: intp(40)}),
		judged("b", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 13, Skipped: 1, TriageCalls: 1,
			Retries: intp(0), PromptTokens: intp(1300), CompletionTokens: intp(90)}),
		judged("c", ledger.JudgeUsage{Basis: ledger.UsageReported, Retries: intp(0)}),
		{Sample: "d", Outcome: ledger.Scored, Attempted: true, Verdict: "malicious"},
	}
	got := SumJudgeUsage(rows)
	if got == nil {
		t.Fatal("no totals for a run whose samples carry judge usage")
	}
	want := &JudgeUsage{
		Basis: ledger.UsageReported, ReportedSamples: 3,
		Calls: 20, Failed: 1, Skipped: 1, TriageCalls: 2,
		Retries: intp(2), PromptTokens: intp(2000), CompletionTokens: intp(130),
	}
	note := got.BasisNote
	got.BasisNote = ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("totals:\n got %+v\nwant %+v", *got, *want)
	}
	if !strings.Contains(note, "tool's own") {
		t.Errorf("basis note does not say the counts are the tool's own: %q", note)
	}
}

// TestSumJudgeUsage_DerivedRunSaysSoAndTotalsNothingItCannot: a run from a binary that reported
// no triage count is folded the documented way, and run.yaml says so. Retries and tokens cannot
// be derived, so there is no total for them — an absent line, never a 0.
func TestSumJudgeUsage_DerivedRunSaysSoAndTotalsNothingItCannot(t *testing.T) {
	got := SumJudgeUsage([]ledger.Row{
		judged("a", ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 7, TriageCalls: 1}),
		judged("b", ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 4, Failed: 1, TriageCalls: 1}),
		judged("c", ledger.JudgeUsage{Basis: ledger.UsageDerived}),
	})
	want := &JudgeUsage{Basis: ledger.UsageDerived, DerivedSamples: 3, Calls: 11, Failed: 1,
		TriageCalls: 2, TokensUnreported: 2}
	note := got.BasisNote
	got.BasisNote = ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("totals:\n got %+v\nwant %+v", *got, *want)
	}
	for _, want := range []string{"derived", "one per artifact with a deterministic finding", "skipped"} {
		if !strings.Contains(note, want) {
			t.Errorf("basis note does not say %q: %q", want, note)
		}
	}
}

// TestSumJudgeUsage_MixedNeverTotalsWhatSomeDidNotReport: one run, one binary, so mixed should
// not happen — but a total over half the samples printed as the total is the exact misreading
// this exists to prevent, so it is refused rather than assumed away.
func TestSumJudgeUsage_MixedNeverTotalsWhatSomeDidNotReport(t *testing.T) {
	got := SumJudgeUsage([]ledger.Row{
		judged("a", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 7, TriageCalls: 1,
			Retries: intp(1), PromptTokens: intp(700), CompletionTokens: intp(40)}),
		judged("b", ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 4, TriageCalls: 1}),
	})
	if got.Basis != UsageMixed || got.ReportedSamples != 1 || got.DerivedSamples != 1 {
		t.Errorf("basis = %q (%d reported, %d derived), want mixed (1, 1)", got.Basis, got.ReportedSamples, got.DerivedSamples)
	}
	if got.Retries != nil || got.PromptTokens != nil || got.CompletionTokens != nil {
		t.Errorf("a mixed run totals retries/tokens over the samples that reported them: %+v", *got)
	}
	if got.Calls != 11 || got.TriageCalls != 2 {
		t.Errorf("calls = %d, triage_calls = %d; want 11 and 2", got.Calls, got.TriageCalls)
	}
}

// TestSumJudgeUsage_TokensMissingOnACalledSampleAreCounted: an endpoint that reports no usage
// leaves the binary's token fields absent. A sum over the samples that had them would read as
// the run's cost; it is withheld and the gap is counted instead.
func TestSumJudgeUsage_TokensMissingOnACalledSampleAreCounted(t *testing.T) {
	got := SumJudgeUsage([]ledger.Row{
		judged("a", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 7, TriageCalls: 1,
			Retries: intp(1), PromptTokens: intp(700), CompletionTokens: intp(40)}),
		judged("b", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 4, TriageCalls: 1, Retries: intp(0)}),
	})
	if got.PromptTokens != nil || got.CompletionTokens != nil || got.TokensUnreported != 1 {
		t.Errorf("tokens = %v / %v, unreported = %d; want no totals and 1 unreported sample",
			got.PromptTokens, got.CompletionTokens, got.TokensUnreported)
	}
	if got.Retries == nil || *got.Retries != 1 {
		t.Errorf("retries = %v, want 1: every sample reported it", got.Retries)
	}
}

// TestSumJudgeUsage_StaticRunHasNone is the reverse assertion: without a judge summary on any
// sample there is no block, so a static run's run.yaml and signature are what they were.
func TestSumJudgeUsage_StaticRunHasNone(t *testing.T) {
	rows := []ledger.Row{{Sample: "a", Outcome: ledger.Scored, Attempted: true, Verdict: "benign"}}
	if got := SumJudgeUsage(rows); got != nil {
		t.Fatalf("a run with no judge summary has totals: %+v", *got)
	}
	r := complete()
	b, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "judge_usage") {
		t.Errorf("a run without judge usage writes the key:\n%s", b)
	}
	r.ToolExtraArgs = []string{"--no-reputation"}
	if !strings.HasSuffix(strings.SplitN(r.Signature(), ". PROVISIONAL", 2)[0], "lives in raw/ and is not committed") {
		t.Errorf("the extra-args sentence changed for a run without judge usage: %q", r.Signature())
	}
}

// TestSignatureSaysWhereTheJudgeCostIs: the signature says what the extra flags added lives in
// raw/ and is not committed. Once the cost is summed into run.yaml that is no longer the whole
// truth, and the line printed above every figure must not say it is.
func TestSignatureSaysWhereTheJudgeCostIs(t *testing.T) {
	r := complete()
	r.ToolExtraArgs = []string{"--llm", "--config", "<judge config>"}
	r.JudgeUsage = SumJudgeUsage([]ledger.Row{judged("a", ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 1, Retries: intp(0)})})
	if !strings.Contains(r.Signature(), "judge_usage") {
		t.Errorf("signature does not say the judge's cost is in run.yaml: %q", r.Signature())
	}

	b, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out Run
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b)
	}
	if !reflect.DeepEqual(r.JudgeUsage, out.JudgeUsage) {
		t.Errorf("judge_usage did not survive the round trip:\n in: %+v\nout: %+v\n%s", *r.JudgeUsage, out.JudgeUsage, b)
	}
	for _, want := range []string{"judge_usage:", "basis: reported", "triage_calls:", "retries:"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("run.yaml has no %q line:\n%s", want, b)
		}
	}
}
