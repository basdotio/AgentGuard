// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

func sarifOf(t *testing.T, res model.ScanResult) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := SARIF(&buf, res, "1.2.3", "https://example.invalid"); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v\n%s", err, buf.String())
	}
	return out
}

func run0(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("no runs in %+v", doc)
	}
	return runs[0].(map[string]any)
}

func results(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	rs, _ := run0(t, doc)["results"].([]any)
	for _, r := range rs {
		out = append(out, r.(map[string]any))
	}
	return out
}

// THE central invariant of this output. The judge does not move `overall` and does not fail
// `--fail-on`; if its findings arrive in Code Scanning as red `error` annotations beside deterministic
// ones, the distinction the terminal report is careful to draw is destroyed in the surface most people
// will actually read. Advisory dimensions (7/8, where static detection can hint but not confirm) get
// the same treatment.
func TestSARIF_JudgeAndAdvisoryNeverOutrankDeterministic(t *testing.T) {
	res := model.ScanResult{
		Root: "/r",
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
			{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "curl piped to shell", Evidence: []model.Evidence{{File: "skills/s/run.sh", Line: 3, Snippet: "curl x | bash"}}},
			{RuleID: "LLM-001", Dimension: 1, Severity: model.SevCritical, Source: model.SrcLLM,
				Title: "judge says hidden injection", Evidence: []model.Evidence{{File: "skills/s/SKILL.md", Line: 9}}},
			{RuleID: "BD-001", Dimension: 7, Severity: model.SevHigh, Source: model.SrcStatic, Advisory: true,
				Title: "environment-triggered conditional", Evidence: []model.Evidence{{File: "skills/s/run.sh", Line: 12}}},
		}}},
	}
	byRule := map[string]map[string]any{}
	for _, r := range results(t, sarifOf(t, res)) {
		byRule[r["ruleId"].(string)] = r
	}
	if got := byRule["EXEC-001"]["level"]; got != "error" {
		t.Errorf("a deterministic high is an error; got %v", got)
	}
	if got := byRule["LLM-001"]["level"]; got != "note" {
		t.Errorf("a judge finding must be a note however severe the judge called it; got %v", got)
	}
	if got := byRule["BD-001"]["level"]; got != "note" {
		t.Errorf("an advisory finding must be a note; got %v", got)
	}
	// And the numeric scale GitHub filters on must agree with the level, or the filter re-promotes
	// what the level demoted.
	if got := byRule["LLM-001"]["properties"].(map[string]any)["judge"]; got != true {
		t.Errorf("a judge result must be labelled as such; got %v", got)
	}
}

// Line 0 means "the whole artifact" — a config-level fact, a JSON value. SARIF requires startLine >= 1,
// and faking it to line 1 would annotate a line the finding is not about, which is worse than
// annotating the file.
func TestSARIF_WholeArtifactFindingOmitsRegion(t *testing.T) {
	res := model.ScanResult{Root: "/r", Artifacts: []model.ArtifactReport{{
		Kind: model.KindPermission, Name: "permissions", Findings: []model.Finding{
			{RuleID: "PERM-004", Dimension: 2, Severity: model.SevLow, Source: model.SrcStatic,
				Title: "no deny fallback", Evidence: []model.Evidence{{File: "settings.json", Line: 0}}},
		}}}}
	loc := results(t, sarifOf(t, res))[0]["locations"].([]any)[0].(map[string]any)
	phys := loc["physicalLocation"].(map[string]any)
	if _, has := phys["region"]; has {
		t.Errorf("a whole-artifact finding must carry no region; got %+v", phys)
	}
	if phys["artifactLocation"].(map[string]any)["uri"] != "settings.json" {
		t.Errorf("the file must still be named; got %+v", phys)
	}
}

