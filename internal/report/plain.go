// SPDX-License-Identifier: MIT
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// This file is the report's plain-language layer, shared by the terminal and HTML renderers so
// the two say the same thing in the same words.
//
// The report has two readers and used to serve one. An auditor wants the rule id, the exact
// path, the score model and the coverage gaps; a person who just installed a plugin wants to
// know whether they are safe and what, if anything, to do. The first version wrote everything
// for the auditor and asked the second reader to translate. Everything here is DERIVED from the
// findings — a sentence, a label, a shorter path — and never a new judgement: the verdict line
// cannot disagree with the list under it, and a plain label cannot soften a severity.

// dimLabel says, in one plain phrase, what a finding in this dimension is about. One phrase per
// dimension rather than per rule (70+ rules) — coarse on purpose: the rule's own Title and Why
// carry the specifics, this only tells a non-specialist which drawer the problem is in.
func dimLabel(d int) string {
	switch d {
	case 1:
		return "text that tries to steer the AI"
	case 2:
		return "asks for more permission than it needs"
	case 3:
		return "could send data or secrets off this machine"
	case 4:
		return "can run programs on your machine"
	case 5:
		return "installs or fetches software from outside"
	case 6:
		return "hides what it really does"
	case 7:
		return "may behave differently under certain conditions"
	case 8:
		return "could exhaust your machine's resources"
	case 9:
		return "reaches into sensitive files"
	case 10:
		return "does something other than what it says"
	}
	return ""
}

// friendlyArtifact turns "plugin:figma@claude-plugins-official (2.2.107)" into
// "figma plugin, from claude-plugins-official (2.2.107)". The kind:name form is the artifact's
// identity in JSON and stays there; this is how a sentence refers to it.
func friendlyArtifact(art string) string {
	kind, name, ok := strings.Cut(art, ":")
	if !ok {
		return art
	}
	switch kind {
	case "plugin":
		if base, from, ok := strings.Cut(name, "@"); ok {
			return base + " plugin, from " + from
		}
		return name + " plugin"
	case "skill":
		return "skill " + name
	case "connector":
		return name + " connector (remote MCP, from Claude Desktop)"
	case "hook":
		return "hook " + name
	case "permission":
		if strings.Contains(name, "local") {
			return "permission rules (project settings.local.json)"
		}
		return "permission rules (settings.json)"
	case "mcp":
		return "MCP server " + name
	case "memory":
		return "memory file " + name
	case "instruction":
		return "instructions " + name
	}
	return kind + " " + name
}

// shortPath makes an evidence path readable at a glance: a plugin-cache path drops the
// plugins/cache/<marketplace>/<plugin>/<version>/ prefix in favour of "plugin › rest", and any
// other long path keeps its last three segments. The full path stays in --verbose, JSON and
// SARIF — this is for finding the file, not for opening it from a copy-paste.
func shortPath(file string) string {
	segs := strings.Split(file, "/")
	for i := 0; i+4 < len(segs); i++ {
		if segs[i] == "plugins" && segs[i+1] == "cache" {
			rest := segs[i+5:]
			if len(rest) == 0 {
				return segs[i+3]
			}
			return segs[i+3] + " › " + strings.Join(rest, "/")
		}
	}
	if len(segs) > 4 {
		return "…/" + strings.Join(segs[len(segs)-3:], "/")
	}
	return file
}

// actionable counts the deterministic groups at medium or above — the threshold the report
// already draws between "act on this" and "worth knowing".
func actionable(det []Group) int {
	n := 0
	for _, g := range det {
		if g.Severity.Rank() >= model.SevMedium.Rank() {
			n++
		}
	}
	return n
}

// verdictSentence is the one sentence a non-specialist reads. It is a function of the level
// band and the counts already printed below it, so it can never say something the list does not.
func verdictSentence(level string, act, total int) string {
	var lead string
	switch level {
	case "Low":
		lead = "Your Claude Code setup looks safe."
	case "Watch":
		lead = "Mostly fine, with a few things to review."
	case "Elevated":
		lead = "There are problems you should fix before relying on this setup."
	default:
		lead = "Serious problems were found. Stop and review before using this setup."
	}
	return lead + countClause(act, total)
}

