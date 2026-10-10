// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// P-041: a judge finding's severity is the tool's, read off the category the model names, never
// the severity word it writes. These tests pin the table, the two ways a verdict reaches it (a
// scripted client and a real HTTP reply), and the reverse: an unknown category cannot raise the
// weight, LLM-007 and LLM-009 are untouched.

// TestSeverityTable_EveryPassCategoryIsDecided: every (mode, category) the prompts list maps to a
// severity; an unknown category maps to medium; the model's word never changes the result; a mode
// without a table (MCP config, advisory-only) keeps the clamped word as before.
func TestSeverityTable_EveryPassCategoryIsDecided(t *testing.T) {
	for mode, rows := range categoryTables {
		if len(rows) == 0 {
			t.Errorf("mode %d has an empty category table", mode)
		}
		for _, r := range rows {
			for _, word := range []string{"low", "medium", "high", "critical", "", "bogus"} {
				got := severityFor(mode, Verdict{Flagged: true, Category: r.name, Severity: word})
				if got != r.sev {
					t.Errorf("mode %d category %q with the model saying %q: got %s, want %s (the word must not decide)", mode, r.name, word, got, r.sev)
				}
			}
			// Case and surrounding space carry no meaning, as for the severity word.
			if got := severityFor(mode, Verdict{Flagged: true, Category: "  " + strings.ToUpper(r.name) + " "}); got != r.sev {
				t.Errorf("mode %d category %q spelled loudly: got %s, want %s", mode, r.name, got, r.sev)
			}
		}
		for _, unknown := range []string{"", "catastrophe", "other-thing", "HIGH"} {
			if got := severityFor(mode, Verdict{Flagged: true, Category: unknown, Severity: "critical"}); got != model.SevMedium {
				t.Errorf("mode %d unknown category %q rated critical by the model: got %s, want medium", mode, unknown, got)
			}
		}
	}
	// Exactly these passes carry a table; the advisory-only MCP pass keeps the word, clamped.
	for _, m := range []Mode{ModeInjection, ModeIntent, ModeExplain, ModeCollusion, ModeCapability} {
		if _, ok := categoryTables[m]; !ok {
			t.Errorf("mode %d has no category table", m)
		}
	}
	if _, ok := categoryTables[ModeMCPConfig]; ok {
		t.Error("the MCP config pass is advisory-only and must not gain a table here")
	}
	if got := severityFor(ModeMCPConfig, Verdict{Flagged: true, Category: "network", Severity: "critical"}); got != model.SevHigh {
		t.Errorf("MCP config keeps the clamped word: got %s, want high", got)
	}
	// Every table has an escape valve named other / other-directive at medium, and the high rows
	// are exactly the behaviours the task sentences name.
	for mode, rows := range categoryTables {
		valve := false
		for _, r := range rows {
			if strings.HasPrefix(r.name, "other") {
				valve = true
				if r.sev != model.SevMedium {
					t.Errorf("mode %d: the valve %q must be medium, is %s", mode, r.name, r.sev)
				}
			}
			if r.sev == model.SevLow {
				t.Errorf("mode %d: no table row is low (%q); the one-step rule reaches low only through disclosure", mode, r.name)
			}
		}
		if !valve {
			t.Errorf("mode %d has no escape-valve category", mode)
		}
	}
}

// TestSeverity_IntentDisclosedIsOneStepLower: on the intent pass a behaviour the declared purpose
// states is one step lower — except a changed software source, which the prompt flags disclosed or
// not.
func TestSeverity_IntentDisclosedIsOneStepLower(t *testing.T) {
	cases := []struct {
		category  string
		disclosed bool
		want      model.Severity
	}{
		{"credential-read", false, model.SevHigh},
		{"credential-read", true, model.SevMedium},
		{"other", false, model.SevMedium},
		{"other", true, model.SevLow},
		{"software-source", true, model.SevHigh},
		{"software-source", false, model.SevHigh},
	}
	for _, c := range cases {
		got := severityFor(ModeIntent, Verdict{Flagged: true, Category: c.category, Disclosed: c.disclosed, Severity: "high"})
		if got != c.want {
			t.Errorf("intent %q disclosed=%v: got %s, want %s", c.category, c.disclosed, got, c.want)
		}
	}
	// Disclosure is an intent-pass notion: on every other pass the field changes nothing.
	for _, m := range []Mode{ModeInjection, ModeCapability, ModeExplain, ModeCollusion} {
		rows := categoryTables[m]
		a := severityFor(m, Verdict{Flagged: true, Category: rows[0].name})
		b := severityFor(m, Verdict{Flagged: true, Category: rows[0].name, Disclosed: true})
		if a != b {
			t.Errorf("mode %d: disclosed changed %s to %s", m, a, b)
		}
	}
}

// categoryClient answers every pass with the same flagged verdict, quoting a line the judge sent.
func categoryClient(v Verdict) *scriptedClient {
	return &scriptedClient{answer: func(r Request) (Verdict, error) {
		out := v
		out.Evidence = quotableFrom(r)
		return out, nil
	}}
}

