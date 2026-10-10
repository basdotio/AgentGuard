// SPDX-License-Identifier: MIT

package judgefold

import (
	"reflect"
	"strings"
	"testing"
)

func answer(dir, incomplete string, rules ...string) Answer {
	votes := []Vote{}
	for _, r := range rules {
		votes = append(votes, Vote{Rule: r, Kind: "skill", K: 3, N: 3, Severity: "high", Escalates: true})
	}
	return Answer{Dir: dir, Row: Row{Sample: "s", Class: "benign", EscalatedRules: []string{}, Votes: &votes, Incomplete: incomplete}}
}

// TestSelect_FirstCompleteAnswerWins: shards, resumed runs and whole-sample retries are merged by
// one rule. A retry replaces the whole row: a vote from the failed attempt must not survive next to
// the retry's, because the two are different draws from the model.
func TestSelect_FirstCompleteAnswerWins(t *testing.T) {
	failed := answer("run", "the judge failed on 1 call(s)", "LLM-001")
	retry := answer("retry", "", "LLM-003")
	got, ok := Select([]Answer{failed, retry})
	if !ok {
		t.Fatal("two attempts and no answer")
	}
	if got.Dir != "retry" || !reflect.DeepEqual(got.Row, retry.Row) {
		t.Errorf("selected %s %+v, want the retry's row untouched", got.Dir, got.Row)
	}
	for _, v := range *got.Row.Votes {
		if v.Rule == "LLM-001" {
			t.Errorf("a vote of the failed attempt survived the retry: %+v", *got.Row.Votes)
		}
	}

	first := answer("run", "", "LLM-001")
	if got, _ := Select([]Answer{first, retry}); got.Dir != "run" {
		t.Errorf("with two complete answers %s won, want the first in argument order", got.Dir)
	}
}

// TestSelect_NoCompleteAnswerIsMarked: when every attempt is incomplete the first is kept, and the
// row says so — an incomplete answer must never read as a clean one.
func TestSelect_NoCompleteAnswerIsMarked(t *testing.T) {
	a := answer("run", "the judge failed on 1 call(s)")
	b := answer("retry", "the judge skipped 2 planned call(s)")
	got, ok := Select([]Answer{a, b})
	if !ok || got.Dir != "run" {
		t.Fatalf("selected %q (ok %v), want the first attempt", got.Dir, ok)
	}
	if !strings.Contains(got.Row.Incomplete, "failed on 1 call(s)") || !strings.Contains(got.Row.Incomplete, "2 attempts") {
		t.Errorf("incomplete = %q, want the first attempt's reason and the attempt count", got.Row.Incomplete)
	}
	if _, ok := Select(nil); ok {
		t.Error("no attempt at all produced an answer")
	}
}
