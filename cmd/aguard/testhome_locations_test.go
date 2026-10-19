// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/gate"
	"github.com/basdotio/AgentGuard/internal/testhome"
)

// inside reports whether p is dir or lies below it, by path: both are absolute and built from
// the same resolved home, so no symlink is resolved here.
func inside(p, dir string) bool {
	if p == "" || dir == "" || !filepath.IsAbs(p) {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// defaultLocation is one place the binary reads or writes when the operator names nothing.
type defaultLocation struct{ what, path string }

// defaultLocations asks the product, through the functions its commands call, for every
// default location it resolves (P-077). A path built here instead would only agree with itself;
// calling the resolvers is what notices one of them starting to read another variable.
// defaultReportPath creates its directory, which lands inside the test home.
func defaultLocations(t *testing.T) []defaultLocation {
	t.Helper()
	must := func(p string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	root := defaultRoot()
	locs := []defaultLocation{
		{"config file (every command reads it; llm setup writes it)", must(config.DefaultPath())},
		{"judge key file (llm setup writes it)", must(config.DefaultKeyPath())},
		{"--report directory", filepath.Dir(must(defaultReportPath(time.Unix(0, 0))))},
		{"default --root", root},
		{"gate approvals and pending verdicts", gate.ApprovalsPath(root)},
		{"settings.json hook install edits, and its backups beside it", gate.SettingsPath(root)},
		{"clean's quarantine and manifest", filepath.Join(root, collect.TrashDir)},
		{"default --inbox (Downloads)", defaultInbox()},
		{"~ in a configured path (api_key_file, --inbox)", must(config.ExpandHome("~/x"))},
		{"os.UserHomeDir (the home the judge scrubs, the sandbox note)", must(os.UserHomeDir())},
		{"os.UserConfigDir (read by nothing today)", must(os.UserConfigDir())},
		{"os.UserCacheDir (read by nothing today)", must(os.UserCacheDir())},
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		locs = append(locs, defaultLocation{"$" + key, os.Getenv(key)})
	}
	// Everything a root scan reads beside the root: the user-level MCP config, the Claude
	// Desktop store and session cache.
	for _, l := range scanLocations(root) {
		locs = append(locs, defaultLocation{"scan location " + l.Name, l.Path})
	}
	return locs
}

// TestDefaultLocations_AreInsideTheTestHome: every default location the binary resolves lies
// under the temporary home testhome.Main set up, and the usage log — the one location given only
// by an environment variable — is unset (P-077). It compares against testhome.Home(), not $HOME:
// without this package's TestMain, $HOME is the developer's and everything would be "inside" it.
func TestDefaultLocations_AreInsideTheTestHome(t *testing.T) {
	home := testhome.Home()
	if home == "" {
		t.Fatal("testhome.Main did not run: this package's TestMain must call it, or every default below is the developer's")
	}
	// Control: the check can fail — the home the binary was started with is not inside.
	for _, kv := range testhome.OriginalEnviron() {
		if was, ok := strings.CutPrefix(kv, "HOME="); ok && was != "" && inside(was, home) {
			t.Fatalf("control: the original HOME %q counts as inside the test home %q; the check is blind", was, home)
		}
	}
	locs := defaultLocations(t)
	if len(locs) < 16 {
		t.Fatalf("control: only %d locations resolved; the table lost rows", len(locs))
	}
	for _, l := range locs {
		if !inside(l.path, home) {
			t.Errorf("%s resolves to %q, outside the test home %s", l.what, l.path, home)
		}
	}
	if v, ok := os.LookupEnv(usageLogEnv); ok {
		t.Errorf("%s is set to %q: every judge run in this package would append to it", usageLogEnv, v)
	}
	if v, ok := os.LookupEnv("CLAUDE_CONFIG_DIR"); ok {
		t.Errorf("CLAUDE_CONFIG_DIR is set to %q: it, not the test home, would be the default root", v)
	}
}

// TestIsolatedHome_OwnConfigIsStillRefused is the reverse assertion: the test home removes the
// developer's config, not config loading (P-077). A config the test writes itself, naming a
// provider this build does not accept, is refused through --config, and at the default location
// inside the test home — with XDG_CONFIG_HOME set, and empty so the HOME fallback is the one
// read. With neither, the scan runs on defaults.
func TestIsolatedHome_OwnConfigIsStillRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := scanEnv(root, scanOpts{quiet: true}); err != nil {
		t.Fatalf("no config anywhere: %v, want a scan on defaults", err)
	}
	const bad = "llm:\n  enabled: true\n  provider: bogus\n"
	refused := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), `"bogus"`) {
			t.Errorf("%s: err = %v, want the refusal naming the provider", what, err)
		}
	}

	own := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, own, bad)
	_, err := scanEnv(root, scanOpts{cfgPath: own, quiet: true})
	refused("scan --config", err)
	_, err = checkTarget(root, scanOpts{cfgPath: own, quiet: true})
	refused("check --config", err)

	def, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !inside(def, testhome.Home()) {
		t.Fatalf("the default config %q is not inside the test home %q", def, testhome.Home())
	}
	mustWriteFile(t, def, bad)
	t.Cleanup(func() { _ = os.Remove(def) }) // the home is shared by every test in this binary
	_, err = scanEnv(root, scanOpts{quiet: true})
	refused("the default location under $XDG_CONFIG_HOME", err)
	t.Setenv("XDG_CONFIG_HOME", "")
	if fallback, _ := config.DefaultPath(); fallback != def {
		t.Fatalf("with XDG_CONFIG_HOME empty the default is %q, want the HOME fallback to be the same file %q", fallback, def)
	}
	_, err = scanEnv(root, scanOpts{quiet: true})
	refused("the default location under $HOME/.config", err)
}
