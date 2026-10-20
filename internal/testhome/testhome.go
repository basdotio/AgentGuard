// SPDX-License-Identifier: MIT
// Package testhome gives every test binary in this module a home of its own (P-075).
//
// The code under test reads the developer's machine through environment variables: HOME (the
// default scan root, `~/` in config paths, the Downloads folder), XDG_CONFIG_HOME (the
// user-level config.yaml, the key file, the reports folder), CLAUDE_CONFIG_DIR (the default
// root) and AGUARD_USAGE_LOG (the file every judge run appends a line to). A test that leaves
// them alone runs against whatever the developer has there: a config.yaml naming a provider
// this build does not accept turned 71 tests in cmd/bas-guard red for reasons that had nothing to
// do with the code under test (P-075), and a usage log set in the developer's shell gained 42
// lines of test traffic from one `go test ./...` (P-077). XDG_CACHE_HOME, XDG_STATE_HOME and
// XDG_DATA_HOME are read by nothing today; they are moved too, so the first default that lands
// there is already inside the temporary home (P-077).
//
// Invariant: once Main has started, no test in a package that calls it can see the HOME, the
// four XDG base directories, CLAUDE_CONFIG_DIR or AGUARD_USAGE_LOG the binary was started with.
// Every package in the module that has tests calls it from TestMain, baselines/ excepted for a
// stated reason (TestEveryTestPackageIsolatesTheHome); TestIsolation_RealHomeConfigIsNotSeen in
// cmd/bas-guard proves it on a planted config, and TestDefaultLocations_AreInsideTheTestHome there
// asks the product for every default location it resolves and requires each under Home().
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
// the home; home is the isolated home Main set up. Both written once, before any test runs.
var (
	original []string
	home     string
)

// xdgDirs are the XDG base-directory variables pointed into the isolated home, each at the
// place the specification gives as its default under HOME — so code that reads the variable and
// code that falls back to HOME see the same layout. Only .config is created (P-075 code reads
// it); the rest are pointed at and left absent, as on a machine that never wrote there.
var xdgDirs = []struct{ name, rel string }{
	{"XDG_CONFIG_HOME", ".config"},
	{"XDG_CACHE_HOME", ".cache"},
	{"XDG_STATE_HOME", filepath.Join(".local", "state")},
	{"XDG_DATA_HOME", filepath.Join(".local", "share")},
}

// unsetVars are removed rather than redirected: CLAUDE_CONFIG_DIR because unset is what CI runs
// with (the default root then falls under the isolated HOME), AGUARD_USAGE_LOG because it names
// a file of the developer's that every judge run appends to (P-077), and the BAS_GUARD_ names that
// win over AGUARD_ ones since the rename (P-082): a developer's own key or log must not change what
// a test reads. A test that exercises one sets it itself with t.Setenv.
var unsetVars = []string{"CLAUDE_CONFIG_DIR", "AGUARD_USAGE_LOG", "BAS_GUARD_USAGE_LOG", "BAS_GUARD_LLM_KEY"}

// Main is the body of a package's TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }
//
// It points HOME at a fresh empty directory and the four XDG base directories inside it, unsets
// CLAUDE_CONFIG_DIR and AGUARD_USAGE_LOG, runs the tests and removes the directory. A test that
// needs a particular home still sets its own with t.Setenv; this only decides what a test that
// sets nothing sees.
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
	h := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(h, ".config"), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", h, err)
	}
	original = os.Environ()
	if err := os.Setenv("HOME", h); err != nil {
		return fmt.Errorf("set HOME: %w", err)
	}
	for _, x := range xdgDirs {
		if err := os.Setenv(x.name, filepath.Join(h, x.rel)); err != nil {
			return fmt.Errorf("set %s: %w", x.name, err)
		}
	}
	for _, name := range unsetVars {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset %s: %w", name, err)
		}
	}
	home = h
	return nil
}

// Home is the isolated home Main set up, or "" when Main has not run in this binary. A guard
// compares against this, not against $HOME: in a package without TestMain, $HOME is the
// developer's, and every default location would be "under the home" (P-077).
func Home() string { return home }

// OriginalEnviron is the environment the test binary started with, for the toolchain
// processes a test starts — `go build`, `git` — whose own configuration (build and module
// caches, GOENV, core.excludesFile) lives under the developer's home. Never for bas-guard itself,
// in-process or built: that is what Main exists to keep away from the developer's home.
// Before Main has run it is os.Environ().
func OriginalEnviron() []string {
	if original == nil {
		return os.Environ()
	}
	return slices.Clone(original)
}
