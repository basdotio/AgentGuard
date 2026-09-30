// SPDX-License-Identifier: MIT
package safeio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// within fails the test if fn does not return in d. The regression this package guards against
// is a HANG, and a hung test reports nothing — so every FIFO case runs under a clock.
func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not return within %s — the read blocked", d)
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
}

// TestReadFile_RefusesFIFOWithoutBlocking: os.Open on a FIFO with no writer blocks forever;
// this must return ErrNotRegular before any Open.
func TestReadFile_RefusesFIFOWithoutBlocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	mkfifo(t, p)
	within(t, 3*time.Second, func() {
		for _, try := range []func() error{
			func() error { _, err := ReadFile(p, MaxConfigBytes); return err },
			func() error { _, err := ReadPrefix(p, 1024); return err },
			func() error { _, err := Open(p); return err },
			func() error { _, err := Stat(p); return err },
		} {
			if err := try(); !errors.Is(err, ErrNotRegular) {
				t.Errorf("FIFO must be ErrNotRegular, got %v", err)
			}
		}
	})
	if _, err := ReadFile(t.TempDir(), MaxConfigBytes); !errors.Is(err, ErrNotRegular) {
		t.Errorf("a directory must be ErrNotRegular, got %v", err)
	}
}

// TestReadFile_CapIsEnforcedByTheRead: over the cap is ErrTooLarge with NO bytes returned,
// and the message names the cap. ReadPrefix returns exactly the prefix.
func TestReadFile_CapIsEnforcedByTheRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 4096)), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := ReadFile(p, 1024)
	if !errors.Is(err, ErrTooLarge) || len(b) != 0 {
		t.Errorf("ReadFile over cap: err=%v len=%d, want ErrTooLarge and no bytes", err, len(b))
	}
	if b, err := ReadFile(p, 4096); err != nil || len(b) != 4096 {
		t.Errorf("ReadFile at cap: err=%v len=%d", err, len(b))
	}
	if b, err := ReadPrefix(p, 100); err != nil || len(b) != 100 {
		t.Errorf("ReadPrefix: err=%v len=%d, want 100", err, len(b))
	}
}

// TestReadFile_FollowsSymlinkToRegularFile is the reverse assertion for invariant #2's
// supported layout: a symlink to a real file is read (Stat, not Lstat). Boundary checks are
// the callers' job, not this package's.
func TestReadFile_FollowsSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	b, err := ReadFile(link, MaxConfigBytes)
	if err != nil || string(b) != `{"ok":true}` {
		t.Errorf("symlink to a regular file must be read: %v %q", err, b)
	}
	if _, err := ReadFile(filepath.Join(dir, "nope"), 10); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file must surface as ErrNotExist for callers that treat absence as normal, got %v", err)
	}
}
