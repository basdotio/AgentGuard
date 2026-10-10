// SPDX-License-Identifier: MIT
package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// The tests in this file assert the new fields through the JSON the report carries (`plugin`,
// `bundled_commands`, `bundled_agents`), so that they compile on a tree without them and fail for
// the reason that matters: no child artifact exists (P-044).

// pluginLoaderTree writes one plugin holding a row for every case of Claude Code 2.1.107's plugin
// loader, and returns the children a collector must produce for it: leaf name → (kind, path to hash,
// directory or not).
func pluginLoaderTree(t *testing.T, dir string) map[string]childWant {
	t.Helper()
	w := func(rel, body string) { mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body) }
	link := func(target, rel string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	w(".claude-plugin/plugin.json", `{"name":"demo","skills":"./more","commands":["./extra/one.md","../outside.md"],"agents":["./more-agents"]}`)
	w("skills/a/SKILL.md", "---\nname: a\ndescription: Formats tables.\n---\nFormat the table.\n")
	w("skills/b/SKILL.md", "---\nname: b\ndescription: Says hello.\n---\nSay hello.\n")
	w("skills/b/scripts/run.sh", "echo hi\n")
	w("skills/notaskill/README.md", "not a skill\n")
	w("shared/linked/SKILL.md", "---\nname: linked\ndescription: Linked skill.\n---\nLinked.\n")
	link(filepath.Join("..", "shared", "linked"), "skills/linked")
	w("commands/c1.md", "---\ndescription: Lists files.\n---\nList the files.\n")
	w("commands/sub/c2.md", "---\ndescription: Shows status.\n---\nShow git status.\n")
	w("commands/sk/SKILL.md", "---\nname: sk\ndescription: Skill-style command.\n---\nDo it.\n")
	w("commands/sk/other.md", "not loaded: the directory holds a SKILL.md\n")
	w("commands/notes.txt", "not markdown\n")
	link("c1.md", "commands/ln.md")
	w("agents/a1.md", "---\nname: a1\ndescription: Reviews code.\n---\nReview.\n")
	w("agents/team/a2.md", "---\nname: a2\ndescription: Plans.\n---\nPlan.\n")
	link("a1.md", "agents/ln.md")
	w("more/SKILL.md", "---\nname: more\ndescription: Declared in the manifest.\n---\nMore.\n")
	w("extra/one.md", "---\ndescription: Declared command.\n---\nOne.\n")
	w("more-agents/a3.md", "---\nname: a3\ndescription: Declared agent.\n---\nThree.\n")
	w("docs/ja/skills/a/SKILL.md", "---\nname: a\ndescription: Formats tables.\n---\nTranslated copy, never loaded.\n")
	w(".agents/skills/a/SKILL.md", "---\nname: a\ndescription: Mirror.\n---\nMirror copy, never loaded.\n")
	mustWrite(t, filepath.Join(filepath.Dir(dir), "outside.md"), "outside the plugin root\n")

	j := func(rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }
	return map[string]childWant{
		"a":       {model.KindSkill, j("skills/a"), true},
		"b":       {model.KindSkill, j("skills/b"), true},
		"linked":  {model.KindSkill, j("skills/linked"), true},
		"more":    {model.KindSkill, j("more"), true},
		"sk":      {model.KindSkill, j("commands/sk"), true},
		"c1":      {model.KindCommand, j("commands/c1.md"), false},
		"sub:c2":  {model.KindCommand, j("commands/sub/c2.md"), false},
		"one":     {model.KindCommand, j("extra/one.md"), false},
		"a1":      {model.KindSubagent, j("agents/a1.md"), false},
		"team:a2": {model.KindSubagent, j("agents/team/a2.md"), false},
		"a3":      {model.KindSubagent, j("more-agents/a3.md"), false},
	}
}

type childWant struct {
	kind model.ArtifactKind
	path string
	dir  bool
}

// jsonField reads one member of v's JSON form, so a test can assert a field the struct may not have.
func jsonField(t *testing.T, v any, key string) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}

