// SPDX-License-Identifier: MIT
package redact

import "strings"

// A quoted value (P-043). A shell user quotes a password with a space or a symbol in it —
// `--password "correct horse"`, `API_TOKEN='P@ss!w0rd'`, `curl -u "admin:pass word"` — and every value
// form the patterns knew was a bare word, so after a flag or `-u` the value went out whole and after a key
// it kept everything past its first bare word. These fragments are the quoted form flagSecretRE,
// flagUserPassRE and assignRE accept besides the bare one; they are not a second pattern set, and nothing
// outside this package names them (invariant #3).
//
// A quoted value ends as POSIX sh reads it — a double-quoted one at the next `"` no backslash escapes, a
// single-quoted one at the next `'` — and never past a newline: an unterminated quote runs to the end of
// its line, so one stray quote cannot swallow the rest of a file the judge reads. What it does NOT accept,
// each measured on the corpus and a real ~/.claude, and each left to the bare reading:
//
//   - a `$` or a backtick inside, in either kind of quote: `"$TOKEN"`, `"Bearer ${API_KEY}"`, `"$(op read …)"`
//     are references, and the name is the evidence; the content hash's guard would refuse the
//     replacement anyway (`$` is shell structure). Dart and others interpolate in single quotes too;
//   - a body that starts with a space or one of `, ; ) ] } + .`, or ends with a space: that quote is the
//     far side of a string being built (`print("token:", t)`, `"--password=" + pw`, `'Bearer ' + t`,
//     `date -u '+%Y-%m-%dT%H:%M:%SZ'`);
//   - a body whose first or last character is invisible: two INJ-004 test lines quote nothing but
//     zero-width characters, and those characters are the finding's evidence.
//
// "Visible" is graphic and not a space: `[^\pC\pZ]`. The backtick is written \x60, so the fragments can
// stay raw strings.
const (
	// dqValue is a double-quoted literal: backslash escapes one character; no `$`, no backtick.
	dqValue = `"` + dqBody + `(?:"|(?m:$))`
	dqBody  = `(?:[^\pC\pZ"\\$\x60,;)\]}+.]|\\[^\pC\pZ])(?:(?:[^"\\$\x60\n]|\\.)*(?:[^\pC\pZ"\\$\x60]|\\[^\pC\pZ]))?`
	// dqTail is a double-quoted body after something already read (a `user:`): no first-character rule.
	dqTail = `(?:(?:[^"\\$\x60\n]|\\.)*(?:[^\pC\pZ"\\$\x60]|\\[^\pC\pZ]))`

	// sqValue is a single-quoted literal: no escapes inside single quotes; no `$`, no backtick.
	sqValue = `'` + sqBody + `(?:'|(?m:$))`
	sqBody  = `[^\pC\pZ'$\x60,;)\]}+.](?:[^'$\x60\n]*[^\pC\pZ'$\x60])?`
	sqTail  = `(?:[^'$\x60\n]*[^\pC\pZ'$\x60])`

	// quotedUser is the user of a quoted `user:pass`, opening quote included: it may be empty, and it
	// starts as a body does.
	quotedUser = `(?:[^\pC\pZ"'\\$\x60,;)\]}+.:][^\s'":$\x60]*)?:`
)

// unquote returns the body of a quoted value v (flagSecretRE's value group), or v itself when it is bare.
func unquote(v string) string {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return v
	}
	if closes(v) {
		return v[1 : len(v)-1]
	}
	return v[1:]
}

// requote is what replaces a value: the marker, inside the quotes it was written in — the closing one
// only if it was there.
func requote(v string) string {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return marker
	}
	if closes(v) {
		return v[:1] + marker + v[:1]
	}
	return v[:1] + marker
}

// closes reports whether a quoted value v ends with its closing quote. A body holds no unescaped quote of
// its kind, so a final `'` closes, and a final `"` closes unless an odd run of backslashes escapes it.
func closes(v string) bool {
	if len(v) < 2 || v[len(v)-1] != v[0] {
		return false
	}
	if v[0] == '\'' {
		return true
	}
	n := len(v) - 2 - len(strings.TrimRight(v[1:len(v)-1], `\`))
	return n%2 == 0
}
