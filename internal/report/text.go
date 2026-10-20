// SPDX-License-Identifier: MIT
// Package report renders scan results. text.go is the terminal summary (spec §9):
// overview → score/level → findings by severity → scan notes.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/basdotio/bas-guard/internal/detect"
	"github.com/basdotio/bas-guard/internal/model"
	"github.com/basdotio/bas-guard/internal/score"
)

// Sanitize strips control characters (C0, DEL, C1) from any string before it is written to a
// PLAIN-TEXT surface. Findings carry attacker-influenced text — scanned file/artifact names,
// and (with --llm) model-authored summaries — so a raw ESC/OSC sequence could otherwise clear
// the screen, move the cursor, or spoof output on the operator's terminal. JSON output escapes
// controls and the HTML report is auto-escaped by html/template, so only the text surfaces
// need it.
func sanitize(s string) string { return Sanitize(s) }

// Sanitize is the one implementation, exported because this file stopped being the only
// terminal renderer of attacker-influenced strings — and a second copy of a stripping
// predicate is exactly the drift this codebase keeps refusing to allow. Its two other
// consumers arrived on different branches, for different reasons, and both are load-bearing:
//   - internal/gate renders hook messages that land in a terminal prompt AND in an agent's
//     context (invariant #7);
//   - internal/clean's interactive prompt prints skill names — directory names, chosen by
//     whoever shipped the artifact. A withdrawn version printed them raw, which let a crafted
//     name forge the selection cursor.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1 // drop; format strings supply their own newlines/spacing
		}
		// Bidi overrides and zero-width characters. A file literally named
		// `pay<U+202E>gnp.sh` rendered in the terminal as `pay2:hs.png` — a high EXEC-001 on a
		// shell script presenting itself as an image, the one extension a reviewer skips. The
		// classifier is detect's, the same one INJ-004 uses on file contents; nothing read the
		// file NAMES. Replaced with U+FFFD rather than dropped so "cleaned" and "never there"
		// stay distinguishable in the output.
		if detect.Invisible(r) {
			return '\uFFFD'
		}
		return r
	}, s)
}

// Text writes the DEFAULT terminal report: everything a reader has to act on, and nothing
// that only an auditor asks for. See writeText for what the two modes differ on.
func Text(w io.Writer, r model.ScanResult) { writeText(w, r, false) }

// TextVerbose writes the full report — every coverage note with its full rationale. This is
// the auditor's view, and the one to reach for when a scan's COVERAGE is the question
// ("what did it not read?") rather than its findings.
func TextVerbose(w io.Writer, r model.ScanResult) { writeText(w, r, true) }

