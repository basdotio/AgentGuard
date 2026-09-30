// SPDX-License-Identifier: MIT

// Package tripwire answers one question about a finished run, before its numbers are allowed to
// become a comparison: did a whole load-path surface come back with nothing flagged.
//
// The corpus's own guide names this as the failure mode that produces a wrong number about
// somebody else's scanner: "a whole column of zeros on one surface is almost always this
// (placement), not a real miss. Check the layout before believing it." A runner WE wrote,
// producing a figure about a competitor, is exactly where that mistake would be both easy and
// unanswerable — so it is checked rather than remembered.
//
// The response is deliberately neither a pass nor a failure, because an all-zero surface has two
// causes and no machine separates them:
//
//   - we staged the samples somewhere the tool does not look (our fault, a wrong number), or
//   - the tool does not cover that surface at all (its documented boundary, a right number).
//
// So the run stops and asks for a one-time declaration per (tool, surface). With a declaration,
// the zero is a disclosed coverage gap and the run proceeds — recording a disclosed product
// boundary as a failure would penalise the disclosure, which is the opposite of what this
// project rewards (invariant #5). Without one, nothing is published.
//
// # Where MinimumN comes from
//
// An all-zero surface is only evidence if there were enough samples for zero to be surprising.
// That line is NOT invented here: agent-artifact-corpus already draws it, in
// harness/internal/score — a proportion is printed as a rate only while its Wilson 95%
// half-width is at most FigureThresholdPoints = 15, and below that it is printed as a bare
// count with "n too small for a rate". Applying the same rule to k=0 gives n >= 22 (at n=22 the
// half-width is 14.9 points; at n=21 it is 15.3).
//
// Measured against the corpus as it stands, that puts the wire on two surfaces and not the
// others: skills (153 malicious samples, half-width 2.4 points) and mcp (130, 2.9) fire;
// permission (6, 39.0), hooks (5, 43.4), connector (5, 43.4) and instruction (1, 79.3) cannot.
// Demanding a declaration for a surface whose zero is unremarkable would train whoever runs this
// to write declarations without reading them, and a declaration nobody reads is how a real
// placement fault gets waved through later.
//
// The constant is duplicated rather than imported: the corpus is a separate module on purpose
// (licence isolation, tool neutrality), and depending on it from here would undo that. The
// duplication is pinned by TestTheCrossoverIsTwentyTwo, so a change on the corpus side surfaces
// as a failing test with the derivation next to it.
package tripwire

