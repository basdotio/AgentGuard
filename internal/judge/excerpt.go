// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/basdotio/agent-guard/internal/collect"
	"github.com/basdotio/agent-guard/internal/detect"
	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/safeio"
)

// Excerpt size caps: keep prompts small (cheap, fast) and bounded regardless of skill size.
const (
	maxExcerptBytes = 6000    // total behavior text sent per artifact
	maxFileBytes    = 2000    // per-file cap so one big file can't crowd out the rest
	maxFileRead     = 1 << 20 // hard ceiling on bytes read from any one file (memory-DoS guard);
	// a hostile skill can ship a multi-GB blob — never materialize it. 1 MiB comfortably
	// covers real scripts, so redaction still sees whole secrets before truncation.
)

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
func singleFileExcerpt(path string) (string, []sourceUnit) {
	raw := readAtMost(path, maxFileRead)
	if raw == nil {
		return "", nil
	}
	// Prose: blank runs collapse (padding hides here too) but nothing is a "comment" to drop.
	text, lm := condense(path, detect.Redact(string(raw)), false)
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
	marker := "# … %d line(s) omitted …"
	headBudget := max * 2 / 3
	tailBudget := max - headBudget - len(marker) - 8
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
		out = append(out, fmt.Sprintf(marker, j-i))
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
func hookExcerpt(file string, h model.Hook) (declared, behavior string, units []sourceUnit) {
	matcher := h.Matcher
	if matcher == "" {
		matcher = "* (every tool)"
	}
	declared = detect.Redact("event: " + h.Event + "\nmatcher: " + matcher)
	behavior = boundedRedact(h.Command, maxExcerptBytes)
	if behavior == "" && h.URL != "" {
		behavior = boundedRedact(h.URL, maxExcerptBytes)
	}
	return declared, behavior, []sourceUnit{{
		file: detect.Redact(filepath.Base(file)), text: behavior, firstLine: 0, collapsed: true,
	}}
}

// mcpExcerpt returns an MCP server's configured strings — command, args, env values — using
// the SAME extraction the static engine scans (detect.ConfigStrings), so the two can't drift.
func mcpExcerpt(path, name string) (string, []sourceUnit) {
	strs := detect.ConfigStrings(path, "mcpServers", name)
	if len(strs) == 0 {
		return "", nil
	}
	text := boundedRedact(strings.Join(strs, "\n"), maxExcerptBytes)
	return text, []sourceUnit{{
		file: detect.Redact(filepath.Base(path)), text: text, firstLine: 0, collapsed: true,
	}}
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
func capabilityDigest(a model.ArtifactReport) (string, []sourceUnit) {
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
			line := fmt.Sprintf("%s:%d [%s] %s", e.File, e.Line, f.RuleID, detect.Redact(e.Snippet))
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

// boundedRedact redacts s (best-effort, see judge.go header) then caps it to max bytes.
// Redaction happens BEFORE truncation so a secret straddling the cap can't survive as a
// sub-threshold partial. Used for the SKILL.md instruction body (injection mode).
func boundedRedact(s string, max int) string {
	red := detect.Redact(s)
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
func behaviorExcerpt(dir string) (string, []sourceUnit) {
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
		red, lm := condense(p, detect.Redact(string(raw)), true)
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