// writeText renders the terminal report. `verbose` changes what is said about the scan, never which
// findings are listed:
//
//   - The coverage/suppression notes (dimension 0) render as one line instead of a block.
//     Their prose is long on purpose — a note has to explain what was NOT read and why —
//     and on a real machine that block is several times the size of the findings it sits
//     under, which is how a reader learns to skip the end of this report. Collapsed, it
//     still carries the COUNT and the HIGHEST SEVERITY, so invariant #5 ("no omission is
//     ever silent") holds in both modes: the default view can say less about a gap, never
//     nothing.
//   - The judge's model, samples and versions, the hygiene list and the scan details (where the
//     scan looked, the inventory, the OWASP statement) are --verbose's; the default view names what
//     --verbose adds in its last line (P-058).
//   - A result with nothing to look at — no finding, no note, no trust decision, nothing unread —
//     prints its title, its score and the judge's line, and stops (cleanShort).
//
// FINDINGS ARE NEVER COLLAPSED, in either mode. Hiding one behind a flag would make the
// default view of a security tool the one that under-reports, and a reader cannot ask for
// detail on a finding they were never shown. Only dimension-0 meta is folded.
//
// Reading order is the other design decision here, and it is for the reader who is NOT an
// auditor (P-058): what this is about → the score and how many things to look at → one plain
// sentence → each finding with the judge's view and a next step → why the score is what it is →
// what the judge did and what left the machine → what was trusted → what was not checked → what
// --verbose adds.
func writeText(w io.Writer, r model.ScanResult, verbose bool) {
	// One aggregation, shared with the HTML report (see Aggregate) so the two never drift.
	// Split by SOURCE, because the two halves answer different questions: the deterministic
	// findings are what your score and --fail-on are made of, while the judge's are leads to
	// check.
	var det, ai []Group
	for _, g := range Aggregate(r) {
		if g.FromJudge() {
			ai = append(ai, g)
		} else {
			det = append(det, g)
		}
	}
	notes, disclosed := withoutDisclosure(notesOf(r)) // the scan's notes and each artifact's own — see notesOf
	trust, coverage := splitNotes(notes)
	_, single := target(r)
	unread := unreadLoaded(r)
	hits := escalated(r)
	short := !verbose && len(det) == 0 && len(ai) == 0 && len(notes) == 0 && r.Inbox == nil && r.Sandbox == nil &&
		!unread && len(hits) == 0 && r.OverallEffective == r.Overall

	// WHAT, AND HOW BAD — every sentence derived from what follows.
	level := score.Level(r.Overall)
	clause := scoreClause(actionable(det), len(det), unread)
	mark := ""
	if clause == nothingToFix && len(coverage) == 0 {
		mark = "✅ "
	}
	fmt.Fprintln(w, Sanitize(title(r)))
	fmt.Fprintf(w, "%sRisk score %d/100 (%s) — %s\n", mark, r.Overall, level, clause)
	if !short {
		fmt.Fprintf(w, "  %s\n", coverageLead(level, r))
	}
	if wl := worstLine(r); wl != "" {
		fmt.Fprintf(w, "  %s\n", Sanitize(wl))
	}
	if r.Sandbox != nil {
		fmt.Fprint(w, sandboxBanner(r.Sandbox))
	}
	// Both numbers, or neither (spec §9). The ENVIRONMENT number is damped twice — averaged across
	// artifacts, then bucket-capped — so a judge finding routinely moves it by nothing while moving a
	// single artifact a lot; the per-artifact drops are named alongside it.
	if len(hits) > 0 || r.OverallEffective != r.Overall {
		fmt.Fprintf(w, "   ↳ with LLM advisories: %d/100 (%s) — not reproducible, does not gate\n",
			r.OverallEffective, score.Level(r.OverallEffective))
		if len(hits) > 0 {
			fmt.Fprintf(w, "     most affected: %s\n", strings.Join(hits, " · "))
		}
	}
	// What was checked: an environment always says it; a single target is named by the title, and
	// says it only when part of it was not fully checked.
	if gaps := append(itemGaps(r), scanGaps(r)...); verbose || !single || len(gaps) > 0 {
		fmt.Fprintf(w, "  %s\n", Sanitize(checkedSummary(r, plainGap)))
	}
	if len(trust) > 0 {
		fmt.Fprintf(w, "  Trusted by allowlist (findings suppressed, see below): %s\n", Sanitize(trustNames(trust)))
	}

	if len(det) == 0 && !short {
		fmt.Fprintln(w, "\n✅ No risk findings (static).")
	}
	if len(det) > 0 {
		fmt.Fprintf(w, "\nWhat to look at — %d finding%s; these set the score and --fail-on:\n", len(det), plural(len(det)))
		// Above three findings an index helps: the verdict line and the first three with a place to start.
		if len(det) > 3 {
			writeVerdict(w, det)
			for i, a := range actions(det, 3) {
				fmt.Fprintf(w, "  #%d %s — %s (%s)", i+1, Sanitize(a.Artifact), Sanitize(a.Title), a.Severity)
				if a.Where != "" {
					fmt.Fprintf(w, ", at %s", Sanitize(a.Where))
				}
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w)
		}
		offered := map[int]bool{} // artifacts already given an approve command
		for i, g := range det {
			writeGroup(w, r, g, fmt.Sprintf("#%d", i+1), verbose, offered)
		}
		if why := whyLine(r, det); why != "" {
			fmt.Fprintf(w, "\n%s\n", why)
		}
	}

	// THE JUDGE — what it did, and what left the machine. Then its own findings, which never set the score.
	sep := "\n"
	if short {
		sep = ""
	}
	if jl := judgeShort(r); jl != "" {
		fmt.Fprintf(w, "%s%s\n", sep, Sanitize(jl))
		sep = ""
	}
	if verbose {
		if jl := judgeSummaryLine(r); jl != "" {
			fmt.Fprintf(w, "  %s\n", Sanitize(jl))
		}
		if il := judgeIdentityLine(r.Judge); il != "" {
			fmt.Fprintf(w, "  %s\n", Sanitize(il))
		}
	}
	if len(disclosed) > 0 {
		fmt.Fprintf(w, "%s%s [%s]\n", sep, Sanitize(disclosureText(disclosureOf(r), func(s string) string { return s })), Sanitize(disclosed[0].RuleID))
		if verbose {
			for _, n := range disclosed {
				fmt.Fprintf(w, "  %s\n", Sanitize(n.Why))
			}
		}
	}
	if len(ai) > 0 {
		fmt.Fprintf(w, "\nFindings · LLM JUDGE (%d) — advisory leads; never set the score or --fail-on:\n", len(ai))
		for _, g := range ai {
			writeGroup(w, r, g, "•", verbose, nil)
		}
	}

	if r.Inbox != nil {
		writeInbox(w, *r.Inbox, verbose)
	}

	writeNotes(w, notes, verbose)

	if len(r.Hygiene) > 0 {
		if verbose {
			fmt.Fprintf(w, "\nJunk / hygiene (%d):\n", len(r.Hygiene))
			for _, h := range r.Hygiene {
				fmt.Fprintf(w, "  · %s: %s\n", Sanitize(h.Kind), Sanitize(h.Detail))
			}
		} else {
			fmt.Fprintf(w, "\nJunk / hygiene: %d item(s) — `bas-guard clean` lists them; --verbose prints them here.\n", len(r.Hygiene))
		}
	}

	switch {
	case verbose:
		writeScanDetails(w, r)
	case !short:
		fmt.Fprintf(w, "\n%s\n", detailsLine(r.Judge != nil && r.Judge.Ran))
	}
}

