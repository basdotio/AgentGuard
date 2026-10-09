// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// CLAUDE.md's per-package guardrails live in .claude/rules/*.md (P-003). A rules file with a
// `paths:` frontmatter is loaded only when Claude touches a matching file — which means a glob
// that matches nothing turns that file into one that is never read, silently. Nothing else
// checks this: Claude Code does not report an unmatched glob. This test does, and it also
// holds each file to the documented ~200-line guidance so the split does not grow back into
// one 800-line file under a different name. A `paths:` block that does not open on line 1 is
// reported too: Claude Code ignores it, and so, until it was reported, did this check.

const maxRuleLines = 200

// ruleFileProblems returns one message per defect in the rules directory under root. A missing
// directory is not a defect: the test passes on a tree that has not been split yet.
func ruleFileProblems(root string) ([]string, error) {
	dir := filepath.Join(root, ".claude", "rules")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tracked, err := repoFiles(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".claude", "rules", e.Name()))
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if n := strings.Count(string(b), "\n"); n > maxRuleLines {
			problems = append(problems, fmt.Sprintf("%s: %d lines, guidance is %d — split it", rel, n, maxRuleLines))
		}
		globs, err := frontmatterPaths(string(b))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		for _, g := range globs {
			re, err := globToRegexp(g)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: paths entry %q: %v", rel, g, err))
				continue
			}
			matched := false
			for _, f := range tracked {
				if re.MatchString(f) {
					matched = true
					break
				}
			}
			if !matched {
				problems = append(problems, fmt.Sprintf("%s: paths entry %q matches no file — this rule would never load", rel, g))
			}
		}
	}
	return problems, nil
}

// frontmatterPaths returns the `paths:` list from a leading `---` block, or nil when the file
// has no frontmatter (an always-loaded rule). A `paths:` block that does not open on line 1 is an
// error rather than "no frontmatter": Claude Code reads frontmatter only when the opening `---` is
// the file's first line, so such a rule loads every session — and here its globs would go unchecked.
func frontmatterPaths(s string) ([]string, error) {
	if !strings.HasPrefix(s, "---\n") {
		if misplacedFrontmatter(s) {
			return nil, fmt.Errorf("frontmatter must start on line 1 (Claude Code ignores it anywhere else, so this rule loads every session)")
		}
		return nil, nil
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return nil, fmt.Errorf("frontmatter opened with --- but never closed")
	}
	var fm struct {
		Paths []string `yaml:"paths"`
	}
	if err := yaml.Unmarshal([]byte(s[4:4+end]), &fm); err != nil {
		return nil, fmt.Errorf("frontmatter is not YAML: %v", err)
	}
	if len(fm.Paths) == 0 {
		return nil, fmt.Errorf("frontmatter present but paths: is empty — drop the block to load always, or list globs")
	}
	return fm.Paths, nil
}

// misplacedFrontmatter reports whether a file that does not start with `---` still carries a
// `---`-delimited block with a `paths` key: below a comment, a blank line or a heading, or behind a
// byte-order mark. Requiring the key keeps `---` rules in a body from counting; blocks inside code
// fences are examples, not frontmatter. A block opening on line 1 counts only behind a BOM: a CRLF
// one there is honoured by Claude Code.
func misplacedFrontmatter(s string) bool {
	const bom = "\uFEFF"
	lines := strings.Split(strings.TrimPrefix(s, bom), "\n")
	open, fenced := -1, false
	for i, raw := range lines {
		l := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || l != "---" {
			continue
		}
		if open >= 0 && hasPathsKey(strings.Join(lines[open+1:i], "\n")) {
			return open > 0 || strings.HasPrefix(s, bom)
		}
		open = i
	}
	return false
}

func hasPathsKey(block string) bool {
	var m map[string]any
	if yaml.Unmarshal([]byte(block), &m) != nil {
		return false
	}
	_, ok := m["paths"]
	return ok
}

