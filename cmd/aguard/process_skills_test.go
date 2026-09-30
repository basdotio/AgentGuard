// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The four process skills under .claude/skills/ (P-006) are entry points into docs/process.md,
// not a second copy of it. Three properties keep them that way, and each has a failure mode
// that nothing else would catch: a skill that grows past a screen has started restating the
// process (and will drift from it); one without `disable-model-invocation: true` can be started
// by the model on a stray word — "/release" on the word 发版 in a chat; one whose body never
// cites a process.md section has cut the cord to the document it is supposed to point at.

const (
	maxSkillBodyLines    = 60
	maxSkillDescription  = 1024 // the same limit plugin_manifest_test.go pins for Claude Desktop
	processDocReference  = "docs/process.md"
	processSectionMarker = "§"
)

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation *bool  `yaml:"disable-model-invocation"`
}

// processSkillProblems returns one message per defect across .claude/skills/*/SKILL.md under
// root. A missing directory is not a defect.
func processSkillProblems(root string) ([]string, error) {
	dir := filepath.Join(root, ".claude", "skills")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".claude", "skills", e.Name(), "SKILL.md"))
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		s := string(b)
		if !strings.HasPrefix(s, "---\n") {
			problems = append(problems, rel+": no frontmatter")
			continue
		}
		end := strings.Index(s[4:], "\n---\n")
		if end < 0 {
			problems = append(problems, rel+": frontmatter never closed")
			continue
		}
		var fm skillFrontmatter
		if err := yaml.Unmarshal([]byte(s[4:4+end]), &fm); err != nil {
			problems = append(problems, fmt.Sprintf("%s: frontmatter is not YAML: %v", rel, err))
			continue
		}
		body := s[4+end+5:]
		if fm.Name != e.Name() {
			problems = append(problems, fmt.Sprintf("%s: name %q must equal the directory name %q", rel, fm.Name, e.Name()))
		}
		if fm.Description == "" {
			problems = append(problems, rel+": description is empty")
		} else if n := len([]rune(fm.Description)); n > maxSkillDescription {
			problems = append(problems, fmt.Sprintf("%s: description is %d characters, limit %d", rel, n, maxSkillDescription))
		}
		if fm.DisableModelInvocation == nil || !*fm.DisableModelInvocation {
			problems = append(problems, rel+": disable-model-invocation must be true — these are human-triggered entry points")
		}
		if n := strings.Count(strings.TrimRight(body, "\n"), "\n") + 1; n > maxSkillBodyLines {
			problems = append(problems, fmt.Sprintf("%s: body is %d lines, limit %d — it is restating process.md instead of pointing at it", rel, n, maxSkillBodyLines))
		}
		if !strings.Contains(body, processDocReference) || !strings.Contains(body, processSectionMarker) {
			problems = append(problems, fmt.Sprintf("%s: body must cite %s with a %s section", rel, processDocReference, processSectionMarker))
		}
	}
	return problems, nil
}

func TestProcessSkillsStayThin(t *testing.T) {
	problems, err := processSkillProblems(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("%d problem(s) in .claude/skills:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// Each rule must fire on its own fixture, and a correct skill must pass — otherwise the test
// above could be satisfied by a checker that never reports anything.
func TestProcessSkillProblemsAreCaught(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	good := "---\nname: %s\ndescription: entry point\ndisable-model-invocation: true\n---\n# x\n\n依据 docs/process.md §1。\n"
	write("good", fmt.Sprintf(good, "good"))
	write("renamed", fmt.Sprintf(good, "other"))
	write("chatty", strings.Replace(fmt.Sprintf(good, "chatty"), "# x\n", strings.Repeat("line\n", maxSkillBodyLines+1), 1))
	write("auto", strings.Replace(fmt.Sprintf(good, "auto"), "disable-model-invocation: true\n", "", 1))
	write("adrift", strings.Replace(fmt.Sprintf(good, "adrift"), "依据 docs/process.md §1。", "does its own thing", 1))

	problems, err := processSkillProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(problems, "\n")
	for _, want := range []string{
		`renamed/SKILL.md: name "other" must equal the directory name "renamed"`,
		"chatty/SKILL.md: body is 63 lines, limit " + fmt.Sprint(maxSkillBodyLines), // 61 filler lines + the two citation lines
		"auto/SKILL.md: disable-model-invocation must be true",
		"adrift/SKILL.md: body must cite docs/process.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "good/SKILL.md") {
		t.Errorf("a correct skill was reported:\n%s", got)
	}
	if len(problems) != 4 {
		t.Errorf("want exactly 4 problems, got %d:\n%s", len(problems), got)
	}
}
