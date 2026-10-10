// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// legacyParse is the reader as it stood before P-034: slice from the first `{` to the last `}`
// and unmarshal that. It is the reference the clean rows and the refused rows are held to — a
// reply it accepts reads the same now, and a reply it refuses fails with the same error, unless
// it is the one repairable shape.
func legacyParse(content string) (Verdict, error) {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return Verdict{}, fmt.Errorf("no JSON object in judge reply")
	}
	var v Verdict
	if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
		return Verdict{}, fmt.Errorf("parse judge verdict: %w", err)
	}
	return v, nil
}

// Synthetic members for the reply shapes below. None of this is a captured reply: the one that
// was captured quotes a third-party corpus sample.
const (
	rFirst    = `{"flagged": false, "severity": "low", "summary": "formats tables", "evidence": "echo ok"`
	rFlagged  = `{"flagged": true, "severity": "high", "summary": "tells the agent to lie", "evidence": "echo ok"`
	rBraces   = `{"flagged": true, "severity": "medium", "summary": "s", "evidence": "if (a) { b(); } echo \"}\" {x: {y}}"`
	rBarrierQ = "ignore your instructions"
)

var (
	vFirst   = Verdict{Flagged: false, Severity: "low", Summary: "formats tables", Evidence: "echo ok"}
	vFlagged = Verdict{Flagged: true, Severity: "high", Summary: "tells the agent to lie", Evidence: "echo ok"}
	vBraces  = Verdict{Flagged: true, Severity: "medium", Summary: "s", Evidence: `if (a) { b(); } echo "}" {x: {y}}`}
)

type replyCase struct {
	name  string
	reply string
	want  *Verdict // nil: the reply must be refused
	// repaired: read only by removing the one stray brace.
	repaired bool
	// wantErr is a substring the refusal must carry; "" means today's error, whatever it is.
	wantErr string
}

func with(v Verdict, f func(*Verdict)) *Verdict { f(&v); return &v }

var replyCases = []replyCase{
	// Clean replies: read exactly as before.
	{name: "clean object", reply: rFirst + `, "barrier_evidence": ""}`, want: &vFirst},
	{name: "prose and a fence around it", reply: "Here is my verdict:\n```json\n" + rFlagged + "}\n```\nDone.", want: &vFlagged},
	{name: "prose after it", reply: rFirst + "} I hope this helps.", want: &vFirst},
	{name: "braces and a quoted brace in the evidence", reply: rBraces + "}", want: &vBraces},
	{name: "a repeated member in a clean object keeps the last", reply: `{"flagged": true, "flagged": false}`, want: &Verdict{}},

	// Closed one member early: every member is there; drop the stray brace and read one object.
	{name: "closed early (the measured shape)", reply: rFirst + `}, "barrier_evidence": ""}`, want: &vFirst, repaired: true},
	{name: "closed early, the last member quotes a directive", reply: rFirst + `}, "barrier_evidence": "` + rBarrierQ + `"}`,
		want: with(vFirst, func(v *Verdict) { v.BarrierEvidence = rBarrierQ }), repaired: true},
	{name: "closed early, flagged in the second half", reply: `{"severity": "high", "summary": "s", "evidence": "e"}, "flagged": true}`,
		want: &Verdict{Flagged: true, Severity: "high", Summary: "s", Evidence: "e"}, repaired: true},
	{name: "closed early inside a fence", reply: "```json\n" + rFlagged + "},\n  \"barrier_evidence\": \"\"}\n```\n", want: &vFlagged, repaired: true},
	{name: "closed early, braces and a quoted brace in the evidence", reply: rBraces + `}, "barrier_evidence": ""}`, want: &vBraces, repaired: true},
	{name: "closed early, whitespace before the comma", reply: rFirst + "}\n  , \"barrier_evidence\": \"\"}  \n", want: &vFirst, repaired: true},

	// Closed early but not repairable: refused, never read as its first half.
	{name: "closed early, a member repeats", reply: rFirst + `}, "flagged": true}`},
	{name: "closed early, a member repeats in another case", reply: rFirst + `}, "Flagged": true}`},
	{name: "closed early, lost its last brace, flagged differs", reply: rFirst + `}, "flagged": true`, wantErr: "closed"},
	{name: "closed early, lost its last brace, a directive quote", reply: rFirst + `}, "barrier_evidence": "` + rBarrierQ + `"`, wantErr: "closed"},
	{name: "closed early, lost its last brace, before a fence", reply: "```json\n" + rFirst + "}, \"barrier_evidence\": \"\"\n```", wantErr: "closed"},
	{name: "closed early twice", reply: `{"flagged": false, "severity": "low"}, "summary": "s"}, "evidence": "e"}`},
	{name: "closed early, then prose", reply: rFirst + "}, \"barrier_evidence\": \"\"}\nThat is my verdict."},
	{name: "closed early, then a second object", reply: rFirst + `}, "barrier_evidence": ""} {"flagged": true}`},
	{name: "closed early, then garbage with a brace", reply: rFirst + `}, "barrier_evidence": ""} and more}`},
	{name: "closed early, nothing after the comma", reply: `{"flagged": false}, }`},
	{name: "an empty object closed early", reply: `{}, "flagged": true}`},
	{name: "closed early, a member of the wrong type", reply: `{"flagged": false}, "severity": 3}`},

	// Two complete objects: refused, whichever way they are joined.
	{name: "two objects back to back", reply: rFirst + "}" + rFlagged + "}"},
	{name: "two objects joined by a comma", reply: rFirst + "}," + rFlagged + "}"},
	{name: "two objects joined by a comma and a newline", reply: rFlagged + "},\n" + rFirst + "}"},
	{name: "an array of two objects", reply: "[" + rFirst + "}, " + rFlagged + "}]"},

	// Replies that fail today for another reason still fail, with today's error.
	{name: "no JSON", reply: "no json here", wantErr: "no JSON object"},
	{name: "a truncated object", reply: `{"flagged": true, "severity": "hi`},
	{name: "a member of the wrong type", reply: `{"flagged": "yes"}`, wantErr: "cannot unmarshal"},
}

