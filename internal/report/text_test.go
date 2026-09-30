// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

func sampleResult() model.ScanResult {
	return model.ScanResult{
		Root: "/x/.claude", Overall: 69,
		Env: model.EnvSummary{Skills: 2, Hooks: 1},
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
				{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "curl piped to shell", Why: "RCE", Source: model.SrcStatic,
					Evidence: []model.Evidence{{File: "run.sh", Line: 3, Snippet: "curl x | bash"}}},
				{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "curl piped to shell", Why: "RCE", Source: model.SrcStatic,
					Evidence: []model.Evidence{{File: "b.sh", Line: 9, Snippet: "curl y | bash"}}},
				{RuleID: "BD-001", Dimension: 7, Severity: model.SevMedium, Title: "env-triggered", Why: "backdoor", Source: model.SrcStatic, Advisory: true,
					Evidence: []model.Evidence{{File: "run.sh", Line: 5, Snippet: "if $(date)"}}},
				{RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Title: "cov note", Source: model.SrcStatic}, // must NOT appear as a risk row
			},
		}},
		Hygiene: []model.CleanItem{{Kind: "context_bloat", Targets: []string{"a"}, ReclaimTokens: 42, Detail: "verbose"}},
		Notes:   []model.Finding{{RuleID: "IO-000", Title: "read failed"}},
	}
}

func TestText_AggregatesAndLabels(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult())
	out := buf.String()
	if !strings.Contains(out, "Risk score 69/100 (Elevated)") {
		t.Error("score/level line missing")
	}
	if !strings.Contains(out, "×2") {
		t.Error("EXEC-001 hit twice should aggregate to ×2")
	}
	if !strings.Contains(out, "advisory: not confirmed") {
		t.Error("advisory (dim7) label missing")
	}
	if strings.Contains(out, "COV-000") {
		t.Error("dim0 coverage note must not appear as a risk finding row")
	}
	// Default mode folds the notes to one line — but the line still names them, so a reader
	// knows a gap exists without being handed the rationale for it.
	if !strings.Contains(out, "coverage note(s)") || !strings.Contains(out, "IO-000") {
		t.Error("collapsed coverage-note line missing")
	}
}

// TestText_FindingsSurviveBothModes is the line the concise view is not allowed to cross.
// Collapsing a COVERAGE note loses prose a reader can ask for; collapsing a FINDING loses the
// only thing that would have made them ask. Every risk row must render identically in both
// modes, so this asserts on the rows themselves rather than on a count that could be right
// for the wrong reason.
func TestText_FindingsSurviveBothModes(t *testing.T) {
	rows := []string{"EXEC-001", "×2", "BD-001", "advisory: not confirmed", "run.sh:3", "b.sh:9"}
	for _, mode := range []struct {
		name string
		fn   func(*bytes.Buffer, model.ScanResult)
	}{
		{"default", func(b *bytes.Buffer, r model.ScanResult) { Text(b, r) }},
		{"verbose", func(b *bytes.Buffer, r model.ScanResult) { TextVerbose(b, r) }},
	} {
		var buf bytes.Buffer
		mode.fn(&buf, sampleResult())
		for _, want := range rows {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("%s mode dropped %q — findings are never collapsed, only dim-0 notes are", mode.name, want)
			}
		}
	}
}

// TestTextVerbose_KeepsNoteRationale pins the other half: --verbose is what a reader reaches
// for when the question is "what did it NOT read?", so the note's Why must be there in full,
// and must not be there by default (that prose is the bulk this split exists to move).
func TestTextVerbose_KeepsNoteRationale(t *testing.T) {
	r := sampleResult()
	r.Notes = []model.Finding{{
		RuleID: "COV-000", Severity: model.SevLow, Title: "skipped a tree",
		Why: "the-long-rationale-nobody-rereads",
	}}
	var full, brief bytes.Buffer
	TextVerbose(&full, r)
	Text(&brief, r)

	if !strings.Contains(full.String(), "⚠ Scan warnings") || !strings.Contains(full.String(), "the-long-rationale-nobody-rereads") {
		t.Error("verbose must print the note block with its full rationale")
	}
	if strings.Contains(brief.String(), "the-long-rationale-nobody-rereads") {
		t.Error("default mode should not print note rationale — that is the prose --verbose exists for")
	}
}

