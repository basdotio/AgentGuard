// SPDX-License-Identifier: MIT
package score

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// unitResult builds a result from JSON, so the `plugin` member that links a child to its plugin can be
// written on a tree whose struct does not have it yet: there the member is ignored, every child is an
// artifact of its own, and the S2 numbers come out (P-044).
func unitResult(t *testing.T, artifacts string) model.ScanResult {
	t.Helper()
	var r model.ScanResult
	if err := json.Unmarshal([]byte(`{"artifacts":`+artifacts+`}`), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

const (
	medium4  = `{"rule_id":"EXEC-004","dimension":4,"severity":"medium","source":"static","evidence":[{"file":"scripts/build.py","line":2}]}`
	dupHigh  = `{"rule_id":"EXEC-001","dimension":4,"severity":"high","source":"static","evidence":[{"file":"skills/s1/SKILL.md","line":6}]}`
	ownMed   = `{"rule_id":"EXFIL-001","dimension":1,"severity":"medium","source":"static","evidence":[{"file":"skills/s1/SKILL.md","line":9}]}`
	llmHigh  = `{"rule_id":"LLM-001","dimension":2,"severity":"high","source":"llm","escalates":true,"evidence":[{"file":"skills/s1/SKILL.md","line":5}]}`
	pluginP  = `{"kind":"plugin","name":"p@mkt (1.0.0)","path":"/h/p","findings":[` + medium4 + `]}`
	hookOfP  = `{"kind":"hook","name":"PreToolUse[Bash]#1 (plugin p@mkt)","path":"/h/p/hooks/hooks.json","findings":[]}`
	cleanKid = `{"kind":"skill","name":"p:s%d (plugin p@mkt)","path":"/h/p/skills/s%d","plugin":"p@mkt (1.0.0)","findings":[]}`
)

func kids(n int) string {
	out := ""
	for i := 2; i < 2+n; i++ {
		out += "," + fmt.Sprintf(cleanKid, i, i)
	}
	return out
}

// TestApply_PluginAndChildrenAreOneUnit: a plugin and its children are ONE entry of the environment
// average, so looking closer at a plugin cannot raise the score (S2 measured 94 → 98 and 88 → 98), and
// a child's qualified LLM finding still reaches overall_effective through its unit.
func TestApply_PluginAndChildrenAreOneUnit(t *testing.T) {
	cases := []struct {
		name         string
		artifacts    string
		overall, eff int
		pluginScore  int
		pluginEff    int
	}{
		{
			name:      "scan: plugin with one medium, its hook, six clean children",
			artifacts: `[` + pluginP + `,` + hookOfP + kids(6) + `]`,
			overall:   94, eff: 94, pluginScore: 88, pluginEff: 88,
		},
		{
			name:      "check <plugin>: the plugin and six clean children",
			artifacts: `[` + pluginP + kids(6) + `]`,
			overall:   88, eff: 88, pluginScore: 88, pluginEff: 88,
		},
		{
			name: "a child repeats the plugin's high: nothing moves",
			artifacts: `[{"kind":"plugin","name":"p@mkt (1.0.0)","path":"/h/p","findings":[` + medium4 + `,` + dupHigh + `]},` +
				`{"kind":"skill","name":"p:s1 (plugin p@mkt)","path":"/h/p/skills/s1","plugin":"p@mkt (1.0.0)","findings":[` + dupHigh + `]}` + kids(3) + `]`,
			overall: 69, eff: 69, pluginScore: 75, pluginEff: 75,
		},
		{
			name: "a child's own finding lowers its unit",
			artifacts: `[` + pluginP + `,` + hookOfP + `,` +
				`{"kind":"skill","name":"p:s1 (plugin p@mkt)","path":"/h/p/skills/s1","plugin":"p@mkt (1.0.0)","findings":[` + ownMed + `]}` + kids(2) + `]`,
			overall: 88, eff: 88, pluginScore: 88, pluginEff: 88,
		},
		{
			name: "a child's qualified LLM high reaches overall_effective, not overall",
			artifacts: `[` + pluginP + `,` + hookOfP + `,` +
				`{"kind":"skill","name":"p:s1 (plugin p@mkt)","path":"/h/p/skills/s1","plugin":"p@mkt (1.0.0)","findings":[` + llmHigh + `]}` + kids(2) + `]`,
			overall: 94, eff: 69, pluginScore: 88, pluginEff: 88,
		},
		{
			name:      "a child naming no plugin of this result is an entry of its own",
			artifacts: `[` + pluginP + `,{"kind":"skill","name":"q:s (plugin q@mkt)","path":"/h/q/skills/s","plugin":"q@mkt (1.0.0)","findings":[]}]`,
			overall:   94, eff: 94, pluginScore: 88, pluginEff: 88,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := unitResult(t, c.artifacts)
			Apply(&r)
			if r.Overall != c.overall || r.OverallEffective != c.eff {
				t.Errorf("overall %d / effective %d, want %d / %d", r.Overall, r.OverallEffective, c.overall, c.eff)
			}
			if r.OverallEffective > r.Overall {
				t.Errorf("overall_effective %d > overall %d", r.OverallEffective, r.Overall)
			}
			p := r.Artifacts[0]
			if p.Score != c.pluginScore || p.ScoreEffective != c.pluginEff {
				t.Errorf("plugin artifact %d / %d, want %d / %d: per-artifact scores keep their definition",
					p.Score, p.ScoreEffective, c.pluginScore, c.pluginEff)
			}
			for _, a := range r.Artifacts {
				if a.Score != artifactScore(a, Deterministic) {
					t.Errorf("%s: score %d is not its own findings' score", a.Name, a.Score)
				}
			}
		})
	}
}

// TestFamilies_LinkByNameThenPath: the link is the collector's Plugin field; when two plugin artifacts
// share a name (two install channels), the one whose path holds the child is its plugin; a name no
// plugin carries links nothing; a plugin is never a child.
func TestFamilies_LinkByNameThenPath(t *testing.T) {
	arts := []model.ArtifactReport{
		{Kind: model.KindPlugin, Name: "p", Path: "/a/p"},
		{Kind: model.KindPlugin, Name: "p", Path: "/b/p"},
		{Kind: model.KindSkill, Name: "p:s", Path: "/b/p/skills/s", Plugin: "p"},
		{Kind: model.KindSkill, Name: "q:s", Path: "/c/q/skills/s", Plugin: "q"},
		{Kind: model.KindPlugin, Name: "r", Path: "/r", Plugin: "p"},
	}
	f := Families(arts)
	for i, want := range []int{-1, -1, 1, -1, -1} {
		if got := f.Parent(i); got != want {
			t.Errorf("Parent(%d) = %d, want %d", i, got, want)
		}
	}
}
