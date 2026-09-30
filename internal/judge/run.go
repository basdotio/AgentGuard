// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/parse"
)

// Options tunes how a judge run talks to the endpoint. The zero value is usable — defaults()
// fills it in — so a caller that doesn't care still gets sane behavior.
type Options struct {
	Concurrency int           // in-flight calls (results still merge in a fixed order)
	CallTimeout time.Duration // per CALL, so one slow call can't consume the run
	MaxCalls    int           // 0 = unlimited; over-budget calls are skipped AND reported
	MaxRetries  int           // retries per call on a retryable transport error
	// Progress, when set, reports how far the run has got. It is called once with (0, total)
	// as soon as the plan is known — a judge run is the slowest thing this tool does, and
	// silence for a minute is indistinguishable from a hang — and then after each completed
	// call. Invocations are SERIALIZED, so an implementation needs no locking of its own.
	// Pure telemetry: it cannot influence results, but a slow implementation slows the run.
	Progress func(done, total int)
	// Samples is how many independent times each question is asked. 1 (the default) asks once
	// and every grounded verdict counts. >1 asks N times and requires a MAJORITY before a
	// finding may affect the effective score — it costs N times as much, so it is opt-in.
	Samples int
}

func (o Options) defaults() Options {
	if o.Concurrency < 1 {
		o.Concurrency = 4
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = 60 * time.Second
	}
	if o.MaxRetries < 0 {
		o.MaxRetries = 0
	}
	if o.Samples < 1 {
		o.Samples = 1
	}
	return o
}

// samplingTemperature is used ONLY when Samples > 1. Asking the same question N times at
// temperature 0 returns the same answer N times, so the votes would agree by construction and
// the consensus would measure nothing. This value has to be high enough for the model's own
// uncertainty to show, low enough that it isn't answering a different question each time.
const samplingTemperature = 0.8

// majority is the consensus bar: more than half of the samples. Derived rather than
// configured, so there is no way to set a "consensus" of 1-of-3.
func majority(samples int) int { return samples/2 + 1 }

// Stats is what one judge run cost. It exists to make the price of `--llm` visible (and to
// be the raw material for a latency/cost baseline). Nothing here feeds a finding or the
// score — it is pure telemetry for the operator.
type Stats struct {
	Calls     int             // calls issued (a retry is the SAME call re-sent, not a new one)
	Retries   int             // retry attempts across all calls
	Failed    int             // calls that ended in an error after retries
	Skipped   int             // calls never issued: budget exhausted or run deadline hit
	Latencies []time.Duration // one per issued call, retries included
}

// Percentile returns the p-th percentile latency (p in [0,1]), or 0 when no call ran.
func (s Stats) Percentile(p float64) time.Duration {
	if len(s.Latencies) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), s.Latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

