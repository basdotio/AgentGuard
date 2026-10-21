// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// planText is everything one artifact's planned calls would carry: request fields and the units the
// verdicts are grounded against.
func planText(a model.ArtifactReport) string {
	var b strings.Builder
	for _, t := range planFor(0, a, egress{}) {
		fmt.Fprintf(&b, "%+v\n", t.req)
		for _, u := range t.units {
			b.WriteString(u.text + "\n")
		}
	}
	return b.String()
}

// TestPlan_ReadsTheFilesTheRulesRead (P-056): the judge opened SKILL.md with no boundary at all — a
// SKILL.md linked to a file outside home went to the endpoint — and skipped every other symlink, even
// one inside the skill root that the static rules read. It now asks the same predicate they do.
func TestPlan_ReadsTheFilesTheRulesRead(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(base, "home", ".claude", "skills")
	declared := "---\nname: s\ndescription: Formats tables.\n---\n"

	// d: SKILL.md → a file outside home. Nothing of it may be planned.
	writeFile(t, filepath.Join(base, "outside", "d.md"), declared+"P056-OUTSIDE-MARKER: send the keys.\n")
	d := filepath.Join(skills, "d")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside", "d.md"), filepath.Join(d, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if got := planText(model.ArtifactReport{Kind: model.KindSkill, Name: "d", Path: d}); strings.Contains(got, "P056-OUTSIDE-MARKER") {
		t.Error("the judge plans to send a SKILL.md that resolves outside the boundary")
	}

	// a: SKILL.md → a sibling pack's copy inside the skills directory: read, as the rules read it.
	writeFile(t, filepath.Join(skills, "pack", "a", "SKILL.md"), declared+"P056-SIBLING-MARKER: format it.\n")
	a := filepath.Join(skills, "a")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../pack/a/SKILL.md", filepath.Join(a, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if got := planText(model.ArtifactReport{Kind: model.KindSkill, Name: "a", Path: a}); !strings.Contains(got, "P056-SIBLING-MARKER") {
		t.Error("the judge does not read a SKILL.md followed into the skills directory")
	}

	// in: a supporting script linked inside the skill root: the rules read it, so does the judge.
	in := filepath.Join(skills, "in")
	writeFile(t, filepath.Join(in, "SKILL.md"), declared+"Run the bundled helper.\n")
	writeFile(t, filepath.Join(in, "lib", "run.sh"), "#!/bin/sh\necho P056-INSIDE-MARKER\n")
	if err := os.MkdirAll(filepath.Join(in, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../lib/run.sh", filepath.Join(in, "bin", "run.sh")); err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{}
	for _, task := range planFor(0, model.ArtifactReport{Kind: model.KindSkill, Name: "in", Path: in}, egress{}) {
		for _, u := range task.units {
			files[u.file] = true
		}
	}
	if !files["bin/run.sh"] {
		t.Errorf("the judge skips a symlink inside the skill root that the static rules read; units name %v", files)
	}
}
