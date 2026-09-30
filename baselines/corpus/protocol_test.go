// SPDX-License-Identifier: MIT

package corpus

import (
	"bytes"
	"strings"
	"testing"
)

const twoGood = `{"sample":"ben-a","path":"corpus/benign/skills/a","class":"benign","surface":["skills"]}
{"sample":"mal-b","path":"corpus/malicious/mcp/b","class":"malicious","surface":["mcp"],"severity":"high"}
`

// TestReadSamplesRefusesRatherThanSkips is the whole point of this reader. A work list read as
// two lines when it has three gives every downstream figure a wrong denominator, and nothing
// later in the pipeline can notice — which is the 11.0%-that-was-12.9% mistake in miniature.
func TestReadSamplesRefusesRatherThanSkips(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"not JSON", twoGood + "this is not json\n", "line 3"},
		{"no sample id", twoGood + `{"path":"corpus/x"}` + "\n", "no sample id"},
		{"no path", twoGood + `{"sample":"mal-c","class":"malicious"}` + "\n", "no path"},
		{
			// Two samples sharing an id means they share one verdict slot, so one of them is
			// silently unmeasured no matter what the runner does.
			name: "a repeated sample id",
			in:   twoGood + `{"sample":"ben-a","path":"corpus/benign/skills/dup","class":"benign"}` + "\n",
			want: "repeats sample id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadSamples(strings.NewReader(tt.in))
			if err == nil {
				t.Fatalf("ReadSamples accepted a bad work list and returned %d samples", len(got))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not mention %q: %v", tt.want, err)
			}
		})
	}
}

func TestReadSamplesKeepsEveryFieldAndIgnoresBlankLines(t *testing.T) {
	got, err := ReadSamples(strings.NewReader("\n" + twoGood + "\n"))
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d samples, want 2", len(got))
	}
	if got[1].Sample != "mal-b" || got[1].Class != "malicious" || got[1].Severity != "high" {
		t.Errorf("second record lost a field: %+v", got[1])
	}
	if len(got[1].Surface) != 1 || got[1].Surface[0] != "mcp" {
		t.Errorf("surface not read: %+v", got[1].Surface)
	}
	if ids := IDs(got); ids[0] != "ben-a" || ids[1] != "mal-b" {
		t.Errorf("IDs = %v, want work-list order", ids)
	}
}

// TestWriteVerdictsSortsBySample — the reverse assertion is that the existing verdicts stay
// BYTE-IDENTICAL while 127 rows are added elsewhere. That is only checkable if the file's order
// does not depend on which worker finished first.
func TestWriteVerdictsSortsBySample(t *testing.T) {
	var buf bytes.Buffer
	in := []Verdict{
		{Sample: "mal-b", Verdict: "malicious", Severity: "critical", Dimensions: []string{"execution"}},
		{Sample: "ben-a", Verdict: "benign"},
	}
	if err := WriteVerdicts(&buf, in); err != nil {
		t.Fatalf("WriteVerdicts: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"ben-a"`) {
		t.Errorf("output is not sorted by sample id:\n%s", buf.String())
	}
	// The scorer parses exactly four fields; an omitted optional must be absent, not null.
	if strings.Contains(lines[0], "severity") || strings.Contains(lines[0], "dimensions") {
		t.Errorf("a benign verdict carries empty optional fields:\n%s", lines[0])
	}
	if in[0].Sample != "mal-b" {
		t.Error("WriteVerdicts reordered the caller's slice in place")
	}
}
