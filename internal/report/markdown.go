// SPDX-License-Identifier: MIT
package report

import (
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/detect"
	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/score"
)

// Markdown renders the scan as GitHub-flavoured markdown — the third human-read renderer, for
// the reader who wants the report inside a pull-request comment or an issue rather than in a
// terminal or a saved HTML file. It is the SAME report: built from sanitizeResult, Aggregate and
// the plain-language layer (plain.go) that the terminal and HTML renderers share, in the same
// reading order, with nothing judged here that is not judged there.
//
// One rule is specific to this surface and it is the reason the renderer exists as code rather
// than as a template: EVERY string that originated on disk — artifact names, paths, snippets,
// finding titles and rationales (static rules format file names into theirs, and the judge's
// are model-authored) — is emitted only inside a code span. In a PR comment a bare `@name`
// pings a person, `[x](url)` is a link, `<img>` is a tracking pixel and `|` splits a table
// cell; a skill's file name is chosen by whoever shipped the skill. Inside a code span GitHub
// renders none of that. The sentences the report itself composes (verdict, labels, hints,
// headings) are the only bare text.
//
// There is no --verbose here. A PR reader cannot re-run with a flag, so evidence carries full
// paths, and the coverage notes fold into <details> whose summary line still states the count
// and the highest severity (invariant #8). Findings never fold.
func Markdown(w io.Writer, r0 model.ScanResult) error {
	r := sanitizeResult(r0)
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("# AgentGuard scan · %s\n\n", code(r.Root))
	p("**Risk score %d/100 (%s)** — deterministic and reproducible; this is the number `--fail-on` reads.\n", r.Overall, score.Level(r.Overall))
	hits, more := escalations(r)
	if len(hits) > 0 || r.OverallEffective != r.Overall {
		p("\n↳ **with LLM advisories: %d/100 (%s)** — not reproducible, does not gate.", r.OverallEffective, score.Level(r.OverallEffective))
		if len(hits) > 0 {
			parts := make([]string, 0, len(hits)+1)
			for _, h := range hits {
				parts = append(parts, fmt.Sprintf("%s %d→%d", code(h.Label), h.From, h.To))
			}
			if more > 0 {
				parts = append(parts, fmt.Sprintf("+%d more", more))
			}
			p(" Most affected: %s.", strings.Join(parts, " · "))
		}
		p("\n")
	}
	if wi, perfect, ok := worstItem(r); ok {
		p("\nWorst single item: **%d/100** — %s. The score above is an average over %d items (%d of them at 100), so it hides this one.\n",
			wi.Score, code(friendlyArtifact(string(wi.Kind)+":"+wi.Name)), len(r.Artifacts), perfect)
	}
	if r.Sandbox != nil {
		p("\n> ⚠ **This scan ran in a temporary cloud environment** (Claude Cloud / Cowork), not on your own computer. The score above is about this throwaway sandbox — your real skills, plugins, hooks and connectors live on your machine and were NOT scanned from here. To check your computer, open the Code tab (</>) in the desktop app and run the scan there.\n")
		if len(r.Sandbox.Signals) > 0 {
			sig := make([]string, 0, len(r.Sandbox.Signals))
			for _, s := range r.Sandbox.Signals {
				sig = append(sig, code(s))
			}
			p(">\n> Why we can tell: %s.\n", strings.Join(sig, "; "))
		}
	}

	var det, ai []Group
	for _, g := range Aggregate(r) {
		if g.FromJudge() {
			ai = append(ai, g)
		} else {
			det = append(det, g)
		}
	}
	trust, coverage := splitNotes(r.Notes)

	// SUMMARY — derived sentences only.
	p("\n## Summary\n\n%s  \n%s  \n", verdictSentence(score.Level(r.Overall), actionable(det), len(det)), checkedLine(r.Env))
	if jl := judgeSummaryLine(r); jl != "" {
		p("%s  \n", text(jl))
	}
	if len(trust) > 0 {
		names := make([]string, 0, len(trust))
		for _, n := range trust {
			names = append(names, code(trustName(n)))
		}
		p("Trusted by allowlist (findings suppressed, see below): %s  \n", strings.Join(names, ", "))
	}

	// WHAT TO LOOK AT — the verdict line and the worst items, each with a place to start.
	p("\n## What to look at\n\n")
	if len(det) == 0 {
		p("✅ No risk findings (static).\n")
	} else if act, worst := actionableWorst(det); act == 0 {
		p("→ Nothing at medium or above. %d informational finding(s) below.\n", len(det))
	} else {
		p("→ %d of %d finding(s) are medium or above — start with %s %s (%s).\n\n",
			act, len(det), code(worst.RuleID), code(friendlyArtifact(worst.Artifact)), worst.Severity)
		for i, a := range actions(det, 3) {
			p("%d. **%s** — %s · %s", i+1, a.Severity, code(a.Artifact), code(a.Title))
			if a.Where != "" {
				p(" — where: %s", code(a.Where))
			}
			p("\n")
		}
	}

	if len(det) > 0 {
		p("\n## Findings · static (%d)\n\nDeterministic; these set the score and `--fail-on`.\n\n", len(det))
		mdGroups(&b, det)
	}
	if len(ai) > 0 {
		p("\n## Findings · LLM judge (%d)\n\nAdvisory leads; they never set the score or `--fail-on`.\n\n", len(ai))
		mdGroups(&b, ai)
	}

	if r.Inbox != nil {
		mdInbox(&b, *r.Inbox)
	}

	if len(r.Hygiene) > 0 {
		p("\n## Junk / hygiene (%d)\n\n", len(r.Hygiene))
		for _, h := range r.Hygiene {
			p("- %s: %s\n", code(h.Kind), code(h.Detail))
		}
	}

	if len(trust) > 0 {
		p("\n## Trusted by allowlist\n\n%d trust decision(s) changed the score; findings were suppressed, not absent.\n\n", len(trust))
		for _, n := range trust {
			file := ""
			if len(n.Evidence) == 1 && n.Evidence[0].File != "" {
				file = " " + code(n.Evidence[0].File)
			}
			p("- %s %s %s%s — highest %s", sevIcon(n.Severity), code(n.RuleID), code(n.Title), file, n.Severity)
			if n.Why != "" {
				p("  \n  %s", code(n.Why))
			}
			p("\n")
		}
	}

	if len(coverage) > 0 {
		worst := model.SevLow
		ids := make([]string, 0, len(coverage))
		seen := map[string]bool{}
		for _, n := range coverage {
			if n.Severity.Rank() > worst.Rank() {
				worst = n.Severity
			}
			if !seen[n.RuleID] {
				seen[n.RuleID] = true
				ids = append(ids, "<code>"+html.EscapeString(n.RuleID)+"</code>")
			}
		}
		p("\n## Not checked\n\n<details>\n<summary>%d coverage note(s), highest %s — %s; coverage is incomplete</summary>\n\n",
			len(coverage), worst, strings.Join(ids, " "))
		for _, n := range coverage {
			file := ""
			if len(n.Evidence) == 1 && n.Evidence[0].File != "" {
				file = " " + code(n.Evidence[0].File)
			}
			p("- %s %s %s%s", sevIcon(n.Severity), code(n.RuleID), code(n.Title), file)
			if n.Why != "" {
				p("  \n  %s", code(n.Why))
			}
			p("\n")
			mdNoteEvidence(&b, n)
		}
		p("\n</details>\n")
	}

	mdScanDetails(&b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

// mdGroups is the findings table — one row per group, the rule id in its own column where an
// auditor finds it — followed by one <details> per group with the plain label, the hint, the
// rule's rationale, the judge's triage and every evidence line with its FULL path.
func mdGroups(b *strings.Builder, groups []Group) {
	fmt.Fprintf(b, "| Severity | Who · what | Rule | Where |\n|---|---|---|---|\n")
	for _, g := range groups {
		what := cell(friendlyArtifact(g.Artifact)) + " · " + cell(g.Title)
		if g.Count > 1 {
			what += fmt.Sprintf(" ×%d", g.Count)
			if g.Files > 1 {
				what += fmt.Sprintf(" in %d files", g.Files)
			}
		}
		if g.Advisory {
			what += " · advisory: not confirmed"
		}
		fmt.Fprintf(b, "| %s %s | %s | %s | %s |\n", sevIcon(g.Severity), g.Severity, what, cell(g.RuleID), cell(where(g)))
	}
	for _, g := range groups {
		fmt.Fprintf(b, "\n<details>\n<summary><code>%s</code> — details and evidence</summary>\n\n", html.EscapeString(g.RuleID))
		if l := dimLabel(g.Dimension); l != "" {
			fmt.Fprintf(b, "- In plain terms: %s\n", l)
		}
		if a := actionHint(g.Dimension); a != "" {
			fmt.Fprintf(b, "- What to do: %s\n", a)
		}
		fmt.Fprintf(b, "- Why: %s\n", code(g.Why))
		if g.Triage != "" {
			fmt.Fprintf(b, "- ⚖ Triage (LLM, advisory): %s\n", code(g.Triage))
		}
		if len(g.Evidence) > 0 {
			fmt.Fprintf(b, "- Evidence:\n")
			for _, e := range g.Evidence {
				switch {
				case e.Line == 0 && e.Snippet != "":
					fmt.Fprintf(b, "  - %s — %s\n", code(e.File), code(e.Snippet))
				case e.Snippet != "":
					fmt.Fprintf(b, "  - %s — %s\n", code(fmt.Sprintf("%s:%d", e.File, e.Line)), code(e.Snippet))
				default:
					fmt.Fprintf(b, "  - %s\n", code(fmt.Sprintf("%s:%d", e.File, e.Line)))
				}
			}
			if rest := g.MoreFiles(); rest > 0 {
				fmt.Fprintf(b, "  - … and %d more file(s) — `--json` or `--sarif` lists them all\n", rest)
			}
		}
		fmt.Fprintf(b, "\n</details>\n")
	}
}

// mdNoteEvidence lists what a coalesced coverage note covers — same three-line budget and the
// same named remainder as the terminal (writeNoteEvidence).
func mdNoteEvidence(b *strings.Builder, n model.Finding) {
	if len(n.Evidence) == 0 {
		return
	}
	if len(n.Evidence) == 1 {
		if e := n.Evidence[0]; e.Snippet != "" {
			fmt.Fprintf(b, "  - %s\n", code(e.Snippet))
		}
		return
	}
	const budget = 3
	for i, e := range n.Evidence {
		if i == budget {
			fmt.Fprintf(b, "  - … and %d more — `--json` or `--sarif` lists them all\n", len(n.Evidence)-budget)
			return
		}
		if e.Snippet != "" {
			fmt.Fprintf(b, "  - %s — %s\n", code(e.File), code(e.Snippet))
			continue
		}
		fmt.Fprintf(b, "  - %s\n", code(e.File))
	}
}

// mdInbox is the Downloads section: each item with its own score and one derived sentence; the
// heading says it is not part of the environment score.
func mdInbox(b *strings.Builder, ib model.InboxReport) {
	fmt.Fprintf(b, "\n## Downloads · %d agent-shaped item(s) under %s\n\nNot installed, checked on their own, not part of the score.\n\n", len(ib.Items), code(ib.Dir))
	if len(ib.Items) == 0 {
		fmt.Fprintf(b, "Nothing agent-shaped among %d entries; nothing else was read.\n", ib.Skipped)
	}
	if ib.Judge != nil && len(ib.Items) > 0 {
		fmt.Fprintf(b, "%s\n\n", text(judgeLine(ib.Judge)))
	}
	for _, it := range ib.Items {
		kind := it.Kind
		if it.Archive {
			kind = "zip"
		}
		if it.Error != "" {
			fmt.Fprintf(b, "- ⚪ %s (%s) — not checked: %s\n", code(it.Name), code(kind), code(it.Error))
			continue
		}
		level := score.Level(it.Overall)
		fmt.Fprintf(b, "- %s **%d/100 %s** %s (%s)", levelIcon(level), it.Overall, level, code(it.Name), code(kind))
		if worst := inboxWorst(it); worst != "" {
			fmt.Fprintf(b, " — worst: %s · %d finding(s)", code(worst), len(it.Findings))
		}
		fmt.Fprintf(b, "  \n  → %s\n", inboxAdvice(level, len(it.Findings), it.Judged))
		for _, n := range it.Notes {
			fmt.Fprintf(b, "  - note: %s\n", code(n.Title))
		}
	}
	if ib.Skipped > 0 && len(ib.Items) > 0 {
		fmt.Fprintf(b, "\n(%d other entries were not agent-shaped and were not read)\n", ib.Skipped)
	}
	for _, n := range ib.Notes {
		fmt.Fprintf(b, "- %s — %s\n", code(n.Title), code(n.Why))
	}
}

// mdScanDetails is the auditor's footer, last for the same reason it is last in the terminal.
func mdScanDetails(b *strings.Builder, r model.ScanResult) {
	fmt.Fprintf(b, "\n## Scan details\n\n")
	if len(r.Locations) > 0 {
		fmt.Fprintf(b, "Where the scan looked:\n\n| Place | Status | Path |\n|---|---|---|\n")
		for _, l := range r.Locations {
			fmt.Fprintf(b, "| %s | %s | %s |\n", cell(l.Name), l.Status, cell(l.Path))
		}
		fmt.Fprintf(b, "\n")
	}
	e := r.Env
	fmt.Fprintf(b, "- Inventory: skills=%d mcp=%d hooks=%d permissions=%d subagents=%d commands=%d plugins=%d connectors=%d\n",
		e.Skills, e.MCPServers, e.Hooks, e.Permissions, e.Subagents, e.Commands, e.Plugins, e.Connectors)
	if e.BundledSkills > 0 {
		fmt.Fprintf(b, "- Inside plugins: %d skill(s), scanned as part of their plugin (not counted in skills= above)\n", e.BundledSkills)
	}
	if n := e.Rules + e.OutputStyles + e.Memories; n > 0 {
		fmt.Fprintf(b, "- Auto-loaded: rules=%d output-styles=%d memory=%d\n", e.Rules, e.OutputStyles, e.Memories)
	}
	if e.Workflows > 0 {
		fmt.Fprintf(b, "- On-demand: workflows=%d (bodies load when invoked, not at startup)\n", e.Workflows)
	}
	if cats := detect.ASICatalogue(); len(cats) > 0 {
		var silent []string
		for _, c := range cats {
			if !c.Covered {
				silent = append(silent, c.ID)
			}
		}
		if len(silent) > 0 {
			fmt.Fprintf(b, "- OWASP Agentic (2026): %d/%d categories have rules; silent on %s (runtime behaviour a static scan cannot witness)\n",
				len(cats)-len(silent), len(cats), strings.Join(silent, ", "))
		}
	}
	fmt.Fprintf(b, "- Static analysis only: it cannot prove malice or see runtime behaviour. `--json` and `--sarif` carry every path in full; rule ids are explained in docs/rules.md: https://github.com/basdotio/agent-guard/blob/dev/docs/rules.md\n")
	fmt.Fprintf(b, "\n<sub>AgentGuard %s", text(r.ToolVersion))
	if r.ScannedAt > 0 {
		fmt.Fprintf(b, " · scanned %s", time.Unix(r.ScannedAt, 0).UTC().Format("2006-01-02 15:04 UTC"))
	}
	fmt.Fprintf(b, "</sub>\n")
}

// code wraps s in an inline code span that no content of s can close early: the fence is one
// backtick longer than the longest run inside, and a leading/trailing backtick or space is
// padded (CommonMark strips one space from each end). Empty stays empty. Control characters,
// bidi overrides and zero-width characters are already gone (sanitizeResult), so s has no
// newline that could end the span.
func code(s string) string {
	if s == "" {
		return ""
	}
	run, longest := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		s = " " + s + " "
	}
	return fence + s + fence
}

// cell is code for a table cell: GitHub splits cells on `|` even inside a code span unless it
// is escaped, so this is the only place the escape belongs.
func cell(s string) string {
	return code(strings.ReplaceAll(s, "|", "\\|"))
}

// text backslash-escapes the ASCII punctuation markdown would otherwise read as markup, for the
// few sentences that are the report's own words with an operator-supplied value inside (the
// judge's endpoint reason, the tool version). Never used for anything that came off the scanned
// tree — that goes through code.
func text(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '\\', '`', '*', '_', '{', '}', '[', ']', '<', '>', '(', ')', '#', '+', '-', '!', '|', '~', '@':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