// TestText_CollapsedNoteCarriesHighestSeverity is invariant #5 surviving the collapse. A
// suppression note that hid a CRITICAL must not read as a low-severity footnote just because
// the default view got shorter — the count alone would do exactly that.
func TestText_CollapsedNoteCarriesHighestSeverity(t *testing.T) {
	r := sampleResult()
	r.Notes = []model.Finding{
		{RuleID: "COV-000", Severity: model.SevLow, Title: "minor gap"},
		{RuleID: "IGN-000", Severity: model.SevCritical, Title: "baseline suppressed a critical"},
	}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "highest critical") {
		t.Errorf("collapsed note line must carry the highest severity among notes, got:\n%s", out)
	}
	if !strings.Contains(out, "IGN-000") {
		t.Error("collapsed note line must name the rule ids, or a suppression is unfindable")
	}
}

// TestText_SuppressionNoteShowsItsReviewByDefault: a REP-GOOD (or IGN-000) note is a decision
// that changed the number at the top of the report, not a coverage gap, and it used to fold
// into "coverage is incomplete" with its rationale hidden behind --verbose. Default view now
// prints each suppression with its entry and the start of the recorded review; coverage notes
// still fold. A reader of the plain report can tell "trusted" from "clean".
func TestText_SuppressionNoteShowsItsReviewByDefault(t *testing.T) {
	r := sampleResult()
	r.Notes = []model.Finding{
		{RuleID: "COV-000", Severity: model.SevLow, Title: "skipped a tree", Why: "the-long-rationale-nobody-rereads"},
		{RuleID: "REP-GOOD", Severity: model.SevHigh, Title: "Findings suppressed: known-trusted artifact",
			Why:      "plugin:superpowers matches reputation allowlist entry superpowers 6.3.0 (publisher obra via claude-plugins-official), reviewed from https://github.com/obra/superpowers.git@b36e082; 18 finding(s) suppressed, highest severity=high. Reviewed 2026-09-03: All 18 findings read in source. EXFIL-001 x5: brainstorm-server tests talk to localhost. " + strings.Repeat("More detail. ", 40),
			Evidence: []model.Evidence{{File: "superpowers@claude-plugins-official (6.3.0)"}}},
	}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()
	for _, want := range []string{
		"trust decision(s) changed the score",
		"[REP-GOOD]", "highest high",
		"publisher obra via claude-plugins-official",           // who was trusted
		"Reviewed 2026-09-03: All 18 findings read in source.", // the review, first sentence
		"coverage note(s), highest low [COV-000]",              // coverage still folds
	} {
		if !strings.Contains(out, want) {
			t.Errorf("default view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "the-long-rationale-nobody-rereads") {
		t.Error("coverage rationale must stay folded in the default view")
	}
	if strings.Count(out, "More detail.") > 20 {
		t.Error("the review must be clipped in the default view; --verbose prints all of it")
	}
	if !strings.Contains(out, " …") {
		t.Error("a clipped review must say it was clipped")
	}
}

func TestClipSentences(t *testing.T) {
	long := "First sentence. Second sentence. " + strings.Repeat("x", 100)
	got := clipSentences(long, 40)
	if got != "First sentence. Second sentence. …" {
		t.Errorf("clip should end at a sentence boundary; got %q", got)
	}
	if got := clipSentences("short", 40); got != "short" {
		t.Errorf("short input untouched; got %q", got)
	}
	if got := clipSentences(strings.Repeat("é", 50), 10); []rune(got)[0] != 'é' || !strings.HasSuffix(got, "…") {
		t.Errorf("multi-byte input must clip on rune boundaries; got %q", got)
	}
}

// TestText_Verdict covers the header line's three states. It is derived from the findings
// below it, so the only failure worth testing for is DISAGREEMENT with that list.
func TestText_Verdict(t *testing.T) {
	base := func(fs ...model.Finding) model.ScanResult {
		return model.ScanResult{Overall: 80, Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Findings: fs}}}
	}
	low := model.Finding{RuleID: "RES-001", Dimension: 8, Severity: model.SevLow, Title: "t", Source: model.SrcStatic}
	high := model.Finding{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Title: "t", Source: model.SrcStatic}

	var clean, onlyLow, mixed bytes.Buffer
	Text(&clean, base())
	Text(&onlyLow, base(low))
	Text(&mixed, base(low, high))

	if strings.Contains(clean.String(), "→ ") {
		t.Error("a clean scan already says ✅; a second way of saying nothing is one too many")
	}
	if !strings.Contains(onlyLow.String(), "Nothing at medium or above") {
		t.Errorf("low-only scan should say so, got:\n%s", onlyLow.String())
	}
	// The worst finding drives the line, not the first one collected.
	if !strings.Contains(mixed.String(), "start with [EXEC-001]") || !strings.Contains(mixed.String(), "1 of 2") {
		t.Errorf("verdict must point at the worst finding, got:\n%s", mixed.String())
	}
}

