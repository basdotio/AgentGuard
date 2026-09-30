// SPDX-License-Identifier: MIT
// Package model holds AgentGuard's immutable result types (spec §8). Every scan
// stage produces these; they are JSON-serializable and carry line-level, redacted
// evidence. ScannedAt is injected by the caller so results are reproducible/testable.
package model

// Severity ranks a finding. Ordering (low<medium<high<critical) is used by scoring
// and by the --fail-on gate.
type Severity string

const (
	SevLow      Severity = "low"
	SevMedium   Severity = "medium"
	SevHigh     Severity = "high"
	SevCritical Severity = "critical"
)

// Rank returns a monotonic integer for comparisons (higher = worse).
func (s Severity) Rank() int {
	switch s {
	case SevLow:
		return 1
	case SevMedium:
		return 2
	case SevHigh:
		return 3
	case SevCritical:
		return 4
	}
	return 0
}

// ArtifactKind identifies what was scanned (spec §4).
type ArtifactKind string

const (
	KindSkill       ArtifactKind = "skill"
	KindMCP         ArtifactKind = "mcp"
	KindHook        ArtifactKind = "hook"
	KindPermission  ArtifactKind = "permission"
	KindSubagent    ArtifactKind = "subagent"
	KindCommand     ArtifactKind = "command"
	KindPlugin      ArtifactKind = "plugin"
	KindInstruction ArtifactKind = "instruction"
	// The four kinds below are all content Claude Code loads WITHOUT being asked — at session
	// start, or straight into a system prompt — which makes each one an instruction surface with
	// the same standing as CLAUDE.md. They were collected by nothing until now, so a payload
	// parked in any of them scored a clean environment.
	//
	// KindRule is a file under rules/ (discovered recursively). One without `paths:` frontmatter
	// loads every session at the same priority as .claude/CLAUDE.md.
	KindRule ArtifactKind = "rule"
	// KindWorkflow is a file under workflows/. MEASURED CORRECTION: unlike rules/ and a selected
	// output style, a workflow body does NOT enter context at startup. On 2.1.229 the directory is
	// enumerated (so /<name> completes) while the file itself was never opened until invoked.
	// Collected regardless, and grouped apart from the auto-loaded surfaces in the report: a
	// workflow orchestrates subagents, so its body is an execution surface, and "loads only when
	// called" is thin mitigation when the call is one slash-command away.
	KindWorkflow ArtifactKind = "workflow"
	// KindOutputStyle is a file under output-styles/. A selected style is injected as a section
	// of the SYSTEM PROMPT, which is the most privileged position any scanned content can hold.
	KindOutputStyle ArtifactKind = "output_style"
	// KindQuarantined is content sitting in <root>/.aguard-trash — something `clean --apply` moved
	// out of the way. It is scanned and SCORED like anything else, because it is still on disk
	// inside the config root: "I moved the malicious skill to another folder" is not "my environment
	// is clean", and a scan that said otherwise would let a cleanup command launder a failing score.
	// Its own report group makes clear it is no longer loaded.
	KindQuarantined ArtifactKind = "quarantined"
	// KindMemory is an auto-memory file (projects/<project>/memory/, agent-memory/<agent>/).
	// Distinct from the others because Claude WRITES these itself: an injection that gets one line
	// into MEMORY.md is loaded into every later session, turning a one-shot into persistence.
	KindMemory ArtifactKind = "memory"
	// KindDirectory is a directory whose layout matched nothing known — produced only by
	// `check <dir>`, never by a root scan. It exists so an unrecognized layout is scanned as
	// a WHOLE TREE instead of being routed to the root collectors, which look only for known
	// sub-layouts and therefore reported a bare directory of scripts as clean.
	KindDirectory ArtifactKind = "directory"
	// KindConnector is a remote MCP server attached to the user's Claude account (the app's
	// Connectors tab: Figma, Notion, Slack, …). The server is remote and never contacted; what
	// is collected is the TOOL LIST it advertised — every tool's name, description and parameter
	// descriptions — as Claude Desktop cached it in a local session file. Those descriptions are
	// text the model reads and acts on in every session the connector is attached to, on this
	// machine and in the cloud sandbox alike, and the server can change them at will: the
	// tool-poisoning surface. One artifact per connector, hashed over the tool list.
	KindConnector ArtifactKind = "connector"
)

