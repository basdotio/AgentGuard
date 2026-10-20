// SPDX-License-Identifier: MIT
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestLLMTest_QueryBaseURLReachesChatCompletions (P-078): `bas-guard llm test` with a base_url that
// carries a query sends its call to <path>/chat/completions with the query as written. Before
// P-078 the suffix was appended after the query, so the call arrived at /v1 with the query value
// "k123/chat/completions".
func TestLLMTest_QueryBaseURLReachesChatCompletions(t *testing.T) {
	var mu sync.Mutex
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path, query = r.URL.Path, r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer srv.Close()

	cfg := writeJudgeConfig(t, srv.URL+"/v1?api-version=2024-01-01&api-key=k123", "advisory")
	if err := runLLMTest(io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if path != "/v1/chat/completions" || query != "api-version=2024-01-01&api-key=k123" {
		t.Errorf("llm test reached path %q query %q, want /v1/chat/completions and api-version=2024-01-01&api-key=k123", path, query)
	}
}
