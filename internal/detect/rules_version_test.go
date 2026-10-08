// SPDX-License-Identifier: MIT
package detect

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"strings"
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

// TestRulesVersion_EveryRuleFieldIsDecided makes adding a field to Rule a decision, and then
// checks the decision. A new behavioural flag that nobody added to the hash would let two different
// rule tables share a version, and nothing else would notice — and so would a field that is LISTED
// as hashed while rulesVersion never writes it, which a list alone cannot catch. So every field is
// changed in a copy of one rule: an exported one by reflection (perturb), an unexported one by an
// explicit mutation, since reflection cannot set it. A hashed field must move the version; a prose
// field must not.
func TestRulesVersion_EveryRuleFieldIsDecided(t *testing.T) {
	hashed := map[string]bool{
		"ID": true, "Dimension": true, "Severity": true, "Advisory": true,
		"HookOnly": true, "ConnectorOnly": true, "RawOnly": true, "ScriptOnly": true,
		"re": true, "except": true,
	}
	prose := map[string]bool{"Title": true, "Why": true, "Ref": true}
	// The unexported fields, set by hand. TestRulesVersion_MovesWithWhatDecidesAFinding keeps its
	// own named cases for both, including taking an except away.
	unexported := map[string]func(Rule) Rule{
		"re":     func(r Rule) Rule { r.re = regexp.MustCompile(r.re.String() + "|never-in-a-real-file"); return r },
		"except": func(r Rule) Rule { return r.exceptWhen(`harmless`) },
	}
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

	base := builtinRules()
	target := indexOf(t, base, "INJ-001")
	want := rulesVersion(base, rulesEpoch)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		mutate := unexported[f.Name]
		switch {
		case f.IsExported():
			mutate = func(r Rule) Rule { perturb(t, reflect.ValueOf(&r).Elem().Field(i), "Rule."+f.Name); return r }
		case mutate == nil:
			t.Errorf("Rule.%s is unexported, so reflection cannot change it — add an explicit mutation for it here", f.Name)
			continue
		}
		moved := rulesVersion(withRule(base, target, mutate), rulesEpoch) != want
		switch {
		case hashed[f.Name] && !moved:
			t.Errorf("Rule.%s is listed as hashed, but changing it does not move the rules version — "+
				"hash it in rulesVersion, or move it to the prose list", f.Name)
		case prose[f.Name] && moved:
			t.Errorf("Rule.%s is listed as prose, but changing it moves the rules version — list it as hashed", f.Name)
		}
	}
}

// perturb sets v to a different value of its own type: a flipped bool, an integer one higher, a
// string — or a string-kinded type such as model.Severity — with a byte appended. Any other kind
// fails the test, so a field of a new kind cannot pass until someone says how to change it.
func perturb(t *testing.T, v reflect.Value, name string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.String:
		v.SetString(v.String() + "x")
	default:
		t.Fatalf("%s has kind %s, which perturb does not know how to change — teach it", name, v.Kind())
	}
}

// constDoc returns the doc comment of the package-level const name in file, whitespace-folded,
// so a test can read what the source tells the next person who edits it.
func constDoc(t *testing.T, file, name string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for _, n := range vs.Names {
				if n.Name != name {
					continue
				}
				doc := vs.Doc
				if doc == nil {
					doc = gd.Doc
				}
				if doc == nil {
					t.Fatalf("const %s in %s has no doc comment", name, file)
				}
				return strings.Join(strings.Fields(doc.Text()), " ")
			}
		}
	}
	t.Fatalf("no const %s in %s", name, file)
	return ""
}

// TestRulesEpoch_ScopeIsDeterministicOnly pins where the epoch's duty stops (spec §5.1). The
// rules version exists so the deterministic score, overall, can be matched to the rules that
// produced it; the judge moves only overall_effective, and nothing in a report but tool_version
// identifies its code. A doc comment that lists judge internals as epoch-covered code obliges
// every judge change to bump the epoch — a coupling nobody editing internal/judge can see — and a
// judge change that skipped the bump would leave the version vouching for detection it does not
// track.
func TestRulesEpoch_ScopeIsDeterministicOnly(t *testing.T) {
	doc := constDoc(t, "rules_version.go", "rulesEpoch")
	for _, want := range []string{
		"DETERMINISTIC detection only",
		"The LLM judge is outside the rules version",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("rulesEpoch's doc comment does not say %q — its scope must stop at deterministic detection", want)
		}
	}
	for _, judgeInternal := range []string{"clampSeverity", "LLM-007", "the judge's rule mapping"} {
		if strings.Contains(doc, judgeInternal) {
			t.Errorf("rulesEpoch's doc comment lists %q as epoch-covered code; the judge is outside the rules version", judgeInternal)
		}
	}
}