// incompleteLead replaces the Low band's "looks safe" when something Claude Code loads was not
// fully read. The words are the ones the Not checked line ends on.
const incompleteLead = "Low risk in what was read, but coverage is incomplete."

// coverageVerdict is verdictSentence with one more question asked of the result: was something
// Claude Code actually loads left not fully read? "Looks safe" is a claim about what the agent
// loads, and a settings.json that did not parse was read as "looks safe … Nothing was found to
// check" over the file that holds the hooks, permissions and env (P-013); a skill whose payload sat
// in a subdirectory the scan could not list read "looks safe" too (P-017).
//
// The set that answers it is named here and nowhere else (unreadLoaded). Only the Low band's lead
// changes; the other three already report problems, which an unread file can only add to. The
// counting half never changes.
func coverageVerdict(level string, act, total int, r model.ScanResult) string {
	if level != "Low" || !unreadLoaded(r) {
		return verdictSentence(level, act, total)
	}
	return incompleteLead + countClause(act, total)
}

// unreadLoaded reports whether the result says that something Claude Code loads was not fully read.
// Selected by rule id and by where the note sits:
//   - any coverage note attached to an artifact (itemGaps): the item is in the inventory and in the
//     score, and was not fully read;
//   - at scan level, an IO-000 or PARSE-000 — a file in a load path that could not be read (an
//     unreadable settings.json is the twin of an unparsable one) or a config entry not understood —
//     and any COV-000 except the deliberate skips below. Most of what detect and collect say they did
//     not read is a scan-level COV-000 (an unlistable skill subdirectory, a file over the size cap, a
//     hook's second stage, a rules/ symlinked out of the root), and none of it used to hedge;
//   - an artifact carrying SUP-004: it points the agent at a file inside a tree the scan skips by name,
//     and its companion note (third-party trees) is one of the deliberate skips.
//
// A future scan-level COV-000 hedges until someone names it a deliberate skip: the wrong direction
// for a gap is to say "safe" once too often. Everything that is not about reading — the judge's
// LLM-* notes (LLM-002 is a privacy notice, decided by the maintainer 2026-10-09), the gate's GATE-*,
// trust decisions — leaves the headline alone and stays in the Not checked line.
func unreadLoaded(r model.ScanResult) bool {
	if len(itemGaps(r)) > 0 {
		return true
	}
	for _, n := range r.Notes {
		if fileNotRead(n) || (n.RuleID == "COV-000" && !deliberateSkip(n.Title)) {
			return true
		}
	}
	for _, a := range r.Artifacts {
		for _, f := range a.Findings {
			if f.RuleID == "SUP-004" && f.Source != model.SrcLLM {
				return true
			}
		}
	}
	return false
}

// fileNotRead is a scan-level note saying a file a collector found could not be read (IO-000) or
// parsed (PARSE-000). One predicate for the two places that ask: the headline (unreadLoaded) and
// the Checked line, which names the file (scanGaps).
func fileNotRead(n model.Finding) bool { return n.RuleID == "IO-000" || n.RuleID == "PARSE-000" }

// deliberateSkip reports whether a scan-level COV-000 title is one of the four notes that disclose a
// skip of something that is not loaded, or that was read after all — the only scan-level coverage
// notes that leave "looks safe" alone. All four share one rule id, one place and one source with the
// gaps, so the title is the one thing in the data that tells them apart; it is compared through the
// producers' own exported constants, the way detect's coalescer matches them, so a reworded title
// moves both sides at once and the report holds no title text of its own.
func deliberateSkip(title string) bool {
	switch title {
	case collect.UnownedNoteTitle:
		// The user's own sessions/, file-history/ at the top of the root: no collector owns them and
		// nothing loads them; present on nearly every real machine (decided by the maintainer, 2026-10-09).
		return true
	case collect.EmptyRootNoteTitle:
		// Nothing was collected, so nothing loaded went unread; the Checked line says so.
		return true
	case detect.GeneratedDirNoteTitle:
		// node_modules/, vendor/, .git/, coverage/ inside an artifact: skipped by name on purpose, and
		// common on real machines. An artifact that points the agent INTO one raises SUP-004, which hedges.
		return true
	case detect.HookOwnedNoteTitle:
		// A hook's script found inside its plugin's tree: read in full with the plugin; only the per-hook
		// attribution is missing.
		return true
	}
	return false
}

