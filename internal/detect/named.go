// SPDX-License-Identifier: MIT
package detect

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/bas-guard/internal/collect"
)

// NamedDocs is the one answer to "which documents does this SKILL.md tell the agent to read".
// The judge's companion pass (P-065) sends them to the injection question; the rules run them as
// instruction text rather than as prose (P-091). It used to live in the judge alone, and the
// engine filed the same file as a doc — two answers to one question about one tree.
//
// Named, not every document in the tree: real skills ship references/*.md by the dozen (58% of 400
// real trees name at least one; the median is three, the longest 69), and what the agent reads is
// what the SKILL.md points it at. One level only — a document naming another is not followed — and
// the first MaxNamedDocs named.
const MaxNamedDocs = 8

// namedRefRe finds the file names a SKILL.md body can point the agent at: a path-ish token
// ending in a document extension. Links, backticks and prose all surround the name with characters
// outside the class, so `[guide](docs/guide.md)`, "`PREFLIGHT.md`" and "see notes.txt." all yield it.
var namedRefRe = regexp.MustCompile(`[\w./\\-]+\.(?i:md|markdown|txt|pdf)\b`)

// namedDocExts are the extensions a named document may have: documents an agent reads as prose.
var namedDocExts = map[string]bool{".md": true, ".markdown": true, ".txt": true, ".pdf": true}

// NamedDocs returns the documents under dir that body names, as slash-separated paths relative
// to dir, in order of first mention, deduplicated, at most MaxNamedDocs. A name resolves by its
// path from the skill root first and by its bare file name anywhere in the tree second — SKILL.md
// bodies write both "references/api.md" and "api.md" — through the rules' predicate (FileReach), so a
// link that escapes the tree names nothing. SKILL.md itself and licence files are not named docs.
func NamedDocs(dir, body string) []string {
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
			if !namedDocExts[strings.ToLower(filepath.Ext(p))] || collect.FileReach(dir, p).Kind != collect.Read {
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
	for _, tok := range namedRefRe.FindAllString(body, -1) {
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
		if len(out) == MaxNamedDocs {
			break
		}
	}
	return out
}
