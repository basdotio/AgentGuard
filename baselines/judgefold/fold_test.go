// SPDX-License-Identifier: MIT

package judgefold

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
)

// The finding shapes below are the ones raw/ carries: a deterministic finding has a dimension and
// source static; a judge vote has source llm, a dimension, Escalates as the judge set it, and the
// "[k of n samples agreed]" the judge's tally appends when samples > 1.

func static(rule string, sev model.Severity, dim int) model.Finding {
	return model.Finding{RuleID: rule, Severity: sev, Dimension: dim, Source: model.SrcStatic}
}

func vote(rule string, sev model.Severity, dim, k, n int, esc bool, sevs ...string) model.Finding {
	why := "the model's reason"
	if n > 1 {
		why += " [" + itoa(k) + " of " + itoa(n) + " samples agreed]"
		if len(sevs) > 0 {
			why += " [severities: " + strings.Join(sevs, ", ") + "]"
		}
	}
	return model.Finding{RuleID: rule, Severity: sev, Dimension: dim, Source: model.SrcLLM, Escalates: esc, Why: why}
}

func note(rule string) model.Finding {
	return model.Finding{RuleID: rule, Severity: model.SevLow, Source: model.SrcLLM}
}

func itoa(n int) string { return strconv.Itoa(n) }

func judged(calls, failed, skipped, samples int) *model.JudgeSummary {
	return &model.JudgeSummary{Ran: true, Calls: calls, Failed: failed, Skipped: skipped, Samples: samples}
}

// scored is the ledger row the adapter would have rebuilt: the fold reads its outcome, its route
// (the secondary-surface flag `check` sets) and its judge_usage, never the findings.
func scored(triage int, flags ...ledger.Flag) ledger.Row {
	return ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign", Flags: flags,
		JudgeUsage: &ledger.JudgeUsage{Basis: ledger.UsageReported, TriageCalls: triage}}
}

var sample = corpus.Sample{Sample: "s", Path: "corpus/benign/skills/s", Class: "benign", Surface: []string{"skills"}}

func result(j *model.JudgeSummary, notes []model.Finding, arts ...model.ArtifactReport) model.ScanResult {
	return model.ScanResult{Artifacts: arts, Notes: notes, Judge: j}
}

func art(kind model.ArtifactKind, fs ...model.Finding) model.ArtifactReport {
	return model.ArtifactReport{Kind: kind, Name: string(kind), Findings: fs}
}

// TestFold_ThePredicateOfTheCommittedRuns pins each definition the committed files were measured
// to follow (see the proposal's "What the committed files mean"), one case per arm.
func TestFold_ThePredicateOfTheCommittedRuns(t *testing.T) {
	cases := []struct {
		name                    string
		res                     model.ScanResult
		static, judge, judgeAny bool
		escalated               []string
		verdict, severity       string
		dimensions              []string
	}{
		{"an escalated high flips the judge, not the static column",
			result(judged(7, 0, 0, 3), nil, art(model.KindSkill, static("SEC-001", model.SevMedium, 3), vote("LLM-003", model.SevHigh, 1, 3, 3, true))),
			false, true, true, []string{"LLM-003"}, "malicious", "high", []string{"injection"}},
		{"LLM-009 escalated by an old binary is listed and never counts",
			result(judged(3, 0, 0, 3), nil, art(model.KindMCP, vote("LLM-009", model.SevHigh, 5, 3, 3, true))),
			false, false, true, []string{"LLM-009"}, "benign", "", nil},
		{"1 of 3 is a vote, not an escalation",
			result(judged(6, 0, 0, 3), nil, art(model.KindSkill, vote("LLM-001", model.SevHigh, 10, 1, 3, false))),
			false, false, true, []string{}, "benign", "", nil},
		{"an escalated medium raises the severity and flags nothing",
			result(judged(6, 0, 0, 3), nil, art(model.KindHook, vote("LLM-008", model.SevMedium, 2, 3, 3, true))),
			false, false, true, []string{}, "benign", "medium", nil},
		{"a low vote is not judge_any",
			result(judged(6, 0, 0, 3), nil, art(model.KindSkill, vote("LLM-001", model.SevLow, 10, 3, 3, true))),
			false, false, false, []string{}, "benign", "low", nil},
		{"a static high is both columns, with its own dimension",
			result(judged(4, 0, 0, 3), nil, art(model.KindSkill, static("EXE-001", model.SevHigh, 4))),
			true, true, true, []string{}, "malicious", "high", []string{"execution"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Fold(sample, scored(1), tc.res, Options{Threshold: model.SevHigh})
			if err != nil {
				t.Fatal(err)
			}
			r := a.Row
			if r.Static != tc.static || r.Judge != tc.judge || r.JudgeAny != tc.judgeAny {
				t.Errorf("static/judge/judge_any = %v/%v/%v, want %v/%v/%v", r.Static, r.Judge, r.JudgeAny, tc.static, tc.judge, tc.judgeAny)
			}
			if !reflect.DeepEqual(r.EscalatedRules, tc.escalated) {
				t.Errorf("escalated_rules = %#v, want %#v", r.EscalatedRules, tc.escalated)
			}
			if a.Verdict == nil {
				t.Fatal("a scored row produced no verdict")
			}
			v := *a.Verdict
			if v.Verdict != tc.verdict || v.Severity != tc.severity || !reflect.DeepEqual(v.Dimensions, tc.dimensions) {
				t.Errorf("verdict = %+v, want %s / %q / %v", v, tc.verdict, tc.severity, tc.dimensions)
			}
		})
	}
}

