// SPDX-License-Identifier: MIT

package run

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// complete is a well-formed run used to test Validate. The tool name and version are
// DELIBERATELY not a real tool's: a fixture reading like `cc-audit 3.23.9` would look like a
// version somebody had observed. A test
// fixture that could be mistaken for evidence is worse than an ugly one.
func complete() Run {
	return Run{
		Tool:         "example-scanner",
		ToolVersion:  "example-scanner 0.0.0-fixture",
		Threshold:    "high",
		CorpusCommit: "7e924371",
		StartedAt:    time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC),
		Adapter:      "agent-guard baselines/adapter/example-scanner",
		Placement:    "per corpus `surface`, staged as the tool's docs describe",
		UploadsBasis: "invariant, enforced by tests in this repository",
		Provisional:  true,
		WorkListSize: 3539,
		Counts:       ledger.Counts{Scored: 3412, NoVerdict: 127},
	}
}

// TestValidateRequiresTheAttributionFields is the enforcement of the cost of one decision.
// Results now live in the repository of the tool being measured, so "whose adapter, whose
// threshold, whose placement" can no longer be answered by where the file sits. It has to be a
// field, and an optional field would be back to being a README sentence.
func TestValidateRequiresTheAttributionFields(t *testing.T) {
	tests := []struct {
		name   string
		mangle func(*Run)
		want   string
	}{
		{"no adapter", func(r *Run) { r.Adapter = "" }, "adapter"},
		{"blank adapter", func(r *Run) { r.Adapter = "   " }, "adapter"},
		{"no placement", func(r *Run) { r.Placement = "" }, "placement"},
		// The bare boolean is the most quotable line in the file and the easiest to state
		// without evidence; an earlier run published one for cc-audit on a partial source read.
		{"no uploads basis", func(r *Run) { r.UploadsBasis = "" }, "uploads_samples_basis"},
		{"no tool", func(r *Run) { r.Tool = "" }, "tool"},
		{"no tool version", func(r *Run) { r.ToolVersion = "" }, "tool_version"},
		{"no threshold", func(r *Run) { r.Threshold = "" }, "threshold"},
		{"no corpus commit", func(r *Run) { r.CorpusCommit = "" }, "corpus_commit"},
		{"no start time", func(r *Run) { r.StartedAt = time.Time{} }, "started_at"},
		{"no work list size", func(r *Run) { r.WorkListSize = 0 }, "work_list_size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := complete()
			tt.mangle(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a run with no %s", tt.want)
			}
			if !errors.Is(err, ErrIncomplete) {
				t.Errorf("error does not wrap ErrIncomplete: %v", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not name %q: %v", tt.want, err)
			}
		})
	}
}

// TestValidateRefusesAPartitionThatDoesNotCoverTheWorkList — this is the same invariant
// ledger.Check enforces per sample, checked again on the summary, because run.yaml is what gets
// read months later and a summary that disagrees with its own ledger is worse than no summary.
func TestValidateRefusesAPartitionThatDoesNotCoverTheWorkList(t *testing.T) {
	r := complete()
	r.Counts = ledger.Counts{Scored: 3400, NoVerdict: 100} // 39 points unaccounted for
	err := r.Validate()
	if err == nil {
		t.Fatal("Validate accepted a run that lost 39 test points")
	}
	for _, want := range []string{"3500", "3539"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not state %s, so the reader cannot see the size of the gap: %v", want, err)
		}
	}
}

// TestValidateAcceptsACompleteRun is the reverse assertion: the checks above must leave a
// well-formed run able to exist.
func TestValidateAcceptsACompleteRun(t *testing.T) {
	if err := complete().Validate(); err != nil {
		t.Fatalf("Validate rejected a complete run: %v", err)
	}
}

// TestSignatureNamesWhatIsOurs — the signature exists to be printed above a figure. If it does
// not say whose adapter and whose threshold, printing it achieves nothing.
func TestSignatureNamesWhatIsOurs(t *testing.T) {
	got := complete().Signature()
	for _, want := range []string{"adapter", "threshold", "placement", "PROVISIONAL"} {
		if !strings.Contains(got, want) {
			t.Errorf("signature does not mention %q: %q", want, got)
		}
	}
}

// TestOurOwnRunIsNotProvisional — provisional means "the vendor has not measured itself yet".
// aguard measuring aguard has no vendor to wait for, so the wording must not appear; a warning
// printed everywhere is a warning nobody reads.
func TestOurOwnRunIsNotProvisional(t *testing.T) {
	r := complete()
	r.Tool = "aguard"
	r.Provisional = false
	if strings.Contains(r.Signature(), "PROVISIONAL") {
		t.Error("our own run claims to be provisional")
	}
	if !strings.Contains(r.Signature(), "adapter") {
		t.Error("our own run dropped the attribution half of the signature")
	}
}

// TestYAMLRoundTripKeepsTheAttribution — run.yaml is the file read six months later, and it is
// written by one program and read by a person. A field that silently fails to serialise takes
// the attribution with it, so the round trip is asserted rather than assumed.
func TestYAMLRoundTripKeepsTheAttribution(t *testing.T) {
	in := complete()
	in.PolicyHash = "sha256:deadbeef"
	in.Declarations = map[string]string{"hooks": "does not read settings.json"}

	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Run
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("a field did not survive the round trip:\n in: %+v\nout: %+v\n%s", in, out, b)
	}
	// The fields a reader needs must be findable by eye in the file, not just by a parser.
	for _, want := range []string{"adapter:", "placement:", "provisional:", "corpus_commit:",
		"tool_version:", "threshold:", "policy_hash:", "uploads_samples:", "work_list_size:"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("run.yaml has no %s line:\n%s", want, b)
		}
	}
	// And it must still be a valid run after the trip, or Validate is only ever checked on
	// something that was never written down.
	if err := out.Validate(); err != nil {
		t.Errorf("the round-tripped run no longer validates: %v", err)
	}
}
