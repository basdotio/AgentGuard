// SPDX-License-Identifier: MIT
package main

// --fail-on-llm and a judge that could not answer (P-026). The LLM gate is a statement about
// every artifact — "nothing at or above this level, the judge's findings included" — and a run
// whose judge did not run, or ran short, cannot make it. That used to exit 0, the same code a
// judge that looked and found nothing gets, so a pipeline that asked the judge to gate it went
// green exactly when the judge was blind. It now exits 4, below a gate that fired (1) and a
// refused or failed run (2), and --fail-on alone never reaches it.

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// gateCode reads failGate's answer as the exit code main would produce: 0 for nil, the
// sentinel's code, or 2 for any other error.
func gateCode(err error) int {
	if err == nil {
		return 0
	}
	var fe *failExit
	if errors.As(err, &fe) {
		return fe.code
	}
	return 2
}

// TestFailGate_LLMGateNotEvaluable pins when the LLM gate has no answer, what wins over it,
// and — the reverse rows — that a judge which ran over everything, and --fail-on on its own,
// keep the codes they always had.
func TestFailGate_LLMGateNotEvaluable(t *testing.T) {
	with := func(r model.ScanResult, j *model.JudgeSummary) model.ScanResult { r.Judge = j; return r }
	none := model.ScanResult{}
	notRun := &model.JudgeSummary{Artifacts: 1, Reason: "API key file is missing"}
	for _, tc := range []struct {
		name              string
		out               model.ScanResult
		failOn, failOnLLM string
		code              int
		says              string // what the exit-4 error must name
	}{
		{"no --llm", with(none, nil), "", "high", 4, "--llm was not passed"},
		{"judge did not run", with(none, notRun), "", "high", 4, "API key file is missing"},
		{"calls failed", with(none, &model.JudgeSummary{Ran: true, Artifacts: 1, Calls: 2, Failed: 2}), "", "high", 4, "2 of 2 call(s) failed"},
		{"calls skipped", with(none, &model.JudgeSummary{Ran: true, Artifacts: 2, Calls: 1, Skipped: 1}), "", "high", 4, "1 planned call(s) were not made"},
		// Reverse: a judge that was asked everything it planned and answered it is an answer.
		{"ran fully, found nothing", with(none, &model.JudgeSummary{Ran: true, Artifacts: 1, Calls: 2}), "", "high", 0, ""},
		{"ran over nothing to ask", with(none, &model.JudgeSummary{Ran: true}), "", "high", 0, ""},
		// Reverse: --fail-on alone never reads the judge's state.
		{"--fail-on alone, judge did not run", with(none, notRun), "low", "", 0, ""},
		{"--fail-on alone, judge failed, hit", with(withHigh(), &model.JudgeSummary{Ran: true, Calls: 2, Failed: 2}), "high", "", 1, ""},
		// Precedence: a gate that fired is an answer, whatever the judge managed.
		{"--fail-on hit, judge did not run", with(withHigh(), notRun), "high", "high", 1, ""},
		{"deterministic hit through --fail-on-llm, no --llm", with(withHigh(), nil), "", "high", 1, ""},
		{"qualified LLM hit, other calls failed", with(llmResult(true), &model.JudgeSummary{Ran: true, Calls: 3, Failed: 2}), "", "high", 1, ""},
		// Precedence: a gate the run cannot honour is still refused as a run error.
		{"typo beats not-evaluable", with(none, nil), "", "hgih", 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := failGate(tc.out, tc.failOn, tc.failOnLLM, true)
			if got := gateCode(err); got != tc.code {
				t.Fatalf("exit %d, want %d (err: %v)", got, tc.code, err)
			}
			if tc.code == 4 && (!strings.Contains(err.Error(), tc.says) || !strings.Contains(err.Error(), "(exit 4)")) {
				t.Errorf("the exit-4 reason must name %q and say (exit 4), got %q", tc.says, err)
			}
		})
	}
}

