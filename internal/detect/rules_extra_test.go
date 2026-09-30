// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

// TestEXEC009_IexContext: PowerShell IEX invocations match; the Elixir `iex` REPL
// mentioned in prose does NOT (v0.2 review FP fix).
func TestEXEC009_IexContext(t *testing.T) {
	eng := New()
	hit := func(content string) bool {
		root, art := skillArtifact(t, map[string]string{"x.ps1": content})
		got, _ := eng.Run(root, []model.ArtifactReport{art})
		for _, f := range got[0].Findings {
			if f.RuleID == "EXEC-009" {
				return true
			}
		}
		return false
	}
	for _, m := range []string{"Invoke-Expression $cmd", "$payload | iex", "iex($code)", "iex \"$x\""} {
		if !hit(m) {
			t.Errorf("EXEC-009 should match %q", m)
		}
	}
	for _, ok := range []string{"Run the Elixir iex REPL to explore.", "iexplore was the old IE binary"} {
		if hit(ok) {
			t.Errorf("EXEC-009 false-positive on %q", ok)
		}
	}
}
