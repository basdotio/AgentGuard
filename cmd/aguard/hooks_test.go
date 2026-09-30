// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// process.md §3 and §6 name two things a hook must refuse: a commit on a p/ branch whose
// message carries no proposal id, and a proposal or issue whose state is set to a terminal
// value with no evidence line. Both are shell scripts under hack/, installed by `make hooks`.
// These tests run them in a throwaway git repository — the same way the npm launcher is
// tested — so the scripts are exercised as git would call them, not re-implemented in Go.

// hookRepo makes a git repository with an unborn branch of the given name and returns its path.
func hookRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	gitInDir(t, dir, "init", "-q")
	gitInDir(t, dir, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return dir
}

func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// runHackScript runs hack/<name> from inside dir and returns exit code and combined output.
func runHackScript(t *testing.T, dir, name string, args ...string) (int, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join(repoRoot(), "hack", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("hook script missing: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatal(err)
	}
	return 0, string(out)
}

func writeHookFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitMsgHookRequiresProposalID(t *testing.T) {
	cases := []struct {
		branch, msg string
		wantExit    int
	}{
		{"p/999-example", "detect: do the thing (P-999)", 0},
		{"p/999-example", "detect: do the thing", 1},
		{"p/999-example", "detect: P-999 mentioned but not in the (P-NNN) form", 1},
		{"p/release-0.10.0", "release: 0.10.0 (P-1000)", 0},
		{"dev", "docs: a chore with no id, allowed off p/ branches", 0},
	}
	for _, c := range cases {
		dir := hookRepo(t, c.branch)
		msgFile := filepath.Join(dir, "MSG")
		writeHookFixture(t, msgFile, c.msg+"\n")
		exit, out := runHackScript(t, dir, "commit-msg", msgFile)
		if exit != c.wantExit {
			t.Errorf("branch %s, message %q: exit %d, want %d\n%s", c.branch, c.msg, exit, c.wantExit, out)
		}
		if c.wantExit != 0 && !strings.Contains(out, "(P-NNN)") {
			t.Errorf("refusal must show the expected form; got:\n%s", out)
		}
	}
}

const proposalBody = `# 010 — example

## 问题
x

## 完成
%s
`

// TestCheckProposalDemandsEvidence: the directory is the state (process.md §5). A proposal
// moved into complete/ must carry an evidence line; one outside the four state directories,
// or one still carrying a 状态: line, is refused — a second copy of the state is what drifted.
// Issues keep the older rule: a staged line flipping the state to 已修复 needs evidence.
func TestCheckProposalDemandsEvidence(t *testing.T) {
	dir := hookRepo(t, "p/010-example")
	design := filepath.Join(dir, "docs", "proposals", "design", "010-example.md")
	complete := filepath.Join(dir, "docs", "proposals", "complete", "010-example.md")
	loose := filepath.Join(dir, "docs", "proposals", "010-example.md")
	issue := filepath.Join(dir, "issues", "021-example.md")
	writeHookFixture(t, design, fmt.Sprintf(proposalBody, ""))
	writeHookFixture(t, issue, "# 021 — example\n\n- **状态**：未修复\n")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "seed (P-010)")

	stageAndRun := func(name, content string) (int, string) {
		t.Helper()
		writeHookFixture(t, name, content)
		gitInDir(t, dir, "add", "-A")
		return runHackScript(t, dir, "check-proposal")
	}
	move := func(from, to string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}

	// 1. moved into complete/ with no evidence line → refused
	move(design, complete)
	if exit, out := stageAndRun(complete, fmt.Sprintf(proposalBody, "合入:abc\n")); exit == 0 {
		t.Errorf("complete/ without 证据 must be refused\n%s", out)
	} else if !strings.Contains(out, "证据") {
		t.Errorf("refusal must name the missing evidence line; got:\n%s", out)
	}
	// 2. same, with an evidence line → allowed (the reverse assertion)
	if exit, out := stageAndRun(complete, fmt.Sprintf(proposalBody, "合入:abc\n证据:TestX(x_test.go);3 → 0\n")); exit != 0 {
		t.Errorf("complete/ with 证据 must pass\n%s", out)
	}
	// 3. a 状态: line inside the file → refused even in the right directory
	if exit, out := stageAndRun(complete, "- **状态**:已完成\n"+fmt.Sprintf(proposalBody, "证据:x\n")); exit == 0 {
		t.Errorf("a 状态 line must be refused — the directory is the state\n%s", out)
	} else if !strings.Contains(out, "状态") {
		t.Errorf("refusal must name the 状态 line; got:\n%s", out)
	}
	// 4. a proposal outside the four directories → refused
	move(complete, loose)
	if exit, out := stageAndRun(loose, fmt.Sprintf(proposalBody, "证据:x\n")); exit == 0 {
		t.Errorf("a proposal outside draft/design/complete/rejected must be refused\n%s", out)
	}
	move(loose, design)
	writeHookFixture(t, design, fmt.Sprintf(proposalBody, ""))
	// 5. an issue flipped to 已修复 with no evidence → refused; with evidence → allowed
	if exit, out := stageAndRun(issue, "# 021 — example\n\n- **状态**：已修复\n"); exit == 0 {
		t.Errorf("issue 已修复 without 证据 must be refused\n%s", out)
	}
	if exit, out := stageAndRun(issue, "# 021 — example\n\n- **状态**：已修复\n\n证据:TestY(y_test.go)\n"); exit != 0 {
		t.Errorf("issue 已修复 with 证据 must pass\n%s", out)
	}
	// 6. nothing relevant staged → allowed, silently
	gitInDir(t, dir, "commit", "-q", "-m", "state changes (P-010)")
	writeHookFixture(t, filepath.Join(dir, "README.md"), "unrelated\n")
	gitInDir(t, dir, "add", "-A")
	if exit, out := runHackScript(t, dir, "check-proposal"); exit != 0 || strings.TrimSpace(out) != "" {
		t.Errorf("unrelated staging must pass quietly; exit %d, out %q", exit, out)
	}
}
