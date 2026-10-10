// SPDX-License-Identifier: MIT

package judgefold

import (
	"math"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/internal/model"
)

// TestWilson_MatchesTheCorpus: the interval is the corpus's (harness/internal/score/wilson.go),
// checked on the textbook case its own test uses, so a figure here and one in a scorecard are the
// same arithmetic.
func TestWilson_MatchesTheCorpus(t *testing.T) {
	p, lo, hi := Wilson(9, 10)
	if p != 0.9 || math.Abs(lo-0.596) > 0.001 || math.Abs(hi-0.982) > 0.001 {
		t.Errorf("Wilson(9,10) = %v [%v, %v], want 0.9 [0.596, 0.982]", p, lo, hi)
	}
	if p, lo, hi := Wilson(0, 0); p != 0 || lo != 0 || hi != 0 {
		t.Errorf("Wilson(0,0) = %v [%v, %v], want the all-zero sentinel", p, lo, hi)
	}
}

// TestCell_FollowsTheFigureRule: a rate is printed only when its Wilson half-width is at most 15
// points (the corpus's FigureThresholdPoints); otherwise the count alone. For an all-zero result
// that is n >= 22 — but 17 of 35 is a count too (±16 pts), which "n < 22" would have printed as a rate.
func TestCell_FollowsTheFigureRule(t *testing.T) {
	cases := []struct {
		k, n int
		rate bool
	}{
		{0, 22, true}, {0, 21, false}, {3, 12, false}, {17, 35, false}, {20, 40, true}, {3, 2480, true}, {0, 0, false},
	}
	for _, tc := range cases {
		got := Cell(tc.k, tc.n)
		if !strings.HasPrefix(got, itoa(tc.k)+"/"+itoa(tc.n)) {
			t.Errorf("Cell(%d,%d) = %q, want it to start with the fraction", tc.k, tc.n, got)
		}
		if strings.Contains(got, "%") != tc.rate {
			t.Errorf("Cell(%d,%d) = %q, rate printed = %v, want %v", tc.k, tc.n, got, !tc.rate, tc.rate)
		}
	}
	if got := Cell(3, 2480); got != "3/2480 0.1% [0.0, 0.4]" {
		t.Errorf("Cell(3,2480) = %q", got)
	}
}

func foldFor(t *testing.T, id, class string, lrowTriage int, res model.ScanResult) Answer {
	t.Helper()
	s := corpus.Sample{Sample: id, Path: "corpus/" + class + "/skills/" + id, Class: class, Surface: []string{"skills"}}
	l := scored(lrowTriage)
	l.Sample = id
	a, err := Fold(s, l, res, Options{Threshold: model.SevHigh})
	if err != nil {
		t.Fatal(err)
	}
	a.Source = "src-" + class
	return a
}

// TestTable_CountsWhatTheJudgeFlagged: FP needs an escalated vote at or above high; FP_any any
// vote at or above high; recall is the same numerator over malicious samples; hard negatives have
// their own line; an incomplete sample is in no denominator and is counted in the header, which
// also says that the kinds are aguard's and not the corpus's surfaces.
func TestTable_CountsWhatTheJudgeFlagged(t *testing.T) {
	esc := func() model.ScanResult {
		return result(judged(7, 0, 0, 3), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 3, 3, true)))
	}
	answers := []Answer{
		foldFor(t, "b1", "benign", 1, esc()),
		foldFor(t, "b2", "benign", 1, result(judged(7, 0, 0, 3), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 1, 3, false)))),
		foldFor(t, "b3", "benign", 1, result(judged(7, 2, 0, 3), nil, art(model.KindSkill, vote("LLM-003", model.SevHigh, 1, 3, 3, true)))),
		foldFor(t, "m1", "malicious", 1, esc()),
		foldFor(t, "h1", "hard-negative", 1, esc()),
	}
	out := Table(answers, Meta{Inputs: []string{"run"}, WorkList: 5, Threshold: model.SevHigh})

	for _, want := range []string{"1 incomplete", "aguard kind `connector`", "aguard kind `mcp`"} {
		if !strings.Contains(out, want) {
			t.Errorf("table does not say %q:\n%s", want, out)
		}
	}
	row := lineWith(out, "skill", "LLM-003")
	for _, cell := range []string{"1/2", "2/2", "1/1"} {
		if !strings.Contains(row, cell) {
			t.Errorf("skill × LLM-003 row lacks %s (FP 1/2, FP_any 2/2, recall 1/1):\n%s", cell, row)
		}
	}
	i := strings.Index(out, "hard negatives")
	if i < 0 {
		t.Fatalf("no hard-negative section:\n%s", out)
	}
	if hn := lineWith(out[i:], "skill", "LLM-003"); !strings.Contains(hn, "1/1") {
		t.Errorf("hard-negative line for skill × LLM-003 = %q, want 1/1", hn)
	}
}

func lineWith(text string, parts ...string) string {
	for _, l := range strings.Split(text, "\n") {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(l, p)
		}
		if ok {
			return l
		}
	}
	return ""
}
