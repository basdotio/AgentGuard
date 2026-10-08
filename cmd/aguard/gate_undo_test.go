// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/gate"
	"github.com/basdotio/AgentGuard/internal/model"
)

// acceptRisk walks one risky skill through the prompt and the load, the way an operator who
// clicks "yes" does, and returns the root and the message the gate printed afterwards.
func acceptRisk(t *testing.T, hash string) (root, message string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), ".claude")
	dir := filepath.Join(root, "skills", "evil")
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: evil\n---\nx\n")
	store := gate.LoadStore(gate.ApprovalsPath(root))
	o := gate.Options{
		Root: root, Home: filepath.Dir(root), Threshold: model.SevHigh, Action: gate.DecisionAsk,
		ToolVersion: "test", Now: func() int64 { return 1_700_000_000 }, Store: store,
		Scan: func(string) (model.ScanResult, error) {
			return model.ScanResult{Artifacts: []model.ArtifactReport{{
				Kind: model.KindSkill, Name: "evil", Path: dir, Hash: hash, Score: 42,
				Findings: []model.Finding{{RuleID: "EXFIL-001", Dimension: 3, Severity: model.SevHigh, Source: model.SrcStatic,
					Title: "t", Evidence: []model.Evidence{{File: "run.sh", Line: 3, Snippet: "x"}}}},
			}}}, nil
		},
	}
	gate.Handle(gate.Event{HookEventName: gate.EventPreToolUse, ToolName: gate.SkillTool, ToolUseID: "c1",
		ToolInput: json.RawMessage(`{"skill":"evil"}`)}, o)
	out, _ := gate.Handle(gate.Event{HookEventName: gate.EventPostToolUse, ToolName: gate.SkillTool, ToolUseID: "c1",
		ToolInput: json.RawMessage(`{"skill":"evil"}`), ToolResponse: json.RawMessage(`{"success":true}`)}, o)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Approved(hash); !ok {
		t.Fatalf("fixture: the approval was not recorded; message %q", out.SystemMessage)
	}
	return root, out.SystemMessage
}

// TestUndoHintPastesAsIs: the gate's one instruction for taking an approval back has to work
// when copied exactly as printed. It printed the display form of the hash — twelve characters
// and an ellipsis — and `forget` read the ellipsis as part of the prefix.
func TestUndoHintPastesAsIs(t *testing.T) {
	hash := "a1e9cd5dbc1a0f00ba11cafe0123456789abcdef0123456789abcdef01234567"
	root, msg := acceptRisk(t, hash)
	_, cmd, ok := strings.Cut(msg, "undo with: ")
	if !ok {
		t.Fatalf("no undo hint in %q", msg)
	}
	args := strings.Fields(cmd)
	if len(args) != 4 || args[0] != "aguard" || args[1] != "approvals" || args[2] != "forget" {
		t.Fatalf("undo hint %q is not `aguard approvals forget <hash>`", cmd)
	}
	var out bytes.Buffer
	if err := forgetApproval(&out, root, args[3]); err != nil {
		t.Fatalf("the undo command as printed failed: %v", err)
	}
	if _, ok := gate.LoadStore(gate.ApprovalsPath(root)).Approved(hash); ok {
		t.Error("the undo command succeeded but the approval is still there")
	}
	if !strings.Contains(msg, "content a1e9cd5dbc1a…") {
		t.Errorf("the display form of the hash changed: %q", msg)
	}
}

// TestForgetAcceptsTheShortHashAsPrinted: every gate message shows a hash in its display form,
// and that is where an operator copies it from. The trailing ellipsis is dropped; everything
// else about prefix matching — unique or refused — stays.
func TestForgetAcceptsTheShortHashAsPrinted(t *testing.T) {
	hash := "a1e9cd5dbc1a0f00ba11cafe0123456789abcdef0123456789abcdef01234567"
	for _, arg := range []string{"a1e9cd5dbc1a…", "a1e9cd5dbc1a...", "a1e9cd5dbc1a"} {
		root, _ := acceptRisk(t, hash)
		if err := forgetApproval(&bytes.Buffer{}, root, arg); err != nil {
			t.Errorf("forget %q: %v", arg, err)
		}
	}
}

// TestForgetRefusesAnEmptyPrefix: the empty string is a prefix of every hash, so `forget ""`
// silently withdrew an approval whenever exactly one existed — and trimming an ellipsis must
// not create the same hole from `forget …`.
func TestForgetRefusesAnEmptyPrefix(t *testing.T) {
	hash := "a1e9cd5dbc1a0f00ba11cafe0123456789abcdef0123456789abcdef01234567"
	for _, arg := range []string{"", "…", "..."} {
		root, _ := acceptRisk(t, hash)
		if err := forgetApproval(&bytes.Buffer{}, root, arg); err == nil {
			t.Errorf("forget %q succeeded; an empty prefix must be refused", arg)
		}
		if _, ok := gate.LoadStore(gate.ApprovalsPath(root)).Approved(hash); !ok {
			t.Errorf("forget %q withdrew the approval", arg)
		}
	}
}

// TestForgetStillRequiresAUniqueMatch is the reverse side: accepting the printed form must not
// loosen matching. A prefix two approvals share is still refused, and one that matches nothing
// still says so, ellipsis or not.
func TestForgetStillRequiresAUniqueMatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	store := gate.LoadStore(gate.ApprovalsPath(root))
	for _, h := range []string{
		"a1e9cd5dbc1a0000000000000000000000000000000000000000000000000001",
		"a1e9cd5dbc1a0000000000000000000000000000000000000000000000000002",
	} {
		store.Approve(gate.Approval{Hash: h, Name: "evil", Kind: "skill", Verdict: gate.VerdictAccepted})
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := forgetApproval(&bytes.Buffer{}, root, "a1e9cd5dbc1a…"); err == nil || !strings.Contains(err.Error(), "matches 2 approvals") {
		t.Errorf("a shared prefix must be refused as ambiguous, got %v", err)
	}
	if err := forgetApproval(&bytes.Buffer{}, root, "ffff0000…"); err == nil || !strings.Contains(err.Error(), "no approval matches") {
		t.Errorf("a prefix that matches nothing must say so, got %v", err)
	}
	if n := len(gate.LoadStore(gate.ApprovalsPath(root)).List()); n != 2 {
		t.Errorf("%d approval(s) left, want both", n)
	}
}
