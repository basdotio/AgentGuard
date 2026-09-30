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
// one 800-line file under a different name.

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
// has no frontmatter (an always-loaded rule).
func frontmatterPaths(s string) ([]string, error) {
	if !strings.HasPrefix(s, "---\n") {
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

// The check is only worth having if it fires: an unmatched glob, an over-long file and a
// frontmatter with no paths must each be reported, and a correct file must not be.
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

	problems, err := ruleFileProblems(root)
	must(err)
	got := strings.Join(problems, "\n")
	for _, want := range []string{
		`dead.md: paths entry "internal/nowhere/**" matches no file`,
		`empty.md: frontmatter present but paths: is empty`,
		fmt.Sprintf("long.md: %d lines", maxRuleLines+1),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "good.md") || strings.Contains(got, "always.md") {
		t.Errorf("a correct file was reported:\n%s", got)
	}
	if len(problems) != 3 {
		t.Errorf("want exactly 3 problems, got %d:\n%s", len(problems), got)
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
