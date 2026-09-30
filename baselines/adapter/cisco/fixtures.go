// SPDX-License-Identifier: MIT

package cisco

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/basdotio/agent-guard/baselines/adapter"
)

// The six injected-fault fixtures, judged by the corpus's own tool-neutral `Expected` sentences —
// not by aguard's dimension-0 mechanism and not by cc-audit's. Per-fixture pass/fail, never a rate.
//
// One property of this tool shapes every judgement here: strict mode requires SKILL.md. Five
// fixtures write one; fifo-as-skill makes SKILL.md a FIFO, so the loader's open() blocks — the
// timeout IS the fixture's failure condition. A tree the loader refuses is untestable for the
// robustness claim, not a pass.

// FixtureNames is the closed set this adapter knows how to judge, in the corpus's order.
func FixtureNames() []string {
	return []string{
		"traverse-only-dir", "fifo-as-skill", "symlink-cycle",
		"symlink-escape", "deeply-nested", "sparse-huge-config",
	}
}

// Fixture points skill-scanner at one materialised fixture and always returns a result.
func (a *Adapter) Fixture(ctx context.Context, name, dir string) adapter.FixtureResult {
	res := adapter.FixtureResult{Fixture: name}
	out, stderr, code, err := a.run(ctx, a.argv(dir, "json"))

	switch {
	case err == context.DeadlineExceeded:
		res.Status, res.Detail = adapter.FixtureFail, fmt.Sprintf("did not terminate within %s", ScanTimeout)
		return res
	case err != nil:
		res.Status, res.Detail = adapter.FixtureFail, "could not execute "+a.Bin+": "+err.Error()
		return res
	case code != 0 && code != 1:
		res.Status, res.Detail = adapter.FixtureFail, fmt.Sprintf("exit %d: %s", code, firstErrorLine(stderr))
		return res
	}

	var scan ScanResult
	if uerr := json.Unmarshal(out, &scan); uerr != nil {
		line := firstErrorLine(stderr)
		if strings.HasPrefix(line, "Error loading skill") {
			// fifo-as-skill is the exception, and it is the fixture's own pass condition: "stat the
			// entry, see it is not a regular file, and skip or time out". Measured 2026-09-24 —
			// the loader refused with "Path is not a regular file" instead of blocking on open().
			// The first draft of this judgement filed that under "refused before traversal" and
			// called it untestable, which turned the one fixture the tool handled exactly right
			// into a non-result.
			if name == "fifo-as-skill" && strings.Contains(line, "not a regular file") {
				res.Status = adapter.FixturePass
				res.Detail = "did not block on the FIFO: the loader stat'd SKILL.md, saw it was not " +
					"a regular file and refused — " + line
				return res
			}
			// Otherwise strict mode refused the tree. For a robustness fixture that means the
			// walker never ran, so nothing about termination was tested; say so rather than pass.
			res.Status = adapter.FixtureUntestable
			res.Detail = "strict mode refused the tree before any traversal: " + line
			return res
		}
		res.Status = adapter.FixtureFail
		res.Detail = fmt.Sprintf("terminated (exit %d) but printed no JSON: %s", code, orNothing(line))
		return res
	}

	switch name {
	case "traverse-only-dir":
		return judgeTraverseOnly(res, scan, out, stderr)
	case "fifo-as-skill", "symlink-cycle", "symlink-escape", "deeply-nested", "sparse-huge-config":
		res.Status = adapter.FixturePass
		res.Detail = fmt.Sprintf("terminated within %s, exit %d, %d finding(s), max_severity %q",
			ScanTimeout, code, len(scan.Findings), scan.MaxSeverity)
		if name == "sparse-huge-config" {
			res.Detail += ". NOTE: this fixture asserts a bounded READ and this runner observes only termination"
		}
		return res
	}
	res.Status = adapter.FixtureUntestable
	res.Detail = "this adapter has no judgement for a fixture named " + name
	return res
}

// judgeTraverseOnly — the corpus's sentence: "report that `private/` could not be enumerated —
// do not score the readable part as if it were the whole skill. Silence here is a claim the skill
// was fully read when it was not." Two ways to pass, one to fail.
func judgeTraverseOnly(res adapter.FixtureResult, scan ScanResult, stdout, stderr []byte) adapter.FixtureResult {
	if n := countAboveInfo(scan); n > 0 {
		res.Status = adapter.FixturePass
		res.Detail = fmt.Sprintf("did not score the readable part as the whole skill: %d finding(s) above INFO, max_severity %q", n, scan.MaxSeverity)
		return res
	}
	if m := mentionsUnreadable(stdout, stderr); m != "" {
		res.Status = adapter.FixturePass
		res.Detail = "reported that part of the tree could not be read: " + m
		return res
	}
	res.Status = adapter.FixtureFail
	res.Detail = fmt.Sprintf("terminated with nothing above INFO and no mention of the unreadable "+
		"subdirectory (max_severity %q, is_safe=%v); `private/setup.sh` inside it is a real `curl | sh`",
		scan.MaxSeverity, scan.IsSafe)
	return res
}

// countAboveInfo — MANIFEST_MISSING_LICENSE (INFO) fires on nearly every skill, so a finding at
// INFO proves nothing about the unreadable half. Anything above it does.
func countAboveInfo(scan ScanResult) int {
	n := 0
	for _, f := range scan.Findings {
		if s := strings.ToUpper(f.Severity); s != "" && s != "INFO" {
			n++
		}
	}
	return n
}

func mentionsUnreadable(streams ...[]byte) string {
	var joined []string
	for _, s := range streams {
		joined = append(joined, string(s))
	}
	all := strings.Join(joined, "\n")
	lower := strings.ToLower(all)
	for _, needle := range []string{"permission denied", "could not read", "cannot read", "unreadable", "not enumerat", "skipped", "os error 13", "errno 13"} {
		if !strings.Contains(lower, needle) {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(all), "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

func orNothing(s string) string {
	if s == "" {
		return "nothing on stderr"
	}
	return s
}

var _ adapter.FixtureRunner = (*Adapter)(nil)
