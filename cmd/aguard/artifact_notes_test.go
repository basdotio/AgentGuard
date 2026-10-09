// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/report"
)

// brokenSettings and goodSettings are the same .claude/settings.json, cut short and written out.
// The broken one is what a half-saved editor buffer or a bad merge leaves behind: collect turns it
// into a hook artifact carrying PARSE-000 (withParseError) rather than a scan-level note, and the
// human reports used to read only the scan-level notes — so the file that holds the hooks,
// permissions and env rendered as "looks safe … Nothing was found to check", exit 0.
const (
	brokenSettings = `{"hooks": {"PreToolUse": [ broken`
	goodSettings   = `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo ok"}]}]}}` + "\n"
)

// settingsRoot builds <tmp>/.claude/settings.json with body and returns the .claude path.
func settingsRoot(t *testing.T, body string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".claude")
	mustWriteFile(t, filepath.Join(root, "settings.json"), body)
	return root
}

type rendered struct{ name, body string }

// humanReports renders r the four ways a person reads it, in a fixed order.
func humanReports(t *testing.T, r model.ScanResult) []rendered {
	t.Helper()
	var brief, full, md, page bytes.Buffer
	writeReport(&brief, r, false)
	writeReport(&full, r, true)
	if err := report.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if err := report.HTML(&page, r); err != nil {
		t.Fatal(err)
	}
	return []rendered{{"terminal", brief.String()}, {"verbose", full.String()}, {"markdown", md.String()}, {"html", page.String()}}
}

// TestBrokenSettingsIsNotReportedSafe: whichever way the broken root is reached, every report a
// person reads names the parse failure and its file, and none of them calls the setup safe or
// says nothing was found. The default terminal view shortens paths, so it is held to the tail.
func TestBrokenSettingsIsNotReportedSafe(t *testing.T) {
	for _, entry := range []struct {
		name string
		run  func(string) (model.ScanResult, error)
	}{
		{"scanEnv", func(p string) (model.ScanResult, error) { return scanEnv(p, scanOpts{}) }},
		{"checkTarget", func(p string) (model.ScanResult, error) { return checkTarget(p, scanOpts{}) }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root := settingsRoot(t, brokenSettings)
			out, err := entry.run(root)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "settings.json")
			for _, r := range humanReports(t, out) {
				want := file
				if r.name == "terminal" {
					want = filepath.Join(".claude", "settings.json")
				}
				if !strings.Contains(r.body, "PARSE-000") || !strings.Contains(r.body, want) {
					t.Errorf("%s report does not name the parse failure [PARSE-000] in %s:\n%s", r.name, want, r.body)
				}
				for _, lie := range []string{"looks safe", "Nothing was found to check"} {
					if strings.Contains(r.body, lie) {
						t.Errorf("%s report says %q about a settings.json it could not parse", r.name, lie)
					}
				}
			}
		})
	}
}

// TestCheckBrokenSettingsCLI is the reviewer's reproduction, through the binary: `aguard check`
// on the broken root. The note is now on stdout; the exit code is still 0, because a dimension-0
// note never gates (score.Deterministic) — making a broken config fail `check` is a different
// contract, and this test pins that it was not changed on the way.
func TestCheckBrokenSettingsCLI(t *testing.T) {
	root := settingsRoot(t, brokenSettings)
	stdout, stderr, code := runAguard(t, "check", root)
	if code != 0 {
		t.Errorf("exit %d, want 0 (dim-0 notes never gate); stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "PARSE-000") || strings.Contains(stdout, "looks safe") {
		t.Errorf("aguard check on a settings.json that does not parse:\n%s", stdout)
	}
}

// normalized makes a report comparable across machines: the temporary directory becomes <TMP>
// (both spellings, since macOS hands out /var paths that resolve under /private), and the clock,
// the build version and the sandbox banner are fixed in the result before rendering.
func normalized(root, s string) string {
	dir := filepath.Dir(root)
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		s = strings.ReplaceAll(s, real, "<TMP>")
	}
	return strings.ReplaceAll(s, dir, "<TMP>")
}

func pinned(out model.ScanResult) model.ScanResult {
	out.ScannedAt = 1767225600 // 2026-01-01 00:00 UTC
	out.ToolVersion = "test"
	out.Sandbox = nil
	return out
}

