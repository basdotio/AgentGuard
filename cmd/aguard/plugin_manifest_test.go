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
	if mk.Name != "guard" {
		t.Errorf("marketplace name = %q, want the public repo name \"guard\" (the desktop looks it up by repo name)", mk.Name)
	}
	if len(mk.Plugins) != 1 || mk.Plugins[0].Name != pluginBundleName {
		t.Fatalf("marketplace must list exactly the %s plugin, got %+v", pluginBundleName, mk.Plugins)
	}
	if plugin.Version == "" || mk.Plugins[0].Version != plugin.Version {
		t.Errorf("marketplace entry version %q must equal plugin.json version %q", mk.Plugins[0].Version, plugin.Version)
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
