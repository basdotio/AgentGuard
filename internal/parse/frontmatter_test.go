// SPDX-License-Identifier: MIT
package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name, in, wantFront, wantBody string
	}{
		{"normal", "---\nname: x\n---\nbody\n", "name: x", "\nbody\n"},
		{"no frontmatter", "# just body\n", "", "# just body\n"},
		// Claude Code 2.1.107 does not read a frontmatter that is not the file's first bytes (P-024), so
		// neither does this: the block is body text, and the body starts after the skipped lead.
		{"leading blank + bom is not frontmatter", "\uFEFF\n---\nname: y\n---\nb", "", "---\nname: y\n---\nb"},
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

// writeMD puts one markdown file in a fresh temp dir and returns its path.
func writeMD(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rule.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Every row is a shape measured on Claude Code 2.1.107 (P-024): an InstructionsLoaded hook recorded
// whether a rule whose paths point at a directory that does not exist loaded at session_start. want is
// "Claude Code honoured paths", i.e. it did NOT load at session start. The label exists to tell a
// reader which rules cost context every session; calling one of these path-scoped says less than the
// agent actually reads.
func TestPathScoped_MatchesClaudeCode(t *testing.T) {
	const scoped = "---\npaths:\n  - \"src/**\"\n---\n# rule\n"
	rule := func(front string) string { return "---\n" + front + "\n---\n# rule\n" }
	cases := []struct {
		name, in string
		want     bool
	}{
		// Leading bytes. Claude Code matches /^---\s*\n/ against the text as read, BOM included.
		{"--- on line 1, LF", scoped, true},
		{"--- on line 1, CRLF", strings.ReplaceAll(scoped, "\n", "\r\n"), true},
		{"a blank line above", "\n" + scoped, false},
		{"a BOM above", "\uFEFF" + scoped, false},
		{"a line of spaces above", "   \n" + scoped, false},
		{"an HTML comment above", "<!-- SPDX-License-Identifier: MIT -->\n" + scoped, false},
		{"a heading above", "# Title\n" + scoped, false},

		// Values, frontmatter on line 1. Claude Code splits strings on top-level commas, expands braces,
		// drops a trailing /** and empty strings; nothing left, or only **, means no paths at all.
		{"list", rule("paths:\n  - \"src/**\""), true},
		{"scalar string", rule(`paths: "src/**"`), true},
		{"comma string", rule(`paths: "src/**, lib/**"`), true},
		{"nested list", rule("paths:\n  - [\"src/**\"]"), true},
		{"brace alternatives", rule(`paths: "src/{a,b}/**"`), true},
		{"no value", rule("paths:"), false},
		{"empty list", rule("paths: []"), false},
		{"empty string", rule(`paths: ""`), false},
		{"list of an empty string", rule("paths:\n  - \"\""), false},
		{"**", rule("paths:\n  - \"**\""), false},
		{"**/**", rule("paths:\n  - \"**/**\""), false},
		{"/**", rule(`paths: "/**"`), false},
		{"** twice in one string", rule(`paths: "**, **"`), false},
		{"braces that expand to **", rule(`paths: "{**,**}"`), false},
		{"a number", rule("paths: 5"), false},
		{"a mapping", rule("paths:\n  a: src/**"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PathScoped(writeMD(t, c.in)); got != c.want {
				t.Errorf("PathScoped = %v, want %v (Claude Code 2.1.107 %s)", got, c.want,
					map[bool]string{true: "honours these paths", false: "loads this rule every session"}[c.want])
			}
		})
	}
}

// Brace alternatives are expanded to decide, and the bytes deciding how far are the artifact
// author's: forty groups are 2^40 expansions. Bounded, the answer comes back at once, and an
// answer cut short says "every session" — the reading that cannot understate what is loaded.
// Timed by the test itself, because the regression is a hang, not a wrong value.
func TestPathScoped_BraceExpansionIsBounded(t *testing.T) {
	cases := []struct {
		name, glob string
		want       bool
	}{
		{"every expansion empty", strings.Repeat("{,}", 40), false},
		{"first expansion already a glob", strings.Repeat("{x,y}", 40), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeMD(t, "---\npaths: \""+c.glob+"\"\n---\n")
			done := make(chan bool, 1)
			go func() { done <- PathScoped(p) }()
			select {
			case got := <-done:
				if got != c.want {
					t.Errorf("PathScoped = %v, want %v", got, c.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("PathScoped did not return within 2s: brace expansion is unbounded")
			}
		})
	}
}

// The same parser reads a skill's name and description. Measured on Claude Code 2.1.107 (P-024): a
// SKILL.md whose --- follows a BOM, a blank line or a line of spaces is still listed, but its
// frontmatter is ignored and the listing shows "---" as the description. Reading the frontmatter
// anyway measured context bloat and duplicate descriptions for text the agent never sees.
func TestReadSkill_FrontmatterMustStartAtTheFirstByte(t *testing.T) {
	const md = "---\nname: probe\ndescription: probe description\n---\n# Body\n"
	cases := []struct {
		name, lead string
		bodyLine   int
	}{
		{"BOM", "\uFEFF", 1},
		{"blank line", "\n", 2},
		{"line of spaces", "   \n", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(c.lead+md), 0o644); err != nil {
				t.Fatal(err)
			}
			m := ReadSkill(dir)
			if !m.OK {
				t.Fatal("a present SKILL.md must read OK")
			}
			if m.Name != "" || m.Description != "" {
				t.Errorf("name/description = %q/%q, want both empty: Claude Code ignores this frontmatter", m.Name, m.Description)
			}
			if !strings.Contains(m.Body, "description: probe description") {
				t.Errorf("the ignored frontmatter is body text the skill delivers; body = %q", m.Body)
			}
			if m.BodyLine != c.bodyLine {
				t.Errorf("BodyLine = %d, want %d (the line the --- is on)", m.BodyLine, c.bodyLine)
			}
		})
	}

	// Reverse: frontmatter on the first byte reads as it always has, with either line ending.
	for name, in := range map[string]string{"LF": md, "CRLF": strings.ReplaceAll(md, "\n", "\r\n")} {
		t.Run("first byte, "+name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(in), 0o644); err != nil {
				t.Fatal(err)
			}
			m := ReadSkill(dir)
			if strings.TrimSpace(m.Name) != "probe" || strings.TrimSpace(m.Description) != "probe description" {
				t.Errorf("name/description = %q/%q, want probe/probe description", m.Name, m.Description)
			}
		})
	}
}
