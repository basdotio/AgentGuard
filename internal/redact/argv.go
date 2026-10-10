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
// the flag comes back unchanged and the element does not come back as it would alone. That is the
// reading the content hash already gives an array (detect.redactTree), so the two agree on which
// elements are secrets. A list of flag names here would be a second redactor, and two redactors drift
// (invariant #3, the package doc).
//
// An announced element is replaced from the first byte the patterns removed to its END. In argv the
// element is one argument, all of it the value, while the patterns — written for a shell line — stop at
// whitespace or a quote: `"--password", "correct horse"` would keep ` horse`. What comes before that
// byte stays, as flagUserPassRE keeps the user of `user:pass`.
func Argv(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = Secrets(a)
		if i == 0 || !strings.HasPrefix(args[i-1], "-") {
			continue
		}
		if v, ok := announced(args[i-1], a); ok {
			out[i] = v
		}
	}
	return out
}

// announced reports whether flag announces arg as a credential, and returns arg with everything from
// the first announced byte to its end replaced. Before that byte the joined reading and arg agree
// byte for byte (a replacement only ever inserts the marker), so the kept head is arg's own.
func announced(flag, arg string) (string, bool) {
	prefix := flag + " "
	rest, ok := strings.CutPrefix(Credentials(prefix+arg), prefix)
	if !ok || rest == Credentials(arg) {
		return "", false // the flag was rewritten itself, or added nothing arg alone does not say
	}
	i := strings.Index(rest, marker)
	if i < 0 {
		return "", false
	}
	return Secrets(rest[:i]) + marker, true
}