// Source marks which stage produced a finding.
type Source string

const (
	SrcStatic     Source = "static"
	SrcLLM        Source = "llm"
	SrcHygiene    Source = "hygiene"
	SrcPermission Source = "permission"
	SrcParseError Source = "parse_error"
)

// Evidence is one redacted, line-anchored citation for a finding. Secret VALUES are
// never stored here — only key names / positions (spec §16.3).
type Evidence struct {
	File    string `json:"file"`            // path relative to root
	Line    int    `json:"line"`            // 1-based; 0 = whole-artifact
	Snippet string `json:"snippet"`         // triggering text, secrets already <REDACTED>
	Scope   string `json:"scope,omitempty"` // config source scope (spec §4), e.g. "user" / "project"
}

// Finding is one issue against a threat dimension (1..10, spec §3).
type Finding struct {
	RuleID    string     `json:"rule_id"`
	Dimension int        `json:"dimension"`
	Severity  Severity   `json:"severity"`
	Title     string     `json:"title"`
	Why       string     `json:"why"`
	Evidence  []Evidence `json:"evidence"`
	Source    Source     `json:"source"`
	// Advisory marks dimensions where static detection can only hint, not confirm
	// (backdoor / resource-abuse — spec §16.6). UI must show "advisory: suspicious surface, not confirmed".
	Advisory bool `json:"advisory,omitempty"`
	// Escalates marks an LLM finding that cleared every precondition for affecting the
	// EFFECTIVE score (spec §5.2.1): its evidence was located in what was sent, and enough
	// independent samples agreed. A finding that fails a precondition is still reported —
	// "the model wasn't consistent about it" is not the same as "it isn't there" — it simply
	// carries no weight. Deterministic findings never set this: they are not escalating
	// anything, they ARE the score.
	Escalates bool `json:"escalates,omitempty"`
}

// Hook is ONE hook from a settings file (spec §4: matcher + command, or type=http + url).
// Hook artifacts are collected per command OR per HTTP target, not per event, so a finding
// names the exact entry that carries it and later stages can weigh that entry against the
// interception point that triggers it.
//
// These fields are scan-internal INPUT, not a result: they hold raw config text, so they
// are never serialized. Only redacted evidence reaches a report (spec §16.3).
type Hook struct {
	Event   string // PreToolUse | PostToolUse | PermissionRequest | …
	Matcher string // tool matcher the hook is scoped to ("" = every tool)
	Command string // the shell command line (command-type hooks)
	// Type is the Claude Code hook type. "" and "command" are the shell form; "http" posts
	// the event payload to URL instead. An HTTP hook has no command to follow, so URL is
	// the scannable target (HOOK-003).
	Type string
	URL  string // http hook destination; empty for command hooks
	// OwnerRoot is the tree this hook SHIPS IN — a plugin's resolved install directory —
	// and is empty for a hook declared in the user's own settings.json.
	//
	// It exists because a plugin hook names its second stage through the plugin root, and
	// almost never by a path a scanner can resolve: `_R="${CLAUDE_PLUGIN_ROOT}"; node
	// "$_R/scripts/x.js"`. Expanding $_R would mean interpreting shell, which this tool does
	// not do — but asking "does scripts/x.js exist inside the tree this hook came from" is a
	// filesystem fact about an already-contained directory, not a guess. Without it a real
	// machine produced 54 warnings saying a second stage went unread, for files the same scan
	// had already read and raised EXEC-001 on.
	OwnerRoot string
}

