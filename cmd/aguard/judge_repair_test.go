// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// closedEarlyServer answers every question like judgeServer, except that the model closes its
// object one member early and keeps writing — the slip measured on 5 of 1,482 calls (P-034).
func closedEarlyServer(t *testing.T, evidence string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		content := `{"flagged": true, "severity": "high", "summary": "undisclosed instruction", "evidence": ` +
			string(mustJSON(t, evidence)) + `}, "barrier_evidence": ""}`
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + string(mustJSON(t, content)) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestE2E_ClosedEarlyRepliesAreAnsweredAndCounted: through the real scan path, every judge reply
// closed one member early is an answered call — none failed, no LLM-000 — and the judge block says
// how many needed the repair. Triage replies are read by their own parser and are not counted.
func TestE2E_ClosedEarlyRepliesAreAnsweredAndCounted(t *testing.T) {
	root := buildTestRunnerSkill(t, true)
	cfg := writeJudgeConfig(t, closedEarlyServer(t, injectedLine).URL, "advisory")

	out, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	j := out.Judge
	if j == nil || !j.Ran || j.Calls == 0 {
		t.Fatalf("judge summary = %+v; want a run with calls", j)
	}
	if j.Failed != 0 || j.Repaired == 0 || j.Repaired != j.Calls-j.TriageCalls {
		t.Errorf("failed %d, repaired %d of %d call(s) (%d triage); want 0 failed and every judge call repaired",
			j.Failed, j.Repaired, j.Calls, j.TriageCalls)
	}
	for _, n := range out.Notes {
		if n.RuleID == "LLM-000" {
			t.Errorf("unexpected LLM-000 %q: every call was answered", n.Why)
		}
	}
	if j.Findings == 0 {
		t.Error("no advisory lead: the repaired verdicts quote a real line and should ground")
	}
}

// TestE2E_RepairedIsAlwaysInTheJudgeBlock: like failed and skipped, repaired is our own count and
// is always recorded, 0 included — its absence means a binary that did not count it.
func TestE2E_RepairedIsAlwaysInTheJudgeBlock(t *testing.T) {
	root := buildTestRunnerSkill(t, true)
	cfg := writeJudgeConfig(t, judgeServer(t, injectedLine).URL, "advisory")

	out, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out.Judge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"repaired":0`) {
		t.Errorf("judge summary %s lacks \"repaired\":0 for a run whose replies were all clean", raw)
	}
}
