// SPDX-License-Identifier: MIT
package report

// The default terminal report, written for the reader who does not write code (P-058): one block
// per finding with the judge's view and a next step, a derived "Why N", a short Judge line, the
// LLM-002 disclosure on its own line, scan details behind --verbose, a short clean report, and no
// root-derived locations for a single target. Every assertion is on rendered text, the way a reader
// meets it.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

const targetRoot = "/h/.claude/skills/deploy"

// rootLocations is what scanLocations writes for a root — and, today, for a single target too.
func rootLocations(root string) []model.Location {
	parent := root[:strings.LastIndex(root, "/")]
	return []model.Location{
		{Name: "Config root", Path: root, Status: model.LocRead},
		{Name: "User MCP config", Path: parent + "/.claude.json", Status: model.LocAbsent},
		{Name: "Claude Desktop store", Path: parent + "/Library/Application Support/Claude/local-agent-mode-sessions", Status: model.LocAbsent},
		{Name: "Desktop session cache", Path: parent + "/Library/Application Support/Claude/claude-code-sessions", Status: model.LocAbsent},
	}
}

func exfil(sev model.Severity) model.Finding {
	return model.Finding{RuleID: "EXFIL-001", Dimension: 3, Severity: sev, Source: model.SrcStatic,
		Title: "Data-exfiltration surface: reads credentials + outbound network", Why: "The same file reads credentials and sends.",
		Evidence: []model.Evidence{{File: "SKILL.md", Line: 822}, {File: "SKILL.md", Line: 909}}}
}

// targetResult is `check <skill>` of a skill with one high finding, triaged by a judge that ran.
func targetResult(fs ...model.Finding) model.ScanResult {
	if len(fs) == 0 {
		fs = []model.Finding{exfil(model.SevHigh)}
	}
	r := model.ScanResult{
		Root: targetRoot, Env: model.EnvSummary{Skills: 1},
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "deploy", Path: targetRoot, Hash: "abc123",
			Findings: fs}},
		Locations: rootLocations(targetRoot),
		Judge: &model.JudgeSummary{Ran: true, Artifacts: 1, Calls: 7, Endpoint: "https://api.example.com/v1",
			PromptVersion: "cc2cab18585d", ExcerptVersion: 7, Model: "fake", Samples: 3},
	}
	score.Apply(&r)
	return r
}

func termOut(r model.ScanResult, verbose bool) string {
	var b bytes.Buffer
	if verbose {
		TextVerbose(&b, r)
	} else {
		Text(&b, r)
	}
	return b.String()
}

