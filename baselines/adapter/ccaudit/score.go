// SPDX-License-Identifier: MIT

package ccaudit

import (
	"fmt"

	"github.com/basdotio/agent-guard/baselines/ledger"
)

// ScanResult is the subset of cc-audit's `--format json` document this adapter reads, named
// after the Rust struct it serialises (src/rules/types.rs, read at v3.23.9). Deliberately
// partial, for the same reason the SARIF types are.
type ScanResult struct {
	Version string    `json:"version"`
	Target  string    `json:"target"`
	Summary Summary   `json:"summary"`
	Risk    *Risk     `json:"risk_score"`
	Finding []Finding `json:"findings"`
}

// Summary is the per-severity tally on cc-audit's OWN ladder — the one thing the SARIF pass
// cannot recover, because its reporter maps Critical and High to the same `error`.
type Summary struct {
	Critical int  `json:"critical"`
	High     int  `json:"high"`
	Medium   int  `json:"medium"`
	Low      int  `json:"low"`
	Errors   int  `json:"errors"`
	Warnings int  `json:"warnings"`
	Passed   bool `json:"passed"`
}

// Finding is one finding. The verdict comes from SARIF, so what is read here is what SARIF
// cannot carry: cc-audit's own severity word, and its own category — the thing the corpus's
// attribution measurement asks for ("having found a problem, did it say what KIND?").
type Finding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Category string `json:"category"`
}

// Risk is the 0-100 deterministic score. This is the field this adapter exists to put next to ours:
// integer weights 40/20/10/5 capped at 100 (src/scoring.rs), the same shape as aguard's and the
// opposite polarity — higher is more dangerous.
//
// It is a POINTER because cc-audit declares it `Option<RiskScore>` with skip_serializing_if, so
// the key is absent rather than zero when there is no score. Reading an absent score as 0 would
// record "perfectly safe" for a sample the tool never scored.
type Risk struct {
	Total int    `json:"total"`
	Level string `json:"level"`
}

// Score is a risk score that knows whether it exists. Present is what separates "scored 0" from
// "did not score".
type Score struct {
	Total   int
	Level   string
	Present bool
}

// ScoreOf reads the score out of a parsed JSON document.
func ScoreOf(r ScanResult) Score {
	if r.Risk == nil {
		return Score{}
	}
	return Score{Total: r.Risk.Total, Level: r.Risk.Level, Present: true}
}

// topSeverity is the worst severity cc-audit itself reported, on its own four-word ladder.
// Empty means it reported none.
//
// taxonomy/tools.yaml requires a tool to be read on its own ladder — "two tools with different
// ladders can annotate the same sample without either adopting the other's". SARIF's `error`
// covers both Critical and High here, so taking severity from SARIF would report every critical
// finding as a high one and understate the tool by a whole rung.
func topSeverity(s Summary) string {
	switch {
	case s.Critical > 0:
		return "critical"
	case s.High > 0:
		return "high"
	case s.Medium > 0:
		return "medium"
	case s.Low > 0:
		return "low"
	}
	return ""
}

// reconcile merges the JSON pass into the row the SARIF pass produced.
//
// The verdict is SARIF's and stays SARIF's; JSON contributes the severity and the score. When
// the two passes contradict each other the row becomes Errored carrying BOTH, rather than
// whichever looks more plausible: a reconcile that silently preferred one would hide the only
// evidence that the two invocations are not measuring the same thing, and two invocations per
// sample is precisely the risk taken on to get the score alongside the verdict.
//
// A benign verdict with a non-zero score is NOT a contradiction: cc-audit's own bands put 1..25
// at LOW and 26..50 at MEDIUM, neither of which the default tier flags. Only the reverse —
// flagged, yet scored zero — cannot both be true.
func reconcile(row ledger.Row, sc Score, res ScanResult, tier Tier) ledger.Row {
	if row.Outcome != ledger.Scored {
		return row
	}
	sum := res.Summary
	if row.Verdict == "malicious" && sc.Present && sc.Total == 0 {
		row.Outcome = ledger.Errored
		row.Verdict = ""
		row.Severity = ""
		row.Rules = nil
		row.Detail = fmt.Sprintf("the two passes disagree: the SARIF pass folded to %q but the "+
			"JSON pass scored %d/100 (%s). Neither is discarded here, because a rule that "+
			"emits a finding the scorer does not weigh is a fact about the tool, not noise",
			"malicious", sc.Total, sc.Level)
		return row
	}
	if sev := topSeverity(sum); sev != "" {
		row.Severity = sev
	}
	row.Dimensions = dimensionsOf(res, tier)
	return row
}
