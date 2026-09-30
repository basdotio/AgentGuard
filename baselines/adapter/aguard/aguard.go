// SPDX-License-Identifier: MIT

package aguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/baselines/adapter"
	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/baselines/scrub"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// ScanTimeout bounds one aguard run. The corpus's injected-fault fixtures include a FIFO in
// place of SKILL.md; a scanner that opens it hangs, and a runner that waits with it has scored
// nothing. A timeout is an Errored row, never a verdict.
const ScanTimeout = 60 * time.Second

// DimensionMap translates aguard's dimension names into the corpus vocabulary. It is a copy of
// `dimension_map` in the corpus's taxonomy/tools.yaml, kept in step by hand, because the corpus
// forbids itself from reading our rule reference and we do not vendor its taxonomy. An empty
// value is a deliberate non-mapping: obfuscation says the sample was hidden, not what it does,
// and intent mismatch is a property of the artifact's honesty, not a kind of attack.
var DimensionMap = map[string]string{
	"Prompt injection":      "injection",
	"Excessive permissions": "permission",
	"Data exfiltration":     "exfiltration",
	"Code execution":        "execution",
	"Supply chain":          "supply-chain",
	"Backdoor":              "backdoor",
	"Resource abuse":        "resource-abuse",
	"Filesystem":            "filesystem",
	"Obfuscation":           "",
	"Intent mismatch":       "",
}

// Adapter measures aguard. The binary is EXECUTED rather than linked: cmd/aguard.analyze is not
// importable, and a copy of its steps here would drift the first time it gained one.
type Adapter struct {
	// Bin is the aguard binary under measurement.
	Bin string
	// Threshold is the severity at which a finding makes the verdict malicious — the same
	// predicate `--fail-on` uses, so the measured gate is the shipped gate.
	Threshold model.Severity
	// XDG is a directory with no aguard config in it, shared across samples.
	XDG string
	// Work is where staged trees are built.
	Work string
	// ExtraArgs are appended to every invocation (LOCAL measurement only, e.g. `--llm --config …`
	// to measure the judge); ExtraEnv is appended to the isolated environment (e.g. the judge's
	// API key variable); Timeout overrides ScanTimeout when set (a judge run needs minutes).
	ExtraArgs []string
	ExtraEnv  []string
	Timeout   time.Duration
	// RawDir, when set, keeps each sample's scan JSON so a run can be re-folded without rerunning it.
	RawDir string
	// Keep leaves staged trees in place for inspection.
	Keep bool
}

// Tool implements adapter.Adapter.
func (a *Adapter) Tool() string { return "aguard" }

// Placement implements adapter.Adapter. This is the sentence the driver used to hardcode for
// every tool; it belongs to this one.
func (a *Adapter) Placement() string {
	return "by content, into a fake home; unplaceable trees are handed to `check` directly"
}

// Version reports the binary's own stamp rather than the repository's, because the thing being
// measured is the file on disk.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	// `version` is a subcommand, not a flag: `--version` exits 2 with "unknown flag".
	out, err := exec.CommandContext(ctx, a.Bin, "version").Output()
	if err != nil {
		return "", fmt.Errorf("read %s version: %w", a.Bin, err)
	}
	// First line only. `aguard version` also prints an advisory on stdout when the installed
	// plugin is older than the binary, and folding that into tool_version put a sentence about
	// `claude plugin update` inside the field a reader uses to identify what was measured.
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line, nil
}

// Scan points aguard at one sample and always returns a row.
//
// Two invocations, in this order, and the second one is the part the old runner lacked:
//
//  1. Place the tree by content into a fake home and run `scan --root`. This is the shipped
//     path and the one the 3,412 placeable samples go down; its output must not change.
//  2. If nothing could be placed, DO NOT return early — run `check <tree>` on the bare sample.
//     The old runner stopped here without executing the binary, which meant 127 samples were
//     never handed to the scanner and the recorded reason was the runner's belief about aguard
//     rather than aguard's behaviour. It also baked one tool's input model into the shared
//     layer: NVIDIA SkillSpector accepts single files, so those same trees may well be
//     scoreable for it.
//
// The second invocation needs care, because `check` on a directory holding no agent artifact
// returns score 100 with no findings and no notes (measured 2026-09-21 on
// corpus/malicious/mcp/ci-act_as_role_injection). Folding that would record a perfect benign
// verdict for a tree aguard never analysed — "calling it benign would be a runner scoring
// itself". So the discriminator is whether aguard COLLECTED anything: if every counter in the
// environment summary is zero, no agent artifact was read, and the row is NoVerdict with
// reason no-load-path. If something was collected, the result is folded normally — a sample
// that `scan` could not place but `check` could read is a scored sample, not an excuse.
func (a *Adapter) Scan(ctx context.Context, s corpus.Sample, tree string) ledger.Row {
	row := ledger.Row{Sample: s.Sample, Surface: s.Surface, Attempted: true}

	dir := filepath.Join(a.Work, filepath.Base(s.Sample))
	if !a.Keep {
		defer func() { _ = os.RemoveAll(dir) }()
	}

	st, err := Stage(tree, s.Sample, dir)
	var np NotPlaceable
	switch {
	case errors.As(err, &np):
		return a.checkBare(ctx, row, tree, np.Reason)
	case err != nil:
		row.Outcome = ledger.Errored
		row.Detail = "staging failed: " + err.Error()
		return row
	}

	res, err := a.run(ctx, "scan", "--root", st.Root, "--json", "--no-reputation", "--inbox", "off")
	if err != nil {
		row.Outcome = ledger.Errored
		row.Detail = err.Error()
		return row
	}
	a.keepRaw(s.Sample, res)
	return fill(row, res, a.Threshold)
}

