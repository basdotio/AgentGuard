// SPDX-License-Identifier: MIT
package clean

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/collect"
	"github.com/basdotio/agent-guard/internal/safeio"
)

// The manifest is what makes a quarantine reversible. Before it existed, recovery was a `mv` line
// printed at the end of a run — reconstructed from a naming convention, so it was simply WRONG for a
// symlink-installed skill (whose real path is not skills/<name>) and for the second quarantine of a
// same-named skill (which lands at <name>.1). A process killed between the rename and the print left
// no record at all.
//
// Two properties are load-bearing and neither is obvious:
//
//   - WRITE-AHEAD. The intent is recorded before the move and confirmed after. A crash in between
//     leaves an `intended` row with no `done`, which the next run reports rather than swallowing:
//     "the file is in the trash and nothing knows how to put it back" is exactly the failure the
//     manifest exists to prevent.
//   - The manifest is DATA, NOT AUTHORITY. It is a plain file inside the user's config root, so
//     anything that can write there can add rows. Undo therefore re-derives every safety decision —
//     containment, exclusion, content identity — instead of trusting what it reads. See Undo.
const (
	manifestName  = "manifest.jsonl"
	lockName      = ".lock"
	stateIntended = "intended"
	stateDone     = "done"
	// stateUndone marks a row whose move has been reversed, so `--undo last` can tell which batch is
	// still outstanding and an undo can itself be audited.
	stateUndone = "undone"
	// stateFailed closes an intent whose move did NOT happen. Without it a failed rename left a bare
	// `intended` row, which pending() cannot distinguish from a crash — so every subsequent run
	// fabricated one more "an earlier run was interrupted" warning about a file that never moved.
	stateFailed = "failed"
)

// Record is one line of the manifest.
type Record struct {
	Batch   string `json:"batch"`
	Item    string `json:"item"`  // CleanItem ID, so a row ties back to what the operator saw
	Kind    string `json:"kind"`  // hygiene kind (zombie, …)
	State   string `json:"state"` // intended | done | undone
	Name    string `json:"name"`
	From    string `json:"from"` // absolute source, recorded rather than reconstructed
	To      string `json:"to"`   // absolute destination inside the trash
	Hash    string `json:"hash"` // content hash at the time of the move
	Unix    int64  `json:"unix"`
	Version string `json:"tool_version"`
	// Prev is the SHA-256 of the previous line's bytes, making the manifest a hash chain.
	//
	// WHAT THIS DETECTS, measured rather than assumed: a row DELETED or EDITED in the middle (the
	// next row's Prev no longer matches), and an unreadable or half-written line, which is the likely
	// non-adversarial case (a truncated write, a crashed editor, a partial sync).
	//
	// WHAT IT DOES NOT DETECT. A chain anchored inside the data it protects cannot notice a change to
	// its own END, because nothing follows to bind it: editing the LAST row is invisible — and combined
	// with replacing the quarantined content it defeats the identity check too, since both sides then
	// agree — and so is emptying the file entirely. What catches the last-row edit is not the chain but
	// the restore PREVIEW (preview.go), which runs the rules over the copy so the payload announces
	// itself on the way past; deleting the last row alone is caught by the write-ahead pairing, since a
	// missing `done` leaves an unconfirmed `intended` that reportPending names.
	//
	// So the honest claim is INTEGRITY, not tamper-proofing, and the README says integrity. Fixing the
	// rest needs the head anchored where an attacker cannot reach, and every option costs a property
	// this tool is chosen for: a second file in root is equally writable, outside root violates clean's
	// own containment rule, a keyring needs platform-specific code, a signature needs somewhere to keep
	// a private key. That trade is not obviously worth making: an attacker who can write .aguard-trash/
	// can already write skills/ directly, so this buys concealment, not privilege.
	//
	// Empty on rows written before the chain existed. Those are reported as unverifiable, never
	// refused — making an upgrade the cause of an unrecoverable batch would be its own data loss.
	Prev string `json:"prev,omitempty"`
}

// genesisPrev is the Prev of the first chained row: a fixed marker rather than "" so a genuine first
// row is distinguishable from a pre-chain row that never had the field.
const genesisPrev = "genesis"

// lineHash is the link value: SHA-256 over the row's serialized bytes, Prev included. Go marshals
// struct fields in declaration order, so the encoding is stable and two runs agree.
func lineHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// trashPath returns the WRITTEN <root>/.aguard-trash path. Callers that are about to write must go
// through safeTrash instead, which resolves it and refuses a symlinked or escaped address; this
// helper exists only for the bookkeeping paths that safeTrash has already validated.
func trashPath(root string) string { return filepath.Join(root, collect.TrashDir) }

// openManifest opens the manifest for appending, refusing anything that is not a regular file.
// os.OpenFile follows symlinks, so `ln -s /outside/victim <trash>/manifest.jsonl` made every apply
// append tool-controlled lines to a file outside the scanned root — and pointed the recovery record
// at storage the operator does not control.
func openManifest(dir string) (*os.File, error) {
	p := filepath.Join(dir, manifestName)
	if fi, err := os.Lstat(p); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to use %s: it is not a regular file (mode %s); "+
			"the quarantine record must not be redirected elsewhere", p, fi.Mode())
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// lock takes an exclusive hold on the trash directory for the duration of a mutating command.
//
// Deliberately an O_EXCL lockfile rather than flock: this binary is cross-compiled for Windows with
// CGO disabled, and a Unix-only advisory lock would silently degrade to no lock at all there — the
// worst outcome, since the failure it guards against (two concurrent applies computing the same free
// trash path and one overwriting the other) is silent data loss.
func lock(root string) (release func(), err error) {
	dir := trashPath(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create trash dir: %w", err)
	}
	p := filepath.Join(dir, lockName)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			held, _ := os.ReadFile(p)
			return nil, fmt.Errorf("another aguard clean is running (lock held since %s).\n"+
				"If no other run is active, remove the stale lock:\n  rm %s",
				strings.TrimSpace(string(held)), p)
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "pid=%d at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	_ = f.Close()
	return func() { _ = os.Remove(p) }, nil
}

