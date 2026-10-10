// SPDX-License-Identifier: MIT
package redact

// Keyed reports whether value, the string a JSON object holds under key — an env variable, a header — is a
// value key announces as a credential, and the byte of value where it starts: all of value from there to its
// end is the secret (P-042).
//
// A member is read as the one string `key=value`, the way assignRE reads an assignment, and assignRE was
// written for a shell line, where a value ends at whitespace or a quote: `"DB_PASSWORD": "correct horse"` kept
// ` horse`, and `"Authorization": "Bearer abcd1234 efgh5678"` kept ` efgh5678`. In an object the member's
// string is one value, all of it what the key names.
//
// The question is asked of assignRE itself, not of a list kept here (invariant #3): of its matches over
// `key=value`, the one whose key group ends where key ends is the key's own assignment, and the value starts
// at that match's bare value — after the separator and a carrier word, which stay (`Bearer <REDACTED>`). A
// match assignValue refuses (a keyword literal, a carrier word read as the value) announces nothing, and so
// does a key word met further on: in `Bearer ${NAME_API_KEY:-…}` the patterns replace the reference's default,
// but that is the value speaking, not the key.
//
// It differs from Announced on purpose. Announced counts a flag as announcing only when the joined reading
// says more than the element alone; a member whose value already reads as a credential on its own
// (`Bearer <long token> tail`, `sk-ant-… tail`) would then keep its tail. Here the key's assignment decides.
//
// Only the bare form answers: a value written in quotes inside the string is read by assignRE's quoted
// branches (P-043), which already forget the body through its closing quote. A value the patterns never start
// reading — fewer than four bytes of their class before another byte (`p@ss word`), a reference (`${TOKEN}`)
// — announces nothing and stays as the caller's reading leaves it.
func Keyed(key, value string) (start int, ok bool) {
	s := key + "=" + value
	for _, loc := range assignRE.FindAllStringSubmatchIndex(s, -1) {
		if loc[3] != len(key) {
			continue // not the key's own assignment
		}
		g := submatches(s, loc)
		if g[10] == "" || assignValue(g) == g[0] {
			return 0, false // a quoted body (P-043's reading), or a value assignValue refuses
		}
		return loc[20] - len(key) - 1, true
	}
	return 0, false
}
