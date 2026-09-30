// SPDX-License-Identifier: MIT
package gate

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCorruptStoreAsksRatherThanAllows is the one place this package deliberately fails OPEN
// in the other direction. The two failure modes are not symmetric: forgetting approvals costs
// prompts, honouring a store we cannot parse hands out a silent allow.
func TestCorruptStoreAsksRatherThanAllows(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"approvals":`,           // truncated mid-write
		`{"version":99,"approvals":{"a":{}}}`, // a format from the future
		`not json at all`,
	} {
		dir := t.TempDir()
		p := ApprovalsPath(dir)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		s := LoadStore(p)
		if s.Corrupt == "" {
			t.Errorf("%q loaded silently", body)
		}
		if _, ok := s.Approved("a"); ok {
			t.Errorf("%q produced an approval", body)
		}
		if err := s.Save(); err == nil {
			t.Errorf("%q was overwritten — the operator loses what could still be recovered", body)
		}
	}
}

// TestKeyMustMatchItsOwnHash: a row whose map key disagrees with the hash inside it was
// written by something that did not understand the format. Trusting it would let a key rename
// approve content nobody read.
func TestKeyMustMatchItsOwnHash(t *testing.T) {
	dir := t.TempDir()
	p := ApprovalsPath(dir)
	body := `{"version":1,"approvals":{"forged":{"hash":"real","name":"x"},"real":{"hash":"real","name":"x"}}}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(p)
	if _, ok := s.Approved("forged"); ok {
		t.Error("a key that does not match its own record was honoured")
	}
	if _, ok := s.Approved("real"); !ok {
		t.Error("a consistent row was dropped")
	}
}

// TestEmptyHashIsNeverApproved: a target the collector could not hash is a target nobody
// audited, so it must not be silenced by an empty key sitting in the store.
func TestEmptyHashIsNeverApproved(t *testing.T) {
	s := LoadStore(ApprovalsPath(t.TempDir()))
	s.Approve(Approval{Hash: "", Name: "ghost"})
	if len(s.Approvals) != 0 {
		t.Fatal("an empty hash was recorded")
	}
	if _, ok := s.Approved(""); ok {
		t.Error("an empty hash matched")
	}
}

// TestSaveIsAtomicAndPrivate: this file is written from a hook that fires on every skill
// load, so an interrupted write must leave the previous store intact — and per LoadStore a
// truncated one would discard every approval the operator has made.
func TestSaveIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	p := ApprovalsPath(dir)
	s := LoadStore(p)
	s.Approve(Approval{Hash: "h1", Name: "a"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600: this file decides what loads without asking", fi.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if _, ok := LoadStore(p).Approved("h1"); !ok {
		t.Error("round-trip lost the approval")
	}
}

// TestPendingExpires bounds a file that is written on every risky load.
func TestPendingExpires(t *testing.T) {
	s := LoadStore(ApprovalsPath(t.TempDir()))
	s.pend("old", Verdict{Hash: "h1"}, 1000)
	s.pend("new", Verdict{Hash: "h2"}, 1000+pendingTTL+1)
	if _, ok := s.pendingFor("old"); ok {
		t.Error("an expired pending verdict survived")
	}
	if _, ok := s.pendingFor("new"); !ok {
		t.Error("a fresh pending verdict was pruned")
	}
}

// TestApprovalsFileNameMatchesTheCollectorsExemption keeps two constants in step: collect
// excludes this name from its unowned-entries note by literal, because it must not import
// the gate (the gate is built on collect).
func TestApprovalsFileNameMatchesTheCollectorsExemption(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "collect", "unowned.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `aguardApprovalsFile = "`+ApprovalsFile+`"`) {
		t.Errorf("collect/unowned.go does not exempt %q — the tool's own state would be "+
			"reported as an unread entry in the user's environment", ApprovalsFile)
	}
	// Same for the backup `hook install` writes: it appears on every scan of a machine where
	// the gate is installed, which is precisely where the note must stay worth reading.
	bak := filepath.Base(SettingsPath("x")) + backupSuffix
	if !strings.Contains(string(b), `aguardSettingsBackup = "`+bak+`"`) {
		t.Errorf("collect/unowned.go does not exempt %q", bak)
	}
}

// TestLoadStore_FIFOReadsAsCorruptNotHang: the designed defence ("a store we cannot parse
// reads as empty") worked for garbage bytes and was bypassed by a file that BLOCKS on open —
// same file, same threat, opposite outcome. Under a clock: the regression is a hang.
func TestLoadStore_FIFOReadsAsCorruptNotHang(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".aguard-approvals.json")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan *Store, 1)
	go func() { done <- LoadStore(p) }()
	select {
	case s := <-done:
		if s.Corrupt == "" || len(s.Approvals) != 0 {
			t.Errorf("a FIFO store must read as corrupt-and-empty (every artifact re-asked), got corrupt=%q n=%d", s.Corrupt, len(s.Approvals))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadStore blocked on a FIFO — the gate would hang past the editor's timeout with zero output")
	}
	// The same shape for the settings file the installer reads: refused, not opened.
	if _, err := readSettings(p); err == nil {
		t.Error("readSettings must refuse a FIFO instead of blocking or treating it as empty")
	}
}
