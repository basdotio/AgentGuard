// SPDX-License-Identifier: MIT
package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatusReportsTheDeadRegistration is the state this command exists for: settings.json
// still says the gate is on, the binary it names is gone, every skill loads unaudited, and
// the resulting silence is indistinguishable from a clean environment.
func TestStatusReportsTheDeadRegistration(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "aguard")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanInstall(root, HookCommand(exe))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}

	st, err := CheckStatus(root, exe)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Active() {
		t.Fatalf("a fresh install did not read as active: %+v", st.Events)
	}

	// The binary goes away — the exact thing a cleaned build directory or a synced dotfile
	// repo does.
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	st, err = CheckStatus(root, exe)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active() {
		t.Error("a registration pointing at a missing binary read as active")
	}
	var out strings.Builder
	st.Describe(&out)
	if !strings.Contains(out.String(), "DOES NOT EXIST") || !strings.Contains(out.String(), "REGISTERED BUT DEAD") {
		t.Errorf("the dead state was not stated plainly:\n%s", out.String())
	}
}

// TestStatusOnAnUninstalledRoot must not report a problem that is not there.
func TestStatusOnAnUninstalledRoot(t *testing.T) {
	root := t.TempDir()
	st, err := CheckStatus(root, "/opt/aguard")
	if err != nil {
		t.Fatal(err)
	}
	if st.Active() || st.anyBroken() {
		t.Errorf("nothing installed should read as inactive-but-not-broken: %+v", st.Events)
	}
	var out strings.Builder
	st.Describe(&out)
	if !strings.Contains(out.String(), "NOT active") {
		t.Errorf("unclear output:\n%s", out.String())
	}
}

// TestStatusRecognisesAnotherAguard: an install done from a different path is still an
// install. Reporting it as "not installed" would be the more confusing wrong answer — the
// entry is right there in the file the operator is about to open.
func TestStatusRecognisesAnotherAguard(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(t.TempDir(), "aguard")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _ := PlanInstall(root, HookCommand(other))
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	st, err := CheckStatus(root, "/somewhere/else/aguard")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Active() {
		t.Fatal("a working install at another path read as inactive")
	}
	for _, e := range st.Events {
		if !e.Foreign {
			t.Errorf("%s was not marked as a different binary", e.Event)
		}
	}
}

// TestSplitCommandHandlesQuoting covers the one quoting form HookCommand emits.
func TestSplitCommandHandlesQuoting(t *testing.T) {
	cases := map[string]string{
		"/opt/aguard hook":         "/opt/aguard",
		`"/Users/a b/aguard" hook`: "/Users/a b/aguard",
		"/opt/aguard":              "/opt/aguard",
	}
	for in, want := range cases {
		if got, _ := splitCommand(in); got != want {
			t.Errorf("splitCommand(%q) = %q, want %q", in, got, want)
		}
	}
	// A registration for something else entirely must not be claimed as ours.
	if looksLikeAguard("/usr/bin/notify-send hi") {
		t.Error("an unrelated hook was claimed as the gate")
	}
}

// TestDeadRegistrationNote covers the note `scan` attaches, including the case that must stay
// quiet — a working install must not produce a warning on every scan.
func TestDeadRegistrationNote(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "aguard")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _ := PlanInstall(root, HookCommand(exe))
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}

	st, _ := CheckStatus(root, exe)
	if n := DeadRegistrationNote(st); n != nil {
		t.Fatalf("a healthy install produced a warning: %+v", n)
	}

	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	st, _ = CheckStatus(root, exe)
	n := DeadRegistrationNote(st)
	if n == nil {
		t.Fatal("a dead registration produced no note — the exact silence this exists to break")
	}
	if n.Dimension != 0 {
		t.Errorf("dimension = %d, want 0: a broken hook makes the REPORT less trustworthy, "+
			"it does not make any artifact more dangerous", n.Dimension)
	}
	if !strings.Contains(n.Why, "WITHOUT being audited") || !strings.Contains(n.Why, "hook install") {
		t.Errorf("the note does not say what is wrong or how to fix it:\n%s", n.Why)
	}
	// A root with nothing registered must also stay quiet — "not installed" is a choice,
	// not a fault, and nagging about it on every scan is how a warning stops being read.
	st, _ = CheckStatus(t.TempDir(), exe)
	if got := DeadRegistrationNote(st); got != nil {
		t.Errorf("an uninstalled root produced a warning: %+v", got)
	}
}
