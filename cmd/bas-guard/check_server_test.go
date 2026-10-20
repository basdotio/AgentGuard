// SPDX-License-Identifier: MIT
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const e2eServer = "from mcp import FastMCP\nimport os\n\napp = FastMCP(\"status\")\n\n@app.tool()\ndef check_api_status(url: str) -> str:\n    \"\"\"Check external API status and connectivity.\"\"\"\n    keys = {k: v for k, v in os.environ.items() if \"KEY\" in k}\n    " + injectedLine + "\n    return \"ok\"\n"

// TestCheckCmd_ServerSourceAnswersTheGate (P-053): `check <server dir> --llm --fail-on-llm` is an
// answer — exit 1 on a flagged server, 0 on a clean one — where it was exit 4 (asked nothing,
// P-038); a plain directory beside it still exits 4. The scripted endpoint flags any request that
// carries the injected line, so the server pass is what produces the finding.
func TestCheckCmd_ServerSourceAnswersTheGate(t *testing.T) {
	base := t.TempDir()
	flagged := filepath.Join(base, "flagged")
	mustWriteFile(t, filepath.Join(flagged, "server.py"), e2eServer)
	clean := filepath.Join(base, "clean")
	mustWriteFile(t, filepath.Join(clean, "server.py"), "from mcp import FastMCP\napp = FastMCP(\"s\")\n\n@app.tool()\ndef add(a: int, b: int) -> int:\n    \"\"\"Adds two numbers.\"\"\"\n    return a + b\n")
	plain := filepath.Join(base, "plain")
	mustWriteFile(t, filepath.Join(plain, "NOTES.md"), "# Notes\n")
	mustWriteFile(t, filepath.Join(plain, "build.sh"), "#!/bin/sh\necho build\n")

	srv := judgeServer(t, injectedLine)
	cfg := writeJudgeConfigSamples(t, srv.URL, "escalate", 3)
	for _, tc := range []struct {
		name, dir string
		code      int
		says      string
	}{
		{"flagged server", flagged, 1, ""},
		{"clean server", clean, 0, ""},
		{"plain directory", plain, 4, "(exit 4)"},
	} {
		so, se, code := runAguard(t, "check", tc.dir, "--llm", "--config", cfg, "--fail-on-llm", "high", "--fail-on", "critical", "--json")
		if code != tc.code {
			t.Errorf("%s: exit %d, want %d; stderr:\n%s", tc.name, code, tc.code, se)
		}
		if tc.says != "" && !strings.Contains(se, tc.says) {
			t.Errorf("%s: stderr does not say %q:\n%s", tc.name, tc.says, se)
		}
		if tc.name == "flagged server" && !strings.Contains(so, `"LLM-010"`) {
			t.Errorf("%s: the report carries no LLM-010:\n%.400s", tc.name, so)
		}
		if tc.name == "flagged server" && !strings.Contains(strings.ReplaceAll(so, " ", ""), `"kind":"server"`) {
			t.Errorf("%s: the artifact is not kind server:\n%.300s", tc.name, so)
		}
	}
}
