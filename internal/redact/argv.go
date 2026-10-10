// SPDX-License-Identifier: MIT
package redact

import "strings"

// marker is what every replacement leaves where a secret was.
const marker = "<REDACTED>"

// Argv is Secrets for an argument vector — a command's arguments as separate strings, the way an MCP
// server's `args` holds them (P-036). Every element is redacted, and an element that follows a flag is
// also read together with that flag.
//
// A credential on a command line is announced by the flag in front of it: `--api-key <key>`,
// `-u user:<pass>`. flagSecretRE and flagUserPassRE see that only within one string, and in an argv the
// flag and its value are two. Redacted one by one, `--api-key` and `k7Qp2xLm9Rt4Vw8Z` each look like
// nothing, and a key under the entropy floor with no known prefix went to the judge as written.
//
// Whether a flag announces the next element is asked of the same patterns, not of a list kept here:
// the pair is redacted as the one string `flag value` (Credentials), and the element is announced when
// the flag comes back unchanged and the element does not come back as it would alone. The content
// hash asks the same question of an array (detect.redactTree, through Announced), so the two agree on
// which elements are secrets and where each one starts. A list of flag names here would be a second
// redactor, and two redactors drift (invariant #3, the package doc).
//
// An announced element is replaced from the first byte the patterns removed to its END. In argv the
// element is one argument, all of it the value, while the patterns — written for a shell line — stop at
// whitespace or a quote: `"--password", "correct horse"` would keep ` horse`. What comes before that
// byte stays, as flagUserPassRE keeps the user of `user:pass`. Which elements are announced, and where
// their value starts, is Announced's answer — the one the content hash takes too (P-039).
func Argv(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = Secrets(a)
		if i == 0 {
			continue
		}
		if at, ok := Announced(args[i-1], a); ok {
			out[i] = Secrets(a[:at]) + marker
		}
	}
	return out
}

// Announced reports whether arg, the element after flag in an argument vector, is a value flag
// announces as a credential, and the byte of arg where that value starts: all of arg from there to its
// end is the secret. A flag is an element starting with "-"; anything else announces nothing.
//
// It is the one decision Argv makes, exported so the content hash (detect.redactTree) asks the same
// question instead of keeping a second reading: the judge's excerpt and the hash then forget the same
// span (P-039 — the hash used to keep the regex reading, and with it the tail ` horse`). What each does
// with the kept head arg[:start] is its own: Secrets for the excerpt, the guarded credential half for
// the hash, which must not take the entropy catch-all (see Secrets).
//
// Before the first marker the joined reading and arg agree byte for byte (a replacement only ever
// inserts the marker, and one that touched the flag fails the prefix check), so start indexes arg.
func Announced(flag, arg string) (start int, ok bool) {
	if !strings.HasPrefix(flag, "-") {
		return 0, false
	}
	prefix := flag + " "
	rest, cut := strings.CutPrefix(Credentials(prefix+arg), prefix)
	if !cut || rest == Credentials(arg) {
		return 0, false // the flag was rewritten itself, or added nothing arg alone does not say
	}
	i := strings.Index(rest, marker)
	if i < 0 || i >= len(arg) {
		return 0, false
	}
	return i, true
}
