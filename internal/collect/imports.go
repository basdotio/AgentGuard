// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/redact"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// An instruction file can pull in other files with `@path` syntax, and those imports are expanded
// into context at launch alongside the file that referenced them. Following them is not a nicety:
// a CLAUDE.md whose entire body is a handful of `@` lines reads as almost empty while loading
// thousands of tokens of instructions the scan never saw. The payload hides one hop away.
//
// Three details from the documented behaviour drive this implementation, and getting any of them
// wrong produces a wrong answer rather than a partial one:
//
//   - Imports RECURSE, to a maximum of four hops. Stopping at one would leave the same hiding place
//     one level deeper.
//   - A relative path resolves against the file containing the import, NOT the working directory.
//   - Import parsing SKIPS code spans and fenced blocks. A backticked `@README` is literal text, so
//     treating it as an import would invent a file the agent never loads — a false finding attached
//     to a real path, which is worse than a miss.

// maxImportDepth mirrors the documented limit of four hops.
const maxImportDepth = 4

// maxImportBytes caps how much of a file is searched for imports. A hostile instruction file can be
// arbitrarily large; the import block is at the top in every real layout.
const maxImportBytes = 1 << 20

// importRE matches an `@path` reference. The path stops at whitespace, a closing paren or a
// backtick, so markdown link syntax and inline code both terminate it rather than being swallowed.
var importRE = regexp.MustCompile(`(?:^|[\s(])@([^\s()` + "`" + `]+)`)

// fenceRE matches a fenced code block, including the language tag and an unterminated final fence
// (a file that opens a fence and never closes it must not have the remainder treated as prose).
// Both CommonMark fence characters are covered, and runs longer than three: an author reaches for
// ~~~ or ```` precisely when the block contains backticks, so those are the blocks most likely to
// hold an `@path` that must stay literal.
var fenceRE = regexp.MustCompile("(?s)(?:```+.*?(?:```+|$)|~~~+.*?(?:~~~+|$))")

// spanRE matches an inline code span.
var spanRE = regexp.MustCompile("`[^`]*`")

// stripCode blanks out fenced blocks and inline spans so import scanning sees only prose. Content
// is replaced rather than deleted so that nothing on either side of a removed block gets joined
// into a token that was never written.
func stripCode(s string) string {
	s = fenceRE.ReplaceAllString(s, "\n")
	return spanRE.ReplaceAllString(s, " ")
}

