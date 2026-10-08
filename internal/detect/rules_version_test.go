// SPDX-License-Identifier: MIT
package detect

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// The rules version is how two reports say whether the same rules produced them. These tests pin
// the two halves of that promise: every property that decides whether a finding fires, and how
// heavily, moves the version — and the prose a reader sees does not.
//
// There is deliberately NO golden literal here, unlike TestHashGolden. This value is supposed to
// change with every rule change, so a literal would be edited in every such commit and would
// protect nothing. The published value lives in the docs/rules.md header, and the docs drift check
// (make verify, CI) is the assertion that fails when code and published value disagree.

// withRule returns a fresh copy of rules with element i replaced by f(rules[i]); the input is
// never modified.
func withRule(rules []Rule, i int, f func(Rule) Rule) []Rule {
	out := append([]Rule(nil), rules...)
	out[i] = f(out[i])
	return out
}

// indexOf finds a rule by ID in the built-in set, failing the test if it is gone.
func indexOf(t *testing.T, rules []Rule, id string) int {
	t.Helper()
	for i, r := range rules {
		if r.ID == id {
			return i
		}
	}
	t.Fatalf("rule %s is not in builtinRules(); pick another fixture rule", id)
	return -1
}

func TestRulesVersion_IsStable(t *testing.T) {
	a, b := RulesVersion(), RulesVersion()
	if a != b {
		t.Fatalf("RulesVersion is not stable across calls: %q then %q", a, b)
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(a) {
		t.Errorf("RulesVersion = %q, want 12 lowercase hex digits", a)
	}
	if got := rulesVersion(builtinRules(), rulesEpoch); got != a {
		t.Errorf("RulesVersion() = %q but rulesVersion(builtinRules(), rulesEpoch) = %q — the exported value must be the table's", a, got)
	}
}

func TestRulesVersion_MovesWithWhatDecidesAFinding(t *testing.T) {
	base := builtinRules()
	inj := indexOf(t, base, "INJ-001")
	excepted := -1
	for i, r := range base {
		if r.except != nil {
			excepted = i
			break
		}
	}
	if excepted < 0 {
		t.Fatal("no built-in rule carries an except pattern; the fixture needs one")
	}
	want := rulesVersion(base, rulesEpoch)

	swapped := append([]Rule(nil), base...)
	swapped[0], swapped[1] = swapped[1], swapped[0]

	cases := []struct {
		name  string
		rules []Rule
		epoch int
	}{
		{"pattern", withRule(base, inj, func(r Rule) Rule {
			r.re = regexp.MustCompile(r.re.String() + "|never-in-a-real-file")
			return r
		}), rulesEpoch},
		{"except added", withRule(base, inj, func(r Rule) Rule { return r.exceptWhen(`harmless`) }), rulesEpoch},
		{"except removed", withRule(base, excepted, func(r Rule) Rule { r.except = nil; return r }), rulesEpoch},
		{"severity", withRule(base, inj, func(r Rule) Rule { r.Severity = model.SevCritical; return r }), rulesEpoch},
		{"dimension", withRule(base, inj, func(r Rule) Rule { r.Dimension = 4; return r }), rulesEpoch},
		{"id", withRule(base, inj, func(r Rule) Rule { r.ID = "INJ-999"; return r }), rulesEpoch},
		{"advisory", withRule(base, inj, func(r Rule) Rule { r.Advisory = !r.Advisory; return r }), rulesEpoch},
		{"hook only", withRule(base, inj, func(r Rule) Rule { r.HookOnly = !r.HookOnly; return r }), rulesEpoch},
		{"connector only", withRule(base, inj, func(r Rule) Rule { r.ConnectorOnly = !r.ConnectorOnly; return r }), rulesEpoch},
		{"raw only", withRule(base, inj, func(r Rule) Rule { r.RawOnly = !r.RawOnly; return r }), rulesEpoch},
		{"script only", withRule(base, inj, func(r Rule) Rule { r.ScriptOnly = !r.ScriptOnly; return r }), rulesEpoch},
		// Findings on one line are appended in rule order, so a reorder can change the report.
		{"order", swapped, rulesEpoch},
		{"rule removed", append([]Rule(nil), base[1:]...), rulesEpoch},
		{"epoch", base, rulesEpoch + 1},
	}
	seen := map[string]string{want: "the built-in table"}
	for _, c := range cases {
		got := rulesVersion(c.rules, c.epoch)
		if got == want {
			t.Errorf("%s changed but the rules version did not (%s) — two reports would claim the same rules", c.name, got)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s hash to the same version %s", c.name, prev, got)
		}
		seen[got] = c.name
	}
}

// TestRulesVersion_IgnoresProse is the reverse assertion: rewording every title, explanation and
// reference leaves the version where it was. Those strings explain a finding; they do not decide
// one, and a version that moved on a typo fix would cry "the rules changed" when they did not.
func TestRulesVersion_IgnoresProse(t *testing.T) {
	base := builtinRules()
	reworded := make([]Rule, len(base))
	for i, r := range base {
		r.Title = "reworded title " + r.ID
		r.Why = "reworded explanation"
		r.Ref = "reworded reference"
		reworded[i] = r
	}
	if got, want := rulesVersion(reworded, rulesEpoch), rulesVersion(base, rulesEpoch); got != want {
		t.Errorf("rewording titles/explanations/references moved the rules version %s → %s", want, got)
	}
}

// TestRulesVersion_EveryRuleFieldIsDecided makes adding a field to Rule a decision. A new
// behavioural flag that nobody added to the hash would let two different rule tables share a
// version, and nothing else would notice.
func TestRulesVersion_EveryRuleFieldIsDecided(t *testing.T) {
	hashed := map[string]bool{
		"ID": true, "Dimension": true, "Severity": true, "Advisory": true,
		"HookOnly": true, "ConnectorOnly": true, "RawOnly": true, "ScriptOnly": true,
		"re": true, "except": true,
	}
	prose := map[string]bool{"Title": true, "Why": true, "Ref": true}
	rt := reflect.TypeOf(Rule{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !hashed[name] && !prose[name] {
			t.Errorf("Rule.%s is neither hashed into the rules version nor listed as prose — "+
				"decide which (rules_version.go), then add it to one of the two lists here", name)
		}
	}
	if n := rt.NumField(); n != len(hashed)+len(prose) {
		t.Errorf("Rule has %d fields but the two lists name %d — a listed field no longer exists", n, len(hashed)+len(prose))
	}
}
