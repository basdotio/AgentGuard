// SPDX-License-Identifier: MIT
package testhome

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

// envValue is the last value of key in an os.Environ()-shaped list, as the process reads it.
func envValue(env []string, key string) (string, bool) {
	val, ok := "", false
	for _, kv := range env {
		if k, v, found := strings.Cut(kv, "="); found && k == key {
			val, ok = v, true
		}
	}
	return val, ok
}

// TestMain_MovesTheHomeAway: inside a test, HOME, XDG_CONFIG_HOME and CLAUDE_CONFIG_DIR are
// none of the values the binary was started with, the home is an empty directory of its own,
// and the user-level config directory is inside it — so a config.yaml in the developer's own
// home cannot be what a test reads.
func TestMain_MovesTheHomeAway(t *testing.T) {
	home, xdg := os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME")
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME"} {
		if was, ok := envValue(OriginalEnviron(), key); ok && was != "" && was == os.Getenv(key) {
			t.Errorf("%s is still the value the binary started with: %s", key, was)
		}
	}
	if v, ok := os.LookupEnv("CLAUDE_CONFIG_DIR"); ok {
		t.Errorf("CLAUDE_CONFIG_DIR is still set: %q", v)
	}
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Errorf("os.UserHomeDir() = %q, %v; want the isolated home %q", got, err, home)
	}
	if want := filepath.Join(home, ".config"); xdg != want {
		t.Errorf("XDG_CONFIG_HOME = %q, want %q", xdg, want)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".config" {
		t.Errorf("the isolated home holds more than its empty .config: %v", entries)
	}
	if resolved, err := filepath.EvalSymlinks(home); err != nil || resolved != home {
		t.Errorf("the isolated home is not a resolved path: %q → %q (%v)", home, resolved, err)
	}
}

// TestOriginalEnviron_IsTheStartingEnvironment: the toolchain environment still carries the
// home the binary was started with, and a caller cannot change it for the next one.
func TestOriginalEnviron_IsTheStartingEnvironment(t *testing.T) {
	if _, ok := envValue(OriginalEnviron(), "HOME"); !ok {
		t.Skip("the binary was started without HOME")
	}
	if got, _ := envValue(OriginalEnviron(), "HOME"); got == os.Getenv("HOME") {
		t.Errorf("OriginalEnviron carries the isolated HOME %q", got)
	}
	env := OriginalEnviron()
	env[0] = "MUTATED=1"
	if OriginalEnviron()[0] == "MUTATED=1" {
		t.Error("OriginalEnviron hands out its own slice")
	}
}

// testMainRE is the TestMain every test package must have; the package itself calls Main
// unqualified.
var testMainRE = regexp.MustCompile(`func TestMain\(m \*testing\.M\) \{\s*os\.Exit\((testhome\.)?Main\(m\)\)\s*\}`)

// TestEveryTestPackageIsolatesTheHome: every directory under cmd/ and internal/ that holds
// tests has a TestMain that runs them through Main. Isolating only the packages that read the
// home today is a list that drifts: collect, judge and config read it directly, and every
// package that imports collect reaches it. A new package with tests and no TestMain fails
// here, before it can read a developer's config.
func TestEveryTestPackageIsolatesTheHome(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	withTests, isolated := map[string]bool{}, map[string]bool{}
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, "_test.go") {
				return nil
			}
			dir, _ := filepath.Rel(root, filepath.Dir(p))
			withTests[dir] = true
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if testMainRE.Match(b) {
				isolated[dir] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var missing []string
	for dir := range withTests {
		if !isolated[dir] {
			missing = append(missing, dir)
		}
	}
	sort.Strings(missing)
	for _, dir := range missing {
		t.Errorf("%s has tests but no TestMain running them through testhome.Main; add a main_test.go with\n"+
			"\tfunc TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }", dir)
	}
	// Positive control: the walk saw this package and cmd/aguard, or it proves nothing.
	for _, dir := range []string{filepath.Join("internal", "testhome"), filepath.Join("cmd", "aguard")} {
		if !withTests[dir] {
			t.Errorf("control: the walk did not see %s", dir)
		}
	}
}
