// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

func TestHTML_RendersAndEscapes(t *testing.T) {
	r := model.ScanResult{
		Root: "/x/.claude", ScannedAt: 1784179023, ToolVersion: "test", Overall: 69,
		Env: model.EnvSummary{Skills: 3, MCPServers: 1},
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "evil", Findings: []model.Finding{
				{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "curl piped to shell",
					Why: "remote code execution", Source: model.SrcStatic,
					// A snippet with HTML metacharacters must be escaped, not injected.
					Evidence: []model.Evidence{{File: "run.sh", Line: 3, Snippet: "<script>alert(1)</script>"}}},
			},
		}},
		Hygiene: []model.CleanItem{{Kind: "context_bloat", Targets: []string{"a", "b"}, ReclaimTokens: 42, Detail: "verbose description"}},
		Notes:   []model.Finding{},
	}
	var buf bytes.Buffer
	if err := HTML(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "AgentGuard") || !strings.Contains(out, "EXEC-001") {
		t.Error("report missing core content")
	}
	if !strings.Contains(out, "69") || !strings.Contains(out, "Elevated") {
		t.Error("gauge score/level missing")
	}
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("evidence snippet was NOT HTML-escaped (XSS in a self-contained report)")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped snippet")
	}
	if !strings.Contains(out, "42") { // reclaim tokens surfaced
		t.Error("hygiene reclaim tokens missing")
	}
}

// TestHTML_NotesCarryTheirRationale: the HTML notes block used to print "[REP-GOOD] Findings
// suppressed" and nothing else — the entry, the count and the recorded review were all in Why
// and Why was not rendered. A report handed to someone else must answer "why was this trusted?"
// without a terminal and --verbose.
func TestHTML_NotesCarryTheirRationale(t *testing.T) {
	r := model.ScanResult{Overall: 100, Notes: []model.Finding{{
		RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "Findings suppressed: known-trusted artifact",
		Why: "matches reputation allowlist entry superpowers 6.3.0 (publisher obra via claude-plugins-official); 18 finding(s) suppressed. Reviewed 2026-09-03: <b>not markup</b>",
	}}}
	var buf bytes.Buffer
	if err := HTML(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "publisher obra via claude-plugins-official") || !strings.Contains(out, "Reviewed 2026-09-03") {
		t.Error("HTML notes must render the note's Why (entry, publisher, review)")
	}
	if strings.Contains(out, "<b>not markup</b>") || !strings.Contains(out, "&lt;b&gt;") {
		t.Error("note Why must be HTML-escaped")
	}
}

// TestHTML_SummaryAndNavigation: the HTML opens with the plain verdict and a "what to look at"
// list that links to the finding cards; artifacts read as words; trust decisions and coverage
// gaps are separate sections; the rule id is still on every card.
func TestHTML_SummaryAndNavigation(t *testing.T) {
	r := model.ScanResult{
		Overall: 69, Env: model.EnvSummary{Plugins: 1, Hooks: 2},
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindPlugin, Name: "figma@claude-plugins-official (2.2.107)", Findings: []model.Finding{
				{RuleID: "EXEC-004", Dimension: 4, Severity: model.SevMedium, Title: "Subprocess / shell invocation", Why: "Spawns a subprocess.", Source: model.SrcStatic,
					Evidence: []model.Evidence{{File: "plugins/cache/claude-plugins-official/figma/2.2.107/scripts/x.py", Line: 33}}},
			},
		}},
		Notes: []model.Finding{
			{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "Findings suppressed: known-trusted artifact", Why: "matches reputation allowlist entry superpowers 6.3.0", Evidence: []model.Evidence{{File: "superpowers (6.3.0)"}}},
			{RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Title: "Unowned entries were not read", Why: "Not read: history.jsonl, shell-snapshots. No collector owns these."},
		},
	}
	var buf bytes.Buffer
	if err := HTML(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"There are problems you should fix before relying on this setup. 1 finding needs a look (medium or above).",
		"Checked 1 plugin, 2 hooks.",
		`id="actions"`, `href="#f-0"`, `id="f-0"`,
		"figma plugin, from claude-plugins-official (2.2.107)",
		"In plain terms: <b>can run programs on your machine</b>",
		`<span class="rid">EXEC-004</span>`,
		`title="plugins/cache/claude-plugins-official/figma/2.2.107/scripts/x.py">figma › scripts/x.py</abbr>:33</div>`,
		`<div class="what">Subprocess / shell invocation</div>`,
		`id="trusted"`, "superpowers 6.3.0",
		`id="notchecked"`, `<span class="chip">history.jsonl</span>`, `<span class="chip">shell-snapshots</span>`, "No collector owns these.",
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestHTML_EmptyIsClean(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, model.ScanResult{Root: "/x", Overall: 100}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No risk findings") {
		t.Error("empty scan should render the clean state")
	}
}

