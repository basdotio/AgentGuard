// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"strings"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// noPassKinds are the artifact kinds planFor has no pass for, in the order the note names them.
// For an artifact of one of these the judge plans no question — at most a triage call, whose labels
// are display-only and never reach --fail-on-llm — so a run over it "answers every question it
// planned" without having read a byte of it. That used to be silent: no task, no LLM-000, and the
// gate's exit 0 read as "looked and found nothing" (P-038).
//
// The table must follow planFor's switch: TestAsksNothingOf_FollowsPlanFor runs planFor over a
// fixture of every kind declared in model.go and fails on a kind the two disagree about. Give one
// of these kinds a pass and it leaves the table in the same commit. KindPermission is in neither
// set by design: an allow list is configuration whose risk permcheck decides, and the scripts it
// names are followed by the static rules — there is no question the judge is meant to ask of it.
var noPassKinds = []model.ArtifactKind{model.KindPlugin, model.KindDirectory, model.KindQuarantined}

// AsksNothingOf reports whether the judge has no question for content of this kind. The LLM-000
// note below and the --fail-on-llm gate (cmd/aguard failGate) both read it, so what the report
// discloses and what the exit code says cannot disagree.
func AsksNothingOf(kind model.ArtifactKind) bool {
	for _, k := range noPassKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// maxUnaskedLabels bounds how many artifacts the note names; the counts per kind cover the rest.
const maxUnaskedLabels = 3

// unaskedNote collapses every artifact the judge asks nothing about into one LLM-000 (invariant #5:
// an omission is never silent), counted per kind, naming the first few by the same redacted
// "kind:name" label the other judge notes use.
func unaskedNote(arts []model.ArtifactReport) (model.Finding, bool) {
	count := map[model.ArtifactKind]int{}
	var labels []string
	total := 0
	for _, a := range arts {
		if !AsksNothingOf(a.Kind) {
			continue
		}
		count[a.Kind]++
		total++
		if len(labels) < maxUnaskedLabels {
			labels = append(labels, detect.Redact(string(a.Kind)+":"+a.Name))
		}
	}
	if total == 0 {
		return model.Finding{}, false
	}
	var kinds []string
	for _, k := range noPassKinds {
		if count[k] > 0 {
			kinds = append(kinds, fmt.Sprintf("%s (%d)", k, count[k]))
		}
	}
	if total > len(labels) {
		labels = append(labels, fmt.Sprintf("and %d more", total-len(labels)))
	}
	return coverageNote(fmt.Sprintf(
		"the judge asked nothing about %d artifact(s) of a kind it has no pass for — %s: no question about "+
			"their content was put to the model, and their static findings, where they have any, were only triaged (%s). "+
			"The static scan read them as usual.",
		total, strings.Join(kinds, ", "), strings.Join(labels, "; "))), true
}
