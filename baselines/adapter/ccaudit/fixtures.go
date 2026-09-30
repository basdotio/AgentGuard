// SPDX-License-Identifier: MIT

package ccaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/basdotio/agent-guard/baselines/adapter"
)

// The six injected-fault fixtures, judged here rather than by `corpus score`. The corpus is
// explicit about why: what counts as passing depends on what the tool promised, so the scorer
// refuses to decide it. These are per-fixture pass/fail and never a rate — one counterexample
// settles a robustness claim, and averaging six of them would destroy the only information they
// carry.
//
// # Where the pass conditions come from
//
// Not from us. Each fixture carries an `Expected` field that the corpus writes tool-neutrally,
// "phrased so it holds for any scanner rather than naming rule ids"
// (harness/internal/fixtures/fixtures.go). This file applies those sentences to cc-audit; it does
// not invent a standard for somebody else's tool, and in particular it does NOT require the
// dimension-0 note that the aguard adapter requires. aguard passes traverse-only-dir by emitting
// COV-000; cc-audit has no dimension-0 concept at all, and holding it to our mechanism rather
// than to the corpus's sentence would be scoring it against our architecture.
//
// Five of the six are robustness (corpus class 7): the claim is that the scanner TERMINATES,
// bounded, on a tree built to make a naive walker hang, loop or allocate. Passing is "finished
// inside the timeout without a run failure", and that is genuinely all this can assert from
// outside the process. It cannot measure peak memory, so `sparse-huge-config` is a weaker test
// here than its author intended — a scanner that read all 8 GiB but had the RAM would pass.
// Said plainly rather than dressed up: the fixture asserts more than this runner can check.
//
// The sixth, traverse-only-dir, is a DISCLOSURE test (class 3) and the only one with a positive
// requirement. The corpus states it:
//
//	report that `private/` could not be enumerated — do not score the readable part as if it
//	were the whole skill. Silence here is a claim the skill was fully read when it was not.
//
// The fixture also plants a real `curl … | sh` inside that unreadable directory, so there are
// two independent ways to pass and only one way to fail. See judgeTraverseOnly.

// ansi matches the SGR escape sequences cc-audit emits even when stdout is redirected.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// FixtureNames is the closed set this adapter knows how to judge, in the corpus's own order.
// A fixture the corpus adds and this list does not carry becomes an untestable row rather than
// a silent absence — see Fixture.
func FixtureNames() []string {
	return []string{
		"traverse-only-dir", "fifo-as-skill", "symlink-cycle",
		"symlink-escape", "deeply-nested", "sparse-huge-config",
	}
}

