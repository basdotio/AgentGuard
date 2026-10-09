// SPDX-License-Identifier: MIT
package judge

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/detect"
)

// egress strips the scanning machine's identity — its home directory, and with it the username —
// from text on its way to the endpoint. Redaction (detect.Redact) removes what LOOKS like a
// secret; nothing in it knows that `/Users/alice/notes` names a person, and a BYO endpoint is a
// third party. The home reached request bodies through content (hook commands, MCP args, CLAUDE.md
// and memory bodies, skill scripts and descriptions, decoded blobs) and through triage, whose
// evidence lines carry static File fields: EXFIL-005's is an absolute path, a finding on
// ~/.claude.json reads `<username>/.claude.json`, a memory file's is Claude Code's encoded
// project directory.
//
// Three placement rules, each load-bearing:
//
//   - Here, never inside detect.Redact. Redact produces every static snippet; teaching it about
//     homes would change text/JSON/SARIF output and re-key SARIF fingerprints for a problem that
//     exists only on the way out of the process.
//   - AFTER Redact, on text Redact has seen whole, at unit construction. Redact's entropy rule
//     decides on the length of a run, and a home is part of the run it sits in:
//     `/Users/alice/Xk9mQ2vL8pR4tZ7wB3n` is redacted, while `~/Xk9mQ2vL8pR4tZ7wB3n` leaves a
//     20-byte run under the 24-byte floor — scrubbing first, as this did at first, made the
//     judge's view of a line LESS redacted than the report's. The price: Redact's class includes
//     '/', so a long digit-bearing home can lose its head and keep its tail, the username
//     (`<REDACTED>.d/alice`). The scrub completes the shapes such a cut leaves (repairHalves) —
//     the same shapes static snippets reach triage in, since they were redacted at detect time.
//   - Into the unit, not just the request. Grounding (ground.go) checks a quote against the units,
//     so they must hold the bytes that were sent: a quote of `~/notes` must ground.
//
// Only the home is replaced, never the bare username: it can be an ordinary word, and replacing
// it in prose would rewrite the content under review. The one structural exception is detect's
// relPath fallback, `<username>/<file>`, rewritten in file positions only (file).
//
// WHICH homes: the OS user's, always (Run), and the scan's, when the caller names one
// (Options.Home). The user's is the default rather than an option because the scrub has to be
// safe when a caller forgets: an empty Options.Home used to mean "send paths unchanged", and
// `check --llm` never set it.
type egress struct {
	homes   []string      // every spelling of every home, longest first
	encoded []encodedHome // the same in Claude Code's project-directory encoding; none for a one-segment home
	users   []string      // each home's last segment, for the relPath fallback only
}

// encodedHome is one home in Claude Code's project-directory encoding, with where its username
// starts — which cannot be read back from the encoded form: every separator became '-', and so
// did every '.' or '-' a username can hold. The clip repair needs it (repairHalves).
type encodedHome struct {
	form   string
	userAt int // first byte of the username segment in form
}

// newEgress prepares the scrub for the given homes, the OS user's first. Each is made absolute
// (filepath.Abs, which also cleans it) — a relative home is resolved against the working directory,
// where the scan resolved its root, not dropped — and replaced in that form and again with symlinks
// resolved; a spelling only the caller's bytes had (`/Users/./alice`) is not one of them. An empty
// home, or one that is the filesystem root, adds nothing: replacing "/" would rewrite every
// absolute path in the excerpt.
//
// A spelling that lies inside an EARLIER home's is dropped: CLAUDE_CONFIG_DIR=~/.config/claude makes
// the scan's home ~/.config, and replacing it with `~` as well would send ~/.config/claude/x as
// ~/claude/x, a path the judge would read as somewhere else. The earlier home already covers it.
// Only in that direction — a scan home that CONTAINS the user's (--root /Users/.claude) keeps both,
// or /Users/alice/x would go out as ~/alice/x.
func newEgress(homes ...string) egress {
	var e egress
	var kept []string
	for _, h := range homes {
		for _, f := range homeSpellings(h) {
			if !within(f, kept) {
				kept = append(kept, f)
				e.users = appendNew(e.users, filepath.Base(f))
			}
		}
	}
	// Longest first: /private/var/…/alice contains /var/…/alice, and replacing the shorter one
	// first would leave "/private~".
	sort.SliceStable(kept, func(i, j int) bool { return len(kept[i]) > len(kept[j]) })
	e.homes = kept
	for _, f := range kept {
		// `-root` (from /root) is too much like a command-line option to replace safely; the raw
		// form /root/… still is.
		if strings.Count(f, string(filepath.Separator)) >= 2 {
			userAt := strings.LastIndexByte(f, filepath.Separator) + 1
			e.encoded = append(e.encoded, encodedHome{projectDirName(f), len(projectDirName(f[:userAt]))})
		}
	}
	return e
}