// ArtifactReport is one scanned artifact plus its findings and score.
type ArtifactReport struct {
	Kind  ArtifactKind `json:"kind"`
	Name  string       `json:"name"`
	Path  string       `json:"path"`
	Hash  string       `json:"hash"` // canonical: skill=tree hash, single-file=sha256 (spec §8)
	Score int          `json:"score"`
	// ScoreEffective is Score with qualified LLM findings folded in — always ≤ Score, and
	// equal to it when the judge didn't run (spec §5.3).
	ScoreEffective int       `json:"score_effective"`
	Findings       []Finding `json:"findings"`
	// Hook is set for KindHook artifacts only: the interception point plus the command
	// or HTTP URL this artifact stands for. Never serialized — see Hook.
	Hook Hook `json:"-"`
	// Connector is set for KindConnector artifacts only: the advertised tool list. Scan input,
	// never serialized — the report carries findings about it, not a copy of it.
	Connector *Connector `json:"-"`
	// Advisory carries DISPLAY-ONLY annotations (e.g. LLM triage labels, spec §5.2.1). They
	// reference findings by RuleID but NEVER alter them — findings and Score are computed
	// only from Findings. Advisory annotations can't move the score or the --fail-on gate.
	Advisory []AdvisoryLabel `json:"advisory,omitempty"`
	// Reputation is set when the artifact's canonical hash matched the embedded reputation
	// list. It is how a 100-score artifact says "trusted" rather than "clean": with a GOOD
	// match its findings are suppressed and it is otherwise indistinguishable, in JSON and
	// HTML, from an artifact that never had any. Audit metadata only — the verdict's effect on
	// Findings and Score is applied once, in cmd/aguard.applyReputation, and the REP-GOOD /
	// REP-BAD note is still the record a reader is meant to see first.
	Reputation *ReputationMark `json:"reputation,omitempty"`
}

// ReputationMark names the reputation entry an artifact matched and what the match did.
type ReputationMark struct {
	Verdict    string `json:"verdict"` // "good" | "malicious"
	Entry      string `json:"entry"`   // the entry's name, e.g. "superpowers"
	Publisher  string `json:"publisher,omitempty"`
	Version    string `json:"version,omitempty"`
	Source     string `json:"source,omitempty"` // git URL the entry was reviewed from
	SHA        string `json:"sha,omitempty"`
	Path       string `json:"path,omitempty"`
	Reviewed   string `json:"reviewed,omitempty"`
	Suppressed int    `json:"suppressed,omitempty"` // scoring findings removed by a GOOD match
}

// Triage label values (LLM advisory). Unknown model output normalizes to LabelReal — the
// safe side, so a slip can never silently mark a genuine finding benign.
const (
	LabelReal   = "likely-real"
	LabelBenign = "likely-benign"
)

// AdvisoryLabel is one LLM triage annotation for an artifact's findings of a given rule.
// It is advisory context only: the labeled finding still renders and still scores/gates.
type AdvisoryLabel struct {
	RuleID string `json:"rule_id"`
	Label  string `json:"label"` // LabelReal | LabelBenign
	Reason string `json:"reason"`
}

// Tier ranks a cleanup item by HOW it may be executed, not by how bad it is. The split is
// two-dimensional on purpose: reversibility alone is not a licence to batch-execute, because a
// reversible action on a weak judgement still costs the operator a pile of undo work. So a tier
// encodes both "can this be taken back" and "does a human have to choose something first".
//
// Only TierAuto may be executed without the operator naming the item, and TierAuto additionally
// requires ConfHigh — see the Actionable/Blockers contract below.
type Tier string

const (
	// TierAuto is a reversible move whose target is unambiguous and whose judgement is strong
	// enough to batch. This is the ONLY tier "clean everything" may touch.
	TierAuto Tier = "A1"
	// TierChoice is a reversible move where the operator must first choose WHICH side to act on
	// (the duplicate-pair case). Never batchable: there is no safe default, and inventing one is
	// how a cleanup tool deletes the wrong half of a pair.
	TierChoice Tier = "A2"
	// TierConfig removes an entry from a config file. Reversible only via a recorded backup.
	TierConfig Tier = "B"
	// TierContent deletes lines inside a file. Reversible only via a recorded backup, and the
	// "should this go" question is a judgement about someone's own writing.
	TierContent Tier = "C"
)

// Confidence is how much the DETECTION is worth, kept separate from severity and from tier.
// It gates TierAuto: a reversible action on a low-confidence signal is still a bad default.
type Confidence string

const (
	ConfLow    Confidence = "low"
	ConfMedium Confidence = "medium"
	ConfHigh   Confidence = "high"
)

