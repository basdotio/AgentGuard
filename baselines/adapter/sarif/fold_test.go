// SPDX-License-Identifier: MIT

package sarif

import (
	"testing"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// TestEmptySarifRunIsNotACleanBill is this adapter's headline criterion, and the trap the
// aguard adapter hit, in the shape SARIF gives it: a run that read nothing and a run that read the file and
// found nothing produce near-identical documents. Folding the first to "benign" scores a
// product boundary as a correct answer — and on a malicious sample it scores it as a miss the
// tool never had the chance to make.
func TestEmptySarifRunIsNotACleanBill(t *testing.T) {
	tests := []struct {
		name    string
		log     Log
		outcome ledger.Outcome
		reason  ledger.Reason
		verdict string
	}{
		{
			name:    "no results and no artifacts is a tool that read nothing",
			log:     Log{Runs: []Run{{}}},
			outcome: ledger.NoVerdict, reason: ledger.NoLoadPath,
		},
		{
			name: "no results but an artifact it looked at IS a clean bill",
			log: Log{Runs: []Run{{
				Artifacts: []Artifact{{Location: Location{URI: "SKILL.md"}}},
			}}},
			outcome: ledger.Scored, verdict: "benign",
		},
		{
			// Findings are themselves proof it read something, so the artifact list must not
			// be consulted when there are results.
			name: "a finding with no artifact list is still a verdict",
			log: Log{Runs: []Run{{
				Results: []Result{{RuleID: "EX-001", Level: "error"}},
			}}},
			outcome: ledger.Scored, verdict: "malicious",
		},
		{
			name:    "no runs at all is a tool that read nothing",
			log:     Log{},
			outcome: ledger.NoVerdict, reason: ledger.NoLoadPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fold(ledger.Row{Sample: "s"}, tt.log, LevelWarning, ArtifactsAreEvidence)
			if !got.Attempted {
				t.Error("Attempted is false: a reason may not be written for a scanner that was not invoked")
			}
			if got.Outcome != tt.outcome {
				t.Errorf("Outcome = %q, want %q", got.Outcome, tt.outcome)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if got.Verdict != tt.verdict {
				t.Errorf("Verdict = %q, want %q", got.Verdict, tt.verdict)
			}
		})
	}
}

// TestAToolThatEmitsNoArtifactsStillGetsAVerdict locks in what cc-audit v3.23.9 actually does,
// measured 2026-09-23. Its SARIF carries results and rule metadata but NO artifacts array —
// not even on a scan that produced three findings. Under ArtifactsAreEvidence every one of the
// corpus's 3,220 benign samples folds to no-verdict and the false-positive rate comes out
// undefined instead of measured; that is a whole column of the comparison destroyed by one
// field a tool is not obliged to populate.
//
// The honest cost is asserted too: under ArtifactsNotEmitted, an empty document folds to benign,
// because for such a tool nothing in the output tells "read it and found nothing" from "never
// read it". cc-audit reports passed:true and 0/100 "safe" for a literally empty directory. The
// surface tripwire is the backstop, not this function.
func TestAToolThatEmitsNoArtifactsStillGetsAVerdict(t *testing.T) {
	// The exact shape cc-audit emitted for a clean benign sample.
	clean := Log{Version: "2.1.0", Runs: []Run{{
		Tool: Tool{Driver: Driver{Name: "cc-audit", Version: "3.23.9", Rules: []Rule{}}},
	}}}

	got := Fold(ledger.Row{Sample: "s"}, clean, LevelError, ArtifactsNotEmitted)
	if got.Outcome != ledger.Scored || got.Verdict != "benign" {
		t.Errorf("Outcome=%q Verdict=%q, want scored/benign — a tool that never emits artifacts "+
			"would otherwise report every clean sample as no-verdict", got.Outcome, got.Verdict)
	}

	// The same document under the other policy is the opposite answer, which is the whole
	// reason the policy is a parameter rather than a constant.
	got = Fold(ledger.Row{Sample: "s"}, clean, LevelError, ArtifactsAreEvidence)
	if got.Outcome != ledger.NoVerdict || got.Reason != ledger.NoLoadPath {
		t.Errorf("Outcome=%q Reason=%q, want no-verdict/no-load-path", got.Outcome, got.Reason)
	}

	// And a finding still folds the same way under either policy: the policy must only govern
	// the empty case, never what a result means.
	withFinding := Log{Runs: []Run{{Results: []Result{{RuleID: "PE-005", Level: "error"}}}}}
	for _, policy := range []ArtifactPolicy{ArtifactsAreEvidence, ArtifactsNotEmitted} {
		if got := Fold(ledger.Row{Sample: "s"}, withFinding, LevelError, policy); got.Verdict != "malicious" {
			t.Errorf("policy %v changed what a finding means: %q", policy, got.Verdict)
		}
	}
}

// TestAMissingLevelIsNotTheBottomOfTheLadder implements SARIF §3.27.10: a result with no `level`
// takes the rule's defaultConfiguration.level, and failing that, `warning`. Reading the missing
// field as `none` would silently discard every finding from a tool that relies on the default —
// which is the ordinary way to write SARIF, and would show up only as an implausibly good
// false-positive rate.
func TestAMissingLevelIsNotTheBottomOfTheLadder(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		res   Result
		want  Level
	}{
		{
			name:  "the result's own level wins",
			rules: []Rule{{ID: "EX-001", DefaultConfiguration: Configuration{Level: "note"}}},
			res:   Result{RuleID: "EX-001", Level: "error"},
			want:  LevelError,
		},
		{
			name:  "no level falls back to the rule's default",
			rules: []Rule{{ID: "EX-001", DefaultConfiguration: Configuration{Level: "error"}}},
			res:   Result{RuleID: "EX-001"},
			want:  LevelError,
		},
		{
			name:  "no level and no rule falls back to warning, not none",
			rules: nil,
			res:   Result{RuleID: "EX-001"},
			want:  LevelWarning,
		},
		{
			name:  "a rule with an empty default still falls back to warning",
			rules: []Rule{{ID: "EX-001"}},
			res:   Result{RuleID: "EX-001"},
			want:  LevelWarning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := Log{Runs: []Run{{
				Tool:    Tool{Driver: Driver{Rules: tt.rules}},
				Results: []Result{tt.res},
			}}}
			got := Fold(ledger.Row{Sample: "s"}, log, LevelError, ArtifactsAreEvidence)
			if got.Severity != string(tt.want) {
				t.Errorf("Severity = %q, want %q (SARIF §3.27.10 fallback chain)", got.Severity, tt.want)
			}
		})
	}
}

