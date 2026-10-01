// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestMarketplaceEntryVersionMatchesPlugin: the marketplace entry carries the plugin version
// because that is the field the Claude Desktop update check reads (its account-level roster
// exposes it as availableVersion; without it the desktop reports "No changes since the last
// release" forever). plugin.json is what the CLI reads and what `aguard version` compares the
// binary against. The two must never drift: Claude Code uses plugin.json silently when they
// differ, so a stale marketplace value would mislead one channel without breaking the other.
func TestMarketplaceEntryVersionMatchesPlugin(t *testing.T) {
	root := filepath.Join("..", "..")
	var plugin struct {
		Version string `json:"version"`
	}
	b, err := os.ReadFile(filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &plugin); err != nil {
		t.Fatal(err)
	}
	var mk struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	b, err = os.ReadFile(filepath.Join(root, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &mk); err != nil {
		t.Fatal(err)
	}
	// Against the constant, not a literal: `aguard version` tells stale installs to switch to
	// homeMarketplace, so the file and that hint must name the same marketplace.
	if mk.Name != homeMarketplace || !strings.HasSuffix(homeMarketplaceRepo, "/"+homeMarketplace) {
		t.Errorf("marketplace name = %q, want %q, the public repo name in %q (the desktop looks it up by repo name)", mk.Name, homeMarketplace, homeMarketplaceRepo)
	}
	if len(mk.Plugins) != 1 || mk.Plugins[0].Name != pluginBundleName {
		t.Fatalf("marketplace must list exactly the %s plugin, got %+v", pluginBundleName, mk.Plugins)
	}
	if plugin.Version == "" || mk.Plugins[0].Version != plugin.Version {
		t.Errorf("marketplace entry version %q must equal plugin.json version %q", mk.Plugins[0].Version, plugin.Version)
	}
}

// TestPluginNameCannotCollideInTheInstallCache: issues/022. Claude Code stages a plugin at
// cache/<plugin.json name> and then moves it to cache/<marketplace>/<plugin>/<version>; its check
// for "target inside the staging dir" is a case-sensitive string compare. Through v0.16.0 the plugin
// was `agentguard` in marketplace `AgentGuard`: on a case-insensitive volume (macOS by default) the
// two are one directory, the check misses, and `claude plugin install` fails with EINVAL and leaves
// the plugin unregistered. The marketplace name cannot move (the desktop keys it by repo name), so
// these pin the plugin name — the one name this repository can change.
func TestPluginNameCannotCollideInTheInstallCache(t *testing.T) {
	root := filepath.Join("..", "..")
	var plugin struct {
		Name string `json:"name"`
	}
	b, err := os.ReadFile(filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &plugin); err != nil {
		t.Fatal(err)
	}
	// The skill namespace (`<name>:skill`) comes from plugin.json; the gate looks a scoped skill up
	// by the plugin ID's bundle half, which is the marketplace entry name. Two different names make
	// every scoped skill unresolvable — GATE-000 on every load.
	if plugin.Name != pluginBundleName {
		t.Errorf("plugin.json name = %q, want %q: it must equal the marketplace entry name, or the gate cannot resolve a scoped skill", plugin.Name, pluginBundleName)
	}
	// The staging dir is cache/<plugin.json name>; it must not be any marketplace's cache dir on a
	// case-insensitive volume: not this one, and not the retired `guard` (basdotio/guard), whose
	// cache dir still exists on machines that never removed it — the staging step rm -rf's it.
	for _, mkName := range []string{homeMarketplace, "guard"} {
		if strings.EqualFold(plugin.Name, mkName) {
			t.Errorf("plugin name %q equals marketplace name %q up to case: `claude plugin install` fails on macOS (issues/022)", plugin.Name, mkName)
		}
	}
	if strings.EqualFold(pluginBundleName, legacyBundleName) {
		t.Errorf("pluginBundleName %q and legacyBundleName %q must differ up to case, or a legacy install is read as current", pluginBundleName, legacyBundleName)
	}
}

// TestSkillFrontmatterFitsDesktopLimits: Claude Desktop validates each skill's frontmatter
// against name ≤ 64 and description ≤ 1024 characters (limits table in the app bundle:
// nameChars:64, descriptionChars:1024, skillsPerEntry:20) and silently drops a skill that
// exceeds them — the plugin card then lists fewer skills than the directory holds. The audit
// skill sat at 1159 characters from v0.4.0 to v0.5.0 and was never listed on the desktop
// (2026-09-08). The CLI has no such limit, which is why nothing else caught it.
func TestSkillFrontmatterFitsDesktopLimits(t *testing.T) {
	skills, err := filepath.Glob(filepath.Join("..", "..", "plugin", "skills", "*", "SKILL.md"))
	if err != nil || len(skills) == 0 {
		t.Fatalf("no skills found: %v", err)
	}
	if len(skills) > 20 {
		t.Errorf("%d skills; the desktop lists at most 20 per plugin", len(skills))
	}
	for _, path := range skills {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fm := frontmatterFields(string(b))
		if n := utf8.RuneCountInString(fm["name"]); n == 0 || n > 64 {
			t.Errorf("%s: name is %d characters, want 1..64", path, n)
		}
		if n := utf8.RuneCountInString(fm["description"]); n == 0 || n > 1024 {
			t.Errorf("%s: description is %d characters, want 1..1024 (the desktop drops the skill otherwise)", path, n)
		}
	}
}

// frontmatterFields reads the `key: value` lines between the first two `---` lines, stripping
// one level of surrounding double quotes. Enough for the three files under plugin/skills.
func frontmatterFields(s string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return out
	}
	for _, l := range lines[1:] {
		if l == "---" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}
