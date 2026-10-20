// SPDX-License-Identifier: MIT
package gate

import "testing"

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