// appendRecord writes one manifest row, flushing to disk before returning so a crash immediately
// after cannot lose the intent that was just recorded.
func appendRecord(root string, r Record) error {
	dir := trashPath(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// The link value comes from the file's CURRENT last line, read under the same lock every mutating
	// command holds — so two rows can never claim the same predecessor.
	prev, perr := lastLineHash(dir)
	if perr != nil {
		return perr
	}
	r.Prev = prev

	f, err := openManifest(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// lastLineHash returns the link value a new row must carry: the hash of the file's last line, or
// genesisPrev for a new file.
func lastLineHash(dir string) (string, error) {
	b, err := safeio.ReadFile(filepath.Join(dir, manifestName), 64<<20)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return genesisPrev, nil
		}
		return "", err
	}
	lines := splitLines(b)
	if len(lines) == 0 {
		return genesisPrev, nil
	}
	return lineHash([]byte(lines[len(lines)-1])), nil
}

// splitLines returns the non-blank lines of a manifest, preserving order and exact bytes — the chain
// is computed over the bytes as written, so trimming anything here would break verification.
func splitLines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// ChainStatus is what verification concluded about a manifest's integrity.
type ChainStatus struct {
	// BrokenAt is the 1-based line whose Prev did not match its predecessor, or 0 when the chain holds.
	BrokenAt int
	// Unchained counts rows written before the chain existed (no Prev field). Not an error.
	Unchained int
	// Corrupt counts lines that did not parse as JSON at all.
	Corrupt int
}

// OK reports whether the chain verified end to end.
func (c ChainStatus) OK() bool { return c.BrokenAt == 0 }

// verifyChain recomputes the chain over the manifest as written.
//
// Reads the RAW BYTES rather than re-marshalling the parsed rows: a link is defined over what is on
// disk, and re-serializing would silently repair a difference (a reordered key, an extra space) that
// is precisely what verification is supposed to notice.
func verifyChain(root string) (ChainStatus, error) {
	var st ChainStatus
	b, err := safeio.ReadFile(filepath.Join(trashPath(root), manifestName), 64<<20)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	expect := genesisPrev
	for i, line := range splitLines(b) {
		var r Record
		if json.Unmarshal([]byte(line), &r) != nil {
			st.Corrupt++
			// A corrupt line cannot supply a link value, so verification of everything after it is
			// meaningless — stop rather than report a cascade of breaks caused by one bad line.
			st.BrokenAt = i + 1
			return st, nil
		}
		if r.Prev == "" {
			st.Unchained++
			expect = lineHash([]byte(line))
			continue
		}
		if r.Prev != expect && st.BrokenAt == 0 {
			st.BrokenAt = i + 1
			return st, nil
		}
		expect = lineHash([]byte(line))
	}
	return st, nil
}

// readManifest returns every row, oldest first. A corrupt line is skipped rather than aborting: a
// half-written final row (killed mid-append) must not make the rest of the history unreadable.
func readManifest(root string) ([]Record, error) {
	mp := filepath.Join(trashPath(root), manifestName)
	if fi, lerr := os.Lstat(mp); lerr == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to read %s: it is not a regular file (mode %s)", mp, fi.Mode())
	}
	b, err := os.ReadFile(mp)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Record
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// batchID derives a deterministic batch name from the run's start time and item count. Not random:
// this package must stay testable, and a collision only matters within one trash directory.
func batchID(now time.Time, n int) string {
	return "b" + now.UTC().Format("20060102T150405") + "-" + strconv.Itoa(n)
}

// pending returns rows recorded as intended but never confirmed done — a move interrupted between
// the write-ahead record and its confirmation.
func pending(recs []Record) []Record {
	confirmed := map[string]bool{}
	for _, r := range recs {
		if r.State == stateDone || r.State == stateUndone || r.State == stateFailed {
			confirmed[r.Batch+"\x00"+r.Item] = true
		}
	}
	var out []Record
	for _, r := range recs {
		if r.State == stateIntended && !confirmed[r.Batch+"\x00"+r.Item] {
			out = append(out, r)
		}
	}
	return out
}

// restorable returns the rows of a batch that are done and not yet undone. "last" means the most
// recent batch that still has something to reverse — a batch already undone is not the answer to
// "undo the last thing I did".
func restorable(recs []Record, batch string) (string, []Record) {
	undone := map[string]bool{}
	for _, r := range recs {
		if r.State == stateUndone {
			undone[r.Batch+"\x00"+r.Item] = true
		}
	}
	byBatch := map[string][]Record{}
	var order []string
	for _, r := range recs {
		if r.State != stateDone || undone[r.Batch+"\x00"+r.Item] {
			continue
		}
		if _, seen := byBatch[r.Batch]; !seen {
			order = append(order, r.Batch)
		}
		byBatch[r.Batch] = append(byBatch[r.Batch], r)
	}
	if batch != "" && batch != "last" {
		return batch, byBatch[batch]
	}
	if len(order) == 0 {
		return "", nil
	}
	last := order[len(order)-1]
	return last, byBatch[last]
}
