// SPDX-License-Identifier: MIT
package detect

import (
	"regexp"

	"github.com/basdotio/agent-guard/internal/model"
)

// Rule is one detection rule (spec §5.1). Rules are re-derived from OWASP Agentic
// Top 10 / MITRE ATLAS (Ref cites the basis) — NOT ported from other tools.
type Rule struct {
	ID        string
	Dimension int // 1..10 (spec §3)
	Severity  model.Severity
	Title     string
	Why       string
	Ref       string         // OWASP/MITRE basis
	re        *regexp.Regexp // compiled matcher (line-oriented)
	Advisory  bool           // dim 7/8: static can only hint, not confirm (spec §16.6)
	// HookOnly restricts a rule to hook COMMAND lines (roleHookCmd). Some constructs are
	// ordinary in a script but meaningful in a hook, which runs shell silently on every
	// matching tool call — running such a rule everywhere would drown it in noise.
	HookOnly bool
	// ConnectorOnly restricts a rule to remote-connector tool descriptions (roleToolDesc). A
	// description is text the model reads to decide when and how to call a tool, written by the
	// server; the shapes below are only meaningful there — in a script or a SKILL.md the same
	// words are the author talking to a reader, not a tool talking to the model.
	ConnectorOnly bool
	// RawOnly restricts a rule to the line AS WRITTEN, never the normalized copy.
	//
	// Needed because normalization does two different things. STRIPPING (invisible characters) can
	// only ever remove a match, so consulting the normalized copy second is safe — that is what lets
	// INJ-004 keep reporting the characters it exists to report. FOLDING (homoglyphs → ASCII)
	// SUBSTITUTES, and a substitution can CREATE a match that the file does not contain.
	//
	// Measured on a real machine, and the false positive was self-inflicted: a plugin shipped Greek,
	// Russian and Ukrainian translation files, the confusables table folded the SUBSET of their letters
	// it knows (ο→o, τ→t, ρ→p) and left the rest, so `Σύντομος` became `Σύntomoς` — ASCII and Greek
	// inside one token, which is precisely what OBF-005 looks for. 45 findings on ordinary translated
	// prose, in three files. A rule about what the ORIGINAL text mixes must read the original text.
	RawOnly bool
	// ScriptOnly restricts a rule to code that RUNS (roleScript, roleHookCmd), never to an
	// instruction file. Some shapes are an action in a script and a lesson in a document: a hook
	// script that prints {"permissionDecision":"allow"} is approving tool calls, while fifteen real
	// SKILL.md files carry the same JSON in a code block to explain how hooks work.
	ScriptOnly bool
	// except, when set, vetoes a match: a line the rule matches is dropped if this also matches.
	// It exists because RE2 has no lookahead, and "a URL that is NOT the official endpoint" cannot
	// be written as one positive pattern. Applied to the same view (raw or norm) that matched.
	except *regexp.Regexp
}

// rule is a small constructor that compiles the pattern (case-insensitive, multiline off —
// we feed one line at a time).
func rule(id string, dim int, sev model.Severity, pattern, title, why, ref string) Rule {
	return Rule{ID: id, Dimension: dim, Severity: sev, Title: title, Why: why, Ref: ref,
		re: regexp.MustCompile("(?i)" + pattern)}
}

// Rules exposes the built-in rule set for documentation generation (hack/gen-rules keeps
// docs/rules.md in step with the code, and CI fails if the two drift). It returns the same
// slice the engine uses, so a rule cannot exist in one and not the other — the alternative,
// a hand-written catalogue, is stale the first time someone adds a rule, and a stale rule
// reference for a security tool is worse than none.
func Rules() []Rule { return builtinRules() }

func (r Rule) advisory() Rule { r.Advisory = true; return r }

func (r Rule) hookOnly() Rule { r.HookOnly = true; return r }

func (r Rule) connectorOnly() Rule { r.ConnectorOnly = true; return r }

func (r Rule) rawOnly() Rule    { r.RawOnly = true; return r }
func (r Rule) scriptOnly() Rule { r.ScriptOnly = true; return r }

// exceptWhen adds a veto pattern (case-insensitive, like the main one); see Rule.except.
func (r Rule) exceptWhen(pattern string) Rule {
	r.except = regexp.MustCompile("(?i)" + pattern)
	return r
}

// matchLine reports whether the rule fires on a single (already-trimmed) line.
func (r Rule) matchLine(line string) bool {
	if !r.re.MatchString(line) {
		return false
	}
	return r.except == nil || !r.except.MatchString(line)
}

// matches reports whether the rule fires on a logical line. The RAW text is tried first and
// the normalized text only if that missed — order matters, not performance: a rule whose whole
// job is to report what normalization removes (INJ-004, invisible characters) must see the
// characters. Since normalizing only ever deletes, consulting it second can add a finding and
// can never take one away.
func (r Rule) matches(ll logicalLine) bool {
	if r.matchLine(ll.raw) {
		return true
	}
	// A RawOnly rule stops here. Folding can invent the very pattern such a rule looks for, so
	// consulting the normalized copy would report a mix the file does not contain — see RawOnly.
	if r.RawOnly {
		return false
	}
	return ll.norm != ll.raw && r.matchLine(ll.norm)
}
