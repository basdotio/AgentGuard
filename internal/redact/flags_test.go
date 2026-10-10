// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// widenedFlags are the flag spellings P-040 adds to the eight flagSecretRE names outright: a flag whose
// last word is in the password family or `secret`, `key` bare or after one credential qualifier, and the
// bare `--pat`, `--bearer`, `--credential(s)`. Before it, each sent a short value as written — after some
// of them (`--key`, `--private-key`, `--db-pass`) at any length, after the ones looseAssignRE reads
// (`--db-password`, `--client-secret`, `--bearer`) under 12 characters.
var widenedFlags = []string{
	"--key", "--private-key", "--secret-key", "--access-key", "--auth-key", "--app-key", "--license-key",
	"--master-key", "--encryption-key", "--signing-key", "--client-key",
	"--db-pass", "--db-password", "--admin_password", "--user_pass", "--keychain-password",
	"--certificate-passphrase", "--pwd", "--db-pwd", "--db-passwd",
	"--client-secret", "--token-secret", "--webhook-secret", "--client_secret",
	"--pat", "--bearer", "--credential", "--credentials",
}

// TestFlagSecrets pins P-040: a value after a widened flag is gone in all three readings — one string
// `flag v`, one string `flag=v`, and the argv pair — whatever its length, while the flag survives and the
// output is a fixed point of Secrets. The values are made up, with no known prefix and under the entropy
// floor, so nothing but the flag announces them.
func TestFlagSecrets(t *testing.T) {
	values := []string{"Hx7Lq2Vw", "Hx7Lq2Vw9Rt", "k7Qp2xLm9Rt4Vw8Z", "k7Qp2xLm9Rt4Vw8ZqX3c"}
	for _, v := range values {
		if Secrets(v) != v {
			t.Fatalf("precondition: %q alone is not redacted", v)
		}
	}
	for _, flag := range widenedFlags {
		for _, f := range []string{flag, strings.ToUpper(flag)} {
			for _, v := range values {
				for _, s := range []string{"tool " + f + " " + v + " --verbose", "tool " + f + "=" + v + " --verbose"} {
					got := Secrets(s)
					if strings.Contains(got, v) || !strings.Contains(got, f) || !strings.Contains(got, marker) {
						t.Errorf("Secrets(%q) = %q: the value must go and the flag stay", s, got)
					}
					if Secrets(got) != got {
						t.Errorf("Secrets(%q) is not a fixed point: %q -> %q", s, got, Secrets(got))
					}
				}
				in := []string{"srv", f, v, "--verbose"}
				got := Argv(in)
				if want := []string{"srv", f, marker, "--verbose"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
					t.Errorf("Argv(%q) = %q, want %q", in, got, want)
				}
				if j := strings.Join(got, " "); Secrets(j) != j {
					t.Errorf("Argv(%q) joined is not a fixed point of Secrets: %q", in, j)
				}
			}
		}
	}
}

// TestFlagSecrets_Reverse is the reverse assertion of P-040, measured on the corpus and a real ~/.claude:
// a value survives, unchanged by Secrets and by Argv, after every flag that carried a non-credential there
// (the credential word not last, an open qualifier before `key`, a `token` compound, a word that only
// contains `pass` or `auth`, a `--no-` switch), after a widened flag when the value is a path or a URL,
// and after the short flags, which are not widened at all.
func TestFlagSecrets_Reverse(t *testing.T) {
	const v = "Hx7Lq2Vw9Rt4" // 12 characters, letters and digits: key-like, but nothing here announces it
	const short = "CAEQAA"   // under looseAssignRE's 12-character floor, for the `token` compounds
	pairs := [][2]string{
		{"--key-id", v}, {"--secret-id", v}, {"--secret_arn", v}, {"--api-key-id", v}, {"--token-id", v},
		{"--key-file", v}, {"--secret-file", v}, {"--password-file", v}, {"--vault-password-file", v},
		{"--password-stdin", v}, {"--key-stdin", v}, {"--token-endpoint", v}, {"--token-type", v},
		{"--keyring", v}, {"--keychain", v}, {"--keyword", v}, {"--keystore", v},
		{"--meta_key", v}, {"--space-key", v}, {"--assignment-key", v}, {"--data-key", v}, {"--api-private-key", v},
		{"--api_key_x", v},
		{"--page-token", short}, {"--from-token", short}, {"--auth-token", short}, {"--refresh-token", short},
		{"--author", v}, {"--bypass", v}, {"--multipass", v}, {"--passes", v}, {"--dbpass", v},
		{"--no-password", "positional"},
		{"--private-key", "./id.pem"}, {"--private-key", "~/.ssh/id_rsa"}, {"--private-key", "/etc/ssl/k"},
		{"--private-key", "../keys/id"}, {"--key", "/bucket/object/path"}, {"--key", "server.key"},
		{"--db-pass", "pw.txt"}, {"--credentials", "https://creds.example/c"}, {"--client-secret", "./c.json"},
		{"-p", "8080"}, {"-p", v}, {"-P", v}, {"-k", v}, {"-t", v},
	}
	for _, p := range pairs {
		readings := []string{"tool " + p[0] + " " + p[1]}
		// The `=` reading of a long flag, unless assignRE takes it on its own (`--page-token=…`: a key word
		// before `=` announces any value of four characters or more, before and after P-040 alike).
		if eq := "tool " + p[0] + "=" + p[1]; strings.HasPrefix(p[0], "--") && !assignRE.MatchString(eq) {
			readings = append(readings, eq)
		}
		for _, s := range readings {
			if got := Secrets(s); got != s {
				t.Errorf("Secrets(%q) = %q, want it unchanged", s, got)
			}
		}
		in := []string{"srv", p[0], p[1]}
		if got := Argv(in); strings.Join(got, "\x00") != strings.Join(in, "\x00") {
			t.Errorf("Argv(%q) = %q, want it unchanged", in, got)
		}
	}
	// Across a line break the next word is the next line, not the flag's argument: the judge's MCP excerpt
	// writes a kept element on its own line, and a widened flag must not take that line for its value.
	for _, s := range []string{"tool --key\n" + v, "args=--private-key\nargs=~/.ssh/id_rsa", "x --db-pass\r\n" + v} {
		if got := Secrets(s); got != s {
			t.Errorf("Secrets(%q) = %q, want it unchanged: a widened flag reads no value across a line break", s, got)
		}
	}
	for _, s := range []string{"mkdir -p build/out", "mysql -p" + v + " db", "find . -path ./x -print"} {
		if got := Secrets(s); got != s {
			t.Errorf("Secrets(%q) = %q, want it unchanged: short flags are not widened", s, got)
		}
	}
}

// TestFlagSecrets_NamedFlagsUnchanged: the exemption is the widened flags' only. The eight flags
// flagSecretRE names outright redact whatever follows them, a path or a URL included, as they did before
// P-040 — their name is the credential word, and P-036 pinned their behaviour.
func TestFlagSecrets_NamedFlagsUnchanged(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"tool --password ./pw.txt", "tool --password <REDACTED>"},
		{"tool --token https://t.example/x", "tool --token <REDACTED>"},
		{"tool --api-key=/k/v", "tool --api-key=<REDACTED>"},
		{"tool --secret ~/s", "tool --secret <REDACTED>"},
		{"tool --password\nhunter2", "tool --password\n<REDACTED>"}, // the named flags still read across a line
	} {
		if got := Secrets(c.in); got != c.want {
			t.Errorf("Secrets(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
