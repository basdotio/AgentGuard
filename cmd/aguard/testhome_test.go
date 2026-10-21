// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/testhome"
)

// TestMain runs this package's tests with a home of their own, never the developer's (P-075).
func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }

// isolationProbes are tests that load the user-level config through the default location
// (`--config` empty) and fail when that config is invalid — the symptom P-075 removes:
// a developer whose own config names a provider this build rejects saw every one of them red.
var isolationProbes = []string{"TestAdversarialCorpus", "TestE2E_StaticBaseline", "TestGateEndToEnd"}

// TestIsolation_RealHomeConfigIsNotSeen plants an invalid config.yaml in the two places a
// test process would find the user's own — $XDG_CONFIG_HOME/aguard and $HOME/.config/aguard —
// then reruns this package's test binary with HOME and XDG_CONFIG_HOME pointing there, as a
// developer's shell would. The probes pass only if TestMain moved both away before any test
// ran (P-075). Running the binary again is the only way to hand a test process a "real" home
// without writing into the actual one.
func TestIsolation_RealHomeConfigIsNotSeen(t *testing.T) {
	planted := t.TempDir()
	home, xdg := filepath.Join(planted, "home"), filepath.Join(planted, "xdg")
	// The gate section, not the llm one: since P-084 an invalid llm section is set aside and a
	// static run goes ahead, so only a value every run reads makes the probes fail.
	const bad = "gate:\n  action: not-an-action-this-build-accepts\n"
	for _, p := range []string{
		filepath.Join(home, ".config", "aguard", "config.yaml"),
		filepath.Join(xdg, "aguard", "config.yaml"),
	} {
		mustWriteFile(t, p, bad)
		// Positive control: the planted file is one LoadUser refuses, so a probe that could
		// see it would fail — otherwise a green child proves nothing.
		if _, err := config.Load(p); err == nil {
			t.Fatalf("control: config.Load(%s) accepted the planted config; the test is blind", p)
		}
	}

	run := "^(" + strings.Join(isolationProbes, "|") + ")$"
	cmd := exec.Command(os.Args[0], "-test.run="+run, "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdg, "CLAUDE_CONFIG_DIR=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("with a planted config in HOME and XDG_CONFIG_HOME the probes fail — tests read the "+
			"developer's own config: %v\n%s", err, out)
	}
	for _, name := range isolationProbes {
		if !strings.Contains(string(out), "--- PASS: "+name+" ") {
			t.Errorf("probe %s did not run and pass in the child; output:\n%s", name, out)
		}
	}
}
