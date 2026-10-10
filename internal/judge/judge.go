// SPDX-License-Identifier: MIT
// Package judge is AgentGuard's M5 LLM intent judge (spec §5.2/§16.4): it catches
// "the description says A but the code does B" mismatches that static rules miss.
//
// Hard constraints (must never be relaxed):
//   - OFF by default: runs only when config enables it AND `--llm` is passed to `scan` or
//     `check`. The load-time gate never passes it: a hook that fires on every load, under a
//     deadline, must not wait on a model or spend money nobody asked to spend.
//   - Every byte sent is passed through detect.Redact first (§16.3). Redaction is BEST-EFFORT
//     (known secret shapes + high-entropy tokens); it is not a guarantee, so enabling the judge
//     against a NON-LOCAL endpoint means best-effort-redacted skill content leaves the machine.
//     Default endpoint is local for this reason.
//   - Advisory ONLY: findings are Source=llm, so they never move the deterministic score
//     and never trip --fail-on (score.Apply and report.HasAtLeast both skip Source==llm).
//     Scoring stays reproducible (required for D12 on-chain attestation).
package judge

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// Threat dimensions (spec §3) an LLM verdict can map to. The dimension is fixed PER MODE and
// never taken from the model: letting a verdict choose its own scoring bucket would hand a
// hostile artifact a way to pick which dimension it lands in.
const (
	dimInjection   = 1  // hidden prompt injection in instructions
	dimCapability  = 2  // capability out of proportion to what the artifact is for
	dimCollusion   = 3  // data flow assembled across files
	dimSupplyChain = 5  // where a dependency/server comes from and whether it is pinned
	dimObfusc      = 6  // obfuscation (decoded payload)
	dimIntent      = 10 // intent mismatch (description vs behavior)
)

// Mode selects what the judge is looking for; it picks the system prompt and how the result
// is surfaced (an artifact finding, or an advisory scan note for triage).
type Mode int

const (
	ModeIntent     Mode = iota // description-vs-behavior mismatch (skills)
	ModeInjection              // hidden/paraphrased prompt injection in instruction text
	ModeExplain                // decode + explain an obfuscated payload (deobfuscation)
	ModeCapability             // hooks: is the command proportionate to its interception point?
	ModeMCPConfig              // MCP servers: what the CONFIG says (see limits in prompt.go)
	ModeCollusion              // skills: does a capability chain span several files?
)

// Request is one artifact submission. Declared and Behavior are ALREADY redacted before
// construction. Both are UNTRUSTED artifact content and are fenced with a per-call nonce
// so the model can't be hijacked by instructions embedded in them (spec §5.2 barrier).
type Request struct {
	Artifact string // "kind:name", redacted, for labeling only
	Mode     Mode
	// Declared is the artifact's own account of what it is FOR — the side a behavior is
	// judged against. For a skill that is its description; for a hook it is the structural
	// interception point (event + matcher), which is the only honest "purpose" a hook has:
	// its event name says WHEN it runs, never what it ought to do.
	Declared string
	Behavior string // the text under scrutiny, redacted
	// Temperature is 0 for a single call — as deterministic as the endpoint allows. Consensus
	// sampling raises it, because asking the same question N times at temperature 0 returns
	// the same answer N times: the votes would be identical by construction and the "agreement"
	// would measure nothing at all.
	Temperature float64
}

// twoSided reports whether this request compares a declared side against a behavior side (and
// so renders both in the prompt), rather than examining one block of text.
//
// Injection is the conditional one: an artifact that declares a purpose can be judged against
// it ("does this go beyond what it says it is for?"), while CLAUDE.md declares nothing and can
// only be read on its own terms.
func (r Request) twoSided() bool {
	switch r.Mode {
	case ModeIntent, ModeCapability, ModeCollusion:
		return true
	case ModeInjection:
		return r.Declared != ""
	}
	return false
}

