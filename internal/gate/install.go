// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/safeio"
)

// hookSubcommand is the argument that turns the binary into a hook runner. ONE command
// serves all three events: the runner dispatches on hook_event_name from stdin.
//
// That is a security decision, not a convenience one. The alternative — a different command
// line per event, or a shell wrapper that pipes into the binary — puts shell metacharacters
// into settings.json, which is precisely the shape HOOK-001 exists to flag. A gate whose own
// installation trips the scanner teaches operators to ignore the scanner.
const hookSubcommand = "hook"

// backupSuffix is appended to settings.json before it is rewritten. Named because
// collect/unowned.go has to exempt the result from its unread-entries note, and a literal in
// two files is a literal that drifts — the gate's test asserts the two agree.
//
// Two slots, because one was a bug: install wrote the original to .aguard-bak, uninstall then
// wrote the post-install file over it, and the operator's ORIGINAL settings were gone after the
// second command. Now .aguard-bak holds the file as it was before aguard ever touched it and is
// never overwritten; .aguard-bak.prev holds the state before the most recent change and is
// overwritten each time. Restoring "what I had before this tool" is always the first one.
const (
	backupSuffix     = ".aguard-bak"
	prevBackupSuffix = ".aguard-bak.prev"
)

// gateEvents are the events the gate registers for, with the tool matcher each needs.
// SessionStart takes no matcher (it is not a tool event).
var gateEvents = []struct{ event, matcher string }{
	{EventPreToolUse, SkillTool},
	{EventPostToolUse, SkillTool},
	{EventSessionStart, ""},
}

// InstallPlan is the outcome of an install/uninstall, reported before anything is written.
type InstallPlan struct {
	SettingsPath string
	Command      string
	Added        []string // "PreToolUse[Skill]"
	Removed      []string
	AlreadyThere []string
	Backup       string // the .prev slot written this run (state before this change)
	Original     string // the .aguard-bak slot, written only the first time it did not exist
	changed      bool
	doc          map[string]any
}

// Changed reports whether the plan would modify the file.
func (p InstallPlan) Changed() bool { return p.changed }

// SettingsPath returns the settings file a root's hooks live in.
func SettingsPath(root string) string { return filepath.Join(root, "settings.json") }

// PlanInstall computes the settings edit that registers the gate, without writing anything.
//
// It merges rather than replaces: the file is the operator's, it very likely holds hooks and
// permissions that have nothing to do with this tool, and a security product that stomps a
// config it does not fully understand has done more damage than the risk it was managing.
// Unknown top-level keys and unknown hook entries are carried through untouched.
//
// Registration is idempotent — an event already carrying this exact command is left alone —
// so running it twice does not fire the gate twice per load.
func PlanInstall(root, command string) (InstallPlan, error) {
	return plan(root, command, true)
}

// PlanUninstall computes the edit that removes the gate's own hook entries. It matches on the
// COMMAND, so hooks the operator added by hand are never removed.
func PlanUninstall(root, command string) (InstallPlan, error) {
	return plan(root, command, false)
}

func plan(root, command string, install bool) (InstallPlan, error) {
	p := InstallPlan{SettingsPath: SettingsPath(root), Command: command}
	doc, err := readSettings(p.SettingsPath)
	if err != nil {
		return p, err
	}
	p.doc = doc

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ge := range gateEvents {
		label := ge.event
		if ge.matcher != "" {
			label += "[" + ge.matcher + "]"
		}
		entries, _ := hooks[ge.event].([]any)
		idx := findEntry(entries, ge.matcher, command)
		switch {
		case install && idx >= 0:
			p.AlreadyThere = append(p.AlreadyThere, label)
		case install:
			entry := map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command}},
			}
			if ge.matcher != "" {
				entry["matcher"] = ge.matcher
			}
			hooks[ge.event] = append(entries, entry)
			p.Added = append(p.Added, label)
			p.changed = true
		case idx >= 0:
			// Remove EVERY occurrence, not the first one findEntry happened to return: a gate
			// registered twice (an older install path, a hand-edit) used to leave one copy
			// behind, so "uninstall" reported success and the hook kept firing. And remove only
			// OUR inner command — an entry that shares this matcher with an operator's own hook
			// keeps that hook; the entry is dropped only when nothing is left in it.
			kept, n := removeCommand(entries, ge.matcher, command)
			if len(kept) == 0 {
				delete(hooks, ge.event)
			} else {
				hooks[ge.event] = kept
			}
			if n > 1 {
				label += fmt.Sprintf(" (×%d)", n)
			}
			p.Removed = append(p.Removed, label)
			p.changed = true
		}
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
	return p, nil
}

