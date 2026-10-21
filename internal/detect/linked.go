// SPDX-License-Identifier: MIT
package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/basdotio/bas-guard/internal/model"
)

// refusedLinksFinding scores the files of a tree that resolve outside its boundary (collect.FileReach,
// P-056). They are never opened (spec §16.2), but an unread file that the artifact runs or loads is a
// risk, not only a coverage gap — HOOK-002's reasoning for a hook's second stage — so the refusal is a
// scored finding on the artifact, not a note. Before P-056 these entries were skipped with no trace:
// a skill whose SKILL.md is a symlink into another directory scored 100 with nothing said, while
// Claude Code loaded that file as the skill. One finding per artifact; each refused entry is one
// evidence line, its link text redacted (the author wrote it).
func refusedLinksFinding(dir string, paths []string) model.Finding {
	sort.Strings(paths)
	const maxList = 10
	ev := make([]model.Evidence, 0, maxList)
	for i, p := range paths {
		if i == maxList {
			break
		}
		t, _ := os.Readlink(p)
		ev = append(ev, model.Evidence{File: p, Line: 0, Snippet: "→ " + redactClip(t) + " (not read)"})
	}
	more := ""
	if len(paths) > maxList {
		more = fmt.Sprintf(" (%d more not listed)", len(paths)-maxList)
	}
	return model.Finding{
		RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
		Title: "Symlink escapes its boundary, not read",
		Why: fmt.Sprintf("%d entry/entries of %s resolve outside the boundary they may be read in — the artifact's own "+
			"directory, or for a skill's SKILL.md the skills directory that holds it%s — and were NOT read (invariant #2). "+
			"Claude Code follows such a link when it loads the skill or the agent runs the file, so what it reads was not "+
			"audited; scored because an unread payload is a risk, not only a gap.", len(paths), filepath.Base(dir), more),
		Evidence: ev,
	}
}

// skillCredentialFinding: a skill's SKILL.md that resolves to a credential path. Claude Code would load
// the secret as the skill's instructions — and derive the skill's listed description from its first
// lines, which enter every session. Scored like an @import of a credential (EXFIL-005); never opened.
func skillCredentialFinding(p string, sev model.Severity) model.Finding {
	t, _ := os.Readlink(p)
	return model.Finding{
		RuleID: "EXFIL-005", Dimension: 3, Severity: sev, Source: model.SrcStatic,
		Title: "Skill's SKILL.md links to a credential",
		Why: "This skill's SKILL.md is a symlink to a credential — a file inside .ssh/.aws/.gnupg/…, or a file named " +
			"like one (.env, id_rsa, credentials, *.key, …). Claude Code loads SKILL.md into context when the skill is " +
			"used, so the secret would travel with the session. The file was NOT read by this scan.",
		Evidence: []model.Evidence{{File: p, Line: 0, Snippet: "→ " + redactClip(t) + " is a credential path"}},
	}
}
