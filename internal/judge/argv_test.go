// SPDX-License-Identifier: MIT
package judge

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// mcpPayload is the text the mcp-config pass would send for the one server in entry.
func mcpPayload(t *testing.T, entry string) string {
	t.Helper()
	cfg := writeFile(t, filepath.Join(t.TempDir(), ".claude.json"), `{"mcpServers":{"x":`+entry+`}}`)
	modes, _ := modesFor(model.ArtifactReport{Kind: model.KindMCP, Name: "x", Path: cfg})
	req, ok := modes[ModeMCPConfig]
	if !ok {
		t.Fatalf("no mcp-config pass planned for %s", entry)
	}
	return req.Behavior
}

// TestPlan_MCPArgvSecretsAreRedacted (P-036): an MCP server usually takes its credential on the command
// line, and in an args array the flag and its value are two elements. ConfigLines renders each as its
// own `args=` line, and redacting each line alone never sees `--api-key` next to the key, so a short
// key — under the entropy floor, no known prefix — went to the judge as written. The values here are
// made up, 14 to 20 characters. The announced value is written on its flag's line, the pair as the
// command line reads it. The payload must also be a fixed point of Redact: a second pass that changes
// it is a pass that found a secret the first one sent.
func TestPlan_MCPArgvSecretsAreRedacted(t *testing.T) {
	for _, c := range []struct {
		name, args, secret string
		want               []string
	}{
		{"api key", `["-y","@upstash/context7-mcp","--api-key","k7Qp2xLm9Rt4Vw8Z"]`, "k7Qp2xLm9Rt4Vw8Z",
			[]string{"args=-y", "args=@upstash/context7-mcp", "args=--api-key <REDACTED>"}},
		{"access token", `["-y","@supabase/mcp-server-supabase@latest","--access-token","sbp9Xk2Lq7Wm4Rt8Zv"]`, "sbp9Xk2Lq7Wm4Rt8Zv",
			[]string{"args=--access-token <REDACTED>"}},
		{"token", `["--token","T0k3nAbCdEf9876"]`, "T0k3nAbCdEf9876", []string{"args=--token <REDACTED>"}},
		{"password", `["--password","Pw4Rd9xQz2Lm7Kt"]`, "Pw4Rd9xQz2Lm7Kt", []string{"args=--password <REDACTED>"}},
		{"secret", `["--secret","S3cR3tVa1ue7Qx"]`, "S3cR3tVa1ue7Qx", []string{"args=--secret <REDACTED>"}},
		{"-u user:pass", `["-u","admin:Hunt3r2Pa55wd","https://api.example.com"]`, "Hunt3r2Pa55wd",
			[]string{"args=-u admin:<REDACTED>\nargs=https://api.example.com"}},
		{"--user user:pass", `["--user","admin:Hunt3r2Pa55wd","https://api.example.com"]`, "Hunt3r2Pa55wd",
			[]string{"args=--user admin:<REDACTED>"}},
		{"value with a space", `["--password","correct horse battery"]`, "horse", []string{"args=--password <REDACTED>"}},
		{"value with a quote", `["--api-key","ab'cd9Xk2Lq7W"]`, "cd9Xk2Lq7W", []string{"args=--api-key <REDACTED>"}},
	} {
		got := mcpPayload(t, `{"command":"npx","args":`+c.args+`}`)
		if strings.Contains(got, c.secret) {
			t.Errorf("%s: the value after the flag reached the judge:\n%s", c.name, got)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: payload is missing %q:\n%s", c.name, w, got)
			}
		}
		if again := detect.Redact(got); again != got {
			t.Errorf("%s: the payload is not a fixed point of Redact — a second pass still finds a secret:\n%s\n---\n%s",
				c.name, got, again)
		}
	}
}

