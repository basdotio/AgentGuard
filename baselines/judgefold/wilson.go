// SPDX-License-Identifier: MIT

package judgefold

import (
	"fmt"
	"math"
)

// z975 and Wilson are the corpus's (harness/internal/score/wilson.go): the same interval, so a
// figure here and one in a scorecard are the same arithmetic.
const z975 = 1.959963984540054

// FigureThresholdPoints is the corpus's line between a rate and a count
// (harness/internal/score FigureThresholdPoints): a proportion whose Wilson 95% half-width is
// wider than this is printed as a bare count. For an all-zero result that is n < 22
// (tripwire.MinimumN), but the rule is the half-width, not the n: 17 of 35 is a count, 20 of 40 a rate.
const FigureThresholdPoints = 15.0

// Wilson returns the Wilson score interval for k of n at 95%; n == 0 is (0, 0, 0).
func Wilson(k, n int) (point, lo, hi float64) {
	if n == 0 {
		return 0, 0, 0
	}
	p := float64(k) / float64(n)
	nf := float64(n)
	z2 := z975 * z975
	denom := 1 + z2/nf
	center := (p + z2/(2*nf)) / denom
	margin := (z975 / denom) * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	return p, math.Max(0, center-margin), math.Min(1, center+margin)
}

// HalfWidthPoints is the wider arm of the interval around the point, in percentage points.
func HalfWidthPoints(point, lo, hi float64) float64 {
	return 100 * math.Max(hi-point, point-lo)
}

// Cell renders k of n for the table: the fraction always, then the rate and its interval in
// percent when the interval is narrow enough to be a figure, the half-width alone when it is not.
func Cell(k, n int) string {
	frac := fmt.Sprintf("%d/%d", k, n)
	if n == 0 {
		return frac
	}
	p, lo, hi := Wilson(k, n)
	if hw := HalfWidthPoints(p, lo, hi); hw > FigureThresholdPoints {
		return fmt.Sprintf("%s (no rate: ±%.0f pts)", frac, hw)
	}
	return fmt.Sprintf("%s %.1f%% [%.1f, %.1f]", frac, 100*p, 100*lo, 100*hi)
}
