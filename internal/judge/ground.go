// SPDX-License-Identifier: MIT
package judge

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/detect"
)

// ExcerptVersion names how an artifact becomes what the judge is shown, and how an answer's quote is
// checked against it (P-031): which passes an artifact gets (planFor), the text each pass carries —
// condensing, comment stripping, the head/tail caps and their line maps, the declared-purpose, hook,
// MCP and connector renderings, the decode limits, padding folds, the home scrub and redaction order
// — and grounding: whether a quote lands, which file and line it cites and what its snippet shows.
// A --llm report carries it as judge.excerpt_version; `aguard version` prints it as judge-excerpt=.
// The prompts wrapped around the excerpt are PromptVersion's, which is computed.
//
// BUMP IT, in the same commit, whenever a change alters — for some artifact — what any pass sends
// or how any quote grounds. Do NOT bump it for comments, report wording or anything that only
// shapes what happens after a verdict is grounded (consensus, severity clamping, rule mapping).
//
// It is a number because none of that can be hashed without hashing code. What makes forgetting it
// visible is TestExcerptVersion_IsPinnedWithItsGolden: a fixture's planned excerpts and grounding
// results are pinned to a digest TOGETHER with this value, so changing either alone is red. A change
// the fixture does not exercise still relies on the editor; a forgotten bump lets two reports claim
// the same excerpts while the judge was shown different text.
const ExcerptVersion = 3

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
// invented lines were rendered under a real file:line, indistinguishable from evidence. Lines too
// long to show whole are cut around the quoted bytes (normalizedUnit.window), never from the start.
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
// position and the sent line(s) it landed on. Comparison is whitespace-insensitive (any Unicode
// space), blind to invisible characters, and ASCII case-insensitive (foldForMatch) — models reflow
// and re-case freely, and none of that changes what the line SAYS — but it is not fuzzy beyond
// that: a paraphrase does not match, which is exactly the intent.
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
	offs  []int    // for each hay byte, its byte offset in unit.text
	src   []string // unit.text split into lines: what a span's text is cut from
}

func normalizeUnits(units []sourceUnit) []normalizedUnit {
	out := make([]normalizedUnit, len(units))
	for i, u := range units {
		hay, lines, offs := normalizeWithLines(u.text)
		out[i] = normalizedUnit{unit: u, hay: hay, lines: lines, offs: offs, src: strings.Split(u.text, "\n")}
	}
	return out
}

