// SPDX-License-Identifier: MIT
package score

import (
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

func art(findings ...model.Finding) model.ArtifactReport {
	return model.ArtifactReport{Kind: model.KindSkill, Findings: findings}
}
func f(dim int, sev model.Severity) model.Finding {
	return model.Finding{Dimension: dim, Severity: sev, Source: model.SrcStatic}
}

func TestArtifactScore_MaxPerDimensionThenAdd(t *testing.T) {
	// Two highs in the SAME dimension count once (25), not twice.
	a := art(f(4, model.SevHigh), f(4, model.SevHigh))
	if got := artifactScore(a, Deterministic); got != 75 {
		t.Errorf("same-dim two highs = %d, want 75 (one 25 penalty)", got)
	}
	// Different dimensions add: high(25) + medium(12) = 37 → 63.
	b := art(f(4, model.SevHigh), f(9, model.SevMedium))
	if got := artifactScore(b, Deterministic); got != 63 {
		t.Errorf("cross-dim add = %d, want 63", got)
	}
}

func TestScore_FloorZero(t *testing.T) {
	a := art(f(1, model.SevCritical), f(2, model.SevCritical), f(3, model.SevCritical), f(4, model.SevCritical))
	if got := artifactScore(a, Deterministic); got != 0 {
		t.Errorf("4 criticals = %d, want floored 0", got)
	}
}

func TestScore_LLMAndNotesExcluded(t *testing.T) {
	a := art(
		model.Finding{Dimension: 4, Severity: model.SevCritical, Source: model.SrcLLM},    // ignored
		model.Finding{Dimension: 0, Severity: model.SevHigh, Source: model.SrcParseError}, // ignored
	)
	if got := artifactScore(a, Deterministic); got != 100 {
		t.Errorf("LLM + parse findings must not score; got %d want 100", got)
	}
}

func TestApply_BucketCap(t *testing.T) {
	// One clean artifact (100) + one with a single high → average would be ~87, but a
	// high anywhere caps the env at ≤69.
	r := model.ScanResult{Artifacts: []model.ArtifactReport{art(), art(f(4, model.SevHigh))}}
	Apply(&r)
	if r.Overall > 69 {
		t.Errorf("high present but overall=%d (>69), bucket cap failed", r.Overall)
	}
	// A critical caps at ≤49.
	r2 := model.ScanResult{Artifacts: []model.ArtifactReport{art(), art(f(4, model.SevCritical))}}
	Apply(&r2)
	if r2.Overall > 49 {
		t.Errorf("critical present but overall=%d (>49)", r2.Overall)
	}
}

func TestApply_Deterministic(t *testing.T) {
	mk := func() model.ScanResult {
		return model.ScanResult{Artifacts: []model.ArtifactReport{art(f(1, model.SevMedium)), art(f(9, model.SevLow))}}
	}
	r1, r2 := mk(), mk()
	Apply(&r1)
	Apply(&r2)
	if r1.Overall != r2.Overall {
		t.Errorf("scoring not deterministic: %d vs %d", r1.Overall, r2.Overall)
	}
}

// llmF builds an LLM finding that CLEARED every precondition — grounded and agreed on by
// enough samples — so it is allowed to weigh on the effective score.
func llmF(dim int, sev model.Severity) model.Finding {
	return model.Finding{Dimension: dim, Severity: sev, Source: model.SrcLLM, Advisory: true, Escalates: true}
}

// llmWeak builds an LLM finding that is reported but carries no weight: grounded, yet without
// enough sample agreement to be acted on.
func llmWeak(dim int, sev model.Severity) model.Finding {
	return model.Finding{Dimension: dim, Severity: sev, Source: model.SrcLLM, Advisory: true}
}

// TestApply_EffectiveNeverExceedsOverall is the one-way-escalation invariant (spec §16.7).
// It is a property, not an example: across every combination of deterministic and LLM
// findings, the effective number can only ever be lower. The min() in Apply is what makes
// "the LLM raised the score" unrepresentable rather than merely unlikely.
func TestApply_EffectiveNeverExceedsOverall(t *testing.T) {
	sevs := []model.Severity{model.SevLow, model.SevMedium, model.SevHigh, model.SevCritical}
	dims := []int{0, 1, 3, 4, 9, 10}

	for _, ds := range sevs {
		for _, dd := range dims {
			for _, ls := range sevs {
				for _, ld := range dims {
					r := model.ScanResult{Artifacts: []model.ArtifactReport{
						art(f(dd, ds), llmF(ld, ls)),
						art(llmF(ld, ls)),
						art(f(dd, ds)),
					}}
					Apply(&r)
					if r.OverallEffective > r.Overall {
						t.Fatalf("effective %d > overall %d (static dim%d/%s, llm dim%d/%s)",
							r.OverallEffective, r.Overall, dd, ds, ld, ls)
					}
					for i, a := range r.Artifacts {
						if a.ScoreEffective > a.Score {
							t.Fatalf("artifact %d: effective %d > score %d", i, a.ScoreEffective, a.Score)
						}
					}
				}
			}
		}
	}
}

// TestApply_LLMMovesOnlyTheEffectiveNumber: the deterministic score must be byte-identical
// whether or not the judge ran. It is what --fail-on and an on-chain attestation depend on,
// so a third party has to be able to recompute it from the same content, offline.
func TestApply_LLMMovesOnlyTheEffectiveNumber(t *testing.T) {
	staticOnly := model.ScanResult{Artifacts: []model.ArtifactReport{art(f(9, model.SevMedium))}}
	withJudge := model.ScanResult{Artifacts: []model.ArtifactReport{
		art(f(9, model.SevMedium), llmF(1, model.SevHigh), llmF(10, model.SevMedium)),
	}}
	Apply(&staticOnly)
	Apply(&withJudge)

	if withJudge.Overall != staticOnly.Overall {
		t.Errorf("overall changed with the judge on: %d vs %d — reproducibility broken",
			withJudge.Overall, staticOnly.Overall)
	}
	if withJudge.OverallEffective >= withJudge.Overall {
		t.Errorf("two flagged LLM findings should lower the effective number: %d vs %d",
			withJudge.OverallEffective, withJudge.Overall)
	}
	// No LLM findings at all → the two numbers are the same, and the report shows one line.
	if staticOnly.OverallEffective != staticOnly.Overall {
		t.Errorf("without the judge the numbers must agree: %d vs %d",
			staticOnly.OverallEffective, staticOnly.Overall)
	}
}

// TestApply_LLMCoverageNotesNeverScore: dimension 0 is scan bookkeeping (LLM-000/LLM-005 and
// friends). It describes the scan, not the artifact, so it may not move either number.
func TestApply_LLMCoverageNotesNeverScore(t *testing.T) {
	r := model.ScanResult{Artifacts: []model.ArtifactReport{
		art(llmF(0, model.SevMedium), model.Finding{Dimension: 0, Severity: model.SevHigh, Source: model.SrcStatic}),
	}}
	Apply(&r)
	if r.Overall != 100 || r.OverallEffective != 100 {
		t.Errorf("dimension-0 notes scored: overall=%d effective=%d, want 100/100", r.Overall, r.OverallEffective)
	}
}

// TestApply_EffectiveTakesTheLeakyBucketToo: a high-severity LLM finding must be able to cap
// the effective environment score, or the second number would understate exactly the case it
// exists to surface.
func TestApply_EffectiveTakesTheLeakyBucketToo(t *testing.T) {
	r := model.ScanResult{Artifacts: []model.ArtifactReport{art(), art(), art(llmF(1, model.SevHigh))}}
	Apply(&r)
	if r.Overall != 100 {
		t.Fatalf("overall = %d, want 100 (no deterministic findings at all)", r.Overall)
	}
	if r.OverallEffective > 69 {
		t.Errorf("effective = %d, want ≤69 — an LLM high must cap the effective bucket", r.OverallEffective)
	}
}

// TestApply_UnqualifiedLLMFindingCarriesNoWeight is the scoring half of the consensus
// contract. A finding the samples disagreed about is still REPORTED — the judge may only ever
// add — but it must not move either number. If this filter read the wrong field, escalation
// would either apply to everything or to nothing, and both fail silently.
func TestApply_UnqualifiedLLMFindingCarriesNoWeight(t *testing.T) {
	weak := model.ScanResult{Artifacts: []model.ArtifactReport{art(llmWeak(1, model.SevHigh))}}
	Apply(&weak)
	if weak.Overall != 100 || weak.OverallEffective != 100 {
		t.Errorf("an unqualified LLM finding moved a score: overall=%d effective=%d, want 100/100",
			weak.Overall, weak.OverallEffective)
	}

	// Same finding, same severity — but this one cleared the bar.
	strong := model.ScanResult{Artifacts: []model.ArtifactReport{art(llmF(1, model.SevHigh))}}
	Apply(&strong)
	if strong.OverallEffective >= strong.Overall {
		t.Errorf("a qualified LLM finding did NOT move the effective score: %d vs %d",
			strong.OverallEffective, strong.Overall)
	}

	// Mixed: only the qualified one counts, so the drop matches the qualified finding alone.
	mixed := model.ScanResult{Artifacts: []model.ArtifactReport{
		art(llmF(1, model.SevLow), llmWeak(3, model.SevCritical)),
	}}
	Apply(&mixed)
	if mixed.Artifacts[0].ScoreEffective != 95 { // low = 5 penalty; the critical is unqualified
		t.Errorf("effective score = %d, want 95 — only the qualified finding may count",
			mixed.Artifacts[0].ScoreEffective)
	}
}
