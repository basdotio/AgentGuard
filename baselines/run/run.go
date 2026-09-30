// SPDX-License-Identifier: MIT

// Package run holds the metadata that travels with a set of numbers, and refuses to let a set
// of numbers exist without it.
//
// This package is the price of one decision. The first draft put adapters and results in a
// separate repository so that "we grade a competitor with a runner we wrote" could be answered
// by location. That was overruled: everything lives in agent-guard, the
// repository of the tool being measured. Location no longer answers anything, so the answer had
// to move somewhere it cannot be forgotten — out of a README sentence and into fields that
// Validate requires and the scorecard prints every time.
//
// Hence Adapter and Placement are mandatory strings. A figure about somebody else's scanner
// rests on our adapter, our threshold choice and our decision about where each sample was
// staged; a reader who is not told that cannot weigh it, and we are not entitled to the
// conclusion. Provisional is mandatory-true for third-party tools for the same reason: the
// corpus is built so that a vendor can measure themselves, and until they do, our figure is the
// best available guess and must say so.
package run

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// Run is one measurement of one tool against one corpus checkout. It is written beside the
// verdicts as run.yaml and is the only thing that makes a verdicts file citable six months
// later.
type Run struct {
	// Tool is the registry id in baselines/tools.yaml.
	Tool string `yaml:"tool"`
	// ToolVersion is what was actually executed, read from the tool, never assumed.
	ToolVersion string `yaml:"tool_version"`
	// Threshold is the tier a verdict was folded at. The corpus's guide is explicit that
	// collapsing a scanner's several states into one word is a CHOICE, and that the choice
	// must be published with the number.
	Threshold string `yaml:"threshold"`
	// PolicyHash identifies the policy file when the tool's pass/fail is policy-driven
	// (skill-scanner's is), so "their default" cannot quietly become a different default later.
	PolicyHash string `yaml:"policy_hash,omitempty"`
	// ToolExtraArgs are flags the operator appended to the tool's invocation beyond what the
	// adapter passes (`--llm --config …` to measure the judge). Recorded because an
	// invocation nobody can see is a number nobody can rerun; when it carries --llm, Uploads
	// and UploadsBasis describe this run rather than the registry's default.
	ToolExtraArgs []string `yaml:"tool_extra_args,omitempty"`
	// Uploads records whether sample content left this machine. Not a licence question — every
	// sample in the corpus is permissively licensed and the corpus repository is public — but a
	// fact a reader is entitled to, and the trigger for the isolation rules.
	Uploads bool `yaml:"uploads_samples"`
	// UploadsBasis says HOW the line above was established, in words a reader can weigh.
	//
	// Required, because the bare boolean is the most quotable line in this file and the easiest
	// to state without evidence. An earlier run published `uploads_samples: false` for cc-audit on the
	// strength of a partial source read — the caveat existed, but it lived in tools.yaml, which
	// is not the file anybody opens when citing a figure. A claim about whether somebody else's
	// scanner phones home is exactly the kind that must carry its own provenance.
	UploadsBasis string `yaml:"uploads_samples_basis"`
	// CorpusCommit pins which corpus produced the work list. Without it the denominator is
	// unknown, and raw/ is not committed precisely because this makes a rerun possible instead.
	CorpusCommit string `yaml:"corpus_commit"`
	// StartedAt is when, because a number about a moving competitor decays.
	StartedAt time.Time `yaml:"started_at"`

	// Adapter says whose adapter produced this. See the package comment.
	Adapter string `yaml:"adapter"`
	// Placement says who decided where each sample was staged, and how.
	Placement string `yaml:"placement"`
	// Provisional says this figure stands only until the vendor measures themselves.
	Provisional bool `yaml:"provisional"`

	// Declarations is the (surface -> stated reason) map the tripwire consumes. Recorded here
	// so a declaration is part of the run's evidence rather than local knowledge.
	Declarations map[string]string `yaml:"surfaces_declared_uncovered,omitempty"`

	// Counts is the ledger partition, duplicated here so run.yaml alone answers "how many
	// points were there, and did they all land somewhere".
	Counts ledger.Counts `yaml:"counts"`
	// WorkListSize is what Counts.Total() must equal.
	WorkListSize int `yaml:"work_list_size"`
}

// ErrIncomplete is returned when a run is missing metadata that makes its numbers readable.
var ErrIncomplete = errors.New("run metadata is incomplete")

// Validate refuses a run whose numbers could not be honestly published. It is deliberately
// strict about the attribution fields: they are the whole defence left after the results moved
// into the measured tool's own repository, and a defence that is optional is not one.
func (r Run) Validate() error {
	var missing []string
	need := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	need("tool", r.Tool)
	need("tool_version", r.ToolVersion)
	need("threshold", r.Threshold)
	need("corpus_commit", r.CorpusCommit)
	need("adapter", r.Adapter)
	need("placement", r.Placement)
	need("uploads_samples_basis", r.UploadsBasis)

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s — these are what let a reader weigh the number instead of "+
			"taking it on trust", ErrIncomplete, strings.Join(missing, ", "))
	}
	if r.StartedAt.IsZero() {
		return fmt.Errorf("%w: started_at is unset, so nobody can tell how stale this is", ErrIncomplete)
	}
	if r.WorkListSize <= 0 {
		return fmt.Errorf("%w: work_list_size is %d, so the denominator is unknown",
			ErrIncomplete, r.WorkListSize)
	}
	if total := r.Counts.Total(); total != r.WorkListSize {
		return fmt.Errorf("%w: the ledger accounts for %d of %d test points — the partition must "+
			"cover the work list exactly, and a gap here is the silent hole the ledger exists to "+
			"replace", ErrIncomplete, total, r.WorkListSize)
	}
	return nil
}

// Signature is the line a scorecard must carry above any figure. It names what the number rests
// on that is OURS rather than the tool's.
func (r Run) Signature() string {
	s := fmt.Sprintf("measured by agent-guard's own adapter (%s), threshold %q chosen by us, "+
		"placement: %s", r.Adapter, r.Threshold, r.Placement)
	if len(r.ToolExtraArgs) > 0 {
		s += fmt.Sprintf(". EXTRA ARGS: %s — the verdicts below fold deterministic findings only "+
			"and are unaffected; anything these flags added lives in raw/ and is not committed",
			strings.Join(r.ToolExtraArgs, " "))
	}
	if r.Provisional {
		s += ". PROVISIONAL: agent-artifact-corpus is public and " + r.Tool +
			" can measure itself against it; until it does, this figure is our best available " +
			"guess and not the vendor's number"
	}
	return s
}