func TestText_Clean(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, model.ScanResult{Overall: 100})
	if !strings.Contains(buf.String(), "No risk findings") {
		t.Error("clean env should say no findings")
	}
}

func TestHygieneReport(t *testing.T) {
	var buf bytes.Buffer
	Hygiene(&buf, []model.CleanItem{{Kind: "context_bloat", Targets: []string{"a", "b"}, ReclaimTokens: 100, Detail: "verbose"}})
	out := buf.String()
	if !strings.Contains(out, "~100 tokens") {
		t.Error("hygiene should quantify reclaimable tokens")
	}
	var empty bytes.Buffer
	Hygiene(&empty, nil)
	if !strings.Contains(empty.String(), "No junk") {
		t.Error("empty hygiene clean state missing")
	}
}

func TestHasAtLeast(t *testing.T) {
	r := sampleResult()
	if !HasAtLeast(r, model.SevHigh) {
		t.Error("should have a high finding")
	}
	if HasAtLeast(r, model.SevCritical) {
		t.Error("no critical present")
	}
	// dim0 notes must not count.
	only0 := model.ScanResult{Artifacts: []model.ArtifactReport{{Findings: []model.Finding{{Dimension: 0, Severity: model.SevCritical}}}}}
	if HasAtLeast(only0, model.SevLow) {
		t.Error("dim0 finding must not trip the gate")
	}
}

// TestText_SanitizesControlChars: a finding's Why (with --llm this is model-authored, fed
// from attacker-controlled skill content) must not carry raw terminal escapes into output.
func TestText_SanitizesControlChars(t *testing.T) {
	r := model.ScanResult{
		Overall: 50,
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "eviln\x1b[2Jame", Findings: []model.Finding{{
				RuleID: "LLM-001", Dimension: 10, Severity: model.SevHigh, Source: model.SrcLLM,
				Title: "Intent mismatch", Why: "benign\x1b]0;pwned\x07 summary", Advisory: true,
				Evidence: []model.Evidence{{File: "run\x1b[31m.sh", Line: 1, Snippet: "x"}},
			}},
		}},
		Notes: []model.Finding{{RuleID: "LLM-000", Title: "note\x1bbad", Why: "why\x9bctrl"}},
	}
	var buf bytes.Buffer
	Text(&buf, r)
	if bytes.ContainsRune(buf.Bytes(), 0x1b) || bytes.ContainsRune(buf.Bytes(), 0x07) || bytes.ContainsRune(buf.Bytes(), 0x9b) {
		t.Errorf("terminal escape survived into report output: %q", buf.String())
	}
}

