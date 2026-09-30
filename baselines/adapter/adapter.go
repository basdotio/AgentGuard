// SPDX-License-Identifier: MIT

// Package adapter is the boundary between the neutral half of a baseline run and the part that
// knows one particular scanner.
//
// The corpus's guide puts it plainly: of the three steps in a measurement, step 2 — "point your
// scanner at the path, decide one word" — is the only one the scanner's side has to write. Steps
// 1 and 3 belong to the corpus and know nothing about any tool. An Adapter is step 2, and
// everything a tool-specific file is allowed to decide lives behind this interface.
//
// The one rule that is not negotiable: Scan must return a row for EVERY sample it is handed.
// There is no error return, on purpose. An adapter that could fail without producing a row would
// reintroduce the silent gap the ledger exists to remove — and it is exactly how the current
// hack/corpus-runner loses 127 samples, by returning early before the scanner is ever invoked.
// Whatever happens, say what happened.
package adapter

import (
	"context"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// Adapter measures one scanner.
type Adapter interface {
	// Tool is the id in baselines/tools.yaml.
	Tool() string

	// Version reports what is actually about to be executed. Read from the tool, never
	// assumed: a figure attributed to the wrong version is worse than an unattributed one.
	Version(ctx context.Context) (string, error)

	// Placement says, in a sentence a reader can argue with, where this adapter put each
	// sample and why. It lands verbatim in run.yaml, which baselines/run makes a mandatory
	// field precisely because a figure whose staging nobody can inspect is not evidence.
	//
	// Only the adapter knows this. The driver used to hardcode one tool's answer and print it
	// for every tool, so the first cc-audit run was labelled "by content, into a fake home" —
	// aguard's staging — for a tool that is simply handed the sample tree.
	// A provenance line that describes the wrong tool is worse than none, because it reads as
	// though somebody checked.
	Placement() string

	// Scan points the scanner at one sample and returns that sample's ledger row.
	//
	// tree is the absolute path to the sample directory in the corpus checkout. The adapter
	// decides placement — most scanners want a config ROOT rather than a bare artifact, and
	// getting this wrong is the documented way to measure your own runner instead of your
	// scanner — and records what it decided so the run's metadata can state it.
	//
	// It must always return a row. NoVerdict with a reason is the answer when the scanner ran
	// and produced nothing; Errored with a detail is the answer when the run itself failed.
	Scan(ctx context.Context, s corpus.Sample, tree string) ledger.Row
}

// FixtureStatus is the outcome of one injected-fault fixture. These are per-fixture pass/fail
// and never a rate: a single counterexample settles a robustness claim, so averaging them would
// destroy the only information they carry.
type FixtureStatus string

const (
	FixturePass FixtureStatus = "pass"
	FixtureFail FixtureStatus = "fail"
	// FixtureUntestable is for a fixture this tool cannot be pointed at at all — recorded,
	// never silently omitted.
	FixtureUntestable FixtureStatus = "untestable"
)

// FixtureResult is one fixture's verdict, judged by the adapter rather than by `corpus score`,
// because what counts as passing depends on what the tool promised.
type FixtureResult struct {
	Fixture string        `json:"fixture"`
	Status  FixtureStatus `json:"status"`
	// Detail is the evidence: what the tool did, in enough words to argue with.
	Detail string `json:"detail"`
}

// FixtureRunner is implemented by adapters that can also be pointed at the six injected-fault
// fixtures. It is a separate, optional interface so that a tool which cannot run them reports
// untestable rows rather than the run pretending the fixtures do not exist.
type FixtureRunner interface {
	Fixture(ctx context.Context, name, dir string) FixtureResult
}
