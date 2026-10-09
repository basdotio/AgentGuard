// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// noteToken is an obviously fake GitHub token, the shape the redactor's known-prefix table removes.
const noteToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

// importFixture lays out base/home/.claude with a CLAUDE.md importing four targets, one per note
// the import graph can produce: a credential FILE (.env), a file in a credential DIRECTORY (.ssh),
// a file outside HOME, and a fifth hop past the documented depth. seg is spliced into each path,
// so the same layout serves the secret case and the ordinary one. Returns the root.
func importFixture(t *testing.T, seg string) string {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	root := filepath.Join(home, ".claude")
	join := func(parts ...string) string { return filepath.Join(parts...) }
	write(t, home, join("vault", seg, ".env"), "X=1\n")
	write(t, home, join(".ssh", seg, "config"), "Host x\n")
	write(t, base, join("outside", seg, "notes.md"), "# notes\n")
	write(t, root, "d/d1.md", "@d2.md\n")
	write(t, root, "d/d2.md", "@d3.md\n")
	write(t, root, "d/d3.md", "@d4.md\n")
	write(t, root, "d/d4.md", "@"+filepath.ToSlash(join(seg, "d5.md"))+"\n")
	write(t, root, join("d", seg, "d5.md"), "# five hops down\n")
	write(t, root, "CLAUDE.md", "# Project\n"+
		"@~/"+filepath.ToSlash(join("vault", seg, ".env"))+"\n"+
		"@~/"+filepath.ToSlash(join(".ssh", seg, "config"))+"\n"+
		"@../../"+filepath.ToSlash(join("outside", seg, "notes.md"))+"\n"+
		"@d/d1.md\n")
	return root
}

// importSnippets returns, for each import-graph finding, its rule and title joined to the snippet,
// so one table can say which note carried what.
func importSnippets(t *testing.T, res Result) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	add := func(f model.Finding) {
		for _, e := range f.Evidence {
			got[f.RuleID+" "+f.Title] = append(got[f.RuleID+" "+f.Title], e.Snippet)
		}
	}
	for _, n := range res.Notes {
		if strings.Contains(n.Title, "import") {
			add(n)
		}
	}
	for _, a := range res.Artifacts {
		for _, f := range a.Findings {
			if f.RuleID == "EXFIL-005" {
				add(f)
			}
		}
	}
	return got
}

const (
	importCredTitle   = "EXFIL-005 Instruction file imports a credential into the agent's context"
	importRefuseTitle = "COV-000 Instruction file imports a credential path, refused"
	importEscapeTitle = "COV-000 Instruction import points outside the scanned tree, not read"
	importDepthTitle  = "COV-000 Instruction import chain hit the depth limit (partial)"
)

// TestImportNotes_SecretInReferenceIsRedacted: each of the four notes the import graph emits used
// to quote the `@path` exactly as the instruction file wrote it, so a token sitting in a directory
// name reached every rendering — while the same line, matched by any engine rule, would have been
// <REDACTED>. The reference is text copied out of a file body; it goes through the redactor like
// every other snippet, and the note keeps its shape so the reader still sees which import it was.
func TestImportNotes_SecretInReferenceIsRedacted(t *testing.T) {
	res := CollectAll(importFixture(t, noteToken))
	want := map[string][]string{
		importCredTitle:   {"@~/vault/<REDACTED>/.env", "@~/.ssh/<REDACTED>/config"},
		importRefuseTitle: {"@~/vault/<REDACTED>/.env is a credential path", "@~/.ssh/<REDACTED>/config is a credential path"},
		importEscapeTitle: {"@../../outside/<REDACTED>/notes.md escapes the scan boundary"},
		importDepthTitle:  {"@<REDACTED>/d5.md beyond depth 4"},
	}
	assertSnippets(t, importSnippets(t, res), want)
	assertTokenAbsent(t, res)
}

