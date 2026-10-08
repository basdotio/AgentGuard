// SPDX-License-Identifier: MIT
// Command gen-rules writes docs/rules.md from the engine's own rule set.
//
// It is generated rather than written because a rule reference is only useful if it is true:
// a hand-maintained catalogue goes stale the first time someone adds a rule, and a stale
// reference for a security tool is worse than no reference — a reader who cannot find
// SUP-004 knows they have to read the source, while a reader who finds the wrong severity
// acts on it. `make docs` regenerates; CI runs it and fails if the result differs from what
// is committed, so drift is not expressible.
//
// Two groups of IDs are NOT in builtinRules() and are therefore maintained by hand below,
// clearly separated: the structural checks (which accumulate evidence across a file or an
// artifact instead of matching a line) and the dimension-0 notes (which are the scan
// describing its own coverage, not findings about the artifact).
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// structural documents the checks that cannot be a Rule: they need more than one line, or
// more than one file, to reach a verdict. Hand-maintained — keep in step with
// internal/detect/exfil.go and the absorb logic described there.
var structural = []entry{
	{"EXFIL-001", 3, model.SevHigh, false,
		"Exfiltration chain in one file",
		"The same file both reads a credential and makes an outbound request. Two legs (credential + egress) complete a chain; encoding is an amplifier, not a requirement. When every network target in that file is loopback (127.0.0.1, localhost, or ::1), the finding stays but drops to low + advisory — same band as EXFIL-002 — because the data has not left the machine. In SKILL.md and CLAUDE.md a bare URL is documentation, not a request: the network leg there needs an actual client call or covert-channel tool, while in scripts a URL literal still counts."},
	{"EXFIL-003", 3, model.SevHigh, false,
		"Exfiltration chain with encoding",
		"All three legs in one file — credential read, encode, egress. Raised INSTEAD of EXFIL-001 (one fact reported twice reads as two problems), together with OBF-004. Same loopback downgrade as EXFIL-001; OBF-004 is not raised when nothing left the machine."},
	{"EXFIL-002", 3, model.SevLow, true,
		"Exfiltration surface split across files",
		"One file in the artifact reads credentials, a different one makes outbound requests. Much weaker than the same-file chain — unrelated files legitimately do each half — so it is advisory, and only raised when no same-file chain was found."},
	{"OBF-004", 6, model.SevMedium, false,
		"Encoding co-occurs with an exfiltration chain",
		"The encode leg of EXFIL-003, scored in dimension 6 so its penalty ADDS to the dimension-3 finding instead of being absorbed by it (within a dimension only the highest hit counts). Not raised when the chain's network targets are all loopback."},
	{"HOOK-002", 4, model.SevMedium, false,
		"Hook second stage is outside HOME and was not read",
		"A hook command names a script that resolves outside the scan home. Spec §16.2 still forbids reading it; this finding scores the refusal itself, because an unaudited payload at a silent intercept is a risk, not just a coverage gap. Raised to high on PermissionRequest, which takes the authorization decision. The matching COV-000 note is kept: coverage and scoring are different facts."},
	{"HOOK-003", 4, model.SevHigh, false,
		"Hook forwards event payload over HTTP",
		"type=http posts the full event (tool inputs, command lines, permission prompts) to a URL. Ordinary events to loopback are low (a local sidecar). PermissionRequest to loopback is high (it still takes allow/deny). Any non-loopback destination is high, because the operation stream is leaving the machine."},
	{"SUP-004", 5, model.SevMedium, false,
		"Artifact points the agent into an excluded directory",
		"Generated/vendored directories (dist/, node_modules/, …) are not scanned, because the canonical hash must survive a rebuild. This fires when the artifact's readable half steers the agent at the half a directory name made unreadable — a path reference, not a bare word, and not in prose docs."},
	{"OBF-006", 6, model.SevHigh, false,
		"File content does not match its name",
		"A file with a text extension (.txt, .md, .json, …) begins with the signature of an archive or executable (zip, gzip, ELF, Mach-O, PE). The text rules read compressed bytes and match nothing — the disguise's purpose. Images and PDFs are not flagged: inert oddities are noise."},
	{"OBF-007", 6, model.SevLow, false,
		"Content hidden below a long run of blank lines",
		"200 or more consecutive blank lines with code after them. No editor page, `head`, or size-capped excerpt reaches past a run that long, so what follows is read by the interpreter and nobody else. Cites the first line after the run."},
	{"SUP-005", 5, model.SevHigh, false,
		"Compiled Python bytecode shipped alongside the source",
		".pyc files (in __pycache__ or loose) travel with the artifact. Python loads a matching .pyc instead of compiling the .py beside it, so what runs need not be what anyone read; no text scanner reads bytecode, this one included. Raised from the tree walk, so it lands even though __pycache__ itself is skipped."},
	{"SUP-006", 5, model.SevHigh, false,
		"Package source redirected to an unofficial registry",
		"An .npmrc/.yarnrc/pip key, a `config set registry` command, or GOPROXY/PIP_INDEX_URL/NPM_CONFIG_REGISTRY points a package manager at a host that is not the vendor's registry, GitHub Packages, a known public mirror, or loopback. Shell variables are resolved one level within the same file; an unresolvable target is reported as unknown, not skipped. Comment-only lines are ignored; comments explaining why the redirect is fine are not consulted."},
	{"EXFIL-005", 3, model.SevHigh, false,
		"Instruction file imports a credential into the agent's context",
		"An @import in CLAUDE.md (or another instruction file) resolves to a credential: a file inside .ssh/.aws/.gnupg/…, or a file named .env, id_rsa, credentials, *.key, *.p12 … Claude Code expands imports into context at launch. The file is NOT read by the scan (a COV-000 says so); this scores the import itself. High for names that hold only secrets; medium for .pem / .npmrc / .pypirc and paths through .config, which often hold configuration. `.env.example` and friends are not credentials and are scanned normally."},
	{"REP-BAD", 3, model.SevCritical, false,
		"Known-malicious artifact (reputation list)",
		"The artifact's canonical hash matches a curated known-bad entry. Hash-exact, so it is the highest-confidence signal the tool has — critical, forcing the environment score to the High band."},
}

