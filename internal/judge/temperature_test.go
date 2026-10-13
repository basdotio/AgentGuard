// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// temperatureRefusingEndpoint answers like an OpenAI-shaped endpoint serving a model that allows
// only its default temperature: a request carrying the member gets the 400 that names it, a
// request without one gets a not-flagged verdict. It records every body it saw.
func temperatureRefusingEndpoint(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if _, has := body["temperature"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported.","type":"invalid_request_error","param":"temperature","code":"unsupported_value"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"flagged\":false,\"category\":\"\",\"disclosed\":false,\"severity\":\"low\",\"summary\":\"\",\"evidence\":\"\",\"barrier_evidence\":\"\"}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any { mu.Lock(); defer mu.Unlock(); return append([]map[string]any(nil), bodies...) }
}

// TestHTTP_TemperatureRefusedIsOmittedThenRemembered (P-052): the first refusal costs one extra
// POST, the same call re-sent without the member; every later call goes without it from the start,
// and the client says so.
func TestHTTP_TemperatureRefusedIsOmittedThenRemembered(t *testing.T) {
	srv, seen := temperatureRefusingEndpoint(t)
	c := NewHTTP(srv.URL, "", "gpt-6.1-sol", nil)
	if c.TemperatureOmitted() {
		t.Fatal("a fresh client has not been refused anything")
	}
	if _, err := c.Judge(context.Background(), Request{Mode: ModeInjection, Behavior: "echo hello world from a skill body"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.Judge(context.Background(), Request{Mode: ModeIntent, Declared: "d", Behavior: "b", Temperature: samplingTemperature}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !c.TemperatureOmitted() {
		t.Error("the client should report the member omitted")
	}
	b := seen()
	if len(b) != 3 {
		t.Fatalf("endpoint saw %d requests, want 3 (refused, re-sent, next call clean)", len(b))
	}
	if _, has := b[0]["temperature"]; !has {
		t.Error("the first request must carry the temperature as before")
	}
	for i := 1; i < 3; i++ {
		if _, has := b[i]["temperature"]; has {
			t.Errorf("request %d still carries the temperature after the refusal", i+1)
		}
	}
}

// TestHTTP_OtherBadRequestsAreNotRetried: a 400 that is not the temperature refusal is the answer
// it always was — one POST, the error returned, nothing remembered.
func TestHTTP_OtherBadRequestsAreNotRetried(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"The model 'nope' does not exist","type":"invalid_request_error","param":"model","code":"model_not_found"}}`)
	}))
	defer srv.Close()
	c := NewHTTP(srv.URL, "", "nope", nil)
	_, err := c.Judge(context.Background(), Request{Mode: ModeInjection, Behavior: "echo hello world from a skill body"})
	if err == nil || !strings.Contains(err.Error(), "model_not_found") {
		t.Fatalf("want the endpoint's 400 back, got %v", err)
	}
	if n != 1 || c.TemperatureOmitted() {
		t.Errorf("posts %d (want 1), omitted %v (want false)", n, c.TemperatureOmitted())
	}
}

// TestRun_TemperatureOmittedReachesTheStats: Run reads the client's report into Stats, which is
// what the report's judge block prints as temperature_omitted.
func TestRun_TemperatureOmittedReachesTheStats(t *testing.T) {
	srv, _ := temperatureRefusingEndpoint(t)
	arts := skillTree(t, 1)
	_, stats := Run(context.Background(), NewHTTP(srv.URL, "", "gpt-6.1-sol", nil), arts, Options{Concurrency: 1, Samples: 3})
	if !stats.TemperatureOmitted {
		t.Error("Stats.TemperatureOmitted should be set after the endpoint refused the member")
	}
	if stats.Failed != 0 {
		t.Errorf("the refusal must not count as a failed call: %+v", stats)
	}
	// Reverse: a client that was never refused reports nothing.
	_, stats = Run(context.Background(), categoryClient(Verdict{}), skillTree(t, 1), Options{Concurrency: 1})
	if stats.TemperatureOmitted {
		t.Error("a scripted client reports no omission")
	}
}
