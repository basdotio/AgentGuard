// SPDX-License-Identifier: MIT
package clean

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifestPath(root string) string {
	return filepath.Join(root, TrashDirForTest, "manifest.jsonl")
}

func readLines(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return splitLines(b)
}

func writeLines(t *testing.T, root string, lines []string) {
	t.Helper()
	if err := os.WriteFile(manifestPath(root), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The chain must be silent when it holds. A line printed on every run is a line nobody reads, which
// would defeat the point of printing anything at all.
func TestChain_HoldsOnAnUntouchedManifest(t *testing.T) {
	root := quarantineOnce(t)
	st, err := verifyChain(root)
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK() || st.Unchained != 0 || st.Corrupt != 0 {
		t.Errorf("an untouched manifest must verify clean; got %+v", st)
	}
	// And the first row must be distinguishable from a pre-chain row: "" means unchained, so a genuine
	// first row carries a marker instead.
	var first Record
	if err := json.Unmarshal([]byte(readLines(t, root)[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Prev != genesisPrev {
		t.Errorf("the first row's Prev = %q, want %q", first.Prev, genesisPrev)
	}
}

// The failure this was built for. Emptying or trimming the record used to leave a skill in the trash
// with `--undo` reporting "no quarantine batch is outstanding" — the exact loss the manifest exists to
// prevent, delivered in silence.
func TestChain_DetectsADeletedRow(t *testing.T) {
	root := quarantineOnce(t)
	lines := readLines(t, root)
	if len(lines) < 2 {
		t.Fatalf("need at least two rows to delete one; got %d", len(lines))
	}
	writeLines(t, root, lines[1:]) // drop the first
	st, err := verifyChain(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.OK() {
		t.Error("deleting a row must break the chain")
	}
}

func TestChain_DetectsAnEditedRow(t *testing.T) {
	root := quarantineOnce(t)
	lines := readLines(t, root)
	var r Record
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatal(err)
	}
	r.Hash = strings.Repeat("0", 64)
	edited, _ := json.Marshal(r)
	writeLines(t, root, append([]string{string(edited)}, lines[1:]...))
	if st, _ := verifyChain(root); st.OK() {
		t.Error("editing a row must break the chain")
	}
}

// KNOWN LIMITS, asserted so they cannot be quietly believed away. A chain anchored inside the data it
// protects cannot notice a change to its own end. These are inverted assertions in the same spirit as
// the adversarial corpus: if a future change starts detecting these, this test fails and the honest
// claim in the README gets upgraded with it.
func TestChain_CannotDetectEndOfFileTampering(t *testing.T) {
	t.Run("editing the last row is invisible", func(t *testing.T) {
		root := quarantineOnce(t)
		lines := readLines(t, root)
		var r Record
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil {
			t.Fatal(err)
		}
		r.Hash = strings.Repeat("a", 64)
		edited, _ := json.Marshal(r)
		writeLines(t, root, append(lines[:len(lines)-1], string(edited)))
		if st, _ := verifyChain(root); !st.OK() {
			t.Error("chain now detects a last-row edit — upgrade the README's claim and delete this row")
		}
	})
	t.Run("emptying the file is invisible", func(t *testing.T) {
		root := quarantineOnce(t)
		writeLines(t, root, nil)
		if st, _ := verifyChain(root); !st.OK() {
			t.Error("chain now detects an emptied manifest — upgrade the claim and delete this row")
		}
	})
}

// A pre-chain row must be reported as unverifiable and STILL be restorable. Making an upgrade the
// cause of an unrecoverable batch would be its own data loss, worse than the gap it closes.
func TestChain_PreChainRowsAreUnverifiableNotRefused(t *testing.T) {
	root := quarantineOnce(t)
	var stripped []string
	for _, l := range readLines(t, root) {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		delete(m, "prev")
		b, _ := json.Marshal(m)
		stripped = append(stripped, string(b))
	}
	writeLines(t, root, stripped)

	st, err := verifyChain(root)
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK() {
		t.Errorf("rows without Prev are unverifiable, not broken; got %+v", st)
	}
	if st.Unchained != len(stripped) {
		t.Errorf("Unchained = %d, want %d", st.Unchained, len(stripped))
	}
	// The load-bearing half: they still restore.
	var buf strings.Builder
	res, uerr := Undo(&buf, root, "last", false)
	if uerr != nil {
		t.Fatal(uerr)
	}
	if len(res.Actions) != 1 || !res.Actions[0].Applied {
		t.Errorf("a pre-chain batch must still be restorable; got %+v (%s)", res, buf.String())
	}
	if !strings.Contains(buf.String(), "predate the hash chain") {
		t.Errorf("the operator must be told the rows are unverifiable; got %q", buf.String())
	}
}

// A break must not block the restore. The file is already out of skills/, so refusing would guarantee
// the loss rather than prevent it — safety comes from the five re-derived checks, and the chain's job
// is to make the operator read the preview with suspicion.
func TestChain_BreakWarnsButDoesNotBlock(t *testing.T) {
	root := quarantineOnce(t)
	lines := readLines(t, root)
	var r Record
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil {
		t.Fatal(err)
	}
	r.Unix += 1
	edited, _ := json.Marshal(r)
	writeLines(t, root, append([]string{string(edited)}, lines[1:]...))

	var buf strings.Builder
	res, err := Undo(&buf, root, "last", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hash chain breaks") {
		t.Errorf("a break must be reported; got %q", buf.String())
	}
	if len(res.Actions) != 1 {
		t.Errorf("a break must not block the restore; got %+v", res)
	}
}