// A fingerprint that moves when an unrelated line is inserted reopens alerts a reviewer already
// dismissed, and a tool that keeps re-raising settled findings gets muted.
func TestSARIF_FingerprintSurvivesALineShift(t *testing.T) {
	mk := func(line int) string {
		res := model.ScanResult{Root: "/r", Artifacts: []model.ArtifactReport{{
			Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
				{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic, Title: "t",
					Evidence: []model.Evidence{{File: "skills/s/run.sh", Line: line, Snippet: "curl x | bash"}}},
			}}}}
		r := results(t, sarifOf(t, res))[0]
		return r["partialFingerprints"].(map[string]any)["aguard/v1"].(string)
	}
	if a, b := mk(3), mk(41); a != b {
		t.Errorf("the same finding at a different line must keep its fingerprint: %s vs %s", a, b)
	}
}

// Same rule, different location, different threat. This is the distinction a rule-only mapping cannot
// express, and it was wrong once: rule metadata is created on first sight, so folding the artifact kind
// into it made the classification depend on walk order.
func TestSARIF_CategoriesAreRefinedPerResultNotPerRule(t *testing.T) {
	inj := func(kind model.ArtifactKind, name, file string) model.ArtifactReport {
		return model.ArtifactReport{Kind: kind, Name: name, Findings: []model.Finding{
			{RuleID: "INJ-001", Dimension: 1, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "override prior instructions", Evidence: []model.Evidence{{File: file, Line: 1}}},
		}}
	}
	res := model.ScanResult{Root: "/r", Artifacts: []model.ArtifactReport{
		inj(model.KindSubagent, "rev", "agents/rev.md"),
		inj(model.KindMemory, "m", "projects/p/memory/MEMORY.md"),
		inj(model.KindSkill, "s", "skills/s/SKILL.md"),
	}}
	doc := sarifOf(t, res)

	// Rule level: the rule's own category, independent of what was walked first.
	rules := run0(t, doc)["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)
	for _, r := range rules {
		rm := r.(map[string]any)
		if rm["id"] != "INJ-001" {
			continue
		}
		cats := rm["properties"].(map[string]any)["owasp-agentic"].([]any)
		if len(cats) != 1 || cats[0] != "ASI-01" {
			t.Errorf("rule-level categories must not depend on walk order; got %v", cats)
		}
	}

	// Result level: refined by where it was found.
	want := map[string][]string{
		"subagent:rev": {"ASI-01", "ASI-07"},
		"memory:m":     {"ASI-01", "ASI-06"},
		"skill:s":      {"ASI-01"},
	}
	for _, r := range results(t, doc) {
		art := r["properties"].(map[string]any)["artifact"].(string)
		exp, ok := want[art]
		if !ok {
			continue
		}
		var got []string
		for _, c := range r["properties"].(map[string]any)["owasp-agentic"].([]any) {
			got = append(got, c.(string))
		}
		if strings.Join(got, ",") != strings.Join(exp, ",") {
			t.Errorf("%s: categories %v, want %v", art, got, exp)
		}
	}
}

// Two runs over an unchanged tree must produce byte-identical output — the determinism `overall` is
// held to, applied to the report. A diffable artifact is also how a reviewer sees what a change did.
func TestSARIF_IsByteStable(t *testing.T) {
	res := model.ScanResult{Root: "/r", Overall: 65, Artifacts: []model.ArtifactReport{
		{Kind: model.KindSkill, Name: "b", Findings: []model.Finding{
			{RuleID: "FS-001", Dimension: 9, Severity: model.SevHigh, Source: model.SrcStatic, Title: "ssh key",
				Evidence: []model.Evidence{{File: "skills/b/x.sh", Line: 2}, {File: "skills/b/y.sh", Line: 1}}},
		}},
		{Kind: model.KindSkill, Name: "a", Findings: []model.Finding{
			{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic, Title: "curl",
				Evidence: []model.Evidence{{File: "skills/a/x.sh", Line: 9}}},
		}},
	}}
	var one, two bytes.Buffer
	if err := SARIF(&one, res, "1.2.3", "u"); err != nil {
		t.Fatal(err)
	}
	if err := SARIF(&two, res, "1.2.3", "u"); err != nil {
		t.Fatal(err)
	}
	if one.String() != two.String() {
		t.Error("SARIF output is not stable across runs")
	}
}

// Coverage notes belong in the machine-readable output too. "This file was not read" is exactly what a
// gate should surface, and dropping it here while keeping it in the terminal would make the two
// outputs disagree about how complete the scan was.
func TestSARIF_CoverageNotesAreEmitted(t *testing.T) {
	res := model.ScanResult{Root: "/r", Notes: []model.Finding{
		{RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
			Title: "entries not read", Evidence: []model.Evidence{{File: "install.sh", Line: 0}}},
	}}
	rs := results(t, sarifOf(t, res))
	if len(rs) != 1 || rs[0]["ruleId"] != "COV-000" {
		t.Fatalf("the coverage note must appear; got %+v", rs)
	}
	tags := run0(t, sarifOf(t, res))["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)[0].(map[string]any)["properties"].(map[string]any)["tags"].([]any)
	found := false
	for _, tg := range tags {
		if tg == "coverage" {
			found = true
		}
	}
	if !found {
		t.Errorf("a coverage note must be tagged as such so it can be filtered apart from risk; got %v", tags)
	}
}

// The score travels with the log — `overall` only. The effective score folds in judge findings and
// gates nothing, so publishing it here would invite exactly the confusion two numbers exist to avoid.
func TestSARIF_CarriesOnlyTheDeterministicScore(t *testing.T) {
	props := run0(t, sarifOf(t, model.ScanResult{Root: "/r", Overall: 65, OverallEffective: 40}))["properties"].(map[string]any)
	if props["aguard/overall"].(float64) != 65 {
		t.Errorf("overall must travel with the log; got %v", props["aguard/overall"])
	}
	for k := range props {
		if strings.Contains(k, "effective") {
			t.Errorf("the effective score must not appear in SARIF: %s", k)
		}
	}
}

// Measured on a real machine: the unowned-entries note came out as
// `Unowned entries under the root were not read: not read`. A coverage note carries its NAMES in Why
// and a bare label in its snippet, so composing the message from the snippet threw away the only part
// the reader could act on.
func TestSARIF_CoverageNoteMessageCarriesTheNames(t *testing.T) {
	res := model.ScanResult{Root: "/r", Notes: []model.Finding{
		{RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
			Title:    "Unowned entries under the root were not read",
			Why:      "These entries match no known layout, so they were NOT scanned: install.sh, tools.",
			Evidence: []model.Evidence{{File: "install.sh", Line: 0, Snippet: "not read"}}},
	}}
	msg := results(t, sarifOf(t, res))[0]["message"].(map[string]any)["text"].(string)
	if !strings.Contains(msg, "install.sh") || !strings.Contains(msg, "tools") {
		t.Errorf("a coverage note must name what went unread; got %q", msg)
	}
	if strings.HasSuffix(msg, ": not read") {
		t.Errorf("the message must not be composed from a bare label; got %q", msg)
	}
}

// A risk finding keeps the opposite shape: the snippet is the line that fired, and it is what a
// reviewer needs on the annotation.
func TestSARIF_RiskFindingMessageCarriesTheLine(t *testing.T) {
	res := model.ScanResult{Root: "/r", Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "s", Findings: []model.Finding{
			{RuleID: "EXEC-001", Dimension: 4, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "curl piped to shell", Why: "Fetches a script and runs it.",
				Evidence: []model.Evidence{{File: "skills/s/run.sh", Line: 3, Snippet: "curl x | bash"}}},
		}}}}
	msg := results(t, sarifOf(t, res))[0]["message"].(map[string]any)["text"].(string)
	if !strings.Contains(msg, "curl x | bash") {
		t.Errorf("a risk finding's message must carry the triggering line; got %q", msg)
	}
}
