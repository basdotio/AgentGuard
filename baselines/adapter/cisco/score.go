// SPDX-License-Identifier: MIT

package cisco

import (
	"fmt"
	"strings"

	"github.com/basdotio/agent-guard/baselines/ledger"
)

// ScanResult is the subset of `--format json` this adapter reads (probed at 2.1.0, 2026-09-24).
// SARIF collapses CRITICAL and HIGH into `error`; the JSON keeps the tool's own ladder, and it is
// the only document that carries a per-finding `category` — which the corpus's attribution
// measurement needs. Hence two passes, as for cc-audit.
type ScanResult struct {
	IsSafe      bool      `json:"is_safe"`
	MaxSeverity string    `json:"max_severity"`
	Findings    []Finding `json:"findings"`
}

// Finding is one finding on the tool's own ladder (INFO/LOW/MEDIUM/HIGH/CRITICAL) with its
// ThreatCategory (17 values, models.py:39-59). Category is recorded for the lift analysis and
// is NOT mapped to a corpus dimension here: the map is measured
// after the first run, never written from the names.
type Finding struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Category string `json:"category"`
}

// reconcile merges the JSON pass into the row the SARIF pass produced. The verdict stays SARIF's.
//
// The one contradiction that cannot stand: SARIF folded to malicious at `error`, yet the JSON says
// `is_safe: true`. Both documents come from the same scan, so this would mean the two reporters
// disagree about the tool's own answer — recorded as Errored carrying both, never resolved by
// preferring one. A benign verdict with is_safe=false is NOT a contradiction: is_safe flips on any
// finding, including the INFO-level MANIFEST_MISSING_LICENSE that 19 of 20 probed skills carry.
func reconcile(row ledger.Row, res ScanResult) ledger.Row {
	if row.Outcome != ledger.Scored {
		return row
	}
	if row.Verdict == "malicious" && res.IsSafe {
		row.Outcome, row.Verdict, row.Severity, row.Rules = ledger.Errored, "", "", nil
		row.Detail = fmt.Sprintf("the two passes disagree: SARIF folded to malicious but the "+
			"JSON pass says is_safe=true with max_severity %q. Neither is discarded", res.MaxSeverity)
		return row
	}
	if s := strings.ToLower(strings.TrimSpace(res.MaxSeverity)); s != "" {
		row.Severity = s
	}
	row.Dimensions = dimensionsOf(res)
	return row
}
