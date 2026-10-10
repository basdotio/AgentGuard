// SPDX-License-Identifier: MIT

package judgefold

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	aguardadapter "github.com/basdotio/AgentGuard/baselines/adapter/aguard"
	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// advisoryRule never counts toward a verdict. Binaries since the former repository's P-019
// (v0.12.0) never escalate it (internal/judge advisoryOnly); the glm run's binary predates that,
// escalated it 12 times, and its committed fold counted none of them — so the predicate excludes
// it by name and old raw folds the way it was folded.
const advisoryRule = "LLM-009"

var (
	agreedRe     = regexp.MustCompile(`\[(\d+) of (\d+) samples agreed\]`)
	severitiesRe = regexp.MustCompile(`\[severities: ([^\]]*)\]`)
)

// Options are the fold's two inputs that raw/ may not carry.
type Options struct {
	// Threshold is the verdict's severity bar, the same one the run's static fold used.
	Threshold model.Severity
	// Samples is how many times each question was asked, for a raw/ whose judge summary
	// predates `samples` (P-031). The summary wins; the two disagreeing is an error.
	Samples int
}

// Answer is one attempt's fold of one sample: its judge.jsonl row and what the table and the
// other outputs need beside it.
type Answer struct {
	Row     Row
	Ledger  ledger.Row      // the rebuilt (or carried-over) ledger row
	Verdict *corpus.Verdict // at the judge's predicate; nil unless the row is scored
	Source  string          // the corpus's source of the sample
	Dir     string          // the input directory the answer came from
	// Kinds is the kind of every artifact in report order; VoteArtifact[i] is the index in
	// Kinds of the artifact vote i sits on. judge.jsonl carries neither — the per-artifact
	// column of the table needs both.
	Kinds        []model.ArtifactKind
	VoteArtifact []int
}

// Fold folds one sample's scan result into its row, at the predicate the committed runs' `fold:`
// states: malicious when a deterministic finding is at or above the threshold, or an LLM finding
// that escalated (grounded and k of n) is at or above it and its rule is not LLM-009. lrow is the
// row the adapter rebuilt from the same bytes; the fold reads its outcome, route and judge_usage,
// never re-deriving them.
func Fold(s corpus.Sample, lrow ledger.Row, res model.ScanResult, opt Options) (Answer, error) {
	row := Row{Sample: s.Sample, Class: s.Class, EscalatedRules: []string{}, LLMNotes: Notes{}}
	a := Answer{Ledger: lrow}
	if lrow.Outcome != ledger.Scored {
		row.Votes, row.TriageCalls, row.Questions = &[]Vote{}, intp(0), intp(0)
		row.Incomplete = unanswered(lrow)
		a.Row = row
		return a, nil
	}

	n, err := samplesPerQuestion(res.Judge, opt.Samples)
	if err != nil {
		return Answer{}, fmt.Errorf("%s: %w", s.Sample, err)
	}
	f, err := foldFindings(res, opt.Threshold, n)
	if err != nil {
		return Answer{}, fmt.Errorf("%s: %w", s.Sample, err)
	}
	row.Static, row.Judge, row.JudgeAny = f.static, f.static || f.judge, f.static || f.any
	row.EscalatedRules = f.escalated
	row.Votes = &f.votes
	for _, nt := range res.Notes {
		row.LLMNotes = row.LLMNotes.Add(nt.RuleID)
	}

	triage := 0
	if lrow.JudgeUsage != nil {
		triage = lrow.JudgeUsage.TriageCalls
	}
	questions := 0
	if j := res.Judge; j != nil {
		row.JudgeCalls, row.JudgeFailed = j.Calls, j.Failed
		if n > 0 && j.Calls > triage {
			questions = (j.Calls - triage) / n
		}
	}
	row.TriageCalls, row.Questions = intp(triage), intp(questions)
	row.Incomplete = incompleteness(lrow, res.Judge)

	verdict := "benign"
	if row.Judge {
		verdict = "malicious"
	}
	a.Row = row
	a.Verdict = &corpus.Verdict{Sample: s.Sample, Verdict: verdict, Severity: string(f.severity), Dimensions: f.dimensions}
	a.Kinds, a.VoteArtifact = f.kinds, f.voteArtifact
	return a, nil
}

// folded is what one pass over the findings yields.
type folded struct {
	static, judge, any bool
	escalated          []string
	votes              []Vote
	severity           model.Severity
	dimensions         []string
	kinds              []model.ArtifactKind
	voteArtifact       []int
}

