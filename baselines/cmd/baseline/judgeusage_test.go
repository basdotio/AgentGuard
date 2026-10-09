// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/baselines/run"
)

func meta(rows []ledger.Row) run.Run {
	return run.Run{
		Tool: "aguard", ToolVersion: "aguard 0.0.0-fixture", Threshold: "high",
		UploadsBasis: "fixture", CorpusCommit: "0000000", StartedAt: time.Unix(0, 0).UTC(),
		Adapter: "agent-guard baselines/adapter/aguard", Placement: "fixture",
		Counts: ledger.Tally(rows), WorkListSize: len(rows),
		JudgeUsage: run.SumJudgeUsage(rows),
	}
}

// TestWriteAllCarriesTheJudgeUsage: the two files a judge run is cited from carry its cost —
// per sample in ledger.jsonl, summed with its basis in run.yaml — and a static run's two files
// carry no trace of it.
func TestWriteAllCarriesTheJudgeUsage(t *testing.T) {
	retries := 0
	judgedRows := []ledger.Row{{Sample: "a", Outcome: ledger.Scored, Attempted: true, Verdict: "benign",
		JudgeUsage: &ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 13, Skipped: 1, TriageCalls: 1, Retries: &retries}}}
	staticRows := []ledger.Row{{Sample: "a", Outcome: ledger.Scored, Attempted: true, Verdict: "benign"}}

	for _, tc := range []struct {
		name string
		rows []ledger.Row
		want bool
	}{{"judge run", judgedRows, true}, {"static run", staticRows, false}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writeAll(dir, tc.rows, meta(tc.rows)); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"ledger.jsonl", "run.yaml"} {
				b, err := os.ReadFile(filepath.Join(dir, f))
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.Contains(string(b), "judge_usage"); got != tc.want {
					t.Errorf("%s mentions judge_usage = %v, want %v:\n%s", f, got, tc.want, b)
				}
			}
		})
	}
}