// writeScanDetails is the auditor's footer: the raw inventory, the auto-loaded surfaces and the
// OWASP coverage statement. Printed in both modes — it is the honesty statement the HTML report
// also carries — but last, because the numbers answer "what exactly did you look at" and that is
// the second question a reader has, not the first.
func writeScanDetails(w io.Writer, r model.ScanResult) {
	fmt.Fprintln(w, "\nScan details")
	// A single target is not a config root: the places a root scan derives from the root's parent
	// (the user MCP config, the desktop store) are never read for it, so they are not listed (P-058).
	if _, single := target(r); single {
		fmt.Fprintf(w, "  Where the scan looked: the target only — %s\n", Sanitize(r.Root))
	} else if len(r.Locations) > 0 {
		fmt.Fprintln(w, "  Where the scan looked:")
		for _, l := range r.Locations {
			fmt.Fprintf(w, "    %-22s %-7s %s\n", l.Name, l.Status, Sanitize(l.Path))
		}
	}
	// hooks counts hook COMMANDS, not events — one event can register several.
	fmt.Fprintf(w, "  Inventory: skills=%d mcp=%d hooks=%d permissions=%d subagents=%d commands=%d plugins=%d connectors=%d\n",
		r.Env.Skills, r.Env.MCPServers, r.Env.Hooks, r.Env.Permissions, r.Env.Subagents, r.Env.Commands, r.Env.Plugins, r.Env.Connectors)
	// Skills inside plugins ARE read — as part of the plugin's tree — and used to be invisible
	// on this line, which sent a reviewer to publish "check plugin/skills/* by hand or it is not
	// audited". Said explicitly; attribution and hashes are unchanged.
	if b := bundledParts(r.Env); b != "" {
		fmt.Fprintf(w, "  Inside plugins: %s, scanned with their plugin and as artifacts of their own (not counted above)\n", b)
	}
	// The auto-loaded surfaces get their own line, and only when present. They answer a different
	// question than the inventory above: these are files nobody chose to invoke — they enter context
	// every session (an output style enters the system prompt), so their count is a standing cost.
	//
	// workflows are deliberately NOT on this line. Measured on 2.1.229: their directory is
	// enumerated at startup but the bodies are only read on invocation, so counting them as
	// auto-loaded overstated the standing cost of an environment. They are inventory, not overhead.
	if n := r.Env.Rules + r.Env.OutputStyles + r.Env.Memories; n > 0 {
		fmt.Fprintf(w, "  Auto-loaded: rules=%d output-styles=%d memory=%d\n",
			r.Env.Rules, r.Env.OutputStyles, r.Env.Memories)
	}
	if r.Env.Workflows > 0 {
		fmt.Fprintf(w, "  On-demand: workflows=%d (bodies load when invoked, not at startup)\n", r.Env.Workflows)
	}
	// The OWASP catalogue is printed with its GAPS, not only its hits. A reader who sees six category
	// names has no way to know whether the other four were clean or unexamined — and for three of them
	// the honest answer is "this tool does not look", because they describe runtime behaviour a static
	// scanner cannot witness. Same discipline as COV-000: a gap that names itself is a decision the
	// operator gets to make.
	if cats := detect.ASICatalogue(); len(cats) > 0 {
		var silent []string
		for _, c := range cats {
			if !c.Covered {
				silent = append(silent, c.ID)
			}
		}
		if len(silent) > 0 {
			fmt.Fprintf(w, "  OWASP Agentic (2026): %d/%d categories have rules; silent on %s (runtime behaviour a static scan cannot witness)\n",
				len(cats)-len(silent), len(cats), strings.Join(silent, ", "))
		}
	}
	fmt.Fprintln(w, "  Static analysis only: it cannot prove malice or see runtime behaviour. Rule ids are in brackets; --json and --sarif carry every path in full.")
}

