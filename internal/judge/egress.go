// SPDX-License-Identifier: MIT
package judge

import (
	"path/filepath"
	"sort"
	"strings"

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
//   - BEFORE Redact, at unit construction. Redact's entropy class includes '/', so a long,
//     digit-bearing home can lose its head and keep its tail — the username — and a scrub run
//     afterwards could no longer recognise it. (Static snippets reach triage already redacted;
//     repairHalves covers the two shapes that leaves.)
//   - Into the unit, not just the request. Grounding (ground.go) checks a quote against the units,
//     so they must hold the bytes that were sent: a quote of `~/notes` must ground.
//
// Only the home is replaced, never the bare username: it can be an ordinary word, and replacing
// it in prose would rewrite the content under review. The one structural exception is detect's
// relPath fallback, `<username>/<file>`, rewritten in file positions only (file).
type egress struct {
	homes   []string // the home as given and with symlinks resolved, longest first
	encoded []string // the same in Claude Code's project-directory encoding; none for a one-segment home
	user    string   // the home's last segment, for the relPath fallback only
}

// newEgress prepares the scrub for one home. An empty, relative or root home yields the zero
// value, which changes nothing: replacing "/" would rewrite every absolute path in the excerpt.
func newEgress(home string) egress {
	if home == "" {
		return egress{}
	}
	home = filepath.Clean(home)
	if !filepath.IsAbs(home) || home == string(filepath.Separator) {
		return egress{}
	}
	forms := []string{home}
	if r, err := filepath.EvalSymlinks(home); err == nil && r != home {
		forms = append(forms, r)
	}
	// Longest first: /private/var/…/alice contains /var/…/alice, and replacing the shorter one
	// first would leave "/private~".
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	e := egress{homes: forms, user: filepath.Base(home)}
	for _, f := range forms {
		// `-root` (from /root) is too much like a command-line option to replace safely; the raw
		// form /root/… still is.
		if strings.Count(f, string(filepath.Separator)) >= 2 {
			e.encoded = append(e.encoded, projectDirName(f))
		}
	}
	return e
}

// projectDirName is how Claude Code names a project's directory under ~/.claude/projects: the
// working directory with every byte that is not a letter or digit turned into '-'. Inferred from
// the directories it creates, not documented — if Claude Code changes it, this form leaks again.
func projectDirName(p string) string {
	b := []byte(p)
	for i, c := range b {
		if !alnum(c) {
			b[i] = '-'
		}
	}
	return string(b)
}

// scrub replaces every form of the home in s with `~`.
func (e egress) scrub(s string) string {
	if len(e.homes) == 0 || s == "" {
		return s
	}
	for _, h := range e.homes {
		s = replaceBounded(s, h, "~", startsPath, endsPath)
	}
	for _, enc := range e.encoded {
		s = replaceBounded(s, enc, "~", startsPath, endsEncoded)
	}
	if strings.Contains(s, redactedMark) {
		s = e.repairHalves(s)
	}
	return s
}

// redact is the one call the excerpt builders make on raw artifact text: scrub, then Redact.
// The order is the point — see the type comment.
func (e egress) redact(s string) string { return detect.Redact(e.scrub(s)) }

// file scrubs a path in a FILE position (a static finding's Evidence.File) and, there only,
// rewrites detect.relPath's two-segment fallback: a file directly in the home, rendered from a
// root below it, reads `<username>/<file>`. Exactly that shape — the username as the first of two
// segments — becomes `~/<file>`; a third segment or a longer first one is some other path.
func (e egress) file(f string) string {
	f = e.scrub(f)
	if e.user == "" {
		return f
	}
	if rest, ok := strings.CutPrefix(f, e.user+"/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "~/" + rest
	}
	return f
}

const redactedMark = "<REDACTED>"

// repairHalves completes a home that Redact already cut in two. Its entropy token class is
// [A-Za-z0-9+/_-], so a long digit-bearing run is replaced up to the first byte outside that class
// and no further: `/var/…/T/Test123/001/home.d/alice` comes out as `<REDACTED>.d/alice`, the
// username intact. A home can only be cut at such a byte, so the two shapes it can leave are known
// exactly — the marker followed by the home's tail from a cut point, or the home's head up to a cut
// point followed by the marker — and only those are replaced. Nothing fuzzier: a near-match is
// some other path.
func (e egress) repairHalves(s string) string {
	for _, h := range e.homes {
		for i := 0; i < len(h); i++ {
			if entropyByte(h[i]) {
				continue
			}
			s = replaceBounded(s, redactedMark+h[i:], "~", anywhere, endsPath)
			s = replaceBounded(s, h[:i+1]+redactedMark, "~/"+redactedMark, startsPath, anywhere)
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

func alnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }

func pathByte(c byte) bool { return alnum(c) || c == '.' || c == '_' || c == '-' }

// entropyByte mirrors the class of detect's entropy token rule: the bytes one redacted run can span.
func entropyByte(c byte) bool { return alnum(c) || c == '+' || c == '/' || c == '_' || c == '-' }
