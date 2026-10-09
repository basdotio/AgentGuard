// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/report"
)

// renderToken is an obviously fake GitHub token, the shape the redactor's known-prefix table removes.
const renderToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

// TestScan_PathSecretsNeverReachARendering: a token sits in two places a finding quotes twice —
// a directory on the path of a hook's out-of-home script (HOOK-002's `ref → resolved`), and the
// credentials of a registry URL whose host cannot be read (SUP-006's explanation). Each finding
// redacted one copy and printed the other. Whatever the redactor removes must be absent from
// every rendering a user can paste or upload, not just from the half that went through it.
func TestScan_PathSecretsNeverReachARendering(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")

	script := filepath.Join(t.TempDir(), "opt", renderToken, "hook.sh")
	mustWriteFile(t, script, "#!/bin/sh\nexit 0\n")
	cmd, err := json.Marshal("sh " + script)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":`+string(cmd)+`}]}]}}`)

	skill := filepath.Join(root, "skills", "demo")
	mustWriteFile(t, filepath.Join(skill, "SKILL.md"),
		"---\nname: demo\ndescription: Sets up the project's toolchain.\n---\nRun scripts/setup.sh once.\n")
	mustWriteFile(t, filepath.Join(skill, "scripts", "setup.sh"),
		"#!/bin/sh\nnpm config set registry https://ci:"+renderToken+"@npm.corp:${PORT}/\n")

	out, err := scanEnv(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"HOOK-002", "SUP-006"} {
		if _, _, ok := findRule(out, id); !ok {
			t.Fatalf("%s must still be reported; artifacts=%+v", id, out.Artifacts)
		}
	}

	renderings := map[string]func(*bytes.Buffer) error{
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
	for name, render := range renderings {
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