// groundOne locates a single quote in the units.
func groundOne(evidence string, units []normalizedUnit) (groundedSpan, bool) {
	needle, _, _ := normalizeWithLines(evidence)
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

// text returns the unit's own line(s) that hay bytes [i, j) came from — or, when those lines are
// longer than a snippet may be, a window of them cut around the matched bytes. It refuses a match
// that touches an omission marker: the marker is a line the excerpt builder inserted, so it is in
// the text that was sent and in no file — a quote of it is not a quote of the artifact.
func (n normalizedUnit) text(i, j int) (string, bool) {
	src := n.src[n.lines[i] : n.lines[j-1]+1]
	for _, l := range src {
		if isOmissionMarker(l) {
			return "", false
		}
	}
	if whole := strings.TrimSpace(strings.Join(src, "\n")); len(whole) <= maxSnippetBytes {
		return whole, true
	}
	from, to := n.offs[i], n.offs[j-1]+1
	if to-from > windowBudget && j-i <= windowBudget {
		// The quote fits once folded (runs of whitespace collapsed, invisible runes gone), but not as
		// source bytes: a run INSIDE it is what overflows the window. Cutting the source here put the
		// cut inside the run, and trimming left the quote's first word as the whole of the evidence.
		return n.collapsedWindow(from, to), true
	}
	return n.window(from, to), true
}

// windowBudget is the room a window has for text: a snippet's bound less an ellipsis at each end.
const windowBudget = maxSnippetBytes - 2*len(ellipsis)

// window cuts a snippet out of the unit's text around the matched source bytes [from, to), for a
// match whose whole lines do not fit in a snippet. Cutting the line from its START instead let an
// author pad an injection line with prose: the quote grounded, the finding — a high LLM-007 among
// them — was reported, and its evidence was the line's harmless first 512 bytes.
//
// The window keeps the matched text (for a quote longer than the window, its beginning), spends
// the room left on context to either side, never reaches past the matched lines, falls on rune
// boundaries, and marks each end it cut with an ellipsis; markers included it is at most
// maxSnippetBytes. The unit text was redacted before it was sent, so this cuts redacted text
// (invariant #3: redact, then truncate).
func (n normalizedUnit) window(from, to int) string {
	lo, hi := matchedLines(n.unit.text, from, to)
	return cutAround(n.unit.text, lo, from, to, hi)
}

// collapsedWindow is window for a match whose source bytes overflow the window only because of the
// runs of whitespace or invisible runes inside it. Measured in source bytes, the run alone could fill
// the window: the cut fell inside it and trimming left "…Note:…" as the evidence of a directive that
// grounded. It renders the matched lines folded — every whitespace run one space, invisible runes
// gone — by the same walk grounding compared them under (foldForMatch, via collapseRuns), so this
// is the text the quote was found in, case aside, and windows that around the quote. The collapsed
// match is exactly as long as the normalised quote, so on text()'s path it always fits; should it
// not, the bound holds by showing its head and tail.
func (n normalizedUnit) collapsedWindow(from, to int) string {
	lo, hi := matchedLines(n.unit.text, from, to)
	c, cf, ct := collapseRuns(n.unit.text[lo:hi], from-lo, to-lo)
	if ct-cf > windowBudget {
		return headTail(c, cf, ct)
	}
	return cutAround(c, 0, cf, ct, len(c))
}

// matchedLines is the extent of the line(s) holding source bytes [from, to), without surrounding
// whitespace — what text() would have shown had it fit.
func matchedLines(text string, from, to int) (lo, hi int) {
	lo = strings.LastIndexByte(text[:from], '\n') + 1
	hi = len(text)
	if k := strings.IndexByte(text[to:], '\n'); k >= 0 {
		hi = to + k
	}
	lo = from - len(strings.TrimLeftFunc(text[lo:from], unicode.IsSpace))
	hi = to + len(strings.TrimRightFunc(text[to:hi], unicode.IsSpace))
	return lo, hi
}

// cutAround cuts text[lo:hi] to a window around [from, to): the match (its beginning, if it is
// longer than the window), then the room left as context to either side, on rune boundaries, with
// an ellipsis at each end it cut.
func cutAround(text string, lo, from, to, hi int) string {
	if to-from >= windowBudget {
		to = from + windowBudget
	} else {
		room := windowBudget - (to - from)
		left := min(room/2, from-lo)
		right := min(room-left, hi-to)
		left = min(room-right, from-lo) // room the right side could not use goes to the left
		from, to = from-left, to+right
	}
	for from < to && !utf8.RuneStart(text[from]) {
		from++
	}
	for to > from && to < len(text) && !utf8.RuneStart(text[to]) {
		to--
	}
	s := strings.TrimSpace(text[from:to])
	if from > lo {
		s = ellipsis + s
	}
	if to < hi {
		s += ellipsis
	}
	return s
}

// collapseRuns returns s folded the way grounding reads it (foldForMatch: every run of match space
// one space, ignored runes gone), case kept, and where s's bytes [from, to) landed in the result.
// s[from] and s[to-1] belong to kept runes: a grounded match never begins or ends on a space or on
// an ignored rune. Every space in the result is a collapsed run, never a kept byte, which is how
// the one in front of the match is told apart from the match's first byte at the same offset.
func collapseRuns(s string, from, to int) (out string, cf, ct int) {
	var b strings.Builder
	b.Grow(len(s))
	cf = -1
	foldForMatch(s, func(c byte, off int) {
		if cf < 0 && off >= from && c != ' ' {
			cf = b.Len()
		}
		b.WriteByte(c)
		if off < to {
			ct = b.Len()
		}
	})
	if cf < 0 {
		cf = b.Len()
	}
	return b.String(), cf, max(cf, ct)
}

// headTail shows c[cf:ct], a match too long for a window, as its head and its tail joined by an
// ellipsis, cut on rune boundaries, with an ellipsis at each end of c it does not reach; at most
// maxSnippetBytes.
func headTail(c string, cf, ct int) string {
	half := (maxSnippetBytes - 3*len(ellipsis)) / 2
	h := cf + half
	for h > cf && !utf8.RuneStart(c[h]) {
		h--
	}
	t := ct - half
	for t < ct && !utf8.RuneStart(c[t]) {
		t++
	}
	s := strings.TrimRightFunc(c[cf:h], unicode.IsSpace) + ellipsis + strings.TrimLeftFunc(c[t:ct], unicode.IsSpace)
	if cf > 0 {
		s = ellipsis + s
	}
	if ct < len(c) {
		s += ellipsis
	}
	return s
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

// normalizeWithLines folds s the way grounding reads it (foldForMatch) and lowercases ASCII,
// returning the result plus, for each byte of it, the 0-based line of s it came from and its
// byte offset in s. The line map is what turns "the quote is in here somewhere" into a citable
// line number; the offsets are what lets a snippet be cut around the quoted bytes (window).
// A kept rune keeps its own bytes, each mapped to its own offset, so a multi-byte rune maps back
// byte for byte. A collapsed space maps to the first byte of the rune after its run: it can never
// begin or end a match, because a normalized quote has no leading or trailing space. Only ASCII is
// lowercased: folding other letters can change their length, and the map is per byte.
func normalizeWithLines(s string) (string, []int, []int) {
	var b strings.Builder
	b.Grow(len(s))
	lines := make([]int, 0, len(s))
	offs := make([]int, 0, len(s))
	line, counted := 0, 0
	foldForMatch(s, func(c byte, off int) {
		line += strings.Count(s[counted:off], "\n") // offsets only grow, so each byte is counted once
		counted = off
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
		lines = append(lines, line)
		offs = append(offs, off)
	})
	return b.String(), lines, offs
}

// foldForMatch walks s as grounding reads it and calls emit for each byte of the result: a run of
// match space (isMatchSpace) becomes one ' ' — none before the first kept rune, none after the
// last — a rune grounding ignores (isMatchIgnored) is skipped without ending a run, and every other
// rune is kept as its own source bytes (an invalid byte is kept as itself). off is the offset in s
// the byte came from; a collapsed space reports the first byte of the rune after its run.
//
// It is the one walk behind both sides of a snippet: normalizeWithLines folds the text and the
// quote with it to match them, and collapseRuns folds the matched lines with it to show them. Two
// walks sharing only a predicate can still disagree on where a run ends; one walk cannot.
func foldForMatch(s string, emit func(c byte, off int)) {
	started, pending := false, false
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		switch {
		case isMatchSpace(r):
			pending = started
		case isMatchIgnored(r):
			// Neither kept nor a space: a run it sits inside goes on, and it starts nothing.
		default:
			if pending {
				emit(' ', i)
				pending = false
			}
			for k := i; k < i+w; k++ {
				emit(s[k], k)
			}
			started = true
		}
		i += w
	}
}

// isMatchSpace is the whitespace grounding collapses: every rune unicode.IsSpace reports — ASCII
// whitespace, and also NEL, NBSP, U+1680, U+2000–U+200A, U+2028/U+2029, U+202F, U+205F and U+3000.
// ASCII alone let a directive be padded from within by U+3000: the pad stayed in the normalised
// quote as ordinary bytes, the window cut inside it and showed "…Note:…", and a model that quoted
// the pad as one space did not ground at all. Read only by foldForMatch, so a snippet shown
// collapsed is collapsed exactly the way the quote was found.
func isMatchSpace(r rune) bool { return unicode.IsSpace(r) }

// isMatchIgnored is what grounding drops outright: the zero-width, joiner and bidi-control runes
// INJ-004 reports in file contents and report.Sanitize neutralises in names — detect.Invisible,
// the one table, not a copy. They occupy no visual space, so a model reading "ig<U+200B>nore"
// quotes "ignore", and a run of them inside a directive would otherwise fill the window with
// nothing a reader can see. Dropped, not collapsed: they separate no words on screen either.
func isMatchIgnored(r rune) bool { return detect.Invisible(r) }
