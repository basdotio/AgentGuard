// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/basdotio/AgentGuard/internal/config"
)

// `--report`: the HTML report without having to invent a path.
//
// `--html <path>` is the CI shape — the caller owns the filename. A person, or the plugin skill
// acting for one, has no filename in mind; they want the report to exist and to be told where.
// `--report` writes it under the user-level config directory, next to the judge's config and
// away from the scan root (a report inside ~/.claude would be an unowned loose file the next
// scan discloses), timestamped so consecutive scans can be compared rather than overwritten.
// The reports hold redacted snippets and the user's own paths: local by design, never uploaded.

const reportsDirHint = "$XDG_CONFIG_HOME/aguard/reports or ~/.config/aguard/reports"

// defaultReportPath returns <config dir>/reports/scan-<timestamp>.html, creating the directory
// (0700, like the config dir: the report describes the user's environment in some detail).
func defaultReportPath(now time.Time) (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "scan-"+now.Format("20060102-150405")+".html"), nil
}
