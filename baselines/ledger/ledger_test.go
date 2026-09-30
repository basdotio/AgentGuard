// SPDX-License-Identifier: MIT

package ledger

import (
	"strings"
	"testing"
)

// work is a three-sample work list. The real one is 3539 lines of `corpus samples`; the
// invariant does not care how long it is, only that every line is accounted for.
func work() []string { return []string{"ben-a", "mal-b", "hn-c"} }

func scored(sample, verdict string) Row {
	return Row{Sample: sample, Outcome: Scored, Attempted: true, Verdict: verdict}
}

// TestCheckRejects is the executing half of the ledger's first criterion: a sample that produced
// nothing must say why, and must not be able to vanish. Each case is a way a ledger could look
// complete while having lost a sample or invented an excuse for one.
func TestCheckRejects(t *testing.T) {
	tests := []struct {
		name string
		rows []Row
		want string
	}{
		{
			// The defect the whole ledger exists to stop: today's runner emits no line at all
			// for a sample it cannot place, and the hole is only visible by subtraction.
			name: "a sample in the work list with no row at all",
			rows: []Row{scored("ben-a", "benign"), scored("mal-b", "malicious")},
			want: "hn-c",
		},
		{
			// Decision of 2026-09-21: the test-point set is exactly this corpus. A row for
			// something else means a point came from somewhere we did not agree to measure.
			name: "a row for a sample outside the work list",
			rows: []Row{scored("ben-a", "benign"), scored("mal-b", "malicious"),
				scored("hn-c", "benign"), scored("ben-invented", "benign")},
			want: "not in the work list",
		},
		{
			// Two rows for one sample means the counts can add up while a sample is missing.
			name: "the same sample twice",
			rows: []Row{scored("ben-a", "benign"), scored("ben-a", "malicious"),
				scored("mal-b", "malicious")},
			want: "twice",
		},
		{
			// `skipped` is the outcome this design deliberately does not have.
			name: "the outcome skipped does not exist",
			rows: []Row{{Sample: "ben-a", Outcome: Outcome("skipped")},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "skipped",
		},
		{
			// The point of no-verdict: it records that the scanner ran and produced nothing.
			// Without attempted, it is indistinguishable from never having run it.
			name: "no-verdict without attempted",
			rows: []Row{{Sample: "ben-a", Outcome: NoVerdict, Reason: NoLoadPath},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "attempted",
		},
		{
			name: "no-verdict with no reason",
			rows: []Row{{Sample: "ben-a", Outcome: NoVerdict, Attempted: true},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "reason",
		},
		{
			// The reason set is closed on purpose: a free-text reason becomes a place to put
			// "did not seem relevant", which is the silent gap wearing a label.
			name: "no-verdict with a reason outside the closed set",
			rows: []Row{{Sample: "ben-a", Outcome: NoVerdict, Attempted: true, Reason: Reason("looked-fine")},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "not a reason this ledger knows",
		},
		{
			name: "scored without a verdict word",
			rows: []Row{{Sample: "ben-a", Outcome: Scored, Attempted: true},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "verdict",
		},
		{
			// `corpus score` parses exactly two words and refuses anything else with a line
			// number. Catching it here names the sample instead.
			name: "scored with a verdict word the scorer cannot parse",
			rows: []Row{scored("ben-a", "suspicious"), scored("mal-b", "malicious"),
				scored("hn-c", "benign")},
			want: "suspicious",
		},
		{
			// A scored row cannot claim it was not attempted; that combination has no meaning.
			name: "scored without attempted",
			rows: []Row{{Sample: "ben-a", Outcome: Scored, Verdict: "benign"},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "attempted",
		},
		{
			// own-fixture and surface-undeclared are labels ON a measured row, not excuses for
			// not measuring. Hanging one on a no-verdict row reintroduces the skip.
			name: "a contamination flag on a row that produced nothing",
			rows: []Row{{Sample: "ben-a", Outcome: NoVerdict, Attempted: true,
				Reason: NoLoadPath, Flags: []Flag{OwnFixture}},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "only a scored row",
		},
		{
			name: "an unknown flag",
			rows: []Row{{Sample: "ben-a", Outcome: Scored, Attempted: true, Verdict: "benign",
				Flags: []Flag{Flag("probably-fine")}},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "not a flag this ledger knows",
		},
		{
			name: "an error row that claims a verdict",
			rows: []Row{{Sample: "ben-a", Outcome: Errored, Attempted: true, Verdict: "benign",
				Detail: "timeout"},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "verdict",
		},
		{
			// An error with no detail is a dead end for whoever has to reproduce it.
			name: "an error row with no detail",
			rows: []Row{{Sample: "ben-a", Outcome: Errored, Attempted: true},
				scored("mal-b", "malicious"), scored("hn-c", "benign")},
			want: "detail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := Check(tt.rows, work())
			if len(errs) == 0 {
				t.Fatalf("Check accepted a ledger it must reject; wanted an error mentioning %q", tt.want)
			}
			var joined []string
			for _, e := range errs {
				joined = append(joined, e.Error())
			}
			all := strings.Join(joined, "\n")
			if !strings.Contains(all, tt.want) {
				t.Errorf("error does not mention %q, so the reader cannot act on it:\n%s", tt.want, all)
			}
		})
	}
}

// TestCheckAcceptsAnExhaustiveLedger is the reverse assertion: the checks above must not be so
// strict that a correct ledger cannot exist. All three outcomes appear, with the flags and the
// reason in their legal positions.
func TestCheckAcceptsAnExhaustiveLedger(t *testing.T) {
	rows := []Row{
		{Sample: "ben-a", Outcome: Scored, Attempted: true, Verdict: "benign",
			Flags: []Flag{SurfaceUndeclared}},
		{Sample: "mal-b", Outcome: NoVerdict, Attempted: true, Reason: NoLoadPath,
			Detail: "server source (.py) has no load path this tool reads"},
		{Sample: "hn-c", Outcome: Errored, Attempted: true, Detail: "did not finish within 60s"},
	}
	if errs := Check(rows, work()); len(errs) != 0 {
		t.Fatalf("Check rejected a well-formed exhaustive ledger: %v", errs)
	}
}

// TestTallyAddsUp pins the arithmetic the criterion states: the three outcomes partition the
// work list, and there is no fourth bucket for the remainder to hide in.
func TestTallyAddsUp(t *testing.T) {
	rows := []Row{
		{Sample: "ben-a", Outcome: Scored, Attempted: true, Verdict: "benign"},
		{Sample: "mal-b", Outcome: NoVerdict, Attempted: true, Reason: NoLoadPath, Detail: "x"},
		{Sample: "hn-c", Outcome: Errored, Attempted: true, Detail: "y"},
	}
	got := Tally(rows)
	if got.Scored != 1 || got.NoVerdict != 1 || got.Errored != 1 {
		t.Fatalf("tally = %+v, want one of each", got)
	}
	if total := got.Total(); total != len(work()) {
		t.Errorf("Total() = %d, work list is %d — a partition that does not cover the work list "+
			"is the silent gap this ledger replaces", total, len(work()))
	}
}

// TestEveryReasonIsDocumented keeps the closed set from growing by accident: a new reason has to
// be added to KnownReasons, which is the list Check validates against and the list the README
// explains. A reason nobody explained is a free-text reason with extra steps.
func TestEveryReasonIsDocumented(t *testing.T) {
	for _, r := range KnownReasons() {
		if strings.TrimSpace(Explain(r)) == "" {
			t.Errorf("reason %q has no explanation; a ledger reader cannot tell what it claims", r)
		}
	}
	if Explain(Reason("invented")) != "" {
		t.Error("Explain invented an explanation for a reason that is not in the closed set")
	}
}
