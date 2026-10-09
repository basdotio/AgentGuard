// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// PromptVersion is computed rather than maintained, and these tests are what make "computed" mean
// "covers what is sent": the capture test drives the real client over every call shape and requires
// each request body to be the one that was hashed, so a prompt, a request field or an encoder
// setting can change only by moving the version or by turning that test red (P-031).

// artifactNotSent stands in for Request.Artifact, which labels the call in reports and must never
// reach a request body.
const artifactNotSent = "<artifact label, never sent>"

var nonceInBody = regexp.MustCompile(`AGUARD:[0-9a-f]{32}===`)

// captureBodies drives a real HTTPClient over each shape against an httptest endpoint and returns
// each request body exactly as it arrived, with only the per-call nonce replaced by the stand-in.
func captureBodies(t *testing.T, shapes []callShape) [][]byte {
	t.Helper()
	var mu sync.Mutex
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = b
		mu.Unlock()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false,\"labels\":[]}"}}]}`))
	}))
	defer srv.Close()
	c := NewHTTP(srv.URL, "", modelStandIn, srv.Client())
	out := make([][]byte, len(shapes))
	for i, s := range shapes {
		var err error
		if s.triage != nil {
			_, err = c.Triage(context.Background(), artifactNotSent, s.triage)
		} else {
			req := s.req
			req.Artifact = artifactNotSent
			_, err = c.Judge(context.Background(), req)
		}
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		mu.Lock()
		out[i] = nonceInBody.ReplaceAll(last, []byte("AGUARD:"+nonceStandIn+"==="))
		mu.Unlock()
	}
	return out
}

// TestPromptVersion_HashesWhatTheClientSends: for every call shape the body the client really sends,
// nonce masked, is byte for byte the body PromptVersion hashed. The triage messages are restated in
// prompt_version.go (Triage builds them inline), and this is what keeps the restatement honest.
func TestPromptVersion_HashesWhatTheClientSends(t *testing.T) {
	shapes := callShapes()
	sent := captureBodies(t, shapes)
	hashed := shapeBodies(shapes)
	for i, s := range shapes {
		if !bytes.Equal(sent[i], hashed[i]) {
			t.Errorf("%s: the client sent\n%s\nbut PromptVersion hashed\n%s\nchange prompt_version.go so it renders what the client sends",
				s.name, sent[i], hashed[i])
		}
		if bytes.Contains(sent[i], []byte(artifactNotSent)) {
			t.Errorf("%s: Request.Artifact reached the request body; it is declared as never sent", s.name)
		}
	}
	if got := promptVersion(sent); got != PromptVersion() {
		t.Errorf("the version over the captured bodies is %s, PromptVersion() is %s", got, PromptVersion())
	}
}

// Decisions for every input a request is built from. A field nobody has decided about is a field
// that could change what is sent while the version stays put.
const (
	standIn    = "content or model: replaced by a fixed stand-in"
	enumerated = "every value the client uses has its own shapes"
	rendered   = "part of the body that is hashed"
	notSent    = "never in a request body"
)

// TestPromptVersion_EveryInputIsDecided forces a new input to be declared: every field of Request,
// TriageItem and chatRequest is on a list, and every Mode the source declares has shapes for both
// temperatures and all four content variants. Mutation: a new Request field, or a seventh Mode
// constant, without a decision here → red.
func TestPromptVersion_EveryInputIsDecided(t *testing.T) {
	for _, c := range []struct {
		typ    reflect.Type
		fields map[string]string
	}{
		{reflect.TypeOf(Request{}), map[string]string{
			"Artifact": notSent, "Mode": enumerated, "Declared": standIn, "Behavior": standIn, "Temperature": enumerated}},
		{reflect.TypeOf(TriageItem{}), map[string]string{"RuleID": standIn, "Evidence": standIn}},
		{reflect.TypeOf(chatRequest{}), map[string]string{"Model": standIn, "Messages": rendered, "Temperature": rendered}},
		{reflect.TypeOf(chatMessage{}), map[string]string{"Role": rendered, "Content": rendered}},
	} {
		for i := 0; i < c.typ.NumField(); i++ {
			if name := c.typ.Field(i).Name; c.fields[name] == "" {
				t.Errorf("%s.%s is not decided for the prompt version — say whether it is a stand-in, enumerated, rendered "+
					"or never sent (prompt_version.go), then list it here", c.typ.Name(), name)
			}
		}
		if n := c.typ.NumField(); n != len(c.fields) {
			t.Errorf("%s has %d fields but the list names %d — a listed field no longer exists", c.typ.Name(), n, len(c.fields))
		}
	}

	declared := modeConstants(t)
	if len(declared) != len(passModes) {
		t.Fatalf("judge.go declares %d Mode constants %v, the call shapes cover %d — add the new pass to passModes",
			len(declared), declared, len(passModes))
	}
	for i, m := range passModes {
		if m != Mode(i) {
			t.Errorf("passModes[%d] = %d; it must list every Mode in declaration order", i, m)
		}
	}
	type key struct {
		mode Mode
		temp float64
	}
	variants := map[key]int{}
	triage := 0
	for _, s := range callShapes() {
		if s.triage != nil {
			triage++
			if len(s.triage) <= maxTriageItems {
				t.Errorf("%s carries %d items; it must exceed the cap (%d) so the cap is in what is hashed", s.name, len(s.triage), maxTriageItems)
			}
			continue
		}
		variants[key{s.req.Mode, s.req.Temperature}]++
	}
	for _, m := range passModes {
		for _, temp := range []float64{0, samplingTemperature} {
			if n := variants[key{m, temp}]; n != 4 {
				t.Errorf("mode %d at temperature %v has %d shapes, want 4 (both sides, declared empty, behavior empty, both empty)", m, temp, n)
			}
		}
	}
	if triage != 1 {
		t.Errorf("%d triage shapes, want 1", triage)
	}
}

// modeConstants reads the names of the Mode constants from judge.go, in declaration order.
func modeConstants(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "judge.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST || len(g.Specs) == 0 {
			continue
		}
		first, ok := g.Specs[0].(*ast.ValueSpec)
		if id, isIdent := first.Type.(*ast.Ident); !ok || !isIdent || id.Name != "Mode" {
			continue
		}
		for _, s := range g.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("judge.go declares no Mode constants; point modeConstants at the file that does")
	}
	return names
}

// TestPromptVersion_MovesWithWhatShapesTheAnswer: each kind of change to what is sent moves the
// version, and no two of them collide.
func TestPromptVersion_MovesWithWhatShapesTheAnswer(t *testing.T) {
	shapes := callShapes()
	base := shapeBodies(shapes)
	want := promptVersion(base)
	first := func(pred func(callShape) bool) int {
		for i, s := range shapes {
			if pred(s) {
				return i
			}
		}
		t.Fatal("no shape matches")
		return -1
	}
	injection := first(func(s callShape) bool { return s.triage == nil && s.req.Mode == ModeInjection })
	intent := first(func(s callShape) bool { return s.triage == nil && s.req.Mode == ModeIntent })
	triage := first(func(s callShape) bool { return s.triage != nil })
	rerender := func(i int, s callShape) func([][]byte) [][]byte {
		return func(bs [][]byte) [][]byte { bs[i] = shapeBodies([]callShape{s})[0]; return bs }
	}
	// The edits below locate what they change by the request envelope only, never by prompt text, so
	// rewording a prompt needs no change here: the version simply moves.
	toggle := func(i int, marker string) func([][]byte) [][]byte {
		return func(bs [][]byte) [][]byte {
			at := bytes.Index(bs[i], []byte(marker))
			if at < 0 {
				t.Fatalf("shape %s has no %s", shapes[i].name, marker)
			}
			for k := at + len(marker); k < len(bs[i]); k++ {
				if c := bs[i][k]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
					bs[i][k] ^= 0x20 // flip its case: one byte of the message changes
					return bs
				}
			}
			t.Fatalf("shape %s has no letter after %s", shapes[i].name, marker)
			return bs
		}
	}
	const system, user = `"role":"system","content":"`, `"role":"user","content":"`
	warmer := shapes[intent]
	warmer.req.Temperature = 0.7
	shorter := shapes[triage]
	shorter.triage = shorter.triage[:maxTriageItems-1]

	seen := map[string]string{want: "the shapes as they are"}
	for _, m := range []struct {
		name   string
		mutate func([][]byte) [][]byte
	}{
		{"one byte of a pass's system message", toggle(injection, system)},
		{"one byte of the user layout", toggle(intent, user)},
		{"one byte of the triage system message", toggle(triage, system)},
		{"a temperature", rerender(intent, warmer)},
		{"the triage cap", rerender(triage, shorter)},
		{"a request field", func(bs [][]byte) [][]byte { bs[intent] = append([]byte(`{"x":1,`), bs[intent][1:]...); return bs }},
		{"a shape dropped", func(bs [][]byte) [][]byte { return bs[1:] }},
		{"two shapes swapped", func(bs [][]byte) [][]byte { bs[0], bs[1] = bs[1], bs[0]; return bs }},
	} {
		cp := make([][]byte, len(base))
		for i, b := range base {
			cp[i] = append([]byte(nil), b...)
		}
		got := promptVersion(m.mutate(cp))
		if prev, dup := seen[got]; dup {
			t.Errorf("%s gives version %s, the same as %s", m.name, got, prev)
		}
		seen[got] = m.name
	}
}

// TestPromptVersion_IsStable: a short lowercase hex value, the same on every call, and the pure
// function over the built-in shapes.
func TestPromptVersion_IsStable(t *testing.T) {
	v := PromptVersion()
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(v) {
		t.Errorf("PromptVersion() = %q, want 12 lowercase hex digits", v)
	}
	if v != PromptVersion() || v != promptVersion(shapeBodies(callShapes())) {
		t.Error("PromptVersion is not stable, or not the version of the built-in call shapes")
	}
	for _, b := range shapeBodies(callShapes()) {
		if !strings.Contains(string(b), nonceStandIn) || !strings.Contains(string(b), modelStandIn) {
			t.Fatalf("a hashed body lacks the nonce or model stand-in:\n%s", b)
		}
	}
}
