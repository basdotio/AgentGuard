// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// ReachKind is the answer to "may the scanner read this file of a tree?" (invariant #2, P-056).
type ReachKind int

const (
	// Read: the file resolves inside its boundary; hash it, scan it, judge it.
	Read ReachKind = iota
	// Unresolved: a link whose text stays inside the boundary but which does not resolve (dangling, a
	// loop). Present and unread — what the hash's unreadable mark and the unreadable note describe.
	Unresolved
	// Outside: the file resolves outside its boundary, or, unresolvable, its link text points there.
	// Never opened; the refusal is scored (SCOPE-001) and folded into the hash.
	Outside
	// Credential: the tree's SKILL.md resolves to a credential path. Never opened; scored like an
	// @import of one (EXFIL-005), because Claude Code would load the secret as the skill.
	Credential
)

// Reach is one verdict; Severity is set for Credential.
type Reach struct {
	Kind     ReachKind
	Severity model.Severity
}

// FileReach is THE predicate behind every read of a tree artifact's files: the tree hash, the static
// tree read, the judge's excerpts and hygiene ask it, so the four cannot disagree about which files an
// artifact is made of. Before P-056 they did: the hash and the rules skipped a SKILL.md symlinked out of
// its skill in silence (every such skill hashed as empty input), the judge read it from anywhere, and
// a dangling link was "inside" to the hash and "outside" to the rules.
//
// root is the tree's own root; p is a file under it, as the walk lists it. Every file must resolve
// inside root (spec §16.2), with ONE exception: the root's SKILL.md — the file Claude Code 2.1.107's
// loader reads, following any symlink with no containment — may resolve inside SkillMDBound(root),
// unless it names a credential. A link that cannot be resolved is placed by its link text.
func FileReach(root, p string) Reach {
	skillMD := filepath.Dir(p) == filepath.Clean(root) && filepath.Base(p) == "SKILL.md"
	bounds := []string{root}
	if skillMD {
		if outer := SkillMDBound(root); outer != "" {
			bounds = append(bounds, outer)
		}
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		lex, ok := linkText(p)
		if !ok {
			return Reach{Kind: Unresolved} // not a link: an unreadable parent, a vanished entry
		}
		for _, b := range bounds {
			if lexicallyWithin(b, lex) {
				return Reach{Kind: Unresolved}
			}
		}
		return Reach{Kind: Outside}
	}
	if skillMD && isSymlink(p) {
		if sev, cred := CredentialPath(real); cred {
			return Reach{Kind: Credential, Severity: sev}
		}
	}
	for _, b := range bounds {
		if withinDir(b, real) {
			return Reach{Kind: Read}
		}
	}
	return Reach{Kind: Outside}
}

// SkillMDRefused reports whether dir's SKILL.md is one the scanner must not open (outside its
// boundary, or a credential): the judge and hygiene open SKILL.md directly and ask this first. A
// missing or unresolvable SKILL.md is not refused — opening it simply fails, as it always did.
func SkillMDRefused(dir string) bool {
	k := FileReach(dir, filepath.Join(dir, "SKILL.md")).Kind
	return k == Outside || k == Credential
}

// SkillMDBound is the wider boundary a skill's SKILL.md may resolve into: the skills directory that
// holds the skill — the resolved root's parent, when it is named `skills` — or, when that directory is
// a plugin's (its parent carries the plugin manifest), the plugin root, the container P-044 bounds a
// plugin's children by. Anything else gets no widening ("").
//
// Derived from the path alone, so `scan`, `check`, the gate, `aguard hash` and `clean` — which all
// hash a skill by its directory — compute the same identity. Measured on a real ~/.claude (P-056):
// 41 skills installed this way, every target inside `<root>/skills`.
func SkillMDBound(root string) string {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	parent := filepath.Dir(real)
	if filepath.Base(parent) != "skills" {
		return ""
	}
	if regularFile(filepath.Join(filepath.Dir(parent), pluginManifest)) {
		return filepath.Dir(parent)
	}
	return parent
}

// CredentialPath classifies a resolved path the way an @import target is classified: through a
// credential directory (.ssh, .aws, …; medium through .config) or named like a credential file. One
// definition, so a skill's SKILL.md and an instruction file's import are refused by the same list.
func CredentialPath(real string) (model.Severity, bool) {
	if hasSensitiveComponent(real) {
		for _, seg := range strings.Split(filepath.ToSlash(real), "/") {
			if seg == ".config" {
				return model.SevMedium, true
			}
		}
		return model.SevHigh, true
	}
	return credentialFile(real)
}

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// linkText returns where the link at p points, made absolute against p's directory, lexically.
func linkText(p string) (string, bool) {
	if !isSymlink(p) {
		return "", false
	}
	t, err := os.Readlink(p)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(p), t)
	}
	return filepath.Clean(t), true
}

// lexicallyWithin compares without resolving (the target does not resolve); base is tried as spelled
// and resolved, since p's directory may be spelled either way.
func lexicallyWithin(base, p string) bool {
	in := func(b string) bool {
		rel, err := filepath.Rel(b, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if in(filepath.Clean(base)) {
		return true
	}
	if rb, err := filepath.EvalSymlinks(base); err == nil {
		return in(rb)
	}
	return false
}
