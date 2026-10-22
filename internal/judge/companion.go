// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/basdotio/bas-guard/internal/detect"
)

// A skill's instruction text is not only SKILL.md. "Read PREFLIGHT.md in full before starting" makes
// PREFLIGHT.md as much an instruction file as the one that named it, and an agent follows that line
// without a second thought. Until P-065 nothing read it: the engine files any .md that is not
// SKILL.md / CLAUDE.md as a prose doc (injection rules only), and the behaviour excerpt lists scripts
// and config extensions. Measured on 240 SkillHarm trees, 92 put the payload in exactly such a
// companion — a document or a PDF the SKILL.md tells the agent to read first — and the tool caught 0.
//
// So the companion pass: the documents SKILL.md's body NAMES, in the order it names them, go to the
// injection question as text the agent will read, beside the declared purpose. Named, not every
// document in the tree: real skills ship references/*.md by the dozen (58% of 400 real trees name at
// least one; the median is three, the longest 69), and what the agent reads is what the SKILL.md
// points it at. One level only — a document naming another document is not followed — and the
// first maxCompanionDocs named, within the excerpt budget the behaviour pass has; what is left out
// is disclosed through boundedNote like the other fitted fields (invariant #5).
// The documents themselves are detect.NamedDocs — the same answer the rules use since P-091.
const maxCompanionDocs = detect.MaxNamedDocs

// companionDocs is detect.NamedDocs: one answer, for the judge and for the rules.
func companionDocs(dir, body string) []string { return detect.NamedDocs(dir, body) }

// companionExcerpt renders the companion pass's text: each named document under a `# <file>` line,
// redacted before it is condensed (blank runs only — a document's comment syntax is prose) or
// capped, within the behaviour pass's per-file and total budgets, and the units grounding reads a
// quote against. A PDF contributes its text layer (pdfText) as one collapsed unit, cited by file
// alone: its "lines" are the extractor's, not positions a reader can open. shortened says what was
// left out — documents past the budget, PDFs without a readable text layer — for boundedNote.
func companionExcerpt(dir, body string, eg egress) (string, []sourceUnit, string) {
	docs := companionDocs(dir, body)
	if len(docs) == 0 {
		return "", nil, ""
	}
	var units []sourceUnit
	total, opaque, past := 0, 0, 0
	for _, rel := range docs {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		raw := readAtMost(p, maxFileRead)
		if raw == nil {
			opaque++
			continue
		}
		text, pdf := string(raw), strings.EqualFold(filepath.Ext(rel), ".pdf")
		if pdf {
			if text = pdfText(raw); text == "" {
				opaque++
				continue
			}
		}
		red, lm := condense(p, eg.redact(text), false)
		red, lm = capHeadTail(red, lm, maxFileBytes)
		header := "# " + detect.Redact(rel) + "\n"
		room := maxExcerptBytes - total - len(header) - 2
		if room <= 0 {
			past++
			continue
		}
		red, lm = capHeadTail(red, lm, room)
		u := sourceUnit{file: detect.Redact(rel), text: red, firstLine: 1, lineMap: lm}
		if pdf {
			u = sourceUnit{file: detect.Redact(rel), text: red, firstLine: 0, collapsed: true}
		}
		units = append(units, u)
		total += len(header) + len(red) + 2
	}
	if len(units) == 0 {
		return "", nil, ""
	}
	files, texts := make([]string, len(units)), make([]string, len(units))
	for k, u := range units {
		files[k], texts[k] = u.file, u.text
	}
	text, sent, left := fitUnits(texts, maxExcerptBytes, headed(files))
	units = units[:len(sent)]
	for k := range sent {
		units[k].text = sent[k]
	}
	var parts []string
	if n := past + left; n > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d named document(s) past the %d-byte excerpt not sent", n, len(docs)-opaque, maxExcerptBytes))
	}
	if opaque > 0 {
		parts = append(parts, fmt.Sprintf("%d named document(s) unreadable or without a text layer", opaque))
	}
	return text, units, strings.Join(parts, "; ")
}