// findEntry returns the index of the hook entry for this matcher that runs this command.
func findEntry(entries []any, matcher, command string) int {
	for i, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		existing, _ := m["matcher"].(string)
		if existing != matcher {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if c, _ := hm["command"].(string); c == command {
				return i
			}
		}
	}
	return -1
}

// removeCommand drops every inner hook running command from every entry with this matcher,
// returning the surviving entries and how many commands were removed. Entries left with no
// inner hooks are dropped; entries for other matchers, and other commands under the same
// matcher, are carried through untouched.
func removeCommand(entries []any, matcher, command string) ([]any, int) {
	var kept []any
	removed := 0
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		existing, _ := m["matcher"].(string)
		if existing != matcher {
			kept = append(kept, e)
			continue
		}
		inner, _ := m["hooks"].([]any)
		var innerKept []any
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if ok {
				if c, _ := hm["command"].(string); c == command {
					removed++
					continue
				}
			}
			innerKept = append(innerKept, h)
		}
		if len(innerKept) == 0 {
			continue // nothing of the operator's left in this entry
		}
		if len(innerKept) != len(inner) {
			m2 := map[string]any{}
			for k, v := range m {
				m2[k] = v
			}
			m2["hooks"] = innerKept
			kept = append(kept, m2)
			continue
		}
		kept = append(kept, e)
	}
	return kept, removed
}

// Apply writes the planned settings file, backing up the previous one first.
//
// The backup is not a courtesy: this rewrites a file the editor reads on every start, and
// Go's JSON encoder reorders keys and drops comments, so even a correct edit does not
// round-trip byte-for-byte. An operator who dislikes the result needs the original back, and
// "restore from your dotfiles repo" is not an answer for someone who does not keep one.
func (p *InstallPlan) Apply() error {
	if !p.changed {
		return nil
	}
	if b, err := safeio.ReadFile(p.SettingsPath, safeio.MaxConfigBytes); err == nil {
		orig := p.SettingsPath + backupSuffix
		if _, serr := os.Stat(orig); errors.Is(serr, fs.ErrNotExist) {
			// First time aguard changes this file: this IS the original. Keep it forever.
			if err := os.WriteFile(orig, b, 0o600); err != nil {
				return fmt.Errorf("backing up %s: %w", p.SettingsPath, err)
			}
			p.Original = orig
		}
		prev := p.SettingsPath + prevBackupSuffix
		if err := os.WriteFile(prev, b, 0o600); err != nil {
			return fmt.Errorf("backing up %s: %w", p.SettingsPath, err)
		}
		p.Backup = prev
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := json.MarshalIndent(p.doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(p.SettingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.SettingsPath, out, 0o600)
}

// Describe writes a human summary of the plan.
func (p InstallPlan) Describe(w io.Writer, applied bool) {
	verb := map[bool]string{true: "", false: "would be "}[applied]
	fmt.Fprintf(w, "settings: %s\ncommand:  %s\n", p.SettingsPath, p.Command)
	for _, e := range p.Added {
		fmt.Fprintf(w, "  + %sregistered   %s\n", verb, e)
	}
	for _, e := range p.Removed {
		fmt.Fprintf(w, "  - %sremoved      %s\n", verb, e)
	}
	for _, e := range p.AlreadyThere {
		fmt.Fprintf(w, "  = already registered %s\n", e)
	}
	if !p.changed {
		fmt.Fprintln(w, "nothing to do.")
		return
	}
	if applied && p.Original != "" {
		fmt.Fprintf(w, "original settings (before aguard ever changed them) saved to %s\n", p.Original)
	}
	if applied && p.Backup != "" {
		fmt.Fprintf(w, "settings as they were before this change saved to %s\n", p.Backup)
	}
}

// readSettings parses settings.json into a generic document. A missing file is an empty one.
//
// A file that exists but does not parse is an ERROR, not an empty document: replacing an
// unparseable settings file with a fresh one containing only our hooks would silently delete
// every permission rule and hook the operator has — the worst possible outcome of running a
// command called "install".
func readSettings(path string) (map[string]any, error) {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s does not parse as JSON (%w) — refusing to rewrite it; fix or move it first", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// HookCommand returns the command line to register: this binary's absolute path plus the
// hook subcommand, quoted only if it has to be.
//
// Resolving the running binary matters because the gate is invoked by the editor, whose PATH
// is not the shell's: "aguard hook" works when typed and fails silently as a hook, and a hook
// that fails silently is a gate that is not there.
func HookCommand(exe string) string {
	if strings.ContainsAny(exe, " \t\"'") {
		return `"` + strings.ReplaceAll(exe, `"`, `\"`) + `" ` + hookSubcommand
	}
	return exe + " " + hookSubcommand
}
