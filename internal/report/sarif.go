// SPDX-License-Identifier: MIT
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// SARIF is the format that puts a finding where the person who can fix it will see it: annotated on
// the line, in the pull request, in GitHub's Code Scanning tab. This tool was already a gate
// (`--fail-on` plus an exit code), but a gate that fails with a wall of terminal text makes the
// reader go hunting. The data needed for the richer form was already in every finding — file, line,
// redacted snippet — it just had nowhere to go.
//
// Three properties of this output are load-bearing:
//
//   - THE JUDGE IS NOT THE SCORE, and the SARIF must not blur that. An LLM finding is emitted at
//     `note` with an `advisory` tag no matter how severe the judge called it, because it does not move
//     `overall` and does not fail `--fail-on`. Rendering it as an `error` beside a deterministic one
//     would import the judge's uncertainty into a place that looks authoritative.
//   - SNIPPETS ARE SAFE TO EMIT because redaction happens before a finding is built, not before it is
//     printed (spec §16.3). SARIF ends up in a CI artifact and often in a third party's UI, so this is
//     the one output where getting that ordering wrong would be unrecoverable.
//   - FINGERPRINTS ARE STABLE ACROSS EDITS. Without them a shifted line number reopens an alert the
//     reviewer already dismissed, and a tool that keeps re-raising settled findings gets muted. The
//     fingerprint deliberately excludes the line number for that reason.
//
// Written against SARIF 2.1.0 (OASIS). Only the subset GitHub consumes is emitted; a partial
// document that validates beats a complete one that does not.

const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool          `json:"tool"`
	Results     []sarifResult      `json:"results"`
	Invocations []sarifInvocation  `json:"invocations,omitempty"`
	Properties  map[string]any     `json:"properties,omitempty"`
	Artifacts   []sarifArtifactRef `json:"artifacts,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name,omitempty"`
	ShortDescription     sarifText         `json:"shortDescription"`
	FullDescription      *sarifText        `json:"fullDescription,omitempty"`
	Help                 *sarifText        `json:"help,omitempty"`
	DefaultConfiguration *sarifRuleConfig  `json:"defaultConfiguration,omitempty"`
	Properties           map[string]any    `json:"properties,omitempty"`
	Relationships        []json.RawMessage `json:"relationships,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactRef `json:"artifactLocation"`
	Region           *sarifRegion     `json:"region,omitempty"`
}

type sarifArtifactRef struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int        `json:"startLine"`
	Snippet   *sarifText `json:"snippet,omitempty"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool `json:"executionSuccessful"`
}

// sarifLevel maps our severity onto SARIF's three-value scale, with one override that matters more
// than the mapping itself.
//
// GitHub renders `error`, `warning` and `note`. An ADVISORY finding — dimension 7/8, where static
// detection can hint but not confirm — is forced to `note` however severe the pattern looked. So is
// anything from the judge. The point of the split in the terminal report ("these set your score,
// these are leads") is lost if both arrive as red annotations on a diff.
func sarifLevel(f model.Finding) string {
	if f.Source == model.SrcLLM || f.Advisory {
		return "note"
	}
	switch f.Severity {
	case model.SevCritical, model.SevHigh:
		return "error"
	case model.SevMedium:
		return "warning"
	default:
		return "note"
	}
}

// sarifURI normalises a path for SARIF. Paths are already root-relative when a finding is built; an
// absolute one (a hook script followed outside the tree, a managed policy file) is kept as-is rather
// than mangled into a fake relative path, because a wrong URI silently attaches an annotation to the
// wrong file.
func sarifURI(p string) string {
	if p == "" {
		return "."
	}
	u := filepath.ToSlash(p)
	return strings.TrimPrefix(u, "./")
}

// fingerprint identifies a finding across runs. Deliberately (rule, file, snippet) with NO LINE
// NUMBER: inserting a line above a finding must not reopen an alert somebody already triaged.
func fingerprint(ruleID, file, snippet string) string {
	h := sha256.Sum256([]byte(ruleID + "\x1f" + sarifURI(file) + "\x1f" + strings.TrimSpace(snippet)))
	return hex.EncodeToString(h[:])[:16]
}

