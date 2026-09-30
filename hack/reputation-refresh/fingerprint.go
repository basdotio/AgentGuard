// SPDX-License-Identifier: MIT
package main

import (
	"sort"

	"github.com/basdotio/AgentGuard/internal/model"
)

// fingerprint reduces a scan to the set of findings a review can be said to cover: one
// "RULE-ID <file>" line per scoring finding, sorted, duplicates kept. Dimension-0 notes are
// the scan describing itself, not the artifact, and are left out.
//
// Line numbers are deliberately NOT part of the fingerprint: a version bump shifts every line
// in a file, and a fingerprint that changed on every edit would never match, which turns the
// refresh into a rubber stamp for "run it by hand and accept whatever". Rule + file is the
// granularity at which the human review was written ("the base64 in server.cjs is the
// WebSocket accept key"), so it is the granularity at which the review still applies.
func fingerprint(arts []model.ArtifactReport) []string {
	var fp []string
	for _, a := range arts {
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				continue
			}
			file := ""
			for _, ev := range f.Evidence {
				if file == "" || ev.File < file {
					file = ev.File
				}
			}
			fp = append(fp, f.RuleID+" "+file)
		}
	}
	sort.Strings(fp)
	return fp
}

// diff compares two fingerprints as multisets and returns what appeared and what went away.
// Both empty means the review still covers every finding present.
func diff(old, cur []string) (added, removed []string) {
	count := map[string]int{}
	for _, f := range old {
		count[f]--
	}
	for _, f := range cur {
		count[f]++
	}
	for f, n := range count {
		for ; n > 0; n-- {
			added = append(added, f)
		}
		for ; n < 0; n++ {
			removed = append(removed, f)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}
