// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
)

// TestRawOutputNamesNoWorkDirectory: aguard reports every path under the staging root,
// which lives in the operator's per-user temp directory. Four committed judge runs carried
// about twelve thousand copies of that directory. keepRaw writes <work> in its place, so a raw
// file says where in the STAGED tree something was without saying whose machine staged it.
func TestRawOutputNamesNoWorkDirectory(t *testing.T) {
	a := &Adapter{Work: t.TempDir(), RawDir: t.TempDir()}
	res := model.ScanResult{Root: a.Work + "/s/home/.claude", Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "x", Path: a.Work + "/s/home/.claude/skills/x"}}}

	b, _ := json.Marshal(res)
	a.keepRaw("s", b)

	b, err := os.ReadFile(filepath.Join(a.RawDir, "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, a.Work) {
		t.Errorf("raw output names the work directory:\n%s", got)
	}
	if !strings.Contains(got, `"<work>/s/home/.claude"`) || !strings.Contains(got, `"<work>/s/home/.claude/skills/x"`) {
		t.Errorf("raw output lost the staged path:\n%s", got)
	}
	_ = context.Background
}

// stubAnswering writes a stand-in for the binary that prints doc on stdout whatever it is asked,
// so what the adapter does with the tool's own bytes can be checked without building aguard.
func stubAnswering(t *testing.T, doc string) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "answer.json")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "aguard")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestRawKeepsTheToolsOwnBytes: raw/ is what a run is re-folded from, so it has to be what the
// tool printed. It used to be the adapter's re-encoding of model.ScanResult, which adds every
// field the rig's model has and drops every one it lacks — an older binary's judge summary came
// back carrying "triage_calls":0 and "retries":0 it never reported, and a fold that picks its
// basis by whether those keys exist would read "not reported" as "reported zero".
func TestRawKeepsTheToolsOwnBytes(t *testing.T) {
	doc := `{
  "artifacts": [],
  "judge": {"ran": true, "artifacts": 1, "calls": 4, "failed": 0, "skipped": 0, "findings": 0},
  "field_from_a_newer_binary": 1
}
`
	a := newAdapter(t, stubAnswering(t, doc))
	a.RawDir = t.TempDir()
	tree := t.TempDir()
	write(t, tree, ".claude/settings.json", `{"permissions":{"allow":["Read"]}}`)

	row := a.Scan(context.Background(), corpus.Sample{Sample: "s", Class: "benign", Surface: []string{"permission"}}, tree)
	if row.Outcome != ledger.Scored {
		t.Fatalf("outcome = %q, want scored (detail: %s)", row.Outcome, row.Detail)
	}
	got := readFile(t, filepath.Join(a.RawDir, "s.json"))
	for _, key := range []string{`"triage_calls"`, `"retries"`} {
		if strings.Contains(got, key) {
			t.Errorf("raw/ carries %s, which the tool never printed:\n%s", key, got)
		}
	}
	if !strings.Contains(got, `"field_from_a_newer_binary"`) {
		t.Errorf("raw/ dropped a field the tool printed:\n%s", got)
	}
}
