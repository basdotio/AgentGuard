// SPDX-License-Identifier: MIT
package redact

import (
	"regexp"
	"strings"
)

// replaceEach is the one loop every pattern of Credentials runs through: each match of re in s is
// replaced by repl(groups), where groups are the match's submatches read in place, and the replacement
// is first offered to keep (nil keeps every one). A replacement keep refuses leaves its match as
// written; the other matches of re, and the patterns after it, still apply.
//
// Submatches are read in place rather than by matching the match again (FindStringSubmatch(m), as the
// patterns once did): a quoted value may end at the end of its line (P-043), and matched again on its
// own a shorter match ends there too — a different branch can then win than the one that matched.
func replaceEach(re *regexp.Regexp, s string, repl func(g []string) string, keep func(match, repl string) bool) string {
	locs := re.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		g := submatches(s, loc)
		r := repl(g)
		if r != g[0] && keep != nil && !keep(g[0], r) {
			r = g[0]
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString(r)
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// submatches reads the groups of one match of s in place, from its index pairs loc; a group that did not
// take part is "".
func submatches(s string, loc []int) []string {
	g := make([]string, len(loc)/2)
	for i := range g {
		if loc[2*i] >= 0 {
			g[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	return g
}
