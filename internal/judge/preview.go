// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// PlannedCall is one question a run would ask, as `aguard llm preview` shows it (P-027): which
// artifact it is about, which pass asks it, and the exact text it would carry inside its nonce
// fence. A question asked several times (llm.samples) is one PlannedCall with Calls > 1 — the
// payload is the same every time.
type PlannedCall struct {
	Artifact int    // index into the artifacts given to Plan
	Pass     string // intent, injection, deobfuscation, collusion, capability, mcp-config, triage
	Rule     string // the rule its verdict would carry; "" for triage, which yields labels
	// Instruction is the system message's task text. The barrier rule that follows it in the real
	// message names the call's nonce, which is drawn per call and so cannot be shown in advance.
	Instruction string
	Temperature float64
	Calls       int // how many times the question is sent
	NotSent     int // how many of those the call budget (llm.max_calls) refuses
	// Payload is the text inside the fence, byte for byte what the client puts there.
	Payload string
	Sources []Source
	// Shortened is what the run discloses as left out of this call: an MCP configuration cut to fit, decoded
	// payloads or digest lines past the excerpt cap (the run's LLM-000 names each), triage items past the
	// per-artifact cap.
	Shortened string
}

// Source is where a payload's lines come from: a file of the artifact, and the lines of the
// ORIGINAL file that were sent (ranges, "1,3-5"). The lines missing from it — comments, blank
// runs, a capped middle — were not sent. Empty Lines: the text has no line in the file (a hook's
// command, an MCP configuration, a digest line, a decoded blob is its blob's one line).
type Source struct {
	File  string
	Lines string
}

// Plan returns the calls Run would make on arts with these options, in Run's order, without
// making any: the same plan (schedule), the same payload renderers the client uses (judgePayload,
// triagePayload). It builds no client and reads only the artifacts' files, so a preview can never
// send anything.
func Plan(arts []model.ArtifactReport, opts Options) []PlannedCall {
	opts = opts.defaults()
	send, refused, _ := schedule(arts, opts)
	all := append(append([]task(nil), send...), refused...)
	var out []PlannedCall
	for i, t := range all {
		notSent := 0
		if i >= len(send) {
			notSent = 1
		}
		// The samples of one question are consecutive in the plan and share its group.
		if i > 0 && sameQuestion(all[i-1], t) {
			out[len(out)-1].Calls++
			out[len(out)-1].NotSent += notSent
			continue
		}
		out = append(out, plannedCall(t, notSent))
	}
	return out
}

func sameQuestion(a, b task) bool {
	return a.kind == taskJudge && b.kind == taskJudge && a.group == b.group
}

// plannedCall renders one task as the preview shows it.
func plannedCall(t task, notSent int) PlannedCall {
	if t.kind == taskTriage {
		items, left := triageSent(t.items)
		c := PlannedCall{Artifact: t.artifact, Pass: "triage", Instruction: triageTask,
			Calls: 1, NotSent: notSent, Payload: triagePayload(items)}
		if left > 0 {
			c.Shortened = fmt.Sprintf("%d static finding(s) past the first %d not sent", left, maxTriageItems)
		}
		return c
	}
	info := modeInfo(t.req.Mode)
	return PlannedCall{Artifact: t.artifact, Pass: info.pass, Rule: info.rule, Instruction: modeTask(t.req.Mode),
		Temperature: t.req.Temperature, Calls: 1, NotSent: notSent, Payload: judgePayload(t.req),
		Sources: sources(t.units), Shortened: t.shortened}
}

// sources lists the files a call's units came from, in order, with the original lines each sent.
func sources(units []sourceUnit) []Source {
	var out []Source
	for _, u := range units {
		var lines []int
		switch {
		case len(u.lineMap) > 0:
			lines = u.lineMap
		case u.firstLine > 0:
			lines = []int{u.firstLine}
		}
		out = append(out, Source{File: u.file, Lines: lineRanges(lines)})
	}
	return out
}

// lineRanges renders line numbers as sorted, merged ranges: [1 3 4 5] → "1,3-5".
func lineRanges(lines []int) string {
	if len(lines) == 0 {
		return ""
	}
	ls := append([]int(nil), lines...)
	sort.Ints(ls)
	var parts []string
	for i := 0; i < len(ls); {
		j := i
		for j+1 < len(ls) && ls[j+1] <= ls[j]+1 {
			j++
		}
		if ls[i] == ls[j] {
			parts = append(parts, strconv.Itoa(ls[i]))
		} else {
			parts = append(parts, strconv.Itoa(ls[i])+"-"+strconv.Itoa(ls[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

// RequestModel is the model a request body names: the configured one, or the client's default.
func RequestModel(configured string) string {
	if configured == "" {
		return "llama3.1"
	}
	return configured
}