func lineWith(out, sub string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// TestText_JudgeViewInPlainWords: the judge's triage label reads as plain words under the finding it
// is about, says it is advice that leaves the finding and the score where they are, and says how many
// readings it rests on — one, because triage is asked once. Nothing in it suggests the finding went away.
func TestText_JudgeViewInPlainWords(t *testing.T) {
	r := targetResult()
	r.Artifacts[0].Advisory = []model.AdvisoryLabel{{RuleID: "EXFIL-001", Label: model.LabelBenign, Reason: "documented deploy step"}}
	for _, verbose := range []bool{false, true} {
		out := termOut(r, verbose)
		want := "Judge's view (advice; the finding and the score stand): likely a false alarm — documented deploy step (one reading)."
		at := strings.Index(out, want)
		if at < 0 {
			t.Fatalf("verbose=%v: the judge's view is not in plain words:\n%s", verbose, out)
		}
		if head := strings.Index(out, "[EXFIL-001]"); head < 0 || head > at {
			t.Errorf("verbose=%v: the judge's view must sit under the finding it is about:\n%s", verbose, out)
		}
		if strings.Contains(out, "likely-benign") || strings.Contains(out, "⚖ triage") {
			t.Errorf("verbose=%v: the raw label is still printed:\n%s", verbose, out)
		}
		l := strings.ToLower(lineWith(out, "Judge's view"))
		for _, bad := range []string{"clear", "dismiss", "lower", "downgrad", "remov", "suppress"} {
			if strings.Contains(l, bad) {
				t.Errorf("verbose=%v: the judge's view says %q — it changes nothing (invariant #4): %s", verbose, bad, l)
			}
		}
	}
	r.Artifacts[0].Advisory = []model.AdvisoryLabel{{RuleID: "EXFIL-001", Label: model.LabelReal, Reason: "sends the key"}}
	if out := termOut(r, false); !strings.Contains(out, "likely real — sends the key (one reading).") {
		t.Errorf("likely-real is not in plain words:\n%s", out)
	}
}

// TestText_JudgeVersionsOnlyInVerbose: the default view says what the judge did in one line; its
// model, samples and versions are for the auditor.
func TestText_JudgeVersionsOnlyInVerbose(t *testing.T) {
	r := targetResult()
	brief, full := termOut(r, false), termOut(r, true)
	if !strings.Contains(brief, "\nJudge: checked 1 skill, nothing to add\n") {
		t.Errorf("default view lacks the short Judge line:\n%s", brief)
	}
	for _, internal := range []string{"prompt_version", "excerpt_version", "samples 3", "model fake"} {
		if strings.Contains(brief, internal) {
			t.Errorf("default view still prints %q", internal)
		}
		if !strings.Contains(full, internal) {
			t.Errorf("--verbose lost %q", internal)
		}
	}
	j := *r.Judge
	j.Findings, j.Failed, j.Skipped = 2, 1, 2
	r.Judge = &j
	if l := lineWith(termOut(r, false), "Judge: "); l != "Judge: checked 1 skill, added 2 leads (below); 1 of 7 call(s) failed and 2 never sent, so its answer is incomplete" {
		t.Errorf("a short judge run must stay visible in the default line, got %q", l)
	}
	r.Judge = &model.JudgeSummary{Ran: false, Reason: "config llm.enabled is false"}
	if l := lineWith(termOut(r, false), "Judge: "); l != "Judge: did not run — config llm.enabled is false" {
		t.Errorf("got %q", l)
	}
	r.Judge = nil
	if strings.Contains(termOut(r, false), "Judge:") {
		t.Error("no --llm, no Judge line")
	}
}

// TestText_JudgeLineSuffix: the Judge line has exactly one extension point, unset in this build.
func TestText_JudgeLineSuffix(t *testing.T) {
	if JudgeLineSuffix != nil {
		t.Fatal("this build sets no Judge line suffix")
	}
	r := targetResult()
	plain := lineWith(termOut(r, false), "Judge: ")
	JudgeLineSuffix = func(j *model.JudgeSummary) string { return "extra\x1b[2J" }
	defer func() { JudgeLineSuffix = nil }()
	out := termOut(r, false)
	if got := lineWith(out, "Judge: "); got != plain+" · extra[2J" {
		t.Errorf("suffix not appended (and sanitized) to the Judge line: %q", got)
	}
	if strings.Count(out, "extra") != 1 {
		t.Errorf("the suffix belongs to the Judge line only:\n%s", out)
	}
}

// TestText_DisclosureIsNotACoverageGap: LLM-002 says where content went. It is printed as that, on its
// own line, and is no longer counted as a medium coverage note.
func TestText_DisclosureIsNotACoverageGap(t *testing.T) {
	r := targetResult()
	r.Notes = []model.Finding{{RuleID: "LLM-002", Severity: model.SevMedium, Source: model.SrcLLM, Title: "LLM judge endpoint is not local",
		Why: "base_url is not loopback: best-effort-redacted skill excerpts are sent off this machine."}}
	brief, full := termOut(r, false), termOut(r, true)
	if !strings.Contains(brief, "\nSent to api.example.com: redacted excerpts in 7 call(s). [LLM-002]\n") {
		t.Errorf("default view lacks the disclosure line:\n%s", brief)
	}
	if strings.Contains(brief, "Not checked") || strings.Contains(brief, "coverage note") {
		t.Errorf("LLM-002 is not a coverage gap:\n%s", brief)
	}
	if !strings.Contains(full, "Sent to api.example.com") || !strings.Contains(full, "best-effort-redacted skill excerpts") {
		t.Errorf("--verbose must keep the disclosure and the note's text:\n%s", full)
	}
	if strings.Contains(full, "Scan warnings") {
		t.Errorf("--verbose must not list LLM-002 among the coverage warnings:\n%s", full)
	}
	r.Notes = append(r.Notes, model.Finding{RuleID: "COV-000", Severity: model.SevLow, Title: "skipped a tree"})
	if l := lineWith(termOut(r, false), "Not checked"); !strings.Contains(l, "1 coverage note(s), highest low [COV-000]") {
		t.Errorf("the coverage line must count only coverage notes: %q", l)
	}
	r.Judge = &model.JudgeSummary{Ran: false, Reason: "endpoint refused", Endpoint: "https://api.example.com/v1"}
	if l := lineWith(termOut(r, false), "api.example.com"); !strings.Contains(l, "no call was made") {
		t.Errorf("a judge that sent nothing must not be said to have sent: %q", l)
	}
}

// TestText_TargetListsOnlyWhatApplies: a single target is not a config root, so the places a root scan
// derives from its parent are not listed for it; a root keeps every row.
func TestText_TargetListsOnlyWhatApplies(t *testing.T) {
	r := targetResult()
	full := termOut(r, true)
	var md bytes.Buffer
	if err := Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"verbose": full, "markdown": md.String()} {
		for _, row := range []string{"User MCP config", "Claude Desktop store", "Desktop session cache"} {
			if strings.Contains(out, row) {
				t.Errorf("%s: a single target lists %q", name, row)
			}
		}
		if !strings.Contains(out, "the target only") {
			t.Errorf("%s: say that the scan looked at the target only:\n%s", name, out)
		}
	}
	env := sampleResult()
	env.Locations = rootLocations("/x/.claude")
	if out := termOut(env, true); !strings.Contains(out, "Claude Desktop store") || !strings.Contains(out, "User MCP config") {
		t.Errorf("a root keeps every row:\n%s", out)
	}
}

