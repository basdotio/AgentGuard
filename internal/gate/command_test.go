// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// ordinaryVerdict is a verdict for a path with nothing in it a shell would act on — the common
// case, whose messages must not change by a single byte.
func ordinaryVerdict(path string, sev model.Severity) Verdict {
	v, _ := Summarize(model.ScanResult{Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "pdf-export", Path: path, Score: 42,
		Hash:     "a1e9cd5dbc1a0f00ba11cafe0123456789abcdef0123456789abcdef01234567",
		Findings: []model.Finding{finding("EXEC-001", sev, 4)},
	}}}, model.SevHigh)
	return v
}

// TestCommandsForAnOrdinaryPathAreUnchanged pins, literally, what the gate prints for a short
// path without shell syntax (a space and a single quote are fine inside double quotes). The
// copyable commands may only change for paths where today's form fails when pasted.
func TestCommandsForAnOrdinaryPathAreUnchanged(t *testing.T) {
	const p = "/Users/someone/Library/Application Support/Claude/skills/o'brien/pdf-export"
	wantReason := "AgentGuard has not audited this skill before, and it carries findings.\n\n" +
		"  pdf-export  42/100 (High)\n  " + p + "\n  content hash a1e9cd5dbc1a…\n\n" +
		"  high     EXEC-001   t-EXEC-001  run.sh:3\n\n" +
		"Static rules only: no model was consulted, nothing was executed, nothing left the machine.\n" +
		"Full report: aguard check \"" + p + "\" · trust these exact bytes: aguard approve \"" + p + "\"\n"
	if got := ordinaryVerdict(p, model.SevHigh).Reason(); got != wantReason {
		t.Errorf("Reason changed for an ordinary path:\ngot  %q\nwant %q", got, wantReason)
	}
	wantLine := "AgentGuard: skill \"pdf-export\" 42/100 (High) · below the threshold, but EXEC-001 (medium) found · " +
		"not recorded as trusted: it is re-audited on every load until the content is clean, or you accept it with: " +
		"aguard approve \"" + p + "\""
	if got := ordinaryVerdict(p, model.SevMedium).UnrememberedLine(); got != wantLine {
		t.Errorf("UnrememberedLine changed for an ordinary path:\ngot  %q\nwant %q", got, wantLine)
	}
}

// TestDenyCommandForAnOrdinaryPathIsUnchanged is the same pin for the refusal an auto-answering
// permission mode gets, through the real handler and a real directory.
func TestDenyCommandForAnOrdinaryPathIsUnchanged(t *testing.T) {
	o, _ := newOpts(t, nil)
	dir := mustSkillDir(t, o.Root, "evil")
	o.Scan = func(string) (model.ScanResult, error) {
		r := result("evil", "h1", 42, finding("EXEC-001", model.SevHigh, 4))
		r.Artifacts[0].Path = dir
		return r, nil
	}
	out, _ := Handle(Event{HookEventName: EventPreToolUse, ToolName: SkillTool, ToolUseID: "c1",
		PermissionMode: "auto", ToolInput: json.RawMessage(`{"skill":"evil"}`)}, o)
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.PermissionDecision != DecisionDeny {
		t.Fatalf("fixture: no deny under auto: %+v", out)
	}
	want := "To load it anyway, decide outside the session: aguard approve \"" + dir + "\"\n"
	if r := out.HookSpecificOutput.PermissionDecisionReason; !strings.HasSuffix(r, want) {
		t.Errorf("the refusal's command changed for an ordinary path:\n%s\nwant suffix %q", r, want)
	}
}

// TestCommandsCannotSizeTheMessage: a command carries the full path only up to a fixed bound,
// so an artifact still cannot turn a one-line notice into the context window.
func TestCommandsCannotSizeTheMessage(t *testing.T) {
	p := "/tmp/" + strings.Repeat("p", 100_000)
	if n := len(ordinaryVerdict(p, model.SevHigh).Reason()); n >= 4000 {
		t.Errorf("Reason is %d bytes for a 100 000-byte path", n)
	}
	if n := len(ordinaryVerdict(p, model.SevMedium).UnrememberedLine()); n >= 4000 {
		t.Errorf("UnrememberedLine is %d bytes for a 100 000-byte path", n)
	}
}
