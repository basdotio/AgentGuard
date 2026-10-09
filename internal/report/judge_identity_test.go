// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// A report whose LLM findings came from a judge says which judge: one line under the judge
// summary, in every human renderer, when the judge ran (P-031).

func judgeIdentityReport(j *model.JudgeSummary) model.ScanResult {
	return model.ScanResult{Root: "/x/.claude", ToolVersion: "test", Overall: 100, OverallEffective: 100,
		Env: model.EnvSummary{Skills: 1}, Judge: j}
}

func ranJudge(modelName string) *model.JudgeSummary {
	return &model.JudgeSummary{Ran: true, Artifacts: 2, Calls: 5, PromptVersion: "0123456789ab", ExcerptVersion: 3,
		Model: modelName, Samples: 3}
}

// render runs the three human renderers over r.
func render(t *testing.T, r model.ScanResult) map[string]string {
	t.Helper()
	var text, md, html bytes.Buffer
	Text(&text, r)
	if err := Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if err := HTML(&html, r); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"text": text.String(), "markdown": md.String(), "html": html.String()}
}

// TestJudgeIdentityLine_NamesTheJudgeWhenItRan: model, samples and both versions, once, in each.
func TestJudgeIdentityLine_NamesTheJudgeWhenItRan(t *testing.T) {
	want := map[string]string{
		"text":     "LLM judge: model gpt-4o-mini · samples 3 · prompt_version 0123456789ab · excerpt_version 3",
		"markdown": `LLM judge: model gpt\-4o\-mini · samples 3 · prompt\_version 0123456789ab · excerpt\_version 3`,
		"html":     "LLM judge: model gpt-4o-mini · samples 3 · prompt_version 0123456789ab · excerpt_version 3",
	}
	for name, out := range render(t, judgeIdentityReport(ranJudge("gpt-4o-mini"))) {
		if n := strings.Count(out, want[name]); n != 1 {
			t.Errorf("%s report carries the judge identity line %d time(s), want once:\n%s", name, n, out)
		}
	}
}

// TestJudgeIdentityLine_AbsentWhenNothingToAttribute: a judge that did not run produced no finding
// to attribute, and a report without --llm says nothing about a judge at all.
func TestJudgeIdentityLine_AbsentWhenNothingToAttribute(t *testing.T) {
	notRun := &model.JudgeSummary{Reason: "config llm.enabled is false", PromptVersion: "0123456789ab", ExcerptVersion: 3}
	for label, r := range map[string]model.ScanResult{
		"not run":  judgeIdentityReport(notRun),
		"no --llm": judgeIdentityReport(nil),
	} {
		for name, out := range render(t, r) {
			if strings.Contains(out, "prompt_version") || strings.Contains(out, `prompt\_version`) {
				t.Errorf("%s, %s report names a judge build:\n%s", label, name, out)
			}
		}
	}
}

// TestJudgeIdentityLine_ModelNameIsInert: the model comes from the operator's config, which no
// scanned artifact writes — but it is printed into a terminal, a PR comment and a web page, and
// the renderers treat it like the endpoint reason next to it: sanitized, escaped, never markup.
func TestJudgeIdentityLine_ModelNameIsInert(t *testing.T) {
	out := render(t, judgeIdentityReport(ranJudge("m\u202egnp.sh `x` [x](https://e.example) <b>")))
	for name, o := range out {
		if strings.Contains(o, "\u202e") {
			t.Errorf("a bidi override in the model name reached the %s report", name)
		}
	}
	if strings.Contains(out["markdown"], "[x](https://e.example)") || strings.Contains(out["markdown"], "`x`") {
		t.Errorf("the model name became markdown markup:\n%s", out["markdown"])
	}
	if strings.Contains(out["html"], "e.example) <b>") || !strings.Contains(out["html"], "e.example) &lt;b&gt;") {
		t.Errorf("the model name became HTML markup:\n%s", out["html"])
	}
}
