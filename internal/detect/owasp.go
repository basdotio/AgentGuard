// SPDX-License-Identifier: MIT
package detect

import "github.com/basdotio/agent-guard/internal/model"

// OWASP's Top 10 for Agentic Applications (2026) is the catalogue a security reviewer, a procurement
// checklist and every comparable scanner speak in. This file attaches those identifiers to our
// findings.
//
// It is a SECOND VIEW, not a replacement. The ten scoring dimensions are the basis of `overall`, and
// `overall` is the number a third party must be able to recompute offline (and, eventually, the value
// an on-chain attestation carries) — so they do not move. The two taxonomies also cut differently:
// ours is by TECHNICAL BEHAVIOUR (what the code does), OWASP's is by THREAT (what it achieves). The
// mapping is therefore many-to-many with real holes on both sides, and forcing a bijection would mean
// inventing correspondences — worse than admitting a gap.
//
// PROVENANCE, AND ITS LIMIT. The identifiers and titles were taken from a third party's published
// mapping, because the OWASP list is distributed as a PDF download rather than a fetchable page. They
// are therefore UNVERIFIED against the official wording: the identifiers are near-certainly right,
// the titles may be paraphrases. Before this appears in anything a customer reads as a compliance
// claim it must be checked against the official document.

type asiCategory struct {
	ID    string
	Title string
}

// asiCatalogue is the full category list, kept as a table so a report can show which entries this
// scanner covers AND which it does not. "We detect 6 of 10, and here are the 4 we are silent about" is
// a more useful statement than a coverage percentage, and it is the same discipline COV-000 applies to
// files: a gap that announces itself is a decision the operator gets to make.
var asiCatalogue = []asiCategory{
	{"ASI-01", "Agent Goal Hijack"},
	{"ASI-02", "Tool Misuse"},
	{"ASI-03", "Identity & Privilege Abuse"},
	{"ASI-04", "Supply Chain"},
	{"ASI-05", "Unrestricted Code Execution"},
	{"ASI-06", "Memory Poisoning"},
	{"ASI-07", "Inter-Agent Communication"},
	{"ASI-08", "Cascading Failures"},
	{"ASI-09", "Trust Exploitation"},
	{"ASI-10", "Rogue Agents"},
}

