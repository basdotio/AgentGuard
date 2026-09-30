// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/basdotio/agent-guard/baselines/adapter"
	"github.com/basdotio/agent-guard/internal/model"
)

// The six injected-fault fixtures, judged here rather than by `corpus score`. The corpus is
// explicit about why: what counts as passing depends on what the tool promised, so the scorer
// refuses to decide it. These are per-fixture pass/fail and never a rate — one counterexample
// settles a robustness claim, and averaging six of them would destroy the only information they
// carry.
//
// They had never been run for aguard at all before this adapter existed: they are not in `corpus samples`,
// so `make bench`'s three steps never touched them.
//
// # What passing means, and the honest limit of it
//
// Five of the six are robustness: the claim is that aguard TERMINATES, bounded, on a tree built
// to make a naive walker hang, loop or allocate. Passing is therefore "finished inside the
// timeout without crashing", and that is genuinely all this can assert from outside the process.
// It cannot measure peak memory, so `sparse-huge-config` is a weaker test here than its author
// intended — a scanner that read all 8 GiB but happened to have the RAM would pass. Said plainly
// rather than dressed up: the fixture asserts more than this runner can check.
//
// The sixth, traverse-only-dir, is a DISCLOSURE test and the only one with a positive
// requirement: a `0111` subdirectory cannot be enumerated, and a scanner that says nothing about
// it has claimed a skill was fully read when it was not. That is invariant #5's own subject
// matter, so passing requires an actual dimension-0 note — silence is a fail, not a pass.
const (
	fixTraverseOnly = "traverse-only-dir"
	fixFIFO         = "fifo-as-skill"
	fixSymlinkCycle = "symlink-cycle"
	fixSymlinkEsc   = "symlink-escape"
	fixDeepNest     = "deeply-nested"
	fixSparseHuge   = "sparse-huge-config"
)

// FixtureNames is the closed set this adapter knows how to judge, in the corpus's own order.
// A fixture the corpus adds and this list does not carry becomes an untestable row rather than
// a silent absence — see Fixture.
func FixtureNames() []string {
	return []string{fixTraverseOnly, fixFIFO, fixSymlinkCycle, fixSymlinkEsc, fixDeepNest, fixSparseHuge}
}

// Fixture points aguard at one materialised fixture and judges it. dir is the fixture's own tree
// (the directory `corpus fixtures --materialize` created for it).
//
// It always returns a result, for the same reason Scan always returns a row: a fixture that
// produced no line is indistinguishable from one nobody ran.
func (a *Adapter) Fixture(ctx context.Context, name, dir string) adapter.FixtureResult {
	res := adapter.FixtureResult{Fixture: name}

	scan, err := a.run(ctx, "check", dir, "--json")
	if err != nil {
		// A timeout IS the failure these fixtures look for: the FIFO one hangs a scanner that
		// opens and reads without stat-ing first, and the cycle one loops a walker with no
		// visited set. Distinguished from a crash because the two mean different things.
		if strings.Contains(err.Error(), "did not finish within") {
			res.Status = adapter.FixtureFail
			res.Detail = "did not terminate: " + err.Error()
			return res
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 2 {
			res.Status = adapter.FixtureFail
			res.Detail = "exited 2 (runtime error) — exit code 2 is not a pass: " + err.Error()
			return res
		}
		res.Status = adapter.FixtureFail
		res.Detail = err.Error()
		return res
	}

	switch name {
	case fixTraverseOnly:
		// The one positive requirement. A note of any dimension-0 kind will do — COV-000,
		// IO-000, PARSE-000, SCOPE-001 — because the assertion is "it said something about
		// what it could not read", not "it used a particular rule id".
		if notes := dimensionZeroNotes(scan); len(notes) > 0 {
			res.Status = adapter.FixturePass
			res.Detail = "disclosed the unreadable subdirectory: " + strings.Join(notes, ", ")
			return res
		}
		res.Status = adapter.FixtureFail
		res.Detail = "terminated but emitted no dimension-0 note; a `0111` subdirectory a walk " +
			"cannot enumerate, passed over in silence, is a claim the skill was read in full"
		return res

	case fixFIFO, fixSymlinkCycle, fixSymlinkEsc, fixDeepNest, fixSparseHuge:
		res.Status = adapter.FixturePass
		res.Detail = fmt.Sprintf("terminated within %s, exit 0 or 1, %d artifact(s) reported",
			ScanTimeout, len(scan.Artifacts))
		if name == fixSparseHuge {
			// Stated on the row, not just in the package comment, so it travels with the
			// result into baselines/results/ where somebody will read it out of context.
			res.Detail += ". NOTE: this fixture asserts a bounded READ, and this runner can " +
				"only observe termination — peak memory is not measured here"
		}
		if name == fixSymlinkEsc {
			if notes := dimensionZeroNotes(scan); len(notes) > 0 {
				res.Detail += "; also disclosed: " + strings.Join(notes, ", ")
			}
		}
		return res
	}

	// A fixture the corpus has added and this adapter has not been taught. Recorded, never
	// omitted: the whole point of the ledger is that nothing disappears quietly.
	res.Status = adapter.FixtureUntestable
	res.Detail = "this adapter has no judgement for a fixture named " + name +
		"; add it to FixtureNames and say what passing means"
	return res
}

// dimensionZeroNotes lists the dimension-0 notes a scan produced, by rule id. Dimension 0 is the
// coverage/suppression channel: notes there describe the scan rather than the artifact, and by
// invariant #5 every gap must produce one.
func dimensionZeroNotes(res model.ScanResult) []string {
	var out []string
	for _, n := range res.Notes {
		out = append(out, n.RuleID)
	}
	for _, art := range res.Artifacts {
		for _, f := range art.Findings {
			if f.Dimension == 0 {
				out = append(out, f.RuleID)
			}
		}
	}
	return out
}

var _ adapter.FixtureRunner = (*Adapter)(nil)
