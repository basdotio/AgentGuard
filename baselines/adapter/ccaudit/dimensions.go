// SPDX-License-Identifier: MIT

package ccaudit

import "sort"

// DimensionMap translates cc-audit's own finding categories into the corpus vocabulary. It is a
// copy of `dimension_map` in the corpus's taxonomy/tools.yaml, kept in step by hand, because the
// corpus forbids itself from reading any tool's rule reference and we do not vendor its taxonomy.
//
// # How these were decided
//
// Not by reading the category names, which is how the first draft got two of them wrong. Each
// mapping was MEASURED against the corpus's own labels: for the 159 read-basis malicious samples
// where cc-audit reported a category, cross-tabulate its categories against the truth dimensions
// and take the LIFT over the base rate — P(dimension | category) ÷ P(dimension) — not the raw
// share. Raw share is worthless here: cc-audit emitted 5,612 `supplychain` findings across the
// corpus, so that category co-occurs with everything, and its largest raw overlap is `execution`
// purely because `execution` is common. Lift removes that.
//
//	category              top lift                              mapping
//	persistence           backdoor      4.33                    backdoor
//	overpermission        permission    3.42                    permission
//	promptinjection       injection     2.83                    injection
//	supplychain           supply-chain  2.38 (execution 2.28)    supply-chain
//	exfiltration          exfiltration  1.96                    exfiltration
//	obfuscation           execution 2.32, exfiltration 1.87     none
//	privilegeescalation   filesystem    1.50 (n=9), spread      none
//	secretleak            n=1 on read-basis samples             none
//
// The five above the line are the tool's own name winning on its own evidence, which is the
// result one hopes for and does not get for free: `supplychain` beat `execution` by 0.10, so it
// is the weakest of the five and worth re-measuring if the corpus grows.
//
// # The three that map to nothing, and why that is a result rather than a gap
//
// The corpus is explicit that a null value "is not a gap to be filled — it is a real and
// interesting outcome. A scanner whose only finding on a base64-wrapped reverse shell is
// 'Obfuscation' passes part 1 and fails part 2: it detected that something was hidden, not that
// there was a backdoor."
//
//   - obfuscation — the corpus's own worked example, and the measurement agrees: two dimensions
//     share the signal (execution 2.32, exfiltration 1.87) with neither dominant. aguard's own
//     Obfuscation maps to nothing for the same reason, so this is symmetric, not a penalty.
//   - privilegeescalation — the interesting one. It fires on 61 of the 159 samples, more than any
//     other category here, and NOTHING reaches the 1.5 bar: its best is filesystem at 1.50 with
//     n=9, then permission 1.42, then injection 1.38. A category that common carrying that little
//     information about the kind of attack is exactly what part 2 of the measurement exists to
//     surface. Mapping it to `permission` on the strength of its name would have credited the
//     tool for naming a kind the evidence says it did not name.
//   - secretleak — unmeasurable, not judged: it appears on ONE read-basis malicious sample, so
//     there is no evidence either way. Null is the conservative reading (a finding that a
//     credential is present is not a finding that it leaves the machine) and it moves the
//     published number by at most one sample. Revisit if the corpus grows.
var DimensionMap = map[string]string{
	"exfiltration":        "exfiltration",
	"promptinjection":     "injection",
	"overpermission":      "permission",
	"supplychain":         "supply-chain",
	"persistence":         "backdoor",
	"obfuscation":         "",
	"privilegeescalation": "",
	"secretleak":          "",
}

// flags reports whether a finding of this severity carries the flag at this tier. The default
// tier reports errors only, and cc-audit's SARIF reporter puts Critical and High there; --strict
// adds medium and low. Dimensions are taken only from findings that FLAGGED, mirroring the aguard
// adapter: a dimension attributed from a finding below the gate would credit the tool for naming
// a kind in a report the gate never showed anyone.
func (t Tier) flags(severity string) bool {
	switch severity {
	case "critical", "high":
		return true
	case "medium", "low":
		return t == TierStrict
	}
	return false
}

// dimensionsOf is the corpus-vocabulary kinds cc-audit named, from the findings that flagged.
// Sorted, so a verdict file is stable across runs.
func dimensionsOf(res ScanResult, tier Tier) []string {
	seen := map[string]bool{}
	for _, f := range res.Finding {
		if !tier.flags(f.Severity) {
			continue
		}
		if d := DimensionMap[f.Category]; d != "" {
			seen[d] = true
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