func foldFindings(res model.ScanResult, threshold model.Severity, n int) (folded, error) {
	out := folded{escalated: []string{}, votes: []Vote{}}
	escalated, dims := map[string]bool{}, map[string]bool{}
	raise := func(sev model.Severity) {
		if sev.Rank() > out.severity.Rank() {
			out.severity = sev
		}
	}
	flag := func(f model.Finding) {
		if d := aguardadapter.DimensionMap[model.DimensionName(f.Dimension)]; d != "" {
			dims[d] = true
		}
	}
	for i, art := range res.Artifacts {
		out.kinds = append(out.kinds, art.Kind)
		for _, f := range art.Findings {
			high := f.Severity.Rank() >= threshold.Rank()
			if score.Deterministic(f) {
				raise(f.Severity)
				if high {
					out.static = true
					flag(f)
				}
				continue
			}
			if f.Source != model.SrcLLM || f.Dimension == 0 {
				continue // a note, not a vote
			}
			v, err := voteOf(f, string(art.Kind), n)
			if err != nil {
				return folded{}, err
			}
			out.votes = append(out.votes, v)
			out.voteArtifact = append(out.voteArtifact, i)
			if f.Severity.Rank() >= model.SevMedium.Rank() {
				out.any = true
			}
			if !score.Escalating(f) { // an LLM finding escalates only when grounded and k of n agreed
				continue
			}
			if high {
				escalated[f.RuleID] = true
			}
			if f.RuleID == advisoryRule {
				continue
			}
			raise(f.Severity)
			if high {
				out.judge = true
				flag(f)
			}
		}
	}
	out.escalated = sortedKeys(escalated, out.escalated)
	out.dimensions = sortedKeys(dims, nil)
	return out, nil
}

// voteOf reads the vote count the judge's tally appended to the finding. It is appended only when
// samples > 1, so a finding without it is 1 of 1 exactly when n is 1; otherwise the count is
// unknown and the fold refuses rather than guess one.
func voteOf(f model.Finding, kind string, n int) (Vote, error) {
	v := Vote{Rule: f.RuleID, Kind: kind, Severity: string(f.Severity), Escalates: f.Escalates}
	m := agreedRe.FindStringSubmatch(f.Why)
	switch {
	case m != nil:
		v.K, _ = strconv.Atoi(m[1])
		v.N, _ = strconv.Atoi(m[2])
		if n > 0 && v.N != n {
			return Vote{}, fmt.Errorf("%s says %d samples were asked, the judge summary %d", f.RuleID, v.N, n)
		}
	case n == 1:
		v.K, v.N = 1, 1
	default:
		return Vote{}, fmt.Errorf("%s carries no \"[k of n samples agreed]\" and samples is %d, so its vote count is unknown", f.RuleID, n)
	}
	if s := severitiesRe.FindStringSubmatch(f.Why); s != nil {
		for _, x := range strings.Split(s[1], ",") {
			v.Severities = append(v.Severities, strings.TrimSpace(x))
		}
	}
	return v, nil
}

// samplesPerQuestion is n in "k of n": the summary's own count since P-031, else the operator's.
func samplesPerQuestion(j *model.JudgeSummary, flag int) (int, error) {
	if j == nil {
		return flag, nil
	}
	switch {
	case j.Samples > 0 && flag > 0 && j.Samples != flag:
		return 0, fmt.Errorf("the judge summary says samples %d, -judge-samples says %d", j.Samples, flag)
	case j.Samples > 0:
		return j.Samples, nil
	case flag == 0 && j.Calls > 0:
		return 0, fmt.Errorf("the judge summary predates `samples` and no -judge-samples was given, so questions cannot be counted")
	}
	return flag, nil
}

// incompleteness says why a scored row is not a complete judge answer, or "" when it is. A
// `check`-routed sample (secondary-surface) is complete without a judge, which never runs there.
func incompleteness(lrow ledger.Row, j *model.JudgeSummary) string {
	if j == nil {
		for _, f := range lrow.Flags {
			if f == ledger.SecondarySurface {
				return ""
			}
		}
		return "the judge did not run on this sample (raw/ carries no judge summary)"
	}
	if !j.Ran {
		return "the judge did not run: " + j.Reason
	}
	var why []string
	if j.Failed > 0 {
		why = append(why, fmt.Sprintf("the judge failed on %d call(s)", j.Failed))
	}
	if j.Skipped > 0 {
		why = append(why, fmt.Sprintf("the judge skipped %d planned call(s)", j.Skipped))
	}
	return strings.Join(why, "; ")
}

// unanswered is the reason for a row that has no scored answer. A no-verdict row is final — aguard
// read nothing it recognises, so there was nothing to judge either.
func unanswered(lrow ledger.Row) string {
	switch lrow.Outcome {
	case ledger.NoVerdict:
		return ""
	case ledger.Errored:
		return "no answer: the run errored (" + lrow.Detail + ")"
	}
	return "no answer: no raw/ output and no ledger row in any input directory"
}

func sortedKeys(m map[string]bool, empty []string) []string {
	if len(m) == 0 {
		return empty
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func intp(n int) *int { return &n }