// Fixture points cc-audit at one materialised fixture and judges it. dir is the fixture's own
// tree (the directory `corpus fixtures --materialize` created for it).
//
// It always returns a result, for the same reason Scan always returns a row: a fixture that
// produced no line is indistinguishable from one nobody ran.
func (a *Adapter) Fixture(ctx context.Context, name, dir string) adapter.FixtureResult {
	res := adapter.FixtureResult{Fixture: name}

	// The JSON pass, not the SARIF one: the verdict is not what is being judged here, and JSON
	// carries the summary and the score, which is what the detail needs to be arguable.
	out, stderr, code, err := a.run(ctx, a.argv(dir, "json"))

	if failed, detail := a.classify(code, err); failed {
		// A timeout IS the failure these fixtures look for: the FIFO one hangs a scanner that
		// opens and reads without stat-ing first, and the cycle one loops a walker with no
		// visited set. Distinguished from a crash because the two mean different things.
		res.Status = adapter.FixtureFail
		switch {
		case strings.Contains(detail, "did not finish within"):
			res.Detail = "did not terminate: " + detail
		case name == "sparse-huge-config" && strings.Contains(detail, "too large"):
			// Credit where it is due, because the fixture's expectation has two halves and the
			// tool met one of them cleanly. It DID bound the read — it refused at a declared
			// 10 MB limit instead of allocating 8 GiB, and named the file and both numbers. What
			// it did not do is the other half: the corpus asks for the oversized config to BE
			// the finding, and cc-audit made it a fatal error that aborted the whole tree, so
			// the skill got no verdict at all. A scanner that abandons a repository because one
			// file in it is too big has not scanned the repository.
			res.Detail = "bounded the read and said so, but aborted the whole scan rather than " +
				"reporting the oversized config as a finding, so the tree got no verdict: " + detail
		default:
			res.Detail = detail
		}
		return res
	}

	var scan ScanResult
	if uerr := json.Unmarshal(jsonPayload(out), &scan); uerr != nil {
		// Terminating with unreadable output is not a pass. A caller cannot tell such a run
		// from one that found nothing, which is the whole failure mode these fixtures probe.
		res.Status = adapter.FixtureFail
		res.Detail = fmt.Sprintf("terminated (exit %d) but its JSON did not parse: %v; stderr: %s",
			code, uerr, lastLine(stderr))
		return res
	}

	switch name {
	case "traverse-only-dir":
		return judgeTraverseOnly(res, scan, out, stderr)

	case "fifo-as-skill", "symlink-cycle", "symlink-escape", "deeply-nested", "sparse-huge-config":
		res.Status = adapter.FixturePass
		res.Detail = fmt.Sprintf("terminated within %s, exit %d, %d finding(s) reported",
			ScanTimeout, code, len(scan.Finding))
		if name == "sparse-huge-config" {
			// Stated on the row, not just in this file's comment, so it travels with the result
			// into baselines/results/ where somebody will read it out of context.
			res.Detail += ". NOTE: this fixture asserts a bounded READ, and this runner can " +
				"only observe termination — peak memory is not measured here"
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

// judgeTraverseOnly applies the corpus's own sentence for the one fixture with a positive
// requirement. Two independent ways to pass, because the fixture supplies two:
//
//  1. Say something about the directory it could not enumerate. cc-audit has no dimension-0
//     channel, so this appears as a tracing line — and measured 2026-09-23, it appears on
//     STDOUT, ahead of the JSON, which is why both streams are searched. The requirement is
//     "report that it could not be enumerated", not "use a particular mechanism" or a
//     particular stream.
//  2. Report the payload anyway. `private/setup.sh` is a real `curl … | sh`, so a scanner that
//     flags anything here has demonstrably not scored the readable part as the whole skill.
//
// Failing is the remaining case and only that case: terminated, said nothing, found nothing —
// a clean bill of health for a skill it read one half of.
func judgeTraverseOnly(res adapter.FixtureResult, scan ScanResult, stdout, stderr []byte) adapter.FixtureResult {
	if n := len(scan.Finding); n > 0 {
		res.Status = adapter.FixturePass
		res.Detail = fmt.Sprintf("did not score the readable part as the whole skill: %d "+
			"finding(s) reported, top severity %q, risk %s",
			n, topSeverity(scan.Summary), describeScore(ScoreOf(scan)))
		return res
	}
	if mention := mentionsUnreadable(stdout, stderr); mention != "" {
		res.Status = adapter.FixturePass
		res.Detail = "reported that it could not read part of the tree: " + mention +
			". NOTE, and it is material: this disclosure is a human-readable tracing line, not " +
			"a field of the result. The machine-readable document alongside it says passed=true " +
			"with no findings, so any consumer reading only the structured output gets a clean " +
			"bill of health for a skill that was read in part"
		return res
	}
	res.Status = adapter.FixtureFail
	res.Detail = fmt.Sprintf("terminated with no findings and no mention of the unreadable "+
		"subdirectory (risk %s, passed=%v). A `0111` subdirectory a walk cannot enumerate, "+
		"passed over in silence, is a claim the skill was read in full — and `private/setup.sh` "+
		"inside it is a real `curl | sh`",
		describeScore(ScoreOf(scan)), scan.Summary.Passed)
	return res
}

// mentionsUnreadable looks for the tool saying, on either stream, that something could not be
// read. Deliberately generous about wording and deliberately not generous about inventing one:
// if the tool has no such channel the answer is silence, and silence is the fail condition.
//
// The needles are the ENGLISH and numeric fragments on purpose. cc-audit's log text is
// localised — the line measured here reads
// "ディレクトリエントリの読み取りに失敗。スキップします" — but the errno tail
// "Permission denied (os error 13)" comes from the Rust std error and does not translate.
// Matching the prose would make this check depend on the operator's locale.
func mentionsUnreadable(streams ...[]byte) string {
	// Strip ANSI before matching and before returning. cc-audit colours its tracing output even
	// when stdout is not a terminal, and this string lands in fixtures.jsonl — invariant #7 is
	// the repository's own rule that control characters do not belong in anything a person
	// reads. The needles would still match with the codes in place; the DETAIL would not be
	// readable.
	joined := strings.Join(func() []string {
		out := make([]string, 0, len(streams))
		for _, s := range streams {
			out = append(out, string(s))
		}
		return out
	}(), "\n")
	joined = ansi.ReplaceAllString(joined, "")
	lower := strings.ToLower(joined)
	for _, needle := range []string{
		"permission denied", "could not read", "cannot read", "unreadable",
		"skipped", "failed to read", "access denied", "os error 13",
	} {
		if !strings.Contains(lower, needle) {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(joined), "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

// describeScore renders the 0-100 score for a fixture detail, keeping "absent" distinct from
// "zero" — the same distinction Score.Present exists for.
func describeScore(sc Score) string {
	if !sc.Present {
		return "not scored"
	}
	return fmt.Sprintf("%d/100 %s", sc.Total, sc.Level)
}

var _ adapter.FixtureRunner = (*Adapter)(nil)
