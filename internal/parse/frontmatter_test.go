// SPDX-License-Identifier: MIT
package parse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name, in, wantFront, wantBody string
	}{
		{"normal", "---\nname: x\n---\nbody\n", "name: x", "\nbody\n"},
		{"no frontmatter", "# just body\n", "", "# just body\n"},
		{"leading blank + bom", "\uFEFF\n---\nname: y\n---\nb", "name: y", "\nb"},
		{"crlf", "---\r\nname: z\r\n---\r\nbody", "name: z\r", "\r\nbody"},
		{"unterminated", "---\nname: q\nno end", "", "---\nname: q\nno end"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, b, _ := splitFrontmatter(c.in)
			if f != c.wantFront {
				t.Errorf("front = %q, want %q", f, c.wantFront)
			}
			if b != c.wantBody {
				t.Errorf("body = %q, want %q", b, c.wantBody)
			}
		})
	}
}

func TestReadSkill(t *testing.T) {
	dir := t.TempDir()
	// multiline YAML description block must be captured.
	md := "---\nname: demo\ndescription: |\n  line one\n  line two\n---\n# Body\nsee refs/x.md\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadSkill(dir)
	if !m.OK {
		t.Fatal("OK should be true for a present SKILL.md")
	}
	if m.Name != "demo" {
		t.Errorf("name = %q", m.Name)
	}
	if m.Description == "" || m.Description[:8] != "line one" {
		t.Errorf("multiline description not captured: %q", m.Description)
	}
	if wantBody := "see refs/x.md"; !contains(m.Body, wantBody) {
		t.Errorf("body missing %q; got %q", wantBody, m.Body)
	}
}

func TestReadSkill_Missing(t *testing.T) {
	if m := ReadSkill(t.TempDir()); m.OK {
		t.Error("missing SKILL.md should yield OK=false")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