// countClause is the counting half of the verdict, with its leading space.
func countClause(act, total int) string {
	switch {
	case act > 0 && total > act:
		return fmt.Sprintf(" %d finding%s need%s a look (medium or above); %d more %s informational.",
			act, plural(act), singularVerb(act), total-act, isAre(total-act))
	case act > 0:
		return fmt.Sprintf(" %d finding%s need%s a look (medium or above).", act, plural(act), singularVerb(act))
	case total > 0:
		return fmt.Sprintf(" %d informational finding%s, nothing needs action.", total, plural(total))
	}
	return " No findings."
}

// checkedLine names what the scan looked at, in words, non-zero counts only.
func checkedLine(e model.EnvSummary) string { return checkedWithGaps(e, "") }

// checkedWithGaps is checkedLine plus the inventory items that were found but not fully checked,
// already rendered by the caller (gapList) because only the caller knows how its surface quotes a
// path. Without them the line said "Nothing was found to check under this root." over a root whose
// one settings.json was found and could not be parsed — the inventory counts come from what the
// collectors extracted, and a file that did not parse yields nothing to count.
func checkedWithGaps(e model.EnvSummary, gaps string) string {
	return composeChecked(checkedParts(e), gaps)
}

// checkedSummary is the Checked line the three human renderers print, derived from the whole result.
//
// The counts are the inventory's. The inventory counts the collectors' surfaces, and some scanned
// things are not among them — the file `check <file>` reads, the tree `check <dir>` reads, a root's
// CLAUDE.md, a settings env block — so a report used to say "Nothing was found to check" above a high
// finding on the very file it had checked. When the inventory counts nothing, the line counts the
// scanned artifacts instead (scannedParts). A report whose inventory counts something keeps the line
// it always had.
//
// The gaps are the items found and not fully checked: an artifact's own coverage note (itemGaps), then
// a scan-level IO-000 or PARSE-000 (scanGaps). item quotes one gap the way the caller's surface quotes
// a path.
func checkedSummary(r model.ScanResult, item func(gap) string) string {
	parts := checkedParts(r.Env)
	if len(parts) == 0 {
		parts = scannedParts(r)
	}
	return composeChecked(parts, gapList(append(itemGaps(r), scanGaps(r)...), item))
}

// composeChecked joins the counts and the rendered gaps into the sentence.
func composeChecked(parts []string, gaps string) string {
	notFully := ""
	if gaps != "" {
		notFully = "Not fully checked: " + gaps + "."
	}
	switch {
	case len(parts) == 0 && notFully == "":
		return "Nothing was found to check under this root."
	case len(parts) == 0:
		return notFully
	case notFully == "":
		return "Checked " + strings.Join(parts, ", ") + "."
	}
	return "Checked " + strings.Join(parts, ", ") + ". " + notFully
}

