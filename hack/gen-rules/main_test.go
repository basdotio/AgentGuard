// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// TestEveryJudgeRuleHasADefinition: SARIF describes a judge rule (Source=llm) with the tool's own
// definition, model.JudgeRuleText, and a judge rule without one is described by nothing — never
// by a finding's Why, which is one model's sentence about one artifact. Which ids are judge rules
// is read from this generator's own llm and notes tables, not listed: TestEveryRuleIDIsDocumented
// already forces every id the code can emit into those tables, so the chain is closed — a new
// judge rule cannot reach Code Scanning without help text because a hand-kept list in a report
// test was not updated.
//
// The two texts are separate copies for different readers (this table writes the reference, the
// model table the one-sentence SARIF help); merging them is a recorded follow-up (P-006).
func TestEveryJudgeRuleHasADefinition(t *testing.T) {
	ids := make([]string, 0, len(llm)+len(notes))
	for _, e := range llm {
		ids = append(ids, e.id)
	}
	judgeNotes := 0
	for _, e := range notes {
		if strings.HasPrefix(e.id, "LLM-") {
			ids = append(ids, e.id)
			judgeNotes++
		}
	}
	if len(llm) == 0 || judgeNotes == 0 {
		t.Fatalf("found %d judge rules and %d judge notes — the tables moved, not the definitions", len(llm), judgeNotes)
	}
	for _, id := range ids {
		if model.JudgeRuleText(id) == "" {
			t.Errorf("judge rule %s is in the reference but has no model.JudgeRuleText entry, so SARIF would describe it with nothing", id)
		}
	}
}

// idLiteral matches a rule-ID string literal as the code writes it: "EXEC-001", "COV-000".
var idLiteral = regexp.MustCompile(`"([A-Z]{2,10}-[0-9]{3})"`)

