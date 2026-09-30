// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSkillOrder checks that the copy which actually LOADS is the copy that gets
// audited: Claude Code prefers a project skill over a user one, so auditing the user copy
// would produce a correct verdict about the wrong bytes.
func TestResolveSkillOrder(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	cwd := filepath.Join(home, "proj")
	mk := func(parts ...string) string {
		p := filepath.Join(parts...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	userCopy := mk(root, "skills", "dup")
	projCopy := mk(cwd, ".claude", "skills", "dup")
	userOnly := mk(root, "skills", "useronly")

	got, err := ResolveSkill(root, home, cwd, "dup")
	if err != nil {
		t.Fatal(err)
	}
	if got != projCopy {
		t.Errorf("resolved %s, want the project copy %s (the user copy is %s)", got, projCopy, userCopy)
	}
	// The fallback half needs the SAME assertion as the preference half. Checking only that
	// this does not error would pass on any path at all, and "resolved something" is exactly
	// the failure this test exists to catch: a verdict about the wrong bytes.
	got, err = ResolveSkill(root, home, cwd, "useronly")
	if err != nil {
		t.Fatalf("user-level skill did not resolve: %v", err)
	}
	if got != userOnly {
		t.Errorf("resolved %s, want the user copy %s", got, userOnly)
	}
}

// TestResolvePluginSkill covers the `plugin:skill` form, which is how every marketplace skill
// is named — leaving it unresolvable would make GATE-000 the normal case for plugin users.
func TestResolvePluginSkill(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	inst := filepath.Join(home, ".claude", "plugins", "cache", "figma", "1.0.0")
	if err := os.MkdirAll(filepath.Join(inst, "skills", "figma-use"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"version": 2, "plugins": map[string]any{
		"figma@official": []any{map[string]any{"installPath": inst, "version": "1.0.0"}},
	}}
	b, _ := json.Marshal(manifest)
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugins", "installed_plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSkill(root, home, "", "figma:figma-use")
	if err != nil {
		t.Fatalf("plugin skill did not resolve: %v", err)
	}
	// installPath is resolved through symlinks, exactly as collect resolves it — on macOS
	// /var is /private/var, and a gate that disagreed with the scanner about which directory
	// a plugin lives in would hash one tree and audit another.
	real, err := filepath.EvalSymlinks(inst)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(real, "skills", "figma-use"); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// A bare name that only a plugin provides still resolves — that spelling is legal too.
	if _, err = ResolveSkill(root, home, "", "figma-use"); err != nil {
		t.Errorf("bare plugin skill name did not resolve: %v", err)
	}
}

// TestResolveRejectsTraversal: the name comes from a tool call, i.e. ultimately from text an
// artifact could influence. A crafted name must not steer the gate at an arbitrary directory
// and — far worse — get that directory's hash written into the store as an audited skill.
func TestResolveRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../../../etc", "..", ".", "", "a/../../b", "/etc/passwd",
		"foo/bar", `foo\bar`, "plug:../../../etc", "plug:..",
	} {
		if p, err := ResolveSkill(root, home, home, name); err == nil {
			t.Errorf("skill name %q resolved to %s", name, p)
		}
	}
}

// TestResolveRefusesAmbiguity: picking one of two same-named plugin skills would record an
// approval for content that may not be the content that loads.
func TestResolveRefusesAmbiguity(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	var insts []any
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(home, ".claude", "plugins", "cache", n)
		if err := os.MkdirAll(filepath.Join(p, "skills", "shared"), 0o755); err != nil {
			t.Fatal(err)
		}
		insts = append(insts, map[string]any{"installPath": p})
	}
	b, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"one@m": []any{insts[0]}, "two@m": []any{insts[1]},
	}})
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugins", "installed_plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := ResolveSkill(root, home, "", "shared"); err == nil {
		t.Errorf("ambiguous name resolved to %s instead of failing", p)
	}
}