// TestText_WhyFollowsTheFormula: the Why line names the findings that set the score, and its numbers are
// the formula's.
func TestText_WhyFollowsTheFormula(t *testing.T) {
	perm := model.Finding{RuleID: "PERM-001", Dimension: 2, Severity: model.SevMedium, Source: model.SrcStatic, Title: "broad grant", Why: "w",
		Evidence: []model.Evidence{{File: "settings.json", Line: 3}}}
	perm2 := perm
	perm2.RuleID = "PERM-002"
	exec := model.Finding{RuleID: "EXEC-003", Dimension: 4, Severity: model.SevLow, Source: model.SrcStatic, Title: "runs a program", Why: "w",
		Evidence: []model.Evidence{{File: "run.sh", Line: 1}}}
	env := func(arts ...model.ArtifactReport) model.ScanResult {
		r := model.ScanResult{Root: "/h/.claude", Artifacts: arts}
		score.Apply(&r)
		return r
	}
	skill := func(name string, fs ...model.Finding) model.ArtifactReport {
		return model.ArtifactReport{Kind: model.KindSkill, Name: name, Path: "/h/.claude/skills/" + name, Hash: "h" + name, Findings: fs}
	}
	for _, c := range []struct {
		name string
		r    model.ScanResult
		want string
	}{
		{"one high caps", targetResult(), "Why 69: a high finding caps the score at 69 (#1); by points alone it would be 75."},
		{"points", targetResult(perm, exec), "Why 83: 100 − 12 (#1, medium) − 5 (#2, low); only the worst finding of each kind counts."},
		{"worst of a kind counts once", targetResult(perm, perm2, exec), "Why 83: 100 − 12 (#1, medium) − 5 (#3, low); only the worst finding of each kind counts."},
		{"environment capped", env(skill("a", exfil(model.SevHigh)), skill("b"), skill("c")), "Why 69: the average over 3 items is 91, and a high finding caps the score at 69 (#1)."},
		{"environment mean", env(skill("a", perm), skill("b")), "Why 94: the average of 2 items' own scores (1 of them at 100)."},
	} {
		if got := lineWith(termOut(c.r, false), "Why "); got != c.want {
			t.Errorf("%s: got %q\nwant %q", c.name, got, c.want)
		}
	}
	if strings.Contains(termOut(targetResult(model.Finding{}), false), "Why 100") {
		t.Error("nothing to explain at 100")
	}
}