// writeVerdict answers, in one line, the question a reader opens this report with: is there
// anything here I have to do something about?
//
// It is derived ENTIRELY from the findings already printed below it — a count, a severity and
// the worst rule's own id. Nothing here is a new judgement, and nothing is a new severity: a
// verdict line that could disagree with the list under it would be worse than no verdict line,
// because it is the half people read.
//
// The medium threshold is the same line the report already draws elsewhere: low is the level
// this tool uses for "worth knowing", and a reader who is told to act on everything acts on
// nothing.
func writeVerdict(w io.Writer, det []Group) {
	if len(det) == 0 {
		return // the ✅ line below already says it; two ways of saying "nothing" is one too many
	}
	act, worst := actionableWorst(det)
	if act == 0 {
		fmt.Fprintf(w, "→ Nothing at medium or above. %d informational finding(s) below.\n", len(det))
		return
	}
	fmt.Fprintf(w, "→ %d of %d finding(s) are medium or above — start with [%s] %s (%s).\n",
		act, len(det), Sanitize(worst.RuleID), Sanitize(friendlyArtifact(worst.Artifact)), worst.Severity)
}

// writeNotes renders the dimension-0 coverage/suppression notes.
//
// Collapsed (the default), the line still carries the COUNT and the HIGHEST SEVERITY among
// the notes. That is the part invariant #5 actually requires: a suppression note that hid a
// critical must not read as a low-severity footnote, and it does not here — the severity
// travels with the count. What the default view drops is the rationale prose, which is the
// part that is long, static, and identical run after run.
func writeNotes(w io.Writer, notes []model.Finding, verbose bool) {
	if len(notes) == 0 {
		return
	}
	if !verbose {
		// Two kinds of dimension-0 note, two different sentences. A COVERAGE note says "I did
		// not read X" — long, static, identical run after run, and folding it is a kindness. A
		// SUPPRESSION note says "I read 18 highs and removed them from your score because
		// someone decided to trust this" — that is a decision about the number the reader is
		// looking at, and it was being folded into the same line and called "coverage is
		// incomplete", which is the wrong sentence. Suppressions print in full, one each, with
		// the first lines of the recorded review; only the coverage notes fold.
		var suppressed, coverage []model.Finding
		for _, n := range notes {
			if isSuppression(n.RuleID) {
				suppressed = append(suppressed, n)
			} else {
				coverage = append(coverage, n)
			}
		}
		if len(suppressed) > 0 {
			fmt.Fprintf(w, "\nTrusted by allowlist — %d trust decision(s) changed the score; findings were suppressed, not absent:\n", len(suppressed))
			for _, n := range suppressed {
				file := ""
				if len(n.Evidence) == 1 && n.Evidence[0].File != "" {
					file = " " + sanitize(n.Evidence[0].File)
				}
				fmt.Fprintf(w, "  · %s [%s] %s%s — highest %s\n", sevIcon(n.Severity), sanitize(n.RuleID), sanitize(n.Title), file, n.Severity)
				if n.Why != "" {
					fmt.Fprintf(w, "      %s\n", sanitize(clipSentences(n.Why, suppressionWhyBudget)))
				}
			}
		}
		if len(coverage) > 0 {
			worst := model.SevLow
			for _, n := range coverage {
				if n.Severity.Rank() > worst.Rank() {
					worst = n.Severity
				}
			}
			ids := make([]string, 0, len(coverage))
			seen := map[string]bool{}
			for _, n := range coverage {
				if !seen[n.RuleID] {
					seen[n.RuleID] = true
					ids = append(ids, Sanitize(n.RuleID))
				}
			}
			fmt.Fprintf(w, "\nNot checked — %d coverage note(s), highest %s [%s] — coverage is incomplete; --verbose to read them\n",
				len(coverage), worst, strings.Join(ids, " "))
		}
		return
	}
	fmt.Fprintf(w, "\n⚠ Scan warnings (%d, coverage incomplete):\n", len(notes))
	for _, n := range notes {
		// The title line names the ONE thing a note is about. A coalesced note is about many,
		// and they are listed below it (writeNoteEvidence) — naming only the first would assert
		// that the note belongs to that hook when it covers a dozen.
		file := ""
		if len(n.Evidence) == 1 {
			file = n.Evidence[0].File
		}
		if file != "" {
			file = " " + sanitize(file)
		}
		// Show the severity icon + Why so a suppression note (IGN-000/REP-GOOD) surfaces
		// the highest suppressed severity in plain output — §12: never silent.
		fmt.Fprintf(w, "  · %s [%s] %s%s\n", sevIcon(n.Severity), sanitize(n.RuleID), sanitize(n.Title), file)
		if n.Why != "" {
			fmt.Fprintf(w, "      %s\n", sanitize(n.Why))
		}
		writeNoteEvidence(w, n)
	}
}

