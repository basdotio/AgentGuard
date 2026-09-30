// SPDX-License-Identifier: MIT

package aguard

import (
	"reflect"
	"testing"

	"github.com/basdotio/agent-guard/baselines/ledger"
	"github.com/basdotio/agent-guard/internal/model"
)

// Moved here with the fold logic from hack/corpus-runner. It pins the one thing that
// makes the measured gate the shipped gate: the verdict predicate is score.Deterministic plus
// the severity rank, the same one --fail-on uses. LLM findings and dimension-0 notes never flag
// (invariants #4 and #5), so a judge run cannot inflate a recall figure.

func finding(id string, dim int, sev model.Severity, src model.Source) model.Finding {
	return model.Finding{RuleID: id, Dimension: dim, Severity: sev, Source: src}
}

// The one-word verdict must be the gate's own decision: score.Deterministic plus the severity
// rank — the predicate `--fail-on` uses. Anything else measures a gate that does not ship.
func TestVerdictFollowsTheGatePredicate(t *testing.T) {
	cases := []struct {
		name      string
		findings  []model.Finding
		threshold model.Severity
		want      ledger.Row
	}{
		{
			name:      "a deterministic high at threshold high is malicious and names its dimension",
			findings:  []model.Finding{finding("EXEC-001", 4, model.SevHigh, model.SrcStatic)},
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "malicious", Severity: "high", Dimensions: []string{"execution"}},
		},
		{
			name:      "the same finding at threshold critical is benign, severity still reported",
			findings:  []model.Finding{finding("EXEC-001", 4, model.SevHigh, model.SrcStatic)},
			threshold: model.SevCritical,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign", Severity: "high"},
		},
		{
			name:      "an LLM high never flags — the gate never runs the judge",
			findings:  []model.Finding{finding("LLM-001", 1, model.SevHigh, model.SrcLLM)},
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign"},
		},
		{
			name:      "a dimension-0 note never flags — it describes the scan, not the artifact",
			findings:  []model.Finding{finding("COV-000", 0, model.SevHigh, model.SrcStatic)},
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign"},
		},
		{
			name:      "permission-sourced findings are deterministic and count",
			findings:  []model.Finding{finding("PERM-001", 2, model.SevCritical, model.SrcPermission)},
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "malicious", Severity: "critical", Dimensions: []string{"permission"}},
		},
		{
			name: "dimensions come only from findings that carried the flag, mapped through tools.yaml; obfuscation is unmapped",
			findings: []model.Finding{
				finding("OBF-001", 6, model.SevHigh, model.SrcStatic),
				finding("EXFIL-001", 3, model.SevHigh, model.SrcStatic),
				finding("INJ-001", 1, model.SevMedium, model.SrcStatic),
			},
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "malicious", Severity: "high", Dimensions: []string{"exfiltration"}},
		},
		{
			name:      "no findings is benign with no severity",
			threshold: model.SevHigh,
			want:      ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := model.ScanResult{Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "a", Findings: tc.findings}}}
			got := fill(ledger.Row{Sample: "s", Attempted: true}, res, tc.threshold)
			// Rules is a diagnostic, not part of the verdict predicate this test pins, and
			// comparing whole structs made the test brittle to every field added later.
			// It gets its own assertion below.
			rules := got.Rules
			got.Rules = nil
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fill:\n got %+v\nwant %+v", got, tc.want)
			}
			if got.Verdict == "malicious" && len(rules) == 0 {
				t.Error("a flagged row names no rule; that diagnostic is the one a rule author " +
					"acts on, and `corpus score` cannot produce it")
			}
			if got.Verdict == "benign" && len(rules) > 0 {
				t.Errorf("a benign row names rules %v; only a flag-carrying finding belongs there", rules)
			}
		})
	}
}
