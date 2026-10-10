// SPDX-License-Identifier: MIT

package corpus

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func annotate(t *testing.T, root, rel, yaml string) Sample {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel)+".yaml")
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimPrefix(strings.SplitN(yaml, "\n", 2)[0], "id: ")
	return Sample{Sample: id, Path: rel}
}

// TestSource_EachArmOfTheCorpusRule: a sample's source is what the corpus's scorecard groups by
// and what `corpus samples --source` filters on (populationOf in its harness/cmd/corpus/basis.go),
// not the free text of origin.source, which is a per-sample URL for most samples.
func TestSource_EachArmOfTheCorpusRule(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		rel, yaml, want string
	}{
		{"corpus/benign/skills/a", "id: ben-a\nclass: benign\nsurface: skills\norigin:\n  type: derived\n" +
			"  source: \"https://github.com/o/r (via skillmd-138k @ abc)\"\n  derived_from:\n    entry: skillmd-138k\n", "skillmd-138k"},
		{"corpus/benign/hooks/b", "id: ben-b\nclass: benign\nsurface: hooks\norigin:\n  type: harvested\n  source: x\n", "harvested"},
		{"corpus/hard-negative/hooks/c", "id: hn-c\nclass: hard-negative\nsurface: [hooks, permission]\norigin:\n  type: harvested\n", "promoted-hard-negative"},
		{"corpus/malicious/connector/d", "id: mal-d\nclass: malicious\nsurface: [connector, mcp]\norigin:\n  type: reconstruction\n  source: CVE\n", "reconstruction:connector"},
		{"corpus/malicious/skills/e", "id: mal-e\nclass: malicious\nsurface: skills\norigin:\n  type: synthetic\n", "synthetic-malicious"},
		{"corpus/malicious/skills/f", "id: mal-f\nclass: malicious\nsurface: skills\norigin:\n  type: real-world\n" +
			"  source: \"see https://github.com/NVIDIA/SkillSpector/tree/main (fixtures)\"\n", "NVIDIA/SkillSpector"},
		{"corpus/malicious/skills/g", "id: mal-g\nclass: malicious\nsurface: skills\norigin:\n  type: real-world\n  source: a blog post\n", "hand-written"},
	}
	for _, tc := range cases {
		s := annotate(t, root, tc.rel, tc.yaml)
		a, err := ReadAnnotation(root, s)
		if err != nil {
			t.Fatalf("%s: %v", tc.rel, err)
		}
		if got := a.Source(); got != tc.want {
			t.Errorf("%s: source = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

// TestSource_TheAnnotationMustBeTheSamples: a work list pointing at another sample's annotation
// would group a sample under someone else's source.
func TestSource_TheAnnotationMustBeTheSamples(t *testing.T) {
	root := t.TempDir()
	s := annotate(t, root, "corpus/benign/skills/a", "id: ben-a\nclass: benign\nsurface: skills\norigin:\n  type: harvested\n")
	s.Sample = "ben-other"
	if _, err := ReadAnnotation(root, s); err == nil {
		t.Error("an annotation with another id was accepted")
	}
	if _, err := ReadAnnotation(root, Sample{Sample: "x", Path: "corpus/benign/skills/missing"}); err == nil {
		t.Error("a missing annotation was accepted")
	}
}

// TestSource_AgreesWithTheCorpus: the port is kept in step by hand, like DimensionMap. Given a
// corpus checkout (AGUARD_CORPUS), every source it assigns must select exactly the samples the
// corpus's own `corpus samples --source <s>` prints.
func TestSource_AgreesWithTheCorpus(t *testing.T) {
	dir := os.Getenv("AGUARD_CORPUS")
	if dir == "" {
		t.Skip("set AGUARD_CORPUS to an agent-artifact-corpus checkout to compare the port with the corpus's own rule")
	}
	all := corpusSamples(t, dir)
	bySource := map[string][]string{}
	for _, s := range all {
		a, err := ReadAnnotation(dir, s)
		if err != nil {
			t.Fatal(err)
		}
		bySource[a.Source()] = append(bySource[a.Source()], s.Sample)
	}
	for src, ids := range bySource {
		var want []string
		for _, s := range corpusSamples(t, dir, "--source", src) {
			want = append(want, s.Sample)
		}
		sort.Strings(ids)
		sort.Strings(want)
		if strings.Join(ids, " ") != strings.Join(want, " ") {
			t.Errorf("source %q: the port assigns %d samples, the corpus %d", src, len(ids), len(want))
		}
	}
	t.Logf("%d samples, %d sources, all equal", len(all), len(bySource))
}

func corpusSamples(t *testing.T, dir string, args ...string) []Sample {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "./cmd/corpus", "samples"}, args...)...)
	cmd.Dir = filepath.Join(dir, "harness")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("corpus samples %v: %v", args, err)
	}
	list, err := ReadSamples(strings.NewReader(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	return list
}