// kindNoun is how the Checked line counts a scanned artifact of each kind when the inventory counted
// nothing. Fixed order — the inventory's own order for the kinds it shares, then the three it never
// counts — so the line does not depend on the order the artifacts were collected in. One noun per
// kind, not per artifact: naming files is the findings list's job. A permission artifact is a block
// of a settings file (its allow/deny list, or its env), so it is counted as one.
var kindNoun = []struct {
	kind      model.ArtifactKind
	one, many string
}{
	{model.KindPlugin, "plugin", "plugins"},
	{model.KindSkill, "skill", "skills"},
	{model.KindMCP, "MCP server", "MCP servers"},
	{model.KindHook, "hook", "hooks"},
	{model.KindPermission, "settings block", "settings blocks"},
	{model.KindSubagent, "subagent", "subagents"},
	{model.KindCommand, "command", "commands"},
	{model.KindRule, "rule file", "rule files"},
	{model.KindOutputStyle, "output style", "output styles"},
	{model.KindMemory, "memory file", "memory files"},
	{model.KindWorkflow, "workflow", "workflows"},
	{model.KindConnector, "remote connector", "remote connectors"},
	{model.KindInstruction, "file", "files"},
	{model.KindDirectory, "directory", "directories"},
	{model.KindQuarantined, "quarantined item", "quarantined items"},
}

// scannedParts counts the scanned artifacts by kind, leaving out the ones that are themselves a gap
// (an artifact carrying its own coverage note was found, not checked — the gap list names it).
// A kind missing from kindNoun is counted as an item rather than dropped.
func scannedParts(r model.ScanResult) []string {
	counts := map[model.ArtifactKind]int{}
	for _, a := range r.Artifacts {
		if !isGapArtifact(a) {
			counts[a.Kind]++
		}
	}
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	other := 0
	known := map[model.ArtifactKind]bool{}
	for _, k := range kindNoun {
		known[k.kind] = true
		add(counts[k.kind], k.one, k.many)
	}
	for k, n := range counts {
		if !known[k] {
			other += n
		}
	}
	add(other, "item", "items")
	return parts
}

// checkedParts lists the non-zero inventory counts in words.
func checkedParts(e model.EnvSummary) []string {
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(e.Plugins, "plugin", "plugins")
	add(e.Skills, "skill", "skills")
	add(e.MCPServers, "MCP server", "MCP servers")
	add(e.Hooks, "hook", "hooks")
	add(e.Permissions, "permission rule", "permission rules")
	add(e.Subagents, "subagent", "subagents")
	add(e.Commands, "command", "commands")
	add(e.Rules, "rule file", "rule files")
	add(e.OutputStyles, "output style", "output styles")
	add(e.Memories, "memory file", "memory files")
	add(e.Workflows, "workflow", "workflows")
	add(e.Connectors, "remote connector", "remote connectors")
	if e.BundledSkills > 0 {
		parts = append(parts, fmt.Sprintf("%d skill(s) inside those plugins", e.BundledSkills))
	}
	return parts
}

// gap is an item that was found but not fully checked: an artifact carrying its own coverage note
// (today that is collect's withParseError — a settings.json, an MCP config or installed_plugins.json
// that did not parse), or a scan-level IO-000 / PARSE-000 — a file a collector found and could not
// read or parse, which yields no artifact at all (an unreadable settings.json is the twin of an
// unparsable one). Where is the note's file in short form, or the artifact's name (or, at scan level,
// the note's title) when the note names no file. Other scan-level notes are not gaps here: they are
// about parts of items the counts already include — a subdirectory of a skill, one file of a plugin —
// with evidence relative to that item, and the Not checked line names them.
type gap struct{ Where, RuleID string }

// itemGaps lists the artifact gaps in artifact order — the data half; each renderer quotes it (gapList).
func itemGaps(r model.ScanResult) []gap {
	var out []gap
	for _, a := range r.Artifacts {
		for _, f := range a.Findings {
			if !isCoverageNote(f) {
				continue
			}
			where := friendlyArtifact(string(a.Kind) + ":" + a.Name)
			if len(f.Evidence) > 0 && f.Evidence[0].File != "" {
				where = shortPath(f.Evidence[0].File)
			}
			out = append(out, gap{Where: where, RuleID: f.RuleID})
		}
	}
	return out
}

// scanGaps lists the scan-level IO-000 and PARSE-000 notes, in note order.
func scanGaps(r model.ScanResult) []gap {
	var out []gap
	for _, n := range r.Notes {
		if !fileNotRead(n) {
			continue
		}
		where := n.Title
		if len(n.Evidence) > 0 && n.Evidence[0].File != "" {
			where = shortPath(n.Evidence[0].File)
		}
		out = append(out, gap{Where: where, RuleID: n.RuleID})
	}
	return out
}

