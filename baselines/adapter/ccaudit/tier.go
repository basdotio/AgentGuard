// SPDX-License-Identifier: MIT

package ccaudit

import (
	"strings"

	"github.com/basdotio/agent-guard/baselines/adapter/sarif"
)

// Tier is cc-audit's gate, and it is NOT a severity name.
//
// The tool has two tiers, not four: the default, and `--strict`. Its docs describe --strict as
// "show medium/low severity findings and treat warnings as errors", so the tier decides both
// what is reported and what counts as a failure. There are finer knobs (--min-severity,
// --min-rule-severity, --min-confidence), and this adapter deliberately touches none of them: the
// measured behaviour has to be the shipped behaviour, the same reason the aguard adapter runs at
// the gate's own default of `high`.
//
// Accepting a severity word like "high" here would silently measure a tier nobody chose, which
// is why ParseTier refuses it rather than guessing which tier was meant.
type Tier string

const (
	// TierDefault reports errors only. cc-audit's SARIF reporter maps Critical and High to
	// `error`, Medium to `warning` and Low to `note` (src/reporter/sarif.rs severity_to_level,
	// read at v3.23.9), so folding at `error` reproduces the tool's own default gate.
	TierDefault Tier = "default"
	// TierStrict is --strict: medium and low are reported and warnings count as errors, so
	// everything above `none` flags.
	TierStrict Tier = "strict"
)

// KnownTiers is what the -threshold flag accepts for this tool, strictest last.
func KnownTiers() []Tier { return []Tier{TierDefault, TierStrict} }

// ParseTier returns the tier, or "" for anything that is not one of the two.
func ParseTier(s string) Tier {
	for _, t := range KnownTiers() {
		if strings.EqualFold(strings.TrimSpace(s), string(t)) {
			return t
		}
	}
	return ""
}

// Threshold is the SARIF level at or above which a finding makes the verdict malicious.
//
// This is a projection of cc-audit's ladder onto SARIF's, and it is lossy in one direction that
// matters: Critical and High BOTH become `error`, so the SARIF pass cannot tell them apart. That
// is why severity is taken from the JSON pass instead — see topSeverity. Recording SARIF's
// `error` as the severity would quietly report every critical finding as a high one.
func (t Tier) Threshold() sarif.Level {
	if t == TierStrict {
		// Above `none`, which never flags whatever the threshold — see sarif.Fold.
		return sarif.LevelNote
	}
	return sarif.LevelError
}

// Args is what the tier adds to the scan argv. The default tier adds nothing on purpose: a run
// that passes no flags is the one whose numbers describe what a user gets out of the box.
func (t Tier) Args() []string {
	if t == TierStrict {
		return []string{"--strict"}
	}
	return nil
}
