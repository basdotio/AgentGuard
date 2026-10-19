// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// failingTransport answers nothing: every round trip fails the way a refused connection does, after
// recording the request it was handed.
type failingTransport struct{ got *http.Request }

func (f *failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.got = r
	return nil, errors.New("connection refused")
}

// TestHTTP_RequestErrorDoesNotEchoCredentials (P-076): net/http names the request URL in its own
// error (`Post "<url>": …`), hiding the password only — the user name and the query stay. That
// error is what an LLM-000 note and `llm test` print. The client shows the URL the way every other
// place shows an endpoint, keeps the cause wrapped, and still classifies the failure as before.
// The request itself still carries the userinfo and the query (the reverse half).
func TestHTTP_RequestErrorDoesNotEchoCredentials(t *testing.T) {
	rt := &failingTransport{}
	c := NewHTTP("https://alice:s3cret@llm.example/v1?api-key=k123", "", "m", &http.Client{Transport: rt})
	_, err := c.Judge(context.Background(), Request{Mode: ModeIntent})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, secret := range []string{"alice", "s3cret", "k123"} {
		if strings.Contains(msg, secret) {
			t.Errorf("error quotes %q from the endpoint: %s", secret, msg)
		}
	}
	if !strings.Contains(msg, "llm.example") || !strings.Contains(msg, "connection refused") {
		t.Errorf("error lost the host or the cause: %s", msg)
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		t.Errorf("the *url.Error is no longer in the chain: %v", err)
	}
	if !isRetryable(err) {
		t.Errorf("a transport failure is no longer retryable: %v", err)
	}
	if rt.got == nil {
		t.Fatal("no request reached the transport")
	}
	if u, p, ok := rt.got.BasicAuth(); !ok || u != "alice" || p != "s3cret" {
		t.Errorf("the request lost the userinfo: Basic auth %q:%q (%v)", u, p, ok)
	}
	if !strings.Contains(rt.got.URL.RawQuery, "api-key=k123") {
		t.Errorf("the request lost the query: %q", rt.got.URL.RawQuery)
	}
}

// TestHTTP_BuildErrorDoesNotEchoCredentials: a base_url net/http cannot even build a request from
// is quoted in its parse error whole, password included.
func TestHTTP_BuildErrorDoesNotEchoCredentials(t *testing.T) {
	c := NewHTTP("https://alice:s3cret@llm.example:99999x/v1?api-key=k123", "", "m", &http.Client{Transport: &failingTransport{}})
	_, err := c.Judge(context.Background(), Request{Mode: ModeIntent})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"alice", "s3cret", "k123"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error quotes %q from the endpoint: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "llm.example") {
		t.Errorf("error no longer names the host: %v", err)
	}
}
