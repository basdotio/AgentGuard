// SPDX-License-Identifier: MIT

// Package judgefold folds what the LLM judge found in a benchmark run's raw/ into the files a
// judge run is cited by: judge.jsonl, a verdicts.jsonl at the judge's predicate, and a
// per-(artifact kind, rule) table. It never calls a model, never re-asks one, and never executes
// anything: every input is bytes a run already wrote.
//
// Invariants this package owns:
//
//   - The schema is the committed one. Row encodes the four committed judge.jsonl files back to
//     their own bytes (TestSchema_RoundTripsTheCommittedJudgeFiles); the only addition is a vote's
//     artifact kind, absent when unknown.
//   - One answer per sample, by one rule: the first complete answer in argument order wins, and a
//     retry replaces the whole row — votes from two attempts are never mixed (Select).
//   - Nothing incomplete passes as clean: a judge that failed, skipped or did not run marks the
//     row, and the table counts it instead of folding it (the spirit of invariant #5).
//   - The predicate is the committed runs' `fold:`, word for word, built on score.Deterministic
//     and score.Escalating rather than a second copy of either.
package judgefold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Row is one line of judge.jsonl: one sample, one answer. The field order is the committed files'
// order and must not change. Votes, TriageCalls and Questions are pointers because the two
// samples:1 committed runs do not carry them at all, while the s3 runs carry `[]` and `0`; a
// non-pointer with omitempty would drop the s3 runs' empty values, one without would invent
// values the samples:1 runs never had.
type Row struct {
	Sample         string   `json:"sample"`
	Class          string   `json:"class"`
	Static         bool     `json:"static"`
	Judge          bool     `json:"judge"`
	JudgeAny       bool     `json:"judge_any"`
	EscalatedRules []string `json:"escalated_rules"`
	LLMNotes       Notes    `json:"llm_notes"`
	JudgeCalls     int      `json:"judge_calls"`
	JudgeFailed    int      `json:"judge_failed"`
	Votes          *[]Vote  `json:"votes,omitempty"`
	TriageCalls    *int     `json:"triage_calls,omitempty"`
	Questions      *int     `json:"questions,omitempty"`
	// Incomplete says why this row is not a complete answer; empty when it is. New in this tool:
	// the committed runs marked nothing, and nine of their s3 rows had failed judge calls.
	Incomplete string `json:"incomplete,omitempty"`
}

// Vote is one vote-carrying LLM finding: a question at least one sample flagged. K of N samples
// agreed; Severity is the finding's own (the first agreeing sample's, as the judge's tally sets
// it); Severities lists every agreeing sample's when the binary printed them.
type Vote struct {
	Rule string `json:"rule"`
	// Kind is the aguard artifact kind the finding sits on, from raw/. It is aguard's vocabulary,
	// not the corpus's surface: corpus surface `mcp` (tool catalogues) is aguard kind `connector`,
	// corpus surface `connector` (.mcp.json) is aguard kind `mcp`.
	Kind       string   `json:"kind,omitempty"`
	K          int      `json:"k"`
	N          int      `json:"n"`
	Severity   string   `json:"severity"`
	Escalates  bool     `json:"escalates"`
	Severities []string `json:"severities,omitempty"`
}

// Notes counts scan-level notes by rule id in first-appearance order. A JSON object, but not a
// map: the committed s3 rows keep their notes unsorted ({"LLM-002":1,"LLM-000":1}) and a Go map
// would sort them on the way out.
type Notes []NoteCount

// NoteCount is one rule id and how many notes carried it.
type NoteCount struct {
	Rule  string
	Count int
}

// Add counts one more note of rule, keeping the order rules first appeared in.
func (n Notes) Add(rule string) Notes {
	out := make(Notes, len(n), len(n)+1)
	copy(out, n)
	for i := range out {
		if out[i].Rule == rule {
			out[i].Count++
			return out
		}
	}
	return append(out, NoteCount{Rule: rule, Count: 1})
}

// String renders the counts as "RULE:n RULE:n", for messages and tests.
func (n Notes) String() string {
	parts := make([]string, len(n))
	for i, c := range n {
		parts[i] = fmt.Sprintf("%s:%d", c.Rule, c.Count)
	}
	return strings.Join(parts, " ")
}

// MarshalJSON writes the counts as an object in their own order.
func (n Notes) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, c := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(c.Rule)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		fmt.Fprintf(&b, ":%d", c.Count)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads an object of counts in the order its keys appear. A repeated key is an
// error: two counts for one rule cannot both be the count.
func (n *Notes) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return fmt.Errorf("llm_notes is not an object")
	}
	out := Notes{}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		rule, ok := t.(string)
		if !ok {
			return fmt.Errorf("llm_notes key %v is not a string", t)
		}
		var c int
		if err := dec.Decode(&c); err != nil {
			return fmt.Errorf("llm_notes[%s]: %w", rule, err)
		}
		if seen[rule] {
			return fmt.Errorf("llm_notes repeats %s", rule)
		}
		seen[rule] = true
		out = append(out, NoteCount{Rule: rule, Count: c})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*n = out
	return nil
}

// Encode is one judge.jsonl line without its newline. HTML escaping is off, so a sample id is
// written as the work list spells it, as the committed files were.
func Encode(r Row) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