// permission documents internal/permcheck. These are configuration semantics rather than text
// patterns, so they are not Rules and cannot be read out of a table — permcheck builds each
// finding inline. Hand-maintained, and TestEveryRuleIDIsDocumented fails if an ID appears in
// the code without appearing here.
var permission = []entry{
	{"PERM-001", 3, model.SevHigh, false, "Inline plaintext secret in a permission entry",
		"An allow entry embeds a credential value directly; remove it and use a secret manager."},
	{"PERM-002", 2, model.SevMedium, false, "Wildcard arbitrary-code-execution grant",
		"The allow wildcards an interpreter (sh/bash/python/node/…), i.e. arbitrary code execution without confirmation; narrow it to exact commands."},
	{"PERM-003", 9, model.SevLow, false, "Overly broad file read/write grant",
		"Read/Write/Edit grant with an over-broad glob (`**`); narrow to specific sub-paths."},
	{"PERM-004", 2, model.SevLow, false, "No deny fallback list",
		"allow without deny. Add a deny fallback for sensitive paths (~/.ssh, ~/.aws, .env). Raised once per settings scope, not per entry."},
	{"PERM-005", 2, model.SevHigh, false, "Allows arbitrary commands via Bash(*)",
		"Any shell command may run without confirmation — command-layer protection is effectively off."},
	{"PERM-006", 2, model.SevMedium, false, "Grant covers a command that can spawn a shell",
		"The allow covers an everyday binary that reaches arbitrary execution anyway — Bash(git *) via `git -c core.pager=`, and likewise find/awk/tar/docker/ssh/npm/make/xargs/rsync. Same capability as PERM-002 through a narrower-looking disguise, hence the same severity. The grant must be open-ended, so a fully specified Bash(git status) always stays clean. Beyond that, whether pinning a subcommand helps depends on the binary, and this was measured rather than assumed: git parses `git log -c k=v` as a revision, so Bash(git status:*) is genuinely out of reach and stays clean, but make, find, rsync, ssh and vim accept their escape option ANYWHERE in the command line, so Bash(make test *) is as open as Bash(make *) and is reported."},
}