// homeSpellings returns a home made absolute, and again with symlinks resolved when that differs.
// None for an empty home or the filesystem root.
func homeSpellings(h string) []string {
	if h == "" {
		return nil
	}
	abs, err := filepath.Abs(h)
	if err != nil || abs == string(filepath.Separator) {
		return nil
	}
	out := []string{abs}
	if r, err := filepath.EvalSymlinks(abs); err == nil && r != abs && r != string(filepath.Separator) {
		out = append(out, r)
	}
	return out
}

// within reports whether p is one of dirs or lies under one of them.
func within(p string, dirs []string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func appendNew(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// projectDirName is how Claude Code names a project's directory under ~/.claude/projects: the
// working directory with every CHARACTER that is not an ASCII letter or digit turned into one
// '-'. Per character, not per byte: /Users/josé is -Users-jos-, and the byte-wise -Users-jos--
// names no directory Claude Code makes, so a non-ASCII home's encoded form went out unreplaced.
// Inferred from the directories it creates, not documented — if Claude Code changes it, this form
// leaks again. (A character outside the Basic Multilingual Plane has not been observed; it is
// mapped to one '-' like any other.)
func projectDirName(p string) string {
	return strings.Map(func(r rune) rune {
		if r < utf8.RuneSelf && alnum(byte(r)) {
			return r
		}
		return '-'
	}, p)
}

// scrub replaces every form of the home in s with `~`, completing a home Redact cut in two. s has
// already been through Redact (see redact), which is what that completion assumes.
func (e egress) scrub(s string) string { return e.scrubCut(s, false) }

// snippet scrubs a static finding's snippet, which detect redacted and then CLIPPED to 200 bytes
// plus `…`: a home here can also end at the clip mark. Only these get that repair — raw artifact
// text is never clipped by detect, so a `…` at its end is something the author wrote, and
// completing the path before it would rewrite the content under review on a guess. Redacted once
// more on the way in, defensively; Redact is idempotent.
func (e egress) snippet(s string) string { return e.scrubCut(detect.Redact(s), true) }

func (e egress) scrubCut(s string, clipped bool) string {
	if len(e.homes) == 0 || s == "" {
		return s
	}
	for _, h := range e.homes {
		s = replaceBounded(s, h, "~", startsPath, endsPath)
	}
	for _, enc := range e.encoded {
		s = replaceBounded(s, enc.form, "~", startsPath, endsEncoded)
	}
	return e.repairHalves(s, clipped)
}

// redact is the one call the excerpt builders make on raw artifact text: Redact, fold padding
// (foldPadding), Redact again if anything was folded, then scrub. The order is the point — see the
// type comment for the scrub. The fold comes after the first Redact because the entropy rule weighs a
// whole run: a token it redacts alone, joined by the fold to a low-entropy tail, would pass. The
// second Redact sees what the fold joined (a key id split by zero-width characters), and the scrub
// runs last so a home split the same way is stripped once whole.
func (e egress) redact(s string) string {
	r := detect.Redact(s)
	if f := foldPadding(r); f != r {
		r = detect.Redact(f)
	}
	return e.scrub(r)
}

// file redacts and scrubs a path in a FILE position (a static finding's Evidence.File, which detect
// stores unredacted) and, there only, rewrites detect.relPath's two-segment fallback: a file
// directly in the home, rendered from a root below it, reads `<username>/<file>`. Exactly that
// shape — the username as the first of two segments — becomes `~/<file>`; a third segment or a
// longer first one is some other path.
func (e egress) file(f string) string {
	f = e.redact(f)
	for _, u := range e.users {
		if rest, ok := strings.CutPrefix(f, u+"/"); ok && rest != "" && !strings.Contains(rest, "/") {
			return "~/" + rest
		}
	}
	return f
}

const (
	redactedMark = "<REDACTED>"
	clipMark     = "…" // detect's snippet cap: 200 bytes, then this, at the very end
)

// repairHalves completes a home that was already cut when the scrub sees it. Two operations can
// split a path, and a split home can keep exactly the part that names the user:
//
//   - Redact — on every path, since everything is redacted before it is scrubbed. Its entropy token
//     class is [A-Za-z0-9+/_-], so a long digit-bearing run is replaced up to the first byte outside
//     that class and no further: `/var/…/T/Test123/001/home.d/alice` comes out as
//     `<REDACTED>.d/alice`. The run can also start at such a byte and eat the end instead:
//     `/home/first.<REDACTED>`.
//   - detect's snippet cap — on static snippets only (clipped), see snippet. It cuts at 200 bytes and
//     appends `…`, wherever that lands: `/Users/ali…`, and in the encoded spelling just the same,
//     `projects/-Users-ali…`. Only a snippet that ENDS in the marker was clipped; one with `…`
//     anywhere else is prose.
//
// Each cut leaves a known shape, so only those are replaced: a cut point is a byte outside the
// entropy class, and a clipped fragment counts only once it reaches into the username (a clipped
// `/Users/…` names nobody, and replacing it would be a guess). Nothing fuzzier: a near-match is some
// other path. The encoded spelling has only the clipped shape: it is letters, digits and '-', inside
// the entropy class end to end, so the entropy rule takes all of it or none and leaves no half.
func (e egress) repairHalves(s string, clipped bool) string {
	hasRed, hasClip := strings.Contains(s, redactedMark), clipped && strings.HasSuffix(s, clipMark)
	if !hasRed && !hasClip {
		return s
	}
	for _, h := range e.homes {
		userAt := strings.LastIndexByte(h, '/') + 1 // first byte of the username segment
		// Where a surviving fragment of h can begin: at h's own start, or — after Redact ate the
		// head — right behind the marker, at a cut point.
		type start struct {
			at    int
			mark  string
			check func(string, int) bool
		}
		starts := []start{{0, "", startsPath}}
		for i := 1; hasRed && i < len(h); i++ {
			if entropyByte(h[i]) {
				continue
			}
			starts = append(starts, start{i, redactedMark, anywhere})
			s = replaceBounded(s, redactedMark+h[i:], "~", anywhere, endsPath)                   // head eaten
			s = replaceBounded(s, h[:i+1]+redactedMark, "~/"+redactedMark, startsPath, anywhere) // tail eaten
		}
		for _, st := range starts {
			for b := len(h) - 1; hasClip && b > userAt && b > st.at; b-- {
				s = replaceBounded(s, st.mark+h[st.at:b]+clipMark, "~"+clipMark, st.check, atEnd) // clipped
			}
		}
	}
	for _, enc := range e.encoded {
		for b := len(enc.form) - 1; hasClip && b > enc.userAt; b-- {
			s = replaceBounded(s, enc.form[:b]+clipMark, "~"+clipMark, startsPath, atEnd) // encoded, clipped
		}
	}
	return s
}

// replaceBounded replaces each occurrence of old in s whose surroundings pass the two checks: an
// occurrence that is the start of a longer word or path (`/Users/alice2`) is a different thing.
func replaceBounded(s, old, repl string, startOK func(s string, i int) bool, endOK func(s string, j int) bool) string {
	if old == "" || !strings.Contains(s, old) {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		k := strings.Index(s[i:], old)
		if k < 0 {
			break
		}
		k += i
		j := k + len(old)
		if startOK(s, k) && endOK(s, j) {
			b.WriteString(s[i:k])
			b.WriteString(repl)
			i = j
			continue
		}
		b.WriteString(s[i : k+1])
		i = k + 1
	}
	b.WriteString(s[i:])
	return b.String()
}

// startsPath: the byte before is not part of a path segment, so the match is not the tail of a
// longer path (`/data/Users/alice` is not the home).
func startsPath(s string, i int) bool { return i == 0 || !pathByte(s[i-1]) }

// endsPath: the byte after ends the path — a separator, a quote, a space, the end — or is a full
// stop that ends a sentence. `/Users/alice.bak` and `/Users/alice2` are siblings, not the home.
func endsPath(s string, j int) bool {
	if j == len(s) || !pathByte(s[j]) {
		return true
	}
	return s[j] == '.' && (j+1 == len(s) || !pathByte(s[j+1]))
}

// endsEncoded: the encoded home continues with '-' (the encoded '/') or ends; a letter or digit
// would make it a different directory.
func endsEncoded(s string, j int) bool { return j == len(s) || !alnum(s[j]) }

func anywhere(string, int) bool { return true }

// atEnd: the cap's marker is appended to the end of a snippet, and a snippet ends every line this
// repairs (triage's `file:line snippet`, the digest's `file:line [RULE] snippet`).
func atEnd(s string, j int) bool { return j == len(s) }

func alnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }

func pathByte(c byte) bool { return alnum(c) || c == '.' || c == '_' || c == '-' }

// entropyByte mirrors the class of detect's entropy token rule: the bytes one redacted run can span.
func entropyByte(c byte) bool { return alnum(c) || c == '+' || c == '/' || c == '_' || c == '-' }
