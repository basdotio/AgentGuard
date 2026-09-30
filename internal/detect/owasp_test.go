// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// Every rule this scanner can emit must have a DECIDED position on the OWASP catalogue: mapped, or
// deliberately unmapped. A rule that is neither is a rule nobody thought about, and the mapping's
// value is that it can be read as a coverage statement.
//
// The unmapped set is spelled out here rather than inferred, so adding a rule to one of those families
// is a decision someone has to make on purpose.
func TestASIMapping_EveryRuleHasADecidedPosition(t *testing.T) {
	deliberatelyUnmapped := map[string]string{
		"OBF-001": "obfuscation is a technique, not a threat category",
		"OBF-002": "obfuscation is a technique, not a threat category",
		"OBF-003": "obfuscation is a technique, not a threat category",
		"RES-001": "resource abuse is runtime-cascade shaped; we see a loop, and say so as advisory",
		"RES-002": "resource abuse is runtime-cascade shaped; we see a loop, and say so as advisory",
		"RES-003": "resource abuse is runtime-cascade shaped; we see a loop, and say so as advisory",
		"BD-001":  "ASI-10 describes runtime rogue behaviour; we see a conditional, and say so as advisory",
		"BD-002":  "ASI-10 describes runtime rogue behaviour; we see a conditional, and say so as advisory",
		"BD-003":  "ASI-10 describes runtime rogue behaviour; we see a conditional, and say so as advisory",
	}
	for _, r := range builtinRules() {
		mapped := len(asiByRule[r.ID]) > 0
		_, excused := deliberatelyUnmapped[r.ID]
		if !mapped && !excused {
			t.Errorf("%s (%s) has no OWASP position — map it, or add it to deliberatelyUnmapped with a reason",
				r.ID, r.Title)
		}
		if mapped && excused {
			t.Errorf("%s is both mapped and listed as deliberately unmapped", r.ID)
		}
	}
}

// A mapping that names a category outside the catalogue is a typo that would silently produce an
// unrenderable report row.
func TestASIMapping_IdentifiersAreInTheCatalogue(t *testing.T) {
	known := map[string]bool{}
	for _, c := range asiCatalogue {
		known[c.ID] = true
	}
	for rule, ids := range asiByRule {
		for _, id := range ids {
			if !known[id] {
				t.Errorf("rule %s maps to unknown category %q", rule, id)
			}
		}
	}
	for kind, ids := range asiByKind {
		for _, id := range ids {
			if !known[id] {
				t.Errorf("kind %s maps to unknown category %q", kind, id)
			}
		}
	}
}

// Location adds a category only on top of a rule that already has one. This is the distinction a
// rule-only mapping gets wrong (an injected line in auto memory is ASI-01 AND ASI-06) and the one a
// location-only mapping would get wrong (an unbounded loop in memory is still not memory poisoning).
func TestASIFor_LocationRefinesButDoesNotInvent(t *testing.T) {
	if got := ASIFor("INJ-001", model.KindSkill); len(got) != 1 || got[0] != "ASI-01" {
		t.Errorf("injection in a skill = ASI-01 only; got %v", got)
	}
	got := ASIFor("INJ-001", model.KindMemory)
	if len(got) != 2 || got[0] != "ASI-01" || got[1] != "ASI-06" {
		t.Errorf("injection in auto memory is a hijack AND persistence; got %v", got)
	}
	if got := ASIFor("RES-001", model.KindMemory); got != nil {
		t.Errorf("an unmapped rule stays unmapped wherever it is found; got %v", got)
	}
	if got := ASIFor("NO-SUCH-RULE", model.KindSkill); got != nil {
		t.Errorf("an unknown rule maps to nothing; got %v", got)
	}
}

// The catalogue must report the categories we are SILENT about, not only the ones we cover. Four are
// expected to be uncovered today; if that number moves, the claim in the README moves with it.
func TestASICatalogue_ReportsSilence(t *testing.T) {
	cat := ASICatalogue()
	if len(cat) != 10 {
		t.Fatalf("the catalogue has ten entries; got %d", len(cat))
	}
	var uncovered []string
	for _, e := range cat {
		if e.Title == "" {
			t.Errorf("%s has no title", e.ID)
		}
		if !e.Covered {
			uncovered = append(uncovered, e.ID)
		}
	}
	want := map[string]bool{"ASI-08": true, "ASI-09": true, "ASI-10": true}
	if len(uncovered) != len(want) {
		t.Errorf("uncovered categories = %v; the README states which ones we are silent about, so update both together", uncovered)
	}
	for _, id := range uncovered {
		if !want[id] {
			t.Errorf("%s is newly uncovered — was a rule removed?", id)
		}
	}
}
