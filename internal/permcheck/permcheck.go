// SPDX-License-Identifier: MIT
// Package permcheck audits the Claude Code permission allow/deny lists (spec §7). It
// flags over-broad grants (wildcard code execution), inline secrets, broad path reads,
// and a missing deny list. Findings attach to the KindPermission artifact so they score.
package permcheck

import (
	"encoding/json"
	"path/filepath"
	"regexp"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// bashAnyExec: a Bash grant whose command IS a wildcard — Bash(*) / Bash( * ) — allows
// ANY command. The single most dangerous grant; a permission auditor must not miss it.
var bashAnyExec = regexp.MustCompile(`(?i)Bash\(\s*\*\s*\)`)

// wildcardExec: a Bash grant that wildcards an interpreter/shell = unconfirmed arbitrary
// code execution (e.g. Bash(python3 -c '*), Bash(uv run *), Bash(bash -c *)). Allows an
// optional leading `env …` prefix before the interpreter.
var wildcardExec = regexp.MustCompile(`(?i)Bash\(\s*(env\s+\S+\s+)*(sh|bash|zsh|python3?|node|deno|bun|uv|ruby|perl|eval)\b[^)]*\*`)

// inlineSecret: a permission entry that embeds a credential value.
var inlineSecret = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|access[_-]?key)\s*[:=]\s*\S{12,}`)

// broadRead: a Read/Write grant with a broad glob (e.g. Read(//tmp/**), Read(/**)).
var broadRead = regexp.MustCompile(`(?i)(Read|Write|Edit)\(\S*\*\*`)

// Audit reads settings.json at path and returns permission findings + whether a deny
// list is present. Corrupt/missing file → no findings (collector already noted it).
func Audit(path string) []model.Finding {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return nil
	}
	var doc struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	// Scope labelling lives in collect (spec §4 B4) so the collector and this auditor can
	// never disagree about what "local" means.
	scope := collect.ScopeFor(path)
	file := filepath.Base(path)
	var out []model.Finding
	for _, entry := range doc.Permissions.Allow {
		esc := escapes(entry)
		switch {
		case inlineSecret.MatchString(entry):
			out = append(out, mk("PERM-001", 3, model.SevHigh, "Inline plaintext secret in a permission entry",
				"An allow entry embeds a credential value directly; remove it and use a secret manager.", entry, file, scope))
		case bashAnyExec.MatchString(entry):
			out = append(out, mk("PERM-005", 2, model.SevHigh, "Allows arbitrary commands via Bash(*)",
				"This allow lets any shell command run without confirmation — effectively disabling command-layer protection; replace with an exact command allowlist.", entry, file, scope))
		case wildcardExec.MatchString(entry):
			out = append(out, mk("PERM-002", 2, model.SevMedium, "Wildcard arbitrary-code-execution grant",
				"This allow wildcards an interpreter, i.e. arbitrary code execution without confirmation; narrow it to exact commands.", entry, file, scope))
		// Same capability as PERM-002 (arbitrary execution behind a wildcard), reached through
		// an everyday tool instead of an obvious interpreter — so the same severity.
		case esc != nil:
			// The positionFree clause is appended here rather than written into each Via so
			// there is one wording to keep true, and so the reason a pinned subcommand did
			// NOT save this grant reaches the person reading the finding. Without it the
			// advice ("narrow it") looks like something they already did.
			why := "This allow covers " + esc.Bin + ", which can execute an arbitrary command via " + esc.Via +
				"; the grant is therefore equivalent to unrestricted execution."
			if esc.positionFree {
				why += " " + esc.Bin + " accepts that option ANYWHERE in the command line, so pinning a " +
					"subcommand or target does not put it out of reach — Bash(" + esc.Bin + " <sub> *) is as open " +
					"as Bash(" + esc.Bin + " *)."
			}
			out = append(out, mk("PERM-006", 2, model.SevMedium, "Grant covers a command that can spawn a shell",
				why+" Narrow it to exact, argument-complete commands.",
				entry, file, scope))
		case broadRead.MatchString(entry):
			out = append(out, mk("PERM-003", 9, model.SevLow, "Overly broad file read/write grant",
				"Read/write grant with an over-broad glob; narrow to specific sub-paths.", entry, file, scope))
		}
	}
	if len(doc.Permissions.Allow) > 0 && len(doc.Permissions.Deny) == 0 {
		out = append(out, model.Finding{
			RuleID: "PERM-004", Dimension: 2, Severity: model.SevLow, Source: model.SrcPermission,
			Title:    "No deny fallback list",
			Why:      "allow without deny; add a deny fallback for sensitive paths (~/.ssh, ~/.aws, .env).",
			Evidence: []model.Evidence{{File: file, Line: 0, Snippet: "permissions.deny is empty", Scope: scope}},
		})
	}
	return out
}

func mk(id string, dim int, sev model.Severity, title, why, entry, file, scope string) model.Finding {
	return model.Finding{
		RuleID: id, Dimension: dim, Severity: sev, Source: model.SrcPermission,
		Title: title, Why: why,
		// Redact so an inline secret value never lands in a snippet (spec §16.3).
		Evidence: []model.Evidence{{File: file, Line: 0, Snippet: detect.Redact(entry), Scope: scope}},
	}
}
