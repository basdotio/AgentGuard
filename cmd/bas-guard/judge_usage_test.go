// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basdotio/bas-guard/internal/judge"
	"github.com/basdotio/bas-guard/internal/model"
)

// TestJudgeUsageLine (P-071): the stderr usage line names the cached share of the prompt tokens only
// when the endpoint reported some. For every endpoint that does not — every local one — the line is
// byte-identical to the format it had before the count existed.
func TestJudgeUsageLine(t *testing.T) {
	stats := judge.Stats{Calls: 9, Retries: 1, Failed: 2,
		Latencies: []time.Duration{3 * time.Second, time.Second, 2 * time.Second}}
	elapsed := 4321 * time.Millisecond

	// The format string and arguments as they stood before P-071, copied verbatim.
	before := fmt.Sprintf("LLM judge: %d call(s) in %s (p50 %s, p95 %s) · %d retry · %d failed · %d tokens in / %d out\n",
		stats.Calls, elapsed.Round(time.Millisecond),
		stats.Percentile(0.5).Round(time.Millisecond), stats.Percentile(0.95).Round(time.Millisecond),
		stats.Retries, stats.Failed, 1200, 90)
	if got := judgeUsageLine(stats, elapsed, judge.TokenUsage{Prompt: 1200, Completion: 90}); got != before {
		t.Errorf("no cached tokens reported:\n got %q\nwant %q", got, before)
	}

	want := "LLM judge: 9 call(s) in 4.321s (p50 2s, p95 2s) · 1 retry · 2 failed · 1200 tokens in (800 cached) / 90 out\n"
	if got := judgeUsageLine(stats, elapsed, judge.TokenUsage{Prompt: 1200, Completion: 90, CachedPrompt: 800}); got != want {
		t.Errorf("cached tokens reported:\n got %q\nwant %q", got, want)
	}
}

