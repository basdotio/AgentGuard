// SPDX-License-Identifier: MIT
package main

import (
	"reflect"
	"testing"
)

// TestScoreArgsCarryTheDeclaringTool: the scorecard is `corpus score`'s own output, so the
// tool's declared out_of_scope section only reaches it if the driver passes -tool. It is opt-in and
// empty by default: a tool the corpus has not registered would make the scorer exit 2, and a tool
// that declares nothing gains nothing from the extra section.
func TestScoreArgsCarryTheDeclaringTool(t *testing.T) {
	if got, want := scoreArgs("/v.jsonl", ""), []string{"run", "./cmd/corpus", "score", "/v.jsonl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no -score-tool: %q, want %q (byte-for-byte the old invocation)", got, want)
	}
	if got, want := scoreArgs("/v.jsonl", "aguard"), []string{"run", "./cmd/corpus", "score", "-tool", "aguard", "/v.jsonl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with -score-tool: %q, want %q", got, want)
	}
}