// importRefs returns the raw `@path` references written in a file, in order, deduplicated.
func importRefs(path string) []string {
	b, err := safeio.ReadPrefix(path, maxImportBytes)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range importRE.FindAllStringSubmatch(stripCode(string(b)), -1) {
		ref := strings.TrimRight(m[1], ".,;:")
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// resolveImport turns a written reference into a filesystem path. `~/` expands against home — which
// here means the scan's home (the parent of root), so the resolution honours --root and a test or a
// project-scoped audit stays hermetic instead of reaching into the real user home.
func resolveImport(home, fromFile, ref string) string {
	switch {
	case strings.HasPrefix(ref, "~/"):
		return filepath.Join(home, ref[2:])
	case filepath.IsAbs(ref):
		return ref
	default:
		return filepath.Join(filepath.Dir(fromFile), ref)
	}
}

// sensitiveDirs are directories an instruction file must never pull this scanner into. The import
// boundary is HOME (a project CLAUDE.md legitimately imports siblings beside root), and HOME also
// holds the user's credentials — so a SINGLE attacker-controlled line, `@~/.ssh/id_ed25519`, was
// enough to make the scanner read a private key, publish its SHA-256 in a shareable report, and
// (with --llm) ship a redacted excerpt to a model endpoint. "Something appended a line to
// CLAUDE.md" is precisely the persistence this tool exists to detect, so it must not also be the
// trigger. Refused and reported, never read.
var sensitiveDirs = map[string]bool{
	".ssh": true, ".aws": true, ".gnupg": true, ".kube": true, ".docker": true,
	".config": true, ".password-store": true, ".netrc": true,
}

func hasSensitiveComponent(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if sensitiveDirs[seg] {
			return true
		}
	}
	return false
}

// credentialFile classifies an import target by NAME. sensitiveDirs refuses by directory;
// this refuses by file, because `@~/.env` sits in HOME itself. Measured:
// that one line made the scanner read .env, publish its real sha256 in the report as a second
// artifact, run every script rule over it (a `curl | bash` inside changed the environment
// score), and — with --llm — ship its whole content to the endpoint; notes were empty and the
// score 100. Refused here, none of that happens: a refused import creates no artifact, so no
// hash and nothing for the judge to read.
//
// Two tiers, following HOOK-002's "grade by consequence" precedent. High: names that hold a
// secret and nothing else (.env, private keys, credential stores). Medium: names that OFTEN
// hold only configuration (.pem is usually a public certificate; .npmrc usually only a
// registry). `.env.example` / `.sample` / `.template` are NOT credentials — they are the file
// an author ships to show the shape — so they are read and scanned like any other import;
// widening the match to `*env*` would hide a payload behind a familiar name, which is exactly
// the inversion refusal-by-name invites (see refusal finding below for the counterweight).
func credentialFile(name string) (model.Severity, bool) {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	for _, suf := range []string{".example", ".sample", ".template", ".dist"} {
		if strings.HasSuffix(lower, suf) {
			return "", false
		}
	}
	switch lower {
	case ".env", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa", "credentials", "credentials.json",
		".git-credentials", "secrets.yml", "secrets.yaml", ".netrc", ".htpasswd":
		return model.SevHigh, true
	case ".npmrc", ".pypirc":
		return model.SevMedium, true
	}
	if strings.HasPrefix(lower, ".env.") {
		return model.SevHigh, true
	}
	switch filepath.Ext(lower) {
	case ".key", ".p12", ".pfx", ".jks", ".keystore":
		return model.SevHigh, true
	case ".pem":
		return model.SevMedium, true
	}
	return "", false
}

// importRef is how every note below quotes an import: the `@path` as the instruction file wrote
// it, through the redactor. It is text copied out of a file body — the same bytes an engine rule
// matching that line would quote as <REDACTED> — and these four notes used to print it raw, so a
// token sitting in a directory name reached every rendering. Only the reference is redacted; the
// fixed tail a note appends is this package's own text.
func importRef(ref string) string { return redact.Secrets("@" + ref) }

// importCredentialFinding is the SCORING half of a refused credential import (dimension 3,
// EXFIL-005). The coverage note above says "I did not read it"; this says "the instruction
// file loads a credential into the agent's context", which is the first leg of exfiltration
// — that context leaves the machine by definition. Two sentences because they are two facts
// (HOOK-002 set the precedent: an unread hook script outside HOME is a COV-000 AND a scored
// finding). It also closes the inversion refusal-by-name opens: an artifact whose import was
// refused cannot come out at 100/100, so naming a payload `secrets.yaml` buys concealment
// from the reader but not a clean score.
func importCredentialFinding(from, ref string, sev model.Severity) model.Finding {
	return model.Finding{
		RuleID: "EXFIL-005", Dimension: 3, Severity: sev, Source: model.SrcStatic,
		Title: "Instruction file imports a credential into the agent's context",
		Why: "An @import in this instruction file resolves to a credential — a private key, a .env, a " +
			"credential store, or a file inside a credential directory. Claude Code expands imports into " +
			"context at launch, so the secret is read into every session and travels with whatever leaves it. " +
			"There is no legitimate reason for an instruction file to load a secret; the file itself was NOT " +
			"read by this scan (see the coverage note), so nothing here says what it contains.",
		Evidence: []model.Evidence{{File: from, Line: 0, Snippet: importRef(ref)}},
	}
}

func importSensitiveNote(from, ref string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevHigh, Source: model.SrcStatic,
		Title: "Instruction file imports a credential path, refused",
		Why: "An @import resolving to a credential — a file inside .ssh/.aws/.gnupg/…, or a file named like one " +
			"(.env, id_rsa, credentials, *.key, …) — was NOT read: no artifact, no hash, nothing sent to a judge. " +
			"An instruction file naming one is itself worth a look: it makes the scanner (and the agent) read " +
			"secrets on the author's behalf. Scored separately as EXFIL-005.",
		Evidence: []model.Evidence{{File: from, Line: 0, Snippet: importRef(ref) + " is a credential path"}},
	}
}

