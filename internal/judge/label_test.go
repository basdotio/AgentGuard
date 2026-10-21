// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// redactingNames are real skill names (issues/027) the entropy catch-all removes whole.
var redactingNames = []string{"migrating-tailwindcss-v4", "sysml-v2-guide-validator"}

// namedSkills is one skill per name, each description repeating the skill's own name.
func namedSkills(t *testing.T, names ...string) []model.ArtifactReport {
	t.Helper()
	root := t.TempDir()
	var arts []model.ArtifactReport
	for _, name := range names {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: Migrates the project; see the " + name + " docs.\n---\n" +
			"Run the upgrade tool, then fix the configuration it reports.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		arts = append(arts, model.ArtifactReport{Kind: model.KindSkill, Name: name, Path: dir, Findings: []model.Finding{}})
	}
	return arts
}

// TestRun_NotesNameArtifactsAsTheReportDoes (P-085, issues/027): the budget note names each skill
// it left unchecked by the name the report lists it under, never as `skill:<REDACTED>` — two such
// skills used to read the same, and the note de-duplicated them into one.
func TestRun_NotesNameArtifactsAsTheReportDoes(t *testing.T) {
	for _, n := range redactingNames {
		if detect.Redact(n) == n {
			t.Fatalf("fixture %q no longer redacts; pick a name that does", n)
		}
	}
	arts := namedSkills(t, redactingNames...)
	notes, stats := Run(context.Background(), &scriptedClient{}, arts, Options{MaxCalls: 1, Concurrency: 1})
	if stats.Skipped == 0 || len(notes) == 0 || !strings.Contains(notes[0].Why, "budget") {
		t.Fatalf("no budget note: skipped %d, notes %+v", stats.Skipped, notes)
	}
	why := notes[0].Why
	for _, n := range redactingNames {
		if !strings.Contains(why, "skill:"+n) {
			t.Errorf("the budget note does not name %q: %q", n, why)
		}
	}
	if strings.Contains(why, "<REDACTED>") {
		t.Errorf("the budget note redacts a name the report prints: %q", why)
	}
	// A name that never redacted is labelled exactly as before.
	if got := artifactLabel(model.ArtifactReport{Kind: model.KindSkill, Name: "route-tester"}); got != "skill:route-tester" {
		t.Errorf("label %q, want skill:route-tester", got)
	}
}

// TestUnaskedNote_NamesArtifactsAsTheReportDoes (P-085): the note on artifacts the judge asks
// nothing of names them as the report does.
func TestUnaskedNote_NamesArtifactsAsTheReportDoes(t *testing.T) {
	arts := []model.ArtifactReport{{Kind: model.KindQuarantined, Name: redactingNames[0]}}
	n, ok := unaskedNote(arts)
	if !ok || !strings.Contains(n.Why, string(model.KindQuarantined)+":"+redactingNames[0]) || strings.Contains(n.Why, "<REDACTED>") {
		t.Fatalf("ok=%v %q", ok, n.Why)
	}
}

// TestPlan_ExcerptsStillRedactNames (P-085, reverse): only the notes changed. A description that
// repeats a redaction-shaped name reaches the model with <REDACTED> in its place, as before.
func TestPlan_ExcerptsStillRedactNames(t *testing.T) {
	arts := namedSkills(t, redactingNames[0])
	calls := Plan(arts, Options{})
	if len(calls) == 0 {
		t.Fatal("no call planned")
	}
	for _, c := range calls {
		if strings.Contains(c.Payload, redactingNames[0]) {
			t.Errorf("%s payload carries the name the egress transform redacts: %q", c.Pass, c.Payload)
		}
	}
	if !strings.Contains(calls[0].Payload, "see the <REDACTED> docs") {
		t.Errorf("%s payload: %q", calls[0].Pass, calls[0].Payload)
	}
}
