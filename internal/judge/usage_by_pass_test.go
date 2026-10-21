// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// passRates are the tokens the stub reports for one answered call of each pass, all different, so
// a call counted under the wrong pass changes a sum.
var passRates = map[string]model.PassUsage{
	"intent":        {PromptTokens: 1000, CompletionTokens: 11, CachedPromptTokens: 1},
	"injection":     {PromptTokens: 2000, CompletionTokens: 22, CachedPromptTokens: 2},
	"result":        {PromptTokens: 3000, CompletionTokens: 33, CachedPromptTokens: 3},
	"deobfuscation": {PromptTokens: 4000, CompletionTokens: 44, CachedPromptTokens: 4},
	"collusion":     {PromptTokens: 5000, CompletionTokens: 55, CachedPromptTokens: 5},
	"capability":    {PromptTokens: 6000, CompletionTokens: 66, CachedPromptTokens: 6},
	"mcp-config":    {PromptTokens: 7000, CompletionTokens: 77, CachedPromptTokens: 7},
	"server":        {PromptTokens: 8000, CompletionTokens: 88, CachedPromptTokens: 8},
	"triage":        {PromptTokens: 9000, CompletionTokens: 99, CachedPromptTokens: 9},
}

// passOf names the pass a system message belongs to, by the task text it starts with: the stub's
// own reading of the request, independent of how the client attributes it.
func passOf(t *testing.T, system string) string {
	t.Helper()
	if strings.HasPrefix(system, triageTask+" ") {
		return "triage"
	}
	for m := ModeIntent; m <= ModeSteering; m++ {
		if strings.HasPrefix(system, modeTask(m)+" TRUST RULE") {
			return modeInfo(m).pass
		}
	}
	t.Errorf("system message of no known pass: %.120s", system)
	return ""
}

// passServer answers every pass with its passRates usage and keeps, per pass, what it reported in
// replies the client can decode. The first call of the capability pass is answered 503 (with a
// usage body the client must not count) and retried; the first call of the injection pass gets an
// unreadable verdict in a 200 envelope, whose usage DOES count, and is re-asked.
func passServer(t *testing.T) (*httptest.Server, func() map[string]model.PassUsage) {
	t.Helper()
	var mu sync.Mutex
	reported := map[string]model.PassUsage{}
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body chatRequest
		if err := json.Unmarshal(raw, &body); err != nil || len(body.Messages) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		pass := passOf(t, body.Messages[0].Content)
		rate := passRates[pass]
		mu.Lock()
		defer mu.Unlock()
		seen[pass]++
		usage := `"usage":{"prompt_tokens":` + strconv.Itoa(rate.PromptTokens) + `,"completion_tokens":` +
			strconv.Itoa(rate.CompletionTokens) + `,"prompt_tokens_details":{"cached_tokens":` + strconv.Itoa(rate.CachedPromptTokens) + `}}`
		if pass == "capability" && seen[pass] == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{` + usage + `}`))
			return
		}
		content := `{\"flagged\":false,\"labels\":[]}`
		if pass == "injection" && seen[pass] == 1 {
			content = `{\"flagged\": true, \"severity\": \"high\"]`
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + content + `"}}],` + usage + `}`))
		got := reported[pass]
		got.PromptTokens += rate.PromptTokens
		got.CompletionTokens += rate.CompletionTokens
		got.CachedPromptTokens += rate.CachedPromptTokens
		reported[pass] = got
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]model.PassUsage {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]model.PassUsage, len(reported))
		for k, v := range reported {
			out[k] = v
		}
		return out
	}
}

