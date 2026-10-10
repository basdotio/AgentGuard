// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/judgefold"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/baselines/run"
	"github.com/basdotio/AgentGuard/internal/model"
)

// writeIncomplete writes the work-list lines of every sample without a complete answer — the
// file the driver's -samples takes to run only those. Written on every fold, empty when nothing
// is left, so a stale list never outlives the fold that would have emptied it.
func writeIncomplete(out string, f *folded) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, s := range f.incomplete {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(out, "incomplete.jsonl"), b.Bytes(), 0o644)
}

// writeAll writes the four files a fold is cited by. Called only once ledger.Check holds.
func writeAll(out string, f *folded, o opts) error {
	var judge bytes.Buffer
	for _, a := range f.answers {
		line, err := judgefold.Encode(a.Row)
		if err != nil {
			return err
		}
		judge.Write(line)
		judge.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(out, "judge.jsonl"), judge.Bytes(), 0o644); err != nil {
		return err
	}

	var vs []corpus.Verdict
	for _, a := range f.answers {
		if a.Verdict != nil {
			vs = append(vs, *a.Verdict)
		}
	}
	var verdicts bytes.Buffer
	if err := corpus.WriteVerdicts(&verdicts, vs); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "verdicts.jsonl"), verdicts.Bytes(), 0o644); err != nil {
		return err
	}

	// Sorted and encoded exactly as the driver writes its ledger, so a fold of a finished run and
	// the run's own ledger.jsonl can be compared byte for byte.
	sorted := make([]ledger.Row, len(f.rows))
	copy(sorted, f.rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sample < sorted[j].Sample })
	var lb bytes.Buffer
	enc := json.NewEncoder(&lb)
	for _, r := range sorted {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(out, "ledger.jsonl"), lb.Bytes(), 0o644); err != nil {
		return err
	}

	table := judgefold.Table(f.answers, judgefold.Meta{
		Inputs: f.inputs, WorkList: len(f.list), Threshold: model.Severity(o.threshold),
		Usage: run.SumJudgeUsage(f.rows),
	})
	return os.WriteFile(filepath.Join(out, "per-kind-rule.txt"), []byte(table), 0o644)
}
