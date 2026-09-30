// SPDX-License-Identifier: MIT
// Package score computes AgentGuard's risk score (spec §5.3). It produces TWO numbers from
// one formula, run over two different sets of findings:
//
//   - Overall          — deterministic sources only (static / permission / hygiene). The same
//     environment always scores the same, which is what makes it usable for --fail-on and for
//     an on-chain attestation a third party can recompute offline.
//   - OverallEffective — the same formula with qualified LLM findings folded in. This is the
//     number for a human and for CI to look at; it is NOT reproducible and never gates.
//
// The split exists because the two properties are irreconcilable: an LLM verdict is useful
// precisely because it sees things a regex can't, and useless as a stable identity because it
// is probabilistic. Rather than trade one for the other, they get a number each.
package score

import "github.com/basdotio/agent-guard/internal/model"

// Penalty weights (spec appendix A). Within a dimension only the max hit counts;
// across dimensions penalties add; final score is clamped to [0,100].
var penalty = map[model.Severity]int{
	model.SevCritical: 40,
	model.SevHigh:     25,
	model.SevMedium:   12,
	model.SevLow:      5,
}

// Level names for a score band (spec §5.3). Both numbers share one band table.
func Level(score int) string {
	switch {
	case score >= 85:
		return "Low"
	case score >= 70:
		return "Watch"
	case score >= 50:
		return "Elevated"
	default:
		return "High"
	}
}

// filter decides whether a finding feeds a score.
//
// Scoring and gating must agree about which findings count, and the two live in different
// packages. Keeping a copy in each would create the drift that fails silently in both
// directions — a gate that starts reacting to LLM output, or an escalation that quietly never
// fires, neither of which surfaces as a test failure on its own. So there is exactly ONE
// definition of each predicate, exported, and the report layer calls these rather than
// restating them.
type filter func(model.Finding) bool

// Deterministic reports whether a finding may touch the reproducible score and --fail-on: not
// LLM-sourced, and not a dimension-0 note (those describe the scan, not the artifact).
func Deterministic(f model.Finding) bool {
	return f.Source != model.SrcLLM && f.Dimension != 0
}

// Escalating: deterministic findings PLUS qualified LLM ones.
//
// Qualification is decided upstream and carried on the finding (Finding.Escalates): the judge
// sets it only after the evidence was located in what was actually sent AND enough independent
// samples agreed. An LLM finding that failed either bar still appears in the report — it is a
// lead, just not a weighted one — so this filter reads the flag rather than re-deriving it.
// Tier authorisation joins the bar in a later step; until then the number this produces is
// DISPLAY-ONLY and gates nothing (spec §5.2.1).
func Escalating(f model.Finding) bool {
	if f.Dimension == 0 {
		return false
	}
	return f.Source != model.SrcLLM || f.Escalates
}

// artifactScore = clamp(100 − Σ_dimension max(severity penalty), 0, 100). Advisory static
// findings (dim 7/8) DO score, at their (low) severity.
func artifactScore(a model.ArtifactReport, keep filter) int {
	maxByDim := map[int]int{}
	for _, f := range a.Findings {
		if !keep(f) {
			continue
		}
		if p := penalty[f.Severity]; p > maxByDim[f.Dimension] {
			maxByDim[f.Dimension] = p
		}
	}
	total := 0
	for _, p := range maxByDim {
		total += p
	}
	return clamp(100-total, 0, 100)
}

// overallScore is the environment number: the average artifact score, then a leaky-bucket cap —
// any critical drags the whole environment to ≤49 (High), any high (no critical) to ≤69.
func overallScore(arts []model.ArtifactReport, keep filter) int {
	worst := model.Severity("")
	sum, n := 0, 0
	for _, a := range arts {
		sum += artifactScore(a, keep)
		n++
		for _, f := range a.Findings {
			if keep(f) && f.Severity.Rank() > worst.Rank() {
				worst = f.Severity
			}
		}
	}
	overall := 100
	if n > 0 {
		overall = sum / n
	}
	switch worst {
	case model.SevCritical:
		overall = min(overall, 49)
	case model.SevHigh:
		overall = min(overall, 69)
	}
	return clamp(overall, 0, 100)
}

// Apply sets both scores on every artifact and on the result (spec §5.3).
//
// The min() calls are the point of this function. Arithmetically they are redundant — adding
// findings can only add penalty — but they make "the LLM raised the score" an UNREPRESENTABLE
// state rather than a bug someone could introduce later. One-way-ness is a property of the
// shape here, not of an argument about the shape.
func Apply(r *model.ScanResult) {
	for i := range r.Artifacts {
		s := artifactScore(r.Artifacts[i], Deterministic)
		r.Artifacts[i].Score = s
		r.Artifacts[i].ScoreEffective = min(s, artifactScore(r.Artifacts[i], Escalating))
	}
	r.Overall = overallScore(r.Artifacts, Deterministic)
	r.OverallEffective = min(r.Overall, overallScore(r.Artifacts, Escalating))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
