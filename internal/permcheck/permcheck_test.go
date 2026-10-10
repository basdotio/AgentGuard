// SPDX-License-Identifier: MIT
package permcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

func write(t *testing.T, json string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ids(fs []model.Finding) map[string]model.Finding {
	m := map[string]model.Finding{}
	for _, f := range fs {
		m[f.RuleID] = f
	}
	return m
}

func TestAudit_WildcardExec(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(uv run *)","Bash(python3 -c '*)"],"deny":["Read(~/.ssh/**)"]}}`)
	got := ids(Audit(p))
	if _, ok := got["PERM-002"]; !ok {
		t.Error("wildcard interpreter exec not flagged PERM-002")
	}
	if _, ok := got["PERM-004"]; ok {
		t.Error("deny present but PERM-004 (missing deny) fired")
	}
}

func TestAudit_InlineSecretRedacted(t *testing.T) {
	secret := "DEEPSEEK_API_KEY=sk-abcdef0123456789abcdef"
	p := write(t, `{"permissions":{"allow":["`+secret+`"]}}`)
	got := Audit(p)
	m := ids(got)
	f, ok := m["PERM-001"]
	if !ok {
		t.Fatal("inline secret not flagged PERM-001")
	}
	for _, e := range f.Evidence {
		if strings.Contains(e.Snippet, "sk-abcdef0123456789abcdef") {
			t.Errorf("inline secret value leaked into evidence: %q", e.Snippet)
		}
	}
}

func TestAudit_BroadReadAndMissingDeny(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Read(/**)","Bash(ls *)"]}}`)
	got := ids(Audit(p))
	if _, ok := got["PERM-003"]; !ok {
		t.Error("broad Read(/**) not flagged PERM-003")
	}
	if _, ok := got["PERM-004"]; !ok {
		t.Error("missing deny list not flagged PERM-004")
	}
}

func TestAudit_BashAnyExec(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(*)"],"deny":["x"]}}`)
	got := ids(Audit(p))
	f, ok := got["PERM-005"]
	if !ok {
		t.Fatal("Bash(*) not flagged PERM-005 (the most dangerous grant)")
	}
	if f.Severity != model.SevHigh {
		t.Errorf("Bash(*) severity = %s, want high", f.Severity)
	}
}

func TestAudit_InlineSecretNoPrefixRedacted(t *testing.T) {
	// No known prefix, no space after '=', 21 chars — the exact B1 gap.
	p := write(t, `{"permissions":{"allow":["Bash(export GH_TOKEN=abcdefghijklmnop)"],"deny":["x"]}}`)
	got := ids(Audit(p))
	f, ok := got["PERM-001"]
	if !ok {
		t.Fatal("inline GH_TOKEN=… not flagged PERM-001")
	}
	for _, e := range f.Evidence {
		if strings.Contains(e.Snippet, "abcdefghijklmnop") {
			t.Errorf("no-prefix inline secret leaked (B1): %q", e.Snippet)
		}
	}
}

func TestAudit_PreciseCommandsClean(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(git status)","Bash(go build ./...)"],"deny":["Read(~/.aws/**)"]}}`)
	if got := Audit(p); len(got) != 0 {
		t.Errorf("precise commands + deny should be clean; got %+v", got)
	}
}

// TestAudit_QuotedSecretRedacted (P-043): a permission entry that quotes the value after a credential flag
// printed it in the finding's snippet — the patterns could not start a value at a quote.
func TestAudit_QuotedSecretRedacted(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(python3 * --password \"correct horse\")","Bash(git -c core.pager=x --token 'correct horse' *)"],"deny":["Read(~/.ssh/**)"]}}`)
	got := Audit(p)
	if len(got) < 2 {
		t.Fatalf("want PERM-002 and PERM-006 to carry the entries, got %+v", got)
	}
	for _, f := range got {
		for _, e := range f.Evidence {
			if strings.Contains(e.Snippet, "horse") || !strings.Contains(e.Snippet, "<REDACTED>") {
				t.Errorf("%s snippet %q must carry the marker, not the value", f.RuleID, e.Snippet)
			}
		}
	}
}
