// SPDX-License-Identifier: MIT

package scrub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlaceholdersReplaceEveryFormOfAPrefix: a committed ledger carried 1,669 copies of
// a maintainer's home directory and the judge raw/ carried thousands of per-user temp paths.
// Both are a fixed prefix per run. The scrubber swaps each for a stable placeholder — in the
// form the operator gave AND in the symlink-resolved form the OS may hand back (macOS turns
// /var/folders/… into /private/var/folders/…) — and longer prefixes win, so a work directory
// created under the temp directory reads as <work>, never as <tmp>/baseline-123.
func TestPlaceholdersReplaceEveryFormOfAPrefix(t *testing.T) {
	tmp := t.TempDir()
	work := filepath.Join(tmp, "baseline-123")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	s := New(map[string]string{"<tmp>": tmp, "<work>": work, "<corpus>": "/Users/someone/src/corpus"})

	cases := map[string]string{
		work + "/sample/home/.claude":                         "<work>/sample/home/.claude",
		resolved + "/sample/home/.claude":                     "<work>/sample/home/.claude",
		tmp + "/other":                                        "<tmp>/other",
		"/Users/someone/src/corpus/corpus/benign/x — refused": "<corpus>/corpus/benign/x — refused",
		"nothing to do here":                                  "nothing to do here",
	}
	for in, want := range cases {
		if got := s.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
	// Bytes is the same substitution on a JSON document, in place of re-parsing it.
	doc := []byte(`{"root":"` + work + `/s/home/.claude","path":"` + resolved + `/s/x"}`)
	if got := string(s.Bytes(doc)); strings.Contains(got, tmp) || !strings.Contains(got, `"<work>/s/home/.claude"`) {
		t.Errorf("Bytes left a prefix behind: %s", got)
	}
	// An empty path is not a prefix of everything.
	if got := New(map[string]string{"<home>": ""}).String("/x/y"); got != "/x/y" {
		t.Errorf("an empty prefix rewrote %q", got)
	}
}
