// SPDX-License-Identifier: MIT
package main

// A redirect from the judge's endpoint, end to end (P-023). The endpoint here is a loopback
// server that answers 307 with a Location on another port of the same machine: a different
// origin, but the same host NAME, which is all net/http compares before it copies Authorization
// onto the next hop. So under Go's default policy the target receives the key and the excerpts;
// the judge must instead refuse the hop, say so in the report the way every other judge
// shortfall is said, and leave the static result exactly as it was.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/model"
)

// redirectingEndpoint returns an endpoint that answers every call with 307 to a second server,
// and a counter of the requests that second server received (it would answer like a model).
func redirectingEndpoint(t *testing.T) (endpoint, target *httptest.Server, targetHits *atomic.Int32) {
	t.Helper()
	targetHits = &atomic.Int32{}
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false,\"summary\":\"OK\"}"}}]}`))
	}))
	t.Cleanup(target.Close)
	endpoint = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Location", target.URL+"/v1/chat/completions")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(endpoint.Close)
	return endpoint, target, targetHits
}

// deterministicFindings is every artifact's non-judge findings, in order, as JSON — the static
// result a judge run must leave byte-for-byte as it found it.
func deterministicFindings(t *testing.T, out model.ScanResult) string {
	t.Helper()
	var all [][]model.Finding
	for _, a := range out.Artifacts {
		var fs []model.Finding
		for _, f := range a.Findings {
			if f.Source != model.SrcLLM {
				fs = append(fs, f)
			}
		}
		all = append(all, fs)
	}
	b, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestE2E_JudgeRedirectIsRefusedAndReported: scan --llm against an endpoint that redirects out of
// its origin sends nothing to the target, reports the refusal as an LLM-000 naming it, does not
// retry it, and changes nothing static.
func TestE2E_JudgeRedirectIsRefusedAndReported(t *testing.T) {
	t.Setenv("AGUARD_LLM_KEY", "sk-e2e-redirect")
	root := buildTestRunnerSkill(t, true)
	endpoint, target, targetHits := redirectingEndpoint(t)
	cfg := writeJudgeConfig(t, endpoint.URL, "escalate")

	static, err := scanEnv(root, scanOpts{quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	judged, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}

	if n := targetHits.Load(); n != 0 {
		t.Errorf("the redirect target received %d request(s): the API key and the excerpts went to an origin the config never named", n)
	}
	note, ok := findNote(judged, "LLM-000")
	if !ok {
		t.Fatalf("no LLM-000 note: the report does not say the judge's calls went nowhere; notes = %+v", judged.Notes)
	}
	if !strings.Contains(note.Why, "redirect") || !strings.Contains(note.Why, target.URL) {
		t.Errorf("LLM-000 does not name the redirect and where it pointed (%s):\n%s", target.URL, note.Why)
	}
	js := judged.Judge
	if js == nil || !js.Ran || js.Calls == 0 || js.Failed != js.Calls {
		t.Errorf("judge summary = %+v, want a run whose every call failed", js)
	}
	if js != nil && js.Retries != 0 {
		t.Errorf("a refused redirect was retried %d time(s) (max_retries is 2): it is the endpoint's answer, not a transient fault", js.Retries)
	}
	if judged.Overall != static.Overall {
		t.Errorf("overall moved: %d without the judge, %d with it", static.Overall, judged.Overall)
	}
	if got, want := deterministicFindings(t, judged), deterministicFindings(t, static); got != want {
		t.Errorf("static findings changed when the judge's calls were refused:\nstatic: %s\njudged: %s", want, got)
	}
	for _, a := range judged.Artifacts {
		for _, f := range a.Findings {
			if f.Source == model.SrcLLM && f.Dimension > 0 {
				t.Errorf("an LLM finding (%s on %s) came out of a run whose every call was refused", f.RuleID, a.Name)
			}
		}
	}
}

// TestLLMTest_RedirectIsRefused: `aguard llm test` is where a redirecting endpoint should be found
// out — before any scan sends excerpts — so it fails, naming the redirect, and sends nothing on.
// TestLLMCommands_SetupTestStatus is the reverse half: an endpoint that answers still reports OK.
func TestLLMTest_RedirectIsRefused(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	endpoint, target, targetHits := redirectingEndpoint(t)
	var out bytes.Buffer
	if err := runLLMSetup(&out, "", llmSetupOpts{Provider: config.ProviderGeneric, BaseURL: endpoint.URL, Model: "test-model", Key: strings.NewReader("sk-test-redirect\n")}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := runLLMTest(&out, "")
	if err == nil {
		t.Fatalf("llm test passed against an endpoint that redirects to %s: output %q", target.URL, out.String())
	}
	if !strings.Contains(err.Error(), "redirect") || !strings.Contains(err.Error(), target.URL) {
		t.Errorf("llm test error does not name the redirect and its target %s: %v", target.URL, err)
	}
	if n := targetHits.Load(); n != 0 {
		t.Errorf("the redirect target received %d request(s) from llm test", n)
	}
}
