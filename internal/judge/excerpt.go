// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// Excerpt size caps: keep prompts small (cheap, fast) and bounded regardless of skill size.
const (
	maxExcerptBytes = 6000    // total behavior text sent per artifact
	maxFileBytes    = 2000    // per-file cap so one big file can't crowd out the rest
	maxFileRead     = 1 << 20 // hard ceiling on bytes read from any one file (memory-DoS guard);
	// a hostile skill can ship a multi-GB blob — never materialize it. 1 MiB comfortably
	// covers real scripts, so redaction still sees whole secrets before truncation.
	// maxDeclaredBytes bounds the declared side of a comparison (a description, an interception
	// point). It had no cap: a SKILL.md description went out whole — up to the 1 MiB the
	// frontmatter reader takes — once per pass that compares against it. A purpose that needs more
	// than a thousand bytes to state is not being stated, it is being padded.
	maxDeclaredBytes = 1000
)

// declaredPurpose prepares the declared side of a request: scrub and redact, THEN cap — the
// redact-before-truncate order every excerpt keeps — cutting on a rune boundary so a description
// in any language never ends in half a character.
func declaredPurpose(s string, eg egress) string {
	s = eg.redact(s)
	if len(s) <= maxDeclaredBytes {
		return s
	}
	cut := maxDeclaredBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// readAtMost reads up to max bytes of a file (never the whole thing) — the memory guard for
// a hostile scanned tree. Returns the bytes read; a read error yields nil.
func readAtMost(path string, max int) []byte {
	b, err := safeio.ReadPrefix(path, int64(max))
	if err != nil {
		return nil
	}
	return b
}

// behaviorExts are the files whose content represents "what the skill does": scripts and
// instruction/config files. Prose docs are excluded (they describe, not act).
var behaviorExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".js": true, ".mjs": true,
	".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".ps1": true, ".psm1": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
}

// --- per-kind behavior extraction -------------------------------------------------------
//
// Each artifact kind carries its "what it does" in a different shape: a skill is a tree, an
// instruction file or subagent is one file, a hook is a command line in JSON, an MCP server
// is a config entry. Everything below returns the same pair — the redacted text to send, and
// the source units to ground a verdict against (ground.go) — so the run loop stays free of
// per-kind special cases and no kind can quietly skip redaction.

// singleFileExcerpt reads ONE artifact file (CLAUDE.md, a subagent, a slash command). These
// kinds have no tree and no frontmatter contract worth splitting: the whole file is the
// instruction text an agent will read.
func singleFileExcerpt(path string, eg egress) (string, []sourceUnit) {
	raw := readAtMost(path, maxFileRead)
	if raw == nil {
		return "", nil
	}
	// Prose: blank runs collapse (padding hides here too) but nothing is a "comment" to drop.
	text, lm := condense(path, eg.redact(string(raw)), false)
	text, lm = capHeadTail(text, lm, maxExcerptBytes)
	return text, []sourceUnit{{file: detect.Redact(filepath.Base(path)), text: text, firstLine: 1, lineMap: lm}}
}

// condense removes what spends the model's budget without carrying behavior: a run of blank
// lines becomes one, and (for code, when stripComments is set) comment-only lines go entirely,
// by the same classifier the static A1 filter uses. Returns the condensed text and, for each
// of its lines, the 1-based line of the original it came from — the map ground.go needs to
// cite a real location once the excerpt is no longer contiguous with the file.
//
// Both removals exist because the evasions they defeat were seen, not imagined, in a published
// set of malicious skills: a hundred thousand blank lines put a payload past a byte cap that
// used to be a plain prefix; and a paragraph of comments explaining why a registry rewrite was
// fine was written FOR the judge — a comment is the one place in a script where an author
// addresses the reader instead of the interpreter, which is exactly where an attacker would
// address this reader. The interpreter never sees comments; neither should the judge.
func condense(path, text string, stripComments bool) (string, []int) {
	var comments map[int]bool
	if stripComments {
		comments = detect.CommentOnlyLines(path, text)
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	lm := make([]int, 0, len(lines))
	blank := false
	for i, l := range lines {
		n := i + 1
		if strings.TrimSpace(l) == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			lm = append(lm, n)
			continue
		}
		if comments[n] && !strings.HasPrefix(strings.TrimSpace(l), "#!") {
			continue // the shebang stays: it says which interpreter, which is behavior
		}
		blank = false
		out = append(out, l)
		lm = append(lm, n)
	}
	for len(out) > 0 && out[len(out)-1] == "" { // a file's trailing newline is not a line
		out, lm = out[:len(out)-1], lm[:len(lm)-1]
	}
	return strings.Join(out, "\n"), lm
}

