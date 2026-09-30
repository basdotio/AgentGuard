// SPDX-License-Identifier: MIT
package model

import (
	"encoding/json"
	"testing"
)

// These values are the WIRE CONTRACT of `clean --json` and `scan --json`. Anything reading that
// output — a script, a dashboard, another tool — matches on them literally.
//
// The test exists because one of them was renamed mid-development
// (side-selection-unimplemented → side-selection-required) and NOTHING went red. The rename was
// right; the silence was not. A stable string asserted only in prose is a promise nobody can
// verify, so it gets asserted here instead: changing one of these now costs a deliberate edit to a
// test whose failure message says what else to update.
//
// TO CHANGE A VALUE BELOW:
//  1. bump CleanPlanSchema,
//  2. say so in docs/clean-guide.md §9,
//  3. mark the commit breaking (`feat(clean)!: …`) — release notes are generated from commit
//     subjects, so an unmarked subject means the break never reaches the release page.
func TestWireContract_CleanItemEnums(t *testing.T) {
	blockers := map[string]string{
		"BlockerOutsideRoot":        "target-outside-root",
		"BlockerContentEdit":        "content-edit-unimplemented",
		"BlockerSideSelection":      "side-selection-required",
		"BlockerNotUnderSkills":     "target-not-under-skills",
		"BlockerProtected":          "target-is-security-config",
		"BlockerAlreadyQuarantined": "already-quarantined",
		"BlockerUnlocatable":        "target-unlocatable",
		"BlockerSameTarget":         "pair-shares-one-target",
	}
	got := map[string]string{
		"BlockerOutsideRoot":        BlockerOutsideRoot,
		"BlockerContentEdit":        BlockerContentEdit,
		"BlockerSideSelection":      BlockerSideSelection,
		"BlockerNotUnderSkills":     BlockerNotUnderSkills,
		"BlockerProtected":          BlockerProtected,
		"BlockerAlreadyQuarantined": BlockerAlreadyQuarantined,
		"BlockerUnlocatable":        BlockerUnlocatable,
		"BlockerSameTarget":         BlockerSameTarget,
	}
	for name, want := range blockers {
		if got[name] != want {
			t.Errorf("blocker %s is %q on the wire, was %q — see this file's header before changing it",
				name, got[name], want)
		}
	}
	if len(got) != len(blockers) {
		t.Errorf("a blocker was added or removed without updating this contract: %v", got)
	}

	tiers := map[Tier]string{TierAuto: "A1", TierChoice: "A2", TierConfig: "B", TierContent: "C"}
	for tier, want := range tiers {
		if string(tier) != want {
			t.Errorf("tier %q must serialise as %q", tier, want)
		}
	}
	confs := map[Confidence]string{ConfLow: "low", ConfMedium: "medium", ConfHigh: "high"}
	for c, want := range confs {
		if string(c) != want {
			t.Errorf("confidence %q must serialise as %q", c, want)
		}
	}
	actions := map[Action]string{ActionNone: "", ActionMove: "move",
		ActionConfigRemove: "config-remove", ActionLineDelete: "line-delete"}
	for a, want := range actions {
		if string(a) != want {
			t.Errorf("action %q must serialise as %q", a, want)
		}
	}
}

// The JSON KEYS are as much of the contract as the values. A struct-tag typo renames a field for
// every consumer at once and is invisible in review.
func TestWireContract_CleanPlanShape(t *testing.T) {
	b, err := json.Marshal(CleanPlan{
		Schema: CleanPlanSchema, ToolVersion: "v0", Root: "/r",
		Items: []CleanItem{{
			ID: "D-1", Kind: "duplicate_fn", Tier: TierChoice, Actionable: true,
			Confidence: ConfMedium, Action: ActionMove, Targets: []string{"a", "b"},
			Locators: []Locator{{Path: "skills/a", Name: "a"}},
			Detail:   "d", Hint: "h", Blockers: []string{BlockerSideSelection},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "tool_version", "root", "items"} {
		if _, ok := envelope[k]; !ok {
			t.Errorf("envelope lost the %q key; consumers key off it", k)
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["items"], &items); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "kind", "tier", "actionable", "confidence", "action",
		"targets", "locators", "detail", "hint", "blockers"} {
		if _, ok := items[0][k]; !ok {
			t.Errorf("item lost the %q key", k)
		}
	}
}

// The schema number only means something if it moves when the shape does. This is a reminder in
// executable form: it fails the moment someone bumps the constant without revisiting the tests
// above, and it documents what the current number covers.
func TestWireContract_SchemaIsAtTheVersionTheseTestsDescribe(t *testing.T) {
	if CleanPlanSchema != 1 {
		t.Fatalf("schema is now %d; update the assertions above to describe THAT shape, then set "+
			"this number to match", CleanPlanSchema)
	}
}