// SARIF writes the scan as a SARIF 2.1.0 log. version/commit identify the analyzer, which is part of
// what makes a result reproducible by someone else.
func SARIF(w io.Writer, res model.ScanResult, version, infoURI string) error {
	rules := map[string]sarifRule{}
	var results []sarifResult

	add := func(f model.Finding, artifact string, kind model.ArtifactKind) {
		lvl := sarifLevel(f)
		if _, seen := rules[f.RuleID]; !seen {
			// RULE-level metadata carries the rule's OWN categories, with NO artifact-kind refinement.
			// Kind-dependent categories belong on the RESULT: a rule's entry in tool.driver.rules is
			// created once, on first sight, so folding the kind in here made the classification depend
			// on which artifact happened to be walked first — INJ-001 came out as goal-hijack plus
			// inter-agent because a subagent preceded the memory file, and the memory-poisoning
			// category silently vanished from a report that had detected it.
			props := map[string]any{
				"tags":              sarifTags(f, ""),
				"security-severity": securitySeverity(f),
				"dimension":         f.Dimension,
				"aguard-source":     string(f.Source),
			}
			if cats := detect.ASIFor(f.RuleID, ""); len(cats) > 0 {
				props["owasp-agentic"] = cats
			}
			r := sarifRule{
				ID:                   f.RuleID,
				Name:                 ruleName(f.RuleID, f.Title),
				ShortDescription:     sarifText{Text: f.Title},
				DefaultConfiguration: &sarifRuleConfig{Level: lvl},
				Properties:           props,
			}
			if f.Why != "" {
				r.FullDescription = &sarifText{Text: f.Why}
				r.Help = &sarifText{Text: f.Why}
			}
			rules[f.RuleID] = r
		}

		// One result per evidence line, not per finding: SARIF's unit is a location, and folding
		// several locations into one result would annotate only the first.
		ev := f.Evidence
		if len(ev) == 0 {
			ev = []model.Evidence{{File: artifact}}
		}
		for _, e := range ev {
			loc := sarifLocation{PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactRef{URI: sarifURI(e.File)},
			}}
			// Line 0 means "the whole artifact" (a JSON value, a config-level fact). SARIF requires
			// startLine >= 1, so the region is omitted rather than faked to line 1 — pointing at a
			// line the finding is not about is worse than pointing at the file.
			if e.Line > 0 {
				loc.PhysicalLocation.Region = &sarifRegion{StartLine: e.Line}
				if e.Snippet != "" {
					loc.PhysicalLocation.Region.Snippet = &sarifText{Text: e.Snippet}
				}
			}
			// A coverage note's value is in Why — that is where the NAMES live ("not read: history.jsonl,
			// tools"). Its snippet is a bare label like "not read", so composing the message from the
			// snippet produced `Unowned entries under the root were not read: not read`, which tells the
			// reader nothing they could act on. Measured on a real machine's scan.
			//
			// A risk finding is the other way round: the snippet is the line that fired, and Why is the
			// same paragraph for every hit of that rule (already carried once, in the rule's
			// fullDescription).
			msg := f.Title
			switch {
			case f.Dimension == 0 && f.Why != "":
				msg = f.Title + " — " + clipMessage(f.Why)
			case e.Snippet != "":
				msg = f.Title + ": " + e.Snippet
			}
			rp := map[string]any{"artifact": artifact}
			// The precise categories for THIS finding, refined by where it was found. An injected
			// instruction in auto memory is a hijack AND persistence; the same rule in a skill is only
			// the first. A reviewer filtering on ASI-06 has to find this one.
			if cats := detect.ASIFor(f.RuleID, kind); len(cats) > 0 {
				rp["owasp-agentic"] = cats
			}
			if f.Advisory {
				rp["advisory"] = true
			}
			if f.Source == model.SrcLLM {
				rp["judge"] = true
				rp["escalates"] = f.Escalates
			}
			results = append(results, sarifResult{
				RuleID:              f.RuleID,
				Level:               lvl,
				Message:             sarifText{Text: msg},
				Locations:           []sarifLocation{loc},
				PartialFingerprints: map[string]string{"aguard/v1": fingerprint(f.RuleID, e.File, e.Snippet)},
				Properties:          rp,
			})
		}
	}

	for _, a := range res.Artifacts {
		for _, f := range a.Findings {
			add(f, string(a.Kind)+":"+a.Name, a.Kind)
		}
	}
	// Scan-level notes (coverage gaps, parse failures) are emitted too. "This file was not read" is
	// exactly the kind of thing a gate should surface, and leaving it out of the machine-readable
	// output while keeping it in the terminal one would make the two disagree about completeness.
	for _, n := range res.Notes {
		add(n, "scan", "")
	}

	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, rules[id])
	}
	// Results are sorted so two runs over an unchanged tree produce byte-identical output — the same
	// determinism `overall` is held to, applied to the report.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].RuleID != results[j].RuleID {
			return results[i].RuleID < results[j].RuleID
		}
		li, lj := results[i].Locations, results[j].Locations
		if len(li) > 0 && len(lj) > 0 {
			ui, uj := li[0].PhysicalLocation.ArtifactLocation.URI, lj[0].PhysicalLocation.ArtifactLocation.URI
			if ui != uj {
				return ui < uj
			}
			ri, rj := li[0].PhysicalLocation.Region, lj[0].PhysicalLocation.Region
			if ri != nil && rj != nil && ri.StartLine != rj.StartLine {
				return ri.StartLine < rj.StartLine
			}
		}
		return results[i].Message.Text < results[j].Message.Text
	})

	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name: "aguard", Version: version, InformationURI: infoURI, Rules: ordered,
			}},
			Results:     results,
			Invocations: []sarifInvocation{{ExecutionSuccessful: true}},
			Properties: map[string]any{
				// The score travels with the log, because a reviewer looking at annotations still needs
				// to know whether the environment as a whole passed. `overall` only: the effective
				// score folds in judge findings and gates nothing, so publishing it here would invite
				// exactly the confusion the two numbers exist to prevent.
				"aguard/overall": res.Overall,
				"aguard/root":    sarifURI(res.Root),
			},
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// sarifTags gives the filters a Code Scanning user reaches for. The OWASP categories are included so
// a reviewer can select "show me everything that is goal hijack" across rule families.
func sarifTags(f model.Finding, kind model.ArtifactKind) []string {
	tags := []string{"security"}
	if f.Source == model.SrcLLM {
		tags = append(tags, "advisory", "llm-judge")
	} else if f.Advisory {
		tags = append(tags, "advisory")
	}
	if f.Dimension == 0 {
		tags = append(tags, "coverage")
	}
	tags = append(tags, detect.ASIFor(f.RuleID, kind)...)
	return tags
}