import (
	"fmt"
	"sort"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// MinimumN is the smallest number of qualifying malicious samples on one surface at which an
// all-zero result is worth stopping for. See the package comment for the derivation.
const MinimumN = 22

// Status is what the tripwire concluded about one surface.
type Status string

const (
	// OK: something on this surface was flagged, so the surface is not silent. Whether the
	// recall is any good is `corpus score`'s report, not this check's.
	OK Status = "ok"
	// TooFew: not enough qualifying malicious samples for a zero to mean anything.
	TooFew Status = "too-few"
	// Declared: nothing flagged, and the tool has declared it does not cover this surface.
	Declared Status = "declared"
	// NeedsDeclaration: nothing flagged, enough samples that it matters, no declaration.
	// This blocks.
	NeedsDeclaration Status = "needs-declaration"
	// StaleDeclaration: a declaration says the tool does not cover this surface, but the run
	// flagged samples on it. One of the two is wrong, and a declaration contradicted by the
	// run's own findings is worse than none — it is a standing excuse for future silence.
	// This blocks.
	StaleDeclaration Status = "stale-declaration"
)

// Sample is the part of a `corpus samples` record this check needs.
type Sample struct {
	Sample   string
	Surface  []string
	Class    string // malicious | benign | hard-negative
	Severity string // truth severity; malicious samples only
}

// Finding is one surface's result. N and Flagged are counts and stay counts: this package never
// computes a rate, because a rate here would invite reading it as recall.
type Finding struct {
	Surface     string
	N           int // qualifying malicious samples: this surface, at or above the bar
	Flagged     int
	Status      Status
	Declaration string
	Advice      string
}

// severityRank is the corpus's tool-neutral ladder. 0 means "not a severity this check knows",
// which keeps an unlabelled sample out of the denominator rather than silently at the bottom.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// Check groups the run by surface and applies the rule. `bar` is the truth severity a malicious
// sample must reach to be in a surface's denominator — a surface full of low-severity samples
// that a gate at `high` was never going to flag is not evidence of a placement fault. An
// unrecognised bar admits every sample carrying a severity this ladder knows, which is the
// inclusive side: a tripwire that fires too often is noticed, one that never fires is not.
//
// `declared` maps surface name to the tool's stated reason for covering nothing there; an empty
// or missing entry means undeclared.
//
// A sample may sit on several surfaces, because one file can be on two load paths; it counts on
// each. Dropping the second would under-count a surface towards silence, which is the direction
// that hides the fault.
func Check(rows []ledger.Row, samples []Sample, bar string, declared map[string]string) []Finding {
	// Only a scored row carrying "malicious" counts as flagged. A no-verdict row explicitly
	// does not: that is the case that matters in practice — aguard produces no verdict for the
	// 127 cisco-derived MCP server sources, so the whole malicious MCP surface comes back with
	// nothing flagged, and it must trip the wire rather than pass because "no verdict" is not
	// "benign".
	flaggedBySample := make(map[string]bool, len(rows))
	for _, r := range rows {
		flaggedBySample[r.Sample] = r.Outcome == ledger.Scored && r.Verdict == "malicious"
	}

	type acc struct{ n, flagged int }
	bySurface := map[string]*acc{}
	touch := func(surface string) *acc {
		a := bySurface[surface]
		if a == nil {
			a = &acc{}
			bySurface[surface] = a
		}
		return a
	}

	barRank := severityRank(bar)
	for _, s := range samples {
		qualifies := s.Class == "malicious" &&
			severityRank(s.Severity) > 0 &&
			severityRank(s.Severity) >= barRank
		for _, surface := range s.Surface {
			a := touch(surface)
			if !qualifies {
				continue
			}
			a.n++
			if flaggedBySample[s.Sample] {
				a.flagged++
			}
		}
	}

	names := make([]string, 0, len(bySurface))
	for name := range bySurface {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Finding, 0, len(names))
	for _, name := range names {
		a := bySurface[name]
		f := Finding{Surface: name, N: a.n, Flagged: a.flagged, Declaration: declared[name]}
		switch {
		case a.flagged > 0 && f.Declaration != "":
			f.Status = StaleDeclaration
			f.Advice = fmt.Sprintf("this run flagged %d sample(s) on a surface declared "+
				"uncovered; delete the declaration or find out why it is still there", a.flagged)
		case a.flagged > 0:
			f.Status = OK
		case a.n == 0:
			f.Status = TooFew
			f.Advice = "no malicious samples at or above the bar on this surface"
		case a.n < MinimumN:
			f.Status = TooFew
			f.Advice = "too thin to interpret: a zero over this few samples is unremarkable " +
				"(the corpus prints a count rather than a rate here)"
		case f.Declaration != "":
			f.Status = Declared
		default:
			f.Status = NeedsDeclaration
			f.Advice = "check the placement first — a whole surface coming back clean is " +
				"almost always the samples being staged where this tool does not look. " +
				"If the tool genuinely does not cover this surface, declare that and the " +
				"zero becomes a disclosed gap instead of a wrong number."
		}
		out = append(out, f)
	}
	return out
}

// Blocking returns the findings that must stop the run before anything is published.
func Blocking(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Status == NeedsDeclaration || f.Status == StaleDeclaration {
			out = append(out, f)
		}
	}
	return out
}