// isSuppression says whether a dimension-0 note records findings REMOVED from the score —
// by the embedded allowlist (REP-GOOD) or an operator baseline (IGN-000) — as opposed to
// content the scan did not reach. The two are rendered differently by default: see writeNotes.
func isSuppression(ruleID string) bool { return ruleID == "REP-GOOD" || ruleID == "IGN-000" }

// suppressionWhyBudget is how much of a suppression note's rationale the default view prints:
// enough for the entry, the count, the reviewer date and the first sentence or two of the
// review, which is what answers "why was this trusted?". --verbose prints all of it.
const suppressionWhyBudget = 320

// clipSentences shortens s to at most max runes, preferring to cut after a sentence end so a
// review is not sliced mid-clause. Never cuts inside a rune; appends an ellipsis when it cut.
func clipSentences(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	cut := string(rs[:max])
	if i := strings.LastIndex(cut, ". "); i > max/2 {
		return cut[:i+1] + " …"
	}
	return cut + "…"
}

// writeNoteEvidence lists what a coalesced coverage note actually covers.
//
// A note used to render as its title plus the FIRST evidence file, which was fine while notes
// were one-per-thing and fatal once they were merged: a real scan produced 53 hook-script notes
// where one hook command names several scripts, so "PostToolUse[*]#1" appeared twice, verbatim,
// and the field that distinguished them — the script reference, in Snippet — was never printed.
// Two identical lines read as a bug in the tool, and the operator stops trusting the section.
//
// Same three-line budget and same remainder line as a finding group (see writeGroup), for the
// same reason: naming the remainder is what keeps the shown three from reading as the whole set.
func writeNoteEvidence(w io.Writer, n model.Finding) {
	if len(n.Evidence) == 0 {
		return
	}
	// A lone instance whose File says everything ("File too large … <path>") is already fully
	// named on the title line, and a second line repeating it is noise. But a lone instance with
	// a SNIPPET is not: the snippet is the answer. The attribution note resolves one reference to
	// one path, and skipping it here dropped that path from the report entirely — the same
	// field-loss as the redaction bug, one layer up.
	if len(n.Evidence) == 1 {
		if e := n.Evidence[0]; e.Snippet != "" {
			fmt.Fprintf(w, "      ← %s\n", sanitize(e.Snippet))
		}
		return
	}
	const budget = 3
	for i, e := range n.Evidence {
		if i == budget {
			fmt.Fprintf(w, "      ← … and %d more — --json or --sarif lists them all\n", len(n.Evidence)-budget)
			return
		}
		if e.Snippet != "" {
			fmt.Fprintf(w, "      ← %s — %s\n", sanitize(e.File), sanitize(e.Snippet))
			continue
		}
		fmt.Fprintf(w, "      ← %s\n", sanitize(e.File))
	}
}

