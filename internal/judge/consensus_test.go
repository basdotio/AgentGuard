// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// votingClient flags the first `flagFirst` samples of the intent question and answers "not
// flagged" thereafter — a stand-in for a model that is only sometimes sure.
func votingClient(flagFirst int32) (*scriptedClient, *atomic.Int32) {
	var seen atomic.Int32
	return &scriptedClient{answer: func(r Request) (Verdict, error) {
		if r.Mode != ModeIntent {
			return Verdict{}, nil
		}
		if seen.Add(1) <= flagFirst {
			return Verdict{Flagged: true, Severity: "medium", Summary: "undisclosed behavior",
				Evidence: quotableFrom(r)}, nil
		}
		return Verdict{Flagged: false}, nil
	}}, &seen
}

func intentFinding(a model.ArtifactReport) *model.Finding {
	for i := range a.Findings {
		if a.Findings[i].RuleID == "LLM-001" {
			return &a.Findings[i]
		}
	}
	return nil
}

// TestConsensus_MajorityDecidesWeight is the k-of-n contract: 1 of 3 does not carry weight,
// 2 of 3 does. Crucially the minority finding is still REPORTED — "the model wasn't consistent
// about it" is a different claim from "it isn't there", and the judge may only ever add.
func TestConsensus_MajorityDecidesWeight(t *testing.T) {
	cases := []struct {
		name          string
		flagged       int32
		wantReported  bool
		wantEscalates bool
	}{
		{"none agreed", 0, false, false},
		{"1 of 3 — reported, no weight", 1, true, false},
		{"2 of 3 — weight", 2, true, true},
		{"3 of 3 — weight", 3, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			arts := skillTree(t, 1)
			client, _ := votingClient(c.flagged)
			Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 1})

			f := intentFinding(arts[0])
			if (f != nil) != c.wantReported {
				t.Fatalf("reported = %v, want %v (findings: %+v)", f != nil, c.wantReported, arts[0].Findings)
			}
			if f == nil {
				return
			}
			if f.Escalates != c.wantEscalates {
				t.Errorf("escalates = %v, want %v", f.Escalates, c.wantEscalates)
			}
			if !strings.Contains(f.Why, "samples agreed") {
				t.Errorf("the vote should be visible to the reader, got %q", f.Why)
			}
			if !c.wantEscalates && !strings.Contains(f.Why, "carries no weight") {
				t.Errorf("a minority finding must say it carries no weight, got %q", f.Why)
			}
		})
	}
}

// severityClient flags the intent question once per entry of sevs, in call order, with that
// entry as the model's own severity; "" means that sample did not flag. With Concurrency 1 the
// call order is the sample order, so each case can say exactly which sample voted what.
func severityClient(sevs ...string) *scriptedClient {
	var seen atomic.Int32
	return &scriptedClient{answer: func(r Request) (Verdict, error) {
		if r.Mode != ModeIntent {
			return Verdict{}, nil
		}
		i := int(seen.Add(1)) - 1
		if i >= len(sevs) || sevs[i] == "" {
			return Verdict{Flagged: false}, nil
		}
		return Verdict{Flagged: true, Severity: sevs[i], Summary: "undisclosed behavior",
			Evidence: quotableFrom(r)}, nil
	}}
}

