// SPDX-License-Identifier: MIT
package detect

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestClip_CutsOnACharacterBoundary (P-037): clip bounds every static snippet, and the judge's triage
// sends snippets as evidence. It cut at byte 200 wherever that fell, so a line of CJK or emoji past the
// cap ended in half a character — invalid UTF-8 that a report prints as a broken glyph and the judge's
// request carries as U+FFFD. ASCII input is cut exactly as before.
func TestClip_CutsOnACharacterBoundary(t *testing.T) {
	for _, run := range []string{"中", "😀"} {
		for k := 0; k < utf8.UTFMax; k++ {
			s := strings.Repeat("a", k) + strings.Repeat(run, 120)
			got := clip(s)
			if !utf8.ValidString(got) {
				t.Errorf("%q, offset %d: clip ends inside a character: %q", run, k, got[len(got)-8:])
			}
			if !strings.HasSuffix(got, "…") || len(got) > 200+len("…") {
				t.Errorf("%q, offset %d: clip = %d bytes, want at most 200 and the mark", run, k, len(got))
			}
			if body := strings.TrimSuffix(got, "…"); len(body) < 200-utf8.UTFMax+1 || !strings.HasPrefix(s, body) {
				t.Errorf("%q, offset %d: clip kept %d bytes, want the longest prefix that ends on a character", run, k, len(body))
			}
		}
	}
	ascii := strings.Repeat("abcdefghij", 30)
	if got, want := clip(ascii), ascii[:200]+"…"; got != want {
		t.Errorf("ASCII: clip = %q, want %q", got, want)
	}
	if got := clip("short"); got != "short" {
		t.Errorf("under the cap: clip = %q, want it unchanged", got)
	}
}
