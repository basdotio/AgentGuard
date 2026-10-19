// SPDX-License-Identifier: MIT
package judge

// The URL every judge request is sent to (P-078). It used to be the configured base_url with
// "/chat/completions" appended to the whole string, so a base_url carrying a query (gateways take
// ?api-version=… or ?api-key=…) was requested at the base path, with the suffix swallowed by the
// last query value; a fragment swallowed it the same way and was never sent. These tests run the
// production client against an httptest stub and look at what the stub receives.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/basdotio/AgentGuard/internal/config"
)

// chatSuffix is what every base_url is extended by; the tests spell it out instead of reading it
// from the code under test.
const chatSuffix = "/chat/completions"

// receivedLine is what the stub saw of one request: the request target as sent on the wire, and
// the path and query the server parsed out of it.
type receivedLine struct {
	requestURI, path, rawQuery string
}

// lineStub answers every request with a valid verdict and records its request line.
func lineStub(t *testing.T) (*httptest.Server, func() []receivedLine) {
	t.Helper()
	var mu sync.Mutex
	var got []receivedLine
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, receivedLine{requestURI: r.RequestURI, path: r.URL.Path, rawQuery: r.URL.RawQuery})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"flagged\":false,\"severity\":\"info\",\"summary\":\"ok\",\"evidence\":\"\"}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []receivedLine {
		mu.Lock()
		defer mu.Unlock()
		return append([]receivedLine(nil), got...)
	}
}

// callBoth makes one Judge call and one Ping (the call behind `aguard llm test`) through the
// client the commands build, NewHTTP(…, nil).
func callBoth(t *testing.T, baseURL string) {
	t.Helper()
	c := NewHTTP(baseURL, "sk-chaturl-test", "m", nil)
	if _, err := c.Judge(context.Background(), Request{Artifact: "skill:x", Mode: ModeInjection, Behavior: "echo hi"}); err != nil {
		t.Fatalf("Judge(%q): %v", baseURL, err)
	}
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping(%q): %v", baseURL, err)
	}
}

// TestHTTP_QueryBaseURLKeepsPathAndQuery: a base_url with a query is requested at
// <path>/chat/completions with the query as written; a fragment is dropped (it is never sent). On
// main the first row arrived at path /v1 with the query "api-version=2024-01-01&api-key=k123/chat/completions".
func TestHTTP_QueryBaseURLKeepsPathAndQuery(t *testing.T) {
	srv, received := lineStub(t)
	cases := []struct {
		name, suffix, path, query string
	}{
		{"query", "/v1?api-version=2024-01-01&api-key=k123", "/v1/chat/completions", "api-version=2024-01-01&api-key=k123"},
		{"trailing slash and query", "/v1/?api-version=2024-01-01&api-key=k123", "/v1/chat/completions", "api-version=2024-01-01&api-key=k123"},
		{"query on the root", "?api-key=k123", "/chat/completions", "api-key=k123"},
		{"fragment", "/v1#section", "/v1/chat/completions", ""},
		{"query and fragment", "/v1?api-key=k123#section", "/v1/chat/completions", "api-key=k123"},
		{"escaped query value", "/v1?api-key=a%2Fb%26c", "/v1/chat/completions", "api-key=a%2Fb%26c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(received())
			callBoth(t, srv.URL+tc.suffix)
			got := received()[before:]
			if len(got) != 2 {
				t.Fatalf("want 2 requests (Judge and Ping), got %d: %+v", len(got), got)
			}
			for i, g := range got {
				if g.path != tc.path || g.rawQuery != tc.query {
					t.Errorf("request %d: path %q query %q, want path %q query %q", i, g.path, g.rawQuery, tc.path, tc.query)
				}
			}
		})
	}
}

// oldChatURL is how the client built the request URL before P-078, kept here as the reference the
// unchanged configurations are compared against byte for byte.
func oldChatURL(baseURL string) string { return strings.TrimRight(baseURL, "/") + chatSuffix }

