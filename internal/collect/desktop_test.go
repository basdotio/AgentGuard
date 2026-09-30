// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// desktopStore lays out Claude Desktop's plugin store under a temporary home, mirroring the layout
// observed on 2026-09-04 (see desktop.go): one rpm bundle named by manifest, and a skills bundle
// holding two skills. It returns home, the config root, and the two bundle directories.
func desktopStore(t *testing.T) (home, root, pluginDir, skillsBundle string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, ".claude")
	mustWrite(t, filepath.Join(root, "settings.json"), `{"permissions":{"allow":[]}}`)

	base := filepath.Join(home, desktopSessionsDir)
	rpm := filepath.Join(base, "acct-1", "sess-1", "rpm")
	pluginDir = filepath.Join(rpm, "plugin_01AAA")
	mustWrite(t, filepath.Join(rpm, "manifest.json"),
		`{"plugins":[{"id":"plugin_01AAA","name":"agentguard","marketplaceName":"guard","installedBy":"user"}]}`)
	mustWrite(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), `{"name":"agentguard","version":"0.3.0"}`)
	mustWrite(t, filepath.Join(pluginDir, "skills", "audit", "SKILL.md"), "---\nname: audit\n---\nscan things\n")

	skillsBundle = filepath.Join(base, "skills-plugin", "sess-1", "acct-1")
	mustWrite(t, filepath.Join(skillsBundle, ".claude-plugin", "plugin.json"), `{"name":"anthropic-skills","version":"1.0.0"}`)
	mustWrite(t, filepath.Join(skillsBundle, "manifest.json"),
		`{"skills":[{"skillId":"docx","creatorType":"anthropic"},{"skillId":"mine","creatorType":"user"}]}`)
	mustWrite(t, filepath.Join(skillsBundle, "skills", "docx", "SKILL.md"), "---\nname: docx\n---\n")
	mustWrite(t, filepath.Join(skillsBundle, "skills", "mine", "SKILL.md"), "---\nname: mine\n---\n")
	return home, root, pluginDir, skillsBundle
}

