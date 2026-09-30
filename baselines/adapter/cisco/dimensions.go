// SPDX-License-Identifier: MIT

package cisco

import "sort"

// DimensionMap translates skill-scanner's ThreatCategory values (models.py:39-59, 17 of them)
// into the corpus vocabulary. A hand copy of `dimension_map` in the corpus's taxonomy/tools.yaml.
//
// # This table is NOT measured, and says so
//
// cc-audit's map was decided by lift over the corpus's own labels, and that method caught two
// name-based guesses that were wrong. The same script here had 23 samples to work with — strict
// mode refuses every MCP tree, and on the skills surface only 23 read-basis malicious samples got
// a HIGH+ finding with a category. No lift on n ≤ 10 is trustworthy; the largest, data_exfiltration
// → execution at 2.42, is n=6 and points away from the category's own name.
//
// The five categories whose names are unambiguous are
// mapped BY NAME, each marked as such in the corpus file, and the other twelve are left null with
// "unmeasured, not guessed". This is a weaker basis than cc-audit's and is labelled as one; recompute
// by lift when the corpus is large enough on the skills surface.
var DimensionMap = map[string]string{
	// by name — n too small to measure (2026-09-24)
	"prompt_injection":    "injection",
	"data_exfiltration":   "exfiltration",
	"command_injection":   "execution",
	"supply_chain_attack": "supply-chain",
	"resource_abuse":      "resource-abuse",
	// unmeasured and not guessed: null is the corpus's own word for "maps to no dimension of ours"
	"unauthorized_tool_use":  "",
	"obfuscation":            "",
	"hardcoded_secrets":      "",
	"social_engineering":     "",
	"policy_violation":       "",
	"malware":                "",
	"harmful_content":        "",
	"skill_discovery_abuse":  "",
	"transitive_trust_abuse": "",
	"autonomy_abuse":         "",
	"tool_chaining_abuse":    "",
	"unicode_steganography":  "",
}

// dimensionsOf is the corpus-vocabulary kinds named by findings that FLAGGED — HIGH and CRITICAL,
// the rungs `--fail-on-severity high` fires on. Mirrors the aguard and cc-audit adapters: a kind
// named only in a report the gate never showed anyone does not count.
func dimensionsOf(res ScanResult) []string {
	seen := map[string]bool{}
	for _, f := range res.Findings {
		switch f.Severity {
		case "HIGH", "CRITICAL":
			if d := DimensionMap[f.Category]; d != "" {
				seen[d] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
