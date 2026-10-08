// SPDX-License-Identifier: MIT
package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rules version is described by hand in several places besides the generated docs/rules.md
// header (which hack/gen-rules tests): the constant's own doc comment, the ScanResult field, spec
// §5.1 and §8, and the architecture pair. These tests read those passages as text, so a claim the
// code does not back cannot sit in one of them while the others are corrected.

// docPassage returns the passage of the repository file rel that begins on the line containing
// start, folded to single spaces with backticks and comment markers dropped, so a phrase can be
// asserted however it is wrapped or quoted. A passage ends at a blank line or a bare "//"; one
// that starts in a Go comment also ends where the comment does. With oneLine, only the starting
// line is taken (a field inside a code block, whose neighbours describe other fields).
func docPassage(t *testing.T, rel, start string, oneLine bool) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if !strings.Contains(l, start) {
			continue
		}
		comment := strings.HasPrefix(strings.TrimSpace(l), "//")
		var out []string
		for _, l := range lines[i:] {
			tl := strings.TrimSpace(l)
			if tl == "" || tl == "//" || (comment && !strings.HasPrefix(tl, "//")) {
				break
			}
			out = append(out, tl)
			if oneLine {
				break
			}
		}
		var words []string
		for _, w := range strings.Fields(strings.ReplaceAll(strings.Join(out, " "), "`", "")) {
			if w != "//" {
				words = append(words, w)
			}
		}
		return strings.Join(words, " ")
	}
	t.Fatalf("%s has no line containing %q; point this test at the passage that replaced it", rel, start)
	return ""
}

// TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion: every passage that puts the judge
// outside the rules version used to send the reader somewhere else for it — "the report's judge
// summary and the llm config say how it ran". Neither does: JudgeSummary records whether the judge
// ran, over how much and against which endpoint — not its model, prompt version, samples or
// authority — and the llm config is not in the report at all. The only thing in a report that
// pins the judge's code today is tool_version, so that is all these passages may promise.
func TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion(t *testing.T) {
	const (
		en = "only through tool_version"
		zh = "只通过 tool_version"
	)
	for _, c := range []struct {
		file, start string
		oneLine     bool
		want, avoid string
	}{
		{"internal/detect/rules_version.go", "// The LLM judge is outside the rules version", false, en, "how it ran"},
		{"internal/model/model.go", "// RulesVersion names the rule table", false, en, "how it ran"},
		{"docs/architecture.md", "**The rule table has a version.**", false, en, "how it ran"},
		{"docs/architecture.zh-CN.md", "**规则表有版本号。**", false, zh, "怎么跑的"},
		{"docs/spec/spec.zh-CN.md", "- **规则表版本 `rules_version`**", false, zh, "怎么跑的"},
		{"docs/spec/spec.zh-CN.md", "RulesVersion string // 规则表版本", true, zh, "Judge 说明"},
	} {
		p := docPassage(t, c.file, c.start, c.oneLine)
		if !strings.Contains(p, c.want) {
			t.Errorf("%s (%q…) does not say the report identifies the judge's code %q", c.file, c.start, c.want)
		}
		if strings.Contains(p, c.avoid) {
			t.Errorf("%s (%q…) says %q: no field in a report records which judge ran or how it was configured", c.file, c.start, c.avoid)
		}
	}
}

// TestArchitectureEpochNotesExcludeLLM: the architecture pair says which checks only the epoch
// covers, and a few lines later puts the judge outside rules_version. "The structural, permission
// and note checks … the epoch is all that covers them" took in LLM-000/002/005, which are notes,
// so the two sentences contradicted each other the way the rules.md header's did. The pair has to
// name the notes it puts on the epoch as the ones that are not LLM- IDs.
func TestArchitectureEpochNotesExcludeLLM(t *testing.T) {
	for _, c := range []struct{ file, start, want, avoid string }{
		{"docs/architecture.md", "**The rule table has a version.**", "the notes that are not LLM- IDs", "permission and note checks"},
		{"docs/architecture.zh-CN.md", "**规则表有版本号。**", "不是 LLM- ID 的 note", "权限和 note"},
	} {
		p := docPassage(t, c.file, c.start, false)
		if !strings.Contains(p, c.want) {
			t.Errorf("%s (%q…) does not say %q; the notes the epoch covers exclude every LLM- ID", c.file, c.start, c.want)
		}
		if strings.Contains(p, c.avoid) {
			t.Errorf("%s (%q…) still says %q, which puts the LLM- notes on the epoch", c.file, c.start, c.avoid)
		}
	}
}