// checkBare is invocation 2: aguard pointed straight at the sample tree.
func (a *Adapter) checkBare(ctx context.Context, row ledger.Row, tree, placementReason string) ledger.Row {
	res, err := a.run(ctx, "check", tree, "--json")
	if err != nil {
		row.Outcome = ledger.Errored
		row.Detail = "no load path for `scan` (" + placementReason + "); `check` then failed: " + err.Error()
		return row
	}
	if readNothing(res) {
		row.Outcome = ledger.NoVerdict
		row.Reason = ledger.NoLoadPath
		// Both halves: what placement found, and what the tool itself did when handed the
		// tree anyway. The second is the evidence; the first is why we had to ask.
		row.Detail = placementReason + "; `check` on the bare tree read nothing"
		return row
	}
	a.keepRaw(row.Sample, res)
	row = fill(row, res, a.Threshold)
	// Disclosed, not hidden. This verdict came from `check` on the bare tree rather than from
	// the `scan --root` placement this sample's surface implies, and the difference is not
	// cosmetic: `direction` §4 says aguard does not scan agent application source code, while
	// `check` measurably does read it. Whether these belong in the headline denominator is a
	// positioning decision, so the row carries the fact and the scorecard owes both figures.
	row.Flags = append(row.Flags, ledger.SecondarySurface)
	row.Detail = placementReason + "; scored by `check` on the bare tree instead"
	return row
}

// readNothing reports whether aguard had nothing at all to read — no collected artifact AND no
// artifact report of any kind. Both halves are needed, and finding that out cost a wrong
// assumption worth writing down.
//
// The first draft used the environment counters alone: all zero means no skill, hook, MCP server
// or connector was collected, so the sample must be outside the input model. Measured, that is
// false. `aguard check` on a bare directory emits an artifact of kind "directory" and DOES read
// the file contents inside it — on the corpus's 127 Python MCP server trees it produces at least
// one finding on 55 of them and trips the gate at `high` on 14 — all with every environment
// counter at zero. Calling those unmeasured would have discarded 14 real catches and, worse,
// kept 127 malicious samples out of the recall denominator on the strength of a boundary that
// the shipped `check` command does not actually have.
//
// So the rule is narrow and deliberately so: only the absence of ANY artifact report counts as
// unread. A `kind: directory` artifact with score 100 and no findings IS a measurement — aguard
// was pointed at the tree, globbed it, and made a claim. That its claim is silent about files it
// skipped for extension reasons is a real defect in aguard's disclosure (filed separately, and
// it is squarely invariant #5's territory), but it is not something this adapter can paper over
// by inventing a no-verdict it cannot substantiate. An adapter that guessed "it probably did not
// read this" would be putting its own belief about the tool back into the measurement, which is
// the exact failure this work item removed.
func readNothing(res model.ScanResult) bool {
	if len(res.Artifacts) > 0 {
		for _, art := range res.Artifacts {
			if len(art.Findings) > 0 {
				return false
			}
		}
	}
	// Round-tripped through JSON rather than field by field, deliberately. EnvSummary has
	// sixteen counters today and gains one every time aguard learns a new surface; a hand-
	// written list would go stale silently, and going stale in this direction means a sample
	// that WAS read gets recorded as unreadable. The check's whole job is "did aguard read
	// anything at all", so it has to see every counter, including ones added after this was
	// written.
	b, err := json.Marshal(res.Env)
	if err != nil {
		return false // cannot prove nothing was read; do not claim it
	}
	var counters map[string]float64
	if err := json.Unmarshal(b, &counters); err != nil {
		return false
	}
	for _, n := range counters {
		if n > 0 {
			return false
		}
	}
	return len(res.Artifacts) == 0
}