// writeGroup renders one aggregated row as a block a reader can act on (P-058): severity and what the
// finding is about in plain words; who · what with the rule id closing the line, where an auditor finds
// it and a first-time reader can skip it; the rule's own Why; the judge's view when it gave one; the next
// step; the evidence. Evidence paths are shortened in the default view (plugin › file) and printed in
// full with --verbose. offered tracks the artifacts already given an approve command — one per artifact,
// under its first finding that warrants it; nil for the judge's findings, which never get one.
func writeGroup(w io.Writer, r model.ScanResult, g Group, mark string, verbose bool, offered map[int]bool) {
	adv := ""
	if g.Advisory {
		adv = " [advisory: not confirmed]"
	}
	times := ""
	if g.Count > 1 {
		times = fmt.Sprintf(" ×%d", g.Count)
		// Depth AND breadth. Suppressed when every hit is in one file, because " in 1 files" would
		// add a word without adding an answer.
		if g.Files > 1 {
			times += fmt.Sprintf(" in %d files", g.Files)
		}
	}
	what := dimLabel(g.Dimension)
	if what == "" {
		what = g.Title
	}
	fmt.Fprintf(w, "  %s %s %s · %s\n", mark, sevIcon(g.Severity), capitalize(string(g.Severity)), Sanitize(what))
	fmt.Fprintf(w, "     %s · %s%s%s  [%s]\n", Sanitize(friendlyArtifact(g.Artifact)), Sanitize(g.Title), times, adv, Sanitize(g.RuleID))
	fmt.Fprintf(w, "     %s\n", Sanitize(g.Why))
	if v := judgeView(g); v != "" {
		fmt.Fprintf(w, "     %s\n", Sanitize(v))
	}
	path := func(f string) string {
		if verbose {
			return Sanitize(f)
		}
		return Sanitize(shortPath(f))
	}
	if next := stepText(nextOf(g), path); next != "" {
		fmt.Fprintf(w, "     %s\n", Sanitize(next))
	}
	if offered != nil && !offered[g.art] {
		if cmd := approveCommand(r, g); cmd != "" {
			offered[g.art] = true
			fmt.Fprintf(w, "     %s%s\n", approveLead, cmd)
		}
	}
	for _, e := range g.Evidence {
		// Line 0 = whole-artifact evidence (a permission entry, a hook command): the file name
		// alone identifies nothing, so show the snippet — already redacted and clipped at detect
		// time — or two grants in one file read identically.
		if e.Line == 0 && e.Snippet != "" {
			fmt.Fprintf(w, "     ← %s — %s\n", path(e.File), Sanitize(e.Snippet))
			continue
		}
		fmt.Fprintf(w, "     ← %s:%d\n", path(e.File), e.Line)
	}
	// Naming the remainder matters more than listing it (see Group.MoreFiles).
	if rest := g.MoreFiles(); rest > 0 {
		fmt.Fprintf(w, "     ← … and %d more file(s) — --json or --sarif lists them all\n", rest)
	}
}

// capitalize upper-cases the first letter of an ASCII word (a severity).
func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// escalated names the artifacts whose score the judge actually moved, worst drop first,
// capped at three so the header stays a header. Empty when the judge added nothing.
func escalated(r model.ScanResult) []string {
	hits, more := escalations(r)
	out := make([]string, 0, 4)
	for _, h := range hits {
		out = append(out, fmt.Sprintf("%s %d→%d", Sanitize(h.Label), h.From, h.To))
	}
	if more > 0 {
		out = append(out, fmt.Sprintf("+%d more", more))
	}
	return out
}

