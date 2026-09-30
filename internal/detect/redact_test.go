// SPDX-License-Identifier: MIT
package detect

import (
	"strings"
	"testing"
)

func TestRedact_URLEmbeddedCredential(t *testing.T) {
	cases := []string{
		"DATABASE_URL=postgres://svc:hunter2long@db.internal/prod",
		"redis://:sup3rsecret@cache:6379/0",
		"amqp://user:pw@rabbit/vhost",
	}
	for _, in := range cases {
		out := Redact(in)
		if strings.Contains(out, "hunter2long") || strings.Contains(out, "sup3rsecret") || strings.Contains(out, ":pw@") {
			t.Errorf("URL password not redacted: %q -> %q", in, out)
		}
		if !strings.Contains(out, "<REDACTED>") {
			t.Errorf("expected <REDACTED> in %q -> %q", in, out)
		}
	}
	// A bare URL with no credentials must be left intact (no false redaction).
	if got := Redact("see http://example.com/docs"); got != "see http://example.com/docs" {
		t.Errorf("bare URL wrongly modified: %q", got)
	}
}

// TestRedact_Table pins the shapes that must lose their value, the shapes that must NOT be
// touched, and — separately from both — the parts of a hit that must SURVIVE. A redactor is
// judged on all three: one that eats the line around the secret protects the value and
// destroys the finding, and the operator acts on the finding.
func TestRedact_Table(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// gone must not appear in the output; kept must.
		gone []string
		kept []string
		// want, when set, pins the output exactly.
		want string
	}{
		// ---------- short values: the key already vouches for them ----------
		{
			// The bug this table was written for. 7 characters is a bad password, not a
			// non-password, and the old 12-char floor shipped it in the clear.
			name: "short password",
			in:   "password=hunter2",
			want: "password=<REDACTED>",
		},
		{
			name: "short api key, quoted and spaced",
			in:   `api_key = "12312sad"`,
			gone: []string{"12312sad"},
			kept: []string{"api_key"},
		},
		{
			name: "short token in JSON",
			in:   `{"token": "abcd", "port": 8080}`,
			gone: []string{`"abcd"`},
			kept: []string{"token", "8080"},
		},
		{
			// Four characters is the floor; below it there is no value shape left to match.
			name: "value below the floor",
			in:   "token=ab",
			want: "token=ab",
		},

		// ---------- the carrier word belongs to the separator ----------
		{
			// Regression guard: if `Bearer` is taken as the VALUE, it gets redacted and the
			// scan resumes past it — publishing the token it was standing in front of.
			name: "authorization header with a bearer scheme",
			in:   `-H "Authorization: Bearer abc123def456ghi"`,
			gone: []string{"abc123def456ghi"},
			kept: []string{"Authorization", "Bearer"},
		},
		{
			name: "basic scheme, whitespace separator",
			in:   "Authorization Basic dXNlcjpwYXNzd29yZA==",
			gone: []string{"dXNlcjpwYXNzd29yZA"},
			kept: []string{"Authorization", "Basic"},
		},

		// ---------- credential passed as a command-line flag ----------
		{
			name: "curl -u user:pass",
			in:   "curl -u admin:s3cr3t https://api.example.com",
			gone: []string{"s3cr3t"},
			kept: []string{"admin:", "https://api.example.com"},
		},
		{
			name: "long --user flag",
			in:   "curl --user svc:pw123 https://x.example",
			gone: []string{"pw123"},
			kept: []string{"svc:"},
		},
		{
			name: "--password=value",
			in:   "mysqldump --password=hunter2 --host db",
			gone: []string{"hunter2"},
			kept: []string{"--password=", "--host db"},
		},
		{
			name: "--token with a space",
			in:   "gh auth login --token ghp_x1y2z3",
			gone: []string{"ghp_x1y2z3"},
			kept: []string{"--token"},
		},

		// ---------- what must survive ----------
		{
			// The variable name is the half the operator acts on. `=` used to be inside the
			// entropy token class, so the name was swallowed with the value.
			name: "entropy hit keeps its variable name",
			in:   "MY_THING=Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MA==",
			gone: []string{"Zm9vYmFyYmF6"},
			kept: []string{"MY_THING="},
		},

		// ---------- precision controls ----------
		{
			// `auth[a-z]*` matched `author`, and with the lowered floor that means redacting
			// somebody's name out of a plugin manifest.
			name: "author is not auth",
			in:   `{"author": "Alice Smith"}`,
			want: `{"author": "Alice Smith"}`,
		},
		{
			// aguard's own config key. A tool that redacts its own documentation is telling
			// the operator its output cannot be trusted to be readable.
			name: "authority is not auth",
			in:   "  authority: escalate",
			want: "  authority: escalate",
		},
		{
			// Whitespace-only separation also fits English, so it keeps the 12-char floor.
			name: "prose keeps the higher floor",
			in:   "the secret sauce is documented above",
			want: "the secret sauce is documented above",
		},
		{
			name: "language literals are not credentials",
			in:   "auth: true, token: null, password: none",
			want: "auth: true, token: null, password: none",
		},
		{
			name: "ordinary config is untouched",
			in:   "timeout_seconds = 30",
			want: "timeout_seconds = 30",
		},
		{
			// --secret is a flag; --secrets-file is a filename, and the boundary is the `[=\s]`.
			name: "flag prefix must not match a longer flag",
			in:   "app --secrets-file config.yaml",
			want: "app --secrets-file config.yaml",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			if tc.want != "" && got != tc.want {
				t.Fatalf("Redact(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
			}
			for _, g := range tc.gone {
				if strings.Contains(got, g) {
					t.Errorf("Redact(%q) leaked %q: %q", tc.in, g, got)
				}
			}
			for _, k := range tc.kept {
				if !strings.Contains(got, k) {
					t.Errorf("Redact(%q) lost %q, which the reader needs: %q", tc.in, k, got)
				}
			}
		})
	}
}

// TestRedact_Idempotent: Redact runs on paths that are already redacted (model output is
// re-redacted as defense in depth, judge.go:157), so a second pass must be a no-op. A pass
// that re-matches its own <REDACTED> marker would erode the surrounding line one call at a time.
func TestRedact_Idempotent(t *testing.T) {
	for _, in := range []string{
		"password=hunter2",
		`-H "Authorization: Bearer abc123def456ghi"`,
		"curl -u admin:s3cr3t https://api.example.com",
		"MY_THING=Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MA==",
		"export ANTHROPIC_API_KEY=sk-ant-abcdefghijklmnopqrstuvwxyz123456",
		`token = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"`,
		"postgres://user:s3cr3t@db.example.com:5432/x",
	} {
		once := Redact(in)
		if twice := Redact(once); twice != once {
			t.Errorf("Redact not idempotent for %q:\n  once  %q\n  twice %q", in, once, twice)
		}
	}
}
