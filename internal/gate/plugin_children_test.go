// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// familyResult is a plugin and one child skill (linked by the `plugin` member, written through JSON so
// the test compiles on a tree without it). Both carry the plugin tree's high; with own, the child also
// carries a high of its own and scores lower than its plugin (P-044).
func familyResult(t *testing.T, own bool) model.ScanResult {
	t.Helper()
	high := `{"rule_id":"EXEC-001","dimension":4,"severity":"high","source":"static","evidence":[{"file":"skills/s1/SKILL.md","line":6}]}`
	child := high
	childScore := 75
	if own {
		child += `,{"rule_id":"EXFIL-001","dimension":1,"severity":"high","source":"static","evidence":[{"file":"skills/s1/SKILL.md","line":9}]}`
		childScore = 50
	}
	doc := `{"artifacts":[{"kind":"plugin","name":"p@mkt (1.0.0)","path":"/h/p","hash":"` + strings.Repeat("a", 64) + `","score":75,"findings":[` + high + `]},` +
		`{"kind":"skill","name":"p:s1 (plugin p@mkt)","path":"/h/p/skills/s1","hash":"` + strings.Repeat("c", 64) + `","plugin":"p@mkt (1.0.0)","score":` +
		itoa(childScore) + `,"findings":[` + child + `]}]}`
	var r model.ScanResult
	if err := json.Unmarshal([]byte(doc), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// TestSessionStart_DoesNotListAPluginsChildren: every blocking rule of a child is already on its
// plugin's row, so the session-start audit lists the plugin once, as before the children existed.
func TestSessionStart_DoesNotListAPluginsChildren(t *testing.T) {
	o, _ := newOpts(t, nil)
	o.ScanRoot = func() (model.ScanResult, error) { return familyResult(t, false), nil }
	out, _ := Handle(Event{HookEventName: EventSessionStart, Source: "startup"}, o)
	if !strings.Contains(out.SystemMessage, "1 artifact(s) carry") || strings.Contains(out.SystemMessage, "p:s1") {
		t.Errorf("session start must list the plugin alone:\n%s", out.SystemMessage)
	}
}

// TestSummarize_NeverPicksAPluginsChild: `aguard approve <plugin>` and the gate's verdict describe the
// plugin; a child scoring lower must not become "the worst artifact", or approving the plugin would
// store the child's hash.
func TestSummarize_NeverPicksAPluginsChild(t *testing.T) {
	v, ok := Summarize(familyResult(t, true), model.SevHigh)
	if !ok || v.Kind != string(model.KindPlugin) || v.Hash != strings.Repeat("a", 64) {
		t.Errorf("verdict = %s %q %q, want the plugin", v.Kind, v.Name, v.Hash)
	}
}