// sumPasses adds up a usage_by_pass map.
func sumPasses(m map[string]model.PassUsage) model.PassUsage {
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

// TestRun_UsageByPass (P-072): a run over one artifact for every pass, each question asked three
// times, attributes every call and every token the endpoint reported to the pass that asked it —
// the retried call and the re-asked one included — and the entries sum exactly to the run's totals.
func TestRun_UsageByPass(t *testing.T) {
	arts := append(previewFixture(t), serverTree(t)...)
	opts := Options{Samples: 3, Concurrency: 4, MaxRetries: 2}

	planned := map[string]int{}
	for _, c := range Plan(arts, opts) {
		planned[c.Pass] += c.Calls - c.NotSent
	}
	if len(planned) != len(passRates) {
		t.Fatalf("the fixture plans %d pass(es), want every one of the %d: %v", len(planned), len(passRates), planned)
	}

	srv, reported := passServer(t)
	client := NewHTTP(srv.URL, "", "", srv.Client())
	_, stats := Run(context.Background(), client, arts, opts)
	if stats.Failed != 0 || stats.Skipped != 0 || stats.Retries != 1 || stats.Reasked != 1 {
		t.Fatalf("stats = failed %d skipped %d retries %d reasked %d; want 0 / 0 / 1 / 1",
			stats.Failed, stats.Skipped, stats.Retries, stats.Reasked)
	}

	got := stats.UsageByPass
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(got) != len(planned) {
		t.Fatalf("usage_by_pass has %v, want one entry per planned pass %v", keys, planned)
	}
	byServer := reported()
	for pass, calls := range planned {
		u, ok := got[pass]
		if !ok {
			t.Errorf("%s: no entry", pass)
			continue
		}
		want := byServer[pass]
		want.Calls = calls
		if pass == "injection" {
			want.Reasked = 1
		}
		if u != want {
			t.Errorf("%s: got %+v, want %+v (what the endpoint reported for it)", pass, u, want)
		}
	}
	// The re-asked call's first, unreadable reply was a 200 the endpoint reported usage for: the
	// injection pass carries one reply more than it has calls. The 503 before the retried
	// capability call reported nothing the client counts.
	if inj := got["injection"]; inj.PromptTokens != (inj.Calls+1)*passRates["injection"].PromptTokens {
		t.Errorf("injection: %d prompt tokens over %d call(s), want the re-asked call's two replies counted", inj.PromptTokens, inj.Calls)
	}
	if c := got["capability"]; c.PromptTokens != c.Calls*passRates["capability"].PromptTokens {
		t.Errorf("capability: %d prompt tokens over %d call(s), want the 503 counted as nothing", c.PromptTokens, c.Calls)
	}

	sum := sumPasses(got)
	usage := client.Usage()
	if sum.Calls != stats.Calls || sum.Reasked != stats.Reasked ||
		sum.PromptTokens != usage.Prompt || sum.CompletionTokens != usage.Completion || sum.CachedPromptTokens != usage.CachedPrompt {
		t.Errorf("the entries sum to %+v; the run's totals are calls %d reasked %d tokens %+v", sum, stats.Calls, stats.Reasked, usage)
	}
}

// TestRun_UsageByPassNamesThePreviewsPasses: the keys are the pass names `bas-guard llm preview`
// prints, so a planned call and the usage it spent can be read side by side.
func TestRun_UsageByPassNamesThePreviewsPasses(t *testing.T) {
	arts := append(previewFixture(t), serverTree(t)...)
	planned := map[string]bool{}
	for _, c := range Plan(arts, Options{}) {
		planned[c.Pass] = true
	}
	_, stats := Run(context.Background(), &scriptedClient{answer: func(Request) (Verdict, error) { return Verdict{}, nil }}, arts, Options{})
	for pass := range stats.UsageByPass {
		if !planned[pass] {
			t.Errorf("usage_by_pass key %q is not a pass the preview names (%v)", pass, planned)
		}
	}
	for pass := range planned {
		if _, ok := stats.UsageByPass[pass]; !ok {
			t.Errorf("planned pass %q has no usage entry", pass)
		}
	}
}

// TestRun_UsageByPassReverse (P-072): a run that made no call has no map at all; a client that
// reports no tokens leaves every entry's token counts 0, with the calls still counted.
func TestRun_UsageByPassReverse(t *testing.T) {
	_, stats := Run(context.Background(), &scriptedClient{}, []model.ArtifactReport{{Kind: model.KindDirectory, Name: "d", Path: t.TempDir()}}, Options{})
	if stats.Calls != 0 || stats.UsageByPass != nil {
		t.Errorf("a run with no call: calls %d, usage_by_pass %v; want 0 and nil", stats.Calls, stats.UsageByPass)
	}

	arts := skillTree(t, 2)
	_, stats = Run(context.Background(), &scriptedClient{answer: func(Request) (Verdict, error) { return Verdict{}, nil }}, arts, Options{Samples: 3})
	sum := sumPasses(stats.UsageByPass)
	if sum != (model.PassUsage{Calls: stats.Calls}) || stats.Calls == 0 {
		t.Errorf("no tokens reported: the entries sum to %+v over %d call(s); want calls only", sum, stats.Calls)
	}
}
