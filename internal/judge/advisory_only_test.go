// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/score"
)

// TestMCPConfig_NeverCarriesWeight: LLM-009 is a hygiene question — unpinned package,
// unknown publisher, credentials in env — and hygiene is what real configs look like. Measured
// on 500 benign configs it was the only judge rule to escalate on benign input (5 of the 7
// judge-added flags). So it is reported, with the model's own severity, but never weighted:
// no vote, however unanimous, gives it Escalates, and the effective score stays where the
// static score was. The same run must still escalate LLM-001 and LLM-007 — the property is
// per rule, not a change to consensus.
func TestMCPConfig_NeverCarriesWeight(t *testing.T) {
	root := t.TempDir()
	cfg := writeFile(t, filepath.Join(root, ".claude.json"),
		`{"mcpServers":{"weather":{"command":"npx","args":["-y","weather-mcp@latest"],"env":{"TOKEN":"ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}}}`)
	arts := hostileSkill(t)
	arts = append(arts, model.ArtifactReport{Kind: model.KindMCP, Name: "weather", Path: cfg, Findings: []model.Finding{}})

	// Unanimous, high, quotable: every bar the consensus machinery knows about is cleared.
	client := &scriptedClient{answer: func(r Request) (Verdict, error) {
		switch r.Mode {
		case ModeMCPConfig:
			return Verdict{Flagged: true, Severity: "high", Summary: "unpinned fetch-on-run package holds a token",
				Evidence: quotableFrom(r)}, nil
		case ModeIntent:
			return Verdict{Flagged: true, Severity: "high", Summary: "undisclosed behavior",
				Evidence: quotableFrom(r)}, nil
		case ModeInjection:
			return Verdict{Flagged: false, BarrierEvidence: plantedDirective}, nil
		}
		return Verdict{}, nil
	}}
	Run(context.Background(), client, arts, Options{Samples: 3, Concurrency: 1})

	byRule := map[string]model.Finding{}
	for _, a := range arts {
		for _, f := range a.Findings {
			byRule[f.RuleID] = f
		}
	}

	f, ok := byRule["LLM-009"]
	if !ok {
		t.Fatalf("LLM-009 must still be REPORTED — advisory-only withholds weight, not visibility: %+v", arts[1].Findings)
	}
	if f.Escalates {
		t.Errorf("LLM-009 escalated on a 3-of-3 vote; it must never carry weight")
	}
	if f.Severity != model.SevHigh {
		t.Errorf("severity = %s, want the model's own (high): advisory-only is not a severity cap", f.Severity)
	}
	if !strings.Contains(f.Why, "never carries weight") {
		t.Errorf("the reader must be told this rule is advisory-only, got %q", f.Why)
	}

	// Reverse assertions: the same run, the same vote, the other rules still escalate.
	if g, ok := byRule["LLM-001"]; !ok || !g.Escalates {
		t.Errorf("LLM-001 on a 3-of-3 vote must escalate (reported=%v, escalates=%v)", ok, ok && g.Escalates)
	}
	if g, ok := byRule["LLM-007"]; !ok || !g.Escalates {
		t.Errorf("LLM-007 on a 3-of-3 vote must escalate (reported=%v, escalates=%v)", ok, ok && g.Escalates)
	}

	// And the score agrees: the MCP artifact's effective score is its static score.
	res := model.ScanResult{Artifacts: arts}
	score.Apply(&res)
	if mcp := res.Artifacts[1]; mcp.ScoreEffective != mcp.Score {
		t.Errorf("mcp score_effective = %d, score = %d: an advisory-only finding moved the score", mcp.ScoreEffective, mcp.Score)
	}
	if sk := res.Artifacts[0]; sk.ScoreEffective >= sk.Score {
		t.Errorf("skill score_effective = %d, score = %d: escalated LLM-001/007 should have moved it", sk.ScoreEffective, sk.Score)
	}
}
