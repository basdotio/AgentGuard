// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"strings"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
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

// Unasked reports, per artifact of arts, whether the judge asks nothing about it: its kind has no pass
// (AsksNothingOf) and, for a plugin, none of its children (score.Families, P-044) is of a kind that has
// one. A plugin's skills, commands and agents are artifacts of their own with their kind's questions, so
// a plugin holding one of them is answered through it; a plugin holding none is still asked nothing.
// The LLM-000 note below and the --fail-on-llm gate (cmd/aguard unaskedTarget) both read this, so what
// the report discloses and what the exit code says cannot disagree. Like AsksNothingOf it goes by kind:
// a child of a judged kind with nothing to send counts as judged (P-038's decision 1).
func Unasked(arts []model.ArtifactReport) []bool {
	out := make([]bool, len(arts))
	judgedChild := map[int]bool{}
	fam := score.Families(arts)
	for j, a := range arts {
		if p := fam.Parent(j); p >= 0 && !AsksNothingOf(a.Kind) && a.Kind != model.KindPermission {
			judgedChild[p] = true
		}
	}
	for i, a := range arts {
		out[i] = AsksNothingOf(a.Kind) && !(a.Kind == model.KindPlugin && judgedChild[i])
	}
	return out
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
	unasked := Unasked(arts)
	for i, a := range arts {
		if !unasked[i] {
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
		switch {
		case count[k] > 0 && k == model.KindQuarantined:
			// Moved out of the load path by `clean`: counted so the omission is not silent, labelled so
			// it does not read as a loaded gap (issues/023, direction D).
			kinds = append(kinds, fmt.Sprintf("%s (%d, no longer loaded)", k, count[k]))
		case count[k] > 0:
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