// TestAPassedCheckIsNotAFinding implements SARIF §3.27.9: `kind` defaults to `fail`, and `level`
// has meaning only for failures. A `pass` result carrying level `error` is a check that passed.
// Counting it manufactures findings out of a tool's own clean report — which on 3,220 benign
// samples would invent a false-positive rate that nobody's tool actually has.
func TestAPassedCheckIsNotAFinding(t *testing.T) {
	for _, kind := range []string{"pass", "notApplicable", "informational", "open", "review"} {
		t.Run(kind, func(t *testing.T) {
			log := Log{Runs: []Run{{
				Artifacts: []Artifact{{Location: Location{URI: "SKILL.md"}}},
				Results:   []Result{{RuleID: "EX-001", Level: "error", Kind: kind}},
			}}}
			got := Fold(ledger.Row{Sample: "s"}, log, LevelError, ArtifactsAreEvidence)
			if got.Verdict != "benign" {
				t.Errorf("kind=%q level=error folded to %q; §3.27.9 gives level meaning only for failures",
					kind, got.Verdict)
			}
			if len(got.Rules) != 0 {
				t.Errorf("kind=%q attributed rules %v to a result that did not fail", kind, got.Rules)
			}
		})
	}
	// The reverse assertion for the loop above: the kinds that DO mean failure must still flag,
	// or this test would pass just as well against a fold that never flags anything.
	for _, kind := range []string{"", "fail"} {
		log := Log{Runs: []Run{{Results: []Result{{RuleID: "EX-001", Level: "error", Kind: kind}}}}}
		if got := Fold(ledger.Row{Sample: "s"}, log, LevelError, ArtifactsAreEvidence); got.Verdict != "malicious" {
			t.Errorf("kind=%q level=error folded to %q, want malicious", kind, got.Verdict)
		}
	}
}

// TestNoneNeverFlags — SARIF §3.27.10 gives `none` the meaning "the concept of severity does not
// apply to this result". It is informational, not a mild violation, so a threshold of `none` is
// nonsensical rather than maximally strict. Letting it flag would turn every informational line
// in a tool's output into a detection.
func TestNoneNeverFlags(t *testing.T) {
	log := Log{Runs: []Run{{Results: []Result{{RuleID: "INFO-001", Level: "none"}}}}}
	for _, threshold := range []Level{LevelNone, LevelNote, LevelWarning, LevelError} {
		if got := Fold(ledger.Row{Sample: "s"}, log, threshold, ArtifactsAreEvidence); got.Verdict != "benign" {
			t.Errorf("threshold %q: a `none` result folded to %q", threshold, got.Verdict)
		}
	}
}

// TestSeverityIsReportedEvenBelowThreshold — the highest level seen is recorded whether or not it
// flagged, so a second pass at another threshold can be read off the verdict file without
// rerunning the scanner over 3,539 samples.
func TestSeverityIsReportedEvenBelowThreshold(t *testing.T) {
	log := Log{Runs: []Run{{Results: []Result{
		{RuleID: "A", Level: "note"},
		{RuleID: "B", Level: "warning"},
	}}}}
	got := Fold(ledger.Row{Sample: "s"}, log, LevelError, ArtifactsAreEvidence)
	if got.Verdict != "benign" {
		t.Errorf("Verdict = %q, want benign at threshold error", got.Verdict)
	}
	if got.Severity != string(LevelWarning) {
		t.Errorf("Severity = %q, want the highest level seen (warning)", got.Severity)
	}
}

// TestAnUnknownLevelStaysAtTheBottom — a value from a later SARIF version must degrade to
// "unranked" rather than flag everything or fail to parse.
func TestAnUnknownLevelStaysAtTheBottom(t *testing.T) {
	if got := ParseLevel("catastrophic").Rank(); got != 0 {
		t.Errorf("Rank of an unknown level = %d, want 0", got)
	}
	for in, want := range map[string]Level{
		"ERROR": LevelError, " warning ": LevelWarning, "Note": LevelNote, "none": LevelNone,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsRunFailureLeavesTheGateAlone — exit 1 is the gate firing, which is the EXPECTED outcome
// on a malicious sample, not a malfunction. Treating it as a failure turns every malicious
// sample into an Errored row and the measured recall into zero. That cost an afternoon on the
// aguard adapter; it is asserted here so it costs nobody a second one.
func TestIsRunFailureLeavesTheGateAlone(t *testing.T) {
	for _, code := range []int{0, 1} {
		if IsRunFailure(code) {
			t.Errorf("exit %d read as a run failure; 0 is clean and 1 is the gate firing", code)
		}
	}
	for _, code := range []int{2, 101, 127, -1} {
		if !IsRunFailure(code) {
			t.Errorf("exit %d not read as a run failure", code)
		}
	}
}
