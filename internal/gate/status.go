// SPDX-License-Identifier: MIT
package gate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// EventStatus is one registered (or missing) hook entry.
type EventStatus struct {
	Event   string // "PreToolUse[Skill]"
	Command string // "" when nothing is registered for it
	// Broken is set when a command IS registered but the binary it names is gone. This is
	// the failure this whole command exists for: settings.json still says the gate is on,
	// nothing runs, and the resulting silence is indistinguishable from "all clear".
	Broken bool
	// Foreign marks an aguard registration at a DIFFERENT path than the running binary —
	// usually a stale entry from a build directory or an older install.
	Foreign bool
}

// Status describes whether the gate is actually wired up.
type Status struct {
	SettingsPath  string
	Exe           string
	Events        []EventStatus
	ApprovalsPath string
	Approvals     int
	StoreProblem  string
}

// Active reports whether every event is registered and runnable.
func (s Status) Active() bool {
	if len(s.Events) == 0 {
		return false
	}
	for _, e := range s.Events {
		if e.Command == "" || e.Broken {
			return false
		}
	}
	return true
}

// CheckStatus inspects settings.json and the approvals store.
//
// It exists because the registration bakes in an ABSOLUTE path, and a path can stop
// resolving — the binary is moved, the build directory is cleaned, dotfiles are synced to a
// machine that never had it. When that happens Claude Code runs a command that is not there,
// every load sails through, and the operator sees exactly what a clean environment looks
// like. Nothing else in this tool reports it, so there had to be somewhere to ask.
func CheckStatus(root, exe string) (Status, error) {
	st := Status{SettingsPath: SettingsPath(root), Exe: exe, ApprovalsPath: ApprovalsPath(root)}
	doc, err := readSettings(st.SettingsPath)
	if err != nil {
		return st, err
	}
	hooks, _ := doc["hooks"].(map[string]any)
	want := HookCommand(exe)

	for _, ge := range gateEvents {
		label := ge.event
		if ge.matcher != "" {
			label += "[" + ge.matcher + "]"
		}
		es := EventStatus{Event: label}
		for _, cmd := range commandsFor(hooks[ge.event], ge.matcher) {
			if !looksLikeAguard(cmd) {
				continue
			}
			es.Command = cmd
			es.Foreign = cmd != want
			es.Broken = !binaryExists(cmd)
			break
		}
		st.Events = append(st.Events, es)
	}

	store := LoadStore(st.ApprovalsPath)
	st.StoreProblem = store.Corrupt
	st.Approvals = len(store.Approvals)
	return st, nil
}

// commandsFor returns the command strings registered for one event and matcher.
func commandsFor(entries any, matcher string) []string {
	list, _ := entries.([]any)
	var out []string
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if existing, _ := m["matcher"].(string); existing != matcher {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if c, _ := hm["command"].(string); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

// looksLikeAguard recognises a registration as ours by the binary's NAME plus the
// subcommand. Matching the full command line would report a gate installed from a different
// path as "not installed", which is the more confusing of the two wrong answers: the entry
// is right there in the file the operator is about to open.
func looksLikeAguard(cmd string) bool {
	exe, rest := splitCommand(cmd)
	return strings.HasPrefix(filepath.Base(exe), "aguard") && strings.Contains(rest, hookSubcommand)
}

// binaryExists reports whether the command's executable is still on disk.
func binaryExists(cmd string) bool {
	exe, _ := splitCommand(cmd)
	if exe == "" {
		return false
	}
	fi, err := os.Stat(exe)
	return err == nil && !fi.IsDir()
}

// splitCommand separates the executable from its arguments, honouring the one quoting form
// HookCommand emits (a fully quoted path, for a binary living under a directory with spaces).
func splitCommand(cmd string) (exe, rest string) {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			return strings.ReplaceAll(cmd[1:i+1], `\"`, `"`), cmd[i+2:]
		}
		return "", cmd
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i], cmd[i+1:]
	}
	return cmd, ""
}

// Describe writes the human report.
func (s Status) Describe(w io.Writer) {
	fmt.Fprintf(w, "settings:    %s\nthis binary: %s\n\n", s.SettingsPath, s.Exe)
	for _, e := range s.Events {
		switch {
		case e.Command == "":
			fmt.Fprintf(w, "  ✗ %-22s not registered\n", e.Event)
		case e.Broken:
			fmt.Fprintf(w, "  ⚠ %-22s %s\n      THAT FILE DOES NOT EXIST — this event runs nothing.\n", e.Event, e.Command)
		case e.Foreign:
			fmt.Fprintf(w, "  ✓ %-22s %s\n      (a different aguard than the one you just ran)\n", e.Event, e.Command)
		default:
			fmt.Fprintf(w, "  ✓ %-22s %s\n", e.Event, e.Command)
		}
	}
	fmt.Fprintln(w)
	switch {
	case s.Active():
		fmt.Fprintln(w, "The gate is ACTIVE for sessions started from now on.")
	case s.anyBroken():
		// Named as loudly as possible: this state LOOKS installed and protects nothing.
		fmt.Fprintln(w, "The gate is REGISTERED BUT DEAD — settings.json points at a binary that is gone,")
		fmt.Fprintln(w, "so every skill loads unaudited and the silence looks exactly like a clean result.")
		fmt.Fprintln(w, "Fix: aguard hook install   (re-points it at this binary)")
	default:
		fmt.Fprintln(w, "The gate is NOT active. Install it with: aguard hook install")
	}
	if s.StoreProblem != "" {
		fmt.Fprintf(w, "\napprovals: %s\n", s.StoreProblem)
		return
	}
	fmt.Fprintf(w, "\napprovals: %d recorded (%s)\n", s.Approvals, s.ApprovalsPath)
	if s.Approvals > 0 {
		fmt.Fprintln(w, "           list them with: aguard approvals")
	}
}

func (s Status) anyBroken() bool {
	for _, e := range s.Events {
		if e.Broken {
			return true
		}
	}
	return false
}

// DeadRegistrationNote turns a broken registration into a scan note, or returns nil.
//
// This is the gap `hook status` alone could not close: nobody runs a status command on a
// schedule, but `scan` is the thing people already run. The state it reports LOOKS installed
// and protects nothing — settings.json still names the gate, Claude Code still tries to run
// it, and every skill loads unaudited while the report says the environment is clean. That is
// invariant #5 (no omission stays silent) applied to the gate's own liveness.
//
// Dimension 0, so it never scores and never gates. A dead hook does not make any artifact more
// dangerous; it makes the REPORT less trustworthy, and that is a different claim. Inflating it
// into a scored finding would move a number that has to mean "what the scan found in your
// artifacts", and would put an environment into the High band for a broken symlink.
func DeadRegistrationNote(st Status) *model.Finding {
	var dead []string
	for _, e := range st.Events {
		if e.Broken {
			dead = append(dead, e.Event)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	cmd := ""
	for _, e := range st.Events {
		if e.Broken {
			cmd = e.Command
			break
		}
	}
	return &model.Finding{
		RuleID: "GATE-001", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
		Title: "Load-time gate is registered but cannot run",
		Why: fmt.Sprintf("%s registers AgentGuard's load-time gate for %s, but the command it names does not exist. "+
			"Claude Code runs nothing at those interception points, so every skill loads WITHOUT being audited — "+
			"and the resulting silence is indistinguishable from a clean result. "+
			"Re-point it at this binary with: aguard hook install",
			st.SettingsPath, strings.Join(dead, ", ")),
		Evidence: []model.Evidence{{File: st.SettingsPath, Line: 0, Snippet: "missing hook command: " + cmd}},
	}
}
