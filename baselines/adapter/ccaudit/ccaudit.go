// SPDX-License-Identifier: MIT

// Package ccaudit measures ryo-ebata/cc-audit against agent-artifact-corpus.
//
// # Why this tool
//
// It is the one competitor that matches aguard's own claim head-on: a deterministic, offline,
// recomputable 0-100 score with no LLM in the loop (src/scoring.rs — integer weights 40/20/10/5
// capped at 100, critical weighted 40 exactly as ours is, polarity inverted). Everything this
// project says about being different rests on the clause that follows in the research notes —
// "but its coverage is narrow" — and the whole evidence for that clause was one hook on one
// machine. This adapter replaces it with 3,539 samples or retires it.
//
// # What is and is not verified here
//
// Read at tag v3.23.9 (2026-09-22) from the published source and docs, WITHOUT executing the
// binary — the same discipline the four-competitor survey used. Verified by reading:
//
//   - `cc-audit check <PATHS>...` takes files or directories and recurses by default (docs/CLI.md).
//   - `--format sarif` and `--format json` are both emitted by the same scan (src/reporter/).
//   - The SARIF reporter maps Critical AND High to `error`, Medium to `warning`, Low to `note`
//     (src/reporter/sarif.rs severity_to_level). This is why the verdict is folded at `error`
//     for the default tier and why severity comes from the JSON pass instead.
//   - `risk_score` is Option with skip_serializing_if, so an absent key is not a zero score
//     (src/rules/types.rs).
//   - Exit 1 is the gate firing, not a malfunction (README: "Result: FAIL (exit code 1)").
//
// NOT verified, and owed to W6 on the real binary: that the default tier corresponds to
// critical+high in practice; whether a default `check` touches the network at all (the only
// outbound path found in source is --report-fp, which shells out to `gh issue create`, and we
// never pass it); and whether every corpus surface is one cc-audit looks at, which is what the
// surface tripwire exists to catch before any figure is published.
package ccaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/baselines/adapter"
	"github.com/basdotio/AgentGuard/baselines/adapter/sarif"
	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// ScanTimeout bounds one scan. Matches the aguard adapter's: the corpus's injected-fault
// fixtures include a FIFO in place of SKILL.md, and a runner that blocks forever on one has
// scored nothing. A timeout is an Errored row, never a verdict.
const ScanTimeout = 60 * time.Second

// SamplePlaceholder is replaced with the sample's path in ScanArgv.
const SamplePlaceholder = "{{sample}}"

// PinnedVersion is the release this adapter measures. cc-audit ships roughly a release a day — 146 of
// them, v1.0.0 to v3.0.0 inside 24 hours — so an unpinned figure cannot be recomputed a week
// later, and "recomputable offline" is the very claim being tested.
const PinnedVersion = "v3.23.9"

// DefaultScanArgv and DefaultVersionArgv come from docs/CLI.md at the pinned tag. The format
// flag is filled in per pass, because the verdict and the score come from two different
// documents of the same scan.
var (
	DefaultScanArgv    = []string{"check", SamplePlaceholder, "--format"}
	DefaultVersionArgv = []string{"--version"}
)

// Adapter measures cc-audit. The binary is executed, never linked.
type Adapter struct {
	// Bin is the cc-audit binary.
	Bin string
	// ScanArgv is the argv after Bin, with SamplePlaceholder standing in for the sample path
	// and a trailing "--format" the pass completes. Empty means DefaultScanArgv.
	ScanArgv []string
	// VersionArgv likewise; empty means DefaultVersionArgv.
	VersionArgv []string
	// Tier is which of cc-audit's two gates to measure. Empty means TierDefault.
	Tier Tier
	// Config is the .cc-audit.yaml this run scans under, passed as --config.
	//
	// Required, and measured rather than assumed: `cc-audit check` with no config exits 2 with
	// "Configuration file not found" and scans nothing. `cc-audit init` writes the default
	// template. It is passed explicitly rather than left to discovery for two reasons — the
	// file is POLICY (it sets per-rule error/warn/ignore, so it decides what counts as a
	// finding, which is why the driver hashes it into run.yaml), and discovery would search
	// upward from the scanned path, which on a corpus checkout means a stray config in the
	// corpus tree silently changes the answer.
	Config string
	// RawDir, when set, keeps each sample's SARIF and JSON for later inspection. Not committed.
	RawDir string
}