// TestFold_VotesCarryTheirKindAndCount: every vote-carrying LLM finding becomes a vote, in report
// order, with the kind of the artifact it sits on — the field without which no per-(kind, rule)
// table can be built — and the counts the judge printed.
func TestFold_VotesCarryTheirKindAndCount(t *testing.T) {
	res := result(judged(10, 0, 0, 3), []model.Finding{note("COV-000"), note("LLM-002"), note("LLM-000")},
		art(model.KindHook, static("PERM-008", model.SevMedium, 2), vote("LLM-008", model.SevMedium, 2, 2, 3, true, "medium", "high")),
		art(model.KindMCP, vote("LLM-009", model.SevHigh, 5, 3, 3, false, "high", "high", "high")))
	a, err := Fold(sample, scored(1), res, Options{Threshold: model.SevHigh})
	if err != nil {
		t.Fatal(err)
	}
	want := []Vote{
		{Rule: "LLM-008", Kind: "hook", K: 2, N: 3, Severity: "medium", Escalates: true, Severities: []string{"medium", "high"}},
		{Rule: "LLM-009", Kind: "mcp", K: 3, N: 3, Severity: "high", Escalates: false, Severities: []string{"high", "high", "high"}},
	}
	if a.Row.Votes == nil || !reflect.DeepEqual(*a.Row.Votes, want) {
		t.Errorf("votes = %+v, want %+v", a.Row.Votes, want)
	}
	if a.Row.TriageCalls == nil || *a.Row.TriageCalls != 1 || a.Row.Questions == nil || *a.Row.Questions != 3 {
		t.Errorf("triage/questions = %v/%v, want 1 and (10-1)/3 = 3", a.Row.TriageCalls, a.Row.Questions)
	}
	if got := a.Row.LLMNotes.String(); got != "COV-000:1 LLM-002:1 LLM-000:1" {
		t.Errorf("llm_notes = %s, want first-appearance order", got)
	}
	if a.Row.Incomplete != "" {
		t.Errorf("a complete answer is marked incomplete: %q", a.Row.Incomplete)
	}
}

// TestFold_RefusesToGuessTheSampleCount: questions are (calls - triage) / samples. The summary
// carries samples since P-031; an older raw needs the operator's -judge-samples. Neither, or the
// two disagreeing, is an error rather than a number; and a vote without its "[k of n]" is 1 of 1
// only when n is 1.
func TestFold_RefusesToGuessTheSampleCount(t *testing.T) {
	old := result(judged(7, 0, 0, 0), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 3, 3, true)))
	if _, err := Fold(sample, scored(1), old, Options{Threshold: model.SevHigh}); err == nil {
		t.Error("no samples anywhere, and the fold produced a question count")
	}
	if a, err := Fold(sample, scored(1), old, Options{Threshold: model.SevHigh, Samples: 3}); err != nil || *a.Row.Questions != 2 {
		t.Errorf("-judge-samples 3 on a summary without samples: %v, %+v", err, a.Row.Questions)
	}
	if _, err := Fold(sample, scored(1), result(judged(7, 0, 0, 3), nil), Options{Threshold: model.SevHigh, Samples: 1}); err == nil {
		t.Error("the summary says 3 samples, the flag says 1, and the fold picked one")
	}
	unmarked := result(judged(6, 0, 0, 3), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 1, 1, true)))
	if _, err := Fold(sample, scored(0), unmarked, Options{Threshold: model.SevHigh}); err == nil {
		t.Error("a vote without its count at samples 3 was read as 1 of 1")
	}
	one := result(judged(2, 0, 0, 1), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 1, 1, true)))
	a, err := Fold(sample, scored(0), one, Options{Threshold: model.SevHigh})
	if err != nil {
		t.Fatal(err)
	}
	if v := (*a.Row.Votes)[0]; v.K != 1 || v.N != 1 {
		t.Errorf("samples 1: vote = %+v, want 1 of 1", v)
	}
}

// TestFold_IncompleteSaysWhy: a judge that failed, skipped or did not run did not answer; that is
// not a clean "nothing found". `check`-routed samples never get a judge, so they are complete.
func TestFold_IncompleteSaysWhy(t *testing.T) {
	cases := []struct {
		name string
		lrow ledger.Row
		res  model.ScanResult
		want string // substring of Incomplete; "" means complete
	}{
		{"failed calls", scored(0), result(judged(4, 2, 0, 3), nil), "failed on 2 call(s)"},
		{"skipped calls", scored(0), result(judged(4, 0, 3, 3), nil), "skipped 3"},
		{"scan route, no judge summary", scored(0), result(nil, nil), "did not run"},
		{"scan route, judge did not run", scored(0), result(&model.JudgeSummary{Reason: "not enabled"}, nil), "not enabled"},
		{"check route, no judge by design", scored(0, ledger.SecondarySurface), result(nil, nil), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Fold(sample, tc.lrow, tc.res, Options{Threshold: model.SevHigh})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == "" && a.Row.Incomplete != "":
				t.Errorf("marked incomplete: %q", a.Row.Incomplete)
			case tc.want != "" && !strings.Contains(a.Row.Incomplete, tc.want):
				t.Errorf("incomplete = %q, want it to say %q", a.Row.Incomplete, tc.want)
			}
		})
	}
}
