// SPDX-License-Identifier: MIT
package gate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
)

// statusToken is an obviously fake GitHub token, the shape the redactor's known-prefix table removes.
const statusToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

func deadStatus(cmd string) Status {
	return Status{
		SettingsPath: "/home/u/.claude/settings.json",
		Events:       []EventStatus{{Event: "PreToolUse[Skill]", Command: cmd, Broken: true}},
	}
}

// TestDeadRegistrationNote_SecretInCommandIsRedacted: GATE-001 is attached to every `scan` whose
// settings.json names a gate binary that is gone, and its snippet quoted the registered command as
// written — a config value, landing in JSON, markdown, SARIF and HTML without the redactor every
// other snippet goes through.
func TestDeadRegistrationNote_SecretInCommandIsRedacted(t *testing.T) {
	n := DeadRegistrationNote(deadStatus("/opt/" + statusToken + "/aguard hook"))
	if n == nil {
		t.Fatal("a dead registration produced no note")
	}
	if got, want := n.Evidence[0].Snippet, "missing hook command: /opt/<REDACTED>/aguard hook"; got != want {
		t.Errorf("snippet:\n  got  %q\n  want %q", got, want)
	}
	for _, s := range []string{n.Title, n.Why, n.Evidence[0].File, n.Evidence[0].Snippet} {
		if strings.Contains(s, statusToken) {
			t.Errorf("the token reached GATE-001 in clear: %q", s)
		}
	}
}

// TestDeadRegistrationNote_OrdinaryCommandUnchanged is the reverse assertion: the commands
// `aguard hook install` actually writes render exactly as before.
func TestDeadRegistrationNote_OrdinaryCommandUnchanged(t *testing.T) {
	for _, cmd := range []string{
		"/usr/local/bin/aguard hook",
		"/opt/homebrew/bin/aguard hook",
		`"/Applications/Some Tool/aguard" hook`,
		HookCommand("/home/u/go/bin/aguard"),
	} {
		n := DeadRegistrationNote(deadStatus(cmd))
		if n == nil {
			t.Fatalf("%q: no note", cmd)
		}
		if got, want := n.Evidence[0].Snippet, "missing hook command: "+cmd; got != want {
			t.Errorf("snippet:\n  got  %q\n  want %q", got, want)
		}
	}
}

// TestStatusDescribe_ShowsTheRegisteredCommandVerbatim pins the other half of the decision:
// `aguard hook status` is not a report. It is the operator asking their own terminal which command
// is registered, and on the "THAT FILE DOES NOT EXIST" line the path IS the answer. The redactor
// masks real install paths — an npx cache path, the one an ephemeral install leaves behind when it
// goes dead, is the case pinned here — so the status output prints the command as written.
func TestStatusDescribe_ShowsTheRegisteredCommandVerbatim(t *testing.T) {
	cmd := "/Users/u/.npm/_npx/6b3c7a1e2f4d5c6b/node_modules/@basdotio/aguard-darwin-arm64/bin/aguard hook"
	if detect.Redact(cmd) == cmd {
		t.Fatal("fixture: this path must be one the redactor changes, or the test proves nothing")
	}
	var b bytes.Buffer
	deadStatus(cmd).Describe(&b)
	if !strings.Contains(b.String(), cmd) {
		t.Errorf("hook status must print the registered command as written:\n%s", b.String())
	}
}
