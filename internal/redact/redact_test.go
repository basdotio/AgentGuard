// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// TestCredentialsIsTheFirstHalfOfSecrets pins the split the content hash depends on
// (detect/contenthash.go, .claude/rules/hash.md): a value the text ANNOUNCES as a credential is
// removed by both passes, while a long opaque run nobody announced — a base64 payload looks exactly
// like one — survives Credentials and is removed only by Secrets. Hashing over Secrets would let a
// payload be swapped without changing the hash; snippeting over Credentials would print the run.
// The behaviour of every individual pattern is pinned by detect's redact_test.go, through
// detect.Redact, which is Secrets.
func TestCredentialsIsTheFirstHalfOfSecrets(t *testing.T) {
	const announced = "export API_KEY=hunter2hunter2"
	for name, f := range map[string]func(string) string{"Credentials": Credentials, "Secrets": Secrets} {
		if got, want := f(announced), "export API_KEY=<REDACTED>"; got != want {
			t.Errorf("%s(%q) = %q, want %q", name, announced, got, want)
		}
	}

	const run = "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MA9x"
	payload := "echo " + run + " | base64 -d | sh"
	if got := Credentials(payload); got != payload {
		t.Errorf("Credentials must leave an unannounced run alone (the hash keys on it): %q", got)
	}
	if got := Secrets(payload); strings.Contains(got, run) || got != "echo <REDACTED> | base64 -d | sh" {
		t.Errorf("Secrets must remove the run: %q", got)
	}
	// Secrets is idempotent and absorbs the first pass: running Credentials first changes nothing.
	for _, s := range []string{announced, payload, "postgres://u:s3cr3t@db/x"} {
		if Secrets(Credentials(s)) != Secrets(s) || Secrets(Secrets(s)) != Secrets(s) {
			t.Errorf("passes do not compose for %q", s)
		}
	}
}
