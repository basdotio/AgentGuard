// SPDX-License-Identifier: MIT
package clean

import "testing"

// TestIgnoreFileKeptAcrossRename: the baseline file keeps its name through the rename to BAS
// Guard (P-082 Out of scope); a new name would silently drop every reviewed suppression.
func TestIgnoreFileKeptAcrossRename(t *testing.T) {
	if ignoreFile != ".aguardignore" {
		t.Errorf("ignoreFile = %q, want .aguardignore", ignoreFile)
	}
}
