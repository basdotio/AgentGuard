// SPDX-License-Identifier: MIT
package collect

import "testing"

// TestTrashDirKeptAcrossRename: the quarantine directory keeps its name through the rename to BAS
// Guard (P-082 Out of scope); a new name would leave everything already quarantined unscanned.
func TestTrashDirKeptAcrossRename(t *testing.T) {
	if TrashDir != ".aguard-trash" {
		t.Errorf("TrashDir = %q, want .aguard-trash", TrashDir)
	}
}
