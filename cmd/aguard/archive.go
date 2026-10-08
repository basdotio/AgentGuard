// SPDX-License-Identifier: MIT
package main

import (
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// checkExtracted checks an archive that inbox.ExtractZip unpacked into dir, and reports it as the
// archive. Both callers — `check x.zip` and the Downloads scan — go through here, so the two cannot
// disagree about what a zip's report names.
func checkExtracted(dir, archive string, o scanOpts) (model.ScanResult, error) {
	out, err := checkTarget(dir, o)
	if err != nil {
		return out, err
	}
	return archiveView(out, dir, archive), nil
}

// archiveView rewrites every absolute path that points into the extraction directory so it names
// the archive instead. That directory is temporary — gone by the time anyone reads the report — and
// its parent has a random name, so a path into it is both useless and different on every run.
//
//   - Root and artifact paths become the archive's path: it is where the artifact can be found
//     again, and what `aguard check` / `aguard approve` accept. A made-up "x.zip/skills/foo" would
//     look like a path and fail as one; where inside the archive is said by name and evidence.
//   - Evidence is already relative to the archive's root; the rare absolute one is made so too.
//   - Scan locations: the root becomes the archive. The rest are derived from the directory above
//     the root, which for an archive is the private temporary directory — empty by construction,
//     deleted on return. Naming them would list places that never existed outside this run.
//
// Paths outside the temporary directory are left alone. Nothing here touches a finding's rule,
// severity, snippet or hash: this is a change of name, not of verdict.
func archiveView(res model.ScanResult, dir, archive string) model.ScanResult {
	tmp := filepath.Dir(dir)
	out := res
	out.Root = archive
	if res.Artifacts != nil {
		out.Artifacts = make([]model.ArtifactReport, len(res.Artifacts))
		for i, a := range res.Artifacts {
			if within(tmp, a.Path) {
				a.Path = archive
			}
			a.Findings = archiveEvidence(a.Findings, dir, archive)
			out.Artifacts[i] = a
		}
	}
	out.Notes = archiveEvidence(res.Notes, dir, archive)
	out.Locations = nil
	for _, l := range res.Locations {
		switch {
		case l.Path == dir:
			l.Path = archive
		case within(tmp, l.Path):
			continue
		}
		out.Locations = append(out.Locations, l)
	}
	return out
}

// archiveEvidence returns findings whose absolute evidence paths inside dir are made relative to
// it — the archive's root — as every other evidence path already is.
func archiveEvidence(fs []model.Finding, dir, archive string) []model.Finding {
	if fs == nil {
		return nil
	}
	out := make([]model.Finding, len(fs))
	for i, f := range fs {
		if f.Evidence != nil {
			ev := make([]model.Evidence, len(f.Evidence))
			for j, e := range f.Evidence {
				e.File = archiveMember(e.File, dir, archive)
				ev[j] = e
			}
			f.Evidence = ev
		}
		out[i] = f
	}
	return out
}

// archiveMember names p as a path inside the archive when it points into dir — the archive's own
// file name for dir itself — and leaves anything else (relative paths, paths elsewhere on disk) as
// it is.
func archiveMember(p, dir, archive string) string {
	if p == dir {
		return filepath.Base(archive)
	}
	if within(dir, p) {
		if rel, err := filepath.Rel(dir, p); err == nil {
			return rel
		}
	}
	return p
}

// within reports whether p is base or lies under it. Both are absolute, cleaned paths built by this
// process; this is a name comparison, not a boundary check on attacker-controlled input.
func within(base, p string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}