// Hygiene renders the cleanup report (spec §6): addressable items + a quantified "reclaimable
// context" total — the "cleanup" framing.
//
// The counts are stated separately on purpose. "Actionable" used to mean len(items), which counted
// stale-ref reports — findings with no fix — as things the operator could act on. An item now has
// to name an executable action to be counted. The subset an UNATTENDED run could take is called
// out separately when there is one — today there never is, so that half stays silent rather than
// printing a permanent zero.
func Hygiene(w io.Writer, hs []model.CleanItem) {
	if len(hs) == 0 {
		fmt.Fprintln(w, "✅ No junk / hygiene issues found.")
		return
	}
	reclaim, actionable, unattended, blocked := 0, 0, 0, 0
	for _, h := range hs {
		reclaim += h.ReclaimTokens
		switch {
		case h.UnattendedSafe():
			unattended++
			actionable++
		case h.Executable():
			actionable++
		case h.Actionable:
			blocked++
		}
	}
	fmt.Fprintf(w, "Cleanup: %d item(s), %d executable", len(hs), actionable)
	if reclaim > 0 {
		fmt.Fprintf(w, ", ~%d tokens of context reclaimable", reclaim)
	}
	fmt.Fprintln(w)
	// The unattended count is printed ONLY when it is non-zero, and that is a correction rather
	// than a shortcut. model.UnattendedSafe is unreachable today (its two conditions hold on
	// disjoint sets — see the predicate), so this line used to render as a permanent
	// "0 item(s) qualify for an unattended batch", which is worse than saying nothing twice over:
	// the zero looked like a measurement that could change, and it implied an unattended command
	// the operator could go and run. No such command exists.
	//
	// The §12 reflex is that a zero must stay visible so "none qualified" cannot read as "not
	// checked". It does not apply here: the ambiguity §12 guards against is about the ENVIRONMENT,
	// and this zero is a property of the TOOL. A constant belongs in the docs and in a test
	// (hygiene.TestConfidenceCeiling_KeepsUnattendedUnreachable), not in every run's output.
	needsDecision := actionable - unattended + blocked
	switch {
	case unattended > 0:
		fmt.Fprintf(w, "  %d item(s) qualify for an unattended batch; %d need a decision from you.\n",
			unattended, needsDecision)
	case needsDecision > 0:
		fmt.Fprintf(w, "  %d item(s) need a decision from you.\n", needsDecision)
	}
	for _, h := range hs {
		id := h.ID
		if id == "" {
			id = "—"
		}
		tier := string(h.Tier)
		if tier == "" {
			tier = "info"
		}
		fmt.Fprintf(w, "  · %s [%s/%s] %s\n", sanitize(id), sanitize(h.Kind), sanitize(tier),
			sanitize(strings.Join(h.Targets, ", ")))
		fmt.Fprintf(w, "      %s\n", sanitize(h.Detail))
		// The listing is where a copyable command belongs: the operator is reading, not answering.
		// The interactive prompt deliberately does not print it — see model.CleanItem.Hint.
		if h.Hint != "" {
			fmt.Fprintf(w, "      %s\n", sanitize(h.Hint))
		}
		if len(h.Blockers) > 0 {
			fmt.Fprintf(w, "      blocked: %s\n", sanitize(strings.Join(h.Blockers, ", ")))
		}
	}
}

func sevIcon(s model.Severity) string {
	switch s {
	case model.SevCritical, model.SevHigh:
		return "🔴"
	case model.SevMedium:
		return "🟠"
	default:
		return "🟡"
	}
}

// HasAtLeast reports whether any DETERMINISTIC finding meets/exceeds sev — drives the
// --fail-on exit code (spec §3). No LLM output can influence it, whatever the effective score
// says.
func HasAtLeast(r model.ScanResult, sev model.Severity) bool {
	return hasAtLeast(r, sev, score.Deterministic)
}

// HasAtLeastEffective is the same question over the findings that feed the EFFECTIVE score —
// deterministic ones plus LLM findings that cleared every precondition. It drives the separate,
// default-off --fail-on-llm gate (spec §3), never --fail-on.
func HasAtLeastEffective(r model.ScanResult, sev model.Severity) bool {
	return hasAtLeast(r, sev, score.Escalating)
}

