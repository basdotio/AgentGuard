// SPDX-License-Identifier: MIT

package tripwire

import (
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/baselines/ledger"
)

// mal builds a qualifying malicious sample on one surface.
func mal(id, surface, severity string) Sample {
	return Sample{Sample: id, Surface: []string{surface}, Class: "malicious", Severity: severity}
}

// spread builds n malicious samples of the given severity on one surface.
func spread(prefix, surface, severity string, n int) []Sample {
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, mal(prefix+string(rune('a'+i%26))+string(rune('0'+i/26)), surface, severity))
	}
	return out
}

// allBenign is the ledger a scanner produces when it reads nothing on a surface: every sample
// scored, every verdict benign. This is the shape the corpus documentation warns about —
// "a whole column of zeros on one surface is almost always placement, not a real miss".
func allBenign(samples []Sample) []ledger.Row {
	rows := make([]ledger.Row, 0, len(samples))
	for _, s := range samples {
		rows = append(rows, ledger.Row{Sample: s.Sample, Outcome: ledger.Scored,
			Attempted: true, Verdict: "benign"})
	}
	return rows
}

func find(t *testing.T, fs []Finding, surface string) Finding {
	t.Helper()
	for _, f := range fs {
		if f.Surface == surface {
			return f
		}
	}
	t.Fatalf("no finding for surface %q in %+v", surface, fs)
	return Finding{}
}

// TestFiresWhenNothingIsFlaggedOnALargeSurface is the case the tripwire exists for: enough
// malicious samples to make an all-zero result mean something, and nothing flagged.
func TestFiresWhenNothingIsFlaggedOnALargeSurface(t *testing.T) {
	samples := spread("mal-skill-", "skills", "critical", 40)
	fs := Check(allBenign(samples), samples, "high", nil)

	got := find(t, fs, "skills")
	if got.Status != NeedsDeclaration {
		t.Fatalf("status = %q, want %q (n=%d, flagged=%d)", got.Status, NeedsDeclaration, got.N, got.Flagged)
	}
	if got.N != 40 || got.Flagged != 0 {
		t.Errorf("n/flagged = %d/%d, want 40/0", got.N, got.Flagged)
	}
	if len(Blocking(fs)) != 1 {
		t.Errorf("Blocking() = %d findings, want 1 — an undeclared all-zero surface must stop "+
			"the run rather than be published", len(Blocking(fs)))
	}
	if !strings.Contains(strings.ToLower(got.Advice), "placement") {
		t.Errorf("advice does not send the reader to placement first: %q", got.Advice)
	}
}

// TestADeclarationTurnsAFailureIntoADisclosure is the other half of the rule, and the reason
// the response is not simply "fail". A surface can come back empty because the tool does not
// cover it — a CLI that only reads SKILL.md would report nothing on settings.json hooks — and
// recording a disclosed product boundary as a failure penalises the disclosure.
func TestADeclarationTurnsAFailureIntoADisclosure(t *testing.T) {
	samples := spread("mal-hook-", "hooks", "high", 30)
	// A made-up declaration: no third-party scanner has been run, so this cannot cite one.
	declared := map[string]string{"hooks": "example-scanner does not read settings.json"}

	fs := Check(allBenign(samples), samples, "high", declared)
	got := find(t, fs, "hooks")
	if got.Status != Declared {
		t.Fatalf("status = %q, want %q", got.Status, Declared)
	}
	if got.Declaration == "" {
		t.Error("the declaration is not carried onto the finding, so the scorecard cannot print it")
	}
	if len(Blocking(fs)) != 0 {
		t.Error("a declared coverage boundary must not block the run")
	}
}

// TestTooFewSamplesNeverFires pins the line at which an all-zero result stops being evidence.
// It is not invented here: the corpus's own scorer prints a bare count instead of a rate once
// the Wilson 95% half-width exceeds FigureThresholdPoints = 15, and for an all-zero result that
// happens below n = 22. Firing under that would mean demanding a declaration for a surface
// whose zero is unremarkable — hooks has 5 malicious samples and a half-width of 43 points.
func TestTooFewSamplesNeverFires(t *testing.T) {
	for _, n := range []int{1, 5, 6, 21} {
		samples := spread("mal-conn-", "connector", "critical", n)
		fs := Check(allBenign(samples), samples, "high", nil)
		got := find(t, fs, "connector")
		if got.Status != TooFew {
			t.Errorf("n=%d: status = %q, want %q", n, got.Status, TooFew)
		}
		if len(Blocking(fs)) != 0 {
			t.Errorf("n=%d: a surface too thin to interpret must not block the run", n)
		}
	}
}

// TestTheCrossoverIsTwentyTwo documents the derivation as a number, so that a change to the
// corpus's FigureThresholdPoints shows up here as a failing test rather than as a tripwire that
// silently fires one sample too early or too late.
func TestTheCrossoverIsTwentyTwo(t *testing.T) {
	just := spread("mal-x-", "skills", "critical", MinimumN)
	if got := find(t, Check(allBenign(just), just, "high", nil), "skills"); got.Status != NeedsDeclaration {
		t.Errorf("at n=%d status = %q, want %q", MinimumN, got.Status, NeedsDeclaration)
	}
	under := spread("mal-y-", "skills", "critical", MinimumN-1)
	if got := find(t, Check(allBenign(under), under, "high", nil), "skills"); got.Status != TooFew {
		t.Errorf("at n=%d status = %q, want %q", MinimumN-1, got.Status, TooFew)
	}
	if MinimumN != 22 {
		t.Errorf("MinimumN = %d; the derivation in the package comment gives 22 (Wilson 95%%, "+
			"k=0, half-width <= 15 points). Recompute it before changing this.", MinimumN)
	}
}

