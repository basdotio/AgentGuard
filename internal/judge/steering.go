// SPDX-License-Identifier: MIT
package judge

import "fmt"

// The result pass (P-067) asks one question of a skill's whole instruction text: does a step it
// presents as required change what the user receives? It is a question neither of the two skill
// passes asks — the intent pass compares the description with the scripts, the injection pass
// looks for a directive beyond the declared purpose — and P-066 showed that folding it into the
// injection question as one more clause is not asking it: the model never once named the row it
// was given (0 of 840 trees). So it is its own pass, with its own prompt (steeringTask) and table.
//
// Its text is everything the agent reads as instructions: the SKILL.md body first, then the
// documents the body names, each under a `# <file>` header, within the excerpt budget. The units
// are the same ones the injection and companion passes ground against, so a quote cites the same
// file:line it would there; what the budget leaves out is disclosed through boundedNote
// (invariant #5), like the other fitted fields.

// steeringExcerpt assembles the result pass's text from the body unit and the companion units, and
// says how many documents did not fit. Nothing to read (no body) means no pass.
func steeringExcerpt(body sourceUnit, companions []sourceUnit) (string, []sourceUnit, string) {
	if body.text == "" {
		return "", nil, ""
	}
	units := append([]sourceUnit{body}, companions...)
	files, texts := make([]string, len(units)), make([]string, len(units))
	for k, u := range units {
		files[k], texts[k] = u.file, u.text
	}
	text, sent, left := fitUnits(texts, maxExcerptBytes, headed(files))
	if len(sent) == 0 {
		return "", nil, ""
	}
	units = units[:len(sent)]
	for k := range sent {
		units[k].text = sent[k]
	}
	shortened := ""
	if left > 0 {
		shortened = fmt.Sprintf("%d of %d named document(s) past the %d-byte excerpt not sent", left, len(companions), maxExcerptBytes)
	}
	return text, units, shortened
}