// llm documents internal/judge's findings. All are Source=llm and advisory: they land only in
// `overall_effective`, never in `overall`, and `--fail-on` cannot see them. Hand-maintained.
var llm = []entry{
	{"LLM-001", 10, model.SevMedium, true, "Intent mismatch",
		"The declared purpose and what the scripts actually do do not line up. Severity is the model's own, clamped to at most high."},
	{"LLM-003", 1, model.SevMedium, true, "Hidden prompt injection",
		"A directive aimed at the agent, phrased to dodge keyword rules. Runs on skills, CLAUDE.md, subagents, slash commands and hooks."},
	{"LLM-004", 6, model.SevMedium, true, "Decoded obfuscated payload",
		"An embedded base64/hex blob was decoded — never executed — and the model was asked what it does."},
	{"LLM-006", 3, model.SevMedium, true, "Cross-file capability chain",
		"Different files of one artifact collect and send between them."},
	{"LLM-007", 1, model.SevHigh, true, "Artifact tried to instruct the analyzer",
		"While being examined, the content addressed the analysis model — telling it what to conclude, or to ignore its instructions. Legitimate content has no reason to talk to a scanner. Unlike every other verdict the SEVERITY IS THE TOOL'S, not the model's: a hijacked model would rate its own capture low."},
	{"LLM-008", 2, model.SevMedium, true, "Hook capability exceeds its interception point",
		"The hook command does more than intercepting its own event requires."},
	{"LLM-009", 5, model.SevMedium, true, "MCP server configuration risk",
		"Unpinned package, unknown publisher, remote endpoint, or credentials passed through env. Advisory only: reported with the model's severity but never escalates, whatever the vote — on 500 real configs it was the only judge rule to flag benign input."},
}

// notes documents the dimension-0 disclosures. They never score and never gate; they exist so
// that no omission is silent (invariant #5). Hand-maintained.
var notes = []entry{
	{"COV-000", 0, "", false, "Coverage gap",
		"Something was not read: a file over the size limit, an unknown extension whose content sniffs as text, an excluded generated directory nobody references, a hook script outside HOME, unowned entries under a root, a named pipe / socket / device node (unbounded to read, so never opened), or a root that collected nothing at all."},
	{"IO-000", 0, "", false, "Path unreadable",
		"The location exists but could not be read (permission or I/O error). Surfaced rather than skipped, so a missed scan cannot masquerade as clean."},
	{"PARSE-000", 0, "", false, "Unparseable configuration",
		"A settings/manifest file parsed into nothing usable. Reported, because 'no hooks found' and 'the hooks block was malformed' are different facts."},
	{"SCOPE-001", 0, model.SevMedium, false, "Symlink escapes its boundary",
		"A skill's internal file, a plugin installPath, or a hook script resolved outside the allowed root and was NOT read. Resolution failures are refusals (invariant #2, fail-closed)."},
	{"IGN-000", 0, "", false, "Findings suppressed by baseline",
		"A .aguardignore file suppressed findings BEFORE scoring. Carries the highest severity it silenced — a baseline that hides a critical must not read as a low footnote."},
	{"REP-GOOD", 0, "", false, "Findings suppressed as known-trusted",
		"The artifact's hash is on the embedded allowlist, so its scoring findings were dropped to cut trusted-tool noise. Also carries the highest suppressed severity."},
	{"LLM-000", 0, "", false, "LLM judge unavailable or incomplete",
		"The judge was requested but could not complete (endpoint error, timeout, unparseable reply). Announced, so an absent opinion is never mistaken for a clean one."},
	{"LLM-002", 0, model.SevMedium, false, "LLM endpoint is not local",
		"base_url points somewhere other than loopback, so redacted excerpts leave the machine. A privacy warning, not a risk finding about the artifact."},
	{"LLM-005", 0, model.SevLow, false, "LLM verdicts discarded as ungrounded",
		"The model returned verdicts whose quoted evidence could not be located in the text that was actually sent, so they were dropped and counted here. A confident hallucination never reaches the report — but the fact that one occurred does."},
}