// TestTriageLabelRendersOnGroup: an artifact's advisory label flows through Aggregate onto
// the matching finding group and appears in the terminal report (display-only; the finding
// still renders with its real severity).
func TestTriageLabelRendersOnGroup(t *testing.T) {
	r := model.ScanResult{
		Overall: 60,
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "s",
			Findings: []model.Finding{{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "curl|bash", Why: "rce", Evidence: []model.Evidence{{File: "run.sh", Line: 1}}}},
			Advisory: []model.AdvisoryLabel{{RuleID: "EXEC-001", Label: "likely-benign", Reason: "doc example"}},
		}},
	}
	groups := Aggregate(r)
	if len(groups) != 1 || groups[0].Triage != "likely-benign — doc example" {
		t.Fatalf("triage label not threaded onto group: %+v", groups)
	}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "triage") || !strings.Contains(out, "likely-benign") {
		t.Errorf("triage label not rendered: %q", out)
	}
	// The finding itself must still show its real severity (label doesn't hide it).
	if !strings.Contains(out, "high") {
		t.Errorf("labeled finding lost its severity in output: %q", out)
	}
}

// TestText_WholeArtifactEvidenceShowsSnippet: with Line 0 the file name identifies nothing
// — two permission grants in one settings.json would render identically — so the (already
// redacted) snippet is what makes the row actionable.
func TestText_WholeArtifactEvidenceShowsSnippet(t *testing.T) {
	r := model.ScanResult{Artifacts: []model.ArtifactReport{{
		Kind: model.KindPermission, Name: "permissions", Findings: []model.Finding{
			{RuleID: "PERM-006", Dimension: 2, Severity: model.SevMedium, Title: "escapable", Why: "w", Source: model.SrcPermission,
				Evidence: []model.Evidence{{File: "settings.json", Line: 0, Snippet: "Bash(git *)"}}},
		},
	}}}
	var buf bytes.Buffer
	Text(&buf, r)
	if !strings.Contains(buf.String(), "Bash(git *)") {
		t.Errorf("whole-artifact evidence must show which entry triggered it:\n%s", buf.String())
	}
}

// TestText_ShowsBothScoresWhenTheyDiffer: spec §9 — showing only the deterministic number
// hides what the judge saw; showing only the effective one implies a reproducibility it does
// not have. They collapse to one line only when they agree.
func TestText_ShowsBothScoresWhenTheyDiffer(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, model.ScanResult{Overall: 83, OverallEffective: 61, Artifacts: []model.ArtifactReport{
		{Kind: model.KindSkill, Name: "s", Score: 83, ScoreEffective: 52},
	}})
	out := buf.String()
	if !strings.Contains(out, "83/100") || !strings.Contains(out, "61/100") {
		t.Errorf("both numbers must appear:\n%s", out)
	}
	if !strings.Contains(out, "does not gate") {
		t.Errorf("the effective line must say it doesn't gate:\n%s", out)
	}

	buf.Reset()
	Text(&buf, model.ScanResult{Overall: 83, OverallEffective: 83, Artifacts: []model.ArtifactReport{
		{Kind: model.KindSkill, Name: "s", Score: 83, ScoreEffective: 83},
	}})
	if strings.Count(buf.String(), "83/100") != 1 {
		t.Errorf("equal numbers should render once, not twice:\n%s", buf.String())
	}
}

// TestText_NamesTheArtifactsTheJudgeMoved: the environment score is averaged and then
// bucket-capped, so it routinely stays put while one artifact drops 30 points. If the report
// only compared the environment numbers, that artifact would be invisible — and this second
// number exists precisely to make it visible.
func TestText_NamesTheArtifactsTheJudgeMoved(t *testing.T) {
	r := model.ScanResult{Overall: 69, OverallEffective: 69, Artifacts: []model.ArtifactReport{
		{Kind: model.KindHook, Name: "PreToolUse[Read]#1", Score: 75, ScoreEffective: 51},
		{Kind: model.KindSkill, Name: "test-runner", Score: 83, ScoreEffective: 52},
		{Kind: model.KindCommand, Name: "deploy", Score: 100, ScoreEffective: 100},
	}}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "75→51") || !strings.Contains(out, "83→52") {
		t.Errorf("per-artifact drops missing even though the env number didn't move:\n%s", out)
	}
	if strings.Contains(out, "deploy") {
		t.Error("an artifact the judge didn't move must not be listed as affected")
	}
	// Worst drop first: the skill lost 31, the hook 24.
	if strings.Index(out, "83→52") > strings.Index(out, "75→51") {
		t.Errorf("affected artifacts should be ordered by drop size:\n%s", out)
	}
}