// TestText_NextStep: a finding says where to start reading and what to ask, in fixed words; the approve
// command is offered only where the load-time gate can use it, once per artifact.
func TestText_NextStep(t *testing.T) {
	out := termOut(targetResult(), false)
	if !strings.Contains(out, "Next: read SKILL.md around line 822. Ask whether this tool has a reason to read credentials AND talk to the network.") {
		t.Errorf("no next step:\n%s", out)
	}
	approve := `If you decide to keep it, tell the load-time gate: aguard approve "` + targetRoot + `"`
	if !strings.Contains(out, approve) {
		t.Errorf("no approve command for an approvable skill:\n%s", out)
	}

	odd := targetResult()
	odd.Root, odd.Artifacts[0].Path = "/h/sk$x", "/h/sk$x"
	if out := termOut(odd, false); !strings.Contains(out, `aguard approve '/h/sk$x'`) {
		t.Errorf("the path must be quoted for a shell:\n%s", out)
	}

	noApprove := map[string]model.ScanResult{}
	r := targetResult()
	r.Artifacts[0].Hash = ""
	noApprove["no hash"] = r
	r = targetResult()
	r.Artifacts[0].Kind = model.KindPlugin
	noApprove["a plugin"] = r
	noApprove["a critical"] = targetResult(exfil(model.SevHigh), model.Finding{RuleID: "REP-BAD", Dimension: 3, Severity: model.SevCritical,
		Source: model.SrcStatic, Title: "Known-malicious artifact (reputation list)", Why: "w", Evidence: []model.Evidence{{File: "deploy"}}})
	noApprove["low only"] = targetResult(exfil(model.SevLow))
	for name, r := range noApprove {
		if out := termOut(r, false); strings.Contains(out, "aguard approve") {
			t.Errorf("%s: approve offered where the gate cannot use it:\n%s", name, out)
		}
	}
	if out := termOut(noApprove["a critical"], false); !strings.Contains(out, "Next: remove it — this exact content is on the known-malicious list.") {
		t.Errorf("REP-BAD next step:\n%s", out)
	}

	grant := model.Finding{RuleID: "PERM-001", Dimension: 2, Severity: model.SevMedium, Source: model.SrcStatic, Title: "broad grant", Why: "w",
		Evidence: []model.Evidence{{File: "settings.json", Snippet: "Bash(*)"}}}
	if out := termOut(targetResult(grant), false); !strings.Contains(out, "Next: look at the entry quoted below.") {
		t.Errorf("whole-entry next step:\n%s", out)
	}

	two := exfil(model.SevMedium)
	two.RuleID = "EXFIL-003"
	env := model.ScanResult{Root: "/h/.claude", Artifacts: []model.ArtifactReport{
		{Kind: model.KindSkill, Name: "in", Path: "/h/.claude/skills/in", Hash: "a", Findings: []model.Finding{exfil(model.SevHigh), two}},
		{Kind: model.KindSkill, Name: "desk", Path: "/h/Library/Application Support/Claude/s/desk", Hash: "b", Findings: []model.Finding{exfil(model.SevHigh)}},
		{Kind: model.KindSkill, Name: "judged", Path: "/h/.claude/skills/judged", Hash: "c", Findings: []model.Finding{{RuleID: "LLM-003", Dimension: 1,
			Severity: model.SevHigh, Source: model.SrcLLM, Title: "lead", Why: "w", Evidence: []model.Evidence{{File: "SKILL.md", Line: 2}}}}},
	}}
	score.Apply(&env)
	out = termOut(env, false)
	if n := strings.Count(out, "aguard approve"); n != 1 {
		t.Errorf("approve must be offered once, for the skill inside the root (got %d):\n%s", n, out)
	}
	if !strings.Contains(out, `aguard approve --root "/h/.claude" "/h/.claude/skills/in"`) {
		t.Errorf("a scan's approve names the store's root:\n%s", out)
	}
}