// gateNotes documents the load-time gate's messages (internal/gate). They are not findings
// in a scan result — they reach the operator through a hook reply — but they carry a rule ID
// for the same reason everything else here does: a message an operator sees must be
// lookup-able in the reference, and a hand-listed ID that no test enforces is one that
// goes stale.
var gateNotes = []entry{
	{"GATE-000", 0, "", false, "Load-time gate could not audit something",
		"The gate ran but produced no verdict: the skill name did not resolve to a directory, the scan failed, the target collected nothing, or the approvals store could not be read or written. The load is ALLOWED anyway and this says so — a gate that blocks when its own scanner breaks is one that gets uninstalled, so the failure is announced rather than enforced (gate invariant #3). Nothing about the artifact is claimed either way."},
	{"GATE-001", 0, model.SevMedium, false,
		"Load-time gate is registered but cannot run",
		"settings.json registers the gate, but the command it names is gone — the binary moved, a build directory was cleaned, a dotfile repo landed on a machine that never had it. Claude Code then runs nothing at those interception points: every skill loads unaudited, and the silence looks exactly like a clean result. Raised by `scan` rather than left to `aguard hook status`, because nobody runs a status command on a schedule. Dimension 0: a dead hook makes the REPORT less trustworthy, it does not make any artifact more dangerous."},
}

// scanGateNote is the one gate message a scan result carries: `scan` raises it from
// internal/gate/status.go, so it is deterministic output the rules epoch covers. GATE-000 is a hook
// reply that no report holds, so the header claims nothing about it either way.
const scanGateNote = "GATE-001"

// allDocumented is every hand-maintained group, for the drift test.
func allDocumented() []entry {
	out := append([]entry{}, structural...)
	out = append(out, permission...)
	out = append(out, llm...)
	out = append(out, notes...)
	return append(out, gateNotes...)
}

type entry struct {
	id       string
	dim      int
	sev      model.Severity
	advisory bool
	title    string
	why      string
}