// Tool implements adapter.Adapter.
func (a *Adapter) Tool() string { return "ccaudit" }

// Placement implements adapter.Adapter. No staging at all: `cc-audit check` takes a directory
// and recurses by default, so the sample tree is handed over exactly as the corpus ships it.
// That is what made it the first third-party column — placement is where a comparison is
// usually lost, and here there is nothing to get wrong.
func (a *Adapter) Placement() string {
	return "none: the sample tree is passed to `check` as-is, which recurses by default"
}

// Version reports what is about to be executed, read from the binary rather than assumed. A
// figure attributed to the wrong version is worse than an unattributed one, and with a release a
// day this tool makes that easy to do.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	args := a.VersionArgv
	if len(args) == 0 {
		args = DefaultVersionArgv
	}
	out, err := exec.CommandContext(ctx, a.Bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read %s version with %v: %w", a.Bin, args, err)
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "", fmt.Errorf("%s %v printed nothing", a.Bin, args)
	}
	return line, nil
}

// Scan points cc-audit at one sample and always returns a row — no error return, for the same
// reason the aguard adapter has none: a sample that disappears when a run fails is the silent
// gap the ledger exists to remove.
//
// Placement is the easy half here, which is most of why this tool made the first column:
// `check` takes a directory and recurses, so the sample tree is handed over as-is with no
// staging and no guessing about where a config root should be.
//
// Two invocations, because the verdict and the score live in different documents: SARIF decides
// the word, JSON supplies cc-audit's own severity and its 0-100 score. The verdict is never
// taken from the JSON pass — see reconcile for what happens when they disagree.
func (a *Adapter) Scan(ctx context.Context, s corpus.Sample, tree string) ledger.Row {
	row := ledger.Row{Sample: s.Sample, Surface: s.Surface, Attempted: true}

	out, stderr, code, err := a.run(ctx, a.argv(tree, "sarif"))
	if failed, detail := a.classify(code, err); failed {
		row.Outcome = ledger.Errored
		row.Detail = detail
		return row
	}
	var log sarif.Log
	if uerr := json.Unmarshal(jsonPayload(out), &log); uerr != nil {
		row.Outcome = ledger.Errored
		row.Detail = fmt.Sprintf("output is not SARIF (exit %d): %v; stderr: %s",
			code, uerr, lastLine(stderr))
		return row
	}
	a.keepRaw(s.Sample, "sarif.json", out)
	row = sarif.Fold(row, log, a.tier().Threshold(), sarif.ArtifactsNotEmitted)

	// The JSON pass runs even when the SARIF pass produced no verdict: a row that says "read
	// nothing" is worth more with the tool's own summary attached than without it, and the
	// second invocation costs the same either way.
	jout, jstderr, jcode, jerr := a.run(ctx, a.argv(tree, "json"))
	if failed, detail := a.classify(jcode, jerr); failed {
		row.Outcome = ledger.Errored
		row.Verdict, row.Severity, row.Rules = "", "", nil
		row.Detail = "the SARIF pass succeeded but the JSON pass did not, so the score cannot " +
			"be paired with the verdict: " + detail
		return row
	}
	var res ScanResult
	if uerr := json.Unmarshal(jsonPayload(jout), &res); uerr != nil {
		row.Outcome = ledger.Errored
		row.Verdict, row.Severity, row.Rules = "", "", nil
		row.Detail = fmt.Sprintf("the JSON pass output did not parse (exit %d): %v; stderr: %s",
			jcode, uerr, lastLine(jstderr))
		return row
	}
	a.keepRaw(s.Sample, "json", jout)
	return reconcile(row, ScoreOf(res), res, a.tier())
}

