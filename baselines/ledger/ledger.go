// SPDX-License-Identifier: MIT

// Package ledger owns one invariant, and it is the one every baseline rests on: every test point in
// agent-artifact-corpus ends up in exactly one recorded outcome, and none of them can be
// absent.
//
// The defect it replaces: hack/corpus-runner emits no verdict line at all for a sample it
// cannot place, and `corpus score` then discovers the hole by subtraction and reports it as
// "uncovered". That reports THAT something is missing, never WHY, and — the part that matters
// — a hole found by subtraction does not announce when it grows. A rule change that quietly
// stops placing another 200 samples looks like a smaller denominator, not like a regression.
//
// So the outcomes are a partition of the work list, they are checked, and there is
// deliberately no `skipped`: no row may say "we did not run it". A sample that produced
// nothing carries NoVerdict, which asserts the scanner WAS pointed at it (Attempted) and
// names a reason from a closed set. This is invariant #5 ("any omission must not be silent",
// .claude/rules/invariants.md) applied to the measuring apparatus rather than to a scan.
//
// Two things that look like outcomes and are not: OwnFixture and SurfaceUndeclared. Neither is
// an inability to measure — a sample derived from the tool's own test suite is still measured,
// it is the PUBLISHED FIGURE that owes two denominators — so they are flags on a scored row.
// Allowing either on a NoVerdict row would reintroduce the skip under a new name, and Check
// refuses it.
//
// Reasons are per-tool and decided AFTER the tool ran. NoLoadPath is a fact about one
// scanner's input model, not about the corpus: the 127 cisco-derived MCP samples are Python
// server implementations that aguard has no load path for, while NVIDIA SkillSpector accepts
// single files and may well score them. One tool's product boundary must not decide how much
// of the corpus another tool is measured on.
package ledger

import (
	"fmt"
	"slices"
	"sort"
)

// Outcome is what happened to one sample. These three partition the work list.
type Outcome string

const (
	// Scored means a verdict was produced and belongs in the file `corpus score` reads.
	Scored Outcome = "scored"
	// NoVerdict means the scanner was pointed at the sample and produced nothing that folds
	// to one word. It is NOT benign: calling it benign would score a disclosed product
	// boundary as a correct answer, which is the mistake hack/corpus-runner's own comment
	// refuses ("Calling it benign would be a runner scoring itself").
	NoVerdict Outcome = "no-verdict"
	// Errored means the run failed: timeout, crash, unparseable output.
	Errored Outcome = "error"
)

// Reason says why a NoVerdict row has no verdict. The set is closed on purpose: a free-text
// reason becomes somewhere to write "did not seem relevant", which is a silent gap wearing a
// label. Adding one means adding it to KnownReasons and to Explain, and Check enforces both.
type Reason string

const (
	// NoLoadPath: the tool read nothing it recognises as something an agent would load.
	NoLoadPath Reason = "no-load-path"
	// UnsupportedInput: the tool refused the input shape outright.
	UnsupportedInput Reason = "unsupported-input"
	// NoOutput: the tool ran and exited cleanly but emitted nothing a verdict folds from.
	NoOutput Reason = "no-output"
)

// Flag is a property of a measured row that the published figure has to disclose.
type Flag string

const (
	// OwnFixture: this sample derives from the measured tool's own test suite, so a figure
	// including it is partly the tool graded on its own fixtures. Measured anyway; published
	// with both denominators.
	OwnFixture Flag = "own-fixture"
	// SurfaceUndeclared: the tool has not yet declared whether it covers this surface, so a
	// whole-surface zero cannot yet be read as either bad placement or a product boundary.
	SurfaceUndeclared Flag = "surface-undeclared"
	// SecondarySurface: the verdict came from an invocation other than the one this sample's
	// surface implies. It exists because of a measured surprise: aguard's placement cannot put
	// a Python MCP server tree on a load path, but `aguard check` pointed straight at the tree
	// reads it anyway and flags 14 of the corpus's 127 such samples. Those are real catches and
	// real misses, not a coverage gap — but they were produced by a different entry point than
	// the rest of the run, and a figure that mixes the two without saying so is not readable.
	SecondarySurface Flag = "secondary-surface"
)

