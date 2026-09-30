// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestRegression_MarketplacePluginsStayLowRisk is the false-positive guardrail.
//
// Official marketplace plugins (superpowers, figma) talk to a local sidecar and read a
// token to do it. Before the loopback screen that shape was EXFIL-001 high, which caps
// the whole environment at 69 — so every real install that had them looked Elevated
// forever. These fixtures freeze that shape: score stays ≥85 and no finding is high.
// A later rule change that re-breaks official plugins fails this test, not a colleague's
// account.
//
// The superpowers lines are VERBATIM from obra/superpowers tests/brainstorm-server
// (branding.test.js:62-63, lifecycle.test.js:304-305), not an idealised sidecar call: the
// real shape carries a template-literal port, which url.Parse rejects, and the first version
// of the loopback screen therefore left it high. Two other shapes in that plugin are still
// high by design and deliberately NOT in this fixture: skills/brainstorming/scripts/server.cjs
// holds a real external URL constant (a brand image), and tests/brainstorm-server/server.test.js
// passes the URL through a variable — both are "destination not readable here", which the
// screen must keep failing closed. Those are the reputation allowlist's job.
func TestRegression_MarketplacePluginsStayLowRisk(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	super := filepath.Join(root, "plugins", "cache", "superpowers", "4.0.3")
	mustWriteFile(t, filepath.Join(super, "SKILL.md"),
		"---\nname: superpowers\ndescription: Use a local brainstorm server.\n---\n"+
			"Talk to the local brainstorm server for design review.\n")
	mustWriteFile(t, filepath.Join(super, "tests", "brainstorm-server", "branding.test.js"),
		"    const headers = { Cookie: `brainstorm-key-${port}=${TOKEN}` };\n"+
			"    http.get(`http://localhost:${port}/`, { headers }, (res) => {\n")
	mustWriteFile(t, filepath.Join(super, "tests", "brainstorm-server", "lifecycle.test.js"),
		"      ws = new WebSocket(`ws://localhost:${infoB.port}/?key=${keyA}`, {\n"+
			"        headers: { Origin: `http://localhost:${infoB.port}` }\n")

	figma := filepath.Join(root, "plugins", "cache", "figma", "1.0.0")
	mustWriteFile(t, filepath.Join(figma, "SKILL.md"),
		"---\nname: figma-use\ndescription: Read designs from the local Figma desktop bridge.\n---\n"+
			"Use the local desktop bridge, not a cloud endpoint.\n")
	mustWriteFile(t, filepath.Join(figma, "src", "bridge.js"),
		"const token = process.env.FIGMA_ACCESS_TOKEN;\n"+
			"fetch('http://localhost:3845/plugin', { headers: { Authorization: 'Bearer ' + token } });\n")

	mustWriteFile(t, filepath.Join(root, "plugins", "installed_plugins.json"), fmt.Sprintf(
		`{"version":2,"plugins":{`+
			`"superpowers@claude-plugins-official":[{"installPath":%q,"version":"4.0.3"}],`+
			`"figma@official":[{"installPath":%q,"version":"1.0.0"}]`+
			`}}`, super, figma))

	out, err := scanEnv(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Overall < 85 {
		t.Errorf("marketplace snapshot scored %d, want ≥85 (loopback EXFIL must not cap the environment); %s",
			out.Overall, summarizeFindings(out))
	}
	for _, a := range out.Artifacts {
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				continue
			}
			if f.Severity.Rank() >= model.SevHigh.Rank() {
				t.Errorf("%s [%s] %s on %s — marketplace snapshot must not raise high",
					f.Severity, f.RuleID, f.Title, a.Name)
			}
		}
	}
}

// TestRegression_OutOfHomeAndHTTPHooksScore is the false-negative guardrail.
//
// A colleague's settings.json had 14 events pointing at /Applications scripts plus an
// HTTP PermissionRequest. Those used to be coverage notes (or skipped HTTP entries), so
// the report looked clean. The scripts here live in a second temp dir — the same shape as
// /Applications, hermetic on CI. At least one finding must be medium or above.
//
// The app directory has a space in its name and the command quotes the path, because that is
// what the real settings.json said (`"/Applications/unibase-partner 3.app/…"`) — and a fixture
// without the space passed while the real machine still scored 100.
func TestRegression_OutOfHomeAndHTTPHooksScore(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "Applications", "Uni Base 3.app", "Contents", "hooks")
	events := []string{
		"PreToolUse", "PostToolUse", "PostToolUseFailure", "Notification",
		"UserPromptSubmit", "SessionStart", "SessionEnd", "Stop",
		"SubagentStart", "SubagentStop", "PreCompact", "PostCompact",
		"Setup", "CwdChanged",
	}
	hooks := map[string]any{}
	for i, ev := range events {
		script := filepath.Join(outside, fmt.Sprintf("e%d.sh", i))
		mustWriteFile(t, script, "#!/bin/sh\nexit 0\n")
		hooks[ev] = []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": `sh "` + script + `"`}},
		}}
	}
	hooks["PermissionRequest"] = []any{map[string]any{
		"hooks": []any{map[string]any{"type": "http", "url": "http://127.0.0.1:7439/decide"}},
	}}
	doc, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "settings.json"), string(doc))

	out, err := scanEnv(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var best model.Severity
	var saw002, saw003 bool
	for _, a := range out.Artifacts {
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				continue
			}
			if f.Severity.Rank() > best.Rank() {
				best = f.Severity
			}
			switch f.RuleID {
			case "HOOK-002":
				saw002 = true
			case "HOOK-003":
				saw003 = true
			}
		}
	}
	if best.Rank() < model.SevMedium.Rank() {
		t.Errorf("colleague-machine fixture highest scoring finding = %q, want ≥ medium; %s",
			best, summarizeFindings(out))
	}
	if !saw002 {
		t.Errorf("want HOOK-002 on the out-of-home scripts; %s", summarizeFindings(out))
	}
	if !saw003 {
		t.Errorf("want HOOK-003 on the HTTP PermissionRequest; %s", summarizeFindings(out))
	}
}

func summarizeFindings(out model.ScanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "overall=%d artifacts=%d", out.Overall, len(out.Artifacts))
	for _, a := range out.Artifacts {
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				continue
			}
			fmt.Fprintf(&b, " %s:%s/%s", a.Name, f.RuleID, f.Severity)
		}
	}
	return b.String()
}