// TestConsensus_VoteSeveritiesAreShown: under sampling the reason lists every agreeing
// vote's severity in sample order, right after the vote count. It is the only record of how far
// apart the samples were — before it, everything after the first vote was dropped in tally — and
// it is what lets one rerun compare "the first vote's severity" with "the majority's" on the same
// raw output. A single call has no votes, so it shows neither bracket.
func TestConsensus_VoteSeveritiesAreShown(t *testing.T) {
	cases := []struct {
		name    string
		samples int
		sevs    []string
		want    string // must appear in Why; "" = neither bracket may appear
	}{
		{"3 of 3, in sample order", 3, []string{"high", "medium", "high"},
			"[3 of 3 samples agreed] [severities: high, medium, high]"},
		{"a sample that did not flag is not a vote", 3, []string{"medium", "", "high"},
			"[2 of 3 samples agreed] [severities: medium, high]"},
		{"below the bar, still shown", 3, []string{"", "low", ""},
			"[1 of 3 samples agreed] [severities: low]"},
		{"samples: 1 has no votes to show", 1, []string{"high"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			arts := skillTree(t, 1)
			Run(context.Background(), severityClient(c.sevs...), arts, Options{Samples: c.samples, Concurrency: 1})

			f := intentFinding(arts[0])
			if f == nil {
				t.Fatalf("no intent finding (findings: %+v)", arts[0].Findings)
			}
			if c.want == "" {
				if strings.Contains(f.Why, "samples agreed") || strings.Contains(f.Why, "severities:") {
					t.Errorf("a single call must not show a vote, got %q", f.Why)
				}
				return
			}
			if !strings.Contains(f.Why, c.want) {
				t.Errorf("reason = %q\nwant it to contain %q", f.Why, c.want)
			}
		})
	}
}

// TestConsensus_CostsWhatItSays: N samples means N calls. Sampling triples the bill, which is
// exactly why it is opt-in and why the budget counts every sample.
func TestConsensus_CostsWhatItSays(t *testing.T) {
	single := skillTree(t, 2)
	_, s1 := Run(context.Background(), &scriptedClient{}, single, Options{Samples: 1})
	triple := skillTree(t, 2)
	_, s3 := Run(context.Background(), &scriptedClient{}, triple, Options{Samples: 3})

	if s3.Calls != s1.Calls*3 {
		t.Errorf("samples=3 issued %d call(s), want 3x the %d of samples=1", s3.Calls, s1.Calls)
	}
	// One finding per question either way — sampling votes, it does not multiply findings.
	if len(triple[0].Findings) != len(single[0].Findings) {
		t.Errorf("sampling changed the finding count: %d vs %d",
			len(triple[0].Findings), len(single[0].Findings))
	}
}

// TestConsensus_TriageIsNotSampled: triage produces display labels, never a finding. There is
// nothing to take a vote on, so paying N times for it would be pure waste.
func TestConsensus_TriageIsNotSampled(t *testing.T) {
	arts := skillTree(t, 1)
	arts[0].Findings = append(arts[0].Findings, model.Finding{
		RuleID: "FS-002", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
		Evidence: []model.Evidence{{File: "run.sh", Line: 1, Snippet: "cat ~/.aws/credentials"}}})

	var triageCalls atomic.Int32
	client := &countingTriageClient{scriptedClient: scriptedClient{}, calls: &triageCalls}
	Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 1})

	if got := triageCalls.Load(); got != 1 {
		t.Errorf("triage ran %d time(s) under samples=3, want exactly 1", got)
	}
}

type countingTriageClient struct {
	scriptedClient
	calls *atomic.Int32
}

func (c *countingTriageClient) Triage(context.Context, string, []TriageItem) ([]model.AdvisoryLabel, error) {
	c.calls.Add(1)
	return nil, nil
}

// TestConsensus_SamplingRaisesTemperature: asking the same question N times at temperature 0
// returns the same answer N times — the votes would agree by construction and the consensus
// would measure nothing at all. Sampling is only meaningful with variance.
func TestConsensus_SamplingRaisesTemperature(t *testing.T) {
	arts := skillTree(t, 1)
	temps := map[float64]int{}
	client := &scriptedClient{answer: func(r Request) (Verdict, error) {
		temps[r.Temperature]++
		return Verdict{}, nil
	}}

	Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 1})
	if temps[0] > 0 {
		t.Errorf("sampled calls went out at temperature 0: %v", temps)
	}

	temps = map[float64]int{}
	Run(context.Background(), client, skillTree(t, 1), Options{Samples: 1, Concurrency: 1})
	if temps[0] == 0 {
		t.Errorf("a single call must stay at temperature 0 (as deterministic as we can ask for): %v", temps)
	}
}

