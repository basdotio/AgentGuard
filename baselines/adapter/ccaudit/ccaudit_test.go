// SPDX-License-Identifier: MIT

package ccaudit

import (
	"context"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/baselines/adapter/sarif"
	"github.com/basdotio/agent-guard/baselines/corpus"
	"github.com/basdotio/agent-guard/baselines/ledger"
)

// TestScanAlwaysReturnsARow is the one rule the Adapter interface says is not negotiable, and
// the reason it has no error return. An adapter that could fail without producing a row would
// reintroduce the silent gap the ledger exists to remove — which is exactly how the old
// hack/corpus-runner lost 127 samples, by returning early before the scanner was ever invoked.
//
// Every case below is a way the run can go wrong BEFORE cc-audit says anything. None of them may
// produce a zero Row.
func TestScanAlwaysReturnsARow(t *testing.T) {
	tests := []struct {
		name string
		ad   *Adapter
		tree string
		want []string // substrings the detail must carry
	}{
		{
			name: "a binary that does not exist",
			ad:   &Adapter{Bin: "cc-audit-does-not-exist-p017"},
			tree: t.TempDir(),
			want: []string{"cc-audit-does-not-exist-p017"},
		},
		{
			// A tree that is not there must still produce a row rather than a panic or a
			// silent skip. The detail names the binary, not the tree, because the binary is
			// what actually blocked the run — a message that named the tree here would send
			// the reader to look for a corpus problem that does not exist.
			name: "a tree that does not exist still produces a row",
			ad:   &Adapter{Bin: "cc-audit-does-not-exist-p017"},
			tree: "/nonexistent/p017",
			want: []string{"could not execute", "cc-audit-does-not-exist-p017"},
		},
		{
			name: "the strict tier fails the same way, not a different way",
			ad:   &Adapter{Bin: "cc-audit-does-not-exist-p017", Tier: TierStrict},
			tree: t.TempDir(),
			want: []string{"could not execute"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.ad.Scan(context.Background(), corpus.Sample{Sample: "s"}, tt.tree)
			if row.Outcome != ledger.Errored {
				t.Errorf("Outcome = %q, want %q: a run that could not start is an error, "+
					"not a benign verdict", row.Outcome, ledger.Errored)
			}
			if row.Detail == "" {
				t.Error("Detail is empty; ledger.Check requires one on an Errored row")
			}
			if row.Verdict != "" {
				t.Errorf("Verdict = %q on an Errored row: a tool that never ran has no opinion", row.Verdict)
			}
			for _, want := range tt.want {
				if !strings.Contains(row.Detail, want) {
					t.Errorf("detail does not name %q:\n%s", want, row.Detail)
				}
			}
		})
	}
}

// TestTheTwoInvocationsMustAgree — this adapter reads the verdict from SARIF and the 0-100 risk score
// from JSON, which means two invocations per sample. When they disagree the honest answer is
// Errored carrying BOTH, not whichever one looks more plausible: a reconcile that silently
// preferred one would hide the only evidence that the two passes are not measuring the same
// thing.
func TestTheTwoInvocationsMustAgree(t *testing.T) {
	tests := []struct {
		name    string
		verdict string
		score   Score
		ok      bool
	}{
		{
			name:    "flagged with a score above zero",
			verdict: "malicious", score: Score{Total: 60, Level: "high", Present: true}, ok: true,
		},
		{
			name:    "clean with a score of zero",
			verdict: "benign", score: Score{Total: 0, Level: "safe", Present: true}, ok: true,
		},
		{
			// cc-audit's own bands put 1..25 at LOW and 26..50 at MEDIUM, neither of which the
			// default tier flags. So benign-with-a-score is ordinary, not a contradiction.
			name:    "clean with a low score is not a contradiction",
			verdict: "benign", score: Score{Total: 20, Level: "low", Present: true}, ok: true,
		},
		{
			// risk_score is Option with skip_serializing_if, so an absent key is not a zero
			// score. Treating it as one would manufacture a contradiction out of a field the
			// tool simply did not emit.
			name:    "flagged with no score at all is not a contradiction",
			verdict: "malicious", score: Score{}, ok: true,
		},
		{
			name:    "flagged but scored zero is a contradiction",
			verdict: "malicious", score: Score{Total: 0, Level: "safe", Present: true}, ok: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := ledger.Row{Sample: "s", Outcome: ledger.Scored, Verdict: tt.verdict}
			row := reconcile(in, tt.score, ScanResult{}, TierDefault)
			if tt.ok {
				if row.Outcome != ledger.Scored {
					t.Fatalf("Outcome = %q, want scored", row.Outcome)
				}
				if row.Verdict != tt.verdict {
					t.Errorf("Verdict = %q, want %q unchanged", row.Verdict, tt.verdict)
				}
				return
			}
			if row.Outcome != ledger.Errored {
				t.Fatalf("Outcome = %q, want %q", row.Outcome, ledger.Errored)
			}
			if row.Verdict != "" {
				t.Errorf("Verdict = %q survived on an Errored row", row.Verdict)
			}
			for _, want := range []string{"malicious", "0"} {
				if !strings.Contains(row.Detail, want) {
					t.Errorf("detail does not carry %q, so the disagreement cannot be argued with:\n%s",
						want, row.Detail)
				}
			}
		})
	}
}