// cachedUsageServer answers like usageServer, with prompt_tokens_details.cached_tokens set, except
// that the first request gets a 503: a retried call adds tokens once, for the reply that answered.
func cachedUsageServer(t *testing.T, prompt, completion, cached int) *httptest.Server {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":5000,"completion_tokens":5000,"prompt_tokens_details":{"cached_tokens":5000}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false}"}}],` +
			`"usage":{"prompt_tokens":` + strconv.Itoa(prompt) + `,"completion_tokens":` + strconv.Itoa(completion) +
			`,"prompt_tokens_details":{"cached_tokens":` + strconv.Itoa(cached) + `}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestE2E_JudgeSummaryCarriesCachedPromptTokens (P-071): an endpoint that reports cached prompt
// tokens gets them into the JSON judge block, summed over every sample and every call, a retried
// call counting once; the two counts that were there before keep their values.
func TestE2E_JudgeSummaryCarriesCachedPromptTokens(t *testing.T) {
	root := buildTestRunnerSkill(t, true)
	srv := cachedUsageServer(t, 100, 7, 64)
	cfg := writeJudgeConfigSamples(t, srv.URL, "advisory", 3)

	out, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	j := out.Judge
	if j == nil || !j.Ran || j.Calls == 0 || j.Failed != 0 || j.Samples != 3 {
		t.Fatalf("judge summary = %+v; want a clean run with calls at samples 3", j)
	}
	if j.Retries == 0 {
		t.Errorf("retries = 0; the stub answered the first request 503, so one call was re-sent")
	}
	if j.CachedPromptTokens != 64*j.Calls {
		t.Errorf("cached_prompt_tokens = %d over %d call(s), want %d (the 503 adds nothing)",
			j.CachedPromptTokens, j.Calls, 64*j.Calls)
	}
	if j.PromptTokens != 100*j.Calls || j.CompletionTokens != 7*j.Calls {
		t.Errorf("tokens = %d in / %d out over %d call(s); want %d / %d — cached is a subset, never added",
			j.PromptTokens, j.CompletionTokens, j.Calls, 100*j.Calls, 7*j.Calls)
	}
}

// TestE2E_UncachedEndpointBlockUnchanged (P-071, the reverse assertion): an endpoint that does not
// report the field leaves the key out, and the block is otherwise what it was — the same run's
// block, re-marshalled without the new key, is byte-identical to the one printed.
func TestE2E_UncachedEndpointBlockUnchanged(t *testing.T) {
	root := buildTestRunnerSkill(t, true)
	srv := usageServer(t, 100, 7)
	cfg := writeJudgeConfigSamples(t, srv.URL, "advisory", 3)

	out, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	j := out.Judge
	if j == nil || !j.Ran || j.Calls == 0 {
		t.Fatalf("judge summary = %+v; want a run with calls", j)
	}
	// P-072 adds usage_by_pass to a block that made calls; every other byte is what P-071 left.
	if j.UsageByPass == nil {
		t.Fatalf("a block that made calls has a usage_by_pass: %+v", j)
	}
	withoutByPass := *j
	withoutByPass.UsageByPass = nil
	raw, err := json.Marshal(withoutByPass)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["cached_prompt_tokens"]; ok {
		t.Errorf("judge block %s carries cached_prompt_tokens although the endpoint reported none", raw)
	}
	if j.PromptTokens != 100*j.Calls || j.CompletionTokens != 7*j.Calls {
		t.Errorf("tokens = %d in / %d out over %d call(s); want %d / %d",
			j.PromptTokens, j.CompletionTokens, j.Calls, 100*j.Calls, 7*j.Calls)
	}
	// Every key the block had before P-071, in the struct's order, with this run's values.
	type before struct {
		Ran                bool   `json:"ran"`
		Reason             string `json:"reason,omitempty"`
		Artifacts          int    `json:"artifacts"`
		Calls              int    `json:"calls"`
		Failed             int    `json:"failed"`
		Skipped            int    `json:"skipped"`
		Repaired           int    `json:"repaired"`
		Reasked            int    `json:"reasked"`
		TemperatureOmitted bool   `json:"temperature_omitted,omitempty"`
		Findings           int    `json:"findings"`
		Endpoint           string `json:"endpoint,omitempty"`
		TriageCalls        int    `json:"triage_calls"`
		Retries            int    `json:"retries"`
		PromptTokens       int    `json:"prompt_tokens,omitempty"`
		CompletionTokens   int    `json:"completion_tokens,omitempty"`
		PromptVersion      string `json:"prompt_version,omitempty"`
		ExcerptVersion     int    `json:"excerpt_version,omitempty"`
		Model              string `json:"model,omitempty"`
		Samples            int    `json:"samples,omitempty"`
	}
	old, err := json.Marshal(before{
		Ran: j.Ran, Reason: j.Reason, Artifacts: j.Artifacts, Calls: j.Calls, Failed: j.Failed,
		Skipped: j.Skipped, Repaired: j.Repaired, Reasked: j.Reasked, TemperatureOmitted: j.TemperatureOmitted,
		Findings: j.Findings, Endpoint: j.Endpoint, TriageCalls: j.TriageCalls, Retries: j.Retries,
		PromptTokens: j.PromptTokens, CompletionTokens: j.CompletionTokens, PromptVersion: j.PromptVersion,
		ExcerptVersion: j.ExcerptVersion, Model: j.Model, Samples: j.Samples,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(old) {
		t.Errorf("judge block moved for an endpoint without the field:\n got %s\nwant %s", raw, old)
	}
}

// sumUsageByPass adds up a usage_by_pass map (P-072).
func sumUsageByPass(m map[string]model.PassUsage) model.PassUsage {
	var s model.PassUsage
	for _, u := range m {
		s.Calls += u.Calls
		s.Reasked += u.Reasked
		s.PromptTokens += u.PromptTokens
		s.CompletionTokens += u.CompletionTokens
		s.CachedPromptTokens += u.CachedPromptTokens
	}
	return s
}

// knownPasses are the pass names a usage_by_pass key can take: the ones `bas-guard llm preview` prints.
var knownPasses = map[string]bool{"intent": true, "injection": true, "result": true, "deobfuscation": true,
	"collusion": true, "capability": true, "mcp-config": true, "server": true, "triage": true}

// assertUsageByPassAddsUp holds a judge block's usage_by_pass to its totals: one entry per pass
// called, named as the preview names it, and the entries summing exactly to calls, reasked and
// the three token counts.
func assertUsageByPassAddsUp(t *testing.T, j *model.JudgeSummary) {
	t.Helper()
	if len(j.UsageByPass) == 0 {
		t.Fatalf("a block with %d call(s) has no usage_by_pass: %+v", j.Calls, j)
	}
	for pass, u := range j.UsageByPass {
		if !knownPasses[pass] {
			t.Errorf("usage_by_pass key %q is no pass the preview names", pass)
		}
		if u.Calls == 0 {
			t.Errorf("%s: an entry for a pass that made no call: %+v", pass, u)
		}
	}
	sum := sumUsageByPass(j.UsageByPass)
	want := model.PassUsage{Calls: j.Calls, Reasked: j.Reasked, PromptTokens: j.PromptTokens,
		CompletionTokens: j.CompletionTokens, CachedPromptTokens: j.CachedPromptTokens}
	if sum != want {
		t.Errorf("usage_by_pass sums to %+v, the block's totals are %+v", sum, want)
	}
}

// TestE2E_JudgeBlockUsageByPass (P-072): at samples 3, with the first request answered 503 and
// retried, the JSON judge block breaks its calls and tokens down by pass, and the breakdown adds
// up to the totals, which are what they were before the breakdown existed.
func TestE2E_JudgeBlockUsageByPass(t *testing.T) {
	root := buildTestRunnerSkill(t, true)
	srv := cachedUsageServer(t, 100, 7, 64)
	cfg := writeJudgeConfigSamples(t, srv.URL, "advisory", 3)

	out, err := scanEnv(root, scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	j := out.Judge
	if j == nil || !j.Ran || j.Calls == 0 || j.Failed != 0 || j.Retries == 0 {
		t.Fatalf("judge summary = %+v; want a clean run with calls and the 503's retry", j)
	}
	assertUsageByPassAddsUp(t, j)
	for pass, u := range j.UsageByPass {
		if u.PromptTokens != 100*u.Calls || u.CompletionTokens != 7*u.Calls || u.CachedPromptTokens != 64*u.Calls {
			t.Errorf("%s: %+v; want 100 / 7 / 64 per call, the retried call counted once", pass, u)
		}
		if pass != "triage" && u.Calls%3 != 0 {
			t.Errorf("%s: %d call(s); a question is asked 3 times at samples 3", pass, u.Calls)
		}
	}
	if tr := j.UsageByPass["triage"]; tr.Calls != j.TriageCalls {
		t.Errorf("triage entry %+v, triage_calls %d", tr, j.TriageCalls)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"usage_by_pass":{`) {
		t.Errorf("the JSON block has no usage_by_pass object: %s", raw)
	}
}

// TestE2E_NoCallNoUsageByPass (P-072, reverse): a judge block that made no call has no
// usage_by_pass key, as it has no token keys.
func TestE2E_NoCallNoUsageByPass(t *testing.T) {
	srv := usageServer(t, 100, 7)
	cfg := writeJudgeConfigSamples(t, srv.URL, "advisory", 3)
	out, err := scanEnv(t.TempDir(), scanOpts{cfgPath: cfg, llm: true, quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Judge == nil || out.Judge.Calls != 0 {
		t.Fatalf("judge summary = %+v; want a block that made no call", out.Judge)
	}
	raw, err := json.Marshal(out.Judge)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "usage_by_pass") {
		t.Errorf("a block with no call carries usage_by_pass: %s", raw)
	}
}
