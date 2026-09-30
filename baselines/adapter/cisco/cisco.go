// SPDX-License-Identifier: MIT

// Package cisco measures cisco-ai-defense/skill-scanner against agent-artifact-corpus.
//
// # Why this tool
//
// It is the closest comparator surveyed: default path zero LLM, zero network, zero account,
// deterministic, four CI gate forms, the only vendor that published a reproducible P/R and
// graded itself as failing. Until this adapter there was no figure between it and aguard.
//
// # The one contract it breaks
//
// cli.py returns only 0 and 1 (v2.1.0, lines 546-803). Exit 1 is the gate firing — AND a
// missing directory, AND a taxonomy load failure, AND SkillLoadError. `sarif.IsRunFailure`'s
// "0 or 1 means the run worked" is false here. So this adapter never decides from the exit code
// alone: exit 1 with SARIF on stdout is a verdict; exit 1 with `Error loading skill` on stderr is
// the tool refusing the input shape (no-verdict / unsupported-input); exit 1 with any other
// `Error` is a run failure. Any tool whose "no credentials" path also exits 1 sets the same trap.
//
// # What is and is not verified here
//
// Read at tag 2.1.0 from source and docs, unexecuted: the SARIF level map, the absence of an
// `artifacts` array (so ArtifactsNotEmitted), the severity ladder, the exit-code collapse, the
// strict-mode SKILL.md requirement and the `--lenient` fallback. NOT verified until W2's probes:
// the exact stderr wording, whether stdout carries log lines ahead of the document, what a
// policy dump looks like, and what `marshal.load()` does with the corpus's one `.pyc`.
package cisco

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

	"github.com/basdotio/agent-guard/baselines/adapter"
	"github.com/basdotio/agent-guard/baselines/adapter/sarif"
	"github.com/basdotio/agent-guard/baselines/corpus"
	"github.com/basdotio/agent-guard/baselines/ledger"
)

// ScanTimeout bounds one scan; a FIFO in place of SKILL.md must be an Errored row, not a hang.
const ScanTimeout = 60 * time.Second

// SamplePlaceholder is replaced with the sample's path in ScanArgv.
const SamplePlaceholder = "{{sample}}"

// PinnedVersion is the PyPI release this adapter measures; the arm64 wheel's sha256 is recorded in
// run.yaml by the install step, not here.
const PinnedVersion = "2.1.0"

// DefaultScanArgv and DefaultVersionArgv come from README.md and docs/ at the pinned tag.
var (
	DefaultScanArgv    = []string{"scan", SamplePlaceholder, "--format"}
	DefaultVersionArgv = []string{"--version"}
)

// Adapter measures skill-scanner. The binary is executed, never linked.
type Adapter struct {
	// Bin is the skill-scanner entry point (a console script inside a pinned venv).
	Bin string
	// ScanArgv overrides DefaultScanArgv; VersionArgv likewise.
	ScanArgv, VersionArgv []string
	// Threshold is passed to --fail-on-severity and decides where the SARIF fold gates.
	// Empty means SeverityHigh, which is what the tool's own legacy --fail-on-findings meant.
	Threshold Severity
	// Policy is the policy file passed as --policy and hashed into run.yaml. Required: the
	// built-in default is dumped to a file first so that "default" is a hash, not a version
	// number.
	Policy string
	// RawDir, when set, keeps each sample's SARIF. Not committed.
	RawDir string
}

// Tool implements adapter.Adapter.
func (a *Adapter) Tool() string { return "skill-scanner" }

// Placement implements adapter.Adapter. Strict mode is kept on purpose: it is the shipped
// behaviour, and it is the one decision that empties four rows of the comparison.
func (a *Adapter) Placement() string {
	return "the sample tree is passed to `scan` as-is in strict mode (no --lenient): a tree " +
		"without SKILL.md is refused by the loader and recorded as no-verdict/unsupported-input, " +
		"which is why mcp, hooks, permission, instruction and connector surfaces read as uncovered"
}

// Version reads what is about to be executed rather than assuming it.
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

func (a *Adapter) threshold() Severity {
	if a.Threshold == "" {
		return SeverityHigh
	}
	return a.Threshold
}