// TestSeverityComesFromTheToolsOwnLadder — cc-audit's SARIF reporter maps BOTH Critical and High
// to `error` (src/reporter/sarif.rs severity_to_level), so the SARIF pass cannot tell them
// apart. Taking severity from SARIF would report every critical finding as a high one and
// understate the tool by a whole rung. taxonomy/tools.yaml is explicit that a tool is read on
// its own ladder, and this is that ladder.
func TestSeverityComesFromTheToolsOwnLadder(t *testing.T) {
	tests := []struct {
		name string
		sum  Summary
		want string
	}{
		{name: "critical outranks high", sum: Summary{Critical: 1, High: 3}, want: "critical"},
		{name: "high when there is no critical", sum: Summary{High: 2, Medium: 9}, want: "high"},
		{name: "medium", sum: Summary{Medium: 1, Low: 4}, want: "medium"},
		{name: "low", sum: Summary{Low: 1}, want: "low"},
		{name: "nothing reported leaves severity alone", sum: Summary{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := ledger.Row{Sample: "s", Outcome: ledger.Scored, Verdict: "malicious"}
			got := reconcile(in, Score{Total: 40, Present: true}, ScanResult{Summary: tt.sum}, TierDefault)
			if got.Severity != tt.want {
				t.Errorf("Severity = %q, want %q", got.Severity, tt.want)
			}
		})
	}
}

// TestReconcileLeavesUnscoredRowsAlone — a row the SARIF pass could not score has no verdict to
// contradict, and attaching a severity to it would make "the tool read nothing here" look like
// a finding.
func TestReconcileLeavesUnscoredRowsAlone(t *testing.T) {
	for _, outcome := range []ledger.Outcome{ledger.NoVerdict, ledger.Errored} {
		in := ledger.Row{Sample: "s", Outcome: outcome, Reason: ledger.NoLoadPath}
		got := reconcile(in, Score{Total: 90, Present: true}, ScanResult{Summary: Summary{Critical: 2}}, TierDefault)
		if got.Outcome != outcome {
			t.Errorf("Outcome = %q, want %q unchanged", got.Outcome, outcome)
		}
		if got.Severity != "" {
			t.Errorf("Severity = %q attached to a %q row", got.Severity, outcome)
		}
	}
}

// TestTheDefaultTierPassesNoFlags — the measured behaviour has to be the shipped behaviour, so
// the default tier adds nothing to the command line, and the threshold it folds at is `error`,
// which is where cc-audit's reporter puts Critical and High.
func TestTheDefaultTierPassesNoFlags(t *testing.T) {
	if args := TierDefault.Args(); args != nil {
		t.Errorf("the default tier added %v to the argv", args)
	}
	if got := TierDefault.Threshold(); got != sarif.LevelError {
		t.Errorf("default threshold = %q, want %q", got, sarif.LevelError)
	}
	if args := TierStrict.Args(); len(args) != 1 || args[0] != "--strict" {
		t.Errorf("strict tier argv = %v, want [--strict]", args)
	}
	if got := TierStrict.Threshold(); got != sarif.LevelNote {
		t.Errorf("strict threshold = %q, want %q", got, sarif.LevelNote)
	}
	// A severity word is not one of the two tiers, and accepting it would silently measure a
	// tier nobody chose.
	for _, in := range []string{"high", "critical", "error", ""} {
		if got := ParseTier(in); got != "" {
			t.Errorf("ParseTier(%q) = %q, want a refusal", in, got)
		}
	}
	for in, want := range map[string]Tier{"default": TierDefault, " STRICT ": TierStrict} {
		if got := ParseTier(in); got != want {
			t.Errorf("ParseTier(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestVersionIsReadNotAssumed — cc-audit ships roughly a release a day (146 of them, v3.23.9 on
// 2026-09-22). A figure attributed to the wrong version is worse than an unattributed one, so
// Version must fail loudly rather than return a guess when the binary cannot be run.
func TestVersionIsReadNotAssumed(t *testing.T) {
	ad := &Adapter{Bin: "cc-audit-does-not-exist-p017"}
	got, err := ad.Version(context.Background())
	if err == nil {
		t.Fatalf("Version returned %q with no binary to read it from", got)
	}
	if got != "" {
		t.Errorf("Version returned %q alongside an error; a guess with a version number on it "+
			"is the thing this method exists to prevent", got)
	}
}
