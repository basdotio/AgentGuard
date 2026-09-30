// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m
}

// TestInstallMergesAndNeverStomps is the property that matters most about a command called
// "install": settings.json is the operator's, it holds permissions and hooks that have
// nothing to do with this tool, and a security product that flattens a config it does not
// fully understand has done more damage than the risk it was managing.
func TestInstallMergesAndNeverStomps(t *testing.T) {
	root := t.TempDir()
	p := SettingsPath(root)
	existing := `{
  "permissions": {"allow": ["Bash(git status)"], "deny": ["Read(~/.ssh/**)"]},
  "someFutureKey": {"kept": true},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/bin/mine"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "/usr/bin/notify"}]}]
  }
}`
	if err := os.WriteFile(p, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanInstall(root, "/opt/aguard hook")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Added) != 3 {
		t.Fatalf("registered %v, want all three events", plan.Added)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, p)
	if _, ok := got["permissions"]; !ok {
		t.Error("permissions were dropped")
	}
	if _, ok := got["someFutureKey"]; !ok {
		t.Error("an unknown top-level key was dropped — this build must not decide what a future one may store")
	}
	hooks := got["hooks"].(map[string]any)
	if len(hooks["Stop"].([]any)) != 1 {
		t.Error("an unrelated hook event was disturbed")
	}
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse has %d entries, want the operator's plus ours", len(pre))
	}
	if plan.Backup == "" {
		t.Error("no backup was written; the encoder reorders keys and drops comments, so the original must survive")
	}
	if b, err := os.ReadFile(plan.Backup); err != nil || string(b) != existing {
		t.Error("the backup is not the original file")
	}
}

// TestInstallIsIdempotent: running it twice must not fire the gate twice on every load.
func TestInstallIsIdempotent(t *testing.T) {
	root := t.TempDir()
	first, err := PlanInstall(root, "/opt/aguard hook")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Apply(); err != nil {
		t.Fatal(err)
	}
	second, err := PlanInstall(root, "/opt/aguard hook")
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed() || len(second.Added) != 0 || len(second.AlreadyThere) != 3 {
		t.Errorf("second install was not a no-op: added=%v already=%v", second.Added, second.AlreadyThere)
	}
}

// TestUninstallLeavesOtherHooksAlone: it matches on the command, so hooks the operator wrote
// by hand — including ones on the same event and matcher — are untouched.
func TestUninstallLeavesOtherHooksAlone(t *testing.T) {
	root := t.TempDir()
	p := SettingsPath(root)
	if err := os.WriteFile(p, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Skill","hooks":[{"type":"command","command":"/usr/bin/theirs"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	inst, _ := PlanInstall(root, "/opt/aguard hook")
	if err := inst.Apply(); err != nil {
		t.Fatal(err)
	}
	un, err := PlanUninstall(root, "/opt/aguard hook")
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Removed) != 3 {
		t.Fatalf("removed %v", un.Removed)
	}
	if err := un.Apply(); err != nil {
		t.Fatal(err)
	}
	hooks := readJSON(t, p)["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("operator's own Skill hook was removed: %v", hooks)
	}
	if _, ok := hooks["SessionStart"]; ok {
		t.Error("an event left empty was not cleaned up")
	}
}