// maxFillerRunBytes is the longest run of filler an excerpt passes on as written. Real layout stays
// under it: across 2.2 million non-blank lines of installed plugins and the benchmark corpus the
// longest indentation is 121 bytes, and only 28 lines hold a longer run (27 markdown table rows'
// column alignment, one run of spaces and zero-width characters at the end of a line). Lower, and
// it starts to eat indentation; higher only halves what spacing the padding out costs (see foldPadding).
const maxFillerRunBytes = 128

// isFiller reports whether r is filler: a rune grounding reads as a word break or as nothing at all
// (isMatchSpace, isMatchIgnored), except the line break, which the line map counts.
func isFiller(r rune) bool { return r != '\n' && (isMatchSpace(r) || isMatchIgnored(r)) }

// foldPadding replaces every run of filler longer than maxFillerRunBytes with what grounding reads it
// as: one space, or nothing when the run holds only invisible runes (foldForMatch). It is the
// horizontal twin of condense's blank-run collapse. The byte caps measure what an excerpt may cost,
// and one line padded with thousands of bytes of whitespace — Unicode spaces and zero-width
// characters included — no longer fit: capHeadTail sent the omission marker in its place (and,
// as the head's first overlong line, every line after it until the tail), a body of that one line
// went out as a prefix of blanks, a hook command or an MCP value was cut to its padding, and three
// mostly-blank scripts each under the per-file cap spent the whole excerpt. The directive the
// judge exists to read never left the machine.
//
// A run never crosses '\n', so the line count — and every line map — is unchanged; the result is
// never longer than s; and since it is what grounding reads anyway, the folded text normalizes to
// exactly what s does (FuzzFoldPadding), so quotes, citations and snippet windows are unaffected.
// Filler broken up by a visible character at least every maxFillerRunBytes is content as far as
// this can tell, and is capped like any other.
func foldPadding(s string) string {
	var b strings.Builder
	copied := 0 // s[:copied] is in b, or nothing was folded yet
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		if !isFiller(r) {
			i += w
			continue
		}
		j, spaced := i, false
		for j < len(s) {
			r, w := utf8.DecodeRuneInString(s[j:])
			if !isFiller(r) {
				break
			}
			spaced = spaced || isMatchSpace(r)
			j += w
		}
		if j-i > maxFillerRunBytes {
			b.WriteString(s[copied:i])
			if spaced {
				b.WriteByte(' ')
			}
			copied = j
		}
		i = j
	}
	if copied == 0 {
		return s
	}
	b.WriteString(s[copied:])
	return b.String()
}

// omittedLinesMarker is the line an excerpt carries where it left lines out, so the model is told
// the text it sees is not the whole file (capHeadTail) or the whole configuration (mcpExcerpt).
const omittedLinesMarker = "# … %d line(s) omitted …"

