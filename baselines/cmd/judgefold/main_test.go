// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJudgefold_ImportsNoNetwork: the fold never calls a model and never re-asks one. The
// strongest form of that available to a test is that no networking package is linked in at all.
func TestJudgefold_ImportsNoNetwork(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable here: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		if p == "net" || strings.HasPrefix(p, "net/") {
			t.Errorf("judgefold links %s", p)
		}
	}
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// miniCorpus lays out three placeable skill samples with their annotations and returns the
// corpus directory and the work list path.
func miniCorpus(t *testing.T) (dir, samples string) {
	t.Helper()
	dir = t.TempDir()
	var list strings.Builder
	for _, id := range []string{"ben-a", "ben-b", "ben-c"} {
		rel := "corpus/benign/skills/" + id
		put(t, filepath.Join(dir, rel, "SKILL.md"), "---\nname: "+id+"\ndescription: d\n---\nbody\n")
		put(t, filepath.Join(dir, rel+".yaml"), "id: "+id+"\nclass: benign\nsurface: skills\norigin:\n  type: harvested\n")
		list.WriteString(`{"sample":"` + id + `","path":"` + rel + `","class":"benign","surface":["skills"]}` + "\n")
	}
	samples = filepath.Join(dir, "samples.jsonl")
	put(t, samples, list.String())
	return dir, samples
}

// rawDoc is a complete samples:1 judge answer with nothing found.
const rawDoc = `{"root":"<work>/x/home/.claude","artifacts":[{"kind":"skill","name":"x","path":"<work>/x/home/.claude/skills/x","score":100,"findings":[]}],"notes":[],"judge":{"ran":true,"artifacts":1,"calls":2,"failed":0,"skipped":0,"findings":0,"triage_calls":0,"retries":0,"samples":1}}`

// TestJudgefold_ResumesAndRefusesAHole: a sample with no raw/ anywhere and no ledger row is a
// hole. The fold says so, writes ONLY the resume list (the driver's "nothing is written until the
// ledger holds"), and exits 1; once a second directory supplies the missing sample, it writes
// everything and exits 0. A no-verdict row from a directory's ledger.jsonl is a final answer:
// the driver keeps no raw/ for a tree aguard read nothing in.
func TestJudgefold_ResumesAndRefusesAHole(t *testing.T) {
	dir, samples := miniCorpus(t)
	run1 := t.TempDir()
	put(t, filepath.Join(run1, "raw", "ben-a.json"), rawDoc)
	put(t, filepath.Join(run1, "ledger.jsonl"), `{"sample":"ben-b","outcome":"no-verdict","attempted":true,"reason":"no-load-path","detail":"read nothing","surface":["skills"]}`+"\n")

	out := t.TempDir()
	var stderr bytes.Buffer
	code := realMain(opts{samples: samples, corpus: dir, out: out, threshold: "high", dirs: []string{run1}}, &stderr)
	if code != 1 {
		t.Fatalf("exit %d with ben-c missing everywhere, want 1; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ben-c") {
		t.Errorf("stderr does not name the missing sample:\n%s", stderr.String())
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 || entries[0].Name() != "incomplete.jsonl" {
		t.Errorf("with a hole in the ledger the fold wrote %v, want only incomplete.jsonl", entries)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "incomplete.jsonl")); !strings.Contains(string(b), `"ben-c"`) || strings.Contains(string(b), `"ben-a"`) {
		t.Errorf("incomplete.jsonl = %s, want ben-c's work-list line only", b)
	}

	run2 := t.TempDir()
	put(t, filepath.Join(run2, "raw", "ben-c.json"), rawDoc)
	stderr.Reset()
	if code := realMain(opts{samples: samples, corpus: dir, out: out, threshold: "high", dirs: []string{run1, run2}}, &stderr); code != 0 {
		t.Fatalf("exit %d with every sample accounted for; stderr:\n%s", code, stderr.String())
	}
	for _, f := range []string{"judge.jsonl", "verdicts.jsonl", "ledger.jsonl", "per-kind-rule.txt", "incomplete.jsonl"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	judge, _ := os.ReadFile(filepath.Join(out, "judge.jsonl"))
	if n := strings.Count(string(judge), "\n"); n != 3 {
		t.Errorf("judge.jsonl has %d rows, want one per work-list sample:\n%s", n, judge)
	}
	if v, _ := os.ReadFile(filepath.Join(out, "verdicts.jsonl")); strings.Contains(string(v), "ben-b") {
		t.Errorf("a no-verdict sample got a verdict line:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "incomplete.jsonl")); len(b) != 0 {
		t.Errorf("incomplete.jsonl = %s, want empty once every answer is complete", b)
	}
}

// TestJudgefold_RefusesARawFileNoSampleOwns: raw/ from another work list is an operator mistake,
// and folding around it silently would leave its sample out of every count.
func TestJudgefold_RefusesARawFileNoSampleOwns(t *testing.T) {
	dir, samples := miniCorpus(t)
	run1 := t.TempDir()
	for _, id := range []string{"ben-a", "ben-b", "ben-c", "ben-stray"} {
		put(t, filepath.Join(run1, "raw", id+".json"), rawDoc)
	}
	var stderr bytes.Buffer
	if code := realMain(opts{samples: samples, corpus: dir, out: t.TempDir(), threshold: "high", dirs: []string{run1}}, &stderr); code != 2 ||
		!strings.Contains(stderr.String(), "ben-stray") {
		t.Errorf("exit %d, stderr %q; want 2 naming the stray file", code, stderr.String())
	}
}
