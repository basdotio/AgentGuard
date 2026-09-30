// SPDX-License-Identifier: MIT
package collect

import (
	"path/filepath"
	"strings"
)

// withinDir reports whether path, after resolving symlinks, still lives under base.
// It is the containment primitive behind two distinct rules:
//   - §16.2: a skill's INTERNAL file must not symlink out of the skill root (base =
//     skill root) — otherwise ~/.ssh / /etc/passwd content leaks into hash/report.
//   - install guard: a top-level skill dir that is itself a symlink is a legitimate
//     audit target, but its resolved target must stay under HOME (base = HOME) so a
//     skill symlinked to a system path is refused.
//
// It resolves the real path of the deepest existing ancestor (EvalSymlinks fails on
// non-existent leaves) and checks containment. On any resolution error it returns
// false (fail-closed).
func withinDir(base, path string) bool {
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		realBase = filepath.Clean(base)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real, err = filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return false
		}
		real = filepath.Join(real, filepath.Base(path))
	}
	rel, err := filepath.Rel(realBase, real)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