// TestHTML_JudgeSectionPlaceholder: with --llm the judge section exists even when it has no
// leads, carrying the one-line account; without --llm the page does not mention the judge at all.
func TestHTML_JudgeSectionPlaceholder(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100,
		Judge: &model.JudgeSummary{Ran: true, Artifacts: 3, Calls: 6}}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, `id="judge"`) || !strings.Contains(s, "had nothing to add") || !strings.Contains(s, `href="#judge">LLM judge</a>`) {
		t.Errorf("judge placeholder missing:\n%s", s)
	}
	out.Reset()
	r.Judge = nil
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `id="judge"`) || strings.Contains(out.String(), "LLM judge") {
		t.Error("without --llm the report must not mention the judge")
	}
}

// TestHTML_ReadsForNonSpecialists: each finding carries a fixed "what to do", evidence is folded
// behind a summary that still names the first place, cleanup items lead with a plain label and
// fold their codes, and an allowlist decision shows the decision with the reviewer's notes folded.
func TestHTML_ReadsForNonSpecialists(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 80, OverallEffective: 80,
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Path: "/r/skills/s", Score: 80, Findings: []model.Finding{{
			RuleID: "EXEC-004", Dimension: 4, Severity: model.SevMedium, Source: model.SrcStatic, Title: "Subprocess",
			Why: "spawns", Evidence: []model.Evidence{{File: "/r/skills/s/run.py", Line: 3, Snippet: "subprocess.run(x)"}, {File: "/r/skills/s/b.py", Line: 9}},
		}}}},
		Hygiene: []model.CleanItem{{ID: "B-1", Kind: "context_bloat", Tier: "C", Detail: "Description is ~251 tokens", Targets: []string{"s"}}},
		Notes: []model.Finding{{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
			Title:    "Findings suppressed: known-trusted artifact",
			Why:      "skill:docx matches reputation allowlist entry docx; 10 finding(s) suppressed, highest severity=medium. Reviewed 2026-09-04: All 10 findings read in source.",
			Evidence: []model.Evidence{{File: "docx"}}}},
	}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{
		"What to do: Look at what it runs.",
		"<summary>Where: <abbr", "and 2 places in all</summary>",
		"<b>Long description</b> — Description is ~251 tokens",
		"<summary>details</summary><span class=\"k\">context_bloat/C</span>",
		"skill:docx matches reputation allowlist entry docx; 10 finding(s) suppressed, highest severity=medium.</div>",
		"<summary>Why it is trusted — the reviewer's notes</summary><div class=\"why\">Reviewed 2026-09-04: All 10 findings read in source.</div>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

// TestHTML_DownloadsSection: the HTML carries the same Downloads section, with a TOC entry, and
// leaves it out entirely when no inbox was scanned.
func TestHTML_DownloadsSection(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100,
		Inbox: &model.InboxReport{Dir: "/home/u/Downloads", Skipped: 3, Items: []model.InboxItem{
			{Name: "bad-skill", Kind: "skill", Overall: 35, Findings: []model.Finding{{RuleID: "EXEC-001", Severity: model.SevHigh, Title: "curl piped to shell", Evidence: []model.Evidence{{File: "setup.sh", Line: 1}}}}},
		}}}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{`href="#inbox">Downloads <b>1</b>`, `id="inbox"`, "35/100 · High", "Worst: [EXEC-001] curl piped to shell", "What to do: Do not install this.", "3 other entries were not agent-shaped"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	out.Reset()
	r.Inbox = nil
	_ = HTML(&out, r)
	if strings.Contains(out.String(), `id="inbox"`) {
		t.Error("no inbox: section must be absent")
	}
}

// TestHTML_WhereTheScanLooked: the same list in the HTML scan details.
func TestHTML_WhereTheScanLooked(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100, Locations: []model.Location{
		{Name: "Config root", Path: "/r", Status: model.LocRead}, {Name: "Downloads", Path: "/h/Downloads", Status: model.LocAbsent}}}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Where the scan looked") || !strings.Contains(out.String(), "Downloads · <span class=\"loc\">/h/Downloads</span> · absent") {
		t.Errorf("locations missing:\n%s", out.String())
	}
}

// TestHTML_TriageLabelIsSanitized: the HTML report is built from sanitizeResult because
// html/template escapes markup but passes bidi and zero-width characters through. The triage
// label was the one attacker-influenced string that copy never touched — a model-written reason
// carrying U+202E reached the page as is. JSON, for machines, keeps the bytes.
func TestHTML_TriageLabelIsSanitized(t *testing.T) {
	const reason = "doc ‮gnp.sh example"
	r := model.ScanResult{Overall: 60, OverallEffective: 60, Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "s",
		Findings: []model.Finding{{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic,
			Title: "curl|bash", Why: "rce", Evidence: []model.Evidence{{File: "run.sh", Line: 1, Snippet: "curl x | bash"}}}},
		Advisory: []model.AdvisoryLabel{{RuleID: "EXEC-001", Label: model.LabelBenign, Reason: reason}},
	}}}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out.String(), '‮') {
		t.Error("a bidi override in a triage reason survived into the HTML report")
	}
	if !strings.Contains(out.String(), "doc �gnp.sh example") {
		t.Errorf("the cleaned character must leave a visible mark in the triage line:\n%s", out.String())
	}
	// Reverse: the machine format carries the original bytes.
	js, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), reason) {
		t.Error("JSON must carry the triage reason's bytes unsanitised")
	}
}
