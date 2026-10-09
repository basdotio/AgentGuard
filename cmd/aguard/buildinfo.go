// SPDX-License-Identifier: MIT
package main

import "runtime/debug"

// applyBuildInfo fills the -ldflags stamps from the toolchain's own build info when the Makefile
// did not set them. `go install github.com/basdotio/AgentGuard/cmd/aguard@latest` — the line
// hack/github-action.yml runs — stamps nothing, so that binary reported
// "aguard dev (commit none, built unknown)", wrote "dev" as the ToolVersion into every approval
// and report, and could not compare itself with the installed plugin. The module version and the
// vcs.* settings are recorded by the go tool from the module proxy or the checkout, so they are as
// trustworthy as what -ldflags would have said; a stamp that IS set always wins. "(devel)" is what
// a plain `go build` in a checkout reports and carries no information, so it is left as "dev".
func applyBuildInfo(bi *debug.BuildInfo) {
	if bi == nil {
		return
	}
	if version == "dev" {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			version = v
		}
	}
	var rev, when, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if commit == "none" && rev != "" {
		if len(rev) > 7 {
			rev = rev[:7]
		}
		if modified == "true" {
			rev += "-dirty"
		}
		commit = rev
	}
	if date == "unknown" && when != "" {
		date = when
	}
}

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		applyBuildInfo(bi)
	}
}