func main() {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	// Read before the header is written: the header publishes the counts, and a count that
	// is derived here rather than typed cannot disagree with the tables below it.
	rules := detect.Rules()
	scoring := 0
	for _, r := range rules {
		if r.Dimension != 0 {
			scoring++
		}
	}
	for _, e := range allDocumented() {
		if e.dim != 0 {
			scoring++
		}
	}
	total := len(rules) + len(allDocumented())

	p("<!-- Generated by hack/gen-rules. Do not edit by hand: run `make docs`. -->\n")
	p("# Rule reference\n\n")
	p("Every finding AgentGuard can emit, with the dimension it scores in and why it fires.\n")
	p("Look up the rule ID printed in your report.\n\n")
	p("This page is **generated from the engine's own rule set** (`make docs`), and CI fails if\n")
	p("it drifts from the code — so a severity here is the severity that will gate your build.\n")
	p("It is English-only, unlike the rest of `docs/`: the text comes from the code, and the\n")
	p("code's user-visible strings are English by convention. A hand-translated copy could not\n")
	p("be kept honest by the drift check.\n\n")
	p("**Scoring, in one line:** within a dimension only the highest-severity hit counts;\n")
	p("penalties add across dimensions (critical 40 · high 25 · medium 12 · low 5). Dimension 0\n")
	p("never scores. See [architecture.md](architecture.md#scoring) for the full formula.\n\n")
	p("**Advisory** means static analysis cannot confirm it — the report says so, and you should\n")
	p("read the evidence before acting.\n\n")

	// The one authoritative count. Everything else in this repository that states how many
	// rules there are is a copy, and a copy goes stale silently: a planning document said
	// "57 rule IDs across 16 families" for three months while the real numbers were 72 and
	// 19, and four other documents inherited it. ROADMAP.md declines to write counts down at
	// all for exactly this reason; this line is the place it can point at instead.
	p("**%d rule IDs** in this build — %d engine rules + %d structural + %d permission + %d LLM\n",
		total, len(rules), len(structural), len(permission), len(llm))
	p("+ %d scan notes + %d gate messages. Of these, **%d score** and %d never do (dimension 0).\n",
		len(notes), len(gateNotes), scoring, total-scoring)
	p("These counts are generated from the same tables the drift check reads, so if a count\n")
	p("anywhere else in this repository disagrees with this line, that other count is stale.\n\n")

	writeRulesVersion(p, rules)

	byDim := map[int][]entry{}
	for _, r := range rules {
		byDim[r.Dimension] = append(byDim[r.Dimension], entry{
			id: r.ID, dim: r.Dimension, sev: r.Severity, advisory: r.Advisory,
			title: r.Title, why: r.Why + hookOnlyNote(r),
		})
	}
	for _, e := range structural {
		byDim[e.dim] = append(byDim[e.dim], e)
	}
	for _, e := range permission {
		byDim[e.dim] = append(byDim[e.dim], e)
	}
	for _, e := range llm {
		byDim[e.dim] = append(byDim[e.dim], e)
	}

	p("## Contents\n\n")
	for d := 1; d <= model.DimensionCount; d++ {
		if len(byDim[d]) == 0 {
			continue
		}
		p("- [%d — %s](#%d--%s) (%d)\n", d, model.DimensionName(d), d,
			strings.ToLower(strings.ReplaceAll(model.DimensionName(d), " ", "-")), len(byDim[d]))
	}
	p("- [Scan notes (dimension 0)](#scan-notes-dimension-0) (%d)\n", len(notes))
	p("- [Load-time gate](#load-time-gate) (%d)\n", len(gateNotes))
	p("- [Not covered by any rule](#not-covered-by-any-rule)\n\n")

	for d := 1; d <= model.DimensionCount; d++ {
		es := byDim[d]
		if len(es) == 0 {
			continue
		}
		sort.Slice(es, func(i, j int) bool { return es[i].id < es[j].id })
		p("## %d — %s\n\n", d, model.DimensionName(d))
		if d == 10 {
			p("Reachable only with the optional LLM judge (`--llm`), which never moves `overall`\n")
			p("and never trips `--fail-on`.\n\n")
		}
		table(p, es)
	}

	p("## Scan notes (dimension 0)\n\n")
	p("These are the scan describing itself, not findings about an artifact. They never score\n")
	p("and never gate. A severity on one mirrors the highest severity it suppressed or the\n")
	p("seriousness of the gap, so a silenced critical cannot read as a footnote.\n\n")
	table(p, notes)

	p("## Load-time gate\n\n")
	p("Messages from `aguard hook`, the gate that audits a skill before an agent loads it.\n")
	p("They reach the operator through a hook reply rather than a scan report, so they are not\n")
	p("findings about an artifact and never score. They are listed here because an operator who\n")
	p("sees one needs somewhere to look it up.\n\n")
	table(p, gateNotes)

	p("## Not covered by any rule\n\n")
	p("Stated because a gap you know about is a different thing from a gap you do not:\n\n")
	p("- Contents of generated/vendored directories (`dist/`, `node_modules/`, …) — excluded to\n")
	p("  keep the canonical hash stable across rebuilds; `SUP-004` covers being pointed into one.\n")
	p("- Files whose extension is not in the reader's allowlist — announced per artifact as `COV-000`.\n")
	p("- Command names spelled with Unicode homoglyphs (a Cyrillic `с` for a Latin `c`). Invisible\n")
	p("  characters are stripped before matching; folding confusables needs a table that is not\n")
	p("  compiled in.\n")
	p("- Chained expressions (`Buffer.from(secret).toString('base64')`) and following a value\n")
	p("  across statements — both need a syntax tree, see the AST entry in `ROADMAP.md`.\n\n")
	p("The full, current list with rationale lives in `ROADMAP.md` under \"Known limitations\",\n")
	p("where each confirmed evasion is also asserted inverted in `cmd/aguard/adversarial_test.go`\n")
	p("so that closing it fails a test.\n")

	out := "docs/rules.md"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-rules:", err)
		os.Exit(1)
	}
	fmt.Printf("gen-rules: wrote %s (%d engine rules + %d structural + %d permission + %d llm + %d notes + %d gate = %d IDs)\n",
		out, len(rules), len(structural), len(permission), len(llm), len(notes), len(gateNotes),
		len(rules)+len(allDocumented()))
}