// TestConsensus_FabricationCountsOnce: three fabricated samples of ONE question are one
// discarded finding, not three. Counting per sample would make a single hallucination look
// like an epidemic in the coverage note.
func TestConsensus_FabricationCountsOnce(t *testing.T) {
	arts := skillTree(t, 1)
	client := &scriptedClient{answer: func(r Request) (Verdict, error) {
		if r.Mode != ModeIntent {
			return Verdict{}, nil
		}
		return Verdict{Flagged: true, Severity: "high", Summary: "invented",
			Evidence: "rm -rf / --no-preserve-root --force-yes"}, nil
	}}

	notes, _ := Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 1})

	var why string
	for _, n := range notes {
		if n.RuleID == "LLM-005" {
			why = n.Why
		}
	}
	if why == "" {
		t.Fatalf("fabricated samples were not reported: %+v", notes)
	}
	if !strings.Contains(why, "1 flagged") {
		t.Errorf("three samples of one question should count as one discard, got %q", why)
	}
}

// TestConsensus_VarianceNeverReachesTheDeterministicScore is the regression that pays for
// sampling being allowed at all. Sampling deliberately introduces variance (temperature > 0),
// so two runs of the same tree can produce different LLM findings — and `overall` must still
// come out bit-identical, because --fail-on and any attestation are built on it.
func TestConsensus_VarianceNeverReachesTheDeterministicScore(t *testing.T) {
	// A client whose verdict flips with every call: maximum variance, worst case for the claim.
	var flip atomic.Int32
	client := &scriptedClient{answer: func(r Request) (Verdict, error) {
		if flip.Add(1)%2 == 0 {
			return Verdict{Flagged: false}, nil
		}
		return Verdict{Flagged: true, Severity: "high", Summary: "maybe", Evidence: quotableFrom(r)}, nil
	}}

	var overalls []int
	for run := 0; run < 4; run++ {
		arts := skillTree(t, 3)
		for i := range arts {
			arts[i].Findings = append(arts[i].Findings, model.Finding{
				RuleID: "FS-002", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
				Evidence: []model.Evidence{{File: "run.sh", Line: 1, Snippet: "cat ~/.aws/credentials"}}})
		}
		Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 4})

		r := model.ScanResult{Artifacts: arts}
		score.Apply(&r)
		overalls = append(overalls, r.Overall)
		if r.OverallEffective > r.Overall {
			t.Fatalf("run %d: effective %d > overall %d", run, r.OverallEffective, r.Overall)
		}
	}
	for i, o := range overalls {
		if o != overalls[0] {
			t.Fatalf("overall drifted across runs: %v (run %d = %d)", overalls, i, o)
		}
	}
}

// TestRun_StatsCountTriageApart: a judge question is asked `samples` times, triage once. With a
// single counter for both, what one question costs could only be inferred — and was, by hand,
// for the committed samples:3 judge runs (`questions = (calls - triage) / 3`, with triage itself
// guessed as "one per artifact with a static finding"). Stats keeps the two apart.
func TestRun_StatsCountTriageApart(t *testing.T) {
	withStatic := func() []model.ArtifactReport {
		arts := skillTree(t, 1)
		arts[0].Findings = append(arts[0].Findings, model.Finding{
			RuleID: "FS-002", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
			Evidence: []model.Evidence{{File: "run.sh", Line: 1, Snippet: "cat ~/.aws/credentials"}}})
		return arts
	}
	_, s1 := Run(context.Background(), &scriptedClient{}, withStatic(), Options{Samples: 1, Concurrency: 1})
	_, s3 := Run(context.Background(), &scriptedClient{}, withStatic(), Options{Samples: 3, Concurrency: 1})

	if s1.TriageCalls != 1 || s3.TriageCalls != 1 {
		t.Fatalf("triage calls = %d (samples=1), %d (samples=3); want 1 and 1", s1.TriageCalls, s3.TriageCalls)
	}
	questions := s1.Calls - s1.TriageCalls
	if questions < 1 {
		t.Fatalf("samples=1 issued %d call(s) with %d triage — no judge question was asked", s1.Calls, s1.TriageCalls)
	}
	if want := 3*questions + 1; s3.Calls != want {
		t.Errorf("samples=3 issued %d call(s), want 3 x %d question(s) + 1 triage = %d", s3.Calls, questions, want)
	}
}