// TestOneFlaggedSampleClearsTheSurface — the tripwire asks "did this surface produce nothing at
// all", not "did recall look good". Judging recall is `corpus score`'s job.
func TestOneFlaggedSampleClearsTheSurface(t *testing.T) {
	samples := spread("mal-skill-", "skills", "critical", 40)
	rows := allBenign(samples)
	rows[7].Verdict = "malicious"

	fs := Check(rows, samples, "high", nil)
	got := find(t, fs, "skills")
	if got.Status != OK {
		t.Fatalf("status = %q, want %q — 1 of 40 is terrible recall but it is not silence", got.Status, OK)
	}
	if len(Blocking(fs)) != 0 {
		t.Error("a surface that flagged something must not block; bad recall is the scorer's report, not a run failure")
	}
}

// TestNoVerdictCountsAsNotFlagged pins that silence never counts as a catch.
//
// The comment here used to claim this was aguard's own situation on the 127 cisco-derived MCP
// server sources. It was, until W5 measured it: pointing `aguard check` at the bare trees scores
// all 127 and flags 14, so aguard no longer produces a no-verdict row for any of them. The rule
// still has to hold for whatever tool does — a no-verdict row is the tool saying nothing, and
// nothing is not a benign call — so the case is kept and the stale claim removed.
func TestNoVerdictCountsAsNotFlagged(t *testing.T) {
	samples := spread("mal-mcp-ci-", "mcp", "high", 30)
	rows := make([]ledger.Row, 0, len(samples))
	for _, s := range samples {
		rows = append(rows, ledger.Row{Sample: s.Sample, Outcome: ledger.NoVerdict,
			Attempted: true, Reason: ledger.NoLoadPath, Detail: "no load path this tool reads"})
	}

	got := find(t, Check(rows, samples, "high", nil), "mcp")
	if got.Status != NeedsDeclaration {
		t.Fatalf("status = %q, want %q", got.Status, NeedsDeclaration)
	}
	if got.Flagged != 0 {
		t.Errorf("flagged = %d, want 0", got.Flagged)
	}
}

// TestSeverityBarFiltersTheDenominator — the criterion says "malicious samples that meet
// truth.severity". A surface full of low-severity samples that a gate at `high` was never going
// to flag is not evidence of a placement fault.
func TestSeverityBarFiltersTheDenominator(t *testing.T) {
	var samples []Sample
	samples = append(samples, spread("mal-lo-", "skills", "low", 40)...)
	samples = append(samples, spread("mal-hi-", "skills", "critical", 3)...)

	got := find(t, Check(allBenign(samples), samples, "high", nil), "skills")
	if got.N != 3 {
		t.Fatalf("n = %d, want 3 — only the samples at or above the bar belong in the denominator", got.N)
	}
	if got.Status != TooFew {
		t.Errorf("status = %q, want %q", got.Status, TooFew)
	}
}

// TestBenignAndHardNegativesAreNotInTheDenominator — flagging a benign sample is a false
// positive, so counting them here would make a clean run look like a broken one.
func TestBenignAndHardNegativesAreNotInTheDenominator(t *testing.T) {
	samples := []Sample{
		{Sample: "ben-1", Surface: []string{"skills"}, Class: "benign"},
		{Sample: "hn-1", Surface: []string{"skills"}, Class: "hard-negative"},
	}
	fs := Check(allBenign(samples), samples, "high", nil)
	for _, f := range fs {
		if f.N != 0 {
			t.Errorf("surface %q has n=%d; only malicious samples count", f.Surface, f.N)
		}
	}
}

// TestASampleOnTwoSurfacesCountsOnBoth — the work list's `surface` is a list because one file
// can sit on two load paths, and dropping the second would under-count a surface into silence.
func TestASampleOnTwoSurfacesCountsOnBoth(t *testing.T) {
	samples := spread("mal-both-", "skills", "critical", 25)
	for i := range samples {
		samples[i].Surface = []string{"skills", "instruction"}
	}
	fs := Check(allBenign(samples), samples, "high", nil)
	if got := find(t, fs, "skills"); got.N != 25 {
		t.Errorf("skills n = %d, want 25", got.N)
	}
	if got := find(t, fs, "instruction"); got.N != 25 {
		t.Errorf("instruction n = %d, want 25", got.N)
	}
}

// TestADeclarationForASurfaceWithFindingsIsAMistake — declaring "we do not cover hooks" while
// flagging hooks samples means the declaration is stale, and a stale declaration is how a real
// placement fault gets waved through later.
func TestADeclarationForASurfaceWithFindingsIsAMistake(t *testing.T) {
	samples := spread("mal-skill-", "skills", "critical", 40)
	rows := allBenign(samples)
	rows[0].Verdict = "malicious"

	fs := Check(rows, samples, "high", map[string]string{"skills": "does not read skills"})
	got := find(t, fs, "skills")
	if got.Status != StaleDeclaration {
		t.Fatalf("status = %q, want %q", got.Status, StaleDeclaration)
	}
	if len(Blocking(fs)) != 1 {
		t.Error("a declaration contradicted by the run's own findings must stop the run")
	}
}