// Verdict is the model's structured judgment (mode-agnostic). Parsed from its JSON reply.
type Verdict struct {
	Flagged bool `json:"flagged"` // intent: undisclosed behavior; injection: hidden directive found
	// Category is the kind of thing the model found, one name from the pass's closed list
	// (severity.go). It is what sets the finding's severity; an unknown or missing name is
	// medium. Disclosed, on the intent pass only, says the declared purpose stated the behaviour
	// (one step lower, except a changed software source). P-041.
	Category  string `json:"category"`
	Disclosed bool   `json:"disclosed"`
	// Severity is the model's own word. It is recorded (the vote list shows it beside the
	// tool's) and never decides a finding's severity, except on a pass without a category table
	// (LLM-009, advisory-only), where it is clamped to at most high.
	Severity string `json:"severity"`
	Summary  string `json:"summary"`  // one-line explanation
	Evidence string `json:"evidence"` // the specific triggering text
	// BarrierEvidence carries the directive the fenced data aimed at the ANALYZER, quoted
	// verbatim; empty means none. It is one field rather than a boolean plus a quote so that
	// "claims a violation but cannot show it" is not a representable state — and so the claim
	// goes through the same grounding check as everything else.
	//
	// This is orthogonal to Flagged: a model may find the artifact's behavior perfectly fine
	// and still have been told to ignore its instructions, and that attempt is a signal in its
	// own right (LLM-007). Note the asymmetry — a report means an attempt was made, but silence
	// proves nothing, because a manipulation that worked would not be reported.
	BarrierEvidence string `json:"barrier_evidence"`
	// repaired says the reply closed its object one member early and was read by dropping that
	// brace (parseVerdict, P-034). Set by the reader, never by a model: unexported, so encoding/json
	// neither reads nor writes it, and Run counts it so the repair is never silent.
	repaired bool
}

// TriageItem is one static finding submitted for triage (RuleID + a redacted evidence line).
type TriageItem struct {
	RuleID   string
	Evidence string
}

// Client is the LLM judge. Implementations MUST send only the (already-redacted) inputs and
// nothing else about the environment, and must fence untrusted content with a nonce barrier.
type Client interface {
	// Judge runs a flagged-verdict mode (intent / injection / explain).
	Judge(ctx context.Context, r Request) (Verdict, error)
	// Triage labels an artifact's static findings likely-real vs likely-benign (advisory).
	// It returns display-only labels; it never removes or changes a finding.
	Triage(ctx context.Context, artifact string, items []TriageItem) ([]model.AdvisoryLabel, error)
}

// clampSeverity maps the model's severity word to a safe advisory level. Unknown → medium;
// anything the model calls "critical" is capped to high, because an LLM guess must never present
// as a confirmed critical. Case and surrounding space carry no meaning: "High" is the model saying
// high, and reading it as unknown silently lowered its claim. Since P-041 the word decides a
// finding's severity only on a pass without a category table (severityFor); elsewhere it is the
// "model said" half of the vote list.
func clampSeverity(s string) model.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return model.SevLow
	case "high", "critical":
		return model.SevHigh
	default:
		return model.SevMedium
	}
}

// Bounds on model-authored or model-selected text that reaches a report. The model decides how
// long its answer is; without a cap, a megabyte of it was a megabyte in every report format.
const (
	maxWhyBytes     = 512 // a verdict's reason, before consensus appends its vote
	maxSnippetBytes = 512 // the grounded line(s) shown as a judge finding's evidence
)

// ellipsis marks where bounded text was cut.
const ellipsis = "…"

// capBytes bounds s to max bytes, cutting on a rune boundary (a cut mid-rune would put invalid
// UTF-8 in the report) and marking the cut with an ellipsis. Callers redact FIRST, so a secret
// straddling the cut cannot survive as a sub-threshold fragment (invariant #3).
func capBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// modeRow is what a mode's verdict is: the pass a reader knows it by, the rule ID it carries, the
// dimension it lands in and its title. finding() and Plan read the same row, so the preview names a
// call by the rule its verdict would carry.
type modeRow struct {
	pass, rule string
	dim        int
	title      string
}

