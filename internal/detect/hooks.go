// SPDX-License-Identifier: MIT
package detect

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/basdotio/AgentGuard/internal/model"
)

// Hooks are the highest-privilege surface in the environment: they run shell silently, on
// every matching tool call, with no confirmation. Scanning the command line alone is not
// enough — `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/pre.sh"` hides everything that matters
// behind a filename. So a hook contributes two kinds of unit: the command itself, and the
// local script it invokes (spec §5.1).

// scriptExts are the extensions worth following out of a hook command: things that get
// EXECUTED. A referenced .md or .json is data, not a second stage.
var scriptExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".js": true, ".mjs": true,
	".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".ps1": true, ".psm1": true,
}

// wordBreakers are the shell metacharacters that end a word the way whitespace does. Used by
// shellWords; this is TOKENISATION ONLY — the command is never interpreted and never run
// (spec §16.1); we just need the words.
const wordBreakers = ";|&()<>"

// shellWords splits a command line into words the way a POSIX shell tokenises it, minus every
// expansion: quotes group, a backslash escapes the next character (outside single quotes), and
// the metacharacters in wordBreakers end a word like whitespace does. Quotes are removed from
// the word they wrap.
//
// Quote-awareness is not polish. A hook on a real machine read
// `node "/Applications/unibase-partner 3.app/…/unibase-hook.js"`; splitting on whitespace
// turned that into the relative fragment `3.app/…/unibase-hook.js`, which resolved to "no such
// file" — a coverage note, where HOOK-002 was due. macOS application paths carry spaces as a
// matter of course, so a whitespace splitter misses exactly the second stages that live outside
// HOME. An unterminated quote runs to the end of the line: the shell would reject the command,
// but for us the words are still the words.
func shellWords(cmd string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune // 0, '\'' or '"'
	flush := func() {
		if inWord {
			out = append(out, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true // "" is a word, even an empty one
		case r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			inWord = true
		case unicode.IsSpace(r) || strings.ContainsRune(wordBreakers, r):
			flush()
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return out
}

// hookUnits returns the scannable text for one hook artifact: the command line, plus any
// local script it references, plus coverage notes for references it could not follow, plus
// scoring findings for HTTP targets (HOOK-003) and out-of-home scripts (HOOK-002).
//
// Reading a referenced script obeys the same containment as every other read (§16.2): the
// resolved path must stay under home, and a path that fails to resolve, escapes, or cannot
// be read yields a note rather than silence (§12) — "we saw a second stage and did not read
// it" must never look like "we read it and it was clean".
func hookUnits(root string, a model.ArtifactReport) ([]unit, []model.Finding) {
	var notes []model.Finding
	if strings.EqualFold(strings.TrimSpace(a.Hook.Type), "http") {
		url := strings.TrimSpace(a.Hook.URL)
		if url == "" {
			return nil, nil // collect already noted PARSE-000; should not reach here
		}
		// The URL is the target, not a command line. Scanning it as roleHookCmd would
		// trip HOOK-001 on every query string that contains `&`. HOOK-003 is the check.
		return nil, []model.Finding{httpHookFinding(a, url)}
	}
	cmd := strings.TrimSpace(a.Hook.Command)
	if cmd == "" {
		return nil, nil
	}
	// The command lives inside JSON, so it has no meaningful line number: synthetic.
	units := []unit{{file: a.Path, text: cmd, role: roleHookCmd, synthetic: true}}

	// home is root's parent — the same anchor collect uses for the user-level config, which
	// keeps the scan hermetic and honours --root instead of reading the ambient environment.
	home := filepath.Dir(root)
	for _, ref := range scriptRefs(cmd) {
		path, why := resolveHookScript(root, home, ref)
		if why != "" {
			// Before declaring a gap, look inside the tree this hook SHIPS IN — see
			// resolveInOwnerRoot for why that is a lookup and not a guess.
			//
			// A hit is NOT read here, and that restraint is measured. The owner root is a plugin
			// tree, which is already scanned whole as its own artifact, so reading it again under
			// the hook duplicates findings rather than adding any: five hooks routed through one
			// runner.js took a fixture from 9 findings to 29 for the same three lines, and
			// EXEC-001 from 1 to 6. That is the inflation issue 008 refused, arriving by another
			// door. What the report gets instead is a note that names the file — because the old
			// note's claim was simply false, and a wrong coverage warning is worse than a coarse one.
			if p, ok := resolveInOwnerRoot(a.Hook.OwnerRoot, ref); ok {
				notes = append(notes, hookOwnedNote(a.Name, ref, relPath(root, p)))
				continue
			}
			notes = append(notes, hookRefNote(a.Name, ref, why))
			continue
		}
		if !inBoundary(home, path) {
			// Two statements, not one. COV-000 is the coverage gap (§16.2: we did not read
			// it). HOOK-002 is the risk: an unaudited second stage running silently on this
			// event. Coverage and scoring are different facts; collapsing them made a
			// colleague's /Applications hooks look like a footnote.
			notes = append(notes, hookRefNote(a.Name, ref, "it resolves outside HOME and was not read (§16.2)"))
			notes = append(notes, hookOutsideFinding(a, ref, path))
			continue
		}
		b, cov := readCapped(path, home)
		if cov != nil {
			notes = append(notes, *cov)
			continue
		}
		if b == nil {
			notes = append(notes, hookRefNote(a.Name, ref, "it could not be read"))
			continue
		}
		// A followed script is classified like any other file: every rule applies, and its
		// credential/network lines join the artifact's exfil chain (EXFIL-001/002).
		units = append(units, unit{file: path, text: string(b), role: roleForPath(path)})
	}
	return units, notes
}

// scriptRefs returns the distinct script paths a command mentions, in order of appearance.
func scriptRefs(cmd string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tok := range shellWords(cmd) {
		if tok == "" || seen[tok] || strings.Contains(tok, "://") {
			continue // empty, already seen, or a URL — not a local file
		}
		if !scriptExts[strings.ToLower(filepath.Ext(tok))] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// resolveHookScript turns a reference from a hook command into an existing path, or
// returns "" plus the reason it could not be followed (which becomes the coverage note —
// so the report says WHY a second stage went unread).
//
// $CLAUDE_PROJECT_DIR and ~ both expand to home: the scan's own anchor, never the ambient
// environment. A relative reference is tried against home first and the root second, which
// covers both shapes seen in practice (".claude/hooks/x.sh" from the project dir, and
// "hooks/x.sh" from inside the config root). Any other unexpanded variable is refused —
// guessing a path is how a scanner ends up reading the wrong file.
func resolveHookScript(root, home, ref string) (path, why string) {
	ref = strings.NewReplacer(
		"${CLAUDE_PROJECT_DIR}", home,
		"$CLAUDE_PROJECT_DIR", home,
		"${HOME}", home,
		"$HOME", home,
	).Replace(ref)
	if ref == "~" || strings.HasPrefix(ref, "~/") {
		ref = filepath.Join(home, strings.TrimPrefix(ref, "~"))
	}
	if strings.ContainsAny(ref, "$*?") {
		// Resolving this would mean expanding the environment or the glob — i.e. guessing.
		return "", "its path holds an unresolved variable or glob"
	}
	candidates := []string{ref}
	if !filepath.IsAbs(ref) {
		candidates = []string{filepath.Join(home, ref), filepath.Join(root, ref)}
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, ""
		}
	}
	return "", "no such file under the scanned root"
}

// resolveInOwnerRoot answers a narrower question than resolveHookScript: does the tail of this
// reference exist inside the tree the hook ships in?
//
// It is the difference between an expansion and a lookup. A plugin hook names its second stage
// through the plugin root — `_R="${CLAUDE_PLUGIN_ROOT}"; node "$_R/scripts/x.js"` — and expanding
// `$_R` would mean interpreting the shell, which this tool does not do and should not start doing
// (the assignment right above it has a `[ -z ]` fallback, so `_R` has two possible values and
// picking one IS a guess). But `scripts/x.js` either exists under the plugin's own directory or it
// does not, and that is checked, not assumed.
//
// Longest suffix wins, and a suffix of ONE segment is refused when the reference has more: without
// that, a bare `x.js` would match any same-named file anywhere in the tree, which is the guessing
// this function exists to avoid. Every candidate is confined to ownerRoot, a directory the plugin
// manifest already resolved and contained, so the worst outcome is attributing a file the scan had
// already read to the hook that runs it — never reading something outside.
func resolveInOwnerRoot(ownerRoot, ref string) (string, bool) {
	if ownerRoot == "" {
		return "", false
	}
	segs := strings.Split(filepath.ToSlash(ref), "/")
	// Drop empties so a leading "/" or a "//" does not produce a phantom segment.
	clean := make([]string, 0, len(segs))
	for _, s := range segs {
		if s != "" && s != "." {
			clean = append(clean, s)
		}
	}
	minLen := 2
	if len(clean) < 2 {
		minLen = 1
	}
	for start := 0; len(clean)-start >= minLen; start++ {
		tail := clean[start:]
		// A ".." inside the suffix could climb out of ownerRoot; Join cleans it, and the
		// containment check below is what actually decides.
		p := filepath.Join(ownerRoot, filepath.Join(tail...))
		if !inBoundary(ownerRoot, p) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, true
		}
	}
	return "", false
}

// A NOTE ON WHAT IS REDACTED IN THESE TWO NOTES, because they mix two kinds of string.
//
// The engine's convention, set by every rule finding: Evidence.File is a path or label the
// SCANNER produced and is clipped only; Evidence.Snippet is text out of the scanned FILE and is
// redacted, because it may carry a credential. These notes started on the wrong side of that
// line and it cost exactly the information they exist to supply — the resolved path
// `plugins/cache/thedotmack/claude-mem/10.5.2/scripts/worker-service.cjs` reached a real report
// as `<REDACTED>.5.<REDACTED>.cjs`, filename and all. entropyTokenRE is `[A-Za-z0-9+/=_-]{24,}`
// and `/` is inside that class, so a long path is ONE token with mixed letters and digits and
// entropy to match: to a heuristic that only reads shape, a path and a bespoke API key are the
// same thing. Over-redaction is the right default for file CONTENT and the wrong one here.
//
// So: the hook's own label and the resolved path are clipped, because the scanner built both.
// The reference copied out of the hook COMMAND is still redacted — a hook command is
// attacker-influenced text, and this environment has already been seen with an inline secret in
// a permission entry (PERM-001).

// hookRefPrefix is shared by the producer and the coalescer so the two can never drift on
// where the REASON starts. coalesceCoverageNotes strips it to recover the reason and count
// how many references failed for each one; without a shared constant that would be a prose
// match against a sentence somebody will eventually reword.
const hookRefPrefix = "A hook command invokes a script that was NOT scanned because "

// hookRefNoteTitle is shared with the coalescer for the same reason the prefix is.
const hookRefNoteTitle = "Hook script not followed (incomplete coverage)"

// hookRefNote reports a second stage the scan could not read. Dimension 0: it is a
// coverage gap, not a risk of its own.
//
// The Evidence carries the hook in File and the SCRIPT REFERENCE in Snippet, and both halves
// are load-bearing: one hook command routinely names several scripts (a runner plus the worker
// it runs), so the hook name alone cannot tell two of its own references apart. On a real
// machine that made 53 distinct notes render as 53 indistinguishable lines.
func hookRefNote(artifact, ref, why string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title:    hookRefNoteTitle,
		Why:      hookRefPrefix + why + ".",
		Evidence: []model.Evidence{{File: clip(artifact), Line: 0, Snippet: redactClip(ref)}},
	}
}

// HookOwnedNoteTitle is a DIFFERENT statement from hookRefNoteTitle, and the separate title is
// the point: it keeps the two counts apart in the coalescer, so "we could not find this at all"
// never gets averaged together with "we read this, just under another name". Exported for the
// report for the same reason: the script was read, so this note does not hedge "looks safe".
const HookOwnedNoteTitle = "Hook script attributed to its plugin, not to the hook (partial)"

// hookOwnedNote reports a second stage that WAS read — as part of the plugin tree the hook ships
// in — but whose findings are filed under the plugin rather than under the hook that runs it.
//
// This note exists because the one it replaces was false. A real ~/.claude produced 54 warnings
// saying a hook's script "was NOT scanned" while the same scan reported EXEC-001 and EXEC-009
// from those very files under the plugin artifact. Spec §12 asks that no gap be silent; a gap
// statement that overstates itself fails that in the other direction, and it costs more, because
// an operator who checks one and finds it wrong has no reason to believe the next fifty-three.
//
// What is genuinely lost is the ATTRIBUTION, and it matters: "this plugin contains a script that
// pipes curl into a shell" and "this hook runs that script silently on every tool call" are
// different sentences, and only the second one tells you how often it happens. The note names the
// file so a reader can make that connection by hand.
func hookOwnedNote(artifact, ref, resolved string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: HookOwnedNoteTitle,
		Why: "A hook command names its script through the plugin root, which cannot be expanded " +
			"without interpreting shell. The path was instead LOCATED inside the plugin's own tree, " +
			"which is scanned whole as one artifact — so the content was read and any findings in it " +
			"are filed under that plugin, subject to the coverage notes on it. Only the per-hook " +
			"attribution is missing: the findings do not say that a hook runs this on every " +
			"matching tool call.",
		Evidence: []model.Evidence{{File: clip(artifact), Line: 0,
			Snippet: clip(redactClip(ref) + " → " + resolved)}},
	}
}

// eventPermissionRequest is the Claude Code hook that sits on the authorization prompt.
// A second stage (or an HTTP endpoint) on this event takes the allow/deny decision, which
// is why HOOK-002/003 raise it above every other event.
const eventPermissionRequest = "PermissionRequest"

// hookOutsideFinding is HOOK-002: the second stage resolved outside HOME and was not read.
// Dimension 4, because an unaudited payload at a silent-execution intercept is a code-
// execution surface even though we refused to open the file. Spec §16.2 is unchanged —
// we still do not read it; we score the refusal.
//
// The snippet is redacted WHOLE, arrow and both sides, unlike hookOwnedNote's — the two look alike,
// so the difference is spelled out. resolved here is the reference itself after `~`/$HOME expansion
// (for an absolute reference, the very same bytes); it lies in no tree this scan walks, so it is text
// out of the hook COMMAND, not a path the scanner located. Redacting only the half before the arrow
// printed whatever the redactor had removed right after it — two exits where invariant #3 allows one.
// hookOwnedNote's resolved path is a file inside a plugin tree the scan reads whole, so it stays on
// the clipped-only side (see the note on what the hook notes redact). The cost lands only where
// Redact rewrites a path: one the entropy pass reads as an opaque token is now masked on both sides.
func hookOutsideFinding(a model.ArtifactReport, ref, resolved string) model.Finding {
	sev := model.SevMedium
	why := "This hook command names a script that resolves outside HOME, so the second stage was NOT read: the scanner never follows a path out of the home directory. Confirm what it is: hooks run silently on every matching event, and an unaudited payload at that interception point is a risk of its own, not just a coverage gap."
	if a.Hook.Event == eventPermissionRequest {
		sev = model.SevHigh
		why = "This PermissionRequest hook names a script that resolves outside HOME, so the second stage was NOT read: the scanner never follows a path out of the home directory. PermissionRequest hooks take the authorization decision; an unaudited payload here can allow or deny tool calls without the operator seeing either, so it is high."
	}
	return model.Finding{
		RuleID: "HOOK-002", Dimension: 4, Severity: sev, Source: model.SrcStatic,
		Title:    "Hook second stage is outside HOME and was not read",
		Why:      why,
		Evidence: []model.Evidence{{File: clip(a.Name), Line: 0, Snippet: redactClip(ref + " → " + resolved)}},
	}
}

// httpHookFinding is HOOK-003: the hook posts the full event payload (tool inputs, command
// lines, permission requests) to a URL. Loopback is a local sidecar — low, except on
// PermissionRequest, which still hands over the authorization decision. Anything else is
// the operation stream leaving the machine, and is high.
func httpHookFinding(a model.ArtifactReport, rawURL string) model.Finding {
	local := loopbackURL(rawURL)
	sev := model.SevHigh
	why := "This HTTP hook posts the full event payload (tool inputs, command lines, permission prompts) to a host that is not loopback. The destination is off this machine, so the environment's operation stream leaves with it."
	switch {
	case local && a.Hook.Event == eventPermissionRequest:
		why = "This PermissionRequest hook posts the authorization prompt to a loopback URL. The destination is local, but the hook still takes the allow/deny decision, so it is high — confirm the local service is one you intend to grant that power."
	case local:
		sev = model.SevLow
		why = "This HTTP hook posts event payloads to a loopback URL. The destination is on this machine, so the finding is a prompt to confirm the local sidecar, not a claim that data left."
	}
	return model.Finding{
		RuleID: "HOOK-003", Dimension: 4, Severity: sev, Source: model.SrcStatic,
		Title:    "Hook forwards event payload over HTTP",
		Why:      why,
		Evidence: []model.Evidence{{File: clip(a.Name), Line: 0, Snippet: redactClip(rawURL)}},
	}
}
