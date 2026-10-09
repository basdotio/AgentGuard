// SPDX-License-Identifier: MIT
package judge

// The judge's client and the redirects an endpoint can answer with (P-023). CheckEndpoint vets the
// configured base_url: remote means https, because the API key is a bearer header and the body is
// the redacted excerpts. A 30x from that endpoint used to be followed with Go's default policy,
// which never asked CheckEndpoint again: Go drops Authorization only when the host NAME changes
// (not the scheme, not the port, and a subdomain counts as the same), and 307/308 resend the body
// to wherever Location points. These tests run the production client — NewHTTP(…, nil), transport
// from the seam — against httptest servers reached by real host names, without leaving the machine.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	redirectKey    = "sk-redirect-test"        // a target that sees "Bearer "+this got the API key
	redirectMarker = "REDIRECT-EXCERPT-MARKER" // a target whose body holds this got the excerpt
	// redirectQuery is on every cross-origin Location; it must never reach an error message, because
	// a real Location can carry a one-time credential in exactly this position.
	redirectQuery = "sig=one-time-token"
)

// redirectHit is one request a test server received.
type redirectHit struct {
	method, path string
	key, excerpt bool
}

// redirectServer records what reaches it. With location set it answers status + Location;
// otherwise it answers 200 with a verdict, the way a model would.
type redirectServer struct {
	mu   sync.Mutex
	hits []redirectHit
}