// capHeadTail bounds text to max bytes by keeping the first two-thirds of the budget from the
// head and the last third from the tail, with a marker between. A cap that keeps only a prefix
// has a known blind spot — the end of the file — and the end of the file is where `head`, an
// editor's first page and every reviewer's glance also stop, so it is where payloads go. Cuts
// fall on line boundaries so each kept line still maps to its original through lm.
func capHeadTail(text string, lm []int, max int) (string, []int) {
	if len(text) <= max {
		return text, lm
	}
	lines := strings.Split(text, "\n")
	if len(lm) != len(lines) { // defensive: a map that does not fit is worse than none
		lm = make([]int, len(lines))
		for i := range lm {
			lm[i] = i + 1
		}
	}
	headBudget := max * 2 / 3
	tailBudget := max - headBudget - len(omittedLinesMarker) - 8
	i, used := 0, 0
	for ; i < len(lines) && used+len(lines[i])+1 <= headBudget; i++ {
		used += len(lines[i]) + 1
	}
	j, used := len(lines), 0
	for ; j > i && used+len(lines[j-1])+1 <= tailBudget; j-- {
		used += len(lines[j-1]) + 1
	}
	if i == 0 && j == len(lines) {
		// One line wider than either budget (minified code): a prefix is all there is.
		first := lines[0]
		if len(first) > max {
			first = first[:max]
		}
		return first, lm[:1]
	}
	out := make([]string, 0, i+1+len(lines)-j)
	outLM := make([]int, 0, cap(out))
	out = append(out, lines[:i]...)
	outLM = append(outLM, lm[:i]...)
	if j > i {
		out = append(out, fmt.Sprintf(omittedLinesMarker, j-i))
		outLM = append(outLM, lm[i])
	}
	out = append(out, lines[j:]...)
	outLM = append(outLM, lm[j:]...)
	return strings.Join(out, "\n"), outLM
}

// offsetLines shifts a line map by delta — for text that starts partway into a file (a SKILL.md
// body after its frontmatter).
func offsetLines(lm []int, delta int) []int {
	out := make([]int, len(lm))
	for i, n := range lm {
		out[i] = n + delta
	}
	return out
}

// hookExcerpt renders a hook as its interception point plus the command it runs. The command
// lives inside JSON, so there is no meaningful line to cite — hence a collapsed unit at
// line 0, matching how the static engine reports hook hits.
func hookExcerpt(file string, h model.Hook, eg egress) (declared, behavior string, units []sourceUnit) {
	matcher := h.Matcher
	if matcher == "" {
		matcher = "* (every tool)"
	}
	declared = declaredPurpose("event: "+h.Event+"\nmatcher: "+matcher, eg)
	behavior = boundedRedact(h.Command, maxExcerptBytes, eg)
	if behavior == "" && h.URL != "" {
		behavior = boundedRedact(h.URL, maxExcerptBytes, eg)
	}
	return declared, behavior, []sourceUnit{{
		file: detect.Redact(filepath.Base(file)), text: behavior, firstLine: 0, collapsed: true,
	}}
}

// mcpExcerpt returns an MCP server's configuration as `key=value` lines — the SAME string leaves
// the static engine scans (detect.MCPConfigLines: the entry the rules and the content hash read,
// found by its key, under mcpServers or at the top level of an unwrapped plugin file), keyed and in
// a fixed order. It used to be the bare
// values in Go's map order: keyless, `{"DB_PASS":"hunter2"}` went out as `hunter2`, which no
// keyed redaction can recognise, and the same config gave a different request body each run.
//
// What the server runs and where it connects comes first (mcpLeadKeys), then every other key in
// sorted order; each line is capped at maxConfigLineBytes; lines go in while they fit the excerpt
// budget, and the rest are counted in a marker line. Sorted alone, with a cap on the head of the
// whole text, one padded key that sorts first ("aaa": 6 KB) kept command and env out of the
// excerpt on every run. Each line is masked (maskCredentialValue), redacted and scrubbed BEFORE it
// is capped, so a cap can split neither a secret nor the home.
//
// shortened says what was left out, empty when nothing was: the caller discloses it (LLM-000),
// since a real configuration fits and one that does not has been shaped.
func mcpExcerpt(a model.ArtifactReport, eg egress) (text string, units []sourceUnit, shortened string) {
	lines := detect.MCPConfigLines(a)
	if len(lines) == 0 {
		return "", nil, ""
	}
	lines = mcpLeadFirst(lines)
	budget := maxExcerptBytes - len(omittedLinesMarker) - 8
	out := make([]string, 0, len(lines))
	used, capped, dropped := 0, 0, 0
	for i, l := range lines {
		l = eg.redact(maskCredentialValue(l))
		if c := capLine(l, maxConfigLineBytes); c != l {
			l = c
			capped++
		}
		if used+len(l)+1 > budget {
			dropped = len(lines) - i
			out = append(out, fmt.Sprintf(omittedLinesMarker, dropped))
			break
		}
		out = append(out, l)
		used += len(l) + 1
	}
	text = strings.Join(out, "\n")
	var cut []string
	if capped > 0 {
		cut = append(cut, fmt.Sprintf("%d value(s) cut to %d bytes", capped, maxConfigLineBytes))
	}
	if dropped > 0 {
		cut = append(cut, fmt.Sprintf("%d line(s) past the %d-byte excerpt not sent", dropped, maxExcerptBytes))
	}
	return text, []sourceUnit{{
		file: detect.Redact(filepath.Base(a.Path)), text: text, firstLine: 0, collapsed: true,
	}}, strings.Join(cut, ", ")
}

