// SPDX-License-Identifier: MIT

// Package sarif folds a SARIF 2.1.0 log into the one word the corpus scores.
//
// It belongs to no vendor. Two of the scanners baselines/ measures emit SARIF, and the moment a
// second copy of this logic exists the two columns stop being comparable: the parts below that
// are easy to get wrong are wrong in DIFFERENT ways in two implementations, and the difference
// shows up as a recall gap that belongs to the folds rather than to the tools. That is the whole
// reason this is a package and not a file inside one adapter.
//
// # What is and is not verified here
//
// This is written against the SARIF 2.1.0 specification, with the clause numbers named in the
// comments below so the next reader can check the claim rather than trust it. It is NOT written
// against any particular scanner's output: how a given tool populates these fields is that
// tool's adapter's problem, and the adapters record which parts they have seen real output for.
//
// # The two places SARIF is easy to get wrong
//
// Both silently change a measured recall rather than failing loudly, which is why each has a
// test naming the clause it implements.
//
//  1. A result with no `level` does NOT mean "none". SARIF §3.27.10 says it takes the rule's
//     defaultConfiguration.level, and failing that, `warning`. Reading a missing field as the
//     bottom of the ladder would discard every finding from a tool that relies on the default —
//     which is the ordinary way to write SARIF.
//  2. `level` only carries meaning when `kind` is `fail` (§3.27.9). A `pass` result with level
//     `error` is a check that passed, not a finding. Counting it would manufacture findings out
//     of a tool's own clean report.
//
// # Why the ladder stays SARIF's
//
// The verdict file carries SARIF's own level names rather than a translation into aguard's
// severities. taxonomy/tools.yaml is explicit that a label's expectation bounds are checked
// against the ladder of the tool whose block they sit in — "two tools with different ladders can
// annotate the same sample without either adopting the other's". Translating here would quietly
// impose our scale on somebody else's tool and make the columns look more comparable than they
// are.
package sarif

