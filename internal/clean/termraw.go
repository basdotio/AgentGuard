// SPDX-License-Identifier: MIT
package clean

import (
	"os"

	"golang.org/x/term"
)

// The entire terminal-dependent surface of `clean --ask`, kept to one file so everything else can
// be driven from a byte fixture. Both entry points are package VARIABLES: a test replaces them to
// stand in for a terminal.
//
// This layer is deliberately NOT load bearing. Whichever way these answer, a run behind a pipe
// still moves nothing — the typed-line source and the keystroke source yield the same small set of
// strings, EOF aborts in both, and an unrecognised answer skips in both. The withdrawn picker made
// a terminal check into a safety boundary; a check that has to be right is worse than a design that
// does not need one.
//
// golang.org/x/term is PINNED (v0.28.0, with x/sys v0.29.0). Taking it unpinned raises this
// module's Go floor from 1.23.5 to 1.25.0, which CI does not build. See CLAUDE.md.

var isTerminal = func(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// makeRaw returns a restore function and whether raw mode was entered. A false is a downgrade, not
// an error: the caller falls back to typed answers rather than refusing to work.
var makeRaw = func(f *os.File) (func(), bool) {
	fd := int(f.Fd())
	st, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}, false
	}
	return func() { _ = term.Restore(fd, st) }, true
}
