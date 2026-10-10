// SPDX-License-Identifier: MIT
package score

import (
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// A plugin and its children (P-044).
//
// collect emits a plugin's skills, commands and agents as artifacts of their own, each with Plugin set
// to the plugin artifact's Name, next to the plugin tree artifact that the static rules read whole. The
// same bytes are therefore read twice, and every consumer that must not count them twice asks this
// file, so the answers cannot drift apart:
//
//   - Apply averages the environment over UNITS — a plugin with its children is one entry — so looking
//     closer at a plugin cannot raise the score (issues/008's 86 → 97; P-044 measured 94 → 98 and
//     88 → 98 when children were averaged like hooks);
//   - the human renderers and SARIF print a child's finding its plugin row already shows once, on the
//     plugin row, and the judge does not triage it a second time (ShownByPlugin);
//   - the gate's session start, Summarize and hygiene skip children (Parent ≥ 0).
//
// The link is the collector's Plugin field, never the " (plugin …)" name suffix, which comes out of a
// config key a plugin author controls.

// Family maps each artifact of one result to its plugin, if it is a plugin's child.
type Family struct {
	arts   []model.ArtifactReport
	parent []int
}

// Families links the children in arts to their plugin artifact: the plugin artifact whose Name the
// child's Plugin names, and, when several plugin artifacts share that name, the one whose path holds
// the child's. A child whose plugin is not in arts is an artifact of its own. Build it where it is used:
// it reads the findings arts holds at that moment.
func Families(arts []model.ArtifactReport) Family {
	f := Family{arts: arts, parent: make([]int, len(arts))}
	byName := map[string][]int{}
	for i, a := range arts {
		f.parent[i] = -1
		if a.Kind == model.KindPlugin {
			byName[a.Name] = append(byName[a.Name], i)
		}
	}
	for i, a := range arts {
		if a.Plugin == "" || a.Kind == model.KindPlugin {
			continue
		}
		cands := byName[a.Plugin]
		for _, c := range cands {
			if len(cands) == 1 || holds(arts[c].Path, a.Path) {
				f.parent[i] = c
				break
			}
		}
	}
	return f
}

// holds reports whether path is dir or lies under it, lexically.
func holds(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Parent is the index of artifact i's plugin, or -1 when i is not a plugin's child.
func (f Family) Parent(i int) int {
	if i < 0 || i >= len(f.parent) {
		return -1
	}
	return f.parent[i]
}

// Child reports whether artifact i is a plugin's child.
func (f Family) Child(i int) bool { return f.Parent(i) >= 0 }

// ShownByPlugin reports whether finding x of artifact i is already shown on its plugin's row: i is a
// plugin's child, x is deterministic, and the plugin carries a deterministic finding of the same rule
// whose first evidence names the same file and line. That is the one test of "the plugin row already
// shows this" — the renderers, SARIF and the judge's triage read it, never a copy. A finding on a file
// the plugin row does not name (the copy that loads, where the tree walk met a mirror first) is not
// shown by it.
func (f Family) ShownByPlugin(i int, x model.Finding) bool {
	p := f.Parent(i)
	if p < 0 || !Deterministic(x) {
		return false
	}
	file, line := firstEvidence(x)
	for _, y := range f.arts[p].Findings {
		if y.RuleID != x.RuleID || !Deterministic(y) {
			continue
		}
		if yf, yl := firstEvidence(y); yf == file && yl == line {
			return true
		}
	}
	return false
}

func firstEvidence(x model.Finding) (string, int) {
	if len(x.Evidence) == 0 {
		return "", 0
	}
	return x.Evidence[0].File, x.Evidence[0].Line
}

// units is the environment average's entries: each artifact that is not a plugin's child, with its
// children's findings joined to a plugin's own. A new slice per unit; arts is not modified.
func (f Family) units() [][]model.Finding {
	out := make([][]model.Finding, 0, len(f.arts))
	at := make([]int, len(f.arts))
	for i, a := range f.arts {
		at[i] = -1
		if f.Child(i) {
			continue
		}
		at[i] = len(out)
		out = append(out, append([]model.Finding(nil), a.Findings...))
	}
	for i, a := range f.arts {
		if p := f.Parent(i); p >= 0 {
			out[at[p]] = append(out[at[p]], a.Findings...)
		}
	}
	return out
}

// UnitScores lists the entries the environment average runs over: for each, the index of its lead
// artifact (the artifact itself, or the plugin of a plugin with its children) and its deterministic
// score — the artifact's Score as Apply set it, or, for a plugin with children, the score of their
// findings together. The report's "worst single item" line reads it so that it names the same
// entries, and the same count, the headline averages.
func UnitScores(arts []model.ArtifactReport) (leads, scores []int) {
	f := Families(arts)
	units := f.units()
	hasChildren := map[int]bool{}
	for i := range arts {
		if p := f.Parent(i); p >= 0 {
			hasChildren[p] = true
		}
	}
	for i, a := range arts {
		if f.Child(i) {
			continue
		}
		s := a.Score
		if hasChildren[i] {
			s = findingsScore(units[len(leads)], Deterministic)
		}
		leads = append(leads, i)
		scores = append(scores, s)
	}
	return leads, scores
}
