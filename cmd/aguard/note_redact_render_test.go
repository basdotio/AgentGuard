// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/report"
)

// noteFixture lays out one environment in which every note this change touches fires, each carrying
// renderToken in the part it copies from a file or a config value:
//   - CLAUDE.md imports a .env and a file in .ssh (EXFIL-005 + COV-000 each), a file outside HOME
//     (COV-000) and, five hops down, a sixth file (COV-000) — all through directories named by the token;
//   - a skill holds a 0111 directory named by the token (the unreadable-entries COV-000);
//   - installed_plugins.json installs a plugin keyed by the token outside HOME (SCOPE-001);
//   - settings.json has a hooks entry keyed by the token that is not a list (PARSE-000) and registers
//     the gate at a binary under a token directory that does not exist (GATE-001).
//
// Returns the root and the skill directory.
func noteFixture(t *testing.T) (root, skill string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	root = filepath.Join(home, ".claude")
	outside := filepath.Join(base, "outside")
	tok := renderToken

	mustWriteFile(t, filepath.Join(home, "vault", tok, ".env"), "X=1\n")
	mustWriteFile(t, filepath.Join(home, ".ssh", tok, "config"), "Host x\n")
	mustWriteFile(t, filepath.Join(outside, tok, "notes.md"), "# notes\n")
	mustWriteFile(t, filepath.Join(root, "d", "d1.md"), "@d2.md\n")
	mustWriteFile(t, filepath.Join(root, "d", "d2.md"), "@d3.md\n")
	mustWriteFile(t, filepath.Join(root, "d", "d3.md"), "@d4.md\n")
	mustWriteFile(t, filepath.Join(root, "d", "d4.md"), "@"+tok+"/d5.md\n")
	mustWriteFile(t, filepath.Join(root, "d", tok, "d5.md"), "# five hops down\n")
	mustWriteFile(t, filepath.Join(root, "CLAUDE.md"), "# Project\n@~/vault/"+tok+"/.env\n@~/.ssh/"+tok+
		"/config\n@../../outside/"+tok+"/notes.md\n@d/d1.md\n")

	skill = filepath.Join(root, "skills", "demo")
	mustWriteFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: demo\ndescription: Demo skill.\n---\nRun the helper.\n")
	mustWriteFile(t, filepath.Join(skill, tok, "inner.sh"), "#!/bin/sh\nexit 0\n")
	locked := filepath.Join(skill, tok)
	if err := os.Chmod(locked, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	plug := filepath.Join(outside, "plug")
	if err := os.MkdirAll(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "plugins", "installed_plugins.json"),
		`{"version":2,"plugins":{"`+tok+`@market":[{"installPath":"`+filepath.ToSlash(plug)+`","version":"1.0.0"}]}}`)
	gateCmd, err := json.Marshal(filepath.Join(outside, tok, "aguard") + " hook")
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "settings.json"), `{"hooks":{"`+tok+`":"not-a-list",`+
		`"PreToolUse":[{"matcher":"Skill","hooks":[{"type":"command","command":`+string(gateCmd)+`}]}]}}`)
	return root, skill
}

// allFindings is every scored finding and every note in a result, scan-level and per-artifact.
func allFindings(out model.ScanResult) []model.Finding {
	fs := append([]model.Finding(nil), out.Notes...)
	for _, a := range out.Artifacts {
		fs = append(fs, a.Findings...)
	}
	return fs
}

// renderings are the six ways a result leaves this tool for a person or an upload, encoded the way
// the CLI encodes them.
func renderings(out model.ScanResult) map[string]func(*bytes.Buffer) error {
	return map[string]func(*bytes.Buffer) error{
		"json": func(b *bytes.Buffer) error {
			enc := json.NewEncoder(b) // the same encoding `scan --json` uses
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
		"text":     func(b *bytes.Buffer) error { report.Text(b, out); return nil },
		"verbose":  func(b *bytes.Buffer) error { report.TextVerbose(b, out); return nil },
		"markdown": func(b *bytes.Buffer) error { return report.Markdown(b, out) },
		"sarif":    func(b *bytes.Buffer) error { return report.SARIF(b, out, "test", "https://example.invalid") },
		"html":     func(b *bytes.Buffer) error { return report.HTML(b, out) },
	}
}

func assertNoToken(t *testing.T, out model.ScanResult) {
	t.Helper()
	for name, render := range renderings(out) {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := render(&b); err != nil {
				t.Fatal(err)
			}
			if b.Len() == 0 {
				t.Fatal("empty rendering proves nothing")
			}
			if i := strings.Index(b.String(), renderToken); i >= 0 {
				lo, hi := max(0, i-120), min(b.Len(), i+len(renderToken)+20)
				t.Errorf("the token reached the %s rendering %d time(s); first:\n…%s…",
					name, strings.Count(b.String(), renderToken), b.String()[lo:hi])
			}
		})
	}
}

// TestScan_NoteSecretsNeverReachARendering: the notes collect, the walk and the gate write quoted
// what they copied from a file or a config value without the redactor — an @import line, a plugin
// key, a hook event key, an unreadable entry's name, the gate's registered command. Each one put a
// token sitting in a path into every rendering a user pastes or uploads, while the engine's own
// snippets of the same bytes were <REDACTED>. Every note must still be there; only the token goes.
func TestScan_NoteSecretsNeverReachARendering(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads 0111 directories; the unreadable-entries note never fires for it")
	}
	root, skill := noteFixture(t)
	out, err := scanEnv(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	out.Notes = append(out.Notes, gateLivenessNote(root)...) // as the scan command attaches it

	want := map[string]int{
		"EXFIL-005 Instruction file imports a credential into the agent's context": 2,
		"COV-000 Instruction file imports a credential path, refused":              2,
		"COV-000 Instruction import points outside the scanned tree, not read":     1,
		"COV-000 Instruction import chain hit the depth limit (partial)":           1,
		"COV-000 Entries in this artifact could not be read (incomplete coverage)": 1,
		"SCOPE-001 Plugin install path points outside HOME, skipped":               1,
		"PARSE-000 Hook entry not understood, command not scanned (partial)":       1,
		"GATE-001 Load-time gate is registered but cannot run":                     1,
	}
	got := map[string]int{}
	for _, f := range allFindings(out) {
		got[f.RuleID+" "+f.Title]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %d in the result, want %d — the fix must keep every note", k, got[k], n)
		}
	}
	assertNoToken(t, out)

	// `check <skill> --md` is the rendering written to be pasted into a pull request comment.
	t.Run("check", func(t *testing.T) {
		co, err := checkTarget(skill, scanOpts{})
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := report.Markdown(&b, co); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "COV-000") {
			t.Fatalf("the unreadable entry must still be disclosed by check:\n%s", b.String())
		}
		if strings.Contains(b.String(), renderToken) {
			t.Errorf("the token reached `check --md` %d time(s)", strings.Count(b.String(), renderToken))
		}
	})
}