// TestRun_SeverityIsTheTools: a verdict the model rates medium, in a category the table rates
// high, is reported high and escalates — the weight the gate and the fold read.
func TestRun_SeverityIsTheTools(t *testing.T) {
	arts := skillTree(t, 1)
	Run(context.Background(), categoryClient(Verdict{Flagged: true, Severity: "medium", Category: "exfiltration", Summary: "sends the repo out"}), arts, Options{Concurrency: 1})
	f := findingByRule(arts[0], "LLM-003")
	if f == nil {
		t.Fatalf("no LLM-003: %+v", arts[0].Findings)
	}
	if f.Severity != model.SevHigh {
		t.Errorf("LLM-003 severity = %s, want high (category exfiltration), whatever the model's word", f.Severity)
	}
	if !score.Escalating(*f) {
		t.Errorf("a grounded single-sample finding in a high category must escalate: %+v", f)
	}
	// The intent pass on the same skill: a high category the purpose discloses is medium.
	g := findingByRule(arts[0], "LLM-001")
	if g == nil {
		t.Fatalf("no LLM-001: %+v", arts[0].Findings)
	}
	if g.Severity != model.SevHigh {
		t.Errorf("LLM-001 undisclosed exfiltration = %s, want high", g.Severity)
	}
}

// TestRun_UnknownCategoryCannotRaiseTheWeight: a category the table does not know is medium however
// the model rates it, and it still counts toward the majority — visible, weighted as medium.
func TestRun_UnknownCategoryCannotRaiseTheWeight(t *testing.T) {
	arts := skillTree(t, 1)
	Run(context.Background(), categoryClient(Verdict{Flagged: true, Severity: "critical", Category: "catastrophe", Summary: "the end"}), arts, Options{Concurrency: 1, Samples: 3})
	f := findingByRule(arts[0], "LLM-003")
	if f == nil {
		t.Fatalf("no LLM-003: %+v", arts[0].Findings)
	}
	if f.Severity != model.SevMedium {
		t.Errorf("unknown category rated critical: severity = %s, want medium", f.Severity)
	}
	if !f.Escalates {
		t.Errorf("3 of 3 agreeing on an unknown category still reaches the majority: %+v", f)
	}
	if !strings.Contains(f.Why, "[3 of 3 samples agreed] [severities: high, high, high] [tool: medium]") {
		t.Errorf("the vote must list the model's words and then the tool's severity: %q", f.Why)
	}
}

// TestRun_BarrierSeverityStaysTheTools: LLM-007 is unchanged by this proposal — high, set by the
// tool, with the vote list as before.
func TestRun_BarrierSeverityStaysTheTools(t *testing.T) {
	arts := skillTree(t, 1)
	client := &scriptedClient{answer: func(r Request) (Verdict, error) {
		return Verdict{Flagged: false, Severity: "low", Category: "other", BarrierEvidence: quotableFrom(r)}, nil
	}}
	Run(context.Background(), client, arts, Options{Concurrency: 1, Samples: 3})
	f := findingByRule(arts[0], "LLM-007")
	if f == nil {
		t.Fatalf("no LLM-007: %+v", arts[0].Findings)
	}
	if f.Severity != model.SevHigh || !f.Escalates {
		t.Errorf("LLM-007 must stay high and escalate: %+v", f)
	}
	if !strings.Contains(f.Why, "[severities: high, high, high]") {
		t.Errorf("the barrier vote list is unchanged: %q", f.Why)
	}
}

// TestJudge_HTTPReplyCarriesCategory: through the real client, a reply's category and disclosed
// members are read, and a reply without them (a model answering the old schema) is read as an
// unknown category — an answer, never a failure.
func TestJudge_HTTPReplyCarriesCategory(t *testing.T) {
	replies := map[string]string{
		"new": `{"flagged": true, "category": "Credential-Read", "disclosed": true, "severity": "low", "summary": "s", "evidence": "e", "barrier_evidence": ""}`,
		"old": `{"flagged": true, "severity": "high", "summary": "s", "evidence": "e", "barrier_evidence": ""}`,
	}
	var which string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": replies[which]}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	c := NewHTTP(srv.URL, "", "m", srv.Client())

	which = "new"
	v, err := c.Judge(context.Background(), Request{Mode: ModeIntent, Declared: "d", Behavior: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Category != "Credential-Read" || !v.Disclosed {
		t.Errorf("category/disclosed not read: %+v", v)
	}
	if got := severityFor(ModeIntent, v); got != model.SevMedium {
		t.Errorf("credential-read, disclosed: got %s, want medium", got)
	}
	which = "old"
	v, err = c.Judge(context.Background(), Request{Mode: ModeIntent, Declared: "d", Behavior: "b"})
	if err != nil {
		t.Fatalf("a reply without a category must still be read: %v", err)
	}
	if v.Category != "" || v.repaired {
		t.Errorf("old-schema reply: %+v", v)
	}
	if got := severityFor(ModeIntent, v); got != model.SevMedium {
		t.Errorf("no category: got %s, want medium (unknown)", got)
	}
}

// TestPrompt_EveryTableCategoryIsOffered: the system prompt of each pass that has a table names
// every category in it — the model can only pick what it was offered — and the MCP pass's prompt
// says nothing about categories.
func TestPrompt_EveryTableCategoryIsOffered(t *testing.T) {
	for mode, rows := range categoryTables {
		p := systemPrompt(mode, "nonce")
		for _, r := range rows {
			if !strings.Contains(p, r.name+" = ") {
				t.Errorf("mode %d: prompt does not offer category %q", mode, r.name)
			}
		}
		if !strings.Contains(p, `"category":`) {
			t.Errorf("mode %d: reply schema lacks category", mode)
		}
	}
	if strings.Contains(systemPrompt(ModeMCPConfig, "nonce"), " = ") {
		t.Error("the MCP config pass must not list categories")
	}
}
