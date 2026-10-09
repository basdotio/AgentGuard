// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/report"
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

// expand runs one quoted word through a real shell in an empty directory and returns the
// arguments it became, failing the test if the shell created anything there.
func expand(t *testing.T, shell, word string) []string {
	t.Helper()
	cwd := t.TempDir()
	cmd := exec.Command(shell, "-c", `printf '%s\0' `+word)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "HOME=/nonexistent-home")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s could not expand %q: %v", shell, word, err)
	}
	if ents, _ := os.ReadDir(cwd); len(ents) > 0 {
		t.Errorf("%s executed part of %q: %d file(s) appeared", shell, word, len(ents))
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

// printableQuoted are paths a command must survive that need no escape form: plain text, and
// every character POSIX double quotes do not protect.
var printableQuoted = []string{
	"/Users/someone/.claude/skills/pdf-export",
	"/Users/someone/Library/Application Support/Claude/skills/x",
	"/tmp/o'brien/it''s",
	`/tmp/say "hi"`,
	"/tmp/$HOME/${HOME}/$(touch pwned)/`touch pwned2`",
	`/tmp/back\slash\\double\`,
	"/tmp/bang!/!!/!$",
	"/tmp/技能/naïve/emoji-🙂",
	"/tmp/glob*?[a]/{a,b}/~user/semi;colon&amp|pipe>gt<lt#hash",
	"-leading-dash", "",
}

// invisibleQuoted hold characters that may never be printed raw (invariant #7).
var invisibleQuoted = []string{
	"/tmp/esc\x1b[2J/bell\x07/tab\there/new\nline",
	"/tmp/pay\u202egnp.sh",
	"/tmp/zero\u200bwidth/soft\u00adhyphen/bom\ufeff",
	"/tmp/bad\xffutf8/\xc3",
	"/tmp/octal-boundary\x01" + "777/then\\'!quote",
}

// TestShellQuoteRoundTripsThroughAShell: what the shell makes of a quoted word is exactly the
// path, as one word, with nothing executed. The printable forms are POSIX and go through /bin/sh;
// the $'…' form goes through bash and zsh where they are installed.
func TestShellQuoteRoundTripsThroughAShell(t *testing.T) {
	for _, p := range printableQuoted {
		q := shellQuote(p)
		if strings.HasPrefix(q, "$'") {
			t.Errorf("printable %q took the escape form %s", p, q)
		}
		if got := expand(t, "/bin/sh", q); len(got) != 1 || got[0] != p {
			t.Errorf("/bin/sh read %s as %q, want %q", q, got, p)
		}
	}
	shells := 0
	for _, sh := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			continue
		}
		shells++
		for _, p := range append(append([]string{}, printableQuoted...), invisibleQuoted...) {
			if got := expand(t, path, shellQuote(p)); len(got) != 1 || got[0] != p {
				t.Errorf("%s read %s as %q, want %q", sh, shellQuote(p), got, p)
			}
		}
	}
	if shells == 0 {
		t.Skip("neither bash nor zsh is installed: the $'…' form was not run through a shell")
	}
}

// TestShellQuoteNeverPrintsAnInvisibleCharacter: the escape form spells control, bidi and
// zero-width characters (and bytes that are not UTF-8) in printable ASCII, so a command is as
// safe to render as the rest of the message.
func TestShellQuoteNeverPrintsAnInvisibleCharacter(t *testing.T) {
	for _, p := range invisibleQuoted {
		q := shellQuote(p)
		if !strings.HasPrefix(q, "$'") {
			t.Errorf("%q did not take the escape form: %q", p, q)
		}
		for i := 0; i < len(q); i++ {
			if q[i] < 0x20 || q[i] >= 0x7f {
				t.Errorf("%q quoted to %q, which holds byte %#x", p, q, q[i])
				break
			}
		}
		if report.Sanitize(q) != q {
			t.Errorf("Sanitize would change the quoted form %q", q)
		}
	}
}

// TestShellQuoteIsPercentQForOrdinaryText is the reverse assertion in general form: wherever
// Go's %q was already exact for a shell, the gate prints the same bytes as before.
func TestShellQuoteIsPercentQForOrdinaryText(t *testing.T) {
	for _, p := range []string{
		"/Users/someone/.claude/skills/pdf-export",
		"/Users/someone/Library/Application Support/Claude/local-agent-mode-sessions/a/b/skills/x",
		"/tmp/o'brien", "/tmp/技能/naïve", "relative/skills/plain", "./x", "",
	} {
		if got, want := shellQuote(p), fmt.Sprintf("%q", p); got != want {
			t.Errorf("shellQuote(%q) = %s, want today's %s", p, got, want)
		}
	}
}

// TestCommandArgBound: the full path up to the bound, a placeholder past it — never a clip.
func TestCommandArgBound(t *testing.T) {
	at := "/" + strings.Repeat("a", maxCommandArgLen-3) // quoted: exactly maxCommandArgLen bytes
	if got := CommandArg(at); got != `"`+at+`"` {
		t.Errorf("a path whose quoted form is exactly %d bytes was not printed in full", maxCommandArgLen)
	}
	over := at + "a"
	want := fmt.Sprintf("<path of %d bytes, too long to print>", len(over))
	if got := CommandArg(over); got != want {
		t.Errorf("past the bound got %.40q…, want %q", got, want)
	}
}
