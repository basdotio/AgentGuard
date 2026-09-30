// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"
	"strings"
)

// inBoundary reports whether path, after resolving symlinks, stays under base — the
// same §16.2 containment used by collect, applied here so the engine never reads a
// skill's internal file that symlinks out to a system path. Fail-closed on error.
func inBoundary(base, path string) bool {
	rb, err := filepath.EvalSymlinks(base)
	if err != nil {
		rb = filepath.Clean(base)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rb, real)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
