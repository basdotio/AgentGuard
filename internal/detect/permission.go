// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"

	"github.com/basdotio/AgentGuard/internal/model"
)

// A permission grant naming a local script is the same shape of problem as a hook naming
// one: `Bash(./scripts/deploy.sh *)` reads like a narrow, well-behaved allowance, and what
// it actually hands over is whatever that script does — with open-ended arguments, without
// confirmation. permcheck answers "is this grant shaped dangerously" from the entry text
// alone; it never opens the file, so until now the script itself went unread.
//
// This is deliberately NOT an LLM pass, although the plan originally filed it as one. The
// question "is this grant dangerous" reduces to "what does deploy.sh do", and that is a file
// to read and run rules over — structural, not semantic. Static answers it deterministically,
// for free, and the result scores and gates immediately; a judge verdict would cost money,
// vary between runs, and still have to clear grounding and consensus before it counted.
//
// The machinery is shared with hookUnits on purpose (scriptRefs / resolveHookScript /
// inBoundary / readCapped): one definition of "follow a script named in config", so the
// boundary rule (§16.2) cannot be enforced in one place and forgotten in the other.

// permissionUnits returns the scannable text for one permission artifact: the local scripts
// its ALLOW entries name. Deny entries are skipped — a deny hands over nothing.
//
// Every reference that cannot be followed produces a note rather than silence (§12): "we saw
// a grant pointing at a script and did not read it" must never look like "we read it and it
// was clean".
func permissionUnits(root string, a model.ArtifactReport) ([]unit, []model.Finding) {
	entries := configStrings(a.Path, "permissions", "allow")
	if len(entries) == 0 {
		return nil, nil
	}
	// Same anchor collect and hooks use — the scan's own home, never the ambient environment,
	// so the scan stays hermetic and honours --root.
	home := filepath.Dir(root)

	var units []unit
	var notes []model.Finding
	seen := map[string]bool{}
	for _, entry := range entries {
		// scriptRefs tokenises on parens and whitespace, so the grant wrapper falls away and
		// `Bash(./scripts/deploy.sh *)` yields the path directly — no grant-shape parser here,
		// and therefore nothing to drift out of step with permcheck's.
		for _, ref := range scriptRefs(entry) {
			if seen[ref] {
				continue
			}
			seen[ref] = true

			path, why := resolveHookScript(root, home, ref)
			if why != "" {
				notes = append(notes, permRefNote(a.Name, ref, why))
				continue
			}
			if !inBoundary(home, path) {
				notes = append(notes, permRefNote(a.Name, ref, "it resolves outside HOME and was not read (§16.2)"))
				continue
			}
			b, cov := readCapped(path, home)
			if cov != nil {
				notes = append(notes, *cov)
				continue
			}
			if b == nil {
				notes = append(notes, permRefNote(a.Name, ref, "it could not be read"))
				continue
			}
			units = append(units, unit{file: path, text: string(b), role: roleForPath(path)})
		}
	}
	return units, notes
}

// permRefPrefix is shared with the coalescer for the same reason hookRefPrefix is: it marks
// where the reason begins, so recovering it is not a prose match.
const permRefPrefix = "A permission allow entry grants open-ended arguments to a local script that was NOT scanned because "

// permRefNoteTitle is shared with the coalescer for the same reason the prefix is.
const permRefNoteTitle = "Granted script not followed (incomplete coverage)"

// permRefNote reports a granted script the scan could not read. Dimension 0: a coverage gap,
// not a risk of its own — permcheck has already judged the grant's shape.
func permRefNote(artifact, ref, why string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title:    permRefNoteTitle,
		Why:      permRefPrefix + why + ".",
		Evidence: []model.Evidence{{File: redactClip(artifact), Line: 0, Snippet: redactClip(ref)}},
	}
}
