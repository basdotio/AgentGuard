// SPDX-License-Identifier: MIT
package model

// Dimension names (spec §3). Findings carry the dimension as a bare int because that is what
// scoring arithmetic needs — max within a dimension, sum across them — but a number is not
// something to show a reader, and until now the names existed only in prose (README, docs).
// Anything rendering a dimension for humans must resolve it here, so a renamed dimension can
// never mean two different things in two outputs.
//
// 0 is not a risk dimension: it is the scan's own coverage/parse notes, which never score
// (invariant #5 — every omission is disclosed, none of them moves the number).
var dimensionNames = map[int]string{
	0:  "Scan coverage (never scored)",
	1:  "Prompt injection",
	2:  "Excessive permissions",
	3:  "Data exfiltration",
	4:  "Code execution",
	5:  "Supply chain",
	6:  "Obfuscation",
	7:  "Backdoor",
	8:  "Resource abuse",
	9:  "Filesystem",
	10: "Intent mismatch",
}

// DimensionCount is the number of scored risk dimensions (1..DimensionCount).
const DimensionCount = 10

// DimensionName returns the human-readable name of a dimension, or "unknown" for a value
// outside 0..DimensionCount. It never returns an empty string: a blank column in a report
// would read as "this finding has no category" rather than "this build is inconsistent".
func DimensionName(d int) string {
	if n, ok := dimensionNames[d]; ok {
		return n
	}
	return "unknown"
}