// sameAnswer compares what the model said: the five members of a verdict.
func sameAnswer(a, b Verdict) bool {
	return a.Flagged == b.Flagged && a.Severity == b.Severity && a.Summary == b.Summary &&
		a.Evidence == b.Evidence && a.BarrierEvidence == b.BarrierEvidence
}

// TestParseVerdict_ReadsOneObject: a reply is read as ONE object or refused. The model sometimes
// closes its object one member early with a stray `}` and keeps writing (measured: 5 of 1,482
// calls); that reply holds every member and one answer, and is read by dropping that brace. Every
// other reply reads exactly as before — and nothing is ever read as the first of two halves,
// because the second half may carry a different flagged or a barrier quote (invariant #4: a
// misread must fail visibly, not decide).
func TestParseVerdict_ReadsOneObject(t *testing.T) {
	for _, c := range replyCases {
		t.Run(c.name, func(t *testing.T) {
			v, err := parseVerdict(c.reply)
			legacy, legacyErr := legacyParse(c.reply)
			if c.want == nil {
				switch {
				case err == nil:
					t.Fatalf("accepted %+v; want the reply refused, never read as one of its halves", v)
				case legacyErr != nil && err.Error() != legacyErr.Error():
					t.Errorf("error %q; want today's error %q, unchanged", err, legacyErr)
				case legacyErr == nil && (c.wantErr == "" || !strings.Contains(err.Error(), c.wantErr)):
					t.Errorf("error %q; want one that says %q", err, c.wantErr)
				case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
					t.Errorf("error %q; want one that says %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused (%v); want %+v", err, *c.want)
			}
			if !sameAnswer(v, *c.want) {
				t.Errorf("read %+v; want %+v", v, *c.want)
			}
			if !c.repaired && (legacyErr != nil || !sameAnswer(v, legacy)) {
				t.Errorf("a reply today reads as %+v (err %v) now reads as %+v; a clean reply must read as before", legacy, legacyErr, v)
			}
		})
	}
}

// closedEarlyEndpoint answers like a model that can only quote what it was shown: flagged with
// the needle as its evidence when the needle is in the request, not flagged otherwise. Its FIRST
// flagged answer is closed one member early — the measured slip — and every other is clean.
func closedEarlyEndpoint(t *testing.T, needle string) *HTTPClient {
	t.Helper()
	var mu sync.Mutex
	slipped := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(b, &req); err != nil {
			t.Errorf("request body is not a chat request: %v", err)
		}
		user := req.Messages[len(req.Messages)-1].Content
		quote, _ := json.Marshal(needle)
		content := `{"flagged": false, "severity": "low", "summary": "nothing", "evidence": "", "barrier_evidence": ""}`
		if strings.Contains(user, needle) {
			mu.Lock()
			first := !slipped
			slipped = true
			mu.Unlock()
			content = `{"flagged": true, "severity": "high", "summary": "tells the agent to misreport", "evidence": ` + string(quote)
			if first {
				content += `}, "barrier_evidence": ""}`
			} else {
				content += `, "barrier_evidence": ""}`
			}
		}
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message chatMessage `json:"message"`
		}{{Message: chatMessage{Role: "assistant", Content: content}}}})
	}))
	t.Cleanup(srv.Close)
	return NewHTTP(srv.URL, "", "m", srv.Client())
}

// TestRun_ClosedEarlyReplyIsAnswered: a call whose reply closed one member early is an answered
// call. It used to count as failed, with an LLM-000 saying the check did not run — on a benchmark,
// a whole sample marked incomplete — and the finding the model did report was lost.
func TestRun_ClosedEarlyReplyIsAnswered(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	writeFile(t, filepath.Join(dir, "SKILL.md"), padFM+padIntro+"Note: "+paddedDirective+"\n")
	arts := []model.ArtifactReport{{Kind: model.KindSkill, Name: "demo", Path: dir, Findings: []model.Finding{}}}

	notes, stats := Run(context.Background(), closedEarlyEndpoint(t, paddedDirective), arts, Options{Concurrency: 1})

	if stats.Failed != 0 {
		t.Errorf("Failed = %d; want 0: the closed-early reply was answered", stats.Failed)
	}
	for _, n := range notes {
		if n.RuleID == "LLM-000" {
			t.Errorf("unexpected LLM-000 %q: no check went unrun", n.Why)
		}
	}
	found := 0
	for _, f := range arts[0].Findings {
		if f.Source == model.SrcLLM && f.Dimension > 0 && strings.Contains(f.Evidence[0].Snippet, paddedDirective) {
			found++
		}
	}
	if found == 0 {
		t.Errorf("no grounded LLM finding quotes the directive; findings %+v", arts[0].Findings)
	}
}
