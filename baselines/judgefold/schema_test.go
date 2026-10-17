// SPDX-License-Identifier: MIT

package judgefold

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSchema_RoundTripsTheCommittedJudgeFiles: the four 2026-09 judge runs were folded by a
// one-off script, and their judge.jsonl is the only schema anyone has cited; the four P-041 runs
// (2026-10-12) were folded by this tool and carry `categories` per vote. Every line of them
// must decode into Row and encode back to the same bytes — key order, empty lists, absent fields
// and all — or the tool writes a file that only looks like the committed one. The two samples:1
// runs carry no votes / triage_calls / questions; those must stay absent, not become null or 0.
// Votes carry no kind there, and an absent kind must stay absent.
func TestSchema_RoundTripsTheCommittedJudgeFiles(t *testing.T) {
	files, err := filepath.Glob("../results/aguard/*-llm-*/judge.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 70 {
		t.Fatalf("found %d committed judge.jsonl files, want the twenty-seven judge runs (four of 2026-09, four each of P-041, P-048 and P-049, five of P-050, the full-benign denominator at samples 3, two of P-053, three of P-054): %v", len(files), files)
	}
	total := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			line := sc.Bytes()
			var r Row
			if err := json.Unmarshal(line, &r); err != nil {
				t.Fatalf("%s:%d does not decode: %v", f, n, err)
			}
			got, err := Encode(r)
			if err != nil {
				t.Fatalf("%s:%d does not encode: %v", f, n, err)
			}
			if !bytes.Equal(got, line) {
				t.Errorf("%s:%d changed in a round trip:\n got %s\nwant %s", f, n, got, line)
			}
			total++
		}
	}
	if total != 32852 {
		t.Errorf("round-tripped %d lines, want 32,852 (819 + 819 + 224 + 224 of 2026-09; 224 + 224 + 819 + 3,220 of P-041; 819 + 3,220 + 541 + 541 of P-048; 541 + 541 + 819 + 3,220 of P-049; 28 + 28 + 100 + 100 + 819 of P-050; 3,220 at samples 3; 127 + 76 of P-053; 76 + 224 + 224 of P-054; 547 + 123 + 123 + 216 + 216 + 120 + 120 + 400 + 400 of P-059; 547 + 127 of P-062; 240 of P-064; 240 + 384 + 384 + 123 + 216 of P-065; 240 + 384 + 123 + 216 of P-066; 240 + 384 + 123 + 216 twice + 216 + 216 of P-068; 384 + 240 + 216 of P-069; 240 + 384 + 216 + 216 twice + 216 of P-070)", total)
	}
}

// TestSchema_NotesKeepTheirOrder: llm_notes is a JSON object whose key order the committed s3
// files do not sort ({"LLM-002":1,"LLM-000":1}); a Go map would sort it on the way out.
func TestSchema_NotesKeepTheirOrder(t *testing.T) {
	var n Notes
	if err := json.Unmarshal([]byte(`{"COV-000":1,"LLM-002":1,"LLM-000":2}`), &n); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"COV-000":1,"LLM-002":1,"LLM-000":2}` {
		t.Errorf("notes re-encoded as %s", b)
	}
	if b, _ := json.Marshal(Notes{}); string(b) != `{}` {
		t.Errorf("empty notes encode as %s, want {}", b)
	}
	if err := json.Unmarshal([]byte(`{"A":1,"A":2}`), &n); err == nil {
		t.Errorf("a repeated note key decoded silently")
	}
}
