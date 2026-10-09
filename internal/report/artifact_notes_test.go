// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// parseFailure is the shape collect.withParseError produces: an artifact whose only finding is a
// dimension-0 note, attached to the artifact rather than to the scan. Every human renderer used
// to read notes from ScanResult.Notes alone, so this one was printed nowhere but JSON and SARIF.
func parseFailure(file string) model.ScanResult {
	return model.ScanResult{
		Root: "/x/.claude", ToolVersion: "test", Overall: 100, OverallEffective: 100,
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindHook, Name: "settings.json", Path: file, Score: 100, ScoreEffective: 100,
			Findings: []model.Finding{{
				RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
				Title:    "Parse failed, artifact not fully covered",
				Why:      "Config file is corrupt or malformed; its content checks were skipped (partial).",
				Evidence: []model.Evidence{{File: file, Snippet: "parse error"}},
			}},
		}},
		Notes: []model.Finding{},
	}
}

type humanReport struct{ name, body string }

func humanReports(t *testing.T, r model.ScanResult) []humanReport {
	t.Helper()
	var brief, full, md, page bytes.Buffer
	Text(&brief, r)
	TextVerbose(&full, r)
	if err := Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if err := HTML(&page, r); err != nil {
		t.Fatal(err)
	}
	return []humanReport{{"terminal", brief.String()}, {"verbose", full.String()}, {"markdown", md.String()}, {"html", page.String()}}
}

// TestArtifactNoteReachesEveryHumanRenderer: the note is shown by all four, and the file name it
// carries — chosen by whoever named the directory — is cleaned on the way (invariant #7). A bidi
// override in a path segment must arrive as U+FFFD and an ESC must not arrive at all; the segment
// sits inside the three path segments the default view keeps, so the summary line is held to it too.
func TestArtifactNoteReachesEveryHumanRenderer(t *testing.T) {
	r := parseFailure("/x/pay\u202Egnp\x1b[2J/.claude/settings.json")
	for _, h := range humanReports(t, r) {
		if !strings.Contains(h.body, "PARSE-000") || !strings.Contains(h.body, "settings.json") {
			t.Errorf("%s report does not show the artifact's own note:\n%s", h.name, h.body)
		}
		if strings.ContainsRune(h.body, '\u202E') || strings.ContainsRune(h.body, '\x1b') {
			t.Errorf("%s report carries the raw bidi override or ESC from a scanned path", h.name)
		}
		if strings.Contains(h.body, "PARSE-000") && !strings.ContainsRune(h.body, '\uFFFD') {
			t.Errorf("%s report shows the note but not the U+FFFD that marks what was cleaned from its path", h.name)
		}
	}
}

// summaryOf cuts the part of a report that states the verdict: the terminal's Summary block, the
// markdown's Summary section, the HTML summary card. "coverage is incomplete" appears further down
// in all three already (the Not checked line); the question here is what the HEADLINE says.
func summaryOf(name, body string) string {
	var from, to string
	switch name {
	case "terminal", "verbose":
		from, to = "\nSummary\n", "\n\n"
	case "markdown":
		from, to = "## Summary", "## What to look at"
	case "html":
		from, to = `<section id="summary">`, `</section>`
	}
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	rest := body[i+len(from):]
	if j := strings.Index(rest, to); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// TestSummaryDoesNotCallIncompleteCoverageSafe pins what the headline says when the scan says,
// further down, that coverage is incomplete. The set that decides it is the one the "Not checked"
// line counts — a scan-level note and an artifact's own note alike — so the two can never disagree.
// Trust decisions are not coverage gaps (the summary has its own line for them), and a result with
// no note at all keeps the sentence it always had.
func TestSummaryDoesNotCallIncompleteCoverageSafe(t *testing.T) {
	clean := model.ScanResult{Root: "/x/.claude", ToolVersion: "test", Overall: 100, OverallEffective: 100,
		Env: model.EnvSummary{Skills: 1}, Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Score: 100, ScoreEffective: 100}},
		Notes: []model.Finding{}}
	scanGap := clean
	scanGap.Notes = []model.Finding{{RuleID: "IO-000", Dimension: 0, Severity: model.SevMedium, Title: "settings.json could not be read",
		Evidence: []model.Evidence{{File: "/x/.claude/settings.json"}}}}
	trusted := clean
	trusted.Notes = []model.Finding{{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "Findings suppressed: known-trusted artifact",
		Evidence: []model.Evidence{{File: "superpowers (6.3.0)"}}}}

	for _, c := range []struct {
		name     string
		r        model.ScanResult
		wantSafe bool
	}{
		{"no note", clean, true},
		{"trust decision only", trusted, true},
		{"scan-level coverage note", scanGap, false},
		{"artifact's own coverage note", parseFailure("/x/.claude/settings.json"), false},
	} {
		for _, h := range humanReports(t, c.r) {
			s := summaryOf(h.name, h.body)
			if s == "" {
				t.Fatalf("%s/%s: no summary found in:\n%s", c.name, h.name, h.body)
			}
			safe := strings.Contains(s, "looks safe")
			if safe != c.wantSafe {
				t.Errorf("%s/%s: summary says looks safe = %v, want %v:\n%s", c.name, h.name, safe, c.wantSafe, s)
			}
			if !c.wantSafe && !strings.Contains(s, "coverage is incomplete") {
				t.Errorf("%s/%s: summary must say coverage is incomplete, in the words the Not checked line uses:\n%s", c.name, h.name, s)
			}
		}
	}
}

