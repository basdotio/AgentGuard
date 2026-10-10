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
		{"--db-password", "hunter2"},
		{"run", "--key", v},
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
