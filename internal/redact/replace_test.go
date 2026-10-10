// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// TestCredentialsKeeping pins the per-replacement decision the content hash's guard takes (P-043): a
// replacement keep refuses leaves its own match as written and nothing else — the string's other
// replacements still apply. With nil it is Credentials, byte for byte.
func TestCredentialsKeeping(t *testing.T) {
	noGlob := func(match, repl string) bool { return !strings.Contains(match, "*") }
	for _, c := range []struct{ in, want string }{
		{"curl -u admin:* --token hunter2", "curl -u admin:* --token <REDACTED>"},
		{"deploy --token * API_KEY=abcd1234", "deploy --token * API_KEY=<REDACTED>"},
		{"curl -u admin:hunter2 --token *", "curl -u admin:<REDACTED> --token *"},
		{"nothing to replace", "nothing to replace"},
	} {
		if got := CredentialsKeeping(c.in, noGlob); got != c.want {
			t.Errorf("CredentialsKeeping(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, s := range []string{
		"curl -u admin:* --token hunter2", "https://u:pw@h.example/x", "Authorization: Bearer abcdefghijklmnop",
		"x --password hunter2 --api-key=k7Qp2xLm", "sk-ant-abcdefghijklmnopqrstu", "tool --key ./id.pem",
	} {
		if a, b := CredentialsKeeping(s, nil), Credentials(s); a != b {
			t.Errorf("CredentialsKeeping(%q, nil) = %q, Credentials = %q", s, a, b)
		}
	}
}
