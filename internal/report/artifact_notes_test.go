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
