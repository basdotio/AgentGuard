// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"sync"
)

// The prompt version names what the client wraps around the scanned content: every pass's system
// message, the user-message layout around the excerpt, the response format the model is told to
// use, the temperatures and the request envelope — everything that would make the same excerpt
// get a different answer (P-031). It is computed, never maintained: a sha256 over the exact JSON
// body of each call shape, with three things stood in because they are not the judge's to version
// — the scanned content (an input), the model (reported on its own, as JudgeSummary.Model) and the
// nonce (random per call). TestPromptVersion_HashesWhatTheClientSends drives the real client over
// every shape and requires each body to be the one hashed here, so a change to what is sent either
// moves this value or turns that test red.
//
// Not in it: the endpoint and the headers (the Authorization header is the key), Ping (`llm test`
// sends nothing scanned and yields no finding), and how an artifact becomes the excerpt in the
// first place — that is ExcerptVersion's (ground.go).

// Stand-ins for what the version abstracts over.
const (
	nonceStandIn    = "<nonce>"
	modelStandIn    = "<model>"
	declaredStandIn = "<declared>"
	behaviorStandIn = "<behavior>"
	ruleStandIn     = "<rule>"
	evidenceStandIn = "<evidence>"
)

// promptVersionLen is how many hex digits the version keeps, as rules_version does.
const promptVersionLen = 12

// passModes is every Mode in declaration order; TestPromptVersion_EveryInputIsDecided reads the
// constants from judge.go and fails when one is missing here.
var passModes = []Mode{ModeIntent, ModeInjection, ModeExplain, ModeCapability, ModeMCPConfig, ModeCollusion}

// callShape is one kind of request the client sends: a judge call (req), or a triage call carrying
// the given items. The content in it is stand-ins only.
type callShape struct {
	name   string
	req    Request
	triage []TriageItem
}

// callShapes is every request shape the client can send: each pass with both sides, with the
// declared side empty, with the behavior empty and with both empty (the placeholders the layout
// fills in, and the injection pass's switch between one and two sides), at temperature 0 and at
// samplingTemperature; and triage with one item more than it sends, so its cap is in the body.
func callShapes() []callShape {
	var out []callShape
	for _, m := range passModes {
		for _, temp := range []float64{0, samplingTemperature} {
			for _, v := range []struct {
				name               string
				declared, behavior string
			}{
				{"both sides", declaredStandIn, behaviorStandIn},
				{"declared empty", "", behaviorStandIn},
				{"behavior empty", declaredStandIn, ""},
				{"both empty", "", ""},
			} {
				out = append(out, callShape{
					name: fmt.Sprintf("mode %d, temperature %v, %s", m, temp, v.name),
					req:  Request{Mode: m, Declared: v.declared, Behavior: v.behavior, Temperature: temp},
				})
			}
		}
	}
	items := make([]TriageItem, maxTriageItems+1)
	for i := range items {
		items[i] = TriageItem{RuleID: ruleStandIn, Evidence: evidenceStandIn}
	}
	return append(out, callShape{name: "triage", triage: items})
}

// messages renders a shape's system and user message and its temperature, as the client does. A
// judge call goes through the client's own systemPrompt and userPrompt. Triage builds its messages
// inline in Triage, so they are restated here — exactly, or the capture test fails.
func (s callShape) messages() (system, user string, temperature float64) {
	if s.triage == nil {
		return systemPrompt(s.req.Mode, nonceStandIn), userPrompt(s.req, nonceStandIn), s.req.Temperature
	}
	items := s.triage
	if len(items) > maxTriageItems {
		items = items[:maxTriageItems]
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "[%s] %s\n", it.RuleID, it.Evidence)
	}
	fence := "===AGUARD:" + nonceStandIn + "==="
	return triageTask + " " + barrierRule(nonceStandIn), fmt.Sprintf("%s\n%s\n%s", fence, b.String(), fence), 0
}

// shapeBodies renders each shape as the request body the client would send, encoded the way chat()
// encodes it.
func shapeBodies(shapes []callShape) [][]byte {
	out := make([][]byte, len(shapes))
	for i, s := range shapes {
		system, user, temperature := s.messages()
		var body bytes.Buffer
		enc := json.NewEncoder(&body)
		enc.SetEscapeHTML(false)
		// The request types are plain strings and numbers; encoding them cannot fail.
		_ = enc.Encode(chatRequest{
			Model:       modelStandIn,
			Temperature: temperature,
			Messages:    []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		})
		out[i] = body.Bytes()
	}
	return out
}

var promptVersionOnce = sync.OnceValue(func() string { return promptVersion(shapeBodies(callShapes())) })

// PromptVersion names the prompts this build's judge sends: the first 12 hex digits of a sha256
// over the request body of every call shape, scanned content, model and nonce stood in. A --llm
// report carries it as judge.prompt_version and `aguard version` prints it as judge-prompt=.
//
// Two values that differ mean the model was asked differently. Two that agree mean every prompt,
// layout, response format, temperature and request field was the same — not that the model, the
// excerpts or the answers were.
func PromptVersion() string { return promptVersionOnce() }

// promptVersion is PromptVersion over arbitrary bodies, so tests can mutate them. Bodies are taken
// in order and length-prefixed: a body can contain any byte, so no separator is safe.
func promptVersion(bodies [][]byte) string {
	h := sha256.New()
	hashField(h, strconv.Itoa(len(bodies)))
	for _, b := range bodies {
		hashField(h, string(b))
	}
	return hex.EncodeToString(h.Sum(nil))[:promptVersionLen]
}

// hashField appends one length-prefixed field; hash.Hash writes never fail.
func hashField(h hash.Hash, s string) {
	_, _ = fmt.Fprintf(h, "%d:%s;", len(s), s)
}
