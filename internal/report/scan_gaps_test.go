// SPDX-License-Identifier: MIT
package report

import (
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// lowResult is a Low-band result over one clean skill, the base every row below adds one thing to.
func lowResult() model.ScanResult {
	return model.ScanResult{Root: "/x/.claude", ToolVersion: "test", Overall: 100, OverallEffective: 100,
		Env:       model.EnvSummary{Skills: 1},
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Path: "/x/.claude/skills/s", Score: 100, ScoreEffective: 100}},
		Notes:     []model.Finding{}}
}

func withNote(n model.Finding) model.ScanResult {
	r := lowResult()
	if n.Dimension != 0 {
		panic("a note is dimension 0")
	}
	r.Notes = []model.Finding{n}
	return r
}

func cov(title string) model.Finding {
	return model.Finding{RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic, Title: title,
		Evidence: []model.Evidence{{File: "s", Snippet: "x"}}}
}

// TestScanLevelNotesThatHedge pins which scan-level notes take "looks safe" away. The headline is a
// claim that what Claude Code loads was read; detect and collect file most of their "this was not
// read" notes at scan level as COV-000, and those used to leave the headline alone. They now hedge
// it — all of them except the four that disclose a deliberate skip of something that is not
// loaded, or that was read after all. Notes that are not about reading (the judge's, the gate's,
// trust decisions) never hedge.
func TestScanLevelNotesThatHedge(t *testing.T) {
	sup := lowResult()
	sup.Overall, sup.OverallEffective = 88, 88
	sup.Artifacts[0].Score, sup.Artifacts[0].ScoreEffective = 88, 88
	sup.Artifacts[0].Findings = []model.Finding{{RuleID: "SUP-004", Dimension: 5, Severity: model.SevMedium, Source: model.SrcStatic,
		Title:    "Instructions point into a directory the scan does not read",
		Evidence: []model.Evidence{{File: "SKILL.md", Line: 5, Snippet: "Run node node_modules/dep/setup.js"}}}}

	for _, c := range []struct {
		name  string
		r     model.ScanResult
		hedge bool
	}{
		// What Claude Code loads and this scan did not read.
		{"IO-000", withNote(model.Finding{RuleID: "IO-000", Dimension: 0, Severity: model.SevMedium, Title: "Path unreadable, scan incomplete (partial)",
			Evidence: []model.Evidence{{File: "/x/.claude/settings.local.json"}}}), true},
		{"PARSE-000", withNote(model.Finding{RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
			Title: "Plugin hook file could not be parsed, hooks not audited (partial)", Evidence: []model.Evidence{{File: "/x/hooks.json"}}}), true},
		{"unreadable entries in an artifact", withNote(cov("Entries in this artifact could not be read (incomplete coverage)")), true},
		{"a file over the size cap", withNote(cov("File too large, content scan skipped (incomplete coverage)")), true},
		{"non-regular files in a tree", withNote(cov("Non-regular files not read (incomplete coverage)")), true},
		{"a non-regular single file", withNote(cov("Not a regular file, content scan skipped (incomplete coverage)")), true},
		{"a hook script not followed", withNote(cov("Hook script not followed (incomplete coverage)")), true},
		{"a granted script not followed", withNote(cov("Granted script not followed (incomplete coverage)")), true},
		{"entries resolving outside the root", withNote(cov("Entries resolve outside the scanned root, not read")), true},
		{"unresolvable entries in a load namespace", withNote(cov("Entries in a load namespace could not be resolved (incomplete coverage)")), true},
		{"a managed policy file", withNote(cov("Managed policy instructions present, not scanned")), true},
		{"an artifact pointing into a skipped tree", sup, true},

		// Deliberate skips, disclosed: not loaded, or read after all.
		{"unowned top-level entries", withNote(cov("Unowned entries under the root were not read")), false},
		{"an empty root", withNote(cov("Nothing to audit under this root")), false},
		{"third-party / VCS trees", withNote(cov("Third-party / VCS trees not read (incomplete coverage)")), false},
		{"a hook script read as part of its plugin", withNote(cov("Hook script attributed to its plugin, not to the hook (partial)")), false},

		// Not about reading at all.
		{"LLM-000", withNote(model.Finding{RuleID: "LLM-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM, Title: "LLM judge did not run"}), false},
		{"LLM-002", withNote(model.Finding{RuleID: "LLM-002", Dimension: 0, Severity: model.SevMedium, Source: model.SrcLLM, Title: "LLM judge endpoint is not local"}), false},
		{"LLM-005", withNote(model.Finding{RuleID: "LLM-005", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM, Title: "Ungrounded judge findings dropped"}), false},
		{"GATE-001", withNote(model.Finding{RuleID: "GATE-001", Dimension: 0, Severity: model.SevMedium, Title: "Load-time gate registered but not runnable"}), false},
		{"REP-GOOD", withNote(model.Finding{RuleID: "REP-GOOD", Dimension: 0, Severity: model.SevHigh, Title: "Findings suppressed: known-trusted artifact",
			Evidence: []model.Evidence{{File: "superpowers (6.3.0)"}}}), false},
		{"IGN-000", withNote(model.Finding{RuleID: "IGN-000", Dimension: 0, Severity: model.SevMedium, Title: "Findings suppressed by baseline"}), false},
	} {
		for _, h := range humanReports(t, c.r) {
			s := summaryOf(h.name, h.body)
			if s == "" {
				t.Fatalf("%s/%s: no summary found in:\n%s", c.name, h.name, h.body)
			}
			if safe := strings.Contains(s, "looks safe"); safe == c.hedge {
				t.Errorf("%s/%s: summary says looks safe = %v, want %v:\n%s", c.name, h.name, safe, !c.hedge, s)
			}
			if c.hedge && !strings.Contains(s, "coverage is incomplete") {
				t.Errorf("%s/%s: summary must say coverage is incomplete:\n%s", c.name, h.name, s)
			}
		}
	}
}

// checkedOf is the Checked line of the terminal summary: the second line of the Summary block.
func checkedOf(t *testing.T, r model.ScanResult) string {
	t.Helper()
	for _, h := range humanReports(t, r) {
		if h.name != "terminal" {
			continue
		}
		lines := strings.Split(strings.TrimSpace(summaryOf(h.name, h.body)), "\n")
		if len(lines) < 2 {
			t.Fatalf("summary has no Checked line:\n%s", h.body)
		}
		return strings.TrimSpace(lines[1])
	}
	t.Fatal("no terminal report")
	return ""
}

// TestCheckedLineDerivesFromWhatWasScanned pins the Checked line. The inventory counts the
// collectors' surfaces; when it counts nothing, the line counts the scanned artifacts instead —
// one fixed noun per kind, in a fixed order — so it can no longer say "Nothing was found to check"
// above a finding on the file it checked. A non-empty inventory keeps the line it always had.
// Named as not fully checked: an artifact's own coverage note (P-013), then a scan-level IO-000 or
// PARSE-000 — a file found and not read or parsed. Other scan-level notes are not named; they are
// about parts of items the counts already include.
func TestCheckedLineDerivesFromWhatWasScanned(t *testing.T) {
	art := func(kind model.ArtifactKind, name string) model.ArtifactReport {
		return model.ArtifactReport{Kind: kind, Name: name, Path: "/x/" + name, Score: 100, ScoreEffective: 100}
	}
	base := func(env model.EnvSummary, arts ...model.ArtifactReport) model.ScanResult {
		return model.ScanResult{Root: "/x/.claude", ToolVersion: "test", Overall: 100, OverallEffective: 100, Env: env,
			Artifacts: append([]model.ArtifactReport{}, arts...), Notes: []model.Finding{}}
	}
	parseGap := parseFailure("/u/x/.claude/settings.json").Artifacts[0]
	ioNote := model.Finding{RuleID: "IO-000", Dimension: 0, Severity: model.SevMedium, Title: "Path unreadable, scan incomplete (partial)",
		Evidence: []model.Evidence{{File: "/u/x/.claude/settings.local.json", Snippet: "permission denied"}}}

	many := base(model.EnvSummary{},
		art(model.KindDirectory, "tool"), art(model.KindInstruction, "a.md"), art(model.KindQuarantined, "old"),
		art(model.KindPermission, "settings env"), art(model.KindCommand, "deploy.md"), art(model.KindInstruction, "b.sh"))
	withGap := base(model.EnvSummary{}, art(model.KindInstruction, "CLAUDE.md"), parseGap)
	countsAndIO := base(model.EnvSummary{Hooks: 1}, art(model.KindHook, "PreToolUse[Bash]#1"))
	countsAndIO.Notes = []model.Finding{ioNote}
	onlyIO := base(model.EnvSummary{})
	onlyIO.Notes = []model.Finding{ioNote}
	bothGaps := base(model.EnvSummary{}, parseGap)
	bothGaps.Notes = []model.Finding{ioNote}
	covOnly := base(model.EnvSummary{Skills: 1}, art(model.KindSkill, "s"))
	covOnly.Notes = []model.Finding{cov("Entries in this artifact could not be read (incomplete coverage)")}

	for _, c := range []struct {
		name string
		r    model.ScanResult
		want string
	}{
		{"one file", base(model.EnvSummary{}, art(model.KindInstruction, "install.sh")), "Checked 1 file."},
		{"every uncounted kind, in a fixed order", many, "Checked 1 settings block, 1 command, 2 files, 1 directory, 1 quarantined item."},
		{"a gap is not counted as checked", withGap, "Checked 1 file. Not fully checked: …/x/.claude/settings.json [PARSE-000]."},
		{"a scan-level IO-000 is named after the counts", countsAndIO, "Checked 1 hook. Not fully checked: …/x/.claude/settings.local.json [IO-000]."},
		{"a scan-level IO-000 on its own", onlyIO, "Not fully checked: …/x/.claude/settings.local.json [IO-000]."},
		{"artifact gaps before scan-level ones", bothGaps, "Not fully checked: …/x/.claude/settings.json [PARSE-000], …/x/.claude/settings.local.json [IO-000]."},
		{"a scan-level COV-000 is not named", covOnly, "Checked 1 skill."},
		{"a counted inventory keeps its line", base(model.EnvSummary{Skills: 2}, art(model.KindSkill, "a"), art(model.KindSkill, "b"), art(model.KindInstruction, "CLAUDE.md")), "Checked 2 skills."},
		{"nothing at all", base(model.EnvSummary{}), "Nothing was found to check under this root."},
	} {
		if got := checkedOf(t, c.r); got != c.want {
			t.Errorf("%s: Checked line\n  %q\nwant\n  %q", c.name, got, c.want)
		}
	}
}