// TestHasAtLeast_UnaffectedByEffectiveScore: the deterministic gate reads findings, never the
// effective number — the two filter pairs must not drift into each other.
func TestHasAtLeast_UnaffectedByEffectiveScore(t *testing.T) {
	r := model.ScanResult{Overall: 100, OverallEffective: 20, Artifacts: []model.ArtifactReport{{
		Score: 100, ScoreEffective: 20,
		Findings: []model.Finding{{RuleID: "LLM-003", Dimension: 1, Severity: model.SevHigh, Source: model.SrcLLM}},
	}}}
	if HasAtLeast(r, model.SevLow) {
		t.Error("--fail-on must not fire on an LLM finding, however low the effective score")
	}
}

// TestText_SeparatesStaticFromJudge: a reader must be able to tell which findings set the
// score and gate their CI, and which are leads to check. Before this split, the only clue was
// knowing that "LLM-" is a meaningful rule-id prefix.
func TestText_SeparatesStaticFromJudge(t *testing.T) {
	r := model.ScanResult{Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
			{RuleID: "FS-001", Dimension: 9, Severity: model.SevHigh, Title: "ssh key", Why: "w", Source: model.SrcStatic,
				Evidence: []model.Evidence{{File: "SKILL.md", Line: 9}}},
			{RuleID: "LLM-003", Dimension: 1, Severity: model.SevMedium, Title: "injection", Why: "w", Source: model.SrcLLM, Advisory: true,
				Evidence: []model.Evidence{{File: "SKILL.md", Line: 9}}},
		},
	}}}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()

	staticAt, judgeAt := strings.Index(out, "Findings · STATIC"), strings.Index(out, "Findings · LLM JUDGE")
	if staticAt < 0 || judgeAt < 0 {
		t.Fatalf("both sections must be labelled:\n%s", out)
	}
	if staticAt > judgeAt {
		t.Error("the deterministic section should come first — it is the one that gates")
	}
	// Each heading must state the consequence, not just the source.
	if !strings.Contains(out[staticAt:judgeAt], "--fail-on") || !strings.Contains(out[judgeAt:], "never set") {
		t.Errorf("headings should say what each half does to the score/gate:\n%s", out)
	}
	// And the findings must land in the right halves.
	if !strings.Contains(out[staticAt:judgeAt], "FS-001") || strings.Contains(out[staticAt:judgeAt], "LLM-003") {
		t.Errorf("findings landed in the wrong section:\n%s", out)
	}
}

// TestText_NoJudgeSectionWhenTheJudgeDidntRun: an empty "LLM judge" heading on every static
// scan would be noise suggesting something is missing.
func TestText_NoJudgeSectionWhenTheJudgeDidntRun(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult())
	if strings.Contains(buf.String(), "LLM JUDGE") {
		t.Error("static-only scan should not print an empty judge section")
	}
}

// A wide group must say it is wide. Both halves are asserted because either alone still leaves
// the report lying: the count without the remainder line reads as "×56, and here are all three of
// them", and the remainder line without the count makes the reader do the arithmetic. The numbers
// here mirror what a real ~/.claude produced (a group spread far wider than the 3-line budget).
func TestText_WideGroupNamesItsBreadthAndItsRemainder(t *testing.T) {
	var fs []model.Finding
	for i := 0; i < 8; i++ {
		fs = append(fs, model.Finding{
			RuleID: "EXFIL-001", Dimension: 3, Severity: model.SevHigh,
			Title: "credential read then sent", Why: "exfiltration", Source: model.SrcStatic,
			Evidence: []model.Evidence{{File: fmt.Sprintf("skills/s%d/SKILL.md", i), Line: 12}},
		})
	}
	var buf bytes.Buffer
	Text(&buf, model.ScanResult{Root: "/x", Overall: 40,
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Findings: fs}}})
	out := buf.String()
	if !strings.Contains(out, "×8 in 8 files") {
		t.Errorf("breadth missing from the count line:\n%s", out)
	}
	// 8 affected, 3 printed.
	if !strings.Contains(out, "and 5 more file(s)") {
		t.Errorf("remainder line missing — the 3 printed paths read as the complete set:\n%s", out)
	}
}

