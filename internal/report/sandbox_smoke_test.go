// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestSandboxBanner_RendersInTextAndHTML: when a scan is marked as run in a sandbox, the
// terminal, the saved HTML and the markdown must carry the warning near the top, with the signals — a saved
// report opened without the chat must still say the 100 is about a throwaway cloud box.
func TestSandboxBanner_RendersInTextAndHTML(t *testing.T) {
	r := model.ScanResult{
		Root: "/root/.claude", ToolVersion: "v0.7.0", Overall: 100, OverallEffective: 100,
		Env:     model.EnvSummary{Skills: 1},
		Sandbox: &model.SandboxInfo{Signals: []string{"container marker /.dockerenv", "running as root (home /root)"}},
	}
	var tb bytes.Buffer
	Text(&tb, r)
	if !strings.Contains(tb.String(), "temporary cloud environment") || !strings.Contains(tb.String(), "container marker /.dockerenv") {
		t.Errorf("text banner missing:\n%s", tb.String())
	}
	var hb bytes.Buffer
	if err := HTML(&hb, r); err != nil {
		t.Fatal(err)
	}
	h := hb.String()
	if !strings.Contains(h, `class="sandbox"`) || !strings.Contains(h, "not your computer") {
		t.Errorf("html banner missing")
	}
	var mb bytes.Buffer
	if err := Markdown(&mb, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mb.String(), "temporary cloud environment") || !strings.Contains(mb.String(), "container marker /.dockerenv") {
		t.Errorf("markdown banner missing:\n%s", mb.String())
	}
	// A local run has neither.
	r.Sandbox = nil
	var lb bytes.Buffer
	Text(&lb, r)
	if strings.Contains(lb.String(), "temporary cloud environment") {
		t.Error("local run must not print the banner")
	}
}
