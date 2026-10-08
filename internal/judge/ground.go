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

// groundedSpan is where a quote landed: the citation, plus the text of the line(s) it landed
// on, taken from the unit — what was SENT, already redacted — and never from the quote. The
// model's quote decides whether a verdict is kept; this is what the report shows as its evidence.
// Showing the quote instead let a single real line vouch for anything written around it: a quote
// that fails whole is retried line by line, so one real line plus invented ones was kept, and the
// invented lines were rendered under a real file:line, indistinguishable from evidence.
type groundedSpan struct {
	file string
	line int
	text string
}

// ground is groundSpan without the text, for callers that only need the citation.
func ground(evidence string, units []sourceUnit) (file string, line int, ok bool) {
	s, ok := groundSpan(evidence, units)
	return s.file, s.line, ok
}

// groundSpan locates a model-supplied quote in the text that was sent and returns its real
// position and the sent line(s) it landed on. Comparison is whitespace-insensitive and
// case-insensitive — models reflow and re-case freely, and neither changes what the line SAYS —
// but it is not fuzzy beyond that: a paraphrase does not match, which is exactly the intent.
func groundSpan(evidence string, units []sourceUnit) (groundedSpan, bool) {
	prepared := normalizeUnits(units)
	if s, ok := groundOne(evidence, prepared); ok {
		return s, true
	}
	// A model asked for "the line" often answers with several, stitched together from
	// different places in the file (seen on a registry-rewriting sample: its two
	// registry writes and a chmod, 50 lines apart, as one quote). Each piece is still a literal
	// quote held to the same bar; the first one that lands is the citation, and its line is all
	// that is shown. Nothing here loosens the match — a paraphrase still fails line by line.
	for _, part := range strings.Split(evidence, "\n") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if s, ok := groundOne(part, prepared); ok {
			return s, true
		}
	}
	return groundedSpan{}, false
}

// normalizedUnit is a unit prepared for matching once per quote rather than once per line of
// it: a stitched quote is retried line by line, and re-normalizing every unit for every line made
// a long quote cost the square of the model's verbosity.
type normalizedUnit struct {
	unit  sourceUnit
	hay   string   // normalized text
	lines []int    // for each hay byte, the 0-based line of unit.text it came from
	src   []string // unit.text split into lines: what a span's text is cut from
}

func normalizeUnits(units []sourceUnit) []normalizedUnit {
	out := make([]normalizedUnit, len(units))
	for i, u := range units {
		hay, lines := normalizeWithLines(u.text)
		out[i] = normalizedUnit{unit: u, hay: hay, lines: lines, src: strings.Split(u.text, "\n")}
	}
	return out
}

// groundOne locates a single quote in the units.
func groundOne(evidence string, units []normalizedUnit) (groundedSpan, bool) {
	needle, _ := normalizeWithLines(evidence)
	if needle == "" {
		return groundedSpan{}, false
	}
	if len(needle) < minGroundedChars {
		// The floor exists to stop a coincidental FRAGMENT match, and quoting a whole short
		// document is not a fragment. Without this, a hook whose command is shorter than the
		// floor (`echo done`) could never be cited at all, and every verdict about it would be
		// discarded for a reason that has nothing to do with whether it is true.
		for _, n := range units {
			if n.hay != "" && n.hay == needle {
				if text, ok := n.text(0, len(n.hay)); ok {
					return groundedSpan{file: n.unit.file, line: n.unit.firstLine, text: text}, true
				}
			}
		}
		return groundedSpan{}, false
	}
	for _, n := range units {
		// Every occurrence, not just the first: one that touches an omission marker is refused,
		// and a later one that does not is still a real citation.
		for from := 0; from < len(n.hay); {
			i := strings.Index(n.hay[from:], needle)
			if i < 0 {
				break
			}
			i += from
			if text, ok := n.text(i, i+len(needle)); ok {
				return groundedSpan{file: n.unit.file, line: n.line(i), text: text}, true
			}
			from = i + 1
		}
	}
	return groundedSpan{}, false
}

// line is the ORIGINAL line that hay byte i came from.
func (n normalizedUnit) line(i int) int {
	u := n.unit
	if u.collapsed {
		return u.firstLine
	}
	if m := len(u.lineMap); m > 0 {
		idx := n.lines[i]
		if idx >= m {
			idx = m - 1
		}
		return u.lineMap[idx]
	}
	return u.firstLine + n.lines[i]
}

// text returns the unit's own line(s) that hay bytes [i, j) came from. It refuses a match that
// touches an omission marker: the marker is a line the excerpt builder inserted, so it is in the
// text that was sent and in no file — a quote of it is not a quote of the artifact.
func (n normalizedUnit) text(i, j int) (string, bool) {
	src := n.src[n.lines[i] : n.lines[j-1]+1]
	for _, l := range src {
		if isOmissionMarker(l) {
			return "", false
		}
	}
	return strings.TrimSpace(strings.Join(src, "\n")), true
}

// isOmissionMarker reports whether a line is the marker capHeadTail puts where it cut an
// excerpt ("# … N line(s) omitted …"). Recognized by its shape, so the excerpt builder stays the
// only owner of the wording; TestGround_OmissionMarkerIsNotEvidence pins the two together against
// capHeadTail's real output.
func isOmissionMarker(line string) bool {
	const head, tail = "# … ", " line(s) omitted …"
	l := strings.TrimSpace(line)
	if len(l) <= len(head)+len(tail) || !strings.HasPrefix(l, head) || !strings.HasSuffix(l, tail) {
		return false
	}
	for _, c := range l[len(head) : len(l)-len(tail)] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
