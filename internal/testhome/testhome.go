// SPDX-License-Identifier: MIT
// Package testhome gives every test binary in this module a home of its own (P-075).
//
// The code under test reads the developer's machine through three environment variables:
// HOME (the default scan root, `~/` in config paths, the Downloads folder), XDG_CONFIG_HOME
// (the user-level config.yaml, the key file, the reports folder) and CLAUDE_CONFIG_DIR (the
// default root). A test that leaves them alone runs against whatever the developer has there:
// a config.yaml naming a provider this build does not accept turned 71 tests in
// cmd/aguard red for reasons that had nothing to do with the code under test, and a valid one
// would silently have changed what they test.
//
// Invariant: once Main has started, no test in a package that calls it can see the HOME,
// XDG_CONFIG_HOME or CLAUDE_CONFIG_DIR the binary was started with. Every package under cmd/
// and internal/ that has tests calls it from TestMain (TestEveryTestPackageIsolatesTheHome);
// TestIsolation_RealHomeConfigIsNotSeen in cmd/aguard proves it on a planted config.
//
// This package is imported by tests only and must stay free of module imports: internal/config
// tests import it, so importing internal/config here would be a cycle.
package testhome

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// original is the environment the test binary started with, captured before Main replaces
// the home. Written once, before any test runs.
var original []string

// Main is the body of a package's TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }
//
// It points HOME at a fresh empty directory, XDG_CONFIG_HOME at that home's .config, unsets
// CLAUDE_CONFIG_DIR, runs the tests and removes the directory. A test that needs a particular
// home still sets its own with t.Setenv; this only decides what a test that sets nothing sees.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "aguard-testhome-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testhome: cannot create a home for the tests:", err)
		return 2
	}
	defer os.RemoveAll(dir)
	if err := isolate(dir); err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		return 2
	}
	return m.Run()
}

// isolate does Main's environment change under dir. The directory is resolved first: macOS
// puts the temp root under the /var → /private/var symlink, and a home that is a symlinked
// path makes engine evidence paths differ by host (see the golden tests).
func isolate(dir string) error {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	home := filepath.Join(dir, "home")
	xdg := filepath.Join(home, ".config")
	if err := os.MkdirAll(xdg, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", xdg, err)
	}
	original = os.Environ()
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg} {
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("set %s: %w", k, err)
		}
	}
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		return fmt.Errorf("unset CLAUDE_CONFIG_DIR: %w", err)
	}
	return nil
}

// OriginalEnviron is the environment the test binary started with, for the toolchain
// processes a test starts — `go build`, `git` — whose own configuration (build and module
// caches, GOENV, core.excludesFile) lives under the developer's home. Never for aguard itself,
// in-process or built: that is what Main exists to keep away from the developer's home.
// Before Main has run it is os.Environ().
func OriginalEnviron() []string {
	if original == nil {
		return os.Environ()
	}
	return slices.Clone(original)
}