// globToRegexp supports the subset Claude Code documents for paths: `**` (any depth), `*`
// (within one segment), `?`, and `{a,b}` alternatives. Everything else is literal.
func globToRegexp(g string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '{':
			j := strings.IndexByte(g[i:], '}')
			if j < 0 {
				return nil, fmt.Errorf("unclosed {")
			}
			alts := strings.Split(g[i+1:i+j], ",")
			for k := range alts {
				alts[k] = regexp.QuoteMeta(alts[k])
			}
			sb.WriteString("(?:" + strings.Join(alts, "|") + ")")
			i += j
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// repoFiles lists every file under root as a slash-separated path relative to root, skipping
// the same trees the link check skips.
func repoFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func TestClaudeRulesAreScopedToExistingPaths(t *testing.T) {
	problems, err := ruleFileProblems(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("%d problem(s) in .claude/rules:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// The check is only worth having if it fires: an unmatched glob, an over-long file, a
// frontmatter with no paths and one that does not start on line 1 must each be reported, and a
// correct file must not be.
func TestClaudeRulesProblemsAreCaught(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	rules := filepath.Join(root, ".claude", "rules")
	must(os.MkdirAll(filepath.Join(root, "internal", "detect"), 0o755))
	must(os.MkdirAll(rules, 0o755))
	must(os.WriteFile(filepath.Join(root, "internal", "detect", "x.go"), []byte("package detect\n"), 0o644))

	must(os.WriteFile(filepath.Join(rules, "good.md"), []byte("---\npaths:\n  - \"internal/detect/**\"\n  - \"{Makefile,go.mod}\"\n---\n# ok\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "Makefile"), []byte(""), 0o644))
	must(os.WriteFile(filepath.Join(rules, "always.md"), []byte("# no frontmatter, always loaded\n"), 0o644))
	must(os.WriteFile(filepath.Join(rules, "dead.md"), []byte("---\npaths:\n  - \"internal/nowhere/**\"\n---\n# never loads\n"), 0o644))
	must(os.WriteFile(filepath.Join(rules, "empty.md"), []byte("---\ntitle: x\n---\n# paths missing\n"), 0o644))
	long := strings.Repeat("line\n", maxRuleLines+1)
	must(os.WriteFile(filepath.Join(rules, "long.md"), []byte(long), 0o644))

	// Anything above the opening `---` turns the frontmatter into body text, and the rule then
	// loads every session. Each glob here matches, so placement is the only problem.
	const scoped = "---\npaths:\n  - \"internal/detect/**\"\n---\n"
	misplaced := map[string]string{
		"late-comment.md": "<!-- SPDX-License-Identifier: MIT -->\n" + scoped + "# x\n",
		"late-blank.md":   "\n" + scoped + "# x\n",
		"late-heading.md": "# Title\n" + scoped + "# x\n",
		"late-bom.md":     "\uFEFF" + scoped + "# x\n",
	}
	for name, body := range misplaced {
		must(os.WriteFile(filepath.Join(rules, name), []byte(body), 0o644))
	}
	// None of these may be reported: an always-loaded rule that opens with comments, a body whose
	// `---` rules enclose prose YAML would read as a mapping, a scoping example inside a code fence
	// (its glob matches nothing, and that must not be reported either), frontmatter on line 1
	// with the licence comment right after it, and CRLF frontmatter on line 1, which Claude Code
	// honours and so must not be called misplaced.
	correct := map[string]string{
		"licensed.md": "<!-- SPDX-License-Identifier: MIT -->\n<!-- always loaded: no paths -->\n# x\n",
		"ruled.md":    "# x\n\nintro\n\n---\n\nNote: prose with a colon.\n\n---\n\nend\n",
		"example.md":  "# Scoping\n\n```markdown\n---\npaths:\n  - \"src/**\"\n---\n```\n",
		"moved.md":    scoped + "<!-- SPDX-License-Identifier: MIT -->\n# x\n",
		"crlf.md":     strings.ReplaceAll(scoped, "\n", "\r\n") + "# x\r\n",
	}
	for name, body := range correct {
		must(os.WriteFile(filepath.Join(rules, name), []byte(body), 0o644))
	}

	problems, err := ruleFileProblems(root)
	must(err)
	got := strings.Join(problems, "\n")
	wants := []string{
		`dead.md: paths entry "internal/nowhere/**" matches no file`,
		`empty.md: frontmatter present but paths: is empty`,
		fmt.Sprintf("long.md: %d lines", maxRuleLines+1),
	}
	for name := range misplaced {
		wants = append(wants, name+": frontmatter must start on line 1")
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}
	clean := []string{"good.md", "always.md"}
	for name := range correct {
		clean = append(clean, name)
	}
	for _, name := range clean {
		if strings.Contains(got, name) {
			t.Errorf("a correct file (%s) was reported:\n%s", name, got)
		}
	}
	if len(problems) != len(wants) {
		t.Errorf("want exactly %d problems, got %d:\n%s", len(wants), len(problems), got)
	}

	// Reading a line-oriented file the same way the checker does keeps the count definition honest.
	f, err := os.Open(filepath.Join(rules, "long.md"))
	must(err)
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		n++
	}
	if n != maxRuleLines+1 {
		t.Fatalf("fixture has %d lines, want %d", n, maxRuleLines+1)
	}
}

// TestEpochRuleLoadsWhereCoveredCodeIsEdited: detect.rulesEpoch is the only thing that moves the
// rules version when deterministic detection outside builtinRules() changes, and nothing enforces
// the bump. Its doc comment sits in internal/detect/rules_version.go, which a change to permcheck,
// collect or cmd/aguard never opens — so the rule has to be in a rules file that loads when that
// code is edited, on one line that names the constant, the table it does not cover and the
// regeneration step.
func TestEpochRuleLoadsWhereCoveredCodeIsEdited(t *testing.T) {
	const rulesFile = ".claude/rules/pipeline.md"
	b, err := os.ReadFile(filepath.Join(repoRoot(), rulesFile))
	if err != nil {
		t.Fatal(err)
	}
	globs, err := frontmatterPaths(string(b))
	if err != nil {
		t.Fatalf("%s: %v", rulesFile, err)
	}
	// One file per place deterministic findings are decided outside the rule table.
	for _, covered := range []string{
		"internal/detect/shape.go",        // shape checks
		"internal/detect/hooks.go",        // hook checks
		"internal/detect/logical.go",      // lexical layer
		"internal/collect/imports.go",     // EXFIL-005
		"internal/permcheck/permcheck.go", // PERM-*
		"internal/gate/status.go",         // GATE-001, raised by scan
		"cmd/aguard/main.go",              // analyze(): which stages run
	} {
		if _, err := os.Stat(filepath.Join(repoRoot(), covered)); err != nil {
			t.Fatalf("%s is gone; point this test at the file that now holds that check: %v", covered, err)
		}
		// No leading frontmatter means an always-loaded rule (frontmatterPaths), which loads
		// wherever anything is edited; a paths list has to reach this file.
		loaded := globs == nil
		for _, g := range globs {
			re, err := globToRegexp(g)
			if err != nil {
				t.Fatalf("%s: paths entry %q: %v", rulesFile, g, err)
			}
			if re.MatchString(covered) {
				loaded = true
				break
			}
		}
		if !loaded {
			t.Errorf("%s does not load when %s is edited (paths: %v)", rulesFile, covered, globs)
		}
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "`detect.rulesEpoch`") && strings.Contains(line, "`builtinRules()`") &&
			strings.Contains(line, "`make docs`") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s has no line naming `detect.rulesEpoch`, `builtinRules()` and `make docs` — "+
			"whoever changes deterministic detection outside the table is never told to bump the epoch", rulesFile)
	}
}
