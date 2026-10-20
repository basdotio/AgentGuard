// SPDX-License-Identifier: MIT
package report

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/basdotio/bas-guard/internal/cmdline"
	"github.com/basdotio/bas-guard/internal/model"
	"github.com/basdotio/bas-guard/internal/redact"
	"github.com/basdotio/bas-guard/internal/score"
)

// This file is the second half of the plain layer (plain.go is the first): the sentences the default
// terminal report prints for the reader who does not write code (P-058) — a title, the clause on the
// score line, the judge's view of a finding, a next step, why the score is what it is, a short Judge
// line and the disclosure of what left the machine. The same rule as plain.go holds for every one of
// them: derived from the result, fixed phrases, no new judgement. Each sentence that embeds a string
// from disk or from the model is split into data and composition, so markdown can put that string in
// a code span without re-deriving the sentence.

// JudgeLineSuffix is the one extension point of the short Judge line: when set, its non-empty result is
// appended to that line after " · ". This build never sets it. The line is sanitized after the suffix
// is added, so whatever a suffix returns reaches the terminal under the same rule as everything else.
var JudgeLineSuffix func(*model.JudgeSummary) string

// target returns the artifact a result is about when it is about one target — `check <path>`: the
// artifact whose path is the result's root, and which is not a plugin's child. A root (scan, or a
// check of a directory laid out as one) has no artifact at its own path.
func target(r model.ScanResult) (model.ArtifactReport, bool) {
	for _, a := range r.Artifacts {
		if a.Plugin == "" && a.Path != "" && a.Path == r.Root {
			return a, true
		}
	}
	return model.ArtifactReport{}, false
}

// kindWord is how a sentence names one artifact of a kind: kindNoun's singular, else the kind itself.
func kindWord(k model.ArtifactKind) string {
	for _, n := range kindNoun {
		if n.kind == k {
			return n.one
		}
	}
	return string(k)
}

// title is the report's first line: what it is about, in words.
func title(r model.ScanResult) string {
	if a, ok := target(r); ok {
		return "AgentGuard · " + a.Name + " (" + kindWord(a.Kind) + ")"
	}
	return "AgentGuard · " + r.Root
}

// nothingToFix is the score line's clause when there is nothing to look at and nothing went unread.
const nothingToFix = "nothing to fix"

// scoreClause is what follows the score on its line, from the counts the list below prints.
func scoreClause(act, total int, unread bool) string {
	switch {
	case act > 0:
		return fmt.Sprintf("%d finding%s to look at", act, plural(act))
	case total > 0:
		return fmt.Sprintf("nothing at medium or above, %d informational finding%s", total, plural(total))
	case unread:
		return "no findings in what was read"
	}
	return nothingToFix
}

// coverageLead is coverageVerdict without its count — the score line already carries the count.
func coverageLead(level string, r model.ScanResult) string {
	if level == "Low" && unreadLoaded(r) {
		return incompleteLead
	}
	return levelLead(level)
}

// judgeViewLead opens the judge's view of a static finding. It says what the view is (advice) and what
// it is not: the finding is still listed at its severity and still counts (invariant #4). Nothing in
// the sentence may read as the judge clearing, lowering or removing anything.
const judgeViewLead = "Judge's view (advice; the finding and the score stand): "

// oneReading is the vote share a triage label rests on: triage is asked once, at temperature 0
// (judge.Triage), so there is exactly one reading and no count to print.
const oneReading = " (one reading)."

// triageWord is a triage label in plain words; a label this build does not know is printed as is.
func triageWord(label string) string {
	switch label {
	case model.LabelBenign:
		return "likely a false alarm"
	case model.LabelReal:
		return "likely real"
	}
	return label
}

// judgeView is the judge's view of a group as plain text, or "" when the judge gave none.
func judgeView(g Group) string {
	if g.triage.Label == "" {
		return ""
	}
	s := judgeViewLead + triageWord(g.triage.Label)
	if g.triage.Reason != "" {
		s += " — " + g.triage.Reason
	}
	return s + oneReading
}

// step is a finding's next step, as data: which file and line to start reading, or the entry quoted
// under it, then the dimension's fixed sentence. Composed per surface by stepText.
type step struct {
	File   string // the first evidence's file, "" when there is none
	Line   int    // > 0: read around this line
	Entry  bool   // whole-entry evidence with a snippet: the entry is quoted under the finding
	Hint   string // actionHint for the finding's dimension
	Remove bool   // REP-BAD: the content is on the known-malicious list
}

// nextOf derives g's next step. REP-BAD has its own sentence: a hash-exact match with a known-malicious
// entry has nothing left to read.
func nextOf(g Group) step {
	if g.RuleID == "REP-BAD" {
		return step{Remove: true}
	}
	s := step{Hint: actionHint(g.Dimension)}
	if len(g.Evidence) > 0 {
		e := g.Evidence[0]
		s.File, s.Line, s.Entry = e.File, e.Line, e.Line == 0 && e.Snippet != ""
	}
	return s
}