// Row is one sample's entry. The JSON names are the on-disk ledger format.
type Row struct {
	Sample    string   `json:"sample"`
	Outcome   Outcome  `json:"outcome"`
	Attempted bool     `json:"attempted"`
	Verdict   string   `json:"verdict,omitempty"`  // Scored only: malicious | benign
	Severity  string   `json:"severity,omitempty"` // the tool's own ladder, if it has one
	Reason    Reason   `json:"reason,omitempty"`   // NoVerdict only
	Detail    string   `json:"detail,omitempty"`   // the tool's own words; required on Errored
	Flags     []Flag   `json:"flags,omitempty"`    // Scored only
	Surface   []string `json:"surface,omitempty"`  // carried from the work list, for the tripwire
	// Dimensions is which KIND of problem, in the corpus's vocabulary. Carried so the verdict
	// file can be derived from the ledger rather than assembled twice; Check does not inspect
	// it, because attribution is `corpus score`'s question, not the partition's.
	Dimensions []string `json:"dimensions,omitempty"` // Scored only
	// Rules is which of the tool's own rules carried the flag. The scorer cannot produce this —
	// it knows nothing about any tool's rule ids — and it is the one diagnostic a rule author
	// acts on: "EXFIL-001 fired on 144 benign samples" is a work item, while "false positives
	// are 5.5%" is only a status. Check does not inspect it.
	Rules []string `json:"rules,omitempty"` // Scored only
	// JudgeUsage is what the tool's model-backed pass cost on this sample (aguard's --llm judge),
	// folded from the tool's own output. Nil when that output carried no judge summary — every
	// static run, and any sample routed where the judge does not run — so a static ledger is
	// byte-for-byte what it was. Check does not inspect it: it is the run's cost, not the partition.
	JudgeUsage *JudgeUsage `json:"judge_usage,omitempty"`
}

// The two bases a JudgeUsage's TriageCalls can rest on.
const (
	// UsageReported: the tool counted its triage calls and retries itself and printed them.
	UsageReported = "reported"
	// UsageDerived: the tool printed neither (a binary older than its judge summary's cost
	// fields), so TriageCalls is the documented derivation — one per artifact with a
	// deterministic finding, exact only when no call was skipped — and Retries is unknown.
	UsageDerived = "derived"
)

// JudgeUsage is one sample's judge cost. Every count is the tool's own except TriageCalls, which
// is derived when the tool did not print it, and Basis says which. Retries and the two token
// counts are pointers because "the tool did not say" is not 0: a derived sample has no retry
// count, and an endpoint that reports no usage leaves the tokens out. Repaired is not a pointer: a
// binary that does not print it predates the repair (P-034), so its count is exactly 0, and a row
// with none reads as it did before the count existed.
type JudgeUsage struct {
	Basis            string `json:"basis"`
	Calls            int    `json:"calls"`
	Failed           int    `json:"failed"`
	Skipped          int    `json:"skipped"`
	Repaired         int    `json:"repaired,omitempty"` // answered calls read by the closed-early repair (P-034)
	TriageCalls      int    `json:"triage_calls"`
	Retries          *int   `json:"retries,omitempty"`
	PromptTokens     *int   `json:"prompt_tokens,omitempty"`
	CompletionTokens *int   `json:"completion_tokens,omitempty"`
}

// KnownReasons returns the closed set, sorted so callers and error messages are stable.
func KnownReasons() []Reason {
	rs := []Reason{NoLoadPath, UnsupportedInput, NoOutput}
	slices.Sort(rs)
	return rs
}

// Explain returns what a reason claims, in the words a ledger reader needs. An empty string
// means the reason is not in the closed set — which is how Check and the documentation test
// tell "new reason" from "typo".
func Explain(r Reason) string {
	switch r {
	case NoLoadPath:
		return "the tool read nothing it recognises as a path an agent loads from"
	case UnsupportedInput:
		return "the tool refused this input shape"
	case NoOutput:
		return "the tool ran and exited cleanly but produced nothing a verdict folds from"
	}
	return ""
}

// knownFlag reports whether f is one of the two disclosure flags.
func knownFlag(f Flag) bool {
	return f == OwnFixture || f == SurfaceUndeclared || f == SecondarySurface
}

// Counts is the partition. There is no fourth field for a remainder to hide in.
type Counts struct {
	Scored    int `json:"scored" yaml:"scored"`
	NoVerdict int `json:"no_verdict" yaml:"no_verdict"`
	Errored   int `json:"error" yaml:"error"`
}

// Total is what must equal the work list's length.
func (c Counts) Total() int { return c.Scored + c.NoVerdict + c.Errored }

// Tally counts the outcomes. Rows with an unknown outcome are counted nowhere, so a ledger
// carrying one fails Total() as well as Check — a bad value cannot pass by being ignored.
func Tally(rows []Row) Counts {
	var c Counts
	for _, r := range rows {
		switch r.Outcome {
		case Scored:
			c.Scored++
		case NoVerdict:
			c.NoVerdict++
		case Errored:
			c.Errored++
		}
	}
	return c
}