// Action names the executor a cleanup item needs. ActionNone marks an item that is reported for
// the operator to act on by hand — "we found this" without "and we can fix it".
type Action string

const (
	ActionNone         Action = ""
	ActionMove         Action = "move"          // relocate into <root>/.aguard-trash
	ActionConfigRemove Action = "config-remove" // delete one entry from a config file
	ActionLineDelete   Action = "line-delete"   // delete named lines inside a file
)

// Locator addresses ONE cleanup target precisely enough to act on it, and stably enough to name
// it in a command line across runs.
//
// Path is deliberately ROOT-RELATIVE with forward slashes. Artifact paths are absolute and
// symlink-resolved, and hashing those would make an item's ID depend on where the root happens to
// live — `--root ~/.claude` and `--root /home/u/.claude` would address the same skill by two
// different IDs, and a copied command line would silently miss.
type Locator struct {
	Path string `json:"path"`           // root-relative, slash-separated
	Name string `json:"name,omitempty"` // display label; NEVER part of the ID (attacker-chosen)
	// Entry addresses a position INSIDE Path — an MCP server name, a JSON pointer, a line-range
	// anchor. Empty means the whole file/directory is the target.
	Entry string `json:"entry,omitempty"`
	// Hash is the target's content hash when the item was produced, carried so an executor can
	// refuse to act on something that changed while the operator was reading the list. It is NOT
	// part of the ID: editing a file must not renumber the list.
	Hash string `json:"hash,omitempty"`
}

// Key returns the ID-stable identity of a target: what is addressed, never how it is labelled.
func (l Locator) Key() string { return l.Path + "\x1f" + l.Entry }

// CleanItem is ONE addressable cleanup candidate (spec §6). Deterministic, LLM-free.
//
// This used to be an aggregate — every zombie in one finding's Targets, every bloated skill in
// another — which reads fine and cannot be acted on: there is no way to say "that one, not the
// other four". Items are now one-per-decision, which is what makes selection, scripting, and a
// stable ID possible at all.
type CleanItem struct {
	// ID is stable across runs for the same target set and unique within one listing. Empty for
	// items that address nothing (a notice explaining why a check could not run).
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind"` // context_bloat | stale_ref | zombie | duplicate_fn
	Tier Tier   `json:"tier,omitempty"`
	// Actionable reports whether this item names an action on a concrete target. It says nothing
	// about whether that action can run RIGHT NOW — see Blockers. An executor must require both.
	Actionable bool       `json:"actionable"`
	Confidence Confidence `json:"confidence,omitempty"`
	Action     Action     `json:"action,omitempty"`
	// Targets are display names, kept as a plain string list so a report can print an item
	// without understanding locators.
	Targets  []string  `json:"targets"`
	Locators []Locator `json:"locators,omitempty"`
	Detail   string    `json:"detail"`
	// Hint is HOW TO ANSWER this item — a command line the operator can copy. Separate from Detail
	// because the two have different audiences: a listing needs both, while an interactive prompt
	// that is already asking the question needs only Detail. Printing a "run this command" line
	// inside the prompt that is collecting the answer is noise at best, and reads as an instruction
	// from the tool at worst.
	Hint string `json:"hint,omitempty"`
	// ReclaimTokens is the estimated context tokens recoverable BY THIS ITEM (context_bloat).
	// Callers that want a total add them up; the aggregate no longer hides inside one finding.
	ReclaimTokens int `json:"reclaim_tokens,omitempty"`
	// Blockers name every reason this item may not be executed yet — an unimplemented executor,
	// a target outside root, a reference held elsewhere. A blocked item is still REPORTED, since
	// "we cannot safely move this" is information the operator wants; it is simply not offered.
	Blockers []string `json:"blockers,omitempty"`
}