// TestPlan_MCPExcerptWithoutSecretFlagsIsUnchanged is the reverse assertion of P-036: an entry with no
// flag announcing a credential sends exactly what it sent before — every ConfigLines line masked and
// redacted on its own, lead keys first. Non-secret flags keep the element after them, and a pair
// already written as one element (`--api-key=…`) stays the one-element case.
func TestPlan_MCPExcerptWithoutSecretFlagsIsUnchanged(t *testing.T) {
	for _, entry := range []string{
		`{"command":"npx","args":["-y","@acme/db-mcp@1.2.3","--verbose","plainword","--version","1.2.3","--port","8080",` +
			`"-u","root"],"env":{"DB_PASS":"hunter2","LOG_LEVEL":"debug"}}`,
		`{"command":"node","args":["server.js","--api-key=k7Qp2xLm9Rt4Vw8Z","positional"]}`,
		`{"command":"uvx","args":["mcp-server-git","--repository","/srv/repo"],"timeout":30}`,
		`{"type":"http","url":"https://mcp.example.com/x","headers":{"Authorization":"Bearer abcdefghijklmnop","X-Trace":"on"}}`,
	} {
		cfg := writeFile(t, filepath.Join(t.TempDir(), ".claude.json"), `{"mcpServers":{"x":`+entry+`}}`)
		var ref []string
		for _, l := range mcpLeadFirst(detect.MCPConfigLines(model.ArtifactReport{Kind: model.KindMCP, Name: "x", Path: cfg})) {
			ref = append(ref, egress{}.redact(maskCredentialValue(l)))
		}
		if got, want := mcpPayload(t, entry), strings.Join(ref, "\n"); got != want {
			t.Errorf("an entry without a secret flag changed:\n%s\n--- want (each line alone) ---\n%s", got, want)
		}
	}
}

// TestPlan_MCPWidenedFlagSecretsAreRedacted (P-040): a flag the patterns now recognise by its last word
// announces the element after it in the mcp-config payload too, through the same redact.Announced the
// P-036 rows above go through — no flag list of the judge's own. Before, `["--key", "<short key>"]` and
// `["--db-pass", "<pw>"]` went to the judge as two lines with the value in the clear. A path after
// `--private-key` is the exemption's: it is which key file the server reads, and the judge should see it.
func TestPlan_MCPWidenedFlagSecretsAreRedacted(t *testing.T) {
	for _, c := range []struct{ flag, secret string }{
		{"--key", "k7Qp2xLm9Rt4Vw8Z"},
		{"--private-key", "Pk9Xw2Lm7Qt4Vz"},
		{"--secret-key", "Sk3Lq8Vw2Rt5"},
		{"--db-pass", "Hx7Lq2Vw9Rt4"},
		{"--db-password", "hunter2"},
		{"--client-secret", "S3cR3tV"},
		{"--pat", "Pt8Kw3Lx9Qm2"},
		{"--bearer", "Br4Qx9Lm"},
		{"--credentials", "Cr7Vw2Lq9"},
	} {
		got := mcpPayload(t, `{"command":"srv","args":["--verbose","`+c.flag+`","`+c.secret+`","--port","8080"]}`)
		if strings.Contains(got, c.secret) {
			t.Errorf("%s: the value after the flag reached the judge:\n%s", c.flag, got)
		}
		if want := "args=" + c.flag + " <REDACTED>"; !strings.Contains(got, want) {
			t.Errorf("%s: payload is missing %q:\n%s", c.flag, want, got)
		}
		if !strings.Contains(got, "args=--port\nargs=8080") {
			t.Errorf("%s: the pair after it must keep its own lines:\n%s", c.flag, got)
		}
		if again := detect.Redact(got); again != got {
			t.Errorf("%s: the payload is not a fixed point of Redact:\n%s\n---\n%s", c.flag, got, again)
		}
	}
	got := mcpPayload(t, `{"command":"srv","args":["--private-key","~/.ssh/id_ed25519","--key-file","./k.pem"]}`)
	for _, w := range []string{"args=--private-key\nargs=~/.ssh/id_ed25519", "args=--key-file\nargs=./k.pem"} {
		if !strings.Contains(got, w) {
			t.Errorf("a key file path is evidence, not a secret: payload is missing %q:\n%s", w, got)
		}
	}
}
