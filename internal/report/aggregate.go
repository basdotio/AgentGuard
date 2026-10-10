// SPDX-License-Identifier: MIT
package report

import (
	"sort"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// Group is one aggregated risk row: findings sharing (artifact, rule) folded into a
// single entry with a hit count + up to 3 representative evidence lines. THE single
// source of truth for both the terminal and HTML reports (so the two never drift).
type Group struct {
	Severity model.Severity
	RuleID   string
	Artifact string
	Title    string
	Why      string
	Advisory bool
	// Dimension is the rule's dimension (1–10). Renderers turn it into the plain-language
	// label a non-specialist reads (dimLabel); it is never used for scoring here.
	Dimension int
	// Source is what produced the finding. It drives the report's split between the
	// deterministic findings (which set the score and --fail-on) and the judge's advisory
	// ones (which never do) — a distinction a reader cannot otherwise make without knowing
	// that "LLM-" is a meaningful prefix.
	Source model.Source
	Count  int
	// Files is how many DISTINCT files the group's findings touch — its breadth, where Count is
	// its depth. A real ~/.claude produced a group of 56 findings spread over 28 files while the
	// report showed three evidence lines: "×56" alone reads the same whether one bad file hit
	// fifty-six times or half a plugin hit twice each, and those call for opposite reactions
	// (fix one file vs. distrust the whole tree). Counted here rather than in a renderer so the
	// terminal and HTML reports can never disagree about it.
	Files    int
	Evidence []model.Evidence
	// soleEvidence is the full evidence list of the group's FIRST finding, kept only until
	// the group is closed (see Aggregate). Unexported: renderers read Evidence.
	soleEvidence []model.Evidence
	// files is the distinct-file set behind Files, kept only while the group is open. Unexported
	// for the same reason as soleEvidence: renderers read the count, never the set — printing the
	// set would put an unbounded, attacker-chosen list of paths in the report.
	files map[string]bool
	// Triage is an optional LLM advisory label ("likely-benign — reason") for this rule on
	// this artifact (spec §5.2.1). Display only — it never changes severity/count/order.
	Triage string
}

// FromJudge reports whether this group came from the LLM judge rather than a deterministic
// check — the split both renderers use.
func (g Group) FromJudge() bool { return g.Source == model.SrcLLM }

// MoreFiles is how many affected files the displayed Evidence does NOT name. Both renderers show
// at most three evidence lines, so a wide group's remainder is invisible unless it is counted;
// without it an operator who fixes the three named paths believes the group is closed. Derived
// from what will actually be PRINTED rather than from the evidence budget, so it stays correct if
// that budget ever changes — and lives here, not in a renderer, because the HTML template cannot
// compute it and the terminal report must not be the only place that knows.
func (g Group) MoreFiles() int {
	shown := map[string]bool{}
	for _, e := range g.Evidence {
		shown[e.File] = true
	}
	if n := g.Files - len(shown); n > 0 {
		return n
	}
	return 0
}

// notesOf is every dimension-0 note a human renderer shows: the scan-level notes first, then each
// artifact's own, in artifact order. The two live in different places in the data and that is
// right — a corrupt settings.json becomes a hook artifact carrying PARSE-000 (collect's
// withParseError), so JSON and SARIF attribute the note to the thing that was not read. But the
// terminal, markdown and HTML reports read ScanResult.Notes alone, and Aggregate (below) skips
// dimension 0, so a note on an artifact was printed by none of them: the report said "looks safe"
// over a settings.json it never parsed (invariant #5). One function, used by all three, so they
// cannot disagree about what was not read. A new slice every call: r.Notes is never appended to.
func notesOf(r model.ScanResult) []model.Finding {
	out := make([]model.Finding, 0, len(r.Notes))
	out = append(out, r.Notes...)
	for _, a := range r.Artifacts {
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				out = append(out, f)
			}
		}
	}
	return out
}

// Aggregate folds all non-dim0 findings by (artifact, rule), sorted by severity then
// hit count. Nothing is dropped — only folded (spec §12 honesty). The dim-0 notes it skips are
// rendered through notesOf.
//
// A plugin's child (P-044) repeats the plugin tree's findings on its own files; one its plugin row
// already shows (score.Family.ShownByPlugin: same rule, same file and line) is folded into that row
// rather than printed again, so looking closer at a plugin does not double its rows or its headline
// count. JSON keeps it on both artifacts. What only the child says — the judge's findings, a finding
// on the copy that loads where the tree walk met a mirror — is printed on the child's row.
func Aggregate(r model.ScanResult) []Group {
	index := map[string]*Group{}
	var order []*Group
	fam := score.Families(r.Artifacts)
	for ai, a := range r.Artifacts {
		triageByRule := map[string]model.AdvisoryLabel{}
		for _, t := range a.Advisory {
			// On a duplicate rule id, prefer likely-real (safe side): a benign label must not
			// override a real one for the same rule.
			if ex, ok := triageByRule[t.RuleID]; ok && ex.Label == model.LabelReal {
				continue
			}
			triageByRule[t.RuleID] = t
		}
		for _, f := range a.Findings {
			if f.Dimension == 0 { // parse/IO/coverage notes shown separately (notesOf)
				continue
			}
			if fam.ShownByPlugin(ai, f) {
				continue
			}
			art := string(a.Kind) + ":" + a.Name
			key := art + "|" + f.RuleID
			g := index[key]
			if g == nil {
				g = &Group{Severity: f.Severity, RuleID: f.RuleID, Artifact: art,
					Title: f.Title, Why: f.Why, Advisory: f.Advisory, Source: f.Source, Dimension: f.Dimension}
				if t, ok := triageByRule[f.RuleID]; ok && t.Label != "" {
					g.Triage = t.Label
					if t.Reason != "" {
						g.Triage += " — " + t.Reason
					}
				}
				g.soleEvidence = f.Evidence
				g.files = map[string]bool{}
				index[key] = g
				order = append(order, g)
			}
			g.Count++
			// Every evidence line counts toward breadth, not just the one that gets displayed:
			// a structural finding (EXFIL-003) names its legs across files, and those files are
			// affected whether or not there is room to print them.
			for _, e := range f.Evidence {
				g.files[e.File] = true
			}
			if len(g.Evidence) < 3 && len(f.Evidence) > 0 {
				g.Evidence = append(g.Evidence, f.Evidence[0])
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if a, b := order[i].Severity.Rank(), order[j].Severity.Rank(); a != b {
			return a > b
		}
		return order[i].Count > order[j].Count
	})
	out := make([]Group, len(order))
	for i, g := range order {
		// One line per finding is the right budget when a rule hit MANY times — three lines
		// from the first hit would hide that it hit twelve files. But a lone structural finding
		// carries its evidence as legs of one argument (EXFIL-003: read here, encoded there,
		// sent there), and showing only the first turns a three-part claim into an assertion.
		// So: still at most three lines, chosen by breadth when there is breadth to show.
		if g.Count == 1 && len(g.soleEvidence) > len(g.Evidence) {
			g.Evidence = g.soleEvidence
			if len(g.Evidence) > 3 {
				g.Evidence = g.Evidence[:3]
			}
		}
		g.Files = len(g.files)
		g.soleEvidence = nil
		g.files = nil
		out[i] = *g
	}
	return out
}