// isCoverageNote is a dimension-0 note that is not a trust decision.
func isCoverageNote(f model.Finding) bool { return f.Dimension == 0 && !isSuppression(f.RuleID) }

// isGapArtifact reports whether an artifact carries its own coverage note.
func isGapArtifact(a model.ArtifactReport) bool {
	for _, f := range a.Findings {
		if isCoverageNote(f) {
			return true
		}
	}
	return false
}

// gapBudget is how many gaps the summary names before counting the rest; the full list is in the
// Not checked section, so the summary only has to make the first ones impossible to miss.
const gapBudget = 3

// gapList renders gaps with item, at most gapBudget of them, and says how many it left out.
func gapList(gs []gap, item func(gap) string) string {
	parts := make([]string, 0, gapBudget+1)
	for i, g := range gs {
		if i == gapBudget {
			parts = append(parts, fmt.Sprintf("and %d more", len(gs)-gapBudget))
			break
		}
		parts = append(parts, item(g))
	}
	return strings.Join(parts, ", ")
}

// plainGap is a gap as plain text — "path [RULE-ID]", the rule id in brackets as everywhere else
// in the terminal report. The terminal sanitizes the whole line; HTML renders from sanitizeResult.
func plainGap(g gap) string { return g.Where + " [" + g.RuleID + "]" }

// action is one line of "what to look at": the worst groups, in the order they are printed.
type action struct {
	Severity model.Severity
	Artifact string // friendly
	Title    string
	Where    string // short path of the first evidence, plus how many more files
	RuleID   string
}

// actions lists the deterministic groups at medium or above, at most max, in report order
// (severity, then breadth) — the top-3 recommendations spec §9 asks for, derived and not invented.
func actions(det []Group, max int) []action {
	var out []action
	for _, g := range det {
		if g.Severity.Rank() < model.SevMedium.Rank() || len(out) == max {
			continue
		}
		out = append(out, action{Severity: g.Severity, Artifact: friendlyArtifact(g.Artifact), Title: g.Title, RuleID: g.RuleID, Where: where(g)})
	}
	return out
}

// where is the place a reader starts on a group: the first evidence's short path and line,
// plus how many more files the group touches. Shared by the "what to look at" list and the
// markdown table so the two never name a different starting point.
func where(g Group) string {
	if len(g.Evidence) == 0 {
		return ""
	}
	e := g.Evidence[0]
	w := shortPath(e.File)
	if e.Line > 0 {
		w += fmt.Sprintf(":%d", e.Line)
	}
	if g.Files > 1 {
		w += fmt.Sprintf(" (+%d more file%s)", g.Files-1, plural(g.Files-1))
	}
	return w
}

// actionableWorst counts the deterministic groups at medium or above and names the worst of
// them — the two facts the verdict line is made of, derived from the groups already printed.
func actionableWorst(det []Group) (act int, worst Group) {
	for _, g := range det {
		if g.Severity.Rank() < model.SevMedium.Rank() {
			continue
		}
		act++
		if worst.RuleID == "" || g.Severity.Rank() > worst.Severity.Rank() {
			worst = g
		}
	}
	return act, worst
}

// splitNotes separates trust decisions (REP-GOOD, IGN-000) from coverage gaps: the first kind
// changed the score and is shown in full; the second is folded by default.
func splitNotes(notes []model.Finding) (trust, coverage []model.Finding) {
	for _, n := range notes {
		if isSuppression(n.RuleID) {
			trust = append(trust, n)
		} else {
			coverage = append(coverage, n)
		}
	}
	return trust, coverage
}

// trustNames lists what the trust decisions were about, for the summary line.
func trustNames(trust []model.Finding) string {
	var names []string
	for _, n := range trust {
		names = append(names, trustName(n))
	}
	return strings.Join(names, ", ")
}