import (
	"slices"
	"strings"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// Level is a SARIF result level. Ordered by Rank, most severe first.
type Level string

const (
	LevelError   Level = "error"
	LevelWarning Level = "warning"
	LevelNote    Level = "note"
	LevelNone    Level = "none"
)

// Rank orders the ladder. 0 means "not a level this package knows", which keeps an unrecognised
// value at the bottom instead of letting a typo in a future SARIF version flag everything.
func (l Level) Rank() int {
	switch l {
	case LevelError:
		return 4
	case LevelWarning:
		return 3
	case LevelNote:
		return 2
	case LevelNone:
		return 1
	}
	return 0
}

// ParseLevel reads a level name case-insensitively, returning an unranked value for anything
// else. SARIF names are lower-case; the tolerance is for hand-written policy files.
func ParseLevel(s string) Level {
	switch Level(strings.ToLower(strings.TrimSpace(s))) {
	case LevelError:
		return LevelError
	case LevelWarning:
		return LevelWarning
	case LevelNote:
		return LevelNote
	case LevelNone:
		return LevelNone
	}
	return Level(s)
}

// Log is the subset of a SARIF 2.1.0 log this package reads. Deliberately partial: a scanner's
// SARIF carries far more than a verdict needs, and unmarshalling only what is used keeps the
// fold from depending on fields whose meaning we have not checked.
type Log struct {
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is one tool invocation's results.
type Run struct {
	Tool      Tool       `json:"tool"`
	Artifacts []Artifact `json:"artifacts"`
	Results   []Result   `json:"results"`
}

type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver names the scanner and carries the rule metadata a missing level falls back to.
type Driver struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

type Rule struct {
	ID                   string        `json:"id"`
	DefaultConfiguration Configuration `json:"defaultConfiguration"`
}

type Configuration struct {
	Level string `json:"level"`
}

// Artifact is a file the tool looked at. Only its presence matters here: an empty list next to
// an empty result list is how a run that read nothing looks.
type Artifact struct {
	Location Location `json:"location"`
}

type Location struct {
	URI string `json:"uri"`
}

// Result is one finding. Level and Kind are strings rather than typed enums because a SARIF
// document may legitimately carry a value from a later version, and the fold must degrade to
// "unranked" rather than fail to parse.
type Result struct {
	RuleID  string  `json:"ruleId"`
	Level   string  `json:"level"`
	Kind    string  `json:"kind"`
	Message Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
}

// isFinding reports whether a result is a failure at all. SARIF §3.27.9: `kind` defaults to
// `fail`, and `level` has meaning only for failures.
func (r Result) isFinding() bool {
	switch strings.ToLower(strings.TrimSpace(r.Kind)) {
	case "", "fail":
		return true
	}
	return false
}

// levelOf resolves a result's level through SARIF's fallback chain (§3.27.10):
// the result's own level, else the rule's defaultConfiguration.level, else `warning`.
func levelOf(r Result, rules map[string]Rule) Level {
	if r.Level != "" {
		return ParseLevel(r.Level)
	}
	if rule, ok := rules[r.RuleID]; ok && rule.DefaultConfiguration.Level != "" {
		return ParseLevel(rule.DefaultConfiguration.Level)
	}
	return LevelWarning
}

// ArtifactPolicy says what this tool's empty `artifacts` array means. SARIF does not require the
// array, so its emptiness is evidence for one tool and meaningless for another — and getting
// this backwards is not a small error in either direction.
type ArtifactPolicy int

const (
	// ArtifactsAreEvidence: the tool populates `artifacts` with what it read, so an empty
	// artifact list next to an empty result list means it read nothing.
	ArtifactsAreEvidence ArtifactPolicy = iota

	// ArtifactsNotEmitted: the tool never populates `artifacts` at all, so emptiness proves
	// nothing and every clean report would otherwise be misread as "read nothing".
	//
	// Measured on cc-audit v3.23.9 (2026-09-23), which is why this exists: its SARIF carries
	// `results` and rule metadata but NO artifacts array, even on a scan that found three
	// findings. Under ArtifactsAreEvidence every one of the corpus's 3,220 benign samples would
	// have folded to no-verdict and the false-positive rate would have come out undefined
	// rather than measured.
	//
	// The cost of this setting is real and has to be said plainly: for such a tool NOTHING in
	// its output distinguishes "scanned this tree and found nothing" from "never looked at this
	// tree". cc-audit reports `passed: true` with a 0/100 "safe" score for a literally empty
	// directory. The surface tripwire is the only backstop left — it is what catches
	// a whole load path scoring clean because the tool does not read it.
	ArtifactsNotEmitted
)

// readNothing reports whether the log shows a tool that never looked at anything. Findings are
// themselves proof it read something, so the artifact list is only consulted when there are no
// findings at all — and only when the tool emits one.
//
// This is the same trap the aguard adapter hit, in the shape SARIF gives it: a run that read
// nothing and a run that read the file and found nothing produce near-identical documents.
// Folding the first to "benign" scores a product boundary as a correct answer.
func readNothing(log Log, policy ArtifactPolicy) bool {
	if policy == ArtifactsNotEmitted {
		return false
	}
	for _, run := range log.Runs {
		if len(run.Results) > 0 || len(run.Artifacts) > 0 {
			return false
		}
	}
	return true
}

// rulesByID indexes every run's rule metadata so levelOf can resolve the §3.27.10 fallback.
// Returns nil when no run declared any rules, which levelOf reads as "fall through to warning".
func rulesByID(log Log) map[string]Rule {
	var rules map[string]Rule
	for _, run := range log.Runs {
		if len(run.Tool.Driver.Rules) == 0 {
			continue
		}
		if rules == nil {
			rules = make(map[string]Rule, len(run.Tool.Driver.Rules))
		}
		for _, rule := range run.Tool.Driver.Rules {
			rules[rule.ID] = rule
		}
	}
	return rules
}

// Fold turns a SARIF log into the one word the corpus scores. Severity is the highest level
// seen among findings — reported whether or not it flagged, so a second pass at another
// threshold can be read off the same file without rerunning the scanner.
//
// row is taken and returned by value: the caller's row is never modified.
func Fold(row ledger.Row, log Log, threshold Level, policy ArtifactPolicy) ledger.Row {
	row.Attempted = true

	if readNothing(log, policy) {
		row.Outcome = ledger.NoVerdict
		row.Reason = ledger.NoLoadPath
		row.Detail = "the SARIF run lists no artifacts and no results: the tool read nothing " +
			"here, which is not the same as finding nothing"
		return row
	}

	row.Outcome = ledger.Scored
	row.Verdict = "benign"

	rules := rulesByID(log)
	var top Level
	for _, run := range log.Runs {
		for _, res := range run.Results {
			if !res.isFinding() {
				continue
			}
			lvl := levelOf(res, rules)
			if lvl.Rank() > top.Rank() {
				top = lvl
			}
			// `none` never flags, whatever the threshold. SARIF §3.27.10 gives it the meaning
			// "the concept of severity does not apply to this result" — it is informational,
			// not a mild violation. A threshold of `none` is nonsensical rather than maximally
			// strict, and letting it flag would turn every informational line in a tool's
			// output into a detection.
			if lvl.Rank() > LevelNone.Rank() && lvl.Rank() >= threshold.Rank() {
				row.Verdict = "malicious"
				row.Rules = appendUnique(row.Rules, res.RuleID)
			}
		}
	}
	if top != "" {
		row.Severity = string(top)
	}
	return row
}

func appendUnique(xs []string, s string) []string {
	if s == "" || slices.Contains(xs, s) {
		return xs
	}
	return append(xs, s)
}

// IsRunFailure says whether an exit code means the run itself failed.
//
// Both SARIF-emitting scanners measured so far document the same contract — exit 1 when
// something was found, 0 when clean (cc-audit's README prints it literally: "Result: FAIL (exit
// code 1)"). So 1 is the gate firing, which is the EXPECTED outcome on a malicious sample, not
// a malfunction. Treating it as a failure turns every malicious sample into an Errored row and
// the measured recall into zero; that cost an afternoon on the aguard adapter and is
// written down here so it costs nobody a second one.
//
// An adapter whose tool does not follow this contract must not use this helper — it should say
// so in its own package and decide for itself.
func IsRunFailure(exitCode int) bool { return exitCode != 0 && exitCode != 1 }
