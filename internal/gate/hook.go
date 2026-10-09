// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/report"
	"github.com/basdotio/AgentGuard/internal/score"
)

// Hook event names and the tool the gate intercepts. Claude Code exposes nine events; these
// are the three the gate uses. There is deliberately no event for INSTALLATION — see the
// package doc for why loading is the boundary that is both holdable and the one that matters.
const (
	EventPreToolUse   = "PreToolUse"
	EventPostToolUse  = "PostToolUse"
	EventSessionStart = "SessionStart"
	SkillTool         = "Skill"
)

// Permission decisions the gate can return on PreToolUse.
const (
	DecisionAsk  = "ask"  // hand the choice to the operator (default)
	DecisionDeny = "deny" // refuse outright
	// DecisionAllow is defined for completeness and deliberately never returned. Replying
	// "allow" would OVERRIDE the operator's own permission rules for that call, so a gate
	// that approved something would also be quietly widening what else is permitted. When
	// the gate is satisfied it returns no decision at all and the normal rules stand.
	DecisionAllow = "allow"
)

// Event is the hook payload Claude Code writes to the hook's stdin. Only the fields the gate
// reads are declared; unknown ones are ignored, so a future field cannot break the gate.
type Event struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	ToolUseID     string          `json:"tool_use_id"`
	CWD           string          `json:"cwd"`
	// PermissionMode is the mode the session is running under. It decides whether an "ask"
	// is a question or a no-op — see askIsAnswered.
	PermissionMode string `json:"permission_mode"`
	// Source distinguishes a fresh SessionStart from a resume/compact re-entry.
	Source string `json:"source"`
}

// SkillName extracts the skill being loaded from a Skill tool call.
func (e Event) SkillName() string {
	var in struct {
		Skill string `json:"skill"`
	}
	if len(e.ToolInput) == 0 || json.Unmarshal(e.ToolInput, &in) != nil {
		return ""
	}
	return in.Skill
}

// succeeded reports whether a PostToolUse event describes a call that actually ran. A tool
// that errored did not load anything, so nothing about it should be recorded as trusted.
func (e Event) succeeded() bool {
	var r struct {
		Success *bool `json:"success"`
	}
	if len(e.ToolResponse) == 0 || json.Unmarshal(e.ToolResponse, &r) != nil {
		return false
	}
	return r.Success == nil || *r.Success
}

// answeredAutomatically lists the permission modes that answer a prompt WITHOUT showing it.
//
// Under any of these, returning "ask" is not a question — it is a yes, delivered silently.
// The gate therefore refuses instead (see handlePre). This was found on a real machine: a
// skill scoring 51/100 with a credential-exfiltration chain produced an `ask`, the mode
// auto-accepted it, the skill loaded, and NOTHING was shown. A gate whose decision is
// swallowed has failed open in silence, which is the one failure this package does not allow
// itself (invariant #3: fail open, but LOUD).
//
// `default` and `plan` are absent deliberately: both put the question in front of a human,
// which is what "ask" means.
var answeredAutomatically = map[string]bool{
	"auto": true, "acceptEdits": true, "bypassPermissions": true, "dontAsk": true,
}

// askIsAnswered reports whether an "ask" in this session would be resolved without a human
// seeing it. An UNKNOWN or empty mode reads as false: escalating on a mode string this build
// does not recognise would turn every future Claude Code release into a wall of denials.
func askIsAnswered(mode string) bool { return answeredAutomatically[mode] }

// Output is the hook's reply. Every field is optional; an empty Output means "no opinion",
// which is what the gate returns for the overwhelmingly common case of already-approved
// content. Silence on the common path is what keeps the rare message worth reading.
type Output struct {
	HookSpecificOutput *HookSpecific `json:"hookSpecificOutput,omitempty"`
	// SystemMessage is shown to the OPERATOR and does not enter the agent's context.
	SystemMessage string `json:"systemMessage,omitempty"`
}

