// SPDX-License-Identifier: MIT
package hygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestAnalyze_SkipsAPluginsChildren: a plugin's skills are artifacts of their own for the judge and the
// report (P-044), not entries hygiene may propose to merge or trim — `clean` cannot move anything inside
// a plugin, and the blocker it printed ("installed elsewhere, e.g. by Claude Desktop") was wrong for
// them. Two children with the same description used to yield a duplicate_fn item; the same two skills
// installed at the top level still do.
func TestAnalyze_SkipsAPluginsChildren(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"s2", "s3"} {
		p := filepath.Join(dir, s, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: "+s+"\ndescription: Says hello to the user politely.\n---\nSay hello.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	doc := `[{"kind":"skill","name":"p:s2 (plugin p@mkt)","path":` + quote(filepath.Join(dir, "s2")) + `,"plugin":"p@mkt (1.0.0)"},` +
		`{"kind":"skill","name":"p:s3 (plugin p@mkt)","path":` + quote(filepath.Join(dir, "s3")) + `,"plugin":"p@mkt (1.0.0)"}]`
	var kids []model.ArtifactReport
	if err := json.Unmarshal([]byte(doc), &kids); err != nil {
		t.Fatal(err)
	}
	for _, it := range Analyze(dir, kids, Options{}) {
		for _, tg := range it.Targets {
			if strings.Contains(tg, "(plugin p@mkt)") {
				t.Errorf("hygiene item %s names a plugin's child: %q", it.Kind, it.Targets)
			}
		}
	}
	top := []model.ArtifactReport{{Kind: model.KindSkill, Name: "s2", Path: filepath.Join(dir, "s2")},
		{Kind: model.KindSkill, Name: "s3", Path: filepath.Join(dir, "s3")}}
	found := false
	for _, it := range Analyze(dir, top, Options{}) {
		found = found || it.Kind == "duplicate_fn"
	}
	if !found {
		t.Error("reverse: two top-level skills with one description must still be a duplicate_fn item")
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }
