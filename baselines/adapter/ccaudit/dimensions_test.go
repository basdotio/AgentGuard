// SPDX-License-Identifier: MIT

package ccaudit

import (
	"slices"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// TestEveryCategoryTheToolEmitsHasADecision — the eight values below are the complete set cc-audit
// actually emitted across all 3,539 samples, counted from the raw JSON on 2026-09-23. A category
// missing from the map falls through to "" and silently scores as "did not name the kind", which
// is indistinguishable from a deliberate null. Both are legitimate answers; being unable to tell
// them apart is not.
func TestEveryCategoryTheToolEmitsHasADecision(t *testing.T) {
	// category -> how many findings carried it, corpus-wide. Kept so the next reader knows
	// which decisions rest on a lot of evidence and which rest on almost none.
	emitted := map[string]int{
		"supplychain": 5612, "persistence": 1671, "overpermission": 658, "obfuscation": 601,
		"exfiltration": 541, "privilegeescalation": 422, "promptinjection": 395, "secretleak": 136,
	}
	for cat := range emitted {
		if _, ok := DimensionMap[cat]; !ok {
			t.Errorf("cc-audit emits category %q and the map has no entry for it: an absent key "+
				"and a deliberate null both score as \"did not name the kind\", and nobody can "+
				"tell which one this was", cat)
		}
	}
	for cat := range DimensionMap {
		if _, ok := emitted[cat]; !ok {
			t.Errorf("the map carries %q, which the tool never emitted — either it was invented "+
				"or the tool changed; re-measure before trusting the rest", cat)
		}
	}
}

// TestTheMappingsAreTheOnesTheEvidenceChose guards the five that were measured, against the
// plausible-sounding alternatives the first draft reached for. Each was decided by lift over the
// base rate, not by the category's name — see DimensionMap's table.
func TestTheMappingsAreTheOnesTheEvidenceChose(t *testing.T) {
	for cat, want := range map[string]string{
		"persistence":     "backdoor",     // lift 4.33, the strongest signal measured
		"overpermission":  "permission",   // 3.42
		"promptinjection": "injection",    // 2.83
		"supplychain":     "supply-chain", // 2.38, beating execution by 0.10
		"exfiltration":    "exfiltration", // 1.96
	} {
		if got := DimensionMap[cat]; got != want {
			t.Errorf("DimensionMap[%q] = %q, want %q", cat, got, want)
		}
	}
	// The three the evidence declined to map. A null here is a RESULT — the corpus says so in
	// as many words — and quietly filling one in later would credit the tool for naming a kind
	// the measurement says it did not name.
	for _, cat := range []string{"obfuscation", "privilegeescalation", "secretleak"} {
		if got := DimensionMap[cat]; got != "" {
			t.Errorf("DimensionMap[%q] = %q; the measurement found no dimension for it, and "+
				"filling it in on the strength of the name is the error this table exists to "+
				"prevent", cat, got)
		}
	}
}

// TestOnlyFlaggingFindingsContributeADimension mirrors the aguard adapter: a dimension taken from
// a finding below the gate would credit the tool for naming a kind in a report the gate never
// showed anyone. The tier decides which severities flag, so it must decide this too.
func TestOnlyFlaggingFindingsContributeADimension(t *testing.T) {
	res := ScanResult{Finding: []Finding{
		{ID: "PE-005", Severity: "critical", Category: "persistence"},
		{ID: "OP-006", Severity: "medium", Category: "overpermission"},
		{ID: "SL-004", Severity: "low", Category: "secretleak"},
	}}

	got := dimensionsOf(res, TierDefault)
	if !slices.Equal(got, []string{"backdoor"}) {
		t.Errorf("default tier = %v, want [backdoor]: medium and low are below its gate", got)
	}

	// The reverse assertion: --strict opens the gate, so the medium finding must now count.
	// Without this, a map that returned nothing at all would pass the line above.
	got = dimensionsOf(res, TierStrict)
	if !slices.Equal(got, []string{"backdoor", "permission"}) {
		t.Errorf("strict tier = %v, want [backdoor permission]", got)
	}
}

// TestDimensionsAreDeduplicatedAndSorted — a verdict file that reordered between runs would make
// the byte-identical reverse assertion on the other tool impossible to state.
func TestDimensionsAreDeduplicatedAndSorted(t *testing.T) {
	res := ScanResult{Finding: []Finding{
		{Severity: "high", Category: "supplychain"},
		{Severity: "critical", Category: "persistence"},
		{Severity: "high", Category: "supplychain"},
		{Severity: "high", Category: "exfiltration"},
	}}
	got := dimensionsOf(res, TierDefault)
	if !slices.Equal(got, []string{"backdoor", "exfiltration", "supply-chain"}) {
		t.Errorf("dimensionsOf = %v, want sorted and deduplicated", got)
	}
}

// TestACategoryMappedToNothingProducesNoDimension — the case the whole null column rests on. A
// sample where cc-audit found something real but only named it "obfuscation" must score as
// caught-but-not-named, not as caught-and-named-obfuscation.
func TestACategoryMappedToNothingProducesNoDimension(t *testing.T) {
	res := ScanResult{Finding: []Finding{
		{ID: "OB-008", Severity: "critical", Category: "obfuscation"},
		{ID: "PE-002", Severity: "critical", Category: "privilegeescalation"},
	}}
	if got := dimensionsOf(res, TierDefault); got != nil {
		t.Errorf("dimensionsOf = %v, want nil", got)
	}

	// And it reaches the row that way, rather than being dropped somewhere in between.
	row := reconcile(
		ledger.Row{Sample: "s", Outcome: ledger.Scored, Verdict: "malicious"},
		Score{Total: 80, Present: true},
		ScanResult{Summary: Summary{Critical: 2}, Finding: res.Finding},
		TierDefault,
	)
	if len(row.Dimensions) != 0 {
		t.Errorf("row.Dimensions = %v on a sample whose only categories map to nothing", row.Dimensions)
	}
	if row.Verdict != "malicious" {
		t.Errorf("Verdict = %q; naming no kind must not change whether it was caught", row.Verdict)
	}
}