// HookSpecific carries the per-event reply fields.
type HookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// scanDeadline bounds how long the gate will wait for its own scanner.
//
// This package has no timeout anywhere else, and that was the gap: the gate blocks while it
// audits, so ANY scan that does not return holds the load. A named pipe planted in a skill did
// exactly that (closed in detect.regularFile), but the pipe was one cause, not the property —
// a huge tree, a stalled network filesystem, or the next bug in the reader all end the same
// way. With no deadline the editor's own hook timeout eventually fires and the skill loads
// with NOTHING said: the gate fails open SILENTLY, and silence is the one outcome this package
// does not allow (invariant #3 — fail open, but never quietly).
//
// 30s against a measured ~0.75s for a full scan of a real ~/.claude, i.e. ~40x headroom.
// Generous on purpose: expiring early would report an artifact as unaudited when the scanner
// was merely slow, and "unaudited" is a claim worth being sure of. Raise it before lowering it.
const scanDeadline = 30 * time.Second

// withDeadline runs a scan under scanDeadline, returning an error rather than waiting forever.
// Callers hand that error to the ordinary unaudited() path, so a timeout reaches the operator
// as the same GATE-000 as any other scanner failure — one code path, one message, and no third
// definition of "we could not audit this".
//
// The abandoned goroutine is left running: cancelling the walk would mean threading a context
// through collect and detect, a far larger change than the one this fixes, and the hook process
// exits as soon as it has written its decision, so the goroutine dies with it. The channel is
// buffered for exactly that reason — a send nobody is waiting for must not block a goroutine
// that would then outlive the answer.
// The duration is a parameter rather than read from the const so the tests can drive this in
// milliseconds instead of making the suite wait out a production timeout.
func withDeadline(d time.Duration, fn func() (model.ScanResult, error)) (model.ScanResult, error) {
	return underDeadline(d, "the scan", fn)
}

// resolveDeadline bounds the reads that happen BEFORE the scan: resolving a skill name walks
// the config root and reads installed_plugins.json. Those reads are now refused for non-regular
// files (safeio), so the common hang is gone; the deadline is the backstop for what a guard
// cannot see — a network filesystem that stalls, a directory listing that never returns. Short,
// because nothing here should take long, and because it stacks with scanDeadline.
const resolveDeadline = 10 * time.Second

// underDeadline runs fn and returns its result, or an error once d has passed. The
// three reads that used to sit OUTSIDE withDeadline — resolving the skill name, loading the
// approvals store, loading the config — could each block the gate forever with zero output,
// which is the one outcome this package does not allow (see the comment above scanDeadline).
// `what` names the step in the error so the operator's GATE-000 says which read stalled.
// The abandoned goroutine is not cancelled, for the same reason withDeadline never was: the
// hook process exits right after writing its verdict, and the buffered channel means a late
// send cannot block it.
func underDeadline[T any](d time.Duration, what string, fn func() (T, error)) (T, error) {
	type outcome struct {
		res T
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := fn()
		ch <- outcome{res, err}
	}()
	select {
	case o := <-ch:
		return o.res, o.err
	case <-time.After(d):
		var zero T
		return zero, fmt.Errorf("%s did not finish within %s", what, d)
	}
}

// Scanner audits one target path with the same static path `check` uses.
//
// It is injected rather than imported so this package never depends on the CLI's wiring, and
// so the tests can drive every branch — including the ones that matter most, where the
// scanner fails — without building a filesystem fixture for each.
type Scanner func(path string) (model.ScanResult, error)

// RootScanner audits the whole environment, as `scan` does.
type RootScanner func() (model.ScanResult, error)

// Options configures a gate run.
type Options struct {
	Root        string
	Home        string
	Threshold   model.Severity // findings at or above this level stop a load
	Action      string         // DecisionAsk (default) or DecisionDeny
	ToolVersion string
	Now         func() int64
	Scan        Scanner
	ScanRoot    RootScanner
	Store       *Store
}

func (o Options) now() int64 {
	if o.Now == nil {
		return 0
	}
	return o.Now()
}

func (o Options) action() string {
	if o.Action == DecisionDeny {
		return DecisionDeny
	}
	return DecisionAsk
}

