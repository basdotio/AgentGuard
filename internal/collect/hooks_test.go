// SPDX-License-Identifier: MIT
package collect

import (
	"path/filepath"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// hookArtifacts collects a root and returns its hook artifacts.
func hookArtifacts(t *testing.T, settings string) []model.ArtifactReport {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".claude")
	mustWrite(t, filepath.Join(root, "settings.json"), settings)
	var out []model.ArtifactReport
	for _, a := range CollectAll(root).Artifacts {
		if a.Kind == model.KindHook {
			out = append(out, a)
		}
	}
	return out
}

// TestCollectHooks_PerCommand: a hook artifact is ONE (event, matcher, command) triple —
// per-event blobs cannot say which command is at fault (spec §4).
func TestCollectHooks_PerCommand(t *testing.T) {
	arts := hookArtifacts(t, `{"hooks":{
		"PreToolUse":[
			{"matcher":"Bash","hooks":[{"type":"command","command":"echo a"},{"type":"command","command":"echo b"}]},
			{"matcher":"Read","hooks":[{"type":"command","command":"echo c"}]}],
		"UserPromptSubmit":[{"hooks":[{"type":"command","command":"echo d"}]}]}}`)

	want := []model.Hook{
		{Event: "PreToolUse", Matcher: "Bash", Command: "echo a"},
		{Event: "PreToolUse", Matcher: "Bash", Command: "echo b"},
		{Event: "PreToolUse", Matcher: "Read", Command: "echo c"},
		{Event: "UserPromptSubmit", Matcher: "", Command: "echo d"},
	}
	if len(arts) != len(want) {
		t.Fatalf("hook artifacts = %d, want %d", len(arts), len(want))
	}
	for i, w := range want {
		got := arts[i].Hook
		got.Entry = "" // the raw entry is checked below; the four fields are the point here
		if got != w {
			t.Errorf("artifact %d (%s) hook = %+v, want %+v", i, arts[i].Name, got, w)
		}
	}
	// Entry is the hook's own JSON object as written — what the content hash binds.
	if e := arts[1].Hook.Entry; e != `{"type":"command","command":"echo b"}` {
		t.Errorf("artifact 1 Entry = %s, want the entry exactly as written", e)
	}
	// Names must identify the interception point, so a finding can be attributed.
	for i, wantName := range []string{
		"PreToolUse[Bash]#1", "PreToolUse[Bash]#2", "PreToolUse[Read]#3", "UserPromptSubmit[*]#1",
	} {
		if arts[i].Name != wantName {
			t.Errorf("artifact %d name = %q, want %q", i, arts[i].Name, wantName)
		}
	}
}

// TestCollectHooks_EventOrderDeterministic: map iteration order is random, so events must
// be sorted — scan output has to be byte-stable across runs (it feeds attestation).
func TestCollectHooks_EventOrderDeterministic(t *testing.T) {
	settings := `{"hooks":{
		"Stop":[{"hooks":[{"type":"command","command":"echo s"}]}],
		"PreToolUse":[{"hooks":[{"type":"command","command":"echo p"}]}],
		"UserPromptSubmit":[{"hooks":[{"type":"command","command":"echo u"}]}]}}`
	want := []string{"PreToolUse[*]#1", "Stop[*]#1", "UserPromptSubmit[*]#1"}
	for run := 0; run < 5; run++ {
		arts := hookArtifacts(t, settings)
		for i, a := range arts {
			if a.Name != want[i] {
				t.Fatalf("run %d: artifact %d = %q, want %q (event order must be sorted)", run, i, a.Name, want[i])
			}
		}
	}
}

// TestCollectHooks_UnknownShapeNoted: an entry we cannot read a command out of is a
// COVERAGE GAP and must be reported — "not scanned" may never look like "clean".
func TestCollectHooks_UnknownShapeNoted(t *testing.T) {
	cases := map[string]string{
		"event is not an array":    `{"hooks":{"PreToolUse":{"matcher":"Bash"}}}`,
		"entry carries no command": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"future"}]}]}}`,
		"http hook with no url":    `{"hooks":{"PreToolUse":[{"hooks":[{"type":"http"}]}]}}`,
	}
	for name, settings := range cases {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".claude")
			mustWrite(t, filepath.Join(root, "settings.json"), settings)
			res := CollectAll(root)
			found := false
			for _, n := range res.Notes {
				if n.RuleID == "PARSE-000" && n.Dimension == 0 {
					found = true
				}
			}
			if !found {
				t.Errorf("unparsable hook entry dropped silently; notes=%+v", res.Notes)
			}
		})
	}
}

// TestCollectHooks_EmptyEventIsNotAGap: an event registering nothing is not a gap.
func TestCollectHooks_EmptyEventIsNotAGap(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	mustWrite(t, filepath.Join(root, "settings.json"), `{"hooks":{"PreToolUse":[]}}`)
	res := CollectAll(root)
	if res.Env.Hooks != 0 {
		t.Errorf("hooks = %d, want 0", res.Env.Hooks)
	}
	if n := notesExceptEmptyRoot(res.Notes); len(n) != 0 {
		t.Errorf("empty hook event should not warn; notes=%+v", n)
	}
}

// TestCollectHooks_HTTPTypeIsAnArtifact: type=http with a URL is a first-class hook, not a
// coverage gap. The URL is the target; skipping it used to make "payload posted off-box"
// look like "no hook here".
func TestCollectHooks_HTTPTypeIsAnArtifact(t *testing.T) {
	arts := hookArtifacts(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[
		{"type":"http","url":"http://127.0.0.1:9/h"},
		{"type":"command","command":"echo x"}
	]}]}}`)
	if len(arts) != 2 {
		t.Fatalf("artifacts = %d, want 2 (http + command)", len(arts))
	}
	http := arts[0].Hook
	if http.Type != "http" || http.URL != "http://127.0.0.1:9/h" || http.Command != "" {
		t.Errorf("http hook = %+v, want type=http, url set, command empty", http)
	}
	if arts[0].Name != "PreToolUse[Bash]#1" {
		t.Errorf("http hook name = %q", arts[0].Name)
	}
	if arts[1].Hook.Command != "echo x" || arts[1].Hook.URL != "" {
		t.Errorf("command hook = %+v", arts[1].Hook)
	}
}