func (s *redirectServer) serve(status int, location func(n int) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.hits = append(s.hits, redirectHit{
			method: r.Method, path: r.URL.Path,
			key:     r.Header.Get("Authorization") == "Bearer "+redirectKey,
			excerpt: strings.Contains(string(b), redirectMarker),
		})
		n := len(s.hits)
		s.mu.Unlock()
		if location != nil {
			if loc := location(n); loc != "" {
				w.Header().Set("Location", loc)
				w.WriteHeader(status)
				return
			}
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false,\"summary\":\"fine\"}"}}]}`))
	})
}

func (s *redirectServer) got() []redirectHit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]redirectHit(nil), s.hits...)
}

// httptestRoots trusts the certificate every httptest TLS server shares; its SANs include
// example.com, so verifying every routed host under that name keeps real TLS without real DNS.
func httptestRoots(t *testing.T) *x509.CertPool {
	t.Helper()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	return srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
}

// routeSeam points judge.Transport at a transport that dials each host:port in routes to the
// listener registered for it (anything else is refused, so nothing leaves the machine), and
// counts every round trip that crosses the seam.
func routeSeam(t *testing.T, roots *x509.CertPool, routes map[string]*httptest.Server) *atomic.Int32 {
	t.Helper()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			srv, ok := routes[addr]
			if !ok {
				return nil, fmt.Errorf("test transport: no route for %s", addr)
			}
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com"},
	}
	var trips atomic.Int32
	prev := Transport
	Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		trips.Add(1)
		return tr.RoundTrip(r)
	})
	t.Cleanup(func() {
		Transport = prev
		tr.CloseIdleConnections()
	})
	return &trips
}

func newRedirectTestServer(t *testing.T, tlsOn bool, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	if tlsOn {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	return srv
}

// judgeOnce makes the call a scan makes: one Judge with an excerpt, through NewHTTP(…, nil).
func judgeOnce(t *testing.T, base string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewHTTP(base, redirectKey, "m", nil).Judge(ctx, Request{
		Artifact: "skill:redirect", Mode: ModeIntent, Declared: "formats markdown", Behavior: redirectMarker + " cat notes.md",
	})
	return err
}

var redirectStatuses = []int{
	http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
	http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
}

// TestNewHTTP_RefusesCrossOriginRedirects: every row of P-023's table that leaves the configured
// origin — a downgrade to http on the same host, another host over https or http, a subdomain,
// a loopback endpoint bouncing to a remote host or to another port — under each redirect status.
// The hop is never sent (the target sees nothing: no key, no excerpt, no verdict taken from it),
// the call fails with an error that names where it was sent and not the path or query, and the
// failure is final rather than retryable.
func TestNewHTTP_RefusesCrossOriginRedirects(t *testing.T) {
	roots := httptestRoots(t)
	type endpoint struct {
		base, hostPort string
		tls            bool
	}
	remote := endpoint{"https://example.com/v1", "example.com:443", true}
	local := endpoint{"http://localhost:11434/v1", "localhost:11434", false}
	rows := []struct {
		name       string
		ep         endpoint
		target     string // scheme://host[:port] the endpoint redirects to
		hostPort   string // what the dialer sees for it
		tls        bool
		configured string // the configured origin, as the error must name it
	}{
		{"https endpoint, same host downgraded to http", remote, "http://example.com", "example.com:80", false, "https://example.com"},
		{"https endpoint, another host over https", remote, "https://collector.test", "collector.test:443", true, "https://example.com"},
		{"https endpoint, another host over http", remote, "http://collector.test", "collector.test:80", false, "https://example.com"},
		{"https endpoint, a subdomain", remote, "https://eu.example.com", "eu.example.com:443", true, "https://example.com"},
		{"loopback endpoint, a remote host", local, "http://collector.test", "collector.test:80", false, "http://localhost:11434"},
		{"loopback endpoint, another port", local, "http://localhost:8080", "localhost:8080", false, "http://localhost:11434"},
	}
	for _, row := range rows {
		for _, status := range redirectStatuses {
			t.Run(fmt.Sprintf("%s/%d", row.name, status), func(t *testing.T) {
				ep, tg := &redirectServer{}, &redirectServer{}
				location := row.target + "/landing/chat?" + redirectQuery
				epSrv := newRedirectTestServer(t, row.ep.tls, ep.serve(status, func(int) string { return location }))
				tgSrv := newRedirectTestServer(t, row.tls, tg.serve(0, nil))
				trips := routeSeam(t, roots, map[string]*httptest.Server{row.ep.hostPort: epSrv, row.hostPort: tgSrv})

				err := judgeOnce(t, row.ep.base)

				if hits := tg.got(); len(hits) != 0 {
					t.Errorf("the redirect target received %d request(s) (first: %s, API key %v, excerpt %v): the hop to an origin the user never configured was sent",
						len(hits), hits[0].method, hits[0].key, hits[0].excerpt)
				}
				if n := len(ep.got()); n != 1 {
					t.Errorf("the configured endpoint received %d request(s), want 1", n)
				}
				if n := trips.Load(); n != 1 {
					t.Errorf("%d round trip(s) crossed the seam, want 1 (the call to the configured endpoint)", n)
				}
				if err == nil {
					t.Fatalf("the call succeeded: a verdict was taken from %s, which the user never configured", row.target)
				}
				msg := err.Error()
				for _, want := range []string{"redirect", fmt.Sprint(status), row.target, row.configured} {
					if !strings.Contains(msg, want) {
						t.Errorf("error %q does not say %q", msg, want)
					}
				}
				if strings.Contains(msg, redirectQuery) || strings.Contains(msg, "/landing/") {
					t.Errorf("error %q carries the Location's path or query, which can hold a one-time credential", msg)
				}
				if isRetryable(err) {
					t.Errorf("a refused redirect is retryable (%v): it is the endpoint's answer, and a retry only sends the same excerpt again to be refused again", err)
				}
			})
		}
	}
}

// TestNewHTTP_SameOriginRedirectsAndPlainCallsUnchanged is the reverse half: an endpoint that does
// not redirect is called exactly as before, a redirect inside the configured origin is still
// followed through the same seam, and a same-origin loop still stops at Go's ten hops instead of
// running until the call times out.
func TestNewHTTP_SameOriginRedirectsAndPlainCallsUnchanged(t *testing.T) {
	roots := httptestRoots(t)
	const base, hostPort = "https://example.com/v1", "example.com:443"

	t.Run("no redirect", func(t *testing.T) {
		ep := &redirectServer{}
		srv := newRedirectTestServer(t, true, ep.serve(0, nil))
		trips := routeSeam(t, roots, map[string]*httptest.Server{hostPort: srv})
		if err := judgeOnce(t, base); err != nil {
			t.Fatalf("a plain https endpoint must answer as it always did: %v", err)
		}
		hits := ep.got()
		if len(hits) != 1 || hits[0].method != http.MethodPost || !hits[0].key || !hits[0].excerpt || hits[0].path != "/v1/chat/completions" {
			t.Errorf("hits = %+v, want one POST to /v1/chat/completions carrying the key and the excerpt", hits)
		}
		if n := trips.Load(); n != 1 {
			t.Errorf("%d round trip(s) crossed the seam, want 1", n)
		}
	})

	for _, status := range []int{http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprintf("same origin %d", status), func(t *testing.T) {
			ep := &redirectServer{}
			srv := newRedirectTestServer(t, true, ep.serve(status, func(n int) string {
				if n == 1 {
					return "https://example.com/v1/chat/completions/"
				}
				return ""
			}))
			trips := routeSeam(t, roots, map[string]*httptest.Server{hostPort: srv})
			if err := judgeOnce(t, base); err != nil {
				t.Fatalf("a redirect within the configured origin must still be followed: %v", err)
			}
			hits := ep.got()
			if len(hits) != 2 || hits[1].path != "/v1/chat/completions/" || !hits[1].key {
				t.Fatalf("hits = %+v, want the second request at the redirected path, with the key", hits)
			}
			// 307/308 resend the POST with its body; 301 turns it into a GET without one. That is
			// net/http's behaviour inside the origin, and this policy leaves it alone.
			wantPost := status != http.StatusMovedPermanently
			if (hits[1].method == http.MethodPost) != wantPost || hits[1].excerpt != wantPost {
				t.Errorf("second hop = %+v, want POST with the excerpt: %v", hits[1], wantPost)
			}
			if n := trips.Load(); n != 2 {
				t.Errorf("%d round trip(s) crossed the seam, want 2: a followed hop must go through the same transport the zero-dial counters watch", n)
			}
		})
	}

	t.Run("same-origin loop", func(t *testing.T) {
		ep := &redirectServer{}
		srv := newRedirectTestServer(t, true, ep.serve(http.StatusTemporaryRedirect, func(int) string {
			return "https://example.com/v1/chat/completions"
		}))
		routeSeam(t, roots, map[string]*httptest.Server{hostPort: srv})
		err := judgeOnce(t, base)
		if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
			t.Fatalf("err = %v, want the ten-hop limit", err)
		}
		if n := len(ep.got()); n != 10 {
			t.Errorf("the endpoint received %d request(s), want 10 (the call plus nine followed hops)", n)
		}
	})
}
