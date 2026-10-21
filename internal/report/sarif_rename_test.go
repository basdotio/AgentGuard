// SPDX-License-Identifier: MIT
package report

import (
	"strings"
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// The rename to BAS Guard (P-082) leaves every name a SARIF consumer keys on as it was. GitHub code
// scanning matches an alert across uploads by the tool name and the partialFingerprints key: a new
// key closes every open alert and reopens it as a new one, and each dismissal a reviewer made is
// lost. Scripts read the run properties by name. These stay `aguard` (P-082, Out of scope); only
// the prose around them says BAS Guard. The stage-2 bulk rename moved the three keys and the tests
// beside them in the same commit, so nothing went red: this test names the old keys on purpose.
func TestSARIF_MachineNamesKeptAcrossRename(t *testing.T) {
	doc := sarifOf(t, model.ScanResult{Root: "/r", Overall: 65, Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
			{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic, Title: "t",
				Evidence: []model.Evidence{{File: "skills/s/run.sh", Line: 3, Snippet: "curl x | bash"}}},
		}}}})
	run := run0(t, doc)

	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	if driver["name"] != "aguard" {
		t.Errorf("tool.driver.name = %v, want aguard: code scanning keys alerts by it", driver["name"])
	}

	rs := results(t, doc)
	if len(rs) != 1 {
		t.Fatalf("want one result, got %d", len(rs))
	}
	fp, _ := rs[0]["partialFingerprints"].(map[string]any)
	if _, ok := fp["aguard/v1"]; !ok || len(fp) != 1 {
		t.Errorf("partialFingerprints = %v, want exactly the key aguard/v1: a new key reopens every alert", fp)
	}

	props, _ := run["properties"].(map[string]any)
	for _, k := range []string{"aguard/overall", "aguard/root"} {
		if _, ok := props[k]; !ok {
			t.Errorf("run property %s missing; got %v", k, props)
		}
	}
	for k := range props {
		if strings.HasPrefix(k, "bas-guard/") {
			t.Errorf("run property %s: the keys keep the aguard/ prefix", k)
		}
	}
}
