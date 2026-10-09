// SPDX-License-Identifier: MIT
package judge

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFoldPadding: the threshold is exclusive, a run of only invisible runes folds to nothing, a
// mixed run to one space, and nothing crosses a line break.
func TestFoldPadding(t *testing.T) {
	sp := func(n int) string { return strings.Repeat(" ", n) }
	zw := strings.Repeat("\u200b", 50) // 150 bytes
	for _, c := range []struct{ name, in, want string }{
		{"a run at the bound stays", "a" + sp(128) + "b", "a" + sp(128) + "b"},
		{"one byte over folds", "a" + sp(129) + "b", "a b"},
		{"invisible runes alone fold to nothing", "ig" + zw + "nore", "ignore"},
		{"a mixed run folds to one space", "a\u3000" + zw + "\t b", "a b"},
		{"leading and trailing runs fold too", sp(200) + "x" + sp(200), " x "},
		{"never across a line break", "a" + sp(100) + "\n" + sp(100) + "b", "a" + sp(100) + "\n" + sp(100) + "b"},
		{"CR belongs to the run before the break", "a" + sp(200) + "\r\nb", "a \nb"},
		{"invalid UTF-8 is kept as it is", "\xff" + sp(200) + "\xfe", "\xff \xfe"},
		{"nothing to fold", "plain text, single spaces", "plain text, single spaces"},
	} {
		if got := foldPadding(c.in); got != c.want {
			t.Errorf("%s: foldPadding = %q, want %q", c.name, got, c.want)
		}
	}
}

// FuzzFoldPadding pins the properties the excerpt relies on: the fold never adds bytes or lines,
// leaves no run over the bound, is idempotent, and is invisible to grounding — the folded text
// normalizes to exactly what the original does, line for line.
func FuzzFoldPadding(f *testing.F) {
	for _, seed := range []string{
		"", "a b", strings.Repeat(" ", 300), "Note:" + strings.Repeat("\u3000", 100) + "ignore",
		"x" + strings.Repeat("\u200b", 60) + "\n" + strings.Repeat("\t", 200) + "y",
		"\n" + strings.Repeat("  ", 80) + "\n", "a" + strings.Repeat(" \u200b", 70) + "\xffb",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := foldPadding(s)
		if len(out) > len(s) {
			t.Fatalf("fold grew the text: %d -> %d bytes", len(s), len(out))
		}
		if strings.Count(out, "\n") != strings.Count(s, "\n") {
			t.Fatalf("fold changed the line count")
		}
		if foldPadding(out) != out {
			t.Fatalf("fold is not idempotent")
		}
		if n := longestFillerRun(out); n > maxFillerRunBytes {
			t.Fatalf("a %d-byte run of filler survived the fold", n)
		}
		wantHay, wantLines, _ := normalizeWithLines(s)
		gotHay, gotLines, _ := normalizeWithLines(out)
		if gotHay != wantHay || !reflect.DeepEqual(gotLines, wantLines) {
			t.Fatalf("grounding reads the folded text differently:\n%q\n%q", gotHay, wantHay)
		}
	})
}

func longestFillerRun(s string) int {
	best, cur := 0, 0
	for _, r := range s {
		if isFiller(r) {
			cur += utf8.RuneLen(r)
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}