// securitySeverity is GitHub's 0–10 numeric scale, which drives its own severity filter. Advisory and
// judge findings are pinned low for the same reason their level is `note`.
func securitySeverity(f model.Finding) string {
	if f.Source == model.SrcLLM || f.Advisory {
		return "1.0"
	}
	switch f.Severity {
	case model.SevCritical:
		return "9.5"
	case model.SevHigh:
		return "8.0"
	case model.SevMedium:
		return "5.0"
	default:
		return "2.0"
	}
}

// ruleName turns a title into the PascalCase identifier SARIF wants alongside the ID. Falls back to
// the ID when a title has nothing alphanumeric in it.
func ruleName(id, title string) string {
	var b strings.Builder
	upper := true
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			if upper {
				b.WriteRune(toUpper(r))
				upper = false
			} else {
				b.WriteRune(r)
			}
		default:
			upper = true
		}
	}
	if b.Len() == 0 {
		return id
	}
	return b.String()
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r
}

// clipMessage bounds a coverage note's text. SARIF messages land in a table cell in every viewer, and
// some Why paragraphs run several hundred characters explaining a trade-off — useful in the terminal,
// unreadable as an annotation. The full text is still carried once on the rule.
func clipMessage(s string) string {
	const max = 300
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	// Break on a word boundary so the truncation does not land mid-identifier.
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