// RetryableError marks a transport failure a retry could plausibly fix (429, 5xx, a network
// hiccup). CLASSIFICATION lives in the transport — only it sees status codes — while the
// POLICY (how many times, how long to wait) lives here, so it applies to any Client.
type RetryableError struct {
	Err   error
	After time.Duration // the server's Retry-After hint; 0 when it didn't send one
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// Retryable wraps err as retryable with an optional server-supplied wait hint.
func Retryable(err error, after time.Duration) error { return &RetryableError{Err: err, After: after} }

const (
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 30 * time.Second
)

// taskKind distinguishes the two call shapes: a verdict, or a set of triage labels.
type taskKind int

const (
	taskJudge taskKind = iota
	taskTriage
)

// task is one queued call, pinned to the artifact it belongs to. Every task has a STABLE
// position in the table, and results are written back by index — which is what lets the run
// be concurrent without changing a single byte of its output (same guarantee, same shape, as
// detect.Engine.Run).
type task struct {
	kind     taskKind
	artifact int    // index into arts
	label    string // redacted "kind:name", for triage + error messages
	req      Request
	items    []TriageItem
	// units is the text this call was built from — the basis a verdict's quote is checked
	// against before the finding is kept (ground.go).
	units []sourceUnit
	// group ties the samples of ONE question together: with Samples>1 the same question is
	// queued N times, and the votes are counted per group at merge time. Samples of a group
	// are consecutive in the table, so counting them stays order-stable.
	group int
}

// result is one task's outcome. `done` distinguishes "ran and produced nothing" from
// "never ran" — without it, a skipped call would silently read as a clean verdict.
type result struct {
	done       bool
	finding    *model.Finding
	labels     []model.AdvisoryLabel
	err        error
	latency    time.Duration
	retries    int
	barrier    *model.Finding // the data block addressed the analyzer (LLM-007)
	ungrounded bool           // something was claimed but couldn't be quoted — dropped
	unquoted   string         // the quote that failed to ground, clipped — for the LLM-005 note
}

// groundedFinding turns a verdict into a finding ONLY if the text it quotes can be located in
// what was actually sent (ground.go). A grounded finding gets the real file:line it came from,
// replacing the artifact-level "line 0" that told a reader nothing; an unquotable one is
// dropped and counted, never rendered.
//
// This does NOT give the judge any authority: a grounded finding is still Source=llm, still
// advisory, still excluded from the score and from --fail-on (§5.2.1). Grounding buys
// accuracy and a citation — the decision power question is a separate, later one.
//
// A verdict can yield TWO independent findings: what the pass was looking for, and — from any
// pass — the fenced data having addressed the analyzer (LLM-007). The second holds whether or
// not the first fired: content can be perfectly benign and still argue with the scanner.
func groundedFinding(t task, v Verdict) (verdict, barrier *model.Finding, dropped bool, unquoted string) {
	if f := finding(t.req, v); f != nil {
		if file, line, ok := ground(v.Evidence, t.units); ok {
			f.Evidence[0].File, f.Evidence[0].Line = file, line
			verdict = f
		} else {
			dropped = true
			unquoted = clipQuote(v.Evidence)
		}
	}
	if quote := strings.TrimSpace(v.BarrierEvidence); quote != "" {
		if file, line, ok := ground(quote, t.units); ok {
			b := barrierFinding(t.req, quote)
			b.Evidence[0].File, b.Evidence[0].Line = file, line
			barrier = b
		} else {
			// Same bar as any other claim. A manipulation attempt nobody can point at is not
			// evidence of one, and exempting this signal because it "feels" high-confidence is
			// how a bar stops being a bar.
			dropped = true
			if unquoted == "" {
				unquoted = clipQuote(quote)
			}
		}
	}
	return verdict, barrier, dropped, unquoted
}

// clipQuote bounds a model-supplied quote for the LLM-005 note: the model saw only redacted
// text, so what it quotes back is at worst redacted text — but it is model output, so it is
// clipped, flattened to one line and passes through the same redaction once more on its way
// into a report. Seeing the failed quote is what separates "the model paraphrased" from "the
// excerpt cut the line it needed" — two different fixes.
func clipQuote(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 120 {
		q = q[:120] + "…"
	}
	return detect.Redact(q)
}

// Run applies the LLM judge to the artifacts in arts (in place) and returns advisory scan-level
// notes plus the run's telemetry. Called only when the judge is enabled (config + --llm).
// Which passes an artifact gets depends on its kind (planFor, spec §5.2):
//
//   - ModeInjection  : hidden directives in text an agent reads → dim 1,  LLM-003 · skills,
//     CLAUDE.md, subagents, commands, hooks — UNCONDITIONALLY
//   - ModeIntent     : declared purpose vs. scripts             → dim 10, LLM-001 · skills
//   - ModeExplain    : decode+explain obfuscated payloads        → dim 6,  LLM-004 · skills with a decodable blob
//   - ModeCollusion  : capability chain across files             → dim 3,  LLM-006 · skills the static screen flagged
//   - ModeCapability : hook command vs. its interception point   → dim 2,  LLM-008 · hooks
//   - ModeMCPConfig  : MCP server configuration risk             → dim 5,  LLM-009 · MCP servers
//   - Triage         : annotate existing STATIC findings         → advisory labels · any kind that has some
//
// Iron law (spec §5.2.1): findings are Source=llm — never scored, never gate — and the judge
// NEVER removes a static finding. Triage is advisory context only. Every gap (a failed call,
// a call the budget refused to make, a run that hit its deadline) collapses into an honest
// LLM-000 note, so "the judge didn't run" can never masquerade as "nothing was found".
func Run(ctx context.Context, c Client, arts []model.ArtifactReport, opts Options) ([]model.Finding, Stats) {
	if c == nil {
		return []model.Finding{coverageNote("LLM judge unavailable: no client configured")}, Stats{}
	}
	opts = opts.defaults()

	// The whole call plan is built BEFORE any call is made. That is not just tidiness: it
	// snapshots each artifact's static findings before advisory ones are appended (triage must
	// judge the deterministic findings, not the judge's own output), and it makes the budget
	// cut deterministic — a counter raced by workers would drop a different set each run.
	tasks := buildTasks(arts, opts.Samples)
	var notes []model.Finding
	stats := Stats{}
	if over := len(tasks) - opts.MaxCalls; opts.MaxCalls > 0 && over > 0 {
		notes = append(notes, coverageNote(fmt.Sprintf(
			"call budget (llm.max_calls=%d) reached: %d planned call(s) covering %s were NOT made",
			opts.MaxCalls, over, artifactSpan(tasks[opts.MaxCalls:]))))
		stats.Skipped = over
		tasks = tasks[:opts.MaxCalls]
	}
	if len(tasks) == 0 {
		return notes, stats
	}

	if opts.Progress != nil {
		opts.Progress(0, len(tasks))
	}
	results := runTasks(ctx, c, tasks, opts)

	// Merge in task order: identical to what a serial run would have produced. Judge results
	// are tallied per group first (a group is one question, asked `Samples` times) and emitted
	// afterwards in group order, which is the same order the first sample of each group had.
	deadlineSkipped := 0
	failed, firstErr := 0, error(nil)
	ballots := map[int]*ballot{}
	var groupOrder []int
	for i, t := range tasks {
		r := results[i]
		if !r.done {
			stats.Skipped++
			deadlineSkipped++
			continue
		}
		stats.Calls++
		stats.Retries += r.retries
		stats.Latencies = append(stats.Latencies, r.latency)
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		switch t.kind {
		case taskJudge:
			b := ballots[t.group]
			if b == nil {
				b = &ballot{artifact: t.artifact, label: t.label}
				ballots[t.group] = b
				groupOrder = append(groupOrder, t.group)
			}
			b.cast(r)
		case taskTriage:
			// Labels land on a SEPARATE display channel; the findings themselves are never
			// touched (iron law #2, §5.2.1).
			arts[t.artifact].Advisory = append(arts[t.artifact].Advisory, r.labels...)
		}
	}
	ungrounded := 0
	var unquoted []string
	seenBarrier := map[string]bool{} // artifact+file+line: several passes see the same directive
	for _, g := range groupOrder {
		b := ballots[g]
		f, barrier, dropped := b.result(opts.Samples)
		if dropped {
			ungrounded++
			if b.unquoted != "" && len(unquoted) < 3 {
				unquoted = append(unquoted, fmt.Sprintf("%s: %q", b.label, b.unquoted))
			}
		}
		if f != nil {
			arts[b.artifact].Findings = append(arts[b.artifact].Findings, *f)
		}
		if barrier == nil {
			continue
		}
		// Every pass on an artifact reads overlapping text, so one planted directive would
		// otherwise be reported once per pass. Deduped by WHERE it is, not by artifact, so two
		// genuinely different directives still surface separately.
		ev := barrier.Evidence[0]
		key := fmt.Sprintf("%d|%s|%d", b.artifact, ev.File, ev.Line)
		if seenBarrier[key] {
			continue
		}
		seenBarrier[key] = true
		arts[b.artifact].Findings = append(arts[b.artifact].Findings, *barrier)
	}
	stats.Failed = failed
	if failed > 0 {
		notes = append(notes, coverageNote(
			fmt.Sprintf("LLM judge failed on %d call(s); those checks did not run (first error: %v)", failed, firstErr)))
	}
	if deadlineSkipped > 0 {
		notes = append(notes, coverageNote(fmt.Sprintf(
			"run deadline (llm.total_timeout) reached: %d planned call(s) were not made", deadlineSkipped)))
	}
	if ungrounded > 0 {
		// Dropping a finding is itself something the operator must be able to see: a silent
		// drop would make a mis-tuned prompt or a paraphrasing model look like a clean scan.
		notes = append(notes, model.Finding{
			RuleID: "LLM-005", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM,
			Title: "LLM findings discarded: evidence not found in the scanned text",
			Why: fmt.Sprintf("%d flagged verdict(s) quoted text that does not appear in what was sent to the model, "+
				"so they were discarded rather than reported: a verdict whose quoted evidence cannot be located in the scanned text is treated as fabricated.%s", ungrounded,
				unquotedSuffix(unquoted)),
		})
	}
	return notes, stats
}

// crossFileChainRule is the static cross-file screen (detect). It is the documented trigger
// for the collusion pass (spec §5.2): coarse and cheap, so it costs a few extra calls rather
// than report noise — the screen itself stays low + advisory either way.
const crossFileChainRule = "EXFIL-002"

// buildTasks turns the artifact list into the full call plan, in a fixed order, expanding
// each question into `samples` independent calls. Samples of one question stay consecutive and
// share a group id, so the votes can be counted without disturbing the order everything else
// depends on.
func buildTasks(arts []model.ArtifactReport, samples int) []task {
	var tasks []task
	group := 0
	for i := range arts {
		for _, t := range planFor(i, arts[i]) {
			if t.kind == taskTriage {
				// Triage produces display labels, not findings — there is no vote to take, and
				// paying N times for a label that cannot move a number would be waste.
				tasks = append(tasks, t)
				continue
			}
			t.group = group
			group++
			if samples > 1 {
				t.req.Temperature = samplingTemperature
			}
			for s := 0; s < samples; s++ {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// planFor decides which calls one artifact gets (spec §5.2 trigger table). Two rules shape
// that table, and neither is about caution:
//
//   - INJECTION IS UNCONDITIONAL on every kind carrying text an agent reads. Gating it on a
//     static hit would leave the judge re-examining only what the regexes already found —
//     precisely the blind spot it exists to cover. Its scope is one file, so it is affordable.
//   - The expensive whole-tree passes (intent, collusion) ARE gated on a static signal, which
//     is a reasonable prior once a call costs real money.
func planFor(i int, a model.ArtifactReport) []task {
	label := detect.Redact(string(a.Kind) + ":" + a.Name)
	var out []task
	ask := func(req Request, units []sourceUnit) {
		req.Artifact = label
		out = append(out, task{kind: taskJudge, artifact: i, label: label, req: req, units: units})
	}

	switch a.Kind {
	case model.KindSkill:
		skill := parse.ReadSkill(a.Path)
		declared := detect.Redact(skill.Description)
		behavior, behaviorUnits := behaviorExcerpt(a.Path)
		// The declared purpose is part of what the model saw, so a verdict may legitimately
		// quote it; it lives in SKILL.md's frontmatter, near the top.
		intentUnits := append([]sourceUnit{{file: "SKILL.md", text: declared, firstLine: 1, collapsed: true}}, behaviorUnits...)
		ask(Request{Mode: ModeIntent, Declared: declared, Behavior: behavior}, intentUnits)

		body, bodyLM := condense("SKILL.md", detect.Redact(skill.Body), false)
		body, bodyLM = capHeadTail(body, bodyLM, maxExcerptBytes)
		// The description is the second side here too: "beyond what it says it is for" is a
		// far sharper question than "contains instructions", which every skill body does.
		ask(Request{Mode: ModeInjection, Declared: declared, Behavior: body},
			[]sourceUnit{{file: "SKILL.md", text: body, firstLine: skill.BodyLine, lineMap: offsetLines(bodyLM, skill.BodyLine-1)}})

		if payloads := decodedPayloads(a.Path); len(payloads) > 0 {
			texts := make([]string, len(payloads))
			for k, p := range payloads {
				texts[k] = p.text
			}
			ask(Request{Mode: ModeExplain, Behavior: strings.Join(texts, "\n---\n")}, payloads)
		}

		// Collusion: only once the static screen has seen the two halves land in DIFFERENT
		// files. The digest describes what each file can do; it does not ship the files.
		if hasFinding(a, crossFileChainRule) {
			if digest, units := capabilityDigest(a); len(units) > 0 {
				ask(Request{Mode: ModeCollusion, Declared: declared, Behavior: digest},
					append([]sourceUnit{{file: "SKILL.md", text: declared, firstLine: 1, collapsed: true}}, units...))
			}
		}

	case model.KindInstruction, model.KindSubagent, model.KindCommand,
		model.KindRule, model.KindWorkflow, model.KindOutputStyle, model.KindMemory:
		// One file of instruction text the agent reads. Subagents and slash commands declare a
		// purpose in their frontmatter, so they get the same two-sided question a skill does;
		// CLAUDE.md declares nothing and is judged on its own terms.
		//
		// The four auto-loaded kinds belong here for the same reason they are collected at all: each
		// is instruction text that enters context unbidden, and an output style enters the system
		// prompt itself. Omitting them would leave the surfaces where a REWRITTEN injection hides
		// best covered by regex only — the blind spot the judge exists for — and silently, since a
		// kind that generates no task also generates no LLM-000 coverage note.
		if text, units := singleFileExcerpt(a.Path); text != "" {
			ask(Request{Mode: ModeInjection,
				Declared: detect.Redact(parse.ReadMarkdown(a.Path).Description),
				Behavior: text}, units)
		}

	case model.KindConnector:
		// A remote connector's tool descriptions are text the model reads to decide when and
		// how to call each tool — written by the server, changeable at will. Injection is the
		// question; the "declared purpose" is the connector itself, so "beyond what this
		// connector is for" has a second side. The judge sees the same rendering the static
		// pass scans (detect.ConnectorText), capped head+tail like any excerpt.
		if text := detect.ConnectorText(a.Connector); text != "" {
			text, lm := condense(a.Name+".txt", detect.Redact(text), false)
			text, _ = capHeadTail(text, lm, maxExcerptBytes)
			ask(Request{Mode: ModeInjection,
				Declared: detect.Redact("Remote MCP connector \"" + a.Name + "\": the tool list its server sent (names, descriptions, parameter descriptions)"),
				Behavior: text}, []sourceUnit{{file: detect.Redact(a.Name) + " (connector)", text: text, firstLine: 0, collapsed: true}})
		}
	case model.KindHook:
		// A hook gets injection (does it push directives into the agent's context?) and
		// capability (is the command proportionate to its interception point?). It does NOT
		// get intent: an event name declares WHEN a hook runs, never what it ought to do, so
		// a mismatch comparison would have nothing honest on the other side.
		declared, behavior, units := hookExcerpt(a.Path, a.Hook)
		if behavior != "" {
			ask(Request{Mode: ModeInjection, Behavior: behavior}, units)
			ask(Request{Mode: ModeCapability, Declared: declared, Behavior: behavior}, units)
		}

	case model.KindMCP:
		// Configuration only. What the server's TOOLS do is invisible without connecting to
		// it, which this tool never does — the prompt says so, so a verdict cannot be read as
		// a statement about the server's behavior.
		if text, units := mcpExcerpt(a.Path, a.Name); text != "" {
			ask(Request{Mode: ModeMCPConfig, Behavior: text}, units)
		}
	}

	// Triage runs for ANY kind that has static findings — including permissions, whose
	// findings come from permcheck rather than the detect engine. Snapshotting it here, while
	// the plan is built, is what keeps triage judging the deterministic findings rather than
	// the judge's own output.
	if static := staticFindings(a.Findings); len(static) > 0 {
		out = append(out, task{kind: taskTriage, artifact: i, label: label, items: triageItems(static)})
	}
	return out
}

// ballot collects the samples of ONE question and turns them into at most one finding.
type ballot struct {
	artifact int
	label    string // redacted "kind:name", for the LLM-005 note
	// flagged holds the samples that flagged AND grounded, in sample order. Only these are
	// votes: a verdict whose quote isn't in the source can't support anything.
	flagged []model.Finding
	// barriers holds the samples that reported (and could quote) an instruction aimed at the
	// analyzer. Voted on exactly like the verdict: this signal is strong, but "strong" is not
	// a reason to skip the bar.
	barriers []model.Finding
	// ungrounded records that at least one sample claimed something it could not quote. Tracked
	// per GROUP, not per sample, so three fabricated samples of one question count as one
	// discard rather than inflating the number threefold.
	ungrounded bool
	unquoted   string // one of the quotes that failed, for the note
	votes      int    // samples that answered at all (the denominator)
}

func (b *ballot) cast(r result) {
	b.votes++
	if r.ungrounded {
		b.ungrounded = true
		if b.unquoted == "" {
			b.unquoted = r.unquoted
		}
	}
	if r.finding != nil {
		b.flagged = append(b.flagged, *r.finding)
	}
	if r.barrier != nil {
		b.barriers = append(b.barriers, *r.barrier)
	}
}

// result decides the group's outcome.
//
// A finding below the consensus bar is still REPORTED, just not weighted: "the model wasn't
// consistent about it" is a different claim from "it isn't there", and iron law #2 only ever
// lets the judge add. What consensus withholds is influence, not visibility — and the reader
// is told the vote so the weaker signal reads as weaker.
func (b *ballot) result(samples int) (verdict, barrier *model.Finding, dropped bool) {
	verdict = tally(b.flagged, samples)
	barrier = tally(b.barriers, samples)
	if verdict == nil && barrier == nil {
		dropped = b.ungrounded
	}
	return verdict, barrier, dropped
}

// advisoryOnly names the judge rules that are reported but NEVER weighted, whatever the vote.
// The escalation bar (grounding + consensus, spec §5.2.1) asks "did the model really see
// this?"; it cannot ask "is this the kind of thing that should move a score?". LLM-009 is a
// hygiene question — unpinned package, unknown publisher, credentials in env — and hygiene is
// what real MCP configs look like: on 500 benign configs it was the only judge rule to
// escalate on benign input, 5 of the 7 judge-added flags. The two malicious samples
// it alone caught are static shapes (a reverse shell in args; an autorun switch the static
// rules deliberately do not gate), not the judge's job. So the question is still asked and the answer still
// shown, with the model's own severity — it just never sets Escalates.
var advisoryOnly = map[string]bool{"LLM-009": true}

// tally turns one group's votes into at most one finding, recording the count for the reader.
func tally(votes []model.Finding, samples int) *model.Finding {
	if len(votes) == 0 {
		return nil
	}
	f := votes[0] // first sample wins: deterministic, since samples merge in order
	f.Escalates = len(votes) >= majority(samples) && !advisoryOnly[f.RuleID]
	if samples > 1 {
		f.Why += fmt.Sprintf(" [%d of %d samples agreed] [severities: %s]", len(votes), samples, voteSeverities(votes))
		if !f.Escalates && !advisoryOnly[f.RuleID] {
			f.Why += " — below the consensus bar, so it is shown but carries no weight"
		}
	}
	if advisoryOnly[f.RuleID] {
		f.Why += " [advisory only — this rule never carries weight]"
	}
	return &f
}

// voteSeverities lists each agreeing vote's own severity, in sample order. The finding
// itself carries one severity; without this the others were dropped here, so how far apart the
// samples were could not be read back from any report — and a rerun could not compare "the first
// vote's severity" with "the majority's" on one raw output.
func voteSeverities(votes []model.Finding) string {
	sevs := make([]string, len(votes))
	for i, v := range votes {
		sevs[i] = string(v.Severity)
	}
	return strings.Join(sevs, ", ")
}

// hasFinding reports whether an artifact already carries a given static rule.
func hasFinding(a model.ArtifactReport, ruleID string) bool {
	for _, f := range a.Findings {
		if f.RuleID == ruleID && f.Source != model.SrcLLM {
			return true
		}
	}
	return false
}

// runTasks executes the plan with a bounded worker pool, writing each result to its own slot.
// Workers share nothing mutable, so the merge above is race-free and order-stable.
func runTasks(ctx context.Context, c Client, tasks []task, opts Options) []result {
	results := make([]result, len(tasks))
	workers := opts.Concurrency
	if workers > len(tasks) {
		workers = len(tasks)
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	done := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				results[i] = execute(ctx, c, tasks[i], opts)
				if opts.Progress != nil {
					// Serialized here so callers can just print, and so the count can never go
					// backwards or be reported twice for the same call.
					progressMu.Lock()
					done++
					opts.Progress(done, len(tasks))
					progressMu.Unlock()
				}
			}
		}()
	}
dispatch:
	for i := range tasks {
		select {
		case idx <- i:
		case <-ctx.Done():
			break dispatch // run deadline: stop queueing; unqueued slots stay done=false
		}
	}
	close(idx)
	wg.Wait()
	return results
}

// execute performs one task under its OWN timeout, retrying a retryable transport error with
// exponential backoff. The per-call deadline is the point of the exercise: previously a whole
// judge run shared one clock, so a handful of slow calls could starve every remaining check.
func execute(ctx context.Context, c Client, t task, opts Options) result {
	start := time.Now()
	res := result{done: true}
	for attempt := 0; ; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, opts.CallTimeout)
		var err error
		switch t.kind {
		case taskJudge:
			var v Verdict
			if v, err = c.Judge(cctx, t.req); err == nil {
				res.finding, res.barrier, res.ungrounded, res.unquoted = groundedFinding(t, v)
			}
		case taskTriage:
			res.labels, err = c.Triage(cctx, t.label, t.items)
		}
		cancel()
		if err == nil || attempt >= opts.MaxRetries || !isRetryable(err) || ctx.Err() != nil {
			res.err = err
			break
		}
		res.retries++
		if !waitBackoff(ctx, attempt, err) {
			res.err = err // run deadline hit while backing off — report the original failure
			break
		}
	}
	res.latency = time.Since(start)
	return res
}

func isRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// waitBackoff sleeps before a retry: the server's Retry-After when it sent one, else
// exponential backoff with full jitter. Jitter only shifts WHEN a retry happens — results are
// merged by index, so it can never change what a run produces.
func waitBackoff(ctx context.Context, attempt int, err error) bool {
	d := baseBackoff << attempt
	if d > maxBackoff {
		d = maxBackoff
	}
	d = time.Duration(rand.Int64N(int64(d)) + int64(baseBackoff))
	var re *RetryableError
	if errors.As(err, &re) && re.After > 0 {
		d = min(re.After, maxBackoff)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// artifactSpan names the artifacts a set of skipped tasks belonged to, so a budget note says
// WHAT went unchecked rather than just how many calls were dropped.
func artifactSpan(skipped []task) string {
	seen := map[string]bool{}
	var names []string
	for _, t := range skipped {
		if !seen[t.label] {
			seen[t.label] = true
			names = append(names, t.label)
		}
	}
	if len(names) > 3 {
		return fmt.Sprintf("%s and %d more artifact(s)", strings.Join(names[:3], ", "), len(names)-3)
	}
	return strings.Join(names, ", ")
}

// staticFindings returns the deterministic findings (the ones triage may comment on): not
// LLM-sourced, not dim-0 notes.
func staticFindings(fs []model.Finding) []model.Finding {
	var out []model.Finding
	for _, f := range fs {
		if f.Source != model.SrcLLM && f.Dimension != 0 {
			out = append(out, f)
		}
	}
	return out
}

// triageItems builds the redacted (RuleID, evidence) pairs sent for triage. The evidence
// snippet is already redacted at detect time; we re-redact defensively.
func triageItems(fs []model.Finding) []TriageItem {
	items := make([]TriageItem, 0, len(fs))
	for _, f := range fs {
		ev := ""
		if len(f.Evidence) > 0 {
			ev = fmt.Sprintf("%s:%d %s", f.Evidence[0].File, f.Evidence[0].Line, f.Evidence[0].Snippet)
		}
		items = append(items, TriageItem{RuleID: f.RuleID, Evidence: detect.Redact(ev)})
	}
	return items
}

// coverageNote builds a dim-0 scan note. Severity is low: a judge outage reduces coverage
// but is not itself a risk; it is surfaced so the gap is never silent.
func coverageNote(msg string) model.Finding {
	return model.Finding{
		RuleID:    "LLM-000",
		Dimension: 0,
		Severity:  model.SevLow,
		Source:    model.SrcLLM,
		Title:     "LLM judge: partial coverage",
		Why:       msg,
	}
}

// unquotedSuffix renders the failed quotes for LLM-005, or nothing when none were captured.
func unquotedSuffix(qs []string) string {
	if len(qs) == 0 {
		return ""
	}
	return " Quoted but not found — " + strings.Join(qs, "; ") + "."
}
