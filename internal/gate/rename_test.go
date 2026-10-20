// SPDX-License-Identifier: MIT
package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLooksLikeOurs: the gate claims a registration as its own under the command's old name and
// under its new one (P-082), including the release-asset names a downloaded binary keeps, and
// claims nothing else. A gate that misses its own entry reports "not installed" with the entry
// right there in settings.json; one that claims a stranger's entry would uninstall it.
func TestLooksLikeOurs(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/local/bin/aguard hook":                                 true,
		"/usr/local/bin/bas-guard hook":                              true,
		`"/Users/a b/bin/aguard" hook`:                               true,
		`"/Users/a b/bin/bas-guard" hook`:                            true,
		"/home/a/Downloads/aguard-linux-amd64 hook":                  true,
		"/home/a/Downloads/bas-guard-darwin-arm64 hook":              true,
		"/x/node_modules/@bas.io/guard-linux-x64/bin/aguard hook":    true,
		"/x/node_modules/@bas.io/guard-linux-x64/bin/bas-guard hook": true,
		"/usr/local/bin/bas-guard scan":                              false,
		"/usr/local/bin/guard hook":                                  false,
		"/usr/local/bin/basguard hook":                               false,
		"/usr/local/bin/other-tool hook":                             false,
	} {
		if got := looksLikeOurs(cmd); got != want {
			t.Errorf("looksLikeOurs(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// TestOnDiskNamesKeptAcrossRename: the files the gate leaves in the operator's root keep their
// names through the rename (P-082 Out of scope). Renaming them would orphan every approval store
// and every settings backup already on disk.
func TestOnDiskNamesKeptAcrossRename(t *testing.T) {
	for got, want := range map[string]string{
		ApprovalsFile:    ".aguard-approvals.json",
		backupSuffix:     ".aguard-bak",
		prevBackupSuffix: ".aguard-bak.prev",
	} {
		if got != want {
			t.Errorf("on-disk name changed: %q, want %q", got, want)
		}
	}
}

// TestStatusNamesARegistrationUnderTheOldName: a gate registered as `aguard hook` keeps working
// after the rename (P-082), and `hook status` says how to move it to the new name — it does not
// move it (Q4): the operator decides. A registration under bas-guard gets no such line.
func TestStatusNamesARegistrationUnderTheOldName(t *testing.T) {
	for _, tc := range []struct {
		registered string
		wantHint   bool
	}{{"aguard", true}, {"bas-guard", false}} {
		root := t.TempDir()
		dir := t.TempDir()
		exe := filepath.Join(dir, tc.registered)
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		plan, err := PlanInstall(root, HookCommand(exe))
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.Apply(); err != nil {
			t.Fatal(err)
		}
		st, err := CheckStatus(root, filepath.Join(dir, "bas-guard"))
		if err != nil {
			t.Fatal(err)
		}
		if !st.Active() {
			t.Fatalf("%s: a live registration under either name must read as active: %+v", tc.registered, st.Events)
		}
		var out strings.Builder
		st.Describe(&out)
		got := strings.Contains(out.String(), "old command name aguard") && strings.Contains(out.String(), exe+" hook uninstall")
		if got != tc.wantHint {
			t.Errorf("registered as %s: switch hint present = %v, want %v:\n%s", tc.registered, got, tc.wantHint, out.String())
		}
	}
}
