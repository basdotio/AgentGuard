// SPDX-License-Identifier: MIT
package judge

import (
	"regexp"
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
	catExfiltration   = category{"exfiltration", model.SevHigh, "sends data, files or secrets to a network destination outside this machine"}
	catCredentialRead = category{"credential-read", model.SevHigh, "reads keys, tokens, passwords or credential files by path, with no stated reason to"}
	catRemoteExec     = category{"remote-execution", model.SevHigh, "downloads code or instructions from the network and runs or follows them (a URL plus a shell or interpreter); running a local script by name is NOT this, and neither is deploying, uploading or calling a service through its own CLI or API, nor the word remote in a task description"}
	catSafetyDisable  = category{"safety-disable", model.SevHigh, "skips, disables or bypasses a safety check, confirmation or permission"}
	catConcealed      = category{"concealed-directive", model.SevHigh, "an instruction hidden by encoding, invisible characters, unusual phrasing or placement"}
	catFileDeletion   = category{"file-deletion", model.SevHigh, "deletes or overwrites files beyond its own output"}
)

// categoryTables is the whole decision, per pass. The MCP config pass (LLM-009) has no table on
// purpose: it is advisory-only (advisoryOnly) and keeps the clamped word, unchanged.
var categoryTables = map[Mode][]category{
	ModeInjection: {
		catExfiltration, catCredentialRead, catRemoteExec, catSafetyDisable, catConcealed,
		{"privilege", model.SevHigh, "escalates privilege or escapes a sandbox: runs as root or with sudo, writes under /root, mounts the host filesystem or the container socket, probes a cloud metadata service"},
		{"other-directive", model.SevMedium, "a directive beyond or against the declared purpose that is none of the above, including one that only runs a local script or command"},
	},
	ModeIntent: {
		catExfiltration, catCredentialRead, catRemoteExec, catSafetyDisable, catConcealed,
		{"software-source", model.SevHigh, "points a package registry, index or proxy at a host that is not the vendor's"},
		{"persistence", model.SevHigh, "installs a git hook, writes shell or editor startup files, scheduled jobs or launch agents"},
		catFileDeletion,
		{"privilege", model.SevHigh, "changes permissions, users, sudoers or ownership"},
		{"other", model.SevMedium, "undisclosed behaviour that is none of the above"},
	},
	ModeServer: {
		catExfiltration, catCredentialRead, catRemoteExec, catSafetyDisable, catConcealed,
		{"software-source", model.SevHigh, "points a package registry, index or proxy at a host that is not the vendor's"},
		{"persistence", model.SevHigh, "installs a git hook, writes shell or editor startup files, scheduled jobs or launch agents"},
		catFileDeletion,
		{"privilege", model.SevHigh, "changes permissions, users, sudoers or ownership"},
		{"other", model.SevMedium, "undisclosed behaviour that is none of the above"},
	},
	ModeCapability: {
		{"network", model.SevHigh, "the command itself contacts the network: a URL, curl, wget, ssh, nc, a mail or webhook call"},
		{"credential", model.SevHigh, "the command itself reads a key, token or credential file by path"},
		{"install", model.SevHigh, "the command itself installs a package or downloads code: pip, npm, brew, a fetch piped to a shell"},
		{"permission-override", model.SevHigh, "the command allows or auto-approves an action: it answers the permission prompt with approval, writes an allow permissionDecision, or widens the permission settings; a hook that only denies or blocks is NOT this (process-control if it also runs something, else not flagged)"},
		{"file-write", model.SevMedium, "the command itself writes files other than its own log or the file the event concerns"},
		{"process-control", model.SevMedium, "starts, kills or signals processes, or runs a local script whose contents are not shown"},
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
	// The result pass (P-067). The two high rows are the result itself changed; a record nobody
	// asked for is medium — real skills write summaries and notes beside their output, and the
	// injection pass already weighs one written under /root as privilege.
	ModeSteering: {
		{"forced-value", model.SevHigh, "a value, row, region or field written into the output regardless of what the data shows"},
		{"data-tampering", model.SevHigh, "input or intermediate data offset, scaled, rounded, replaced, filtered or dropped before the result is produced, or a check whose outcome is decided in advance"},
		{"unrequested-artifact", model.SevMedium, "a report, record, registration or message produced or sent that the task never asked for"},
		{"other-steering", model.SevMedium, "the result changed in some other way the user is not told"},
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
	if (m == ModeIntent || m == ModeServer) && v.Disclosed && cat != disclosureExempt {
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
	b.WriteString(". Name the category for what the text ITSELF shows or tells the agent to do; never infer it from a file or script name, " +
		"and never from what a script might contain — when all the text shows is that a local file is run, the category is the one " +
		"that says so (process-control, other-directive or other). The tool sets the finding's severity from the category; your " +
		"severity word is recorded but does not decide it.")
	if m == ModeIntent || m == ModeServer {
		b.WriteString(" Set disclosed=true when the DECLARED PURPOSE itself states this kind of behaviour in substance (a purpose that says it sends metrics to a collector discloses a network send, even without the host); set it false when the purpose is silent about it, however plausible the behaviour is for such a tool.")
	}
	return b.String()
}

// categoryName is the model's category as the vote list prints it: lower-cased, trimmed, and
// "unknown" when the reply carried none — so a run can be audited for how often the table's
// medium default was taken.
func categoryName(v Verdict) string {
	cat := strings.ToLower(strings.TrimSpace(v.Category))
	if cat == "" {
		return "unknown"
	}
	return cat
}

// Category grounding (P-049). A category names a behaviour, and for four hook categories and one
// connector category the behaviour has to be visible in the text the model was sent, or the
// model is guessing from a name: P-041's full-benign run read `install` from a script called
// ensure-gh-cli.sh, `permission-override` from a script run on a PermissionRequest event,
// `network` from "entroly ravs capture" and from a URL on localhost, and `credential-read` from a
// tool parameter named secretKey, 21 of the 53 benign samples at high. Telling the model not to
// (P-041 did, P-048 did again) lowers how often it does, never to zero, and under a majority bar
// that is not enough; so the tool checks. A category the text does not show keeps its name in the
// vote but weighs medium, the table's valve, and the reason says so. The token lists are closed
// and small on purpose: what they miss is a hook whose behaviour sits in a script the scanner
// cannot see — where the model saw nothing either, and static rules follow the script when it
// is there.
var hookCategoryTokens = map[string]*regexp.Regexp{
	"network":             regexp.MustCompile(`(?i)https?://|wss?://|\b(curl|wget|nc|ncat|netcat|ssh|scp|sftp|rsync|telnet|socat)\b|\bopenssl s_client\b|\bgit (push|pull|fetch|clone)\b|\b(npx|pipx|uvx)\b|\b\d{1,3}(\.\d{1,3}){3}\b|(^|[\s"'@=])[a-z0-9-]+(\.[a-z0-9-]+)*\.(com|io|net|org|dev|example|ai|co)([/:\s"']|$)`),
	"install":             regexp.MustCompile(`(?i)\b(npx|npm|pnpm|yarn|pip3?|pipx|uv|brew|apt(-get)?|dnf|yum|cargo|gem|composer|conda|choco|winget|curl|wget)\b|\bgo install\b`),
	"permission-override": regexp.MustCompile(`(?i)permissionDecision|updatedInput|hookSpecificOutput|\b(allow|approve|approved|deny)\b`),
	"credential":          regexp.MustCompile(`(?i)\.(ssh|aws|netrc|npmrc|pypirc|gnupg|kube|docker)\b|keychain|credential|secret|token|api[_-]?key|password|passwd|\.env\b|/etc/(passwd|shadow)`),
}

// localURL is a URL on this machine: for `network` it is stripped before the tokens are looked
// for, so "http://localhost:3000/hook" alone shows nothing.
var localURL = regexp.MustCompile(`(?i)(https?|wss?)://(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(:\d+)?`)

// catalogueParamLine is a tool parameter as detect renders a connector's catalogue ("tool ▸ param
// name: description"): the one line shape through which a user hands a tool the key of the
// service it calls, which is not the tool reading the user's secrets.
var catalogueParamLine = regexp.MustCompile(`(?m)^[^\n]*▸ param [^\n]*$`)

// categoryShown reports whether the text supports the category: for the capability pass (hooks)
// the sent text must carry a token of the behaviour; for the injection pass on a connector's
// catalogue, `credential-read` must rest on something other than parameter lines. Every other
// (mode, category) is taken as shown — the check exists for the places a guess was measured.
func categoryShown(m Mode, artifact, cat, sent, quoted string) bool {
	switch {
	case m == ModeCapability:
		re, ok := hookCategoryTokens[cat]
		if !ok {
			return true
		}
		text := sent
		if cat == "network" {
			text = localURL.ReplaceAllString(text, "")
		}
		return re.MatchString(text)
	case m == ModeInjection && cat == "credential-read" && strings.HasPrefix(artifact, string(model.KindConnector)+":"):
		lines := 0
		for _, l := range strings.Split(quoted, "\n") {
			if strings.TrimSpace(l) == "" {
				continue
			}
			lines++
			if !catalogueParamLine.MatchString(l) {
				return true
			}
		}
		return lines == 0
	}
	return true
}