// maxConfigLineBytes caps one `key=value` line of the MCP excerpt — a value or a key — so no single
// setting can spend the excerpt's budget. Real values (a command, an argument, a URL, an env var)
// are far shorter.
const maxConfigLineBytes = 500

// mcpLeadKeys are the top-level settings that say what an MCP server runs and where it connects,
// sent first and in this order. command, args and url are a string or a list of strings, so their
// lines carry exactly the key; env and headers are objects, whose lines read `env.NAME`. A
// top-level key that merely starts with one of these (`command.x`) ranks with the rest: matching
// it as a lead key would let padding under such names take the lead keys' place.
var mcpLeadKeys = []struct {
	key    string
	object bool
}{{"command", false}, {"args", false}, {"env", true}, {"url", false}, {"headers", true}}

// mcpLeadFirst orders ConfigLines output: lead keys first, in mcpLeadKeys order, then the rest.
// Stable, so within a rank the lines keep ConfigLines' sorted order — and since "env" sorts before
// "env.<anything>", the real env object's lines come before any top-level key named like them.
func mcpLeadFirst(lines []string) []string {
	rank := func(line string) int {
		key, _, _ := strings.Cut(line, "=")
		for i, k := range mcpLeadKeys {
			if key == k.key || k.object && strings.HasPrefix(key, k.key+".") {
				return i
			}
		}
		return len(mcpLeadKeys)
	}
	out := append([]string(nil), lines...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// capLine cuts s to at most max bytes on a character boundary and says how much it left out.
func capLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf(" … (%d bytes omitted)", len(s)-cut)
}

// credentialKeyRE and credentialSegmentRE recognise a configuration key that names a credential.
//
// Deliberately wider than detect.Redact's key list, which has no `pass` (DB_PASS) and no `pwd`
// (MYSQL_PWD): that list runs over prose and code, where `bypass=` and `compass:` are words, and
// widening it would change every static snippet. Here the key is STRUCTURE — an env var name, a
// header name — not prose, so the wider net has nothing to misfire on but another key, and an
// over-match costs the model one value. `pw` is matched only as a whole segment (DB_PW), never
// inside a word.
var (
	credentialKeyRE     = regexp.MustCompile(`(?i)pass|pwd|secret|token|key|auth|cred|private|cookie`)
	credentialSegmentRE = regexp.MustCompile(`(?i)(^|[_\-])pw($|[_\-])`)
)

// maskCredentialValue replaces the value of one `key=value` line from detect.ConfigLines with
// <REDACTED> when the key's last segment names a credential. The key stays: which setting holds a
// secret is what the judge (and anyone reading its verdict) needs, the secret itself is not.
func maskCredentialValue(line string) string {
	key, val, ok := strings.Cut(line, "=")
	if !ok || val == "" {
		return line
	}
	last := key
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		last = key[i+1:]
	}
	if credentialKeyRE.MatchString(last) || credentialSegmentRE.MatchString(last) {
		return key + "=" + redactedMark
	}
	return line
}

// capabilityDims are the dimensions whose findings describe what a file CAN DO — the raw
// material for the collusion digest. Injection/obfuscation findings say something about how
// text is written, not about a capability that could form one end of a chain.
var capabilityDims = map[int]bool{
	3: true, // data exfiltration
	4: true, // code execution
	5: true, // supply chain
	9: true, // filesystem / credentials
}