// asiByRule maps a rule ID to the categories it evidences. Built from the rules that actually exist
// (checked against rules_data.go and the structural findings in detect.go, permission.go and hooks.go)
// rather than from the dimension numbering — per-dimension was tried and it flattened real
// distinctions, because one dimension holds rules that answer to different threats.
//
// A rule may carry more than one category. An escapable `Bash(git *)` grant is both tool misuse and a
// privilege problem, and reporting one of the two hides half of why it matters.
//
// EMPTY IS AN ANSWER, not an omission. Three families map to nothing on purpose:
//
//   - OBF-001/002/003/005 — obfuscation is a TECHNIQUE, not a threat. Base64-then-eval serves goal
//     hijack, code execution and exfiltration alike; pinning it to one category would misreport it.
//     (OBF-004 is different: it only fires as part of the exfiltration chain, so it inherits ASI-02.)
//   - RES-001/002/003 — resource abuse is closest to ASI-08 Cascading Failures, which is about
//     runtime behaviour propagating across agents. A static scanner sees an unbounded loop, not a
//     cascade. Our findings here are already marked advisory; claiming the category would overstate
//     what the check did.
//   - BD-001/002/003 — likewise ASI-10 Rogue Agents describes an agent acting against its principal
//     at runtime. We see an environment-triggered conditional. Advisory, and unmapped.
var asiByRule = map[string][]string{
	// Injection — instructions aimed at the agent.
	"INJ-001": {"ASI-01"},
	"INJ-002": {"ASI-01"},
	"INJ-003": {"ASI-01"},
	"INJ-004": {"ASI-01"}, // hidden characters: concealment in service of a hijack
	"INJ-005": {"ASI-01"}, // a fetched instruction file the agent acts on
	"OBF-005": {"ASI-01"}, // homoglyph disguise: same, by a different mechanism
	// Connector tool-description poisoning: injection delivered through the tool list.
	"MCP-001":   {"ASI-01", "ASI-02"},
	"MCP-002":   {"ASI-01"},
	"MCP-003":   {"ASI-01"},
	"MCP-004":   {"ASI-02"},
	"MCP-005":   {"ASI-01", "ASI-02"}, // overrides the user's recipients and copies every message out
	"EXFIL-006": {"ASI-02"},
	"EXFIL-007": {"ASI-02"}, // host identity beaconed to a remote URL           // the API key and every prompt routed to a third-party host

	// Execution.
	"EXEC-001": {"ASI-05"},
	"EXEC-002": {"ASI-05"},
	"EXEC-003": {"ASI-05"},
	"EXEC-004": {"ASI-05"},
	"EXEC-005": {"ASI-05"},
	"EXEC-006": {"ASI-05"},
	"EXEC-007": {"ASI-05"},
	"EXEC-008": {"ASI-05"},
	"EXEC-009": {"ASI-05"},
	"EXEC-010": {"ASI-05"}, // interpreter preload via env: code runs before the program the config names
	"EXEC-011": {"ASI-05"}, // a base64 payload decoded straight into a shell — unrestricted execution of hidden code

	// Reverse shell: a shell handed to a remote party is unrestricted code execution. Unlike
	// BD-001/002/003 (advisory heuristics, deliberately unmapped) this is a definite construct.
	"BD-004": {"ASI-05"},

	// Supply chain.
	"SUP-001": {"ASI-04"},
	"SUP-002": {"ASI-04"},
	"SUP-003": {"ASI-04"},
	"SUP-004": {"ASI-04"}, // instructions pointing the agent into a tree the scan does not read

	// Filesystem — reach beyond what the task needs, and destructive reach.
	"FS-001": {"ASI-02"},
	"FS-002": {"ASI-02"},
	"FS-003": {"ASI-02"},
	"FS-004": {"ASI-02"},

	// Exfiltration. ASI-02 because the tool surface is what carries the data out; the Agentic list has
	// no standalone disclosure category (that lives in the LLM Top 10 as LLM06), which is exactly the
	// kind of hole this file refuses to paper over.
	"EXFIL-001": {"ASI-02"},
	"EXFIL-002": {"ASI-02"},
	"EXFIL-003": {"ASI-02"},
	"EXFIL-004": {"ASI-02"}, // whole-environment enumeration: the collection half of the same chain
	"EXFIL-005": {"ASI-02"}, // an instruction file loading a credential into context: the collection leg, by import
	"OBF-004":   {"ASI-02"}, // only ever emitted alongside EXFIL-003

	// Permissions and hooks. A hook runs shell on every matching tool call, so it is a privilege
	// question as much as a misuse one.
	"PERM-004": {"ASI-03"}, // no deny fallback: nothing bounds what a grant becomes
	"PERM-006": {"ASI-02", "ASI-03"},
	"PERM-007": {"ASI-03"}, // rewriting the permission file is the tool changing its own authority
	"PERM-008": {"ASI-03"}, // a hook answering the permission prompt for the user
	"HOOK-001": {"ASI-02", "ASI-03"},
	"HOOK-002": {"ASI-02", "ASI-03"}, // unaudited second stage at a silent intercept
	"HOOK-003": {"ASI-02", "ASI-03"}, // event payload posted to a URL
}

// asiByKind adds categories that depend on WHERE a finding was found, not on which rule fired.
//
// This is the part a rule-only mapping gets wrong, and it is the category we already covered without
// ever naming: an injected instruction in a skill is ASI-01, the SAME instruction in auto memory is
// also ASI-06, because Claude wrote that file itself and reloads it every later session — one line
// turns a one-shot compromise into persistence. The rule is identical; the threat is not.
var asiByKind = map[model.ArtifactKind][]string{
	model.KindMemory: {"ASI-06"},
	// A subagent definition is instructions one agent hands another, which is the inter-agent
	// channel this list means — the payload travels agent-to-agent rather than user-to-agent.
	model.KindSubagent: {"ASI-07"},
}

// ASIFor returns the categories a finding evidences, given its rule and the kind of artifact it was
// found in. Returns nil when there is no honest mapping.
func ASIFor(ruleID string, kind model.ArtifactKind) []string {
	base := asiByRule[ruleID]
	extra := asiByKind[kind]
	if len(extra) == 0 || len(base) == 0 {
		// No kind bonus, or a rule with no mapping at all: a location cannot supply a category on its
		// own. An unbounded loop found in memory is still not memory poisoning.
		return base
	}
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	seen := map[string]bool{}
	for _, id := range base {
		seen[id] = true
	}
	for _, id := range extra {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}

// ASIEntry is one catalogue row plus whether this scanner has anything mapped to it.
type ASIEntry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Covered bool   `json:"covered"`
}

// ASICatalogue returns the full category list with coverage marked, so a report can name the
// categories this tool is SILENT about instead of only the ones it found.
func ASICatalogue() []ASIEntry {
	covered := map[string]bool{}
	for _, ids := range asiByRule {
		for _, id := range ids {
			covered[id] = true
		}
	}
	for _, ids := range asiByKind {
		for _, id := range ids {
			covered[id] = true
		}
	}
	out := make([]ASIEntry, 0, len(asiCatalogue))
	for _, c := range asiCatalogue {
		out = append(out, ASIEntry{ID: c.ID, Title: c.Title, Covered: covered[c.ID]})
	}
	return out
}