// Check verifies a ledger against the work list it claims to cover, returning every problem
// rather than the first: a run that lost four samples should say so once, not four times over
// four invocations.
//
// It answers two questions that are easy to conflate. Per row: is this entry meaningful.
// Across rows: does the set of entries cover exactly the work list, once each.
func Check(rows []Row, workList []string) []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	inWork := make(map[string]bool, len(workList))
	for _, s := range workList {
		inWork[s] = true
	}

	seen := make(map[string]int, len(rows))
	for _, r := range rows {
		if r.Sample == "" {
			add("a ledger row has no sample id, so it cannot be matched to the work list")
			continue
		}
		seen[r.Sample]++
		if seen[r.Sample] == 2 {
			// Two rows for one sample lets the totals add up while a sample is missing.
			add("sample %q appears twice in the ledger; one sample, one outcome", r.Sample)
		}
		if !inWork[r.Sample] {
			add("sample %q is not in the work list — the agreed test-point set is exactly "+
				"`corpus samples`, so a row from anywhere else is a point nobody approved", r.Sample)
		}
		errs = append(errs, checkRow(r)...)
	}

	// Sorted so the same broken run prints the same report twice.
	var missing []string
	for _, s := range workList {
		if seen[s] == 0 {
			missing = append(missing, s)
		}
	}
	sort.Strings(missing)
	for _, s := range missing {
		add("sample %q has no ledger row at all: a sample that produced nothing must say why, "+
			"not be absent", s)
	}
	return errs
}

// checkRow validates one entry's internal consistency.
func checkRow(r Row) []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	switch r.Outcome {
	case Scored:
		if !r.Attempted {
			add("sample %q is scored but not attempted; that combination has no meaning", r.Sample)
		}
		switch r.Verdict {
		case "malicious", "benign":
		case "":
			add("sample %q is scored with no verdict; `corpus score` reads exactly one of "+
				"\"malicious\" or \"benign\"", r.Sample)
		default:
			// The scorer refuses unknown words with a line number; naming the sample here is
			// more use than a line number in a 3,539-line file.
			add("sample %q has verdict %q, which `corpus score` cannot parse — a scanner's "+
				"middle tier has to be folded to one word, and which way is a stated choice",
				r.Sample, r.Verdict)
		}
		for _, f := range r.Flags {
			if !knownFlag(f) {
				add("sample %q carries %q, which is not a flag this ledger knows", r.Sample, f)
			}
		}
		if r.Reason != "" {
			add("sample %q is scored but carries reason %q; a reason explains the ABSENCE of a "+
				"verdict", r.Sample, r.Reason)
		}

	case NoVerdict:
		if !r.Attempted {
			// The whole point of no-verdict. Without this, it is indistinguishable from the
			// skip the design refuses to have.
			add("sample %q claims no-verdict but was never attempted; a reason may not be "+
				"written for a scanner that was not invoked on the sample", r.Sample)
		}
		switch {
		case r.Reason == "":
			add("sample %q has no verdict and no reason, which is the silent gap this ledger "+
				"replaces", r.Sample)
		case Explain(r.Reason) == "":
			add("sample %q gives reason %q, which is not a reason this ledger knows (the closed "+
				"set is %v)", r.Sample, r.Reason, KnownReasons())
		}
		if r.Verdict != "" {
			add("sample %q has outcome no-verdict but carries verdict %q", r.Sample, r.Verdict)
		}
		if len(r.Flags) > 0 {
			// own-fixture and surface-undeclared describe a measurement that happened.
			add("sample %q hangs %v on a row that produced nothing; those flags belong on "+
				"only a scored row, because neither of them is an inability to measure",
				r.Sample, r.Flags)
		}

	case Errored:
		if !r.Attempted {
			add("sample %q errored but was never attempted", r.Sample)
		}
		if r.Detail == "" {
			add("sample %q errored with no detail, leaving nobody able to reproduce it", r.Sample)
		}
		if r.Verdict != "" {
			add("sample %q errored but carries verdict %q; a failed run has no verdict",
				r.Sample, r.Verdict)
		}

	case "":
		add("sample %q has no outcome", r.Sample)

	default:
		add("sample %q has unknown outcome %q — the outcomes are %q, %q and %q, and there is "+
			"deliberately no \"skipped\": no row may say we did not run it",
			r.Sample, r.Outcome, Scored, NoVerdict, Errored)
	}
	return errs
}