// capabilityDigest summarizes what each FILE of an artifact can do, one already-redacted line
// per capability, reusing the evidence the static pass produced.
//
// It exists so the collusion question ("do these combine into a chain?") does not cost a
// whole-tree upload: the model only needs to know which file does what, and the static pass
// already found exactly those lines. Sending the tree instead would multiply the price of the
// cheapest question the judge asks.
//
// The digest line carries the static File and Snippet, so both are scrubbed for sending (the
// File as a file position, the Snippet as the clipped static snippet it is); the unit keeps the
// static File as its citation, since that is what the report shows for the same line.
func capabilityDigest(a model.ArtifactReport, eg egress) (string, []sourceUnit) {
	var lines []string
	var units []sourceUnit
	seen := map[string]bool{}
	for _, f := range a.Findings {
		if f.Source == model.SrcLLM || !capabilityDims[f.Dimension] || len(f.Evidence) == 0 {
			continue
		}
		for _, e := range f.Evidence {
			if e.File == "" || e.Snippet == "" {
				continue
			}
			line := fmt.Sprintf("%s:%d [%s] %s", eg.file(e.File), e.Line, f.RuleID, eg.snippet(e.Snippet))
			if seen[line] {
				continue
			}
			seen[line] = true
			lines = append(lines, line)
			// The unit text is the WHOLE digest line: the model may quote it with or without
			// the file prefix, and both must ground.
			units = append(units, sourceUnit{file: e.File, text: line, firstLine: e.Line, collapsed: true})
		}
	}
	return strings.Join(lines, "\n"), units
}

// boundedRedact redacts s (best-effort, see judge.go header), strips the home, then caps it to max
// bytes. Redaction happens BEFORE truncation so a secret straddling the cap can't survive as a
// sub-threshold partial, and before the scrub so it sees each run whole (egress.go). Used for the
// one-line behaviors: a hook's command or URL, an MCP server's configuration.
func boundedRedact(s string, max int, eg egress) string {
	red := eg.redact(s)
	if len(red) > max {
		red = red[:max]
	}
	return red
}

// behaviorExcerpt walks a skill dir and returns a bounded, REDACTED concatenation of its
// executable/config files — the "actual behavior" side of the intent comparison — together
// with the source units it was assembled from. It skips vendored/generated dirs (shared
// skip-set) and never follows symlinks (§16.2), and every byte passes through detect.Redact
// (best-effort — see judge.go header) before leaving here.
//
// The units are the SAME bytes as the returned text, truncation included. That equality is
// the whole point: grounding (ground.go) checks a verdict's quote against what the model was
// actually shown, so a unit holding text that never got sent would ground a claim about
// something the model could not have seen.
func behaviorExcerpt(dir string, eg egress) (string, []sourceUnit) {
	var units []sourceUnit
	total := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if collect.ExcludedFromScan(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 { // never read a symlink target (may escape the skill root)
			return nil
		}
		if !behaviorExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		raw := readAtMost(p, maxFileRead)
		if raw == nil {
			return nil
		}
		// Redact BEFORE condensing or truncating: a secret straddling the byte cap must not
		// survive as a sub-threshold partial. The rel path is redacted too (a path segment could
		// be a token). Then condense — blank runs to one, comments out — and cap head+tail, so
		// the 2000 bytes carry code from both ends of the file rather than whitespace from one.
		red, lm := condense(p, eg.redact(string(raw)), true)
		red, lm = capHeadTail(red, lm, maxFileBytes)
		rel, _ := filepath.Rel(dir, p)
		header := "# " + detect.Redact(rel) + "\n"
		room := maxExcerptBytes - total - len(header) - 2 // 2 = the blank line after each file
		if room <= 0 {
			return filepath.SkipAll
		}
		red, lm = capHeadTail(red, lm, room)
		units = append(units, sourceUnit{file: detect.Redact(rel), text: red, firstLine: 1, lineMap: lm})
		total += len(header) + len(red) + 2
		return nil
	})

	var b strings.Builder
	for _, u := range units {
		b.WriteString("# " + u.file + "\n")
		b.WriteString(u.text)
		b.WriteString("\n\n")
	}
	return b.String(), units
}
