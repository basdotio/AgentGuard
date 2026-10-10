// SPDX-License-Identifier: MIT
package detect

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
	"strconv"
	"sync"
)

// rulesEpoch is the part of the rules version that the rule table cannot see for itself (spec §5.1).
//
// The rules version covers DETERMINISTIC detection only: the findings overall is computed from,
// so that a report's overall can be recomputed against the rules that produced it. RulesVersion
// hashes builtinRules(), but a good share of deterministic detection lives outside that table:
// the structural and shape checks (the exfiltration chain, hooks, SUP-004/005/006,
// OBF-004/006/007), the role gate (roleAllows, roleForPath), the lexical layer (logical.go),
// comment handling, which files the reader opens, the credential-import check in
// internal/collect (EXFIL-005), and internal/permcheck.
//
// The LLM judge is outside the rules version, and so outside this epoch: nothing in
// internal/judge — prompts, grounding, consensus, severity clamping — is covered, and changing it
// does not bump this. The judge moves only overall_effective, and this value does not identify
// it: a --llm report names the judge in its own judge block — prompt_version and excerpt_version
// for the judge's code, model and samples for how it ran (P-031).
//
// BUMP IT, in the same commit, whenever a change outside builtinRules() alters — for some input —
// which deterministic findings are produced, or a finding's rule ID, dimension, severity or
// advisory flag. Do NOT bump it for titles, explanations, evidence formatting or score weights:
// those do not decide a finding, and a version that moves without the rules moving says "the
// rules changed" when they did not.
//
// Nothing enforces this mechanically. Hashing the source would move on every comment edit, which
// is the false alarm this value exists to avoid; the cost is that a forgotten bump lets two
// reports claim the same rules while different detection code produced them.
const rulesEpoch = 2 // 2: a plugin's skills, commands and agents are artifacts of their own and carry findings (P-044)

// rulesVersionLen is how many hex digits of the sha256 the version keeps.
const rulesVersionLen = 12

// rulesVersionOnce caches the built-in table's version: builtinRules() compiles every pattern,
// and the load-time gate runs a whole analysis on each skill load.
var rulesVersionOnce = sync.OnceValue(func() string { return rulesVersion(builtinRules(), rulesEpoch) })

// RulesVersion names the rule table this build detects with: the first 12 hex digits of a sha256
// over everything in builtinRules() that decides whether a finding fires and how heavily, plus
// rulesEpoch. It is what `scan --json` reports as rules_version, what `aguard version` prints,
// and what the docs/rules.md header carries.
//
// Two values that differ mean the rules changed. Two values that agree mean the rule table is the
// same; the deterministic detection code around it is the same only as far as rulesEpoch has been
// kept, and the judge is not covered at all.
func RulesVersion() string { return rulesVersionOnce() }

// rulesVersion is RulesVersion over an arbitrary table, so tests can mutate one.
//
// What goes in is what changes a verdict: ID, dimension, severity, the advisory and *Only flags,
// and both patterns' source. Title, Why and Ref do not — they explain a finding, they do not
// decide one. Rules are taken in ENGINE order, not sorted: findings on one line are appended in
// rule order, so a reorder can change a report and should move the version. Every field is
// length-prefixed because a pattern may contain any byte, so no separator is safe.
func rulesVersion(rules []Rule, epoch int) string {
	h := sha256.New()
	writeField(h, strconv.Itoa(epoch))
	writeField(h, strconv.Itoa(len(rules)))
	for _, r := range rules {
		writeField(h, r.ID)
		writeField(h, strconv.Itoa(r.Dimension))
		writeField(h, string(r.Severity))
		writeField(h, flagBits(r.Advisory, r.HookOnly, r.ConnectorOnly, r.RawOnly, r.ScriptOnly))
		writeField(h, patternSource(r.re))
		writeField(h, patternSource(r.except))
	}
	return hex.EncodeToString(h.Sum(nil))[:rulesVersionLen]
}

// writeField appends one length-prefixed field; hash.Hash writes never fail.
func writeField(h hash.Hash, s string) {
	_, _ = fmt.Fprintf(h, "%d:%s;", len(s), s)
}

// flagBits renders the boolean flags in a fixed order as "0"/"1" digits.
func flagBits(flags ...bool) string {
	b := make([]byte, len(flags))
	for i, f := range flags {
		b[i] = '0'
		if f {
			b[i] = '1'
		}
	}
	return string(b)
}

// patternSource is a compiled pattern's source text, or "" for none (a rule without a veto).
func patternSource(re *regexp.Regexp) string {
	if re == nil {
		return ""
	}
	return re.String()
}
