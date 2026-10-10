// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestContentHash_WidenedFlagValuesAreNotDigestInputs (P-040): the hash reads redact.Credentials, so a
// flag the patterns now recognise by its last word forgets its value in the digest input too — before,
// `["--key", "<short key>"]` went in as written, and the published hash was a digest of the key. Changing
// only that value must not re-key, the same rule TestContentHash_SecretsAreNotDigestInputs pins for the
// flags named outright.
//
// The reverse half is what the exemption is for: a path after `--private-key` is not a secret but which
// key file a tool is pointed at, and an approval must re-ask when it is swapped; a flag whose credential
// word is not its last (`--key-file`, `--token-endpoint`) announces nothing.
func TestContentHash_WidenedFlagValuesAreNotDigestInputs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	settings := filepath.Join(root, "settings.json")
	mcp := func(args string) model.ArtifactReport {
		return mcpArtifact(writeAt(t, filepath.Join(t.TempDir(), ".mcp.json"),
			`{"mcpServers":{"s":{"command":"srv","args":`+args+`}}}`), "s")
	}
	hook := func(command string) model.ArtifactReport {
		return hookArtifact(settings, cmdHook("PreToolUse", "Bash", command))
	}

	same := []struct {
		name      string
		a, b, red model.ArtifactReport
		secret    string
	}{
		{"--key in args", mcp(`["--key","k7Qp2xLm9Rt4Vw8Z"]`), mcp(`["--key","Hx7Lq2Vw9Rt4"]`),
			mcp(`["--key","<REDACTED>"]`), "k7Qp2xLm9Rt4Vw8Z"},
		{"--db-pass in args", mcp(`["--db-pass","hunter2"]`), mcp(`["--db-pass","letmein9"]`),
			mcp(`["--db-pass","<REDACTED>"]`), "hunter2"},
		{"--client-secret under 12 characters", mcp(`["--client-secret","S3cR3tV"]`), mcp(`["--client-secret","Zq9Wm2x"]`),
			mcp(`["--client-secret","<REDACTED>"]`), "S3cR3tV"},
		{"--private-key=value in one element", mcp(`["--private-key=Pk9Xw2Lm"]`), mcp(`["--private-key=Qz8Vy3Kn"]`),
			mcp(`["--private-key=<REDACTED>"]`), "Pk9Xw2Lm"},
		{"--db-password in a hook command", hook("mytool sync --db-password hunter2 --host db"),
			hook("mytool sync --db-password letmein9 --host db"),
			hook("mytool sync --db-password <REDACTED> --host db"), "hunter2"},
	}
	for _, c := range same {
		ha, hb, hr := hashOf(root, c.a), hashOf(root, c.b), hashOf(root, c.red)
		if ha == "" || ha != hb || ha != hr {
			t.Errorf("%s: changing only the value after the flag must not re-key (got %q / %q / redacted form %q)",
				c.name, ha, hb, hr)
		}
		if in := inputOf(t, root, c.a); strings.Contains(in, c.secret) {
			t.Errorf("%s: the value reached the digest input: %s", c.name, in)
		}
	}

	differ := []struct {
		name string
		a, b model.ArtifactReport
	}{
		{"a path after --private-key", mcp(`["--private-key","./keys/a.pem"]`), mcp(`["--private-key","./keys/b.pem"]`)},
		{"a home path after --private-key", hook("deploy --private-key ~/.ssh/id_rsa"), hook("deploy --private-key ~/.ssh/other")},
		{"a URL after --credentials", mcp(`["--credentials","https://a.example/c"]`), mcp(`["--credentials","https://b.example/c"]`)},
		{"--key-file", mcp(`["--key-file","k7Qp2xLm9Rt4"]`), mcp(`["--key-file","Hx7Lq2Vw9Rt4"]`)},
		{"--token-endpoint", mcp(`["--token-endpoint","k7Qp2xLm9Rt4"]`), mcp(`["--token-endpoint","Hx7Lq2Vw9Rt4"]`)},
		{"a short flag", mcp(`["-p","8080"]`), mcp(`["-p","9090"]`)},
	}
	for _, c := range differ {
		if ha, hb := hashOf(root, c.a), hashOf(root, c.b); ha == "" || ha == hb {
			t.Errorf("%s: the two must hash differently (got %q for both)", c.name, ha)
		}
	}
}
