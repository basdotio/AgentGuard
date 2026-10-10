// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// jsonSpace is the whitespace encoding/json skips between tokens.
const jsonSpace = " \t\r\n"

// closingFence is the one thing besides whitespace a repaired object may be followed by.
const closingFence = "```"

// parseVerdict reads the model's verdict from its reply: ONE JSON object, tolerating prose or a
// code fence around it. The reading is the one it always was — the text from the first `{` to the
// last `}` (sliceOutermost) — with one repair (P-034).
//
// The model sometimes closes its object one member early with a stray `}` and keeps writing:
// `{"flagged": false, …, "evidence": "…"}, "barrier_evidence": ""}` (measured: 5 of 1,482 calls,
// each counted as failed with an LLM-000 saying the check did not run). Every member is there and
// there is one answer, so that reply is read by dropping that brace — and only when the joined
// text is one object, followed by nothing but whitespace or a closing fence, that names no member
// twice. Any other text after the first object that starts with `,` is refused, never read as its
// first half: the second half is where a different `flagged` or a barrier quote would sit, and the
// judge may only add (invariant #4) — a misread must fail visibly, not decide.
func parseVerdict(content string) (Verdict, error) {
	v, err := sliceOutermost(content)
	first, rest, ok := firstObject(content)
	if !ok || !strings.HasPrefix(strings.TrimLeft(rest, jsonSpace), ",") {
		// The first object IS the outermost slice, or that slice fails: either way, its answer.
		return v, err
	}
	rv, rerr := joinClosedEarly(first, rest)
	if rerr == nil {
		rv.repaired = true
		return rv, nil
	}
	if err != nil {
		return Verdict{}, err // refused with the error it always had
	}
	// The outermost slice ends at the first object's brace, because the rest lost its own:
	// reading it would be reading the first half.
	return Verdict{}, fmt.Errorf("parse judge verdict: the object was closed before the reply ended, "+
		"and the rest of the reply does not complete it: %w", rerr)
}

// sliceOutermost is the reading parseVerdict always had: the outermost `{ … }`, unmarshalled.
func sliceOutermost(content string) (Verdict, error) {
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

// firstObject splits the reply after the first complete JSON value that starts at its first `{`:
// that object's text, and everything after it. ok is false when there is no such value.
func firstObject(content string) (first, rest string, ok bool) {
	start := strings.IndexByte(content, '{')
	if start < 0 {
		return "", "", false
	}
	dec := json.NewDecoder(strings.NewReader(content[start:]))
	var raw json.RawMessage
	if dec.Decode(&raw) != nil {
		return "", "", false
	}
	end := start + int(dec.InputOffset())
	return content[start:end], content[end:], true
}

// joinClosedEarly reads `first` without its closing brace, followed by `rest`, as one object. It
// refuses unless that is exactly one object, followed by nothing but whitespace and at most a
// closing fence, whose members are all named once.
func joinClosedEarly(first, rest string) (Verdict, error) {
	joined := first[:len(first)-1] + rest // first ends at its `}`: firstObject starts it at a `{`
	dec := json.NewDecoder(strings.NewReader(joined))
	var obj json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return Verdict{}, err
	}
	if tail := strings.Trim(joined[dec.InputOffset():], jsonSpace); tail != "" && tail != closingFence {
		return Verdict{}, errors.New("text follows the joined object")
	}
	name, repeated, err := repeatedMember(obj)
	if err != nil {
		return Verdict{}, err
	}
	if repeated {
		return Verdict{}, fmt.Errorf("member %q is named twice", name)
	}
	var v Verdict
	if err := json.Unmarshal(obj, &v); err != nil {
		return Verdict{}, err
	}
	return v, nil
}

// repeatedMember reports the first top-level member name that repeats an earlier one.
// Names are compared the way encoding/json matches a member to a struct field — without case —
// so "Flagged" after "flagged" is a second answer to the same question, not a new member.
func repeatedMember(obj []byte) (name string, repeated bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil { // the opening `{`
		return "", false, err
	}
	var seen []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", false, err
		}
		key, _ := tok.(string)
		for _, s := range seen {
			if strings.EqualFold(s, key) {
				return key, true, nil
			}
		}
		seen = append(seen, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return "", false, err
		}
	}
	return "", false, nil
}