func (a *Adapter) argv(tree, format string) []string {
	args := a.ScanArgv
	if len(args) == 0 {
		args = DefaultScanArgv
	}
	argv := make([]string, 0, len(args)+4)
	for _, arg := range args {
		argv = append(argv, strings.ReplaceAll(arg, SamplePlaceholder, tree))
	}
	argv = append(argv, format, "--fail-on-severity", string(a.threshold()))
	if a.Policy != "" {
		argv = append(argv, "--policy", a.Policy)
	}
	return argv
}

// Scan always returns a row. Decided by what the tool PRINTED, never by the exit code alone —
// see the package comment.
func (a *Adapter) Scan(ctx context.Context, s corpus.Sample, tree string) ledger.Row {
	row := ledger.Row{Sample: s.Sample, Surface: s.Surface, Attempted: true}

	out, stderr, code, err := a.run(ctx, a.argv(tree, "sarif"))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		row.Outcome, row.Detail = ledger.Errored, fmt.Sprintf("did not finish within %s", ScanTimeout)
		return row
	case err != nil:
		row.Outcome, row.Detail = ledger.Errored, "could not execute "+a.Bin+": "+err.Error()
		return row
	case code != 0 && code != 1:
		// Not a code this tool documents. Whatever is on stdout, the run is not trusted.
		row.Outcome, row.Detail = ledger.Errored, fmt.Sprintf("exit %d: %s", code, firstErrorLine(stderr))
		return row
	}

	var log sarif.Log
	if uerr := json.Unmarshal(out, &log); uerr == nil && len(log.Runs) > 0 {
		a.keepRaw(s.Sample, "sarif.json", out)
		row = sarif.Fold(row, log, a.threshold().Level(), sarif.ArtifactsNotEmitted)
		return a.jsonPass(ctx, row, s.Sample, tree)
	}

	// No SARIF. Exit 0 or 1 with nothing to fold is one of the tool's error paths, and which
	// one is on stderr.
	line := firstErrorLine(stderr)
	if strings.HasPrefix(line, "Error loading skill") {
		row.Outcome, row.Reason = ledger.NoVerdict, ledger.UnsupportedInput
		row.Detail = line + " — strict mode refuses a tree without SKILL.md; this is the tool " +
			"declining the input shape, not a miss and not a crash"
		return row
	}
	row.Outcome = ledger.Errored
	if line == "" {
		line = "nothing on stderr"
	}
	row.Detail = fmt.Sprintf("exit %d with no SARIF on stdout: %s", code, line)
	return row
}

// jsonPass runs the scan again for the tool's own ladder and categories. It runs only after a
// SARIF verdict exists: a tree the loader refused will be refused again, and one Errored row is
// enough. A JSON pass that fails or contradicts the SARIF pass turns the row Errored — see
// reconcile.
func (a *Adapter) jsonPass(ctx context.Context, row ledger.Row, id, tree string) ledger.Row {
	out, stderr, code, err := a.run(ctx, a.argv(tree, "json"))
	if err != nil || (code != 0 && code != 1) {
		row.Outcome, row.Verdict, row.Severity, row.Rules = ledger.Errored, "", "", nil
		row.Detail = fmt.Sprintf("the SARIF pass succeeded but the JSON pass did not (exit %d, %v): %s",
			code, err, firstErrorLine(stderr))
		return row
	}
	var res ScanResult
	if uerr := json.Unmarshal(out, &res); uerr != nil {
		row.Outcome, row.Verdict, row.Severity, row.Rules = ledger.Errored, "", "", nil
		row.Detail = fmt.Sprintf("the JSON pass output did not parse (exit %d): %v; stderr: %s",
			code, uerr, firstErrorLine(stderr))
		return row
	}
	a.keepRaw(id, "json", out)
	return reconcile(row, res)
}

// run executes skill-scanner. Exit 1 is NOT an error here; Scan decides what it meant.
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
	var ee *exec.ExitError
	switch {
	case runErr == nil:
		return so.Bytes(), se.Bytes(), 0, nil
	case errors.As(runErr, &ee):
		return so.Bytes(), se.Bytes(), ee.ExitCode(), nil
	}
	return nil, se.Bytes(), 0, runErr
}

// firstErrorLine is the first non-empty stderr line that starts with "Error", else the last
// non-empty line. cli.py prints its failures as `Error…: …` on one line.
func firstErrorLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for _, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "Error") {
			return t
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func (a *Adapter) keepRaw(id, ext string, doc []byte) {
	if a.RawDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(a.RawDir, filepath.Base(id)+"."+ext), doc, 0o644)
}

var _ adapter.Adapter = (*Adapter)(nil)
