// SPDX-License-Identifier: MIT
package detect

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// memberContexts are the four places a content hash reads a value under an object key: an MCP server's env, an
// MCP server's headers, an HTTP hook's headers, and the settings env block. Each builder writes one member,
// key → val, into its own file and returns the artifact that hashes it.
var memberContexts = []struct {
	name  string
	build func(t *testing.T, key, val string) model.ArtifactReport
}{
	{"mcp env", func(t *testing.T, key, val string) model.ArtifactReport {
		return mcpMember(t, map[string]any{"command": "srv", "env": map[string]string{key: val}})
	}},
	{"mcp headers", func(t *testing.T, key, val string) model.ArtifactReport {
		return mcpMember(t, map[string]any{"type": "http", "url": "https://mcp.example/x", "headers": map[string]string{key: val}})
	}},
	{"http hook headers", func(t *testing.T, key, val string) model.ArtifactReport {
		b, err := json.Marshal(map[string]any{"type": "http", "url": "https://hooks.example/x", "headers": map[string]string{key: val}})
		if err != nil {
			t.Fatal(err)
		}
		return hookArtifact(filepath.Join(t.TempDir(), "settings.json"), hookEntry("PostToolUse", "", string(b)))
	}},
	{"settings env", func(t *testing.T, key, val string) model.ArtifactReport {
		b, err := json.Marshal(map[string]any{"env": map[string]string{key: val}})
		if err != nil {
			t.Fatal(err)
		}
		return permArtifact(writeAt(t, filepath.Join(t.TempDir(), "settings.json"), string(b)), collect.SettingsEnvName)
	}},
}

// mcpMember writes one MCP server entry into its own .mcp.json.
func mcpMember(t *testing.T, entry map[string]any) model.ArtifactReport {
	t.Helper()
	b, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"s": entry}})
	if err != nil {
		t.Fatal(err)
	}
	return mcpArtifact(writeAt(t, filepath.Join(t.TempDir(), ".mcp.json"), string(b)), "s")
}

