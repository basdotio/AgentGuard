// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"
	"strings"
)

// anchorRoot is the one spelling of the scan root every path check in this package sees. The
// scripts a hook or a grant names under `~/` and `$CLAUDE_PROJECT_DIR/` resolve against
// filepath.Dir(root), and Dir answers about the STRING: for `--root ~/.claude/` (shell completion
// adds the slash) it drops an empty last segment and returns the root itself, for `--root .` it
// returns `.` again, and for `--root home/.claude` it returns a relative home that the "relative
// reference" candidates then join a second time. Each way the script was looked for in the wrong
// place, was not read, and the hook scored a clean 100 behind a note blaming a missing file.
//
// Abs, not EvalSymlinks: resolving would move home to wherever a symlinked ~/.claude points, which
// is not the anchor collect uses, and `~/.claude/hooks/x.sh` would miss again. Symlinks are resolved
// where they always were — inBoundary, at check time (invariant #2: resolve, then check). If the
// working directory is gone and Abs fails, Clean still fixes the slash, and `.` stays home == root:
// a narrower boundary, which refuses more rather than less.
func anchorRoot(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return filepath.Clean(root)
}

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
