// SPDX-License-Identifier: MIT

package cisco

import (
	"strings"

	"github.com/basdotio/agent-guard/baselines/adapter/sarif"
)

// Severity is skill-scanner's own ladder, as --fail-on-severity takes it: cli.py:435
// `_SEVERITY_ORDER = ["critical", "high", "medium", "low", "info"]` (read at v2.1.0). SAFE exists
// in the model but is not a rung the gate accepts.
//
// It is NOT SARIF's ladder and not cc-audit's tiers. taxonomy/tools.yaml requires a tool to be read
// on its own ladder, and accepting "error" here would gate at a rung nobody chose — the tool
// would refuse it at run time, after the binary was installed.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// KnownSeverities is the ladder most severe first.
func KnownSeverities() []Severity {
	return []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo}
}

// ParseSeverity returns the rung or "" for anything not on the ladder.
func ParseSeverity(s string) Severity {
	for _, k := range KnownSeverities() {
		if strings.EqualFold(strings.TrimSpace(s), string(k)) {
			return k
		}
	}
	return ""
}

// Level is where this rung lands in the SARIF the tool emits, so the fold gates where
// --fail-on-severity gates. sarif_reporter.py:40-45 (v2.1.0): CRITICAL and HIGH both become
// `error`, MEDIUM `warning`, LOW and INFO `note`, SAFE `none`. The same lossy collapse cc-audit
// has: critical and high are indistinguishable in SARIF, which is why severity on the row comes
// from the JSON pass rather than from here.
func (s Severity) Level() sarif.Level {
	switch s {
	case SeverityCritical, SeverityHigh:
		return sarif.LevelError
	case SeverityMedium:
		return sarif.LevelWarning
	case SeverityLow, SeverityInfo:
		return sarif.LevelNote
	}
	return sarif.LevelError
}
