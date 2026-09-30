// SPDX-License-Identifier: MIT
package collect

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// sumFile streams a file into sha256 rather than reading it whole. The bytes hashed here
// are attacker-chosen — a skill can ship a file of any size — and `os.ReadFile` sized the
// allocation from that file, so a multi-GB blob dropped into a skill directory took the
// scanner down with an OOM instead of producing a verdict. A scanner that can be killed by
// the thing it is scanning is not a control. io.Copy holds a fixed 32 KiB window, so the
// cost of a file is now its read time, not its size.
//
// The digest is unchanged: sha256 over the same bytes, chunked or not, is the same 32 bytes.
// That matters more than it looks — this value is the reputation-list key AND the key the
// load-time gate stores approvals under, so a changed digest would silently invalidate every
// seeded allowlist entry and re-open every skill the user already approved.
//
// Non-regular files are skipped rather than opened. The size problem has a twin: `os.Open` on
// a FIFO blocks until someone writes, so a named pipe planted in a skill directory hung the
// scan forever — a quieter failure than the OOM and a worse one. Stat (which follows symlinks,
// as this walk intends) answers that before the open. Symlinks to real files are still
// followed and hashed; containment is `withinDir`'s job, not this function's.
//
// MERGE NOTE, load-bearing: FileHash and TreeHash below were exported on one branch (clean
// re-derives hashes when it quarantines and restores) while this hardening landed on another.
// The exported names survive; the os.ReadFile bodies they carried must not — resolving that
// conflict "theirs" would have silently reverted the OOM and FIFO fixes. Everything routes
// through this function.
func sumFile(path string) ([32]byte, bool) {
	var zero [32]byte
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return zero, false
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, false
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, false
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, true
}

// FileHash returns the sha256 of a single file's bytes (hex).
//
// Exported for internal/clean, which re-derives an artifact's identity from the filesystem
// when it quarantines and restores content, instead of trusting its own records.
func FileHash(path string) string {
	sum, ok := sumFile(path)
	if !ok {
		return ""
	}
	return hex.EncodeToString(sum[:])
}

// TreeHash returns a canonical hash over a directory: every regular file's relative
// path + content is folded in, sorted by path, so the digest is stable regardless of
// walk order (spec §8 — the risk lives in the scripts, not just SKILL.md). Files whose
// resolved target escapes `boundary` (the skill's own root) are skipped (spec §16.2).
// Returns "" on error.
func TreeHash(boundary, dir string) string {
	type entry struct {
		rel string
		sum [32]byte
	}
	var entries []entry
	// unreadableMark stands in for the content of an entry that exists but cannot be read
	// (a 0111 directory, a 000 file). Folding the entry's PRESENCE in, rather than dropping
	// it, is what keeps "a tree with an unreadable X" and "a tree with no X" on different
	// hashes. This hash is the key of every reputation entry and every gate approval, and the
	// gate's whole design rests on "any edit re-asks by itself"; an approval recorded against
	// the tree without X must not also cover the tree that hides X behind a mode bit.
	// Readable trees are unaffected (TestHashGolden pins that), and no tree containing an
	// unreadable entry ever had a stable hash to break — it hashed as if the entry were absent.
	unreadableMark := sha256.Sum256([]byte("aguard:unreadable-entry"))
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p != dir {
				rel, _ := filepath.Rel(dir, p)
				entries = append(entries, entry{rel: rel, sum: unreadableMark})
			}
			return nil
		}
		if d.IsDir() {
			if ExcludedFromHash(d.Name()) {
				return filepath.SkipDir // vendored/generated/VCS: not authored content, and volatile
			}
			return nil
		}
		if !withinDir(boundary, p) {
			return nil // don't read files whose target escapes the skill root
		}
		sum, ok := sumFile(p)
		if !ok {
			// Regular file that could not be opened (or a non-regular entry): present, unread.
			sum = unreadableMark
		}
		rel, _ := filepath.Rel(dir, p)
		entries = append(entries, entry{rel: rel, sum: sum})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}