// TestCoverageVerdict: only the Low band's lead depends on the gaps, and the counting half never
// does — a verdict that changed its counts with coverage would say something the list does not.
func TestCoverageVerdict(t *testing.T) {
	for _, c := range []struct {
		level            string
		act, total, gaps int
		want             string
	}{
		{"Low", 0, 0, 0, "Your Claude Code setup looks safe. No findings."},
		{"Low", 0, 0, 1, "Low risk in what was read, but coverage is incomplete. No findings."},
		{"Low", 1, 3, 2, "Low risk in what was read, but coverage is incomplete. 1 finding needs a look (medium or above); 2 more are informational."},
		{"Watch", 1, 1, 4, "Mostly fine, with a few things to review. 1 finding needs a look (medium or above)."},
		{"Elevated", 2, 2, 1, "There are problems you should fix before relying on this setup. 2 findings need a look (medium or above)."},
	} {
		if got := coverageVerdict(c.level, c.act, c.total, c.gaps); got != c.want {
			t.Errorf("coverageVerdict(%s,%d,%d,%d) =\n  %q\nwant\n  %q", c.level, c.act, c.total, c.gaps, got, c.want)
		}
	}
}

// TestCheckedWithGaps: the inventory line names what was found but not fully checked — after the
// counts when there are counts, on its own when there are none — and never says "Nothing was found"
// once something was. The list is capped and names its remainder; a gap whose note names no file
// falls back to the artifact's name; a trust note is not a gap.
func TestCheckedWithGaps(t *testing.T) {
	one := []gap{{"…/u/.claude/settings.json", "PARSE-000"}}
	five := append(one, gap{"…/u/.mcp.json", "PARSE-000"}, gap{"x", "PARSE-000"}, gap{"y", "PARSE-000"}, gap{"z", "PARSE-000"})
	for _, c := range []struct {
		name string
		env  model.EnvSummary
		gs   []gap
		want string
	}{
		{"nothing at all", model.EnvSummary{}, nil, "Nothing was found to check under this root."},
		{"only a gap", model.EnvSummary{}, one, "Not fully checked: …/u/.claude/settings.json [PARSE-000]."},
		{"counts and a gap", model.EnvSummary{Hooks: 1}, one, "Checked 1 hook. Not fully checked: …/u/.claude/settings.json [PARSE-000]."},
		{"counts only", model.EnvSummary{Hooks: 1}, nil, "Checked 1 hook."},
		{"capped", model.EnvSummary{}, five, "Not fully checked: …/u/.claude/settings.json [PARSE-000], …/u/.mcp.json [PARSE-000], x [PARSE-000], and 2 more."},
	} {
		if got := checkedWithGaps(c.env, gapList(c.gs, plainGap)); got != c.want {
			t.Errorf("%s: got\n  %q\nwant\n  %q", c.name, got, c.want)
		}
	}

	r := model.ScanResult{Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
		{RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Title: "no evidence"},
		{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "trusted"},
		{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "a finding, not a gap"},
	}}}}
	if got := itemGaps(r); len(got) != 1 || got[0] != (gap{"skill s", "COV-000"}) {
		t.Errorf("itemGaps = %+v, want exactly the coverage note, named by its artifact", got)
	}
}