func desktopByKind(res Result, kind model.ArtifactKind) []model.ArtifactReport {
	var out []model.ArtifactReport
	for _, a := range res.Artifacts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// TestCollectDesktop_PluginsAndSkillsAreCollected: what the desktop app hands to the CLI as
// --plugin-dir is loaded in every session and must be in the inventory — with a canonical hash,
// named by the desktop's own manifest, and marked with the channel it came through so a report
// can tell a desktop install from a CLI one.
func TestCollectDesktop_PluginsAndSkillsAreCollected(t *testing.T) {
	_, root, pluginDir, _ := desktopStore(t)
	res := CollectAll(root)

	plugs := desktopByKind(res, model.KindPlugin)
	if len(plugs) != 1 {
		t.Fatalf("plugin artifacts = %d, want 1: %+v", len(plugs), plugs)
	}
	if want := "agentguard@guard via Claude Desktop (0.3.0)"; plugs[0].Name != want {
		t.Errorf("plugin name = %q, want %q", plugs[0].Name, want)
	}
	if real, _ := filepath.EvalSymlinks(pluginDir); plugs[0].Path != real {
		t.Errorf("plugin path = %q, want %q", plugs[0].Path, real)
	}
	if plugs[0].Hash == "" {
		t.Error("desktop plugin has no canonical tree hash")
	}

	var names []string
	for _, s := range desktopByKind(res, model.KindSkill) {
		names = append(names, s.Name)
		if s.Hash == "" {
			t.Errorf("desktop skill %s has no canonical tree hash", s.Name)
		}
	}
	if len(names) != 2 || names[0] != "docx (Claude Desktop)" || names[1] != "mine (Claude Desktop)" {
		t.Errorf("skill names = %v, want [docx (Claude Desktop) mine (Claude Desktop)]", names)
	}
	if res.Env.Plugins != 1 || res.Env.Skills != 2 {
		t.Errorf("env plugins/skills = %d/%d, want 1/2", res.Env.Plugins, res.Env.Skills)
	}
	for _, n := range res.Notes {
		if strings.Contains(n.Why, "Desktop") {
			t.Errorf("a clean store produced a note: %s — %s", n.RuleID, n.Why)
		}
	}
}

// TestCollectDesktop_AbsentStoreIsSilent: no desktop app (or a project-level root, whose "home"
// is the project directory) is the normal case and must cost nothing — no artifact, no note. A
// disclosure printed on every machine that never had the store would train operators to skip
// disclosures.
func TestCollectDesktop_AbsentStoreIsSilent(t *testing.T) {
	home := t.TempDir()
	var env model.EnvSummary
	out, notes := collectDesktop(home, &env)
	if len(out) != 0 || len(notes) != 0 {
		t.Fatalf("absent store: artifacts=%d notes=%d, want 0/0", len(out), len(notes))
	}
}

// TestCollectDesktop_UnparseableManifestStillCollectsBundles: the manifest only supplies names.
// Losing it must degrade the label, never the coverage — and must say so (PARSE-000).
func TestCollectDesktop_UnparseableManifestStillCollectsBundles(t *testing.T) {
	home, root, pluginDir, _ := desktopStore(t)
	mustWrite(t, filepath.Join(filepath.Dir(pluginDir), "manifest.json"), `{"plugins":[`)
	_ = home

	res := CollectAll(root)
	plugs := desktopByKind(res, model.KindPlugin)
	if len(plugs) != 1 {
		t.Fatalf("plugin artifacts = %d, want 1 (bundle must survive a broken manifest)", len(plugs))
	}
	if want := "plugin_01AAA@Claude Desktop (0.3.0)"; plugs[0].Name != want {
		t.Errorf("fallback name = %q, want %q", plugs[0].Name, want)
	}
	var parseNote bool
	for _, n := range res.Notes {
		if n.RuleID == "PARSE-000" && strings.Contains(n.Title, "Claude Desktop") {
			parseNote = true
		}
	}
	if !parseNote {
		t.Error("broken desktop manifest was not disclosed as PARSE-000")
	}
}

// TestCollectDesktop_EscapingBundleRefused: a store entry is a path the scanner did not choose,
// so it gets the installPath containment — resolved, and refused (not silently) when it lands
// outside home.
func TestCollectDesktop_EscapingBundleRefused(t *testing.T) {
	home, root, pluginDir, _ := desktopStore(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	mustWrite(t, filepath.Join(outside, ".claude-plugin", "plugin.json"), `{"name":"evil"}`)
	mustWrite(t, filepath.Join(outside, "SKILL.md"), "---\nname: evil\n---\n")
	if err := os.Symlink(outside, filepath.Join(filepath.Dir(pluginDir), "plugin_02EVIL")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	_ = home

	res := CollectAll(root)
	for _, a := range desktopByKind(res, model.KindPlugin) {
		if strings.Contains(a.Name, "02EVIL") || a.Path == outside {
			t.Errorf("escaping bundle was collected: %+v", a)
		}
	}
	var scoped bool
	for _, n := range res.Notes {
		if n.RuleID == "SCOPE-001" && strings.Contains(n.Title, "Claude Desktop") {
			scoped = true
		}
	}
	if !scoped {
		t.Error("escaping desktop bundle was skipped without a SCOPE-001 note")
	}
}

// TestCollectDesktop_PluginHooksAreAudited: a desktop-installed plugin's hooks get the same
// per-(event, matcher, command) artifacts an installed plugin's do — hooks are the surface that
// runs shell silently, and the desktop is the channel a non-developer installs through.
func TestCollectDesktop_PluginHooksAreAudited(t *testing.T) {
	_, root, pluginDir, _ := desktopStore(t)
	mustWrite(t, filepath.Join(pluginDir, "hooks", "hooks.json"),
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi"}]}]}`)

	res := CollectAll(root)
	var hook *model.ArtifactReport
	for _, a := range desktopByKind(res, model.KindHook) {
		if strings.Contains(a.Name, "Claude Desktop") {
			hook = &a
		}
	}
	if hook == nil {
		t.Fatal("desktop plugin hook was not collected as its own artifact")
	}
	if !strings.HasPrefix(hook.Name, "PreToolUse[Bash]#1") {
		t.Errorf("hook name = %q, want PreToolUse[Bash]#1 (plugin … via Claude Desktop)", hook.Name)
	}
	if res.Env.Hooks != 1 {
		t.Errorf("env.hooks = %d, want 1", res.Env.Hooks)
	}
}

// TestPluginPaths_IncludesDesktopBundles: the gate resolves `plugin:skill` through PluginPaths.
// A bundle only the desktop installed used to resolve to nothing (GATE-000 on every load); now
// it resolves to what actually loads — and an installed_plugins.json entry of the same bundle
// name still wins, so the CLI channel's resolution is unchanged.
func TestPluginPaths_IncludesDesktopBundles(t *testing.T) {
	home, root, pluginDir, _ := desktopStore(t)
	real, _ := filepath.EvalSymlinks(pluginDir)

	got := PluginPaths(root, home)
	if got["agentguard"] != real {
		t.Fatalf("PluginPaths[agentguard] = %q, want desktop bundle %q", got["agentguard"], real)
	}

	cli := filepath.Join(root, "plugins", "cache", "mkt", "agentguard", "9.9.9")
	mustWrite(t, filepath.Join(cli, "skills", "s", "SKILL.md"), "---\nname: s\n---\n")
	installedPlugins(t, root, "agentguard@mkt", cli)
	cliReal, _ := filepath.EvalSymlinks(cli)
	if got := PluginPaths(root, home)["agentguard"]; got != cliReal {
		t.Errorf("with both channels installed PluginPaths[agentguard] = %q, want the CLI install %q", got, cliReal)
	}
}

// TestCollectDesktop_BundledMCPServersAreCounted: same rule for a desktop-installed plugin.
func TestCollectDesktop_BundledMCPServersAreCounted(t *testing.T) {
	_, root, pluginDir, _ := desktopStore(t)
	mustWrite(t, filepath.Join(pluginDir, ".mcp.json"), `{"mcpServers":{"design":{"type":"http","url":"https://mcp.example.com/"}}}`)
	res := CollectAll(root)
	var names []string
	for _, a := range res.Artifacts {
		if a.Kind == model.KindMCP {
			names = append(names, a.Name)
		}
	}
	if len(names) != 1 || !strings.Contains(names[0], "design (plugin agentguard@guard via Claude Desktop") {
		t.Fatalf("desktop plugin MCP servers = %v", names)
	}
	if res.Env.MCPServers != 1 {
		t.Errorf("env.mcp_servers = %d, want 1", res.Env.MCPServers)
	}
}

// TestCollectDesktop_OlderSessionBundlesAreNotDoubleCounted: the desktop leaves one skills bundle
// per session behind. Identical copies collapse to one artifact (the newest bundle's); a copy
// whose content differs is a different thing and stays.
func TestCollectDesktop_OlderSessionBundlesAreNotDoubleCounted(t *testing.T) {
	home, root, _, newest := desktopStore(t)
	older := filepath.Join(home, desktopSessionsDir, "skills-plugin", "sess-0", "acct-1")
	mustWrite(t, filepath.Join(older, "manifest.json"), `{"lastUpdated": 1, "skills":[{"skillId":"docx"},{"skillId":"mine"}]}`)
	mustWrite(t, filepath.Join(older, "skills", "docx", "SKILL.md"), "---\nname: docx\n---\n")             // identical to the newest
	mustWrite(t, filepath.Join(older, "skills", "mine", "SKILL.md"), "---\nname: mine\n---\nolder text\n") // differs
	mustWrite(t, filepath.Join(newest, "manifest.json"), `{"lastUpdated": 2, "skills":[{"skillId":"docx","creatorType":"anthropic"},{"skillId":"mine","creatorType":"user"}]}`)

	res := CollectAll(root)
	var names []string
	for _, a := range res.Artifacts {
		if a.Kind == model.KindSkill {
			names = append(names, a.Name)
		}
	}
	if len(names) != 3 {
		t.Fatalf("skills = %v, want docx once and mine twice (contents differ)", names)
	}
	if res.Env.Skills != 3 {
		t.Errorf("env.skills = %d, want 3", res.Env.Skills)
	}
	// The kept docx is the newest bundle's copy.
	for _, a := range res.Artifacts {
		if a.Name == "docx (Claude Desktop)" && !strings.HasPrefix(a.Path, mustReal(t, newest)) {
			t.Errorf("docx kept from %s, want the newest bundle %s", a.Path, newest)
		}
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestCollectDesktop_OlderSessionPluginCopiesAreNotDoubleCounted: the rpm store is per session too.
func TestCollectDesktop_OlderSessionPluginCopiesAreNotDoubleCounted(t *testing.T) {
	home, root, pluginDir, _ := desktopStore(t)
	older := filepath.Join(home, desktopSessionsDir, "acct-1", "sess-0", "rpm")
	mustWrite(t, filepath.Join(older, "manifest.json"), `{"lastUpdated": 1, "plugins":[{"id":"plugin_01AAA","name":"agentguard","marketplaceName":"guard"}]}`)
	// The older copy differs from desktopStore's (version 0.2.0 vs 0.3.0), so it is a different thing.
	mustWrite(t, filepath.Join(older, "plugin_01AAA", ".claude-plugin", "plugin.json"), `{"name":"agentguard","version":"0.2.0"}`)
	mustWrite(t, filepath.Join(older, "plugin_01AAA", "skills", "audit", "SKILL.md"), "---\nname: audit\n---\nscan things\n")
	// The newest bundle's manifest gets a later stamp.
	mustWrite(t, filepath.Join(filepath.Dir(pluginDir), "manifest.json"), `{"lastUpdated": 2, "plugins":[{"id":"plugin_01AAA","name":"agentguard","marketplaceName":"guard"}]}`)

	res := CollectAll(root)
	var names []string
	for _, a := range res.Artifacts {
		if a.Kind == model.KindPlugin {
			names = append(names, a.Name)
		}
	}
	// Different bytes → both stay (they are different things). Make the older copy identical to
	// the newest and only one may remain — the newest bundle's.
	if len(names) != 2 {
		t.Fatalf("differing copies: %v, want both", names)
	}
	mustWrite(t, filepath.Join(older, "plugin_01AAA", ".claude-plugin", "plugin.json"), `{"name":"agentguard","version":"0.3.0"}`)
	res = CollectAll(root)
	names = names[:0]
	for _, a := range res.Artifacts {
		if a.Kind == model.KindPlugin {
			names = append(names, a.Name)
		}
	}
	if len(names) != 1 || res.Env.Plugins != 1 {
		t.Errorf("identical copies: %v (env.plugins=%d), want exactly one", names, res.Env.Plugins)
	}
}