// hasAtLeast walks the findings with the SAME predicate the scorer uses. The predicates live
// in one place (score) precisely so a gate and its score can never disagree about what counts.
func hasAtLeast(r model.ScanResult, sev model.Severity, counts func(model.Finding) bool) bool {
	for _, a := range r.Artifacts {
		for _, f := range a.Findings {
			if counts(f) && f.Severity.Rank() >= sev.Rank() {
				return true
			}
		}
	}
	return false
}

// writeInbox prints the Downloads section: each agent-shaped item found where downloads land,
// checked on its own, with its own score. It is not part of the environment score and the
// header says so, because the same reader who trusts the big number must not read these into it.
func writeInbox(w io.Writer, ib model.InboxReport, verbose bool) {
	fmt.Fprintf(w, "\nDownloads · %d agent-shaped item(s) under %s — not installed, checked on their own, not part of the score:\n",
		len(ib.Items), Sanitize(ib.Dir))
	if len(ib.Items) == 0 {
		fmt.Fprintf(w, "  Nothing agent-shaped among %d entries; nothing else was read.\n", ib.Skipped)
	}
	if ib.Judge != nil && len(ib.Items) > 0 {
		fmt.Fprintf(w, "  %s\n", Sanitize(judgeLine(ib.Judge)))
	}
	for _, it := range ib.Items {
		kind := it.Kind
		if it.Archive {
			kind = "zip"
		}
		if it.Error != "" {
			fmt.Fprintf(w, "  ⚪ ---/100 %-8s %s (%s) — not checked: %s\n", "", Sanitize(it.Name), kind, Sanitize(it.Error))
			continue
		}
		level := score.Level(it.Overall)
		fmt.Fprintf(w, "  %s %3d/100 %-8s %s (%s)\n", levelIcon(level), it.Overall, level, Sanitize(it.Name), kind)
		if worst := inboxWorst(it); worst != "" {
			fmt.Fprintf(w, "      worst: %s · %d finding(s)\n", Sanitize(worst), len(it.Findings))
		}
		fmt.Fprintf(w, "      → %s\n", inboxAdvice(level, len(it.Findings), it.Judged))
		// The item's own coverage notes are not folded behind --verbose: "nothing flagged" next
		// to "part of it was not read" is the whole story, and half of it is the half that matters.
		for _, n := range it.Notes {
			fmt.Fprintf(w, "      note: %s\n", Sanitize(n.Title))
		}
	}
	if ib.Skipped > 0 && len(ib.Items) > 0 {
		fmt.Fprintf(w, "  (%d other entries were not agent-shaped and were not read)\n", ib.Skipped)
	}
	for _, n := range ib.Notes {
		fmt.Fprintf(w, "  · %s — %s\n", Sanitize(n.Title), Sanitize(n.Why))
	}
}

// levelIcon mirrors sevIcon for a score level.
func levelIcon(level string) string {
	switch level {
	case "Low":
		return "🟢"
	case "Watch":
		return "🟡"
	case "Elevated":
		return "🟠"
	}
	return "🔴"
}

// sandboxBanner is the same text in the terminal and (via plain.go) the HTML: this scan ran in
// a throwaway cloud container, so the score is about that container, not the reader's computer.
// The signals are printed so the claim is checkable, and the fix — run on the real machine —
// is stated, because that is the whole point of saying it at all.
func sandboxBanner(sb *model.SandboxInfo) string {
	var b strings.Builder
	b.WriteString("\n  ⚠ This scan ran in a temporary cloud environment (Claude Cloud / Cowork), not on your\n")
	b.WriteString("    own computer. The score above is about this throwaway sandbox — your real skills,\n")
	b.WriteString("    plugins, hooks and connectors live on your machine and were NOT scanned from here.\n")
	b.WriteString("    To check your computer, open the Code tab (</>) in the desktop app and run the scan there.\n")
	if len(sb.Signals) > 0 {
		b.WriteString("    Why we can tell: " + Sanitize(strings.Join(sb.Signals, "; ")) + ".\n")
	}
	return b.String()
}
