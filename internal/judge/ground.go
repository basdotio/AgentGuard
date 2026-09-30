// SPDX-License-Identifier: MIT
package judge

import "strings"

// A model can produce a fluent, plausible, entirely invented finding. Nothing downstream can
// tell that apart from a real one — same shape, same severity, same confident sentence. So a
// verdict only survives if the text it quotes can be FOUND in what we actually sent it.
//
// That check buys two things at once: hallucinations are dropped instead of rendered, and the
// findings that survive gain a real file:line (they used to carry Line 0, i.e. "somewhere in
// this artifact — go look yourself"). It is also the precondition for ever letting the judge
// influence the score: an unverifiable claim must never be able to move a number.
//
// The comparison is against the REDACTED text that left the machine, never the file on disk.
// Re-reading the original here would put unredacted content back in play after the single
// redaction chokepoint (§16.3) — the check must not become a side channel.

// sourceUnit is one piece of what was sent to the model, with its origin.
type sourceUnit struct {
	file      string // path relative to the artifact root, redacted
	text      string // redacted text, exactly as sent
	firstLine int    // 1-based line in the ORIGINAL file that text's first line came from
	// collapsed marks text that does not exist in the file line-by-line — a DECODED blob,
	// which lived on one line in its encoded form. Every position in it maps to firstLine;
	// counting lines inside it would invent a location that was never in the file.
	collapsed bool
	// lineMap, when set, gives the 1-based ORIGINAL line of each line of text. Condensed
	// excerpts (excerpt.go: blank runs collapsed, comments dropped, head+tail kept) are no
	// longer contiguous with the file, so firstLine arithmetic would cite the wrong place —
	// and a wrong citation on a real finding is worse than none: the reader goes there, sees
	// nothing, and stops believing the report.
	lineMap []int
}

// minGroundedChars is the shortest quote worth trusting. A three-character "curl" appears in
// half the corpus, so accepting it would ground a hallucination on a coincidence.
const minGroundedChars = 16

// ground locates a model-supplied quote in the text that was sent and returns its real
// position. Comparison is whitespace-insensitive and case-insensitive — models reflow and
// re-case freely, and neither changes what the line SAYS — but it is not fuzzy beyond that:
// a paraphrase does not match, which is exactly the intent.
func ground(evidence string, units []sourceUnit) (file string, line int, ok bool) {
	if file, line, ok = groundOne(evidence, units); ok {
		return file, line, true
	}
	// A model asked for "the line" often answers with several, stitched together from
	// different places in the file (seen on a registry-rewriting sample: its two
	// registry writes and a chmod, 50 lines apart, as one quote). Each piece is still a literal
	// quote held to the same bar; the first one that lands is the citation. Nothing here loosens
	// the match — a paraphrase still fails line by line.
	for _, part := range strings.Split(evidence, "\n") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if file, line, ok = groundOne(part, units); ok {
			return file, line, true
		}
	}
	return "", 0, false
}

// groundOne locates a single quote in the units.
func groundOne(evidence string, units []sourceUnit) (file string, line int, ok bool) {
	needle, _ := normalizeWithLines(evidence)
	if needle == "" {
		return "", 0, false
	}
	if len(needle) < minGroundedChars {
		// The floor exists to stop a coincidental FRAGMENT match, and quoting a whole short
		// document is not a fragment. Without this, a hook whose command is shorter than the
		// floor (`echo done`) could never be cited at all, and every verdict about it would be
		// discarded for a reason that has nothing to do with whether it is true.
		for _, u := range units {
			if hay, _ := normalizeWithLines(u.text); hay != "" && hay == needle {
				return u.file, u.firstLine, true
			}
		}
		return "", 0, false
	}
	for _, u := range units {
		hay, lines := normalizeWithLines(u.text)
		i := strings.Index(hay, needle)
		if i < 0 {
			continue
		}
		if u.collapsed {
			return u.file, u.firstLine, true
		}
		if n := len(u.lineMap); n > 0 {
			idx := lines[i]
			if idx >= n {
				idx = n - 1
			}
			return u.file, u.lineMap[idx], true
		}
		return u.file, u.firstLine + lines[i], true
	}
	return "", 0, false
}

// normalizeWithLines lowercases s and collapses every whitespace run to a single space,
// returning the result plus, for each byte of it, the 0-based line of s it came from. The
// line map is what turns "the quote is in here somewhere" into a citable line number.
func normalizeWithLines(s string) (string, []int) {
	var b strings.Builder
	b.Grow(len(s))
	lines := make([]int, 0, len(s))
	line, pendingSpace := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f' {
			if c == '\n' {
				line++
			}
			if b.Len() > 0 {
				pendingSpace = true // collapse; leading whitespace is dropped entirely
			}
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			lines = append(lines, line)
			pendingSpace = false
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
		lines = append(lines, line)
	}
	return b.String(), lines
}