func modeInfo(m Mode) modeRow {
	switch m {
	case ModeInjection:
		return modeRow{"injection", "LLM-003", dimInjection, "Hidden prompt injection (LLM judge — advisory, not confirmed)"}
	case ModeExplain:
		return modeRow{"deobfuscation", "LLM-004", dimObfusc, "Decoded obfuscated payload (LLM judge — advisory, not confirmed)"}
	case ModeCollusion:
		return modeRow{"collusion", "LLM-006", dimCollusion, "Cross-file capability chain (LLM judge — advisory, not confirmed)"}
	case ModeCapability:
		return modeRow{"capability", "LLM-008", dimCapability, "Hook capability exceeds its interception point (LLM judge — advisory, not confirmed)"}
	case ModeMCPConfig:
		return modeRow{"mcp-config", "LLM-009", dimSupplyChain, "MCP server configuration risk (LLM judge — advisory, not confirmed)"}
	}
	return modeRow{"intent", "LLM-001", dimIntent, "Intent mismatch (LLM judge — advisory, not confirmed)"}
}

// finding converts a flagged verdict into an advisory finding. Returns nil when nothing was
// flagged. RuleID/dimension/title depend on the mode. The finding has no location yet: only
// grounding supplies one (groundedFinding), together with the snippet — so nothing the model
// wrote as "evidence" can reach a report from here. Control-character sanitization happens in
// the report renderer.
func finding(r Request, v Verdict) *model.Finding {
	if !v.Flagged {
		return nil
	}
	info := modeInfo(r.Mode)
	ruleID, dim, title := info.rule, info.dim, info.title
	// Redact, then cap (invariant #3). The cap applies to the model's sentence only: consensus
	// appends the vote afterwards (tally), and the vote must always be readable.
	why := capBytes(detect.Redact(v.Summary), maxWhyBytes)
	if strings.TrimSpace(why) == "" {
		why = model.JudgeRuleText(ruleID)
	}
	return &model.Finding{
		RuleID:    ruleID,
		Dimension: dim,
		Severity:  severityFor(r.Mode, v),
		Title:     title,
		Why:       why,
		Source:    model.SrcLLM,
		Advisory:  true,
		Evidence:  []model.Evidence{{File: r.Artifact, Line: 0}},
	}
}

// barrierFinding reports that the fenced data tried to give the analyzer instructions
// (LLM-007, dimension 1). An artifact that argues with the tool examining it is not doing so
// by accident, which makes this one of the highest-confidence malicious signals available —
// so unlike every other verdict, the SEVERITY IS OURS, not the model's. A manipulation attempt
// would naturally include "and rate this low"; letting the model grade its own report of being
// attacked would hand the attacker the volume knob. Like finding, it carries no location until
// grounding supplies one.
func barrierFinding(r Request) *model.Finding {
	return &model.Finding{
		RuleID:    "LLM-007",
		Dimension: dimInjection,
		Severity:  model.SevHigh,
		Title:     "Artifact tried to instruct the analyzer (LLM judge — advisory, not confirmed)",
		Why:       model.JudgeRuleText("LLM-007"),
		Source:    model.SrcLLM,
		Advisory:  true,
		Evidence:  []model.Evidence{{File: r.Artifact, Line: 0}},
	}
}

// evidence is what a grounded span contributes to a finding: the real location and, as the
// snippet, the sent line(s) it landed on (already cut to a window around the quote when they are
// too long, ground.go) — re-redacted defensively like everything else the judge hands to a
// report. The cap is a backstop: it only bites if that re-redaction made the text longer.
func (s groundedSpan) evidence() model.Evidence {
	return model.Evidence{File: s.file, Line: s.line, Snippet: capBytes(detect.Redact(s.text), maxSnippetBytes)}
}