// TestContentHash_MemberValueIsForgottenWhole (P-042): a JSON member's value is one string, all of it the value
// its key names, while the patterns — written for a shell line — stop at whitespace, a quote, `@`, `:` or
// padding. The hash used to keep what came after that point: `"DB_PASSWORD": "correct horse"` hashed ` horse`, a
// fragment of the secret anyone holding the published hash can brute-force, and the identity followed it —
// rotating the head kept the approval, rotating the tail re-asked.
//
// Each row, in each context: two secrets that differ in head and tail, and the same member written with the
// replacement already in place, must share one hash, and no fragment of either secret may be in the digest input.
func TestContentHash_MemberValueIsForgottenWhole(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	cases := []struct {
		name, key, a, b, redacted string
		fragments                 []string
	}{
		{"a space", "DB_PASSWORD", "correct horse", "battery staple", "<REDACTED>", []string{"horse", "staple"}},
		{"a tab", "API_TOKEN", "abcd9\tdefx", "wxyz8\tuvwq", "<REDACTED>", []string{"defx", "uvwq"}},
		{"a quote", "DB_PASSWORD", "abcd'efg9", "wxyz'qrs8", "<REDACTED>", []string{"efg9", "qrs8"}},
		{"an @", "CLIENT_SECRET", "abcd@efgh", "wxyz@qrst", "<REDACTED>", []string{"efgh", "qrst"}},
		{"a colon", "X-Api-Key", "k7Qp2x:Lm9Rt4", "zzzzzz:Qq8Yy3", "<REDACTED>", []string{"Lm9Rt4", "Qq8Yy3"}},
		{"base64 padding after a carrier word", "Authorization", "Basic dXNlcjpwYXNz==", "Basic Zm9vOmJhcmJh==", "Basic <REDACTED>", []string{"=="}},
		{"a space after a carrier word", "Authorization", "Bearer abcd1234 efgh5678", "Bearer wxyz9876 qrst4321", "Bearer <REDACTED>", []string{"efgh5678", "qrst4321"}},
		{"a long token and a tail", "Authorization", "Bearer abcdefghijklmnop qrst", "Bearer zyxwvutsrqponmlk wxyz", "Bearer <REDACTED>", []string{"qrst", "wxyz"}},
		{"a known-prefix token and a tail", "API_TOKEN", "sk-ant-aaaaaaaaaaaaaaaaaaaa extra", "sk-ant-bbbbbbbbbbbbbbbbbbbb other", "<REDACTED>", []string{"extra", "other"}},
	}
	for _, ctx := range memberContexts {
		for _, c := range cases {
			t.Run(ctx.name+"/"+c.name, func(t *testing.T) {
				a, b, r := ctx.build(t, c.key, c.a), ctx.build(t, c.key, c.b), ctx.build(t, c.key, c.redacted)
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
	}
}

// TestContentHash_MemberWithoutTailIsUnchanged (P-042, reverse assertion): only a member whose key announces a
// credential, and whose value the patterns start replacing and stop reading before its end, re-keys. These
// canonical inputs were measured on the base, before the member rule existed, and must not move: references
// (which secret a server is handed is configuration, so two references must hash differently), placeholders and
// literals, a value with nothing after the match, a value the patterns never start reading (kept as written: the
// residual the proposal names), keys that announce nothing, a URL credential under an ordinary key, a value whose
// forgotten span would hold structure (the guard refuses it and the base's reading stays), a quoted value inside
// the string (P-043's reading), and a key word inside a reference, which is not the key's own assignment.
func TestContentHash_MemberWithoutTailIsUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	cases := []struct {
		name, key, val, input string
	}{
		{"a reference", "API_TOKEN", "${MY_TOKEN}", `"API_TOKEN":"${MY_TOKEN}"`},
		{"another reference", "API_TOKEN", "${OTHER_TOKEN}", `"API_TOKEN":"${OTHER_TOKEN}"`},
		{"a reference after a carrier word", "Authorization", "Bearer ${GH_TOKEN}", `"Authorization":"Bearer ${GH_TOKEN}"`},
		{"a key word inside a reference", "Authorization", "Bearer ${GH_API_KEY:-abcd1234}", `"Authorization":"Bearer ${GH_API_KEY:<REDACTED>}"`},
		{"a placeholder", "Authorization", "Bearer token", `"Authorization":"Bearer token"`},
		{"a keyword literal", "AUTH", "true", `"AUTH":"true"`},
		{"nothing after the match", "DB_PASSWORD", "hunter2xyz", `"DB_PASSWORD":"<REDACTED>"`},
		{"already replaced", "DB_PASSWORD", "<REDACTED>", `"DB_PASSWORD":"<REDACTED>"`},
		{"never started", "DB_PASSWORD", "p@ss word", `"DB_PASSWORD":"p@ss word"`},
		{"a key that announces nothing", "DESCRIPTION", "plain words here", `"DESCRIPTION":"plain words here"`},
		{"a key word that is not the key's end", "token_count", "abcd efgh", `"token_count":"abcd efgh"`},
		{"a URL credential under an ordinary key", "DATABASE_URL", "postgres://app:pw12@db.example/app x", `"DATABASE_URL":"postgres://app:<REDACTED>@db.example/app x"`},
		{"structure in the span (#)", "DB_PASSWORD", "abcd#efgh", `"DB_PASSWORD":"<REDACTED>#efgh"`},
		{"structure in the span (?)", "DB_PASSWORD", "abcd efgh?x", `"DB_PASSWORD":"<REDACTED> efgh?x"`},
		{"a quoted value inside the string", "DB_PASSWORD", `"correct horse"`, `"DB_PASSWORD":"\"<REDACTED>\""`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := `{"command":"srv","env":{` + c.input + `}}`
			a := mcpMember(t, map[string]any{"command": "srv", "env": map[string]string{c.key: c.val}})
			if got := inputOf(t, root, a); got != want {
				t.Errorf("canonical input moved:\n  got  %s\n  want %s", got, want)
			}
		})
	}
}
