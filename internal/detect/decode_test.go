// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestDetect_DecodeThenExecute: a base64 payload decoded straight into a shell — `base64 -d
// | sh`, `eval $(… base64 -d)`, `exec(base64.b64decode(x))` — hides its curl-to-attacker behind the
// blob, so the line rules saw only `base64 -d` (OBF-001, medium) and the gate let it through. EXEC-011
// raises the SHAPE to high without decoding anything: static sees that decoded content is executed,
// which is code execution (dimension 4). The quiet table is the precision half — decoding to a file
// or into a JSON tool, or plain encoding, is not execution.
func TestDetect_DecodeThenExecute(t *testing.T) {
	const fm = "---\nname: s\ndescription: x\n---\n"
	fire := []struct{ name, body string }{
		{"echo blob piped through base64 -d into sh", "echo \"$CONFIG\" | base64 -d | sh\n"},
		{"macos base64 -D into bash", "echo 'L2Jpbi9iYXNo' | base64 -D | bash\n"},
		{"a decode piped into zsh", "cat payload.b64 | base64 --decode | zsh\n"},
		{"eval of a command-substituted decode", "eval $(echo \"Y3VybA==\" | base64 -d)\n"},
		{"eval of atob", "eval(atob('Y3VybCBldmls'))\n"},
		{"exec of a python b64decode", "import base64\nexec(base64.b64decode('aW1wb3J0IG9z'))\n"},
		{"node buffer-from eval", "eval(Buffer.from(s, 'base64').toString())\n"},
	}
	for _, tc := range fire {
		t.Run("fires/"+tc.name, func(t *testing.T) {
			f, ok := findingsFor(t, map[string]string{"s.sh": tc.body})["EXEC-011"]
			if !ok {
				t.Fatalf("EXEC-011 missing; got %v", keys(findingsFor(t, map[string]string{"s.sh": tc.body})))
			}
			if f.Severity != model.SevHigh || f.Dimension != 4 {
				t.Errorf("EXEC-011 = %s / dim %d, want high / 4", f.Severity, f.Dimension)
			}
		})
	}

	t.Run("fires/.mcp.json args", func(t *testing.T) {
		root, art := mcpConfigArtifact(t, "helper", `{"command":"sh","args":["-c","echo $X | base64 -d | sh"]}`)
		got, _ := New().Run(root, []model.ArtifactReport{art})
		if f, ok := ruleIDs(got[0].Findings)["EXEC-011"]; !ok || f.Severity != model.SevHigh {
			t.Errorf("EXEC-011 high missing on an MCP arg; got %v", keys(ruleIDs(got[0].Findings)))
		}
	})

	quiet := []struct{ name, body string }{
		{"decode to a file is not execution", "echo \"$B\" | base64 -d > out.bin\n"},
		{"decode into a json tool is not a shell", "cat x | base64 -d | jq .\n"},
		{"plain encoding", "base64 img.png > img.b64\n"},
		{"prose naming the command", fm + "To inspect it, run `base64 -d` on the blob and read the output.\n"},
		{"base64 help", "base64 --help\n"},
		{"decode into a pager", "cat x.b64 | base64 -d | less\n"},
	}
	for _, tc := range quiet {
		t.Run("quiet/"+tc.name, func(t *testing.T) {
			files := map[string]string{"s.sh": tc.body}
			if tc.body == fm+"To inspect it, run `base64 -d` on the blob and read the output.\n" {
				files = map[string]string{"SKILL.md": tc.body}
			}
			if _, ok := findingsFor(t, files)["EXEC-011"]; ok {
				t.Errorf("EXEC-011 fired on %s", tc.name)
			}
		})
	}
}