// TestCleanSettingsReportIsUnchanged is the reverse assertion: the same settings.json, written out
// in full, renders byte for byte what it rendered before artifact-level notes reached the human
// reports. The goldens were recorded from main; a clean config has no note of either kind,
// so nothing in this change may touch its output.
func TestCleanSettingsReportIsUnchanged(t *testing.T) {
	root := settingsRoot(t, goodSettings)
	out, err := checkTarget(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Overall != 100 || len(out.Notes) != 0 {
		t.Fatalf("fixture drifted: overall=%d notes=%d — it is meant to be a clean config", out.Overall, len(out.Notes))
	}
	reports := humanReports(t, pinned(out))
	for _, g := range []struct{ name, want string }{
		{"terminal", cleanTerminalGolden},
		{"verbose", cleanTerminalGolden},
		{"markdown", cleanMarkdownGolden},
	} {
		for _, r := range reports {
			if r.name == g.name {
				if got := normalized(root, r.body); got != g.want {
					t.Errorf("%s report of a clean config changed:\n--- got ---\n%s\n--- want ---\n%s", g.name, got, g.want)
				}
			}
		}
	}
	page := reports[3].body
	for _, want := range []string{"Your Claude Code setup looks safe. No findings.", "Checked 1 hook."} {
		if !strings.Contains(page, want) {
			t.Errorf("html report of a clean config lost %q", want)
		}
	}
	if strings.Contains(page, `id="notchecked"`) {
		t.Error("html report of a clean config grew a Not checked section")
	}
}

const cleanTerminalGolden = `AgentGuard scan · root=<TMP>/.claude
Risk score 100/100 (Low)

Summary
  Your Claude Code setup looks safe. No findings.
  Checked 1 hook.


✅ No risk findings (static).

Scan details
  Where the scan looked:
    Config root            read    <TMP>/.claude
    User MCP config        absent  <TMP>/.claude.json
    Claude Desktop store   absent  <TMP>/Library/Application Support/Claude/local-agent-mode-sessions
    Desktop session cache  absent  <TMP>/Library/Application Support/Claude/claude-code-sessions
  Inventory: skills=0 mcp=0 hooks=1 permissions=0 subagents=0 commands=0 plugins=0 connectors=0
  OWASP Agentic (2026): 7/10 categories have rules; silent on ASI-08, ASI-09, ASI-10 (runtime behaviour a static scan cannot witness)
  Static analysis only: it cannot prove malice or see runtime behaviour. Rule ids are in brackets; --json and --sarif carry every path in full.
`

const cleanMarkdownGolden = "" +
	"# AgentGuard scan · `<TMP>/.claude`\n" +
	"\n" +
	"**Risk score 100/100 (Low)** — deterministic and reproducible; this is the number `--fail-on` reads.\n" +
	"\n" +
	"## Summary\n" +
	"\n" +
	"Your Claude Code setup looks safe. No findings.  \n" +
	"Checked 1 hook.  \n" +
	"\n" +
	"## What to look at\n" +
	"\n" +
	"✅ No risk findings (static).\n" +
	"\n" +
	"## Scan details\n" +
	"\n" +
	"Where the scan looked:\n" +
	"\n" +
	"| Place | Status | Path |\n" +
	"|---|---|---|\n" +
	"| `Config root` | read | `<TMP>/.claude` |\n" +
	"| `User MCP config` | absent | `<TMP>/.claude.json` |\n" +
	"| `Claude Desktop store` | absent | `<TMP>/Library/Application Support/Claude/local-agent-mode-sessions` |\n" +
	"| `Desktop session cache` | absent | `<TMP>/Library/Application Support/Claude/claude-code-sessions` |\n" +
	"\n" +
	"- Inventory: skills=0 mcp=0 hooks=1 permissions=0 subagents=0 commands=0 plugins=0 connectors=0\n" +
	"- OWASP Agentic (2026): 7/10 categories have rules; silent on ASI-08, ASI-09, ASI-10 (runtime behaviour a static scan cannot witness)\n" +
	"- Static analysis only: it cannot prove malice or see runtime behaviour. `--json` and `--sarif` carry every path in full; rule ids are explained in docs/rules.md: https://github.com/basdotio/AgentGuard/blob/main/docs/rules.md\n" +
	"\n" +
	"<sub>AgentGuard test · scanned 2026-01-01 00:00 UTC</sub>\n"

// TestBrokenSettingsMachineOutputUnchanged is the other reverse assertion: the data was always
// there, and this change is about who renders it, not where it lives. JSON keeps PARSE-000 on the
// artifact and the scan-level notes empty, SARIF keeps the result attributed to the artifact, the
// score stays 100 and no threshold trips on a dimension-0 note.
func TestBrokenSettingsMachineOutputUnchanged(t *testing.T) {
	root := settingsRoot(t, brokenSettings)
	out, err := checkTarget(root, scanOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var js bytes.Buffer
	enc := json.NewEncoder(&js)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Artifacts []struct {
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Findings []struct {
				RuleID    string `json:"rule_id"`
				Dimension int    `json:"dimension"`
			} `json:"findings"`
		} `json:"artifacts"`
		Notes   []json.RawMessage `json:"notes"`
		Overall int               `json:"overall"`
	}
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Artifacts) != 1 || doc.Artifacts[0].Kind != "hook" || doc.Artifacts[0].Name != "settings.json" ||
		len(doc.Artifacts[0].Findings) != 1 || doc.Artifacts[0].Findings[0].RuleID != "PARSE-000" || doc.Artifacts[0].Findings[0].Dimension != 0 {
		t.Errorf("JSON no longer carries PARSE-000 on the settings.json artifact:\n%s", js.String())
	}
	if doc.Notes == nil || len(doc.Notes) != 0 {
		t.Errorf("JSON scan-level notes must stay an empty list — the note was not moved:\n%s", js.String())
	}

	var sb bytes.Buffer
	if err := report.SARIF(&sb, out, "test", "https://example.invalid"); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Runs []struct {
			Results []struct {
				RuleID     string         `json:"ruleId"`
				Properties map[string]any `json:"properties"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(sb.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range log.Runs[0].Results {
		if r.RuleID == "PARSE-000" && r.Properties["artifact"] == "hook:settings.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("SARIF no longer carries PARSE-000 attributed to hook:settings.json:\n%s", sb.String())
	}

	if out.Overall != 100 || doc.Overall != 100 {
		t.Errorf("score moved: %d / %d, want 100 — the artifact was not read, so it has no findings to score", out.Overall, doc.Overall)
	}
	for _, threshold := range []string{"high", "low"} {
		if err := failGate(out, threshold, "", false); err != nil {
			t.Errorf("--fail-on %s tripped on a dimension-0 note: %v", threshold, err)
		}
	}
}