// fill folds a scan into the one word the corpus scores, at the gate's own predicate. Severity
// is the highest deterministic severity seen, reported whether or not it flagged, so a second
// pass at another threshold can be read off the same file. Dimensions are those of the findings
// that carried the flag, translated; unmapped ones are dropped, not invented.
func fill(row ledger.Row, res model.ScanResult, threshold model.Severity) ledger.Row {
	row.Outcome = ledger.Scored
	row.Verdict = "benign"
	var maxSev model.Severity
	dims := map[string]bool{}
	for _, art := range res.Artifacts {
		for _, f := range art.Findings {
			if !score.Deterministic(f) {
				continue
			}
			if f.Severity.Rank() > maxSev.Rank() {
				maxSev = f.Severity
			}
			if f.Severity.Rank() >= threshold.Rank() {
				row.Verdict = "malicious"
				if d := DimensionMap[model.DimensionName(f.Dimension)]; d != "" {
					dims[d] = true
				}
			}
		}
	}
	if maxSev != "" {
		row.Severity = string(maxSev)
	}
	for d := range dims {
		row.Dimensions = append(row.Dimensions, d)
	}
	sort.Strings(row.Dimensions)
	row.Rules = FlaggingRules(res, threshold)
	return row
}

// FlaggingRules returns the rule ids of the findings that would carry a flag at threshold — the
// per-rule diagnostic the scorer does not give and the one a rule author acts on.
func FlaggingRules(res model.ScanResult, threshold model.Severity) []string {
	seen := map[string]bool{}
	for _, art := range res.Artifacts {
		for _, f := range art.Findings {
			if score.Deterministic(f) && f.Severity.Rank() >= threshold.Rank() {
				seen[f.RuleID] = true
			}
		}
	}
	if len(seen) == 0 {
		// nil rather than an empty slice: the field is omitted from the ledger JSON, and a
		// caller comparing two rows sees no difference between "no rules" and "field unset".
		return nil
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// run executes the binary with an isolated environment and decodes its JSON. HOME and
// XDG_CONFIG_HOME point into the work directory so nothing from the operator's real ~/.claude,
// ~/.claude.json or aguard config can leak into a sample.
func (a *Adapter) run(ctx context.Context, args ...string) (model.ScanResult, error) {
	timeout := ScanTimeout
	if a.Timeout > 0 {
		timeout = a.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if len(args) > 0 && args[0] == "scan" { // the judge only exists on scan; check/version reject --llm
		args = append(append([]string{}, args...), a.ExtraArgs...)
	}
	cmd := exec.CommandContext(ctx, a.Bin, args...)
	cmd.Env = append([]string{
		"HOME=" + a.Work,
		"XDG_CONFIG_HOME=" + a.XDG,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}, a.ExtraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return model.ScanResult{}, fmt.Errorf("aguard did not finish within %s", timeout)
		}
		// Exit 1 is the documented contract for "a finding reached --fail-on" — `check`
		// defaults to `high`, so the gate firing is the expected outcome on a malicious
		// sample, not a failed run. Treating it as an error cost an afternoon: every
		// unplaceable malicious sample came back as Errored. Exit 2 is a real runtime error
		// and anything else is unknown; both stay errors.
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return model.ScanResult{}, fmt.Errorf("aguard: %v: %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	var res model.ScanResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return model.ScanResult{}, fmt.Errorf("aguard output is not a scan result: %w", err)
	}
	return res, nil
}

// ScanRoot is the old runner's entry point, kept so hack/corpus-runner stays a thin driver over
// this package rather than a second implementation.
func (a *Adapter) ScanRoot(ctx context.Context, st StagedRoot) (model.ScanResult, error) {
	return a.run(ctx, "scan", "--root", st.Root, "--json", "--no-reputation", "--inbox", "off")
}

func (a *Adapter) keepRaw(id string, res model.ScanResult) {
	if a.RawDir == "" {
		return
	}
	if b, err := json.Marshal(res); err == nil {
		// Every path aguard reports sits under the staging root, which sits in the operator's
		// per-user temp directory; <work> keeps the staged location and drops the machine.
		b = scrub.New(map[string]string{"<work>": a.Work}).Bytes(b)
		_ = os.WriteFile(filepath.Join(a.RawDir, filepath.Base(id)+".json"), b, 0o644)
	}
}

var _ adapter.Adapter = (*Adapter)(nil)