// Blocker reasons. They live here rather than in the package that produces them because they cross
// package boundaries: hygiene writes them, clean reads them, the report prints them, and anything
// consuming `clean --json` parses them. One definition, so the producer and the consumer cannot
// drift into disagreeing about what a blocker is called.
const (
	// BlockerOutsideRoot: the target resolves outside root (symlink-installed). Moving it would
	// reach out of the tree the operator pointed us at, so it is reported and never offered.
	BlockerOutsideRoot = "target-outside-root"
	// BlockerContentEdit: deleting lines inside a file has no executor yet. The item is real and
	// its token estimate is real; only the automatic fix is missing.
	BlockerContentEdit = "content-edit-unimplemented"
	// BlockerNotUnderSkills: the target resolves inside root but outside skills/, which is the only
	// surface clean owns. Reached through a symlink — skills/x -> shared/x, or into a plugin's own
	// tree — where moving the target would take content out of a surface this command does not own.
	BlockerNotUnderSkills = "target-not-under-skills"
	// BlockerProtected: the target is security configuration, which is never moved.
	BlockerProtected = "target-is-security-config"
	// BlockerAlreadyQuarantined: the target already sits in the trash directory.
	BlockerAlreadyQuarantined = "already-quarantined"
	// BlockerUnlocatable: the target cannot be expressed relative to root, so no decision about it
	// can be trusted.
	BlockerUnlocatable = "target-unlocatable"
	// BlockerSameTarget: the two sides of a pair resolve to ONE directory on disk — an alias
	// install, skills/alias -> skills/real. There is no side to drop: quarantining "the other one"
	// moves the very directory the survivor points at, and every downstream check agrees to it,
	// because the content hash of the two sides is the same tree.
	BlockerSameTarget = "pair-shares-one-target"
	// BlockerSideSelection: acting on a pair needs the operator to say which side survives.
	//
	// It used to read "side-selection-unimplemented", which stopped being true once clean.Resolve
	// landed. The item is still blocked, but for a permanent reason rather than a temporary one:
	// choosing IS the decision, so no batch may take it and no default may be invented. Naming a
	// standing requirement after a missing feature invites someone to "finish" it by picking a side.
	BlockerSideSelection = "side-selection-required"
)

// AnswerBlocker returns a copy of the item with one blocker removed, for the case where the
// operator has supplied exactly the thing that blocker was asking for.
//
// This exists so an executor never needs a private path around Executable(). clean.Resolve answers
// BlockerSideSelection with the operator's --keep and then asks Executable() like everything else,
// which means a target-outside-root pair is still refused. One gate, not two that must be kept in
// step.
func (c CleanItem) AnswerBlocker(reason string) CleanItem {
	out := c
	out.Blockers = nil
	for _, b := range c.Blockers {
		if b != reason {
			out.Blockers = append(out.Blockers, b)
		}
	}
	return out
}

// Executable reports whether an executor may act on this item: it must name an action AND have
// nothing standing in the way. Scoring-style single-definition rule — the CLI, the report and any
// future apply path all ask this, so the three cannot drift into disagreeing about what is safe.
func (c CleanItem) Executable() bool {
	return c.Actionable && c.Action != ActionNone && len(c.Blockers) == 0
}

// UnattendedSafe reports whether a run that did NOT have the operator name this item may still
// take it. The name says "unattended" rather than "batchable" because that is the only situation
// it governs, and because there is NO SUCH COMMAND TODAY.
//
// It is NOT the gate `clean --apply` uses. That gate is Executable(), and it is the right one
// there: --apply requires the operator to name the class (--zombie) and the action (--apply), so
// the run is attended by definition. Anyone reading this predicate and concluding that --apply
// cannot move a low-confidence zombie is reading the wrong gate — it can, and that is intended.
//
// This predicate is currently UNREACHABLE, which is a stronger claim than "no item happens to
// qualify". Its two conditions are satisfied by DISJOINT SETS:
//
//   - TierAuto is produced only by the zombie check, whose confidence comes from
//     usageSource.confidence() and is capped at medium BY DESIGN — one machine's history cannot
//     establish that a skill is unused anywhere.
//   - ConfHigh is produced only by context_bloat, which is TierContent and additionally carries an
//     unimplemented-executor blocker, so it fails Executable() too.
//
// Making this reachable therefore means moving that confidence ceiling, which is a deliberate act
// rather than something that can drift in. hygiene.TestConfidenceCeiling_KeepsUnattendedUnreachable
// is an inverted assertion that fails the moment it happens.
func (c CleanItem) UnattendedSafe() bool {
	return c.Executable() && c.Tier == TierAuto && c.Confidence == ConfHigh
}

