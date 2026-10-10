// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"strings"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// maxEvidenceBytes bounds one static finding's evidence as the judge sends it: a triage item's
// `file:line snippet` (P-037). The snippet was clipped at detect time and the file position never was,
// so a deep enough path sent as much as the path held. A real `file:line snippet` is a few hundred
// bytes; the bound leaves room for a long path and a whole clipped snippet.
const maxEvidenceBytes = 1000

// boundedEvidence cuts already-redacted evidence to maxEvidenceBytes on a character boundary, the
// ellipsis included, and settles it. The caller redacts FIRST (invariant #3), so a token straddling the cut
// is already <REDACTED> and leaves no head behind.
func boundedEvidence(red string) string {
	ev, _ := settle(red, maxEvidenceBytes, func(s string, limit int) string { return capBytes(s, limit-len(ellipsis)) })
	return ev
}

// maxRedactRounds bounds fixRedact's loop.
const maxRedactRounds = 4

// fixRedact returns s as it is sent: sendable, then redacted until Redact leaves it unchanged. Redact is
// idempotent by contract, so that is one pass and one comparison; the bound only keeps a broken contract
// from looping, and FuzzPlanFields would then report the field.
func fixRedact(s string) string {
	s = sendable(s)
	for i := 0; i < maxRedactRounds; i++ {
		r := detect.Redact(s)
		if r == s {
			return s
		}
		s = r
	}
	return s
}

// settle is the last step of building a field that is one text (P-037): f — already redacted — cut to max
// by the field's own cap (recap) if it is longer, then the whole field redacted to a fixed point
// (fixRedact), and, when that lengthened it past max, cut again at a limit that tightens every round, since
// a cut can itself end in a shape Redact rewrites (`--token …`). It reports whether it cut at all.
//
// Every field was assembled from parts redacted one at a time and then joined, separated or cut, and
// nothing made the result a fixed point: a pattern reading across a join, or a run that looks like a token
// only once cut, was left for a second pass — which would have changed what had been sent. The fallback,
// an empty field, is a fixed point within every cap; reaching it takes a cut that Redact rewrites at every
// limit tried.
func settle(f string, max int, recap func(string, int) string) (string, bool) {
	cut := false
	for limit := max; ; limit -= max/16 + 1 {
		if len(f) > max {
			if limit <= 0 {
				return "", true
			}
			f, cut = recap(f, limit), true
		}
		s := fixRedact(f)
		if len(s) <= max {
			return s, cut
		}
		f = s
	}
}

// settleExcerpt is settle for a head/tail excerpt within maxExcerptBytes, with its line map: the re-cut is
// capHeadTail again, and the map follows the text (fixRedact never adds or removes a line).
func settleExcerpt(text string, lm []int) (string, []int) {
	out, _ := settle(text, maxExcerptBytes, func(s string, limit int) string {
		s, lm = capHeadTail(s, lm, limit)
		return s
	})
	if out == "" {
		return "", nil
	}
	return out, lm
}

// span is where one unit's text sits in a field built from whole lines: its first line and how many.
type span struct{ first, n int }

// joined renders texts separated by sep, a separator that is one line break or begins and ends with one
// — the layout of the deobfuscation field ("\n---\n") and the collusion digest ("\n") — and says where
// each text sits.
func joined(sep string) func([]string) (string, []span) {
	return func(texts []string) (string, []span) {
		var b strings.Builder
		spans := make([]span, len(texts))
		line := 0
		for i, t := range texts {
			if i > 0 {
				b.WriteString(sep)
				line += strings.Count(sep, "\n")
			}
			spans[i] = span{line, strings.Count(t, "\n") + 1}
			b.WriteString(t)
			line += strings.Count(t, "\n")
		}
		return b.String(), spans
	}
}

// fitUnits is how a field assembled from several units' texts meets its cap (P-037): whole texts, from the
// first, as many as render — and settle — within max — never one cut in half, since each is a unit grounding checks a
// quote against, and the rest are left out and counted for the caller to disclose. It returns the field,
// the text of each unit as it stands in the field, and how many texts were left out.
//
// The deobfuscation pass used to join all its payloads (up to 6,435 bytes, past the 6,000 the excerpt
// cap states) and the collusion digest every line it had, with no cap at all.
func fitUnits(texts []string, max int, render func([]string) (string, []span)) (string, []string, int) {
	n, sum := 0, 0 // start from the most texts whose bytes alone fit: the separators only add to them
	for n < len(texts) && sum+len(texts[n]) <= max {
		sum += len(texts[n])
		n++
	}
	for ; n > 0; n-- {
		field, spans := render(texts[:n])
		// The last step is the whole field redacted to a fixed point (settle's reason). Redact replaces
		// within a line and never adds or removes one, so each unit is still the same lines of the field
		// and is read back from it: the units hold exactly the bytes sent, headers and separators in none.
		// A count that moved anyway cannot be read back, and the field is cut again instead.
		if s := fixRedact(field); len(s) <= max && strings.Count(s, "\n") == strings.Count(field, "\n") {
			return s, reread(s, spans), len(texts) - n
		}
	}
	return "", nil, len(texts)
}

// reread returns the text at each span of field.
func reread(field string, spans []span) []string {
	lines := strings.Split(field, "\n")
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = strings.Join(lines[s.first:s.first+s.n], "\n")
	}
	return out
}

// boundedNote is shortenedNote's twin for the two fields fitUnits cuts — the deobfuscation payloads and
// the collusion digest (P-037): every planned call that left some out collapses into one LLM-000
// (invariant #5), once per question and only for calls that will be made. The MCP configuration keeps its
// own note, whose sentence is about configurations.
func boundedNote(tasks []task) (model.Finding, bool) {
	seen := map[int]bool{}
	var parts []string
	for _, t := range tasks {
		if t.shortened == "" || t.kind != taskJudge || !fitsByUnits(t.req.Mode) || seen[t.group] {
			continue
		}
		seen[t.group] = true
		parts = append(parts, t.label+" ("+t.shortened+")")
	}
	if len(parts) == 0 {
		return model.Finding{}, false
	}
	if len(parts) > 3 {
		parts = append(parts[:3], fmt.Sprintf("and %d more", len(parts)-3))
	}
	return coverageNote("the judge saw a shortened excerpt for " + strings.Join(parts, "; ") +
		". What was left out was not judged; the static findings are unaffected."), true
}

// fitsByUnits reports whether a mode's behavior is assembled by fitUnits, and so may leave whole units out.
func fitsByUnits(m Mode) bool { return m == ModeExplain || m == ModeCollusion }