// TestEveryRuleIDIsDocumented is the docs half of invariant #5 (nothing is silently omitted).
//
// Most of docs/rules.md is generated from detect.Rules(), so those entries cannot drift. The
// rest — permission checks, LLM verdicts, structural checks, dimension-0 notes — is built
// inline in its own package rather than from a table, so it has to be hand-listed in this
// generator. Hand-listed is exactly what goes stale, so instead of trusting it, this test
// reads every non-test source file, collects every rule-ID literal the code can emit, and
// fails on any ID that no group documents.
//
// A reader who cannot find SUP-004 in the reference knows to read the source. A reader who
// finds the wrong severity acts on it — which is why this is a test and not a review checklist
// item. Writing the catalogue entry is the cost of adding a rule ID.
func TestEveryRuleIDIsDocumented(t *testing.T) {
	documented := map[string]bool{}
	for _, r := range detect.Rules() {
		documented[r.ID] = true
	}
	for _, e := range allDocumented() {
		documented[e.id] = true
	}

	// Literals that are not rule IDs, or IDs the tool never emits.
	allowed := map[string]bool{
		"NOTE-0": true, // test fixture placeholder
	}

	root := repoRoot(t)
	found := map[string][]string{} // id -> files mentioning it
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// dist/bin are build output; .git is not source. The underscore directories are
			// scratch for the remote-tool workarounds described in .gitignore, and
			// _gitlocks/merge.extracted holds a STALE COPY OF THE SOURCE TREE — walking it
			// meant this test was reading 19 .go files that are not this build. It passed
			// only because the old and new ID sets happened to agree; the first rule ID
			// renamed or retired would have turned it red over a file nobody edits.
			switch info.Name() {
			case ".git", "dist", "bin", "node_modules",
				"_gitlocks", "_sync", "_audit", "_to_delete":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The generator's own tables are the documentation, not a second source of truth.
		if strings.Contains(path, filepath.Join("hack", "gen-rules")) {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range idLiteral.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = append(found[m[1]], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("found no rule-ID literals at all — the scan is broken, not the code")
	}

	for id, files := range found {
		if documented[id] || allowed[id] {
			continue
		}
		t.Errorf("rule ID %s is emitted by %s but appears in no group in hack/gen-rules — "+
			"add it so docs/rules.md describes every finding the tool can produce",
			id, strings.Join(dedupe(files), ", "))
	}
}

// TestDocumentedMetadataMatchesEngine catches the other direction for generated entries: a
// hand-written duplicate of a rule that the engine also defines, with different values. The
// engine wins; the catalogue must not restate it.
func TestDocumentedMetadataMatchesEngine(t *testing.T) {
	engine := map[string]bool{}
	for _, r := range detect.Rules() {
		engine[r.ID] = true
	}
	for _, e := range allDocumented() {
		if engine[e.id] {
			t.Errorf("%s is defined in detect.Rules() AND hand-listed in the generator — "+
				"remove the hand-written copy, it can only drift", e.id)
		}
	}
}

// TestRulesDocHeaderCarriesRulesVersion: the reference a reader opens names the same rules version
// a report carries, so a report can be matched to the table that produced it. The drift check
// (make verify, CI) proves the committed file is what the generator writes; this proves the
// generator writes the version at all, and fails in a plain `go test` too.
func TestRulesDocHeaderCarriesRulesVersion(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	v := detect.RulesVersion()
	if !strings.Contains(string(b), "**Rules version `"+v+"`**") {
		t.Errorf("docs/rules.md does not name rules version %s in its header — run `make docs`", v)
	}
}

// rulesDocHeader is the committed docs/rules.md up to its table of contents, whitespace-folded so
// a phrase can be asserted however the generator wraps it.
func rulesDocHeader(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(string(b), "## Contents")
	if !ok {
		t.Fatal("docs/rules.md has no `## Contents` heading, so the header has no end to cut at")
	}
	return strings.Join(strings.Fields(head), " ")
}

// TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion: the rules version covers deterministic
// detection only, and this page lists the LLM entries next to the engine rules, so the header has
// to say which side of that line they are on. A reader who took LLM-001 to be covered would read
// two equal versions as "the judge was the same", which nothing in the hash supports.
//
// Nor may it send the reader elsewhere for the judge: "a report's judge summary says how it ran"
// promised what no field holds. The judge summary records whether it ran, over how much and
// against which endpoint — not its model, prompt version, samples or authority — so the only
// thing in a report that pins the judge's code today is tool_version.
func TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion(t *testing.T) {
	h := rulesDocHeader(t)
	for _, want := range []string{
		"It covers deterministic detection only.",
		"**Outside `rules_version` entirely:** every `LLM-` ID",
		"today a report identifies the judge's code only through `tool_version`",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("docs/rules.md header does not say %q — fix hack/gen-rules, then run `make docs`", want)
		}
	}
	if strings.Contains(h, "how it ran") {
		t.Error("docs/rules.md header still says something in the report tells how the judge ran; nothing there identifies the judge but tool_version")
	}
}

// TestRulesDocHeaderSaysWhatTheHashCovers: the header sorts every ID a report can carry into one of
// three classes and states each class once — hashed (the engine rules), covered only by the epoch
// (structural, permission, the non-LLM notes), outside rules_version (every LLM- ID). The previous
// wording put "scan-note and gate entries" on the epoch and, a sentence later, "every LLM- ID,
// notes included" outside it; LLM-000/002/005 are scan notes, so on a first read the two
// contradicted each other. It also claimed the epoch for GATE-000, a hook reply that no report
// carries, which promised nothing.
//
// The classes are built here from the generator's own tables, the header must state each with its
// count, and every ID on the page must land in exactly one class or be GATE-000. A new gate message,
// or an LLM- ID filed under a deterministic group, then fails here instead of falling under
// whichever sentence a reader finds first. The counts are the tables' own, so they cannot go stale.
func TestRulesDocHeaderSaysWhatTheHashCovers(t *testing.T) {
	const (
		notInAReport = "GATE-000" // a hook reply (internal/gate); no scan result carries it
		scanGate     = "GATE-001" // raised by `scan` (internal/gate/status.go)
	)
	classes := map[string][]string{}
	put := func(id, class string) {
		classes[id] = append(classes[id], class)
		if strings.HasPrefix(id, "LLM-") && class != "outside" {
			classes[id] = append(classes[id], "outside") // the header puts every LLM- ID outside
		}
	}
	rules := detect.Rules()
	for _, r := range rules {
		put(r.ID, "hashed")
	}
	for _, e := range append(append([]entry{}, structural...), permission...) {
		put(e.id, "epoch")
	}
	epochNotes, llmNotes := 0, 0
	for _, e := range notes {
		if strings.HasPrefix(e.id, "LLM-") {
			llmNotes++
			put(e.id, "outside")
			continue
		}
		epochNotes++
		put(e.id, "epoch")
	}
	for _, e := range llm {
		put(e.id, "outside")
	}
	for _, e := range gateNotes {
		switch e.id {
		case scanGate:
			put(e.id, "epoch")
		case notInAReport:
			put(e.id, "in no report")
		}
	}
	page := []string{}
	for _, r := range rules {
		page = append(page, r.ID)
	}
	for _, e := range allDocumented() {
		page = append(page, e.id)
	}
	for _, id := range page {
		if c := classes[id]; len(c) != 1 {
			t.Errorf("%s is in %d header classes %v, want exactly one — say in hack/gen-rules where rules_version leaves it", id, len(c), c)
		}
	}

	h := rulesDocHeader(t)
	for _, want := range []string{
		fmt.Sprintf("**Hashed:** the %d engine rules — each one's ID, dimension, severity, flags and pattern", len(rules)),
		fmt.Sprintf("**Covered only by the epoch:** the %d structural checks, the %d permission checks, the %d scan notes that are not `LLM-` IDs, and `%s`",
			len(structural), len(permission), epochNotes, scanGate),
		fmt.Sprintf("**Outside `rules_version` entirely:** every `LLM-` ID — the %d judge findings and the %d `LLM-` scan notes", len(llm), llmNotes),
	} {
		if !strings.Contains(h, want) {
			t.Errorf("docs/rules.md header does not say %q — fix hack/gen-rules, then run `make docs`", want)
		}
	}
	for _, gone := range []string{"It hashes what decides a finding", "scan-note and gate entries", "notes included", notInAReport} {
		if strings.Contains(h, gone) {
			t.Errorf("docs/rules.md header still says %q — state each ID class once, and nothing about %s, which no report carries", gone, notInAReport)
		}
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find go.mod above the test's working directory")
	return ""
}
