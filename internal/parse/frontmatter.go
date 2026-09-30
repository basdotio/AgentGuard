// SPDX-License-Identifier: MIT
// Package parse extracts structured metadata from artifact files. frontmatter.go reads
// a skill's SKILL.md YAML frontmatter (name/description) — the basis for hygiene checks.
package parse

import (
	"path/filepath"
	"strings"

	"github.com/basdotio/agent-guard/internal/safeio"
	"gopkg.in/yaml.v3"
)

// maxSkillMDBytes caps how much of SKILL.md is read: frontmatter + a generous body prefix.
// A hostile skill can ship a multi-GB SKILL.md; never materialize it whole (matches the
// judge's readAtMost guard, and the excess body has no scanning value).
const maxSkillMDBytes = 1 << 20

// SkillMeta is the parsed SKILL.md frontmatter plus the file body.
type SkillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Body        string `yaml:"-"` // content after frontmatter (for stale-ref scanning)
	// BodyLine is the 1-based line of SKILL.md that Body starts on. Anything reporting a
	// position inside Body must add this offset, or it cites a line number that looks
	// authoritative and points at the wrong place.
	BodyLine int  `yaml:"-"`
	OK       bool `yaml:"-"` // false when SKILL.md missing/unparsable
}

// ReadSkill parses <skillDir>/SKILL.md. A missing or malformed file yields OK=false
// (never an error — hygiene degrades gracefully).
func ReadSkill(skillDir string) SkillMeta { return ReadMarkdown(filepath.Join(skillDir, "SKILL.md")) }

// ReadMarkdown parses any frontmatter-carrying markdown artifact — a subagent or a slash
// command, which declare a `description` the same way a skill does. A file without
// frontmatter (CLAUDE.md) simply yields an empty Description.
func ReadMarkdown(path string) SkillMeta {
	b, err := safeio.ReadPrefix(path, maxSkillMDBytes)
	if err != nil {
		return SkillMeta{}
	}
	front, body, bodyLine := splitFrontmatter(string(b))
	var m SkillMeta
	if front != "" {
		_ = yaml.Unmarshal([]byte(front), &m)
	}
	m.Body = body
	m.BodyLine = bodyLine
	m.OK = true
	return m
}

// PathScoped reports whether a markdown artifact's frontmatter carries a `paths:` key.
//
// It answers a LOADING question, not a content one: a rule with `paths:` enters context only when
// Claude touches a matching file, while one without it loads every session. Both are scanned; the
// distinction is what a reader needs to judge how much a given rule costs them.
//
// Deliberately parsed into a permissive map rather than added to SkillMeta. `paths:` is written as
// a list, but nothing stops a file carrying a scalar, and a typed field would make yaml reject the
// WHOLE frontmatter on mismatch \u2014 silently emptying name and description for an artifact that
// parses fine today.
func PathScoped(path string) bool {
	b, err := safeio.ReadPrefix(path, maxSkillMDBytes)
	if err != nil {
		return false
	}
	front, _, _ := splitFrontmatter(string(b))
	if front == "" {
		return false
	}
	var doc map[string]any
	if yaml.Unmarshal([]byte(front), &doc) != nil {
		return false
	}
	v, ok := doc["paths"]
	return ok && v != nil
}

const bom = "\uFEFF"

// splitFrontmatter separates a leading `---\n…\n---` block from the body. If there is
// no frontmatter, front is "" and body is the whole content. bodyLine is the 1-based line
// of the ORIGINAL text that body begins on, so a position inside body can be reported as a
// real SKILL.md line.
func splitFrontmatter(s string) (front, body string, bodyLine int) {
	orig := s
	s = strings.TrimPrefix(s, bom)
	s = strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", s, lineOf(orig, len(orig)-len(s))
	}
	rest := strings.TrimPrefix(s, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", s, lineOf(orig, len(orig)-len(s)) // unterminated frontmatter → treat all as body
	}
	front = rest[:end]
	body = rest[end+len("\n---"):]
	return front, body, lineOf(orig, len(orig)-len(body))
}

// lineOf returns the 1-based line number of byte offset in s.
func lineOf(s string, offset int) int {
	if offset < 0 || offset > len(s) {
		return 1
	}
	return 1 + strings.Count(s[:offset], "\n")
}
