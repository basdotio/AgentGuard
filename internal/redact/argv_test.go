// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// TestArgv pins P-036: an element that follows a flag is read together with it, through the same
// patterns a single string goes through, so `--api-key <key>` as two elements loses the key as
// `--api-key <key>` as one string does. The values are made up, short enough that neither a known
// prefix nor the entropy floor would catch them alone. The second half of the table is the reverse
// assertion: an element no flag announces comes out exactly as Secrets gives it alone.
func TestArgv(t *testing.T) {
	const v = "k7Qp2xLm9Rt4Vw8Z" // 16 characters: under the 24-character entropy floor, no prefix
	if Secrets(v) != v {
		t.Fatalf("precondition: %q alone is not redacted", v)
	}
	var cases []argvCase
	add := func(name string, in, want []string) {
		cases = append(cases, argvCase{name, in, want, !strings.HasPrefix(name, "reverse ")})
	}
	for _, flag := range []string{"--password", "--passwd", "--passphrase", "--pass", "--token", "--secret",
		"--api-key", "--api_key", "--apikey", "--access-token", "--access_token", "--API-KEY"} {
		add(flag, []string{"srv", flag, v, "--verbose"}, []string{"srv", flag, marker, "--verbose"})
	}
	add("-u user:pass", []string{"-u", "admin:" + v, "https://api.example.com"},
		[]string{"-u", "admin:" + marker, "https://api.example.com"})
	add("--user user:pass", []string{"--user", "admin:" + v}, []string{"--user", "admin:" + marker})
	add("a key word in the flag, 12+ characters", []string{"--db-password", "hunter2hunter2"},
		[]string{"--db-password", marker})
	add("a space in the value", []string{"--password", "correct horse battery"}, []string{"--password", marker})
	add("a quote in the value", []string{"--api-key", "ab'cd" + v}, []string{"--api-key", marker})
	add("the user half keeps up to the value", []string{"-u", "admin:pa ss"}, []string{"-u", "admin:" + marker})
	// Two rows that were reverse rows until P-040 widened flagSecretRE: a key word at the end of the flag
	// now announces a value of any length, and `--key` announces one.
	add("a key word in the flag, under 12 characters", []string{"--db-password", "hunter2"}, []string{"--db-password", marker})
	add("--key", []string{"run", "--key", v}, []string{"run", "--key", marker})

	// Reverse: nothing announced, so each element is Secrets of itself.
	for _, in := range [][]string{
		{"--verbose", "plainword", "--version", "1.2.3"},
		{"-y", "@scope/pkg@1.2.3", "--port", "8080"},
		{"-u", "root"},
		{"--api-key=" + v, "positional"},
		{v, "--token"},
		{"token", v},
		{"--header", "Authorization: Bearer " + v},
		{"--env", "API_KEY=" + v},
		{},
	} {
		want := make([]string, len(in))
		for i, a := range in {
			want[i] = Secrets(a)
		}
		add("reverse "+strings.Join(in, " "), in, want)
	}

	for _, c := range cases {
		got := Argv(c.in)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") || len(got) != len(c.in) {
			t.Errorf("%s: Argv(%q) = %q, want %q", c.name, c.in, got, c.want)
			continue
		}
		if again := Argv(got); strings.Join(again, "\x00") != strings.Join(got, "\x00") {
			t.Errorf("%s: not idempotent: %q -> %q", c.name, got, again)
		}
		if !c.announced {
			continue // two elements no flag links are read alone on purpose (`token`, then a value)
		}
		for _, sep := range []string{" ", "\n"} {
			if j := strings.Join(got, sep); Secrets(j) != j {
				t.Errorf("%s: joined with %q, the output is not a fixed point of Secrets: %q -> %q", c.name, sep, j, Secrets(j))
			}
		}
	}
}

type argvCase struct {
	name      string
	in, want  []string
	announced bool // a flag announces an element: the joined output must then be a fixed point too
}

// TestAnnounced pins the one decision Argv and the content hash share (P-039): whether the element after a
// flag is a value the flag announces, and the byte where that value starts — everything from there to the
// element's end is the secret, however much of it the patterns would have read. The reverse rows are the
// elements Argv's own reverse rows leave to Secrets alone.
func TestAnnounced(t *testing.T) {
	cases := []struct {
		flag, arg string
		start     int
		ok        bool
	}{
		{"--password", "correct horse", 0, true},
		{"--api-key", "ab'cd", 0, true},
		{"--token", "abc\tdef", 0, true},
		{"--client-secret", "abcdefghijklmnop==", 0, true}, // a key word inside the flag (looseAssignRE)
		{"-u", "admin:pass word", len("admin:"), true},
		{"--user", "admin:pw", len("admin:"), true},
		{"--password", " lead", 1, true},          // the separator class takes the space; it is not the secret
		{"--password", `"quoted value"`, 1, true}, // a quoted value (P-043): the opening quote is the kept head
		// Reverse: nothing announced.
		{"--verbose", "plain word", 0, false},
		{"-y", "@scope/pkg", 0, false},
		{"--port", "8080", 0, false},
		{"-u", "root", 0, false},
		{"positional", "correct horse", 0, false}, // not a flag
		{"", "correct horse", 0, false},
		{"--api-key=abc", "positional", 0, false}, // the flag was rewritten itself
		{"--header", "token=abcd1234", 0, false},  // the element says it alone
		{"--token", marker, 0, false},             // already replaced
	}
	for _, c := range cases {
		start, ok := Announced(c.flag, c.arg)
		if start != c.start || ok != c.ok {
			t.Errorf("Announced(%q, %q) = %d, %v; want %d, %v", c.flag, c.arg, start, ok, c.start, c.ok)
			continue
		}
		// Argv is Announced plus Secrets of the head: the two must not drift apart.
		want := Secrets(c.arg)
		if ok {
			want = Secrets(c.arg[:start]) + marker
		}
		if got := Argv([]string{c.flag, c.arg})[1]; got != want {
			t.Errorf("Argv(%q, %q)[1] = %q, want %q", c.flag, c.arg, got, want)
		}
	}
}