// The inverse: a group whose hits are all in ONE file must not gain either phrase. " in 1 files"
// is noise, and a remainder line claiming zero would be a false alarm about scope.
func TestText_DeepButNarrowGroupStaysQuiet(t *testing.T) {
	var fs []model.Finding
	for i := 0; i < 4; i++ {
		fs = append(fs, model.Finding{
			RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh,
			Title: "curl piped to shell", Why: "RCE", Source: model.SrcStatic,
			Evidence: []model.Evidence{{File: "run.sh", Line: i + 1}},
		})
	}
	var buf bytes.Buffer
	Text(&buf, model.ScanResult{Root: "/x", Overall: 40,
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Findings: fs}}})
	out := buf.String()
	if !strings.Contains(out, "×4") {
		t.Errorf("count missing:\n%s", out)
	}
	if strings.Contains(out, " files") || strings.Contains(out, "more file(s)") {
		t.Errorf("a single-file group must not claim breadth:\n%s", out)
	}
}

// Breadth counts every evidence leg, not only the displayed one. A structural finding names its
// legs across files (read here, encoded there, sent there) and all of them are affected — if only
// the first leg counted, a 1-finding/3-file group would report breadth 1 and print nothing.
func TestAggregate_FilesCountsEveryEvidenceLeg(t *testing.T) {
	gs := Aggregate(model.ScanResult{Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "s", Findings: []model.Finding{{
			RuleID: "EXFIL-003", Dimension: 3, Severity: model.SevHigh, Source: model.SrcStatic,
			Evidence: []model.Evidence{
				{File: "read.sh", Line: 1}, {File: "enc.py", Line: 2}, {File: "send.sh", Line: 3},
			},
		}},
	}}})
	if len(gs) != 1 {
		t.Fatalf("want 1 group, got %d", len(gs))
	}
	if gs[0].Files != 3 {
		t.Errorf("Files: want 3 (one per leg), got %d", gs[0].Files)
	}
	if n := gs[0].MoreFiles(); n != 0 {
		t.Errorf("all 3 legs are printed, so nothing remains; MoreFiles=%d", n)
	}
}

// TestText_LoneNoteWithASnippetStillShowsIt: the note that resolves ONE hook reference to ONE
// path carries that path in its snippet, and a "single instances are already named on the title
// line" shortcut dropped it from the report entirely. True for a note whose File says everything
// ("File too large … <path>"); false the moment the answer is in the snippet.
// Runs against the VERBOSE view: after the two-mode merge, the default view collapses all
// dimension-0 notes into one line that carries their count, highest severity and rule ids and
// points at --verbose (that contract has its own tests below). The payload assertions here —
// a lone note's snippet must survive, a fully-named note must not repeat itself — belong to
// the view that renders notes at all; JSON and HTML always carry everything either way.
func TestTextVerbose_LoneNoteWithASnippetStillShowsIt(t *testing.T) {
	const resolved = "plugins/cache/mk/pl/1.0.0/scripts/runner.js"
	var buf bytes.Buffer
	TextVerbose(&buf, model.ScanResult{Root: "/x", Overall: 100, Notes: []model.Finding{
		{RuleID: "COV-000", Severity: model.SevLow, Title: "Hook script attributed to its plugin",
			Why:      "resolved inside the plugin tree",
			Evidence: []model.Evidence{{File: "Stop[*]#1", Snippet: "$_R/scripts/runner.js → " + resolved}}},
		// A note whose File is the whole answer must NOT gain a second line repeating it.
		{RuleID: "COV-000", Severity: model.SevLow, Title: "File too large, content scan skipped",
			Why:      "above the 1 MiB cap",
			Evidence: []model.Evidence{{File: "big/fixture.json"}}},
	}})
	out := buf.String()
	if !strings.Contains(out, resolved) {
		t.Errorf("the resolved path is the note's whole payload and it was dropped:\n%s", out)
	}
	if strings.Count(out, "big/fixture.json") != 1 {
		t.Errorf("a note fully named on its title line must not repeat itself:\n%s", out)
	}
}

