// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
)

// judgedDoc is a scan result shaped like a judged sample: a deterministic high, a judge vote,
// scan-level notes, and a judge summary that reports its own triage count, so the row carries
// every field Scan derives from the bytes (verdict, severity, rules, dimensions, judge_usage).
const judgedDoc = `{
  "root": "/staged/home/.claude",
  "artifacts": [{"kind": "skill", "name": "x", "path": "/staged/home/.claude/skills/x", "score": 60,
    "findings": [
      {"rule_id": "EXE-001", "dimension": 4, "severity": "high", "title": "t", "why": "w", "evidence": [], "source": "static"},
      {"rule_id": "LLM-003", "dimension": 1, "severity": "high", "title": "t", "why": "w [3 of 3 samples agreed]", "evidence": [], "source": "llm", "escalates": true}
    ]}],
  "notes": [{"rule_id": "LLM-000", "dimension": 0, "severity": "low", "title": "t", "why": "w", "evidence": [], "source": "llm"}],
  "judge": {"ran": true, "artifacts": 1, "calls": 7, "failed": 0, "skipped": 0, "findings": 1,
    "triage_calls": 1, "retries": 2, "prompt_tokens": 70, "completion_tokens": 7, "samples": 3}
}
`

// checkDoc is what `check` prints for a bare tree it read: one directory artifact with a finding,
// no judge (the judge never runs on that path).
const checkDoc = `{
  "root": "/corpus/malicious/mcp/srv",
  "artifacts": [{"kind": "directory", "name": "srv", "path": "/corpus/malicious/mcp/srv", "score": 70,
    "findings": [{"rule_id": "EXE-002", "dimension": 4, "severity": "medium", "title": "t", "why": "w", "evidence": [], "source": "static"}]}],
  "notes": []
}
`

// TestRebuild_EqualsScan: a run that died wrote raw/ for the samples it finished and no ledger.
// The fold rebuilds those rows from raw/ — and they must be the rows Scan returned, field for
// field, or a resumed run's ledger differs from an uninterrupted one's for no reason in the data.
// Both routes: `scan --root` on a placed tree, and `check` on a tree that cannot be placed (that
// row carries the secondary-surface flag and the placement reason, which only staging knows).
func TestRebuild_EqualsScan(t *testing.T) {
	cases := []struct {
		name, doc, file, content string
	}{
		{"scan route", judgedDoc, "SKILL.md", "---\nname: x\ndescription: d\n---\nbody\n"},
		{"check route", checkDoc, "server.py", "import os\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, stubAnswering(t, tc.doc))
			a.RawDir = t.TempDir()
			tree := t.TempDir()
			write(t, tree, tc.file, tc.content)
			s := corpus.Sample{Sample: "s", Path: "corpus/x/y/s", Class: "malicious", Surface: []string{"skills"}}

			want := a.Scan(context.Background(), s, tree)
			raw, err := os.ReadFile(filepath.Join(a.RawDir, "s.json"))
			if err != nil {
				t.Fatalf("Scan kept no raw/ (row %+v): %v", want, err)
			}

			// A different adapter value: nothing Scan left in memory may be what makes them equal.
			b := newAdapter(t, filepath.Join(t.TempDir(), "no-such-binary"))
			got, res, err := b.Rebuild(s, tree, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("rebuilt row differs from Scan's:\n got %+v\nwant %+v", got, want)
			}
			if len(res.Artifacts) != 1 {
				t.Errorf("Rebuild returned %d artifacts, want the one raw/ holds", len(res.Artifacts))
			}
		})
	}
}

// TestRebuild_RefusesBytesThatAreNotAScan: a raw file cut short by a dying run is not a scan
// result, and must not become a benign row.
func TestRebuild_RefusesBytesThatAreNotAScan(t *testing.T) {
	a := newAdapter(t, "unused")
	tree := t.TempDir()
	write(t, tree, "SKILL.md", "---\nname: x\n---\n")
	s := corpus.Sample{Sample: "s", Path: "corpus/x/y/s", Class: "benign"}
	if _, _, err := a.Rebuild(s, tree, []byte(`{"artifacts": [{"kind": "sk`)); err == nil {
		t.Error("truncated raw/ rebuilt into a row")
	}
}