// writeGateJudgeConfig is writeJudgeConfig with the knobs these rows turn: enabled, and extra
// llm keys. Retries are off so a closed port fails at once instead of backing off.
func writeGateJudgeConfig(t *testing.T, baseURL string, enabled bool, extra ...string) string {
	t.Helper()
	on := "true"
	if !enabled {
		on = "false"
	}
	body := "llm:\n  enabled: " + on + "\n  provider: openai_compatible\n  base_url: " + baseURL + "\n" +
		"  model: fake\n  concurrency: 4\n  timeout: 10s\n  total_timeout: 1m\n  max_retries: 0\n" +
		"  samples: 1\n  authority: escalate\n"
	for _, e := range extra {
		body += "  " + e + "\n"
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, body)
	return path
}

// TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind drives the built binary: every way the judge can
// fail to answer, on scan and on check, against a skill whose deterministic verdict is one
// medium — under --fail-on high, so anything above 0 came from the gate under test.
func TestFailOnLLM_ExitCodesWhenTheJudgeIsBlind(t *testing.T) {
	root := buildTestRunnerSkill(t, false)
	dir := filepath.Join(root, "skills", "test-runner")
	ok, _ := countingServer(t)
	closed := httptest.NewServer(nil)
	closed.Close() // a port nothing listens on any more: every call is refused
	cfg := map[string]string{
		"ok":       writeGateJudgeConfig(t, ok.URL, true),
		"closed":   writeGateJudgeConfig(t, closed.URL, true),
		"refused":  writeGateJudgeConfig(t, "http://judge.example.invalid/v1", true), // CheckEndpoint: remote must be https
		"nokey":    writeGateJudgeConfig(t, ok.URL, true, "api_key_file: "+filepath.Join(t.TempDir(), "missing-key")),
		"disabled": writeGateJudgeConfig(t, ok.URL, false),
		"budget":   writeGateJudgeConfig(t, ok.URL, true, "max_calls: 1"),
	}
	llmGate := []string{"--llm", "--fail-on-llm", "high"}
	for _, tc := range []struct {
		name, cfg string
		args      []string
		code      int
		says      string // stderr substring for exit 4; "" means stderr must be empty
	}{
		{"closed port", "closed", llmGate, 4, "call(s) failed"},
		{"endpoint refused", "refused", llmGate, 4, "the judge did not run"},
		{"api key file missing", "nokey", llmGate, 4, "the judge did not run"},
		{"llm.enabled false", "disabled", llmGate, 4, "the judge did not run"},
		{"call budget", "budget", llmGate, 4, "were not made"},
		{"no --llm", "ok", []string{"--fail-on-llm", "high"}, 4, "--llm was not passed"},
		// Reverse: the judge answered every question it planned.
		{"ran fully, found nothing", "ok", llmGate, 0, ""},
		// Reverse: --fail-on alone keeps today's code, whatever state the judge is in.
		{"--fail-on high only, closed port", "closed", []string{"--llm", "--fail-on", "high"}, 0, ""},
		{"--fail-on medium only, closed port", "closed", []string{"--llm", "--fail-on", "medium"}, 1, ""},
		// Precedence: a gate that fired wins over a judge that could not answer.
		{"--fail-on medium hit, closed port", "closed", []string{"--llm", "--fail-on", "medium", "--fail-on-llm", "high"}, 1, ""},
		{"--fail-on-llm medium hit, closed port", "closed", []string{"--llm", "--fail-on-llm", "medium"}, 1, ""},
	} {
		for _, cmd := range []string{"scan", "check"} {
			t.Run(cmd+"/"+tc.name, func(t *testing.T) {
				args := []string{"check", dir}
				if cmd == "scan" {
					args = []string{"scan", "--root", root, "--inbox", "off"}
				}
				args = append(append(args, tc.args...), "--config", cfg[tc.cfg], "--json", "--quiet")
				so, se, code := runAguard(t, args...)
				if code != tc.code {
					t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.code, se)
				}
				decodeScan(t, so) // the report is still written, and stdout is still only the report
				if tc.says == "" {
					if se != "" {
						t.Errorf("stderr is not empty:\n%s", se)
					}
					return
				}
				if strings.Count(se, "\n") != 1 || !strings.Contains(se, "(exit 4)") || !strings.Contains(se, tc.says) {
					t.Errorf("stderr must be one line saying (exit 4) and %q, got:\n%s", tc.says, se)
				}
			})
		}
	}
}