func hookOnlyNote(r detect.Rule) string {
	if !r.HookOnly {
		return ""
	}
	return " Runs on hook commands only: unremarkable in a script, telling in a hook."
}

// writeRulesVersion writes the header's rules-version paragraph, so a report can be matched to the
// table that produced it. It lives in the header and not only in reports because the drift check
// then covers it: a pattern change that leaves every title alone still changes this line, so it
// cannot ship without `make docs`.
//
// The wording says only what the hash guarantees; internal/detect's rulesEpoch says what it
// cannot. Each ID a report can carry is stated once, in one of three classes: the engine rules are
// hashed; the rest of deterministic detection is built inline and rides on the epoch alone; every
// LLM- ID is outside, since the version is for recomputing overall, which the judge cannot move
// (spec §5.1). The notes are split by prefix because "scan notes" on the epoch and "LLM- notes"
// outside it, in two sentences, read as a contradiction. Nothing else is offered for the judge:
// only tool_version pins its code today. GATE-000 is left out on purpose — no report carries it.
// TestRulesDocHeaderSaysWhatTheHashCovers checks that the classes partition the page.
func writeRulesVersion(p func(string, ...any), rules []detect.Rule) {
	epochNotes, llmNotes := 0, 0
	for _, e := range notes {
		if strings.HasPrefix(e.id, "LLM-") {
			llmNotes++
		} else {
			epochNotes++
		}
	}
	p("**Rules version `%s`** — reports from this build carry it as `rules_version` (`--json`),\n",
		detect.RulesVersion())
	p("and `aguard version` prints it. It covers deterministic detection only. Each ID a report can\n")
	p("carry falls in exactly one class:\n\n")
	p("- **Hashed:** the %d engine rules — each one's ID, dimension, severity, flags and pattern, not\n",
		len(rules))
	p("  the titles and explanations below.\n")
	p("- **Covered only by the epoch:** the %d structural checks, the %d permission checks, the %d\n",
		len(structural), len(permission), epochNotes)
	p("  scan notes that are not `LLM-` IDs, and `%s` (the gate message `scan` raises). The epoch is\n",
		scanGateNote)
	p("  an integer folded into the hash, which the maintainers bump when deterministic detection\n")
	p("  code outside the engine rule table changes; nothing checks that they did.\n")
	p("- **Outside `rules_version` entirely:** every `LLM-` ID — the %d judge findings and the %d\n",
		len(llm), llmNotes)
	p("  `LLM-` scan notes. The judge moves only `overall_effective`, and today a report identifies\n")
	p("  the judge's code only through `tool_version`.\n\n")
	p("Two reports whose rules versions differ were produced by different rules; two whose versions\n")
	p("agree were produced by the same engine rule table.\n\n")
}

func table(p func(string, ...any), es []entry) {
	p("| ID | Severity | Title | Why it fires |\n|---|---|---|---|\n")
	for _, e := range es {
		sev := string(e.sev)
		if sev == "" {
			sev = "note"
		}
		if e.advisory {
			sev += " · advisory"
		}
		p("| `%s` | %s | %s | %s |\n", e.id, sev, e.title, cell(e.why))
	}
	p("\n")
}

// cell escapes what would otherwise break a markdown table row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}