// CleanPlanSchema is the version of the `clean --json` envelope. Bump it whenever a consumer that
// read the previous version could misread the new one — a removed field, a changed field meaning,
// or a changed enum VALUE (the blocker strings below are part of this contract).
//
// It exists because the previous shape was a BARE ARRAY, which had nowhere to say anything about
// itself. Renaming a blocker string was therefore a silent break by construction, and so would the
// next change have been. `scan --json` never had this problem: it is an object and already carries
// tool_version.
const CleanPlanSchema = 1

// CleanPlan is what `clean --json` emits. The envelope is the point; Items alone was the old shape.
type CleanPlan struct {
	Schema      int         `json:"schema"`
	ToolVersion string      `json:"tool_version"`
	Root        string      `json:"root"`
	Items       []CleanItem `json:"items"`
}

// EnvSummary is the environment overview (counts).
type EnvSummary struct {
	Skills     int `json:"skills"`
	MCPServers int `json:"mcp_servers"`
	// Hooks counts hook COMMANDS (event × matcher × command), not events: one event can
	// register many commands and each is an independent execution surface.
	Hooks       int `json:"hooks"`
	Permissions int `json:"permissions"`
	Subagents   int `json:"subagents"`
	Commands    int `json:"commands"`
	Plugins     int `json:"plugins"`
	// Counts for the surfaces that load without being asked. Reported separately rather than
	// folded into Instructions, because "you have 14 rules loading every session" is a fact an
	// operator reacts to differently than "you have a CLAUDE.md".
	Rules        int `json:"rules"`
	Workflows    int `json:"workflows"`
	OutputStyles int `json:"output_styles"`
	Memories     int `json:"memories"`
	// Connectors counts remote MCP servers attached to the account, as seen in Claude
	// Desktop's session cache (KindConnector). Distinct from MCPServers, which are the ones
	// configured in files on this machine.
	Connectors  int `json:"connectors"`
	Quarantined int `json:"quarantined"`
	// BundledSkills counts SKILL.md files that live INSIDE plugins. Their content is scanned as
	// part of the plugin tree (one artifact, one hash, one score), so they are not in Skills —
	// and an inventory that said `skills=0 plugins=1` about a plugin holding three skills sent
	// a reviewer to publish "it does not recurse". Counted here so the line can say
	// what was read without changing how it is attributed.
	BundledSkills int `json:"bundled_skills"`
}

// ScanResult is the full immutable output of a scan (spec §8). Serialized by --json;
// consumed by the report layer.
type ScanResult struct {
	Root        string           `json:"root"`
	ScannedAt   int64            `json:"scanned_at"` // Unix sec, injected by caller
	ToolVersion string           `json:"tool_version"`
	Env         EnvSummary       `json:"env"`
	Artifacts   []ArtifactReport `json:"artifacts"`
	// Hygiene carries the cleanup items. The JSON key is unchanged, but the ELEMENT shape gained
	// id/tier/locators and split from aggregate to one-per-decision, so a consumer that read the
	// old array needs updating (spec §6).
	Hygiene []CleanItem `json:"hygiene"`
	// Notes are scan-level warnings (I/O errors, skipped artifacts) — never silently
	// swallowed, so "read 0 → all clear" can't masquerade as a clean environment.
	Notes []Finding `json:"notes"`
	// Overall is the deterministic score (0..100, spec §5.3): reproducible, drives --fail-on,
	// and the ONLY value an attestation may carry — a third party must be able to recompute it
	// from the same content, offline.
	Overall int `json:"overall"`
	// OverallEffective is the same formula including qualified LLM findings. Always ≤ Overall
	// (one-way escalation), equal to it when the judge didn't run, and never a gate: it exists
	// so a human can see what the judge saw without that opinion leaking into the number CI
	// and attestation depend on.
	OverallEffective int `json:"overall_effective"`
	// Judge says what the LLM judge did on THIS run, and is nil when --llm was not passed. A
	// report used to show the judge's findings and nothing else about it, so a judge that ran
	// and found nothing looked exactly like a judge that never ran — the one distinction a
	// reader of a clean report most needs (invariant #5 applied to the judge itself).
	Judge *JudgeSummary `json:"judge,omitempty"`
	// Inbox is the Downloads scan: agent-shaped things found where downloads land, each checked
	// on its own. Nil when no inbox was scanned. Nothing here enters Overall — these items are
	// not loaded by any agent, so they say nothing about the environment; they answer "is this
	// safe to install", one item at a time, before it is installed.
	Inbox *InboxReport `json:"inbox,omitempty"`
	// Locations lists the places the scan looked, and the state each was found in, so a reader
	// can tell "nothing found there" from "that place does not exist on this machine" from "the
	// operator turned it off". Empty for a single-target check.
	Locations []Location `json:"locations,omitempty"`
	// Sandbox is set when the scan ran inside a managed cloud container (Claude Cloud / Cowork)
	// rather than on the user's own machine, with the signals that decided it. It changes no
	// score and hides no finding — it lets the report say the 100/100 describes a throwaway
	// cloud environment, not the reader's computer. Nil on an ordinary local run.
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
}

