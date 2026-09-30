// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectEnvironment_NeedsTwoSignals: the app-level marker (Cowork's runtime files under
// the config root) is machine-independent, so it can be exercised on any host. One such file
// is not enough; two is, once paired with a second family — here a second runtime file plus a
// third crosses the bar via the same family only if ≥2 distinct files are present.
func TestDetectEnvironment_NeedsTwoSignals(t *testing.T) {
	// A bare temp dir with nothing sandbox-like is local.
	if e := DetectEnvironment(t.TempDir()); e.Kind != EnvLocal {
		t.Errorf("empty root should be local, got %s (%v)", e.Kind, e.Signals)
	}

	// One managed-launcher file is a single signal (the family counts once), not enough.
	one := t.TempDir()
	must(t, filepath.Join(one, "policy-limits.json"), "{}")
	if e := DetectEnvironment(one); e.Kind != EnvLocal {
		t.Errorf("one marker should not flip to sandbox, got %s (%v)", e.Kind, e.Signals)
	}

	// Two of Cowork's runtime files trip the managed-launcher family — but that is still ONE
	// signal, so on a non-Linux host without container markers it stays local. This pins that
	// the family is corroboration, not a lone trigger.
	two := t.TempDir()
	must(t, filepath.Join(two, "policy-limits.json"), "{}")
	must(t, filepath.Join(two, "launcher-settings.json"), "{}")
	e := DetectEnvironment(two)
	// On a developer Mac/Linux with no container markers this is one signal → local. If the CI
	// host happens to be a container, it may legitimately have a second signal → sandbox. Accept
	// either, but require the managed-launcher signal to be recorded when its two files exist.
	found := false
	for _, s := range e.Signals {
		if s == "managed-launcher runtime files under the config root" {
			found = true
		}
	}
	if !found {
		t.Errorf("two launcher files must record the managed-launcher signal: %v", e.Signals)
	}
}

func must(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