// TestHTTP_PlainBaseURLRequestLineUnchanged is the reverse assertion: a base_url with neither a
// query nor a fragment — with or without trailing slashes — sends the request line it sent before.
func TestHTTP_PlainBaseURLRequestLineUnchanged(t *testing.T) {
	srv, received := lineStub(t)
	for _, suffix := range []string{"", "/", "/v1", "/v1/", "/v1//", "/compatible-mode/v1", "/a%20b/v1"} {
		t.Run(suffix, func(t *testing.T) {
			before := len(received())
			base := srv.URL + suffix
			callBoth(t, base)
			old, err := url.Parse(oldChatURL(base))
			if err != nil {
				t.Fatal(err)
			}
			for i, g := range received()[before:] {
				if g.requestURI != old.RequestURI() {
					t.Errorf("request %d target %q, want %q (as before P-078)", i, g.requestURI, old.RequestURI())
				}
			}
		})
	}
}

// urlRecorder is a RoundTripper that records the URL of each request and answers it without
// touching the network, so the presets' real hosts can be checked offline.
type urlRecorder struct {
	mu   sync.Mutex
	urls []string
}

func (r *urlRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Request: req,
		Body: http.NoBody,
	}, nil
}

// TestHTTP_PresetURLsUnchanged: every preset, and the default local endpoint, is sent to exactly
// the URL it was sent to before P-078.
func TestHTTP_PresetURLsUnchanged(t *testing.T) {
	bases := []string{config.Default().LLM.BaseURL}
	names := make([]string, 0, len(config.Presets))
	for n := range config.Presets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		bases = append(bases, config.Presets[n].BaseURL)
	}
	for _, base := range bases {
		if base == "" {
			continue
		}
		rec := &urlRecorder{}
		c := NewHTTP(base, "k", "m", &http.Client{Transport: rec})
		_, _ = c.Ping(context.Background()) // the empty body fails to parse; the URL is what is checked
		if len(rec.urls) != 1 || rec.urls[0] != oldChatURL(base) {
			t.Errorf("%s: sent to %q, want %q", base, rec.urls, oldChatURL(base))
		}
	}
	if len(bases) < 4 {
		t.Fatalf("only %d endpoints checked; the presets were not read", len(bases))
	}
}

// FuzzChatURL states the rule for any input: without '?' and '#' the URL is the old one, byte for
// byte; an http(s) base_url with either keeps its scheme, userinfo, host and query, gets
// /chat/completions on its path (trailing slashes trimmed first), and loses its fragment.
func FuzzChatURL(f *testing.F) {
	for _, s := range []string{
		"https://api.openai.com/v1", "http://localhost:11434/v1/", "https://h/v1?api-version=2024-01-01&api-key=k123",
		"https://h/v1/?x=1", "https://h/v1#frag", "https://u:p@h:8443/a%2Fb/v1//?k=v#f", "https://h?", "https://h/v1#a?b",
		"https://h/%zz?x", "http://0/ 00%2F#", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, base string) {
		got := chatURL(base)
		if !strings.ContainsAny(base, "?#") {
			if got != oldChatURL(base) {
				t.Fatalf("chatURL(%q) = %q, want the unchanged %q", base, got, oldChatURL(base))
			}
			return
		}
		b, err := url.Parse(base)
		if err != nil {
			if got != oldChatURL(base) {
				t.Fatalf("unparsable %q: chatURL = %q, want the unchanged %q", base, got, oldChatURL(base))
			}
			return
		}
		if (b.Scheme != "http" && b.Scheme != "https") || b.Host == "" || b.Opaque != "" {
			return // CheckEndpoint refuses these before any request is built
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("chatURL(%q) = %q does not parse: %v", base, got, err)
		}
		wantPath := strings.TrimRight(b.EscapedPath(), "/") + chatSuffix
		// The path is compared in its escaped form only when base_url wrote it that way: a path net/url
		// re-encodes (a space, say) is kept as written, and its re-encoding is not this function's rule.
		pathOK := !strings.Contains(base, b.EscapedPath()) || u.EscapedPath() == wantPath
		if u.Scheme != b.Scheme || u.User.String() != b.User.String() || u.Host != b.Host ||
			!pathOK || u.RawQuery != b.RawQuery || u.Fragment != "" {
			t.Fatalf("chatURL(%q) = %q: scheme %q host %q path %q query %q fragment %q; want path %q query %q, no fragment",
				base, got, u.Scheme, u.Host, u.EscapedPath(), u.RawQuery, u.Fragment, wantPath, b.RawQuery)
		}
	})
}
