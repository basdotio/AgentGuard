// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var fenceNonceRE = regexp.MustCompile(`===AGUARD:([^=]+)===`)

// fenceNonces returns the nonce the system message names and the one the user message is
// fenced with; a request carries one nonce, so the two must be equal.
func fenceNonces(t *testing.T, system, user string) string {
	t.Helper()
	s, u := fenceNonceRE.FindStringSubmatch(system), fenceNonceRE.FindStringSubmatch(user)
	if s == nil || u == nil {
		t.Fatalf("no fence\nsystem: %s\nuser: %s", system, user)
	}
	if s[1] != u[1] {
		t.Fatalf("the system message names %s, the user message is fenced with %s", s[1], u[1])
	}
	return s[1]
}

// TestHTTP_JudgeNonce: the client fences a request with the nonce it is given when that is a
// well-formed one (P-080), and draws its own otherwise — an empty or malformed field never
// becomes a short or predictable fence, and a re-ask (P-069) never reuses the nonce it was given.
func TestHTTP_JudgeNonce(t *testing.T) {
	given := strings.Repeat("ab", 16)
	cases := []struct {
		name  string
		nonce string
		reask bool
		want  string // "" = a fresh 32-hex nonce other than the given one
	}{
		{"well-formed is used", given, false, given},
		{"empty draws", "", false, ""},
		{"short draws", "abcd", false, ""},
		{"upper case draws", strings.ToUpper(given), false, ""},
		{"non-hex draws", strings.Repeat("zz", 16), false, ""},
		{"fence characters draw", strings.Repeat("a", 29) + "===", false, ""},
		{"a re-ask draws", given, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var got string
			srv := chatServer(t, func(system, user string) string {
				mu.Lock()
				defer mu.Unlock()
				got = fenceNonces(t, system, user)
				return `{"flagged": false}`
			})
			defer srv.Close()
			r := Request{Artifact: "skill:demo", Mode: ModeInjection, Behavior: "Format the document.", Nonce: c.nonce, Reask: c.reask}
			if _, err := NewHTTP(srv.URL, "", "", srv.Client()).Judge(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			switch {
			case c.want != "" && got != c.want:
				t.Fatalf("fenced with %s, want the given %s", got, c.want)
			case c.want == "" && (!validNonce(got) || got == c.nonce || got == strings.ToLower(c.nonce)):
				t.Fatalf("fenced with %q, want a fresh nonce", got)
			}
		})
	}
}
