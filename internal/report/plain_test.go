// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

func TestFriendlyArtifact(t *testing.T) {
	cases := map[string]string{
		"plugin:figma@claude-plugins-official (2.2.107)": "figma plugin, from claude-plugins-official (2.2.107)",
		"plugin:local-thing":                             "local-thing plugin",
		"skill:deploy":                                   "skill deploy",
		"hook:PreToolUse[Bash]#1":                        "hook PreToolUse[Bash]#1",
		"permission:permissions":                         "permission rules (settings.json)",
		"permission:permissions (local)":                 "permission rules (project settings.local.json)",
		"mcp:github":                                     "MCP server github",
		"no-colon":                                       "no-colon",
	}
	for in, want := range cases {
		if got := friendlyArtifact(in); got != want {
			t.Errorf("friendlyArtifact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortPath(t *testing.T) {
	cases := map[string]string{
		"plugins/cache/claude-plugins-official/figma/2.2.107/workflow-skills/x/scripts/extract.py": "figma › workflow-skills/x/scripts/extract.py",
		"plugins/cache/mk/p/1.0.0": "p",
		"run.sh":                   "run.sh",
		"skills/s/run.sh":          "skills/s/run.sh",
		"a/b/c/d/e/f.py":           "…/d/e/f.py",
	}
	for in, want := range cases {
		if got := shortPath(in); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerdictSentence(t *testing.T) {
	cases := []struct {
		level      string
		act, total int
		want       string
	}{
		{"Low", 0, 0, "Your Claude Code setup looks safe. No findings."},
		{"Low", 0, 2, "Your Claude Code setup looks safe. 2 informational findings, nothing needs action."},
		{"Low", 1, 3, "Your Claude Code setup looks safe. 1 finding needs a look (medium or above); 2 more are informational."},
		{"Elevated", 2, 2, "There are problems you should fix before relying on this setup. 2 findings need a look (medium or above)."},
		{"High", 1, 1, "Serious problems were found. Stop and review before using this setup. 1 finding needs a look (medium or above)."},
	}
	for _, c := range cases {
		if got := verdictSentence(c.level, c.act, c.total); got != c.want {
			t.Errorf("verdictSentence(%s,%d,%d) =\n  %q\nwant\n  %q", c.level, c.act, c.total, got, c.want)
		}
	}
}

func TestCheckedLine(t *testing.T) {
	got := checkedLine(model.EnvSummary{Plugins: 2, Hooks: 3, Permissions: 11, Memories: 1})
	if got != "Checked 2 plugins, 3 hooks, 11 permission rules, 1 memory file." {
		t.Errorf("checkedLine = %q", got)
	}
	if got := checkedLine(model.EnvSummary{}); !strings.Contains(got, "Nothing was found") {
		t.Errorf("empty inventory must say so, got %q", got)
	}
}

// TestText_SummaryLeadsForBothReaders: the default report opens with a plain sentence, what to
// look at, and readable artifact names; the auditor's inventory and OWASP lines close it. The
// rule id is still present on every finding — at the end of its line, not the start.
func TestText_SummaryLeadsForBothReaders(t *testing.T) {
	r := sampleResult()
	r.Artifacts[0].Kind, r.Artifacts[0].Name = model.KindPlugin, "figma@claude-plugins-official (2.2.107)"
	for i := range r.Artifacts[0].Findings {
		for j := range r.Artifacts[0].Findings[i].Evidence {
			r.Artifacts[0].Findings[i].Evidence[j].File = "plugins/cache/claude-plugins-official/figma/2.2.107/scripts/" + r.Artifacts[0].Findings[i].Evidence[j].File
		}
	}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()

	order := []string{
		"Risk score 69/100 (Elevated)",
		"Summary",
		"There are problems you should fix before relying on this setup.",
		"Checked 2 skills, 1 hook.",
		"start with [EXEC-001] figma plugin, from claude-plugins-official (2.2.107)",
		"1. figma plugin, from claude-plugins-official (2.2.107) — curl piped to shell (high)",
		"where: figma › scripts/run.sh:3 (+1 more file)",
		"Findings · STATIC",
		"curl piped to shell ×2 in 2 files  [EXEC-001]", // rule id closes the line
		"in plain terms: can run programs on your machine",
		"← figma › scripts/run.sh:3",
		"Scan details",
		"Inventory: skills=2",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 {
			t.Errorf("missing %q in:\n%s", want, out)
			continue
		}
		if i < last {
			t.Errorf("%q appears before the previous anchor; reading order broken:\n%s", want, out)
		}
		last = i
	}
	if strings.Contains(out, "plugins/cache/claude-plugins-official") {
		t.Error("default view must shorten plugin-cache paths; --verbose keeps them")
	}
	var full bytes.Buffer
	TextVerbose(&full, r)
	if !strings.Contains(full.String(), "plugins/cache/claude-plugins-official/figma/2.2.107/scripts/run.sh:3") {
		t.Error("verbose view must keep the full evidence path")
	}
}