// TestText_JudgeLineStates: the summary tells the three judge states apart — not requested
// (silent), requested but not run (with the reason), ran with nothing to add — because a judge
// section that appears only when there are findings made the last two indistinguishable.
func TestText_JudgeLineStates(t *testing.T) {
	base := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100}
	var out bytes.Buffer
	Text(&out, base)
	if strings.Contains(out.String(), "LLM judge") {
		t.Errorf("no --llm: summary must not mention the judge:\n%s", out.String())
	}
	out.Reset()
	r := base
	r.Judge = &model.JudgeSummary{Artifacts: 4, Reason: "config llm.enabled is false"}
	Text(&out, r)
	if !strings.Contains(out.String(), "LLM judge did not run: config llm.enabled is false") {
		t.Errorf("not-run state missing:\n%s", out.String())
	}
	out.Reset()
	r.Judge = &model.JudgeSummary{Ran: true, Artifacts: 4, Calls: 9, Skipped: 1}
	Text(&out, r)
	if !strings.Contains(out.String(), "LLM judge ran over 4 artifact(s) in 9 call(s) (0 failed, 1 skipped) and had nothing to add.") {
		t.Errorf("ran-clean state missing:\n%s", out.String())
	}
}

// TestText_WhatToDoLine: the terminal report carries the same fixed per-dimension action as the HTML.
func TestText_WhatToDoLine(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 88, OverallEffective: 88,
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Path: "/r/skills/s", Score: 88, Findings: []model.Finding{{
			RuleID: "FS-002", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic, Title: "Reads AWS credentials", Why: "touches",
			Evidence: []model.Evidence{{File: "/r/skills/s/a.sh", Line: 4}}}}}}}
	var out bytes.Buffer
	Text(&out, r)
	if !strings.Contains(out.String(), "what to do: Ask whether this tool needs that access at all.") {
		t.Errorf("missing what-to-do line:\n%s", out.String())
	}
}

