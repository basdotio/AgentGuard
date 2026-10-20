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

// TestMain_MovesTheHomeAway: inside a test, HOME, the four XDG base directories,
// CLAUDE_CONFIG_DIR and AGUARD_USAGE_LOG are none of the values the binary was started with,
// the home is an empty directory of its own, and every XDG directory is inside it at the place
// the base-directory specification gives as its default — so a config.yaml in the developer's
// own home cannot be what a test reads, and a usage log in the developer's shell cannot be what
// a test appends to (P-075, P-077).
func TestMain_MovesTheHomeAway(t *testing.T) {
	home := os.Getenv("HOME")
	if Home() == "" || Home() != home {
		t.Fatalf("Home() = %q, want the isolated HOME %q", Home(), home)
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if was, ok := envValue(OriginalEnviron(), key); ok && was != "" && was == os.Getenv(key) {
			t.Errorf("%s is still the value the binary started with: %s", key, was)
		}
	}
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "AGUARD_USAGE_LOG"} {
		if v, ok := os.LookupEnv(key); ok {
			t.Errorf("%s is still set: %q", key, v)
		}
	}
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Errorf("os.UserHomeDir() = %q, %v; want the isolated home %q", got, err, home)
	}
	for key, rel := range map[string]string{
		"XDG_CONFIG_HOME": ".config",
		"XDG_CACHE_HOME":  ".cache",
		"XDG_STATE_HOME":  filepath.Join(".local", "state"),
		"XDG_DATA_HOME":   filepath.Join(".local", "share"),
	} {
		if got, want := os.Getenv(key), filepath.Join(home, rel); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
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

// excludedTrees are top-level directories of the module whose tests do not run under Main, each
// with its reason. Anything not named here is walked, so a new top-level directory with tests is
// covered by default (P-077; P-075 walked cmd/ and internal/ only, and hack/ was outside).
var excludedTrees = map[string]string{
	// Its tests and its non-test code start the Go toolchain (go build, go list, go run
	// ./cmd/corpus, the scorer) with the inherited environment. Under the isolated home those
	// children get an empty build and module cache and lose the developer's GOENV — slow online,
	// failing offline — and steering the non-test spawns to OriginalEnviron would change
	// non-test code. In process it reads no aguard config, and the aguard it runs gets an
	// environment the adapter builds from nothing (HOME and XDG_CONFIG_HOME in its work
	// directory, no AGUARD_USAGE_LOG).
	"baselines": "starts the Go toolchain from non-test code; its aguard children get an environment built from nothing",
}

// skipDir reports whether the walk never descends into a directory of this name: hidden trees
// (.git, .claude worktrees), fixtures and third-party code are not this module's test packages.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor"
}

// TestEveryTestPackageIsolatesTheHome: every directory of the module that holds tests has a
// TestMain that runs them through Main, except the trees excludedTrees names. Isolating only the
// packages that read the home today is a list that drifts: collect, judge and config read it
// directly, and every package that imports collect reaches it. A new package with tests and no
// TestMain fails here, before it can read a developer's config.
func TestEveryTestPackageIsolatesTheHome(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	withTests, isolated := map[string]bool{}, map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if _, excluded := excludedTrees[rel]; excluded || (p != root && skipDir(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		dir := filepath.Dir(rel)
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
	var missing []string
	for dir := range withTests {
		if !isolated[dir] {
			missing = append(missing, dir)
		}
	}
	sort.Strings(missing)
	for _, dir := range missing {
		t.Errorf("%s has tests but no TestMain running them through testhome.Main; add, in a _test.go file,\n"+
			"\tfunc TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }", dir)
	}
	// Positive control: the walk saw this package, cmd/bas-guard and the hack/ tools, or it proves
	// nothing; and every excluded tree exists, so a rename cannot leave a stale exclusion behind.
	for _, dir := range []string{
		filepath.Join("internal", "testhome"), filepath.Join("cmd", "bas-guard"),
		filepath.Join("hack", "gen-rules"), filepath.Join("hack", "reputation-refresh"),
	} {
		if !withTests[dir] {
			t.Errorf("control: the walk did not see %s", dir)
		}
	}
	for dir := range excludedTrees {
		if fi, err := os.Stat(filepath.Join(root, dir)); err != nil || !fi.IsDir() {
			t.Errorf("excludedTrees names %s, which is not a directory of the module: %v", dir, err)
		}
	}
}