// trustName is what one trust decision was about: the baseline, or the entry's path.
func trustName(n model.Finding) string {
	switch {
	case n.RuleID == "IGN-000":
		return "your .aguardignore baseline"
	case len(n.Evidence) > 0 && n.Evidence[0].File != "":
		return n.Evidence[0].File
	}
	return n.RuleID
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func singularVerb(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// judgeLine is the one sentence a report owes the reader about the LLM judge, or "" when --llm
// was not passed. It separates three states a findings list alone cannot: not requested
// (nothing printed), requested but did not run (and why), and ran with nothing to add — which
// used to look identical to "never ran" because the judge section only appeared when it had
// findings.
func judgeLine(j *model.JudgeSummary) string {
	if j == nil {
		return ""
	}
	if !j.Ran {
		if j.Reason != "" {
			return "LLM judge did not run: " + j.Reason
		}
		return "LLM judge did not run."
	}
	s := fmt.Sprintf("LLM judge ran over %d artifact(s) in %d call(s)", j.Artifacts, j.Calls)
	switch {
	case j.Repaired > 0:
		// A repaired reply is an answer, but the reader should be able to see that some needed it.
		s += fmt.Sprintf(" (%d failed, %d skipped, %d repaired)", j.Failed, j.Skipped, j.Repaired)
	case j.Failed > 0 || j.Skipped > 0:
		s += fmt.Sprintf(" (%d failed, %d skipped)", j.Failed, j.Skipped)
	}
	switch j.Findings {
	case 0:
		s += " and had nothing to add."
	case 1:
		s += " and added 1 advisory lead."
	default:
		s += fmt.Sprintf(" and added %d advisory leads.", j.Findings)
	}
	return s
}

// actionHint is the plain layer's second sentence: what a reader can DO about a finding in this
// dimension. Like dimLabel it is one fixed sentence per dimension, not one per rule, and like
// dimLabel it derives nothing and judges nothing — it never says a finding is fine, only what to
// look at to decide. "This plugin runs ffmpeg, which is normal" is a judgement the reader (or the
// skill reading the report with them) makes; the sentence here only points at the question.
func actionHint(d int) string {
	switch d {
	case 1:
		return "Read the quoted text. If it tells the AI to ignore its rules or do something you did not ask for, remove the skill."
	case 2:
		return "Narrow the grant to the exact commands you need. An open-ended grant is what an attacker needs too."
	case 3:
		return "Ask whether this tool has a reason to read credentials AND talk to the network. If you cannot name one, do not run it."
	case 4:
		return "Look at what it runs. Tools that convert documents or build code run programs; a skill that only gives instructions has no reason to."
	case 5:
		return "Check where it downloads from and whether the version is pinned. An unpinned or unknown source can change under you."
	case 6:
		return "Encoded or hidden content has no place in a skill you are meant to read. Decode it, or remove the skill."
	case 7:
		return "A hint, not a verdict: read the lines and ask what condition changes the behaviour."
	case 8:
		return "A hint, not a verdict: check that the loop or job has a way to stop."
	case 9:
		return "Ask whether this tool needs that access at all. If not, remove it or replace it."
	case 10:
		return "The description and the code disagree. Trust the code, and ask the author why."
	}
	return ""
}

// judgeSummaryLine is the one line about the judge for the report's summary: the environment
// pass, then the Downloads pass when there was one. Two passes, one line — a header that says
// "leads 0" while the Downloads section below carries a lead reads as a contradiction, and the
// first real user hit exactly that.
func judgeSummaryLine(r model.ScanResult) string {
	s := judgeLine(r.Judge)
	if r.Inbox == nil || r.Inbox.Judge == nil || len(r.Inbox.Items) == 0 {
		return s
	}
	dl := strings.TrimPrefix(judgeLine(r.Inbox.Judge), "LLM judge ")
	dl = strings.Replace(dl, "artifact(s)", "Downloads item(s)", 1)
	if s == "" {
		return "LLM judge " + dl
	}
	return s + " Downloads: " + dl
}

// worstLine names the lowest-scoring artifact next to the environment score. The environment
// number is a mean, so 144 perfect artifacts pull a 0/100 up to a 69 headline, and the report
// never printed the 0. Derived only: the artifact's own score and name, plus how many
// artifacts the mean spans and how many sit at 100. Nothing here changes Overall, and the line
// is omitted when it would restate the headline (one artifact, or the worst equals the mean).
func worstLine(r model.ScanResult) string {
	w, perfect, ok := worstItem(r)
	if !ok {
		return ""
	}
	return fmt.Sprintf("Worst single item: %d/100 — %s. The score above is an average over %d items (%d of them at 100), so it hides this one.",
		w.Score, friendlyArtifact(string(w.Kind)+":"+w.Name), len(r.Artifacts), perfect)
}

// worstItem is the artifact worstLine is about, with how many artifacts sit at 100; ok is
// false when the line would restate the headline.
func worstItem(r model.ScanResult) (w model.ArtifactReport, perfect int, ok bool) {
	if len(r.Artifacts) < 2 {
		return w, 0, false
	}
	worst := -1
	for i, a := range r.Artifacts {
		if a.Score == 100 {
			perfect++
		}
		if worst < 0 || a.Score < r.Artifacts[worst].Score {
			worst = i
		}
	}
	w = r.Artifacts[worst]
	if w.Score >= r.Overall {
		return w, perfect, false
	}
	return w, perfect, true
}

// escalation is one artifact the judge moved: its kind:name label and the two scores.
type escalation struct {
	Label    string
	From, To int
}

// escalations lists the artifacts whose score the judge actually moved, worst drop first,
// capped at three; more says how many were cut so the header stays a header.
func escalations(r model.ScanResult) (hits []escalation, more int) {
	for _, a := range r.Artifacts {
		if a.ScoreEffective < a.Score {
			hits = append(hits, escalation{string(a.Kind) + ":" + a.Name, a.Score, a.ScoreEffective})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].From-hits[i].To > hits[j].From-hits[j].To })
	if len(hits) > 3 {
		more = len(hits) - 3
		hits = hits[:3]
	}
	return hits, more
}

// inboxAdvice is the one sentence about a downloaded item, derived from its level, whether it
// has any findings at all, and whether the judge looked — the same rule as verdictSentence:
// the plain layer restates what is below it, it never overrides it. "Nothing flagged" is said
// only when nothing was: an 88/100 with one medium is in the Low band and still has a finding
// to read, and the sentence that said otherwise was wrong on the first real sample.
func inboxAdvice(level string, findings int, judged bool) string {
	if findings == 0 {
		if judged {
			return "Nothing flagged by the static rules or the AI deep check. Install it if you know where it came from."
		}
		return "Nothing flagged by the static rules. Install it if you know where it came from."
	}
	switch level {
	case "Low", "Watch":
		if findings == 1 {
			return "One thing to read before you install it."
		}
		return "Read the findings before you install it."
	case "Elevated":
		return "Read every finding before installing, and ask the author about the ones you cannot explain."
	default:
		return "Do not install this. If you need it, go through the flagged lines with someone who can read them."
	}
}

// inboxWorst names the most severe finding of an item, or "" when it has none.
// It names the worst DETERMINISTIC finding — the one the item's score comes from; an LLM lead
// is advisory and is counted beside it instead, so a "high" the judge guessed never reads as
// the reason for an 83.
func inboxWorst(it model.InboxItem) string {
	var worst *model.Finding
	leads := 0
	for i := range it.Findings {
		f := &it.Findings[i]
		if f.Source == model.SrcLLM {
			leads++
			continue
		}
		if worst == nil || f.Severity.Rank() > worst.Severity.Rank() {
			worst = f
		}
	}
	s := ""
	if worst != nil {
		s = "[" + worst.RuleID + "] " + worst.Title
	}
	if leads > 0 {
		if s != "" {
			s += " · "
		}
		s += fmt.Sprintf("%d AI lead(s), advisory", leads)
	}
	return s
}