// TestText_DownloadsSection: the inbox renders as its own section with per-item scores and advice,
// says what it did not read, and is absent when no inbox was scanned.
func TestText_DownloadsSection(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100}
	var out bytes.Buffer
	Text(&out, r)
	if strings.Contains(out.String(), "Downloads") {
		t.Errorf("no inbox: section must be absent:\n%s", out.String())
	}
	r.Inbox = &model.InboxReport{Dir: "/home/u/Downloads", Skipped: 7, Items: []model.InboxItem{
		{Name: "bad-skill", Kind: "skill", Overall: 35, Findings: []model.Finding{{RuleID: "EXEC-001", Severity: model.SevHigh, Title: "curl piped to shell"}}},
		{Name: "clean.zip", Kind: "archive", Archive: true, Overall: 100},
	}}
	out.Reset()
	Text(&out, r)
	s := out.String()
	for _, want := range []string{
		"Downloads · 2 agent-shaped item(s) under /home/u/Downloads — not installed, checked on their own, not part of the score",
		"🔴  35/100 High     bad-skill (skill)", "worst: [EXEC-001] curl piped to shell · 1 finding(s)", "→ Do not install this.",
		"🟢 100/100 Low      clean.zip (zip)", "(7 other entries were not agent-shaped and were not read)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

// TestText_WhereTheScanLooked: the scan-details footer lists each place with its status.
func TestText_WhereTheScanLooked(t *testing.T) {
	r := model.ScanResult{Root: "/r", Overall: 100, OverallEffective: 100, Locations: []model.Location{
		{Name: "Config root", Path: "/r", Status: model.LocRead},
		{Name: "Claude Desktop store", Path: "/h/Library/Application Support/Claude/local-agent-mode-sessions", Status: model.LocAbsent},
		{Name: "Downloads", Path: "off", Status: model.LocOff},
	}}
	var out bytes.Buffer
	Text(&out, r)
	s := out.String()
	for _, want := range []string{"Where the scan looked:", "Config root            read    /r", "Claude Desktop store   absent  /h/Library", "Downloads              off"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

// TestText_BidiInNamesIsNeutralised pins name sanitising. A file literally named `pay<U+202E>gnp.sh`
// rendered as `pay2:hs.png` — a shell script with a high finding presenting itself as an image.
// Both human renderers must neutralise it with a visible mark; JSON must NOT (machines diff it).
func TestText_BidiInNamesIsNeutralised(t *testing.T) {
	evil := "pay‮gnp.sh"
	r := model.ScanResult{
		Overall: 40,
		Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "sk​ill", Path: "/x/skill", Findings: []model.Finding{{
				RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "curl piped to shell", Why: "runs remote code",
				Evidence: []model.Evidence{{File: "skill/" + evil, Line: 2, Snippet: "curl x | bash"}},
			}},
		}},
	}
	var tb bytes.Buffer
	Text(&tb, r)
	if strings.ContainsRune(tb.String(), '‮') || strings.ContainsRune(tb.String(), '​') {
		t.Errorf("bidi/zero-width character survived into the terminal report: %q", tb.String())
	}
	if !strings.Contains(tb.String(), "pay�gnp.sh") {
		t.Errorf("the cleaned character must leave a visible mark, got %q", tb.String())
	}
	var hb bytes.Buffer
	if err := HTML(&hb, r); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(hb.String(), '‮') || strings.ContainsRune(hb.String(), '​') {
		t.Error("bidi/zero-width character survived into the HTML report; html/template does not escape it")
	}
	if !strings.Contains(hb.String(), "pay�gnp.sh") {
		t.Error("HTML must carry the visible mark too")
	}
	// Reverse: the machine formats keep the bytes exactly.
	js, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), evil) {
		t.Error("JSON must carry the original bytes; sanitising a machine format hides the evidence from tooling")
	}
}

// TestText_WorstArtifactBesideTheMean pins the worst-artifact line: the headline is a mean, the worst item is
// printed next to it with the spread, and the line is absent when it would only restate.
func TestText_WorstArtifactBesideTheMean(t *testing.T) {
	arts := []model.ArtifactReport{
		{Kind: model.KindSkill, Name: "clean-a", Score: 100},
		{Kind: model.KindSkill, Name: "clean-b", Score: 100},
		{Kind: model.KindSkill, Name: "gstack", Score: 0},
	}
	r := model.ScanResult{Overall: 67, Artifacts: arts}
	var buf bytes.Buffer
	Text(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Worst single item: 0/100 — skill gstack") || !strings.Contains(out, "average over 3 items (2 of them at 100)") {
		t.Errorf("worst artifact not named beside the mean:\n%s", out)
	}
	var hb bytes.Buffer
	if err := HTML(&hb, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hb.String(), "Worst single item: 0/100") {
		t.Error("HTML summary must carry the worst line too")
	}
	// One artifact, or worst == mean: nothing to add.
	one := model.ScanResult{Overall: 40, Artifacts: arts[2:]}
	buf.Reset()
	Text(&buf, one)
	if strings.Contains(buf.String(), "Worst single item") {
		t.Error("a single artifact must not get a worst line that restates the headline")
	}
}

// TestText_InventoryCountsBundledSkills pins the bundled-skills count: skills inside plugins are read as part of
// the plugin tree and the inventory says so, instead of printing skills=0 about a plugin that
// bundles three.
func TestText_InventoryCountsBundledSkills(t *testing.T) {
	r := model.ScanResult{Overall: 100, Env: model.EnvSummary{Plugins: 1, BundledSkills: 3}}
	var buf bytes.Buffer
	Text(&buf, r)
	if !strings.Contains(buf.String(), "Inside plugins: 3 skill(s)") || !strings.Contains(buf.String(), "1 plugin, 3 skill(s) inside those plugins") {
		t.Errorf("bundled skills not surfaced:\n%s", buf.String())
	}
}