// SandboxInfo records that a scan ran in a managed cloud container and why the tool thinks so.
type SandboxInfo struct {
	Signals []string `json:"signals"`
}

// Location is one place a scan looks. Status is one of LocRead, LocAbsent, LocOff.
type Location struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

// Location statuses.
const (
	LocRead   = "read"   // present and read
	LocAbsent = "absent" // does not exist on this machine
	LocOff    = "off"    // present or not, the operator disabled it
)

// InboxReport is what the inbox walk found and checked (see internal/inbox).
type InboxReport struct {
	Dir     string      `json:"dir"`
	Items   []InboxItem `json:"items"`
	Skipped int         `json:"skipped"` // entries that were not agent-shaped and were never read
	Notes   []Finding   `json:"notes"`   // dimension-0: caps hit, unreadable dir, archive types not read
	// Judge is the LLM judge's account of its pass over the items — present only when --llm
	// was given, so the section can say whether the deep check looked at these too.
	Judge *JudgeSummary `json:"judge,omitempty"`
}

// InboxItem is one downloaded artifact and the result of checking it in isolation.
type InboxItem struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"` // skill | plugin | marketplace | agent-config | instructions | archive
	Archive  bool      `json:"archive"`
	Hash     string    `json:"hash"`
	Overall  int       `json:"overall"`
	Findings []Finding `json:"findings"`         // scoring findings (dimension > 0), static and LLM
	Notes    []Finding `json:"notes"`            // the item's own coverage notes
	Judged   bool      `json:"judged,omitempty"` // the LLM judge ran over this item
	Error    string    `json:"error,omitempty"`  // could not be checked, and why
}

// JudgeSummary is the LLM judge's own account of a run: whether it ran, over how much, at what
// cost, and — when it did not — why. Findings counts advisory leads it added (Source=llm,
// dimension > 0), never anything about the deterministic score, which it cannot touch.
type JudgeSummary struct {
	Ran       bool   `json:"ran"`
	Reason    string `json:"reason,omitempty"` // why it did not run, or ran short
	Artifacts int    `json:"artifacts"`        // artifacts it was given
	Calls     int    `json:"calls"`            // calls issued (a retry is the same call)
	Failed    int    `json:"failed"`           // calls that ended in error after retries
	Skipped   int    `json:"skipped"`          // calls never issued: budget or deadline
	Findings  int    `json:"findings"`         // advisory leads added
	Endpoint  string `json:"endpoint,omitempty"`
}

// Connector is the tool list a remote MCP server advertised, as cached by Claude Desktop.
type Connector struct {
	UUID  string
	Tools []ConnectorTool
}

// ConnectorTool is one advertised tool: its description and the descriptions of its
// parameters — all of it text the model reads as instructions on how and when to call it.
type ConnectorTool struct {
	Name        string
	Description string
	Params      []ConnectorParam
}

// ConnectorParam is one parameter's name and description from the tool's input schema.
type ConnectorParam struct {
	Name        string
	Description string
}
