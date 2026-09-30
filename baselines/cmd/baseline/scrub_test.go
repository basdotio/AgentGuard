// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/baselines/adapter"
	"github.com/basdotio/agent-guard/baselines/ledger"
)

// TestResultsCarryNoMachinePaths: every row's and fixture's Detail is scrubbed of the
// corpus checkout, the work directory, the operator's home and the temp directory before
// anything is written — whichever adapter produced it, so a new adapter cannot leak by
// forgetting. Detail is the only ledger field that carries free text.
func TestResultsCarryNoMachinePaths(t *testing.T) {
	corpus, work := t.TempDir(), t.TempDir()
	rows := []ledger.Row{
		{Sample: "a", Outcome: ledger.NoVerdict, Detail: corpus + "/corpus/benign/x — strict mode refuses a tree without SKILL.md"},
		{Sample: "b", Outcome: ledger.Errored, Detail: "exit 2: SKILL.md not found in " + work + "/b/home"},
	}
	fixtures := []adapter.FixtureResult{{Fixture: "fifo-as-skill", Detail: "refused: Path is not a regular file: " + work + "/fixtures/fifo"}}

	scrubResults(rows, fixtures, corpus, work)

	if got := rows[0].Detail; got != "<corpus>/corpus/benign/x — strict mode refuses a tree without SKILL.md" {
		t.Errorf("row a detail = %q", got)
	}
	if got := rows[1].Detail; got != "exit 2: SKILL.md not found in <work>/b/home" {
		t.Errorf("row b detail = %q", got)
	}
	if got := fixtures[0].Detail; !strings.HasSuffix(got, "<work>/fixtures/fifo") {
		t.Errorf("fixture detail = %q", got)
	}
	for _, r := range rows {
		if strings.Contains(r.Detail, work) || strings.Contains(r.Detail, corpus) {
			t.Errorf("a machine path survived: %q", r.Detail)
		}
	}
}
