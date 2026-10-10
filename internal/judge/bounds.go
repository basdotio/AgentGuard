// SPDX-License-Identifier: MIT
package judge

// maxEvidenceBytes bounds one static finding's evidence as the judge sends it: a triage item's
// `file:line snippet` (P-037). The snippet was clipped at detect time and the file position never was,
// so a deep enough path sent as much as the path held. A real `file:line snippet` is a few hundred
// bytes; the bound leaves room for a long path and a whole clipped snippet.
const maxEvidenceBytes = 1000

// boundedEvidence cuts already-redacted evidence to maxEvidenceBytes on a character boundary, the
// ellipsis included. The caller redacts FIRST (invariant #3), so a token straddling the cut is already
// <REDACTED> and leaves no head behind.
func boundedEvidence(red string) string {
	if len(red) <= maxEvidenceBytes {
		return red
	}
	return capBytes(red, maxEvidenceBytes-len(ellipsis))
}
