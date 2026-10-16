// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
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
// points it at. Two levels — a document named by a named document is read, nothing deeper — and the
// first maxCompanionDocs named, within the excerpt budget the behaviour pass has; what is left out
// is disclosed through boundedNote like the other fitted fields (invariant #5). Since P-070 the documents
// those documents name are read too — two levels, not one — and the scripts the body names go first in
// the behaviour excerpt (behaviorExcerpt).
const maxCompanionDocs = 8

// companionRefRe finds the file names a SKILL.md body can point the agent at: a path-ish token
// ending in a document extension. Links, backticks and prose all surround the name with characters
// outside the class, so `[guide](docs/guide.md)`, "`PREFLIGHT.md`" and "see notes.txt." all yield it.
var companionRefRe = regexp.MustCompile(`[\w./\\-]+\.(?i:md|markdown|txt|pdf)\b`)

// companionExts are the extensions a named companion may have: documents an agent reads as prose.
// Scripts and config the body names are already the behaviour pass's.
var companionExts = map[string]bool{".md": true, ".markdown": true, ".txt": true, ".pdf": true}

// companionDocs returns the documents under dir that body names, as slash-separated paths relative
// to dir, in order of first mention, deduplicated, at most maxCompanionDocs. A name resolves by its
// path from the skill root first and by its bare file name anywhere in the tree second — SKILL.md
// bodies write both "references/api.md" and "api.md" — through the rules' predicate (FileReach), so a
// link that escapes the tree names nothing. SKILL.md itself and licence files are not companions.
func companionDocs(dir, body string) []string {
	var index map[string]string // relative path → itself; bare name → first relative path
	build := func() {
		index = map[string]string{}
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if collect.ExcludedFromScan(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !companionExts[strings.ToLower(filepath.Ext(p))] || collect.FileReach(dir, p).Kind != collect.Read {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			index[rel] = rel
			if base := strings.ToLower(filepath.Base(rel)); index[base] == "" {
				index[base] = rel
			}
			return nil
		})
	}
	var out []string
	seen := map[string]bool{}
	add := func(text string) {
		for _, tok := range companionRefRe.FindAllString(text, -1) {
			name := strings.TrimLeft(strings.ReplaceAll(tok, `\`, "/"), "./")
			base := strings.ToLower(filepath.Base(name))
			if base == "skill.md" || strings.HasPrefix(base, "license") {
				continue
			}
			if index == nil {
				build()
			}
			rel := index[name]
			if rel == "" {
				rel = index[base]
			}
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
			if len(out) == maxCompanionDocs {
				return
			}
		}
	}
	add(body)
	// One more level (P-070): a document the body names may itself say "read workspace.md first", and
	// the agent does. Measured: 4 of 62 SkillHarm trees the judge missed kept the payload exactly there.
	// The documents named by THOSE are not followed, and the cap is shared.
	for _, rel := range append([]string(nil), out...) {
		if len(out) == maxCompanionDocs {
			break
		}
		if strings.EqualFold(filepath.Ext(rel), ".pdf") {
			continue
		}
		if raw := readAtMost(filepath.Join(dir, filepath.FromSlash(rel)), maxFileRead); raw != nil {
			add(string(raw))
		}
	}
	return out
}

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
