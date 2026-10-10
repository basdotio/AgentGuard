// SPDX-License-Identifier: MIT
package judge

import (
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// A judge finding's severity is the TOOL'S, read off the category the model names, never off
// the severity word it writes (P-041). The word was a one-word self-rating on a scale no prompt
// defined: measured on the vote-level run of 2026-09-29, three of the five static-missed
// malicious samples the judge reached a majority on were "hidden directive found, medium",
// which the gate and the fold read as nothing, while on benign hooks "medium" was the single
// largest source of noise. LLM-007 already fixed its severity for the same reason a hijacked
// model would rate its own capture low; this extends that to every pass that has a category to
// name. The model still answers "what kind of thing is this" — a closed list — and the table
// says what that kind weighs.
//
// Two rules keep the table from becoming a lever for the artifact under test: a category not in
// the table is medium, never high (a model talked into inventing one cannot raise its weight),
// and no row is low (the one-step disclosure rule on the intent pass is the only way down).

// category is one row of a pass's table: the name the model must write, what it weighs, and the
// one line the prompt offers for it.
type category struct {
	name string
	sev  model.Severity
	what string
}

// Rows shared by the passes that look for the same behaviours; one spelling, one weight.
var (
	catExfiltration   = category{"exfiltration", model.SevHigh, "sends data, files or secrets anywhere outside this machine"}
	catCredentialRead = category{"credential-read", model.SevHigh, "reads keys, tokens, passwords or credential files it has no stated reason to read"}
	catRemoteExec     = category{"remote-execution", model.SevHigh, "fetches and runs code, or runs commands the purpose does not call for"}
	catSafetyDisable  = category{"safety-disable", model.SevHigh, "skips, disables or bypasses a safety check, confirmation or permission"}
	catConcealed      = category{"concealed-directive", model.SevHigh, "an instruction hidden by encoding, invisible characters, unusual phrasing or placement"}
	catFileDeletion   = category{"file-deletion", model.SevHigh, "deletes or overwrites files beyond its own output"}
)

// categoryTables is the whole decision, per pass. The MCP config pass (LLM-009) has no table on
// purpose: it is advisory-only (advisoryOnly) and keeps the clamped word, unchanged.
var categoryTables = map[Mode][]category{
	ModeInjection: {
		catExfiltration, catCredentialRead, catRemoteExec, catSafetyDisable, catConcealed,
		{"other-directive", model.SevMedium, "a directive beyond or against the declared purpose that is none of the above"},
	},
	ModeIntent: {
		catExfiltration, catCredentialRead, catRemoteExec, catSafetyDisable, catConcealed,
		{"software-source", model.SevHigh, "changes where this machine gets software or what runs on it: a registry, index or proxy, git hooks, shell or editor startup files, scheduled jobs or launch agents"},
		catFileDeletion,
		{"privilege", model.SevHigh, "changes permissions, users, sudoers or ownership"},
		{"other", model.SevMedium, "undisclosed behaviour that is none of the above"},
	},
	ModeCapability: {
		{"network", model.SevHigh, "sends anything to the network"},
		{"credential", model.SevHigh, "reads keys, tokens or credential files"},
		{"install", model.SevHigh, "installs packages or downloads code"},
		{"permission-override", model.SevHigh, "answers or alters the agent's permission decision, or approves tool calls by itself"},
		{"file-write", model.SevMedium, "writes files other than its own log or the file the event concerns"},
		{"process-control", model.SevMedium, "starts, kills or signals other processes"},
		{"other", model.SevMedium, "more than the event requires, and none of the above"},
	},
	ModeExplain: {
		{"network", model.SevHigh, "makes a network call"},
		catRemoteExec, catCredentialRead, catFileDeletion,
		{"other", model.SevMedium, "sensitive in some other way"},
	},
	ModeCollusion: {
		{"credential-to-network", model.SevHigh, "one file collects credentials or data and another sends it out"},
		{"other", model.SevMedium, "a chain of some other kind"},
	},
}

// disclosureExempt names the intent categories the prompt flags whether or not the declared
// purpose states them: no honest skill needs to change where a machine gets its software.
const disclosureExempt = "software-source"

// severityFor is the finding's severity for a verdict in a mode. A mode without a table keeps the
// model's word, clamped (LLM-009, advisory-only). A category not in the table is medium. On the
// intent pass a disclosed behaviour is one step lower, except disclosureExempt.
func severityFor(m Mode, v Verdict) model.Severity {
	rows, ok := categoryTables[m]
	if !ok {
		return clampSeverity(v.Severity)
	}
	cat := strings.ToLower(strings.TrimSpace(v.Category))
	sev := model.SevMedium
	for _, r := range rows {
		if r.name == cat {
			sev = r.sev
			break
		}
	}
	if m == ModeIntent && v.Disclosed && cat != disclosureExempt {
		sev = oneStepLower(sev)
	}
	return sev
}

// oneStepLower: high → medium → low; low stays low.
func oneStepLower(s model.Severity) model.Severity {
	switch s {
	case model.SevHigh:
		return model.SevMedium
	default:
		return model.SevLow
	}
}

// categoryPrompt is the sentence a pass appends to its task: the closed list the model may pick
// from, one line per name, and the fact that the tool, not the word, sets the severity. Empty for
// a pass without a table.
func categoryPrompt(m Mode) string {
	rows, ok := categoryTables[m]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("When flagged=true, set category to EXACTLY ONE of these names, the one that best describes what you found: ")
	for i, r := range rows {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(r.name + " = " + r.what)
	}
	b.WriteString(". The tool sets the finding's severity from the category; your severity word is recorded but does not decide it.")
	if m == ModeIntent {
		b.WriteString(" Set disclosed=true only when the DECLARED PURPOSE itself states this behaviour; otherwise false.")
	}
	return b.String()
}
