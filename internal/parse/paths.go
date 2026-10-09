// SPDX-License-Identifier: MIT
package parse

import (
	"strings"
	"unicode"
)

// maxBraceExpansions bounds the brace groups expanded while deciding one rule's `paths:`. The bytes
// that decide how much expansion there is belong to the artifact's author, and forty `{a,b}` groups
// are 2^40 expansions. An answer cut short by the bound is "no usable glob" — the rule loads every
// session — the reading that can overstate what is in context but never understate it. A real
// pattern returns on its first expansion and never comes near the bound.
const maxBraceExpansions = 1024

// honoursPaths reports whether Claude Code would scope a rule by this `paths:` value at all.
//
// Mirrors the loader measured on Claude Code 2.1.107 (P-024), read from the CLI's own code and
// checked shape by shape against an InstructionsLoaded hook: strings are split on commas outside
// braces, trimmed and brace-expanded; lists are flattened; any other value contributes nothing; a
// trailing "/**" is dropped and empty strings discarded. When nothing is left, or only "**", the
// rule is treated as having no paths and loads every session. So `paths: []`, `paths: ""`,
// `paths: "**"` and `paths: 5` are not path-scoped, whatever the key's presence suggests.
func honoursPaths(v any) bool {
	budget := maxBraceExpansions
	return anyUsableGlob(v, &budget)
}

func anyUsableGlob(v any, budget *int) bool {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if anyUsableGlob(e, budget) {
				return true
			}
		}
	case string:
		for _, item := range splitTopLevelCommas(x) {
			if expandsToUsableGlob(item, budget) {
				return true
			}
		}
	}
	return false
}

// splitTopLevelCommas splits on commas that are not inside braces, trims each part and drops the
// empty ones — the comma list form `paths: "src/**, lib/**"`.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	add := func(part string) {
		if t := jsTrim(part); t != "" {
			out = append(out, t)
		}
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				add(s[start:i])
				start = i + 1
			}
		}
	}
	add(s[start:])
	return out
}

// expandsToUsableGlob expands the first `{…}` group (the first '{', up to the first '}' after it,
// at least one character between) into its comma-separated, trimmed alternatives and recurses,
// returning on the first expansion that is a usable glob. Like the loader's pattern, a group is not
// expanded when what follows it contains a line break. Depth-first with an early return, so the
// budget is only spent by inputs whose expansions are all empty or "**".
func expandsToUsableGlob(s string, budget *int) bool {
	if open := strings.IndexByte(s, '{'); open >= 0 {
		end := strings.IndexByte(s[open+1:], '}')
		if end > 0 && !strings.ContainsAny(s[open+1+end+1:], "\n\r\u2028\u2029") {
			if *budget <= 0 {
				return false
			}
			*budget--
			prefix, rest := s[:open], s[open+1+end+1:]
			for _, alt := range strings.Split(s[open+1:open+1+end], ",") {
				if expandsToUsableGlob(prefix+jsTrim(alt)+rest, budget) {
					return true
				}
			}
			return false
		}
	}
	g := strings.TrimSuffix(s, "/**")
	return g != "" && g != "**"
}

// jsTrim trims what JavaScript's String.prototype.trim does: Unicode white space and line breaks,
// plus U+FEFF, minus U+0085 — the two places it differs from unicode.IsSpace.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == '\uFEFF' || (r != '\u0085' && unicode.IsSpace(r))
	})
}
