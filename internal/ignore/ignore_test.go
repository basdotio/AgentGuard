// SPDX-License-Identifier: MIT
package ignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

func find(rule, file string) model.Finding {
	return model.Finding{RuleID: rule, Evidence: []model.Evidence{{File: file}}}
}

func loadFrom(t *testing.T, body string) *Matcher {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".aguardignore")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoad_MissingIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil || !m.Empty() {
		t.Fatalf("missing ignore file must be empty no-op; err=%v empty=%v", err, m.Empty())
	}
}

func TestSuppresses_RuleEverywhere(t *testing.T) {
	m := loadFrom(t, "# baseline\nINJ-001\n")
	if !m.Suppresses(find("INJ-001", "a/b.md")) {
		t.Error("bare rule id should suppress everywhere")
	}
	if m.Suppresses(find("EXEC-001", "a/b.md")) {
		t.Error("must not suppress a different rule")
	}
}

func TestSuppresses_RuleWithGlob(t *testing.T) {
	m := loadFrom(t, "EXEC-004 vendor/*\n")
	if !m.Suppresses(find("EXEC-004", "vendor/x.js")) {
		t.Error("glob should match one path segment")
	}
	if m.Suppresses(find("EXEC-004", "src/x.js")) {
		t.Error("glob must not match outside path")
	}
	// single * does NOT cross a slash (filepath.Match semantics)
	if m.Suppresses(find("EXEC-004", "vendor/sub/x.js")) {
		t.Error("single-* glob must not match across a slash")
	}
}

func TestSuppresses_RecursiveGlob(t *testing.T) {
	m := loadFrom(t, "EXEC-004 vendor/**\n")
	if !m.Suppresses(find("EXEC-004", "vendor/sub/deep/x.js")) {
		t.Error("dir/** should match recursively")
	}
	if m.Suppresses(find("EXEC-004", "vendorx/y.js")) {
		t.Error("dir/** must respect the path boundary (no prefix bleed)")
	}
}

func TestSuppresses_PrefixNoCrossBoundary(t *testing.T) {
	// a bare-glob prefix must not bleed across directories (the old prefix-hack bug).
	m := loadFrom(t, "INJ-001 skills/foo*\n")
	if m.Suppresses(find("INJ-001", "skills/foobar/x.md")) {
		t.Error("single-* prefix must not cross into a sub-path")
	}
}

func TestSuppresses_Wildcard(t *testing.T) {
	m := loadFrom(t, "* dist/*\n")
	if !m.Suppresses(find("ANYTHING", "dist/bundle.js")) {
		t.Error("* rule should suppress any rule under the glob")
	}
	if m.Suppresses(find("ANYTHING", "src/bundle.js")) {
		t.Error("* rule must still respect the glob")
	}
}

func TestApply_CountsAndRemoves(t *testing.T) {
	m := loadFrom(t, "INJ-001\n")
	hi := find("INJ-001", "a")
	hi.Severity = model.SevHigh
	lo := find("INJ-001", "c")
	lo.Severity = model.SevLow
	arts := []model.ArtifactReport{{
		Findings: []model.Finding{hi, find("EXEC-001", "b"), lo},
	}}
	res := m.Apply(arts)
	if res.Count != 2 {
		t.Errorf("suppressed count = %d, want 2", res.Count)
	}
	if res.MaxSeverity != model.SevHigh {
		t.Errorf("suppressed MaxSeverity = %q, want high (max of the 2 suppressed)", res.MaxSeverity)
	}
	if len(arts[0].Findings) != 1 || arts[0].Findings[0].RuleID != "EXEC-001" {
		t.Errorf("only EXEC-001 should remain; got %+v", arts[0].Findings)
	}
}

// The baseline holds two kinds of entry in one file, and they must not be able to be mistaken for
// one another: a rule id silences a class of finding everywhere, a cleanup item id retires exactly
// one decision. Parsing a rule id as an item id (or the reverse) would make a narrow entry broad.
func TestBaseline_TellsItemIDsFromRuleIDs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".aguardignore")
	body := "# reviewed\nINJ-004\nD-121d192b  # keep both: connect-chrome, open-devkit-browser\n" +
		"Z-87845f60\nCOV-000  vendor/**\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-121d192b", "Z-87845f60"} {
		if !m.suppressesItem(id) {
			t.Errorf("%s should suppress the cleanup item it names", id)
		}
	}
	if m.suppressesItem("INJ-004") || m.suppressesItem("COV-000") {
		t.Error("a rule id must not be readable as a cleanup item id")
	}
	if !m.Suppresses(model.Finding{RuleID: "INJ-004"}) {
		t.Error("rule entries must keep working unchanged alongside item entries")
	}
	if m.Suppresses(model.Finding{RuleID: "D-121d192b"}) {
		t.Error("an item id must not silence a finding that happens to share its id")
	}
}

// Suppressing a cleanup item is still suppression, so the caller has to be told how many went.
// Unlike a finding there is no severity to mirror here — the count IS the whole disclosure.
func TestBaseline_ItemSuppressionIsCounted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".aguardignore")
	if err := os.WriteFile(p, []byte("D-aaaaaaaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	items := []model.CleanItem{
		{ID: "D-aaaaaaaa", Kind: "duplicate_fn"},
		{ID: "D-bbbbbbbb", Kind: "duplicate_fn"},
		{Kind: "zombie"}, // a note: no id, must never be suppressed by an empty match
	}
	kept, n := m.ApplyItems(items)
	if n != 1 {
		t.Errorf("suppressed count = %d, want 1", n)
	}
	if len(kept) != 2 || kept[0].ID != "D-bbbbbbbb" {
		t.Errorf("wrong item removed: %+v", kept)
	}
}