func importEscapeNote(from, ref string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
		Title: "Instruction import points outside the scanned tree, not read",
		Why: "An @import resolving outside HOME is loaded into context by Claude Code but was NOT scanned: " +
			"following it would read files the operator did not point this scan at.",
		Evidence: []model.Evidence{{File: from, Line: 0, Snippet: importRef(ref) + " escapes the scan boundary"}},
	}
}

func importDepthNote(from, ref string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Instruction import chain hit the depth limit (partial)",
		Why: "Imports are followed four hops, matching Claude Code's own limit; a reference beyond that was " +
			"NOT read. If this fires, the chain is deeper than the documented maximum and may not load either.",
		Evidence: []model.Evidence{{File: from, Line: 0, Snippet: importRef(ref) + " beyond depth " + itoa(maxImportDepth)}},
	}
}

// expandImports follows the import graph out from the already-collected instruction files and
// returns an artifact for each newly reached file.
//
// Containment matches the hook policy: a target outside home is reported as a coverage gap rather
// than read. A missing target is silently ignored — it loads nothing, so it is a stale reference for
// the hygiene pass to talk about, not a scanning gap.
// known are artifacts other collectors already returned; an import landing on one is skipped.
// Without this a CLAUDE.md saying "follow @rules/conventions.md" produced a SECOND artifact for a
// file rules/ had already collected, and every finding inside was reported twice.
//
// The first return value is `seeds` with any refusal findings ATTACHED (EXFIL-005 lands on the
// instruction file that wrote the import, since that is the artifact a reader can act on); the
// second is the newly reached files. Seeds are copied, never mutated in place.
func expandImports(home string, seeds, known []model.ArtifactReport) ([]model.ArtifactReport, []model.ArtifactReport, []model.Finding) {
	all := make([]model.ArtifactReport, len(seeds))
	copy(all, seeds)
	seen := map[string]bool{}
	realpath := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	for _, s := range seeds {
		seen[realpath(s.Path)] = true
	}
	for _, k := range known {
		seen[realpath(k.Path)] = true
	}

	var notes []model.Finding
	frontier := make([]int, len(all)) // indices into all, so findings attach to the real entry
	for i := range frontier {
		frontier[i] = i
	}

	for depth := 1; len(frontier) > 0; depth++ {
		var next []int
		for _, si := range frontier {
			f := all[si]
			for _, ref := range importRefs(f.Path) {
				real := realpath(resolveImport(home, f.Path, ref))
				if seen[real] {
					continue // already collected, or a cycle
				}
				// EXISTENCE IS CHECKED FIRST, before any boundary verdict. withinDir fails closed
				// when a path and its parents are absent, so ordering it earlier reported every
				// stale reference — `@notes/moved.md` after a rename, or a bundler alias like
				// `@/components/Button` — as "escapes the scan boundary", asserting that Claude Code
				// loads a file which does not exist. A missing target loads nothing: it is a stale
				// reference for the hygiene pass, not a scanning gap.
				fi, err := os.Stat(real)
				if err != nil || fi.IsDir() {
					continue
				}
				if depth > maxImportDepth {
					seen[real] = true
					notes = append(notes, importDepthNote(f.Path, ref))
					continue
				}
				if hasSensitiveComponent(real) {
					seen[real] = true
					notes = append(notes, importSensitiveNote(f.Path, ref))
					// .config holds far more than credentials, so a path through it is medium;
					// .ssh/.aws/.gnupg/.kube/.docker/.password-store/.netrc hold little else.
					sev := model.SevHigh
					for _, seg := range strings.Split(filepath.ToSlash(real), "/") {
						if seg == ".config" {
							sev = model.SevMedium
						}
					}
					all[si].Findings = append(all[si].Findings, importCredentialFinding(f.Path, ref, sev))
					continue
				}
				if sev, ok := credentialFile(real); ok {
					seen[real] = true
					notes = append(notes, importSensitiveNote(f.Path, ref))
					all[si].Findings = append(all[si].Findings, importCredentialFinding(f.Path, ref, sev))
					continue
				}
				if !withinDir(home, real) {
					seen[real] = true
					notes = append(notes, importEscapeNote(f.Path, ref))
					continue
				}
				seen[real] = true
				all = append(all, artifact(model.KindInstruction, "@"+ref, real, FileHash(real)))
				next = append(next, len(all)-1)
			}
		}
		if depth > maxImportDepth {
			break
		}
		frontier = next
	}
	return all[:len(seeds)], all[len(seeds):], notes
}
