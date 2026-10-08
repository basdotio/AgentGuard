// SPDX-License-Identifier: MIT
package model

// judgeRuleText is the tool's own one-sentence definition of every rule the LLM judge reports
// under (Source=llm). It lives here, not in internal/judge, because two stages read it and must
// read the SAME text: the judge falls back to it when a verdict's summary is blank, and the SARIF
// writer describes the rule with it. A finding's Why is the model's sentence about ONE artifact
// (or, for the notes, a count or an endpoint error) — never a description of the rule — and the
// report package cannot import the judge (the judge's tests import the report), so a second copy
// kept in step by a comment is the only alternative, and that is the drift this repository keeps
// refusing.
var judgeRuleText = map[string]string{
	"LLM-000": "The LLM judge was requested but some of its checks did not run (endpoint error, call budget, deadline); the gap is announced so an absent opinion is never read as a clean one.",
	"LLM-001": "The artifact's behavior does something its description does not disclose.",
	"LLM-002": "The judge endpoint is not on this machine, so best-effort-redacted excerpts are sent off it.",
	"LLM-003": "The instruction text contains a directive aimed at the agent that isn't disclosed as its purpose.",
	"LLM-004": "An obfuscated (base64/hex) payload decodes to content that performs sensitive actions.",
	"LLM-005": "Flagged verdicts whose quoted evidence could not be located in the text that was sent were discarded, and counted.",
	"LLM-006": "Capabilities in different files of this artifact combine into a credential-to-network chain.",
	"LLM-007": "While being examined, this content addressed the analysis model directly — telling it what to " +
		"conclude, or to disregard its instructions. Legitimate content has no reason to talk to a scanner.",
	"LLM-008": "The hook does more than intercepting this event plausibly requires.",
	"LLM-009": "The server's configuration (source, pinning, transport, credentials) carries supply-chain risk.",
}

// JudgeRuleText returns the tool's definition of an LLM judge rule, or "" for an id that is not
// one. Callers must treat "" as "no definition", never fall back to a finding's Why.
func JudgeRuleText(ruleID string) string { return judgeRuleText[ruleID] }
