// SPDX-License-Identifier: MIT
package clean

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// Everything in this file exists because an audit turned each of these into a working attack against
// a tool whose entire promise is "an over-eager cleanup is always recoverable". The pattern behind
// all of them is the same: a decision was made on a path as WRITTEN rather than as RESOLVED, and a
// symlink or a relative path made the written form lie.
//
// Three rules now hold everywhere a path is judged:
//
//  1. Resolve before deciding. `filepath.Abs` then `filepath.EvalSymlinks`, or the nearest existing
//     ancestor when the path is a destination that does not exist yet.
//  2. Compare segments the way the filesystem would. macOS is case-insensitive and Windows ignores
//     trailing dots and spaces, so `Rules` and `hooks.` name the same directories that `rules` and
//     `hooks` do. This binary is cross-compiled for both.
//  3. A name is a label, never an address. A skill called `synced/websearch` must not be able to
//     steer where its quarantine copy lands.

// segEqual folds a path segment the way a case-insensitive or Win32 filesystem would, so a check
// cannot be defeated by spelling a directory `Hooks` or `hooks.`.
func segEqual(seg, want string) bool {
	return strings.EqualFold(strings.TrimRight(seg, ". "), want)
}

// resolved returns path with symlinks resolved, falling back to the absolute form when the path (or
// an ancestor) does not exist. A destination that does not exist yet resolves through its nearest
// existing ancestor, so a symlinked parent cannot smuggle the final path elsewhere.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	dir, rest := filepath.Dir(abs), filepath.Base(abs)
	for {
		if rd, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(rd, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// isSymlink reports whether path itself is a symbolic link. Used where following one would move the
// decision somewhere the caller never named — a symlinked trash directory or manifest file.
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// trashName turns an artifact name into ONE path element.
//
// Names are not addresses. `collectSkills` names a claude.ai-synced skill `synced/websearch`, and
// joining that onto the trash directory produced a path whose parent nobody created: every apply
// failed with ENOENT *after* writing its write-ahead row, so a whole class of real skills was
// unquarantinable and each attempt left behind a row indistinguishable from a crash. The same join is
// what a `../` in a name would have travelled through.
func trashName(name string) string {
	s := strings.ReplaceAll(filepath.ToSlash(name), "/", "__")
	s = strings.ReplaceAll(s, "\\", "__")
	s = strings.ReplaceAll(s, "..", "__")
	s = strings.TrimSpace(s)
	if s == "" || s == "." {
		return "unnamed"
	}
	return s
}

// safeTrash returns the directory quarantined content may be written to, creating it if needed, and
// refuses every address that would break one of the guarantees the operator is given.
//
// The refusals, each of which was a verified attack:
//
//   - The trash directory is a SYMLINK. `MkdirAll` and `os.Rename` both follow it, so
//     `ln -s /tmp/exfil <root>/.aguard-trash` moved quarantined content clean out of the scanned root:
//     the collector then dropped it (it is no longer within root) and the score went 50 → 100 with
//     `--fail-on high` flipping 1 → 0, while the run printed "still inside the config root, so it
//     still scores". `clean --apply` became the shortest path from red to green.
//   - The RESOLVED trash directory is outside root, or inside a tree Claude Code recurses into.
//     `ln -s <root>/rules <root>/.aguard-trash` landed the payload at `rules/<name>/SKILL.md`, where
//     it loads every session — the report says quarantined, the content is still in the system prompt.
//     Checking root alone missed this, which is why the check runs on the resolved trash path.
//
// create is false for read-only and dry-run callers: `--dry-run` must leave a directory-free tree
// behind, so it validates the address without bringing it into existence.
func safeTrash(root string, create bool) (string, error) {
	p := filepath.Join(root, collect.TrashDir)
	if isSymlink(p) {
		return "", fmt.Errorf("refusing to use %s: it is a symlink, and following it would move "+
			"quarantined content somewhere this scan cannot see (so it would stop being scored, and "+
			"could keep loading). Remove or replace the symlink", p)
	}
	if create {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", fmt.Errorf("create trash dir: %w", err)
		}
	}
	real := resolved(p)
	if !withinDir(root, real) {
		return "", fmt.Errorf("refusing to use %s: it resolves to %s, outside the scanned root", p, real)
	}
	if bad := collect.QuarantineUnsafe(real); bad != "" {
		return "", fmt.Errorf("refusing to use %s: it resolves inside a %q directory, which Claude Code "+
			"loads from recursively — quarantined content there would keep entering context every "+
			"session while this tool reported it as quarantined", p, bad)
	}
	return real, nil
}

// withinTrash reports whether p is the trash directory or something under it. Undo moves FROM here,
// and that end of the move used to be containment-checked against root only — so an appended
// manifest row naming `to: <root>/settings.json` made undo move the operator's deny-list away and
// plant it wherever the row's `from` pointed. Restoring is only ever "take something out of the
// trash": anything else is not a restore.
func withinTrash(trash, p string) bool {
	real := resolved(p)
	return relWithin(trash, real) && filepath.Clean(real) != filepath.Clean(trash)
}

// quarantinable reports why src may not be moved, or "" when it may.
//
// Apply used to check containment and nothing else, while Undo additionally refused security
// configuration — so the two disagreed about what may move, and the gap was exactly the tree that
// must never move. A `skills/x → <root>/hooks` install symlink made apply move the whole hooks tree
// (every hook silently stopped firing) and undo then refused to put it back: the one state the
// package's own doc comment promises cannot happen.
//
// Apply's permitted set is now a subset of Undo's restorable set BY CONSTRUCTION: same protected()
// call, plus a requirement that the source really is an installed skill.
func quarantinable(root, src string) (blocker, why string) {
	real := resolved(src)
	if !withinDir(root, real) {
		return model.BlockerOutsideRoot, "lives outside the config root (installed elsewhere, e.g. by Claude Desktop); reported only, nothing will be moved"
	}
	if protected(root, real) {
		return model.BlockerProtected, "security configuration is never moved"
	}
	if relWithin(resolved(filepath.Join(root, collect.TrashDir)), real) {
		return model.BlockerAlreadyQuarantined, "already quarantined"
	}
	rel, err := filepath.Rel(resolved(root), real)
	if err != nil {
		return model.BlockerUnlocatable, "cannot be located relative to root"
	}
	segs := strings.Split(filepath.ToSlash(rel), "/")
	if len(segs) < 2 || !segEqual(segs[0], "skills") {
		// A zombie is by definition an installed skill. Anything else reaching here arrived through a
		// symlink pointing out of skills/ — into hooks/, into a plugin's own tree — and moving it would
		// take content out of a surface `clean` does not own.
		return model.BlockerNotUnderSkills,
			"not an installed skill (" + filepath.ToSlash(rel) + "); only skills/ is quarantined"
	}
	return "", ""
}

// QuarantineRefusal is quarantinable, exported so the DETECTOR can ask the EXECUTOR what it would
// refuse instead of keeping a second copy of the rules.
//
// It exists because the two answers had drifted, and the report was the one that lied: a skill
// installed as skills/x -> shared/x listed with no blocker at all, was counted in "1 executable",
// and was then refused at apply time with "not an installed skill". Blockers exist so that "this
// cannot be acted on" is visible while the operator is still reading, and that only holds if the
// plan asks the same question the move will.
func QuarantineRefusal(root, src string) (blocker, why string) {
	return quarantinable(anchored(root), src)
}

// anchored is the root every entry point of this package works from: collect.AnchorRoot, the same
// absolute spelling collect and detect use. withinDir resolves its base with EvalSymlinks and never
// made it absolute, EvalSymlinks(".") is ".", and "." cannot be related to a resolved path — so
// `cd ~/.claude && aguard clean --root . --undo last` refused the root's own .aguard-trash as
// "outside the scanned root", and every relative spelling refused every move. Anchoring at the entry
// rather than inside withinDir also makes what the operator reads — the trash path, the baseline
// path — the same under every spelling. Abs, not EvalSymlinks: each check below still resolves
// symlinks itself (rule 1 above), so a symlinked trash or an escaping source is refused as before.
func anchored(root string) string { return collect.AnchorRoot(root) }
