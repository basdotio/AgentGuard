// SPDX-License-Identifier: MIT

// Package corpus is the wire format between agent-artifact-corpus and an adapter here, and
// nothing else. It knows how to read the work list `corpus samples` emits and how to write the
// verdict file `corpus score` reads; it holds no opinion about any scanner, because the moment
// it did it would stop being the neutral half.
//
// The formats are not ours to change. `corpus score` parses exactly four fields and refuses
// anything else WITH A LINE NUMBER — the corpus's guide is explicit that a scorer which
// silently drops a record reports a rate over an unknown denominator, which is worse than not
// running. So reading is strict here too: a malformed work-list line is an error naming the
// line, never a skipped sample.
package corpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// maxLine bounds one JSONL record. The work list's longest lines are sample paths and surface
// lists; a megabyte is far past anything legitimate and stops a corrupt file from being read
// into memory unbounded.
const maxLine = 1 << 20

// Sample is one line of `corpus samples`: the work list a runner iterates. Field names and
// meanings are the corpus's, documented in its docs/using-the-corpus.md.
type Sample struct {
	// Sample is the id that must come back verbatim in the verdict.
	Sample string `json:"sample"`
	// Path is the sample tree, relative to the corpus checkout — point the scanner here.
	Path string `json:"path"`
	// Class is malicious | benign | hard-negative. The answer, published on purpose.
	Class string `json:"class"`
	// Surface is which load path(s) this artifact sits on. A list, because one file can be
	// on two.
	Surface []string `json:"surface"`
	// Severity is the truth severity, on malicious samples only.
	Severity string `json:"severity,omitempty"`
	// Kind is optional and advisory. It must NOT decide placement: the corpus labels 353 tool
	// catalogues as kind `skill`, so placement is decided by content.
	Kind string `json:"kind,omitempty"`
}

// Verdict is one line of what `corpus score` parses: exactly these four fields, two optional.
// Anything else is refused by the scorer, so nothing else may be emitted.
type Verdict struct {
	Sample     string   `json:"sample"`
	Verdict    string   `json:"verdict"`
	Severity   string   `json:"severity,omitempty"`
	Dimensions []string `json:"dimensions,omitempty"`
}

// ReadSamples parses the work list. It fails on the first malformed line rather than skipping
// it: a work list read as 3,400 lines when it has 3,539 gives every downstream figure a wrong
// denominator, and nothing later in the pipeline can notice.
func ReadSamples(r io.Reader) ([]Sample, error) {
	var out []Sample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, maxLine), maxLine)
	seen := map[string]bool{}
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("work list line %d is not JSON: %w", n, err)
		}
		switch {
		case s.Sample == "":
			return nil, fmt.Errorf("work list line %d has no sample id", n)
		case s.Path == "":
			return nil, fmt.Errorf("work list line %d (%s) has no path", n, s.Sample)
		case seen[s.Sample]:
			// A duplicated id makes two different samples share one verdict slot.
			return nil, fmt.Errorf("work list line %d repeats sample id %q", n, s.Sample)
		}
		seen[s.Sample] = true
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read work list: %w", err)
	}
	return out, nil
}

// IDs returns the sample ids in work-list order — what ledger.Check takes.
func IDs(samples []Sample) []string {
	out := make([]string, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Sample)
	}
	return out
}

// WriteVerdicts emits one object per line, sorted by sample id so two runs diff cleanly. That
// ordering is load-bearing for the adapter's reverse assertion: "the 3,412 existing verdicts are
// byte-identical" is only checkable if the file order does not depend on scheduling.
func WriteVerdicts(w io.Writer, vs []Verdict) error {
	sorted := make([]Verdict, len(vs))
	copy(sorted, vs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sample < sorted[j].Sample })
	enc := json.NewEncoder(w)
	for _, v := range sorted {
		if err := enc.Encode(v); err != nil {
			return fmt.Errorf("write verdict %s: %w", v.Sample, err)
		}
	}
	return nil
}