// checkChildren asserts that res holds exactly the wanted children of the plugin artifact named
// parent: kind, "<bundle>:<leaf><suffix>" name, `plugin` member and canonical hash.
func checkChildren(t *testing.T, res Result, parent, bundle, suffix string, want map[string]childWant) {
	t.Helper()
	found := false
	got := map[string]model.ArtifactReport{}
	for _, a := range res.Artifacts {
		if a.Kind == model.KindPlugin && a.Name == parent {
			found = true
		}
		if p, _ := jsonField(t, a, "plugin").(string); p == parent {
			got[a.Name] = a
		}
	}
	if !found {
		t.Fatalf("no plugin artifact named %q", parent)
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(got) != len(want) {
		t.Errorf("children of %q = %d %q, want %d", parent, len(got), names, len(want))
	}
	for leaf, w := range want {
		name := bundle + ":" + leaf + suffix
		a, ok := got[name]
		if !ok {
			t.Errorf("missing child %q (have %q)", name, names)
			continue
		}
		if a.Kind != w.kind {
			t.Errorf("%s: kind %s, want %s", name, a.Kind, w.kind)
		}
		hash := FileHash(w.path)
		if w.dir {
			hash = TreeHash(w.path, w.path)
		}
		if a.Hash != hash || a.Hash == "" {
			t.Errorf("%s: hash %q, want %q (the hash `aguard hash %s` prints)", name, a.Hash, hash, w.path)
		}
	}
	for _, n := range names {
		if strings.Contains(n, "notaskill") || strings.Contains(n, ":ln") || strings.Contains(n, "outside") ||
			strings.Contains(n, "other") || strings.Contains(n, "notes") {
			t.Errorf("collected %q, which Claude Code does not load", n)
		}
	}
}

func envCount(t *testing.T, env model.EnvSummary, key string) int {
	t.Helper()
	n, _ := jsonField(t, env, key).(float64)
	return int(n)
}

// TestPluginContents_FollowClaudeCodesLoader: a plugin's skills, commands and agents are artifacts
// of their own — exactly the ones Claude Code 2.1.107 loads, named as it and the load-time gate name
// them, hashed as `aguard hash` hashes them — for every channel a plugin is installed through and for
// `check <plugin>`. The copies that never load (docs/ja, .agents, a symlinked command or agent, a file
// next to a SKILL.md, a manifest path outside the plugin) are not.
func TestPluginContents_FollowClaudeCodesLoader(t *testing.T) {
	t.Run("installed_plugins.json", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, ".claude")
		install := filepath.Join(root, "plugins", "cache", "mkt", "demo", "1.0.0")
		want := pluginLoaderTree(t, install)
		installedPlugins(t, root, "demo@mkt", install)
		res := CollectAll(root)
		checkChildren(t, res, "demo@mkt (1.0.0)", "demo", " (plugin demo@mkt)", want)
		for key, n := range map[string]int{"bundled_skills": 5, "bundled_commands": 3, "bundled_agents": 3} {
			if got := envCount(t, res.Env, key); got != n {
				t.Errorf("env %s = %d, want %d", key, got, n)
			}
		}
		if res.Env.Skills != 0 || res.Env.Commands != 0 || res.Env.Subagents != 0 {
			t.Errorf("top-level counters moved: skills=%d commands=%d subagents=%d", res.Env.Skills, res.Env.Commands, res.Env.Subagents)
		}
	})
	t.Run("synced", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, ".claude")
		want := pluginLoaderTree(t, filepath.Join(root, "plugins", "synced", "uuid-1", "demo"))
		checkChildren(t, CollectAll(root), "demo", "demo", " (synced plugin demo)", want)
	})
	t.Run("Claude Desktop", func(t *testing.T) {
		_, root, pluginDir, _ := desktopStore(t)
		if err := os.RemoveAll(pluginDir); err != nil {
			t.Fatal(err)
		}
		want := pluginLoaderTree(t, pluginDir)
		res := CollectAll(root)
		var parent string
		for _, a := range desktopByKind(res, model.KindPlugin) {
			parent = a.Name
		}
		label := strings.TrimSuffix(parent, " (0.3.0)")
		label = strings.TrimSuffix(label, " ()")
		bundle, _, _ := strings.Cut(label, "@")
		checkChildren(t, res, parent, bundle, " (plugin "+label+")", want)
	})
	t.Run("check <plugin>", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "demo")
		want := pluginLoaderTree(t, dir)
		res, err := CollectTarget(dir)
		if err != nil {
			t.Fatal(err)
		}
		checkChildren(t, res, "demo", "demo", " (plugin demo)", want)
		if got := envCount(t, res.Env, "bundled_commands"); got != 3 {
			t.Errorf("env bundled_commands = %d, want 3", got)
		}
	})
}

// TestPluginContents_ManifestObjectCommands: `commands` written as an object collects each entry's
// `source` file; an inline `content` entry has no file and is not an artifact (the plugin tree still
// reads plugin.json as text).
func TestPluginContents_ManifestObjectCommands(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "obj")
	mustWrite(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name":"obj","commands":{"deploy":{"source":"./cmds/deploy.md"},"hi":{"content":"say hi"}}}`)
	mustWrite(t, filepath.Join(dir, "cmds", "deploy.md"), "---\ndescription: Deploys.\n---\nDeploy.\n")
	res, err := CollectTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	checkChildren(t, res, "obj", "obj", " (plugin obj)", map[string]childWant{
		"deploy": {model.KindCommand, filepath.Join(dir, "cmds", "deploy.md"), false},
	})
}

// TestPluginContents_TreeArtifactUnchanged: the plugin tree artifact keeps its name, path and tree
// hash; the children are added next to it, never instead of it.
func TestPluginContents_TreeArtifactUnchanged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	pluginLoaderTree(t, dir)
	res, err := CollectTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Artifacts[0]; a.Kind != model.KindPlugin || a.Name != "demo" || a.Path != dir || a.Hash != TreeHash(dir, dir) {
		t.Errorf("first artifact = %s %q %q %q, want the plugin tree as before", a.Kind, a.Name, a.Path, a.Hash)
	}
}