// Handle dispatches one hook event and returns the reply plus whether the store was modified.
//
// It never returns an error. The gate FAILS OPEN AND LOUD (invariant #3): anything that goes
// wrong becomes a GATE-000 message naming what went unaudited, because a hook that blocks
// when its own scanner breaks is a hook that gets deleted, and a deleted hook protects
// nothing. What it must never do is stay quiet — "could not audit" and "audited, it is fine"
// are the two answers this whole tool exists to keep apart.
func Handle(ev Event, o Options) (Output, bool) {
	switch ev.HookEventName {
	case EventPreToolUse:
		if ev.ToolName != SkillTool {
			return Output{}, false
		}
		return handlePre(ev, o)
	case EventPostToolUse:
		if ev.ToolName != SkillTool || !ev.succeeded() {
			return Output{}, false
		}
		return handlePost(ev, o)
	case EventSessionStart:
		return handleSessionStart(ev, o)
	}
	return Output{}, false
}

// handlePre is the load-time gate proper.
func handlePre(ev Event, o Options) (Output, bool) {
	name := ev.SkillName()
	path, err := underDeadline(resolveDeadline, "resolving the skill", func() (string, error) {
		return ResolveSkill(o.Root, o.Home, ev.CWD, name)
	})
	if err != nil {
		return unaudited(name, err), false
	}
	res, serr := withDeadline(scanDeadline, func() (model.ScanResult, error) { return o.Scan(path) })
	if serr != nil {
		return unaudited(name, serr), false
	}
	v, ok := Summarize(res, o.Threshold)
	if !ok {
		// An empty result is the failure mode `check <typo>` used to have: nothing was read,
		// and nothing read renders as a perfect score. It is announced, never passed.
		return unaudited(name, fmt.Errorf("nothing could be collected from %s", path)), false
	}

	if _, done := o.Store.Approved(v.Hash); done {
		return Output{}, false // these exact bytes were accepted before: say nothing
	}
	if !v.Blocking {
		if !v.Remembered() {
			// Below the threshold but not clean: let it load (that is what the threshold
			// says) and say so, but do NOT record it — the next load audits these bytes
			// again. See Verdict.Remembered for why a medium finding is the line.
			return Output{SystemMessage: v.UnrememberedLine()}, false
		}
		if v.Hash == "" {
			// Clean but with no identity: an approval is keyed by the bytes' hash, so there is
			// nothing to record, nothing to save, and no trust to announce.
			return Output{SystemMessage: v.UnhashedLine()}, false
		}
		// Clean and new: record it now. There is no prompt to answer, so there is no later
		// event that could carry the decision, and re-scanning identical bytes on every load
		// of every skill is the cost that would make people turn the gate off.
		o.Store.Approve(Approval{
			Hash: v.Hash, Name: v.Name, Kind: v.Kind, Path: v.Path, Score: v.Score,
			Verdict: VerdictClean, ApprovedAt: o.now(), ToolVersion: o.ToolVersion,
		})
		return Output{SystemMessage: v.Line()}, true
	}

	// Blocking.
	decision, reason := o.action(), v.Reason()
	if decision == DecisionAsk && askIsAnswered(ev.PermissionMode) {
		// "Hand it to the operator" degrades to "stop", never to "go ahead". The mode is
		// named in the reason because the operator has to know WHY they were not asked —
		// otherwise the gate looks stricter than they configured it, and the setting they
		// would need to change is not the gate's.
		decision = DecisionDeny
		reason += fmt.Sprintf(
			"\nThis session runs in permission mode %q, which answers prompts automatically. "+
				"An \"ask\" would have been accepted without you ever seeing it, so this was REFUSED instead.\n"+
				"To load it anyway, decide outside the session: aguard approve %q\n", ev.PermissionMode, v.Path)
	}
	// Park the verdict against this tool call ONLY when a yes is actually reachable, so that
	// PostToolUse can record an approval for the bytes that were shown — and only for those
	// (see handlePost). A refusal has no yes coming, and parking one anyway leaves a record
	// that can never be promoted, in a file written on every risky load.
	if decision == DecisionAsk {
		o.Store.pend(ev.ToolUseID, v, o.now())
	}
	return Output{HookSpecificOutput: &HookSpecific{
		HookEventName:            EventPreToolUse,
		PermissionDecision:       decision,
		PermissionDecisionReason: reason,
	}}, true
}

