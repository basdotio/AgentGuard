// SPDX-License-Identifier: MIT
package main

import (
	"runtime/debug"
	"testing"
)

// The go-install path: no -ldflags, but the module version and VCS stamps are in the build info.
// A stamp the Makefile DID set must never be overwritten, and "(devel)" must not masquerade as a
// version.
func TestApplyBuildInfo(t *testing.T) {
	save := func() func() {
		v, c, d := version, commit, date
		return func() { version, commit, date = v, c, d }
	}
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "c93a9cde1570b5a9c64e0ed5773019ec8cba37da"},
		{Key: "vcs.time", Value: "2026-10-01T11:06:58Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	cases := []struct {
		name                  string
		version, commit, date string
		bi                    *debug.BuildInfo
		wantV, wantC, wantD   string
	}{
		{"go install fills everything", "dev", "none", "unknown",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.17.0"}, Settings: settings},
			"v0.17.0", "c93a9cd", "2026-10-01T11:06:58Z"},
		{"ldflags win", "v0.18.0", "abc1234", "2026-11-01T00:00:00Z",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.17.0"}, Settings: settings},
			"v0.18.0", "abc1234", "2026-11-01T00:00:00Z"},
		{"(devel) stays dev, dirty tree is named", "dev", "none", "unknown",
			&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: append(settings[:2:2], debug.BuildSetting{Key: "vcs.modified", Value: "true"})},
			"dev", "c93a9cd-dirty", "2026-10-01T11:06:58Z"},
		{"nil build info is a no-op", "dev", "none", "unknown", nil, "dev", "none", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer save()()
			version, commit, date = tc.version, tc.commit, tc.date
			applyBuildInfo(tc.bi)
			if version != tc.wantV || commit != tc.wantC || date != tc.wantD {
				t.Errorf("got %s/%s/%s, want %s/%s/%s", version, commit, date, tc.wantV, tc.wantC, tc.wantD)
			}
		})
	}
}
