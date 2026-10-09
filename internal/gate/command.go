// SPDX-License-Identifier: MIT
package gate

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/detect"
)

// maxCommandArgLen bounds one quoted argument of a command printed for copying. A command
// carries the FULL path — a clipped one names a directory that does not exist — so the display
// clip (maxPathLen) cannot bound it, and a path is attacker-chosen in length as well as content.
// 4096 bytes is Linux's PATH_MAX (macOS's is 1024): every path the OS opens in one call fits
// unless most of it has to be escaped.
const maxCommandArgLen = 4096

// CommandArg renders path as one word of a command an operator is meant to copy and paste: the
// full path, quoted for a POSIX shell. Past maxCommandArgLen once quoted it is a placeholder that
// says why there is no command — never a shortened path, which pastes into a command that fails
// (or, shortened at the wrong character, one that audits something else).
func CommandArg(path string) string {
	q := shellQuote(path)
	if len(q) > maxCommandArgLen {
		return fmt.Sprintf("<path of %d bytes, too long to print>", len(path))
	}
	return q
}

// shellQuote quotes s as exactly one word for a POSIX shell, in the first form that is exact:
//
//  1. Double quotes, when every character is printable and none of " \ $ ` ! occurs. This is
//     byte-identical to Go's %q, which the gate printed before, so ordinary paths do not change.
//     `!` is excluded because interactive bash and zsh expand history inside double quotes.
//  2. Single quotes for any other printable text — nothing is special inside them; an embedded
//     quote closes the string, is written backslash-escaped, and reopens it. %q was not shell
//     quoting: a shell expanded $HOME, `…` and $(…) inside it, so pasting ran part of an
//     attacker-named path.
//  3. $'…' when s holds a control or invisible character, or is not valid UTF-8. Such a
//     character must never be printed raw (invariant #7), and $'…' is the one shell form that
//     spells it in printable bytes. Each such byte, and \ ' !, is a three-digit octal escape,
//     which — unlike \x — never absorbs a following digit. This form needs bash, zsh, ksh or a
//     POSIX.1-2024 shell; the other two work in any POSIX shell.
func shellQuote(s string) string {
	plain, special := true, false
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !printableRune(r, n) {
			plain = false
			break
		}
		if strings.ContainsRune("\"\\$`!", r) {
			special = true
		}
		i += n
	}
	switch {
	case plain && !special:
		return `"` + s + `"`
	case plain:
		return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
	}
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if printableRune(r, n) && !strings.ContainsRune(`\'!`, r) {
			b.WriteString(s[i : i+n])
		} else {
			for _, c := range []byte(s[i : i+n]) {
				fmt.Fprintf(&b, `\%03o`, c)
			}
		}
		i += n
	}
	b.WriteString("'")
	return b.String()
}

// printableRune reports whether a decoded rune may be printed as itself: valid UTF-8 (n > 1 for
// RuneError means a literal U+FFFD), printable, and not one of the invisible characters
// report.Sanitize replaces.
func printableRune(r rune, n int) bool {
	if r == utf8.RuneError && n <= 1 {
		return false
	}
	return strconv.IsPrint(r) && !detect.Invisible(r)
}