// handlePost records the approval an operator granted at the prompt.
//
// It re-resolves and re-scans rather than trusting the parked verdict, and promotes it ONLY
// when the hash still matches what the prompt described. Approving a hash nobody was shown
// is the one way this store could quietly certify unaudited content, and the window — however
// narrow — is closed by comparison rather than by argument.
func handlePost(ev Event, o Options) (Output, bool) {
	pending, ok := o.Store.pendingFor(ev.ToolUseID)
	if !ok {
		return Output{}, false // nothing was asked about this call: nothing to record
	}
	o.Store.dropPending(ev.ToolUseID)

	name := ev.SkillName()
	path, err := underDeadline(resolveDeadline, "resolving the skill", func() (string, error) {
		return ResolveSkill(o.Root, o.Home, ev.CWD, name)
	})
	if err != nil {
		return Output{SystemMessage: gateNote(fmt.Sprintf(
			"%q loaded after your approval, but it could not be re-read to record the decision (%v); it will be asked about again.",
			clip(report.Sanitize(name), maxNameLen), err))}, true
	}
	res, serr := withDeadline(scanDeadline, func() (model.ScanResult, error) { return o.Scan(path) })
	if serr != nil {
		return Output{SystemMessage: gateNote(fmt.Sprintf(
			"%q loaded after your approval, but re-reading it failed (%v); it will be asked about again.",
			clip(report.Sanitize(name), maxNameLen), serr))}, true
	}
	v, sok := Summarize(res, o.Threshold)
	if !sok || v.Hash != pending.Hash {
		return Output{SystemMessage: gateNote(fmt.Sprintf(
			"%q changed between the prompt and the load, so your approval was NOT recorded — it covered different bytes. It will be asked about again.",
			clip(report.Sanitize(name), maxNameLen)))}, true
	}
	o.Store.Approve(Approval{
		Hash: v.Hash, Name: v.Name, Kind: v.Kind, Path: v.Path, Score: v.Score,
		Verdict: VerdictAccepted, ApprovedAt: o.now(), ToolVersion: o.ToolVersion,
	})
	return Output{SystemMessage: fmt.Sprintf(
		"AgentGuard: risk accepted for %s %q (%d/100) · content %s · undo with: aguard approvals forget %s",
		v.Kind, v.Name, v.Score, shortHash(v.Hash), hashPrefix(v.Hash))}, true
}

