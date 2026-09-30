// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

func TestCommentOnlyLines_Hash(t *testing.T) {
	src := "# rm -rf /\ncode=1\n  # eval(x)\nx=2 # trailing\n"
	got := commentOnlyLines(langHash, src)
	want := map[int]bool{1: true, 3: true} // lines 2 (code) and 4 (code + trailing comment) kept
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for l := range want {
		if !got[l] {
			t.Errorf("line %d should be comment-only", l)
		}
	}
}

func TestCommentOnlyLines_CStyleBlock(t *testing.T) {
	src := "/* eval(a)\n   curl x | bash */\nreal();\n// note\n"
	got := commentOnlyLines(langCStyle, src)
	for _, l := range []int{1, 2, 4} {
		if !got[l] {
			t.Errorf("line %d should be comment-only", l)
		}
	}
	if got[3] {
		t.Error("line 3 real(); must be kept as code")
	}
}

// CRITICAL zero-false-negative guarantee: a `/*` INSIDE a string must NOT open a block
// comment, or subsequent real code would be wrongly treated as a comment and dropped.
func TestCommentOnlyLines_SlashStarInString(t *testing.T) {
	src := "const s = \"/* not a comment\";\nrm -rf / ;\n"
	got := commentOnlyLines(langCStyle, src)
	if got[1] {
		t.Error("string containing /* must be code, not comment")
	}
	if got[2] {
		t.Error("FALSE NEGATIVE: line after a string-embedded /* was treated as comment")
	}
}

// A comment-only mention of eval() is dropped; a real eval() call is kept.
func TestDetect_DropsCommentMention(t *testing.T) {
	eng := New()
	root, art := skillArtifact(t, map[string]string{"x.js": "// eval(userInput) is risky\nreal(); eval(z)\n"})
	got, _ := eng.Run(root, []model.ArtifactReport{art})
	var lines []int
	for _, f := range got[0].Findings {
		if f.RuleID == "EXEC-003" {
			lines = append(lines, f.Evidence[0].Line)
		}
	}
	if len(lines) != 1 || lines[0] != 2 {
		t.Fatalf("EXEC-003 should fire only on line 2 (real call), got lines %v", lines)
	}
}

// OBF (dim 6) is NEVER dropped even in a comment (user-requested guard).
func TestDetect_ObfNotDroppedInComment(t *testing.T) {
	eng := New()
	root, art := skillArtifact(t, map[string]string{"x.js": "// base64 -d payload here\n"})
	got, _ := eng.Run(root, []model.ArtifactReport{art})
	found := false
	for _, f := range got[0].Findings {
		if f.Dimension == 6 {
			found = true
		}
	}
	if !found {
		t.Error("OBF (dim 6) must NOT be comment-dropped")
	}
}