func (a *Adapter) tier() Tier {
	if a.Tier == "" {
		return TierDefault
	}
	return a.Tier
}

// argv builds one pass's command line. The format is appended rather than substituted so the
// two passes cannot drift apart in anything but the format.
func (a *Adapter) argv(tree, format string) []string {
	args := a.ScanArgv
	if len(args) == 0 {
		args = DefaultScanArgv
	}
	argv := make([]string, 0, len(args)+5)
	for _, arg := range args {
		argv = append(argv, strings.ReplaceAll(arg, SamplePlaceholder, tree))
	}
	argv = append(argv, format)
	if a.Config != "" {
		argv = append(argv, "--config", a.Config)
	}
	return append(argv, a.tier().Args()...)
}

// classify turns a run's outcome into "did this fail, and what does the reader need to know".
// It exists so both passes answer the question the same way.
func (a *Adapter) classify(code int, err error) (bool, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return true, fmt.Sprintf("did not finish within %s", ScanTimeout)
	case err != nil && sarif.IsRunFailure(code):
		return true, fmt.Sprintf("exit %d: %v", code, err)
	case err != nil && code == 0:
		// Could not start at all — a missing binary, a permission problem. Distinct from a
		// non-zero exit, and the message has to say which so the reader knows where to look.
		return true, "could not execute " + a.Bin + ": " + err.Error()
	}
	return false, ""
}

// run executes cc-audit and returns stdout, stderr, the exit code and any error. Exit 1 is the
// gate firing and is NOT an error here; sarif.IsRunFailure decides what counts.
func (a *Adapter) run(ctx context.Context, argv []string) (stdout, stderrOut []byte, code int, err error) {
	ctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.Bin, argv...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, se.Bytes(), 0, context.DeadlineExceeded
	}
	if runErr == nil {
		return so.Bytes(), se.Bytes(), 0, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		c := ee.ExitCode()
		if !sarif.IsRunFailure(c) {
			return so.Bytes(), se.Bytes(), c, nil
		}
		return so.Bytes(), se.Bytes(), c, fmt.Errorf("%w: %s", runErr, lastLine(se.Bytes()))
	}
	return nil, se.Bytes(), 0, runErr
}

// jsonPayload returns the document inside a stream that may have log lines in front of it.
//
// Measured 2026-09-23: cc-audit writes tracing output to STDOUT, ahead of the document, so
// `--format json` is not parseable whenever a warning fires. On the traverse-only-dir fixture
// stdout begins with an ANSI-coloured
//
//	WARN cc_audit::engine::scanners::walker: … error=IO error … Permission denied (os error 13)
//
// and the JSON starts on line 2. Nothing in the 3,539-sample run hit this, which is the danger:
// the failure appears only on trees with something unreadable in them — exactly the trees whose
// answers matter most — and it would have surfaced as "output is not SARIF/JSON" rather than as
// what it is.
//
// The first `{` or `[` at the start of a line is the boundary. Deliberately conservative: it
// does not strip anything once the document has begun, so a tool that interleaves log lines
// INTO its output still fails to parse rather than being silently repaired.
func jsonPayload(b []byte) []byte {
	if len(b) > 0 && (b[0] == '{' || b[0] == '[') {
		return b
	}
	for i := 0; i < len(b); i++ {
		if i > 0 && b[i-1] != '\n' {
			continue
		}
		if b[i] == '{' || b[i] == '[' {
			return b[i:]
		}
	}
	return b
}

// lastLine trims stderr to something that fits on a ledger row. The last non-empty line is the
// one that says what went wrong; anything before it is usually progress.
func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

func (a *Adapter) keepRaw(id, ext string, b []byte) {
	if a.RawDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(a.RawDir, filepath.Base(id)+"."+ext), b, 0o644)
}

var _ adapter.Adapter = (*Adapter)(nil)
