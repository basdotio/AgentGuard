// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// TestKeyed pins the member question the content hash and the env snippets ask (P-042): whether the value
// under key is one key announces, and the byte where it starts — everything from there to the value's end is
// the secret, however much of it the patterns would have read. Every row the hash's member tests use is here,
// with the reverse rows: references, literals, values the patterns never start, keys that announce nothing.
func TestKeyed(t *testing.T) {
	cases := []struct {
		key, value string
		start      int
		ok         bool
	}{
		{"DB_PASSWORD", "correct horse", 0, true},
		{"API_TOKEN", "abcd9\tdefx", 0, true},
		{"DB_PASSWORD", "abcd'efg9", 0, true},
		{"CLIENT_SECRET", "abcd@efgh", 0, true},
		{"X-Api-Key", "k7Qp2x:Lm9Rt4", 0, true},
		{"Authorization", "Basic dXNlcjpwYXNz==", len("Basic "), true},
		{"Authorization", "Bearer abcd1234 efgh5678", len("Bearer "), true},
		{"Authorization", "Bearer abcdefghijklmnop qrst", len("Bearer "), true}, // the value alone reads as a credential too
		{"API_TOKEN", "sk-ant-aaaaaaaaaaaaaaaaaaaa extra", 0, true},             // so does a known prefix
		{"DB_PASSWORD", "hunter2xyz", 0, true},
		{"DB_PASSWORD", " lead space", 1, true}, // the separator class takes the space; it is not the secret
		{"DB_PASSWORD", "abcd#efgh", 0, true},   // structure is the caller's guard, not this question
		{"token", "abcd efgh", 0, true},
		// Reverse: nothing announced.
		{"API_TOKEN", "${MY_TOKEN}", 0, false},
		{"Authorization", "Bearer ${GH_TOKEN}", 0, false},
		{"Authorization", "Bearer ${GH_API_KEY:-abcd1234}", 0, false}, // a key word inside the value is not the key's
		{"Authorization", "Bearer token", 0, false},                   // a carrier word read as the value
		{"AUTH", "true", 0, false},                                    // a keyword literal
		{"DB_PASSWORD", "p@ss word", 0, false},                        // never started: fewer than four bytes before `@`
		{"DB_PASSWORD", "abc", 0, false},
		{"DB_PASSWORD", marker, 0, false},            // already replaced
		{"DB_PASSWORD", `"correct horse"`, 0, false}, // a quoted body: P-043's quoted branch reads it
		{"DB_PASSWORD", "", 0, false},
		{"DESCRIPTION", "plain words here", 0, false},
		{"token_count", "abcd efgh", 0, false}, // a key word that is not the key's end
		{"author", "abcd efgh", 0, false},
		{"DATABASE_URL", "postgres://app:pw12@db.example/app", 0, false},
		{"", "correct horse", 0, false},
	}
	for _, c := range cases {
		start, ok := Keyed(c.key, c.value)
		if start != c.start || ok != c.ok {
			t.Errorf("Keyed(%q, %q) = %d, %v; want %d, %v", c.key, c.value, start, ok, c.start, c.ok)
			continue
		}
		// The start is where the patterns themselves begin replacing the key's assignment: the two must not drift.
		if ok {
			joined := Credentials(c.key + "=" + c.value)
			if at := strings.Index(joined, marker); at != len(c.key)+1+start {
				t.Errorf("Keyed(%q, %q) starts at %d, Credentials replaces from %d (%q)", c.key, c.value, start, at-len(c.key)-1, joined)
			}
		}
	}
}

// TestKeyed_EveryKeyWordAnnounces: the key question is assignRE's, so every word of credKeys — as an env name's
// last segment and as a header — announces, and the same word with anything after it does not.
func TestKeyed_EveryKeyWordAnnounces(t *testing.T) {
	words := []string{
		"api_key", "api-key", "apikey", "secret", "token", "password", "passwd", "auth", "authentication",
		"authorization", "authorisation", "bearer", "access_key", "access-key", "accesskey", "client_secret",
		"client-secret", "clientsecret",
	}
	for _, w := range words {
		for _, key := range []string{w, strings.ToUpper("my_" + w), "X-" + w} {
			if start, ok := Keyed(key, "correct horse"); !ok || start != 0 {
				t.Errorf("Keyed(%q, …) = %d, %v; want 0, true", key, start, ok)
			}
		}
		if _, ok := Keyed(w+"_hint", "correct horse"); ok {
			t.Errorf("Keyed(%q, …) announced: the key word is not the key's end", w+"_hint")
		}
	}
}