// stepText composes a step; file renders the file the way the surface renders a path.
func stepText(s step, file func(string) string) string {
	if s.Remove {
		return "Next: remove it — this exact content is on the known-malicious list."
	}
	var where string
	switch {
	case s.Line > 0:
		where = fmt.Sprintf("read %s around line %d.", file(s.File), s.Line)
	case s.Entry:
		where = "look at the entry quoted below."
	case s.File != "":
		where = "look at " + file(s.File) + "."
	}
	parts := make([]string, 0, 2)
	for _, p := range []string{where, s.Hint} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Next: " + strings.Join(parts, " ")
}

// approveLead introduces the approve command. "Keep it" and "tell the gate" are the two facts: the
// command records a decision for the load-time gate and changes no score.
const approveLead = "If you decide to keep it, tell the load-time gate: "

// approveCommand is the `aguard approve` command for g's artifact, or "" when the gate could not use one.
//
// Offered only for a static finding at medium or above (a low finding never stops a load, and a clean or
// low-only skill is remembered on its own, P-005) on an artifact that:
//   - is a skill — PreToolUse[Skill] is the only place the gate stops anything;
//   - has a content hash — approve refuses one without (P-011);
//   - carries no critical — a known-malicious match or a critical is not waved through with one line;
//   - is the target of a check, or sits inside the scanned root — a Claude Desktop skill outside it does
//     not load through the gate. A scan's command names its root, the store the gate reads for it.
//
// The path is quoted for a shell by cmdline.Arg, the gate's own quoting (P-030).
func approveCommand(r model.ScanResult, g Group) string {
	if g.FromJudge() || g.Severity.Rank() < model.SevMedium.Rank() || g.art < 0 || g.art >= len(r.Artifacts) {
		return ""
	}
	a := r.Artifacts[g.art]
	if a.Kind != model.KindSkill || a.Hash == "" || a.Path == "" {
		return ""
	}
	for _, f := range a.Findings {
		if score.Deterministic(f) && f.Severity == model.SevCritical {
			return ""
		}
	}
	if a.Path == r.Root && a.Plugin == "" {
		return "aguard approve " + cmdline.Arg(a.Path)
	}
	if !filepath.IsAbs(r.Root) || !within(r.Root, a.Path) {
		return ""
	}
	return "aguard approve --root " + cmdline.Arg(r.Root) + " " + cmdline.Arg(a.Path)
}

// within reports whether p lies strictly inside dir, lexically (both are the paths the scan recorded).
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}