// TestText_CleanIsShort: nothing to look at prints the title, the score and, when it ran, the judge.
func TestText_CleanIsShort(t *testing.T) {
	clean := targetResult(model.Finding{})
	clean.Artifacts[0].Findings = nil
	clean.Artifacts[0].Name, clean.Root, clean.Artifacts[0].Path = "careful", "/h/.claude/skills/careful", "/h/.claude/skills/careful"
	score.Apply(&clean)
	want := "AgentGuard · careful (skill)\n✅ Risk score 100/100 (Low) — nothing to fix\nJudge: checked 1 skill, nothing to add\n"
	if got := termOut(clean, false); got != want {
		t.Errorf("clean target:\n%s\nwant:\n%s", got, want)
	}
	clean.Judge = nil
	if got := termOut(clean, false); got != "AgentGuard · careful (skill)\n✅ Risk score 100/100 (Low) — nothing to fix\n" {
		t.Errorf("clean target without the judge:\n%s", got)
	}
	if !strings.Contains(termOut(clean, true), "Scan details") {
		t.Error("--verbose keeps the scan details")
	}

	env := model.ScanResult{Root: "/x/.claude", Env: model.EnvSummary{Hooks: 1},
		Artifacts: []model.ArtifactReport{{Kind: model.KindHook, Name: "PreToolUse[Bash]#1", Path: "/x/.claude/settings.json", Hash: "h"}}}
	score.Apply(&env)
	if got := termOut(env, false); got != "AgentGuard · /x/.claude\n✅ Risk score 100/100 (Low) — nothing to fix\n  Checked 1 hook.\n" {
		t.Errorf("clean environment:\n%s", got)
	}

	low := targetResult(exfil(model.SevLow))
	out := termOut(low, false)
	if !strings.Contains(out, "[EXFIL-001]") || !strings.Contains(out, "--verbose") || strings.Contains(out, "✅") {
		t.Errorf("a low finding is still printed in full:\n%s", out)
	}
	gap := clean
	gap.Notes = []model.Finding{{RuleID: "COV-000", Severity: model.SevMedium, Title: "skipped a tree"}}
	if out := termOut(gap, false); !strings.Contains(out, "highest medium [COV-000]") {
		t.Errorf("incomplete coverage keeps its collapsed line:\n%s", out)
	}
}

// TestMarkdown_ReadingOrder: markdown says the same things in the same order and keeps every datum.
func TestMarkdown_ReadingOrder(t *testing.T) {
	r := targetResult()
	r.Artifacts[0].Advisory = []model.AdvisoryLabel{{RuleID: "EXFIL-001", Label: model.LabelBenign, Reason: "documented deploy step"}}
	r.Notes = []model.Finding{{RuleID: "LLM-002", Severity: model.SevMedium, Source: model.SrcLLM, Title: "LLM judge endpoint is not local",
		Why: "best-effort-redacted skill excerpts are sent off this machine."}}
	var b bytes.Buffer
	if err := Markdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{"Why 69: a high finding caps the score at 69", "likely a false alarm", "Next: read", "Sent to api.example.com",
		"best-effort-redacted skill excerpts", "prompt\\_version", "aguard approve"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "## Not checked") {
		t.Errorf("markdown lists LLM-002 as not checked:\n%s", md)
	}
	if strings.Index(md, "Sent to") > strings.Index(md, "## What to look at") {
		t.Error("the disclosure belongs to the summary")
	}
}