// TestImportNotes_OrdinaryReferenceUnchanged is the reverse assertion: a reference the redactor has
// nothing to say about renders byte for byte as it did — "@" + ref + the fixed tail.
func TestImportNotes_OrdinaryReferenceUnchanged(t *testing.T) {
	res := CollectAll(importFixture(t, "plain"))
	want := map[string][]string{
		importCredTitle:   {"@~/vault/plain/.env", "@~/.ssh/plain/config"},
		importRefuseTitle: {"@~/vault/plain/.env is a credential path", "@~/.ssh/plain/config is a credential path"},
		importEscapeTitle: {"@../../outside/plain/notes.md escapes the scan boundary"},
		importDepthTitle:  {"@plain/d5.md beyond depth 4"},
	}
	assertSnippets(t, importSnippets(t, res), want)
}

// configNoteFixture writes a root whose installed_plugins.json names a plugin installed outside HOME
// under key pluginKey, and whose settings.json has a hooks entry under hookKey that is not a list.
func configNoteFixture(t *testing.T, pluginKey, hookKey string) Result {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	root := filepath.Join(home, ".claude")
	outside := filepath.Join(base, "outside", "plug")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "plugins/installed_plugins.json",
		`{"version":2,"plugins":{"`+pluginKey+`":[{"installPath":"`+filepath.ToSlash(outside)+`","version":"1.0.0"}]}}`)
	write(t, root, "settings.json", `{"hooks":{"`+hookKey+`":"not-a-list"}}`)
	return CollectAll(root)
}

func configSnippets(res Result) map[string][]string {
	got := map[string][]string{}
	for _, n := range res.Notes {
		if n.RuleID == "SCOPE-001" || n.RuleID == "PARSE-000" {
			for _, e := range n.Evidence {
				got[n.RuleID] = append(got[n.RuleID], e.Snippet)
			}
		}
	}
	return got
}

// TestConfigNamesInNotesAreRedacted: two notes quote a KEY out of a config file — the plugin key from
// installed_plugins.json (SCOPE-001, install path outside HOME) and the event key from a hooks entry
// that could not be understood (PARSE-000). Config values are what the redactor exists for; these two
// were the only places in the collector where one reached a snippet unredacted.
func TestConfigNamesInNotesAreRedacted(t *testing.T) {
	res := configNoteFixture(t, noteToken+"@market", noteToken)
	assertSnippets(t, configSnippets(res), map[string][]string{
		"SCOPE-001": {"install path escapes HOME: <REDACTED>@market"},
		"PARSE-000": {"hooks.<REDACTED>"},
	})
	assertTokenAbsent(t, res)
}

// TestConfigNamesInNotes_OrdinaryUnchanged is the reverse assertion, with the shapes a real machine
// has: a marketplace plugin key and a standard event name.
func TestConfigNamesInNotes_OrdinaryUnchanged(t *testing.T) {
	res := configNoteFixture(t, "figma@claude-plugins-official", "PreToolUse")
	assertSnippets(t, configSnippets(res), map[string][]string{
		"SCOPE-001": {"install path escapes HOME: figma@claude-plugins-official"},
		"PARSE-000": {"hooks.PreToolUse"},
	})
}

func assertSnippets(t *testing.T, got, want map[string][]string) {
	t.Helper()
	for k, w := range want {
		g := got[k]
		if len(g) != len(w) {
			t.Errorf("%s: got %d snippet(s) %q, want %q", k, len(g), g, w)
			continue
		}
		for i := range w {
			if g[i] != w[i] {
				t.Errorf("%s:\n  got  %q\n  want %q", k, g[i], w[i])
			}
		}
	}
}

// assertTokenAbsent checks every human-facing field of every note and finding the collector produced.
func assertTokenAbsent(t *testing.T, res Result) {
	t.Helper()
	check := func(where string, f model.Finding) {
		fields := []string{f.Title, f.Why}
		for _, e := range f.Evidence {
			fields = append(fields, e.File, e.Snippet)
		}
		for _, s := range fields {
			if strings.Contains(s, noteToken) {
				t.Errorf("%s %s %q carries the token in clear: %q", where, f.RuleID, f.Title, s)
			}
		}
	}
	for _, n := range res.Notes {
		check("note", n)
	}
	for _, a := range res.Artifacts {
		for _, f := range a.Findings {
			check("finding on "+a.Name, f)
		}
	}
}