// handleSessionStart is the inventory half (plan A): it takes stock of the whole root rather
// than watching a path, so it sees artifacts that arrived by ANY route — marketplace install,
// git clone, a file copied in by hand — including the surfaces the load-time gate cannot
// reach at all, because a plugin's hooks and MCP servers are live from the first turn and are
// never "loaded" through a tool call.
//
// It informs and does not block: SessionStart has no permission decision to return, and that
// asymmetry is exactly why plan B exists alongside it. Announcing this without saying so
// would leave an operator believing the whole environment is gated when only skills are.
func handleSessionStart(ev Event, o Options) (Output, bool) {
	// resume/compact re-enter a session that already got this at startup. Repeating it there
	// spends context on an unchanged answer, and a notice that appears constantly is one
	// people learn to skip past — which costs more than the repetition saves.
	if ev.Source != "" && ev.Source != "startup" && ev.Source != "clear" {
		return Output{}, false
	}
	if o.ScanRoot == nil {
		return Output{}, false
	}
	res, err := withDeadline(scanDeadline, o.ScanRoot)
	if err != nil {
		return Output{SystemMessage: gateNote(fmt.Sprintf("could not audit %s at session start (%v).", o.Root, err))}, false
	}

	type row struct {
		kind, name string
		score      int
		rules      []string
	}
	var rows []row
	for _, a := range res.Artifacts {
		if _, done := o.Store.Approved(a.Hash); done {
			continue
		}
		var rules []string
		blocking := false
		for _, f := range a.Findings {
			if !score.Deterministic(f) {
				continue
			}
			if f.Severity.Rank() >= o.Threshold.Rank() {
				blocking = true
			}
			rules = appendUnique(rules, f.RuleID)
		}
		if !blocking {
			continue
		}
		sort.Strings(rules)
		rows = append(rows, row{string(a.Kind), clip(report.Sanitize(a.Name), maxNameLen), a.Score, rules})
	}
	if len(rows) == 0 {
		// Nothing to raise — but the coverage boundary still has to be stated, and this is the
		// only place a session ever hears it.
		//
		// It used to ride along with the alert block below, which inverted the audience: the
		// operators who most need to know that hooks and MCP servers are NOT gated are exactly
		// the ones whose environment is quiet, and therefore the ones most likely to conclude
		// the gate covers everything. They were the only ones who never got told.
		// TestSessionStartSaysWhatItCannotGate did not catch it because it seeds a high finding,
		// so it only ever exercised the loud path — see TestSessionStartStatesCoverageWhenQuiet.
		//
		// SystemMessage only, deliberately no AdditionalContext: the audience is the operator,
		// the text is a fixed string with nothing attacker-written in it, and spending session
		// context to tell the MODEL what the gate does not cover buys the model nothing. That
		// also means no nonce fence is needed here, unlike the alert path.
		return Output{SystemMessage: quietCoverageMessage(o.Root, o.Threshold)}, false
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].score < rows[j].score })

	omitted := 0
	if len(rows) > maxArtifacts {
		omitted = len(rows) - maxArtifacts
		rows = rows[:maxArtifacts]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "AgentGuard audited %s at session start. %d artifact(s) carry a finding at or above %s and have not been approved:\n",
		o.Root, len(rows)+omitted, o.Threshold)
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-11s %-40s %3d/100  %s\n", r.kind, r.name, r.score, strings.Join(r.rules, ", "))
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  … and %d more, not listed.\n", omitted)
	}
	b.WriteString("Details: aguard scan. These were NOT blocked — a session-start audit can only inform.\n")
	b.WriteString("Hooks, MCP servers and instruction files are live from the first turn; only skills pass through the load-time gate.")

	out := Output{SystemMessage: b.String()}
	// The names in that block were written by whoever wrote the artifacts, so the copy that
	// enters the agent's context goes behind a nonce barrier (invariant #4). A barrier we
	// cannot generate is a barrier a hostile name could forge, so on that failure the agent
	// simply gets nothing and the operator still gets the message.
	if fenced, ferr := fence(b.String()); ferr == nil {
		out.HookSpecificOutput = &HookSpecific{HookEventName: EventSessionStart, AdditionalContext: fenced}
	}
	return out, false
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// unaudited is the fail-open-and-loud path (invariant #3). It allows the load — by returning
// no decision at all, so ordinary permission rules still apply — and says plainly that no
// audit stands behind it.
func unaudited(name string, err error) Output {
	return Output{SystemMessage: gateNote(fmt.Sprintf(
		"skill %q was loaded WITHOUT an audit: %v. Audit it yourself with: aguard check <path>",
		clip(report.Sanitize(name), maxNameLen), err))}
}

// gateNote prefixes the GATE-000 identifier so that every "this went unchecked" message is
// greppable and documented in docs/rules.md, the same as any dimension-0 note.
func gateNote(msg string) string {
	return "AgentGuard [GATE-000] " + report.Sanitize(msg)
}

// quietCoverageMessage is the session-start message when the audit raised nothing.
//
// Not a gateNote: `GATE-000` means "something could not be audited", and prefixing a clean
// result with it would turn a normal startup into an apparent failure — the same mistake as
// reporting the clean path through a non-zero exit code.
//
// Deliberately says the two things the loud path says and nothing else: that nothing was
// blocked, and which surfaces the gate does not stand in front of. No artifact names appear
// here (there are none to name), so unlike the alert path there is no attacker-written text
// and nothing to fence.
func quietCoverageMessage(root string, threshold model.Severity) string {
	return fmt.Sprintf("AgentGuard audited %s at session start: no unapproved artifact carries a finding at or "+
		"above %s. Only skills pass through the load-time gate — hooks, MCP servers and instruction files are live "+
		"from the first turn and are NOT gated. Full picture: aguard scan.", root, threshold)
}