// whyLine says, in one line, why the score is what it is: from score.Explain — the working Apply did —
// and the numbered groups printed above it (#k). "" at 100, and "" whenever the printed groups do not
// add up to the formula's number, so the line can be missing but never wrong.
func whyLine(r model.ScanResult, det []Group) string {
	b := score.Explain(r.Artifacts)
	if r.Overall == 100 || b.Overall != r.Overall || len(det) == 0 {
		return ""
	}
	first, n := -1, 0
	for i, g := range det {
		if g.Severity == b.Worst {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	capped := b.Mean > b.Ceiling && first >= 0
	switch {
	case b.Units == 1 && capped:
		return fmt.Sprintf("Why %d: a %s finding caps the score at %d (%s); by points alone it would be %d.",
			r.Overall, b.Worst, b.Ceiling, numbers(first, n), b.Mean)
	case b.Units == 1:
		return pointsLine(r.Overall, b.Mean, det)
	case capped:
		return fmt.Sprintf("Why %d: the average over %d items is %d, and a %s finding caps the score at %d (%s).",
			r.Overall, b.Units, b.Mean, b.Worst, b.Ceiling, numbers(first, n))
	}
	_, scores := score.UnitScores(r.Artifacts)
	perfect := 0
	for _, s := range scores {
		if s == 100 {
			perfect++
		}
	}
	return fmt.Sprintf("Why %d: the average of %d items' own scores (%d of them at 100).", r.Overall, b.Units, perfect)
}

// numbers names n consecutive printed groups starting at index first: "#1", "#1–#5".
func numbers(first, n int) string {
	if n <= 1 {
		return fmt.Sprintf("#%d", first+1)
	}
	return fmt.Sprintf("#%d–#%d", first+1, first+n)
}

// pointsLine spells out one unit's score: 100 minus the worst finding of each dimension. The worst group
// of a dimension is the first printed at its severity. "" when the terms do not reach the formula's mean.
func pointsLine(overall, mean int, det []Group) string {
	best := map[int]int{} // dimension → index of the group that sets its penalty
	for i, g := range det {
		if j, ok := best[g.Dimension]; !ok || score.Points(g.Severity) > score.Points(det[j].Severity) {
			best[g.Dimension] = i
		}
	}
	total, terms := 0, make([]string, 0, len(best))
	for i, g := range det {
		if best[g.Dimension] != i {
			continue
		}
		p := score.Points(g.Severity)
		total += p
		terms = append(terms, fmt.Sprintf("− %d (#%d, %s)", p, i+1, g.Severity))
	}
	if max(100-total, 0) != mean || overall != mean {
		return ""
	}
	s := fmt.Sprintf("Why %d: 100 %s; only the worst finding of each kind counts.", overall, strings.Join(terms, " "))
	if total > 100 {
		s += " The score stops at 0."
	}
	return s
}

// judgeShort is the default view's one line about the judge, "" when --llm was not passed. What it
// looked at and whether it added anything; failed and never-sent calls stay in it, because a judge that
// answered for part of the result must not read as one that answered for all of it. The model, samples
// and versions are --verbose's (judgeIdentityLine). JudgeLineSuffix may extend it.
func judgeShort(r model.ScanResult) string {
	j := r.Judge
	if j == nil {
		return ""
	}
	var s string
	if !j.Ran {
		s = "Judge: did not run"
		if j.Reason != "" {
			s += " — " + j.Reason
		}
	} else {
		what := fmt.Sprintf("%d artifact%s", j.Artifacts, plural(j.Artifacts))
		if a, ok := target(r); ok && j.Artifacts == 1 {
			what = "1 " + kindWord(a.Kind)
		}
		s = "Judge: checked " + what
		if j.Findings == 0 {
			s += ", nothing to add"
		} else {
			s += fmt.Sprintf(", added %d lead%s (below)", j.Findings, plural(j.Findings))
		}
		var short []string
		if j.Failed > 0 {
			short = append(short, fmt.Sprintf("%d of %d call(s) failed", j.Failed, j.Calls))
		}
		if j.Skipped > 0 {
			if len(short) == 0 {
				short = append(short, fmt.Sprintf("%d call(s) never sent", j.Skipped))
			} else {
				short = append(short, fmt.Sprintf("%d never sent", j.Skipped))
			}
		}
		if len(short) > 0 {
			s += "; " + strings.Join(short, " and ") + ", so its answer is incomplete"
		}
	}
	if JudgeLineSuffix != nil {
		if x := JudgeLineSuffix(j); x != "" {
			s += " · " + x
		}
	}
	return s
}

// isDisclosure is LLM-002: not a gap in what was read, but the statement that excerpts left the machine
// for a judge endpoint that is not local. It is printed as that (disclosureOf), never folded into the
// coverage line. Its place in JSON and SARIF does not change.
func isDisclosure(n model.Finding) bool { return n.RuleID == "LLM-002" }

// withoutDisclosure splits the disclosure notes from the coverage notes.
func withoutDisclosure(notes []model.Finding) (rest, disclosed []model.Finding) {
	for _, n := range notes {
		if isDisclosure(n) {
			disclosed = append(disclosed, n)
		} else {
			rest = append(rest, n)
		}
	}
	return rest, disclosed
}

// disclosure is the disclosure line's data: where the content went and how many calls carried it.
type disclosure struct {
	Host  string
	Calls int
	Sent  bool
}

// disclosureOf derives the disclosure from the judge block. Sent only when the judge ran and issued a
// call — a run stopped before the first call (endpoint refused, no key) sent nothing, and must not say
// it did.
func disclosureOf(r model.ScanResult) disclosure {
	d := disclosure{Host: "the configured judge endpoint"}
	if r.Judge == nil {
		return d
	}
	// judge.endpoint is shown with its credentials replaced (redact.Endpoint, P-076), and
	// `<REDACTED>@` is no userinfo url.Parse accepts: the host comes from the same split.
	if h := redact.EndpointHost(r.Judge.Endpoint); h != "" {
		d.Host = h
	}
	d.Calls, d.Sent = r.Judge.Calls, r.Judge.Ran && r.Judge.Calls > 0
	return d
}

// disclosureText composes the line; host renders the endpoint host the way the surface renders config text.
func disclosureText(d disclosure, host func(string) string) string {
	if d.Sent {
		return fmt.Sprintf("Sent to %s: redacted excerpts in %d call(s).", host(d.Host), d.Calls)
	}
	return fmt.Sprintf("Not sent to %s: the endpoint is off this machine, but no call was made.", host(d.Host))
}

// detailsLine is the default view's last line: what --verbose adds. Fixed words; the judge's versions
// are named only when the judge ran.
func detailsLine(judged bool) string {
	what := "where the scan looked, the inventory, and the full text of every note."
	if judged {
		what = "the judge's versions, " + what
	}
	return "More with --verbose: " + what
}