// TestUnparseableSettingsIsRefused: replacing a settings file this build cannot read with a
// fresh one holding only our hooks would silently delete every permission the operator has —
// the worst possible outcome of a command called "install".
func TestUnparseableSettingsIsRefused(t *testing.T) {
	root := t.TempDir()
	p := SettingsPath(root)
	if err := os.WriteFile(p, []byte(`{"hooks": [oops`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInstall(root, "/opt/aguard hook"); err == nil {
		t.Fatal("an unparseable settings file was about to be rewritten")
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"hooks": [oops` {
		t.Error("the file was modified anyway")
	}
}

// TestRegisteredCommandCarriesNoShellMetacharacters: the gate's own installation must not
// trip HOOK-001. A security tool whose setup step is flagged by the same tool teaches
// operators to ignore its findings.
func TestRegisteredCommandCarriesNoShellMetacharacters(t *testing.T) {
	for _, exe := range []string{"/opt/aguard", "/Users/a b/bin/aguard"} {
		cmd := HookCommand(exe)
		if strings.ContainsAny(cmd, "|;&$`><\n") {
			t.Errorf("HookCommand(%q) = %q contains shell metacharacters", exe, cmd)
		}
		if !strings.HasSuffix(cmd, " "+hookSubcommand) {
			t.Errorf("HookCommand(%q) = %q does not end in the subcommand", exe, cmd)
		}
	}
}

// TestInstallCreatesSettingsWhenAbsent covers the fresh-machine path.
func TestInstallCreatesSettingsWhenAbsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", ".claude")
	plan, err := PlanInstall(root, "/opt/aguard hook")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readJSON(t, SettingsPath(root))["hooks"]; !ok {
		t.Error("hooks were not written")
	}
	if plan.Backup != "" {
		t.Error("backed up a file that did not exist")
	}
}

// TestUninstallRemovesEveryCopyAndOnlyOurs: the two uninstall bugs, pinned. (1) A gate
// registered twice under one event used to lose only the first copy — "uninstall" reported
// success while the hook kept firing. (2) An entry whose inner hooks held OUR command next to
// the operator's own used to be deleted whole, taking theirs with it.
func TestUninstallRemovesEveryCopyAndOnlyOurs(t *testing.T) {
	root := t.TempDir()
	p := SettingsPath(root)
	ours := "/opt/aguard hook"
	// PreToolUse: two separate entries with our command (a duplicate registration), plus an
	// entry where our command shares the inner list with the operator's.
	doc := `{"hooks":{"PreToolUse":[
	  {"matcher":"Skill","hooks":[{"type":"command","command":"/opt/aguard hook"}]},
	  {"matcher":"Skill","hooks":[{"type":"command","command":"/opt/aguard hook"}]},
	  {"matcher":"Skill","hooks":[{"type":"command","command":"/usr/bin/theirs"},{"type":"command","command":"/opt/aguard hook"}]},
	  {"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/other"}]}
	]}}`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	un, err := PlanUninstall(root, ours)
	if err != nil {
		t.Fatal(err)
	}
	if err := un.Apply(); err != nil {
		t.Fatal(err)
	}
	hooks := readJSON(t, p)["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse should keep the operator's Skill entry and the Bash entry, got %d: %v", len(pre), pre)
	}
	for _, e := range pre {
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			if c, _ := h.(map[string]any)["command"].(string); c == ours {
				t.Fatalf("our command survived uninstall: %v", pre)
			}
		}
	}
	var sawTheirs bool
	for _, e := range pre {
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			if c, _ := h.(map[string]any)["command"].(string); c == "/usr/bin/theirs" {
				sawTheirs = true
			}
		}
	}
	if !sawTheirs {
		t.Error("the operator's own hook sharing our matcher was removed")
	}
	if len(un.Removed) == 0 || !strings.Contains(un.Removed[0], "×3") {
		t.Errorf("removal count should say ×3, got %v", un.Removed)
	}
}

// TestBackupKeepsTheOriginal: install then uninstall used to overwrite the single backup
// with the post-install file, so the operator's ORIGINAL settings were unrecoverable after the
// second command. The .aguard-bak slot must hold the original forever; .prev tracks the latest.
func TestBackupKeepsTheOriginal(t *testing.T) {
	root := t.TempDir()
	p := SettingsPath(root)
	original := `{"permissions":{"allow":["Bash(git status)"]}}`
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	inst, _ := PlanInstall(root, "/opt/aguard hook")
	if err := inst.Apply(); err != nil {
		t.Fatal(err)
	}
	if inst.Original == "" {
		t.Fatal("first change must record the original backup")
	}
	un, _ := PlanUninstall(root, "/opt/aguard hook")
	if err := un.Apply(); err != nil {
		t.Fatal(err)
	}
	if un.Original != "" {
		t.Error("second change must NOT rewrite the original slot")
	}
	orig, err := os.ReadFile(p + backupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(orig) != original {
		t.Errorf("original slot was overwritten:\n%s", orig)
	}
	prev, err := os.ReadFile(p + prevBackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prev), "aguard hook") {
		t.Errorf(".prev should hold the post-install state (before uninstall), got:\n%s", prev)
	}
}
