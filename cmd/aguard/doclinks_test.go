// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The documentation tree is one set of files that link to each other by relative path, and a
// move (doc/ was once folded into docs/) or a rename breaks those links without anything else
// noticing: no build step reads them, and a stale link fails only when a reader follows it.
// This test walks every Markdown file in the repository and resolves every relative link,
// so a broken one fails `make test` instead of a reader.

// skippedDirs are trees that are not ours to check: build output, dependencies, git. Used only
// by the fallback walk — see markdownFiles, which asks git first.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "bin": true, "_audit": true, "vendor": true,
}

// markdownFiles lists the .md files this repository is responsible for.
//
// It asks GIT rather than walking the filesystem, because "ours" is a question git already
// answers and a hardcoded directory list keeps getting it wrong. `--cached --others
// --exclude-standard` is exactly "tracked, plus untracked but not ignored" — a doc you have
// written and not yet staged is still checked, while anything .gitignore excludes is not.
//
// This was not a tidy-up. A scratch directory (`/_gitlocks/`, ignored, zero files tracked) held
// 27 stale links, so `make test` failed for everyone on every branch — and a check that is
// always red is a check nobody reads. On 2026-09-24 that noise hid a REAL broken link a PR had
// just introduced: locally it was 28 failures where 27 were expected, and the one that mattered
// was found by CI instead, where the scratch directory does not exist.
//
// Falls back to walking when git is unavailable (an exported tarball, a vendored copy); the
// fallback is the old behaviour, so such a tree is checked no worse than before.
func markdownFiles(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard", "--", "*.md").Output()
	if err != nil {
		return walkMarkdownFiles(root)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" {
			files = append(files, filepath.Join(root, rel))
		}
	}
	if len(files) == 0 {
		return walkMarkdownFiles(root)
	}
	return files, nil
}

// walkMarkdownFiles is the fallback for a tree git does not know about.
func walkMarkdownFiles(root string) ([]string, error) {
	var files []string
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
		if strings.HasSuffix(d.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// markdownLink matches the target of an inline link or image: `](target)`. Reference-style
// links and autolinks are not used in this repository's docs.
var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// inlineCode matches a backtick span. Prose about links (this repository documents its own
// link conventions) writes `](path)` inside backticks; those are examples, not links.
var inlineCode = regexp.MustCompile("`[^`]*`")

// brokenMarkdownLinks returns one "file:line: target" per relative link under root whose
// target does not exist. Links inside fenced code blocks and backtick spans are skipped: those
// are examples, not navigation. Anchors are stripped; only the file part is checked.
func brokenMarkdownLinks(root string) ([]string, error) {
	files, err := markdownFiles(root)
	if err != nil {
		return nil, err
	}
	var broken []string
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		inFence := false
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, m := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				target := m[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "#") ||
					strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "/") {
					continue
				}
				if i := strings.IndexByte(target, '#'); i >= 0 {
					target = target[:i]
				}
				if target == "" {
					continue
				}
				if u, err := url.PathUnescape(target); err == nil {
					target = u
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), target)); err != nil {
					rel, _ := filepath.Rel(root, path)
					broken = append(broken, fmt.Sprintf("%s:%d: %s", rel, n, m[1]))
				}
			}
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return broken, nil
}

func TestDocsRelativeLinksResolve(t *testing.T) {
	broken, err := brokenMarkdownLinks(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) > 0 {
		t.Errorf("%d relative markdown link(s) point at files that do not exist:\n  %s",
			len(broken), strings.Join(broken, "\n  "))
	}
}

// The check above is only worth having if it actually fires. This pins that a missing target
// is reported, that an existing one is not, and that a link inside a code fence is ignored —
// so a future "simplification" cannot turn the test into one that always passes.
func TestBrokenMarkdownLinksAreCaught(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(root, "sub", "exists.md"), []byte("# here\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "a.md"), []byte(strings.Join([]string{
		"[ok](sub/exists.md) [anchor](sub/exists.md#here) [web](https://example.com/x.md)",
		"[gone](sub/missing.md)",
		"```",
		"[example only](not/checked.md)",
		"```",
		"![img](sub/absent.png)",
		"prose about the syntax `[x](not/checked/either.md)` stays prose",
	}, "\n")), 0o644))

	broken, err := brokenMarkdownLinks(root)
	must(err)
	want := []string{"a.md:2: sub/missing.md", "a.md:6: sub/absent.png"}
	if strings.Join(broken, "|") != strings.Join(want, "|") {
		t.Fatalf("broken links = %q, want %q", broken, want)
	}
}

// TestIgnoredFilesAreNotOurDocs is the point of asking git rather than walking: a scratch
// directory the repository ignores is not this repository's documentation, and links inside it
// are not this repository's broken links.
//
// Without this, the check reverts to "always red" the next time anybody leaves an ignored tree
// lying around — and an always-red check is one nobody reads. That is not hypothetical: it
// happened, it stayed broken for everyone on every branch, and on 2026-09-24 it hid a real
// broken link long enough for CI to be the one that caught it.
func TestIgnoredFilesAreNotOurDocs(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable here (%v): %s", err, out)
		}
	}
	run("init", "-q")

	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "/scratch/\n")
	write("scratch/junk.md", "[gone](nowhere-at-all.md)\n")
	write("real.md", "[gone](also-nowhere.md)\n")
	run("add", "-A")

	broken, err := brokenMarkdownLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Fatalf("got %d broken link(s), want only the tracked one:\n  %s",
			len(broken), strings.Join(broken, "\n  "))
	}
	if !strings.HasPrefix(broken[0], "real.md:") {
		t.Errorf("reported %q; the ignored tree is not ours to check", broken[0])
	}

	// The reverse assertion: an UNTRACKED file that is not ignored is still ours. A check that
	// only read `git ls-files --cached` would miss a doc written and not yet staged, which is
	// exactly when a broken link is easiest to fix.
	write("draft.md", "[gone](missing-too.md)\n")
	broken, err = brokenMarkdownLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 2 {
		t.Errorf("got %d, want 2: an unstaged doc is still this repository's:\n  %s",
			len(broken), strings.Join(broken, "\n  "))
	}
}
