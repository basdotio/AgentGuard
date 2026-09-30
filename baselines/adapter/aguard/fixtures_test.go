// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/baselines/adapter"
	"github.com/basdotio/agent-guard/internal/model"
)

// TestAnUnknownFixtureIsUntestableNotAbsent — the corpus can add a seventh fixture tomorrow. A
// runner that iterated its own list and silently ignored the new one would report six passes and
// a complete-looking run, which is the shape of silence this whole directory exists to remove.
func TestAnUnknownFixtureIsUntestableNotAbsent(t *testing.T) {
	a := newAdapter(t, build(t))
	got := a.Fixture(context.Background(), "some-new-fixture", t.TempDir())

	if got.Status != adapter.FixtureUntestable {
		t.Fatalf("status = %q, want %q", got.Status, adapter.FixtureUntestable)
	}
	if !strings.Contains(got.Detail, "some-new-fixture") {
		t.Errorf("detail does not name the fixture: %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "FixtureNames") {
		t.Errorf("detail does not say how to fix it: %q", got.Detail)
	}
}

// TestDimensionZeroNotesReadsBothPlaces — a coverage note can arrive as a top-level scan note or
// as a dimension-0 finding on an artifact. Reading only one of the two would make the disclosure
// fixture fail for a scanner that disclosed correctly, which is the worst possible direction for
// a test of whether disclosure happened.
func TestDimensionZeroNotesReadsBothPlaces(t *testing.T) {
	top := model.ScanResult{Notes: []model.Finding{{RuleID: "COV-000"}}}
	if got := dimensionZeroNotes(top); len(got) != 1 || got[0] != "COV-000" {
		t.Errorf("top-level note not read: %v", got)
	}

	onArtifact := model.ScanResult{Artifacts: []model.ArtifactReport{{
		Findings: []model.Finding{{RuleID: "IO-000", Dimension: 0}, {RuleID: "EXEC-001", Dimension: 4}},
	}}}
	got := dimensionZeroNotes(onArtifact)
	if len(got) != 1 || got[0] != "IO-000" {
		t.Errorf("artifact-level dimension-0 finding not read, or a scoring finding leaked in: %v", got)
	}

	if got := dimensionZeroNotes(model.ScanResult{}); len(got) != 0 {
		t.Errorf("notes invented from an empty result: %v", got)
	}
}

// TestFixtureNamesMatchesTheJudgements keeps the list and the switch in step. A name in the list
// with no judgement would report untestable for a fixture the adapter was supposed to know.
func TestFixtureNamesMatchesTheJudgements(t *testing.T) {
	names := FixtureNames()
	if len(names) != 6 {
		t.Errorf("FixtureNames has %d entries; the corpus ships 6", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("%q appears twice", n)
		}
		seen[n] = true
	}
	for _, want := range []string{fixTraverseOnly, fixFIFO, fixSymlinkCycle, fixSymlinkEsc,
		fixDeepNest, fixSparseHuge} {
		if !seen[want] {
			t.Errorf("FixtureNames is missing %q, so it would never be run", want)
		}
	}
}
