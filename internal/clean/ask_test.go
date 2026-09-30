// SPDX-License-Identifier: MIT
package clean

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// pair builds one duplicate item. Unlike the withdrawn helper, a MULTI-PAIR fixture is
// constructible — the absence of one is why "no answer may settle more than its own pair" was
// never asserted.
func pair(id, a, b string) model.CleanItem {
	return model.CleanItem{
		ID: id, Kind: duplicateKind, Tier: model.TierChoice, Actionable: true,
		Confidence: model.ConfMedium, Action: model.ActionMove,
		Targets:  []string{a, b},
		Locators: []model.Locator{{Path: "skills/" + a, Name: a}, {Path: "skills/" + b, Name: b}},
		Detail:   "pair " + id,
		Blockers: []string{model.BlockerSideSelection},
	}
}

func answersFor(t *testing.T, keys string, pairs ...model.CleanItem) ([]answer, bool) {
	t.Helper()
	var buf bytes.Buffer
	return askAll(&buf, bufLines{bufio.NewReader(strings.NewReader(keys))}, pairs)
}

// THE headline property, and the one the withdrawn version could not even express: one typed line
// settles exactly one pair.
func TestAsk_OneLineAnswersExactlyOnePair(t *testing.T) {
	got, aborted := answersFor(t, "1\n", pair("D-1", "a", "b"), pair("D-2", "c", "d"))
	if !aborted {
		t.Fatal("input ran out during the second pair, which is an abort")
	}
	if len(got) != 1 || got[0].item.ID != "D-1" || got[0].keep != "a" {
		t.Fatalf("one line must settle one pair and no more; got %+v", got)
	}
}

// Running out of input is the ABSENCE of an answer at every position, including mid-line. The
// withdrawn version aborted only on an EMPTY read, so "1" then Ctrl-D became a move.
func TestAsk_PartialLineAtEOFIsAbort(t *testing.T) {
	for _, in := range []string{"", "1", "1\n2", "  "} {
		got, aborted := answersFor(t, in, pair("D-1", "a", "b"), pair("D-2", "c", "d"))
		if !aborted {
			t.Errorf("input %q ends without a terminated answer and must abort; got %+v", in, got)
		}
		for _, a := range got {
			if a.v == verdictKeep && !strings.HasSuffix(in, "\n") && len(got) == 1 {
				continue // the completed first line is legitimate
			}
		}
	}
}

// There is no default. Nothing is selected until a digit is typed, so a blank line, a stray word or
// a pasted paragraph leaves the pair alone rather than confirming a side.
func TestAsk_NoDefaultSide(t *testing.T) {
	for _, in := range []string{"\n", "hello world\n", "y\n", "3\n", "\t\n"} {
		got, _ := answersFor(t, in, pair("D-1", "a", "b"))
		if len(got) != 1 {
			t.Fatalf("input %q: expected one answer, got %+v", in, got)
		}
		if got[0].v != verdictSkip {
			t.Errorf("input %q must not select a side; got %+v", in, got[0])
		}
	}
}

func TestAsk_VerdictTable(t *testing.T) {
	cases := []struct {
		in   string
		want verdict
		keep string
	}{
		{"1\n", verdictKeep, "a"}, {"2\n", verdictKeep, "b"},
		{" 2 \n", verdictKeep, "b"}, {"B\n", verdictBoth, ""},
		{"s\n", verdictSkip, ""}, {"q\n", verdictAbort, ""}, {"Q\n", verdictAbort, ""},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		v, keep := askOne(&buf, bufLines{bufio.NewReader(strings.NewReader(c.in))}, pair("D-1", "a", "b"))
		if v != c.want || keep != c.keep {
			t.Errorf("%q -> (%v,%q), want (%v,%q)", c.in, v, keep, c.want, c.keep)
		}
	}
}

// Attacker-influenced text on a terminal surface. A skill name is a directory name, and the
// withdrawn picker printed it raw, which let a crafted name forge the selection cursor.
func TestAsk_NamesAreSanitisedBeforePrinting(t *testing.T) {
	var buf bytes.Buffer
	evil := "innocent\x1b[2K\r  [1] keep decoy"
	askOne(&buf, bufLines{bufio.NewReader(strings.NewReader("s\n"))}, pair("D-1", evil, "b"))
	if strings.ContainsAny(buf.String(), "\x1b\r") {
		t.Errorf("control characters reached the terminal: %q", buf.String())
	}
}

// Pairs overlap: skills a,b,c produce (a,b),(a,c),(b,c), so one name can be kept in one answer and
// dropped in another. Honouring both is how the withdrawn walk took away a skill the operator had
// explicitly protected.
func TestReconcile_AKeepBeatsADropOfTheSameName(t *testing.T) {
	answers := []answer{
		{item: pair("D-ab", "a", "b"), v: verdictKeep, keep: "b"}, // drops a
		{item: pair("D-ac", "a", "c"), v: verdictKeep, keep: "a"}, // keeps a
	}
	drops, _, conflicts := reconcile(answers)
	for _, d := range drops {
		if d.name == "a" {
			t.Error("a was explicitly kept in one answer and must not be dropped for another")
		}
	}
	if len(conflicts) != 1 || conflicts[0] != "a" {
		t.Errorf("the contradiction must be reported, not resolved quietly; conflicts=%v", conflicts)
	}
	if len(drops) != 1 || drops[0].name != "c" {
		t.Errorf("the uncontested drop should stand; got %+v", drops)
	}
}

func TestReconcile_DuplicateDropsCollapse(t *testing.T) {
	answers := []answer{
		{item: pair("D-ac", "a", "c"), v: verdictKeep, keep: "a"},
		{item: pair("D-bc", "b", "c"), v: verdictKeep, keep: "b"},
	}
	drops, _, _ := reconcile(answers)
	if len(drops) != 1 || drops[0].name != "c" {
		t.Errorf("c is dropped by two pairs and must be moved once; got %+v", drops)
	}
}

// A pair nothing can be done about is never asked about. Asking and then failing is how one blocked
// pair ended the whole walk after earlier pairs had already moved.
func TestAskablePairs_SkipsWhatCannotBeSettled(t *testing.T) {
	blocked := pair("D-blocked", "a", "b")
	blocked.Blockers = append(blocked.Blockers, model.BlockerOutsideRoot)
	same := pair("D-alias", "alias", "real")
	same.Blockers = append(same.Blockers, model.BlockerSameTarget)

	ask, n := askablePairs([]model.CleanItem{pair("D-ok", "x", "y"), blocked, same})
	if len(ask) != 1 || ask[0].ID != "D-ok" {
		t.Errorf("only the settleable pair may be asked about; got %+v", ask)
	}
	if n != 2 {
		t.Errorf("the ones not asked about must be counted and disclosed; got %d", n)
	}
}

// multiPair builds N skills on disk plus one duplicate item PER PAIR — the fixture the withdrawn
// tests could not construct, which is why every multi-pair property went unasserted.
func multiPair(t *testing.T, names ...string) (string, model.ScanResult) {
	t.Helper()
	root := t.TempDir()
	var arts []model.ArtifactReport
	for _, n := range names {
		dir := filepath.Join(root, "skills", n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body of "+n), 0o644); err != nil {
			t.Fatal(err)
		}
		arts = append(arts, model.ArtifactReport{Kind: model.KindSkill, Name: n, Path: dir})
	}
	var items []model.CleanItem
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			items = append(items, pair("D-"+names[i]+names[j], names[i], names[j]))
		}
	}
	return root, model.ScanResult{Root: root, Artifacts: arts, Hygiene: items}
}

// Aborting discards the whole session: half a set of decisions was never what was asked for.
func TestResolveInteractive_AbortChangesNothing(t *testing.T) {
	root, res := multiPair(t, "a", "b", "c") // three pairs
	var buf bytes.Buffer
	if _, err := ResolveInteractive(&buf, strings.NewReader("1\nq\n"), root, res, false); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b", "c"} {
		if !exists(t, root, "skills", n) {
			t.Errorf("%s moved despite the walk being aborted after an answer had been given", n)
		}
	}
	if exists(t, root, TrashDirForTest) {
		t.Error("an aborted walk must not even create the trash directory")
	}
}

// The overlapping-pairs hazard, end to end on disk: answering all three pairs of a,b,c must never
// take away a skill that some answer named as the survivor. The withdrawn walk could empty skills/
// here and report success.
func TestResolveInteractive_OverlappingPairsNeverDropAKeptSkill(t *testing.T) {
	root, res := multiPair(t, "a", "b", "c") // pairs (a,b) (a,c) (b,c)
	var buf bytes.Buffer
	// keep a · keep c · keep b  — mutually contradictory on purpose
	out, err := ResolveInteractive(&buf, strings.NewReader("1\n2\n1\n"), root, res, false)
	if err != nil {
		t.Fatal(err)
	}
	survivors := 0
	for _, n := range []string{"a", "b", "c"} {
		if exists(t, root, "skills", n) {
			survivors++
		}
	}
	if survivors == 0 {
		t.Fatalf("every skill was quarantined although each was kept by some answer; actions=%+v\n%s",
			out.Actions, buf.String())
	}
	for _, act := range out.Actions {
		if !strings.Contains(buf.String(), "keeping "+act.Skill) {
			continue
		}
		t.Errorf("%s was both kept and moved", act.Skill)
	}
}

// The answers are executed in ONE pass, which is what gives a single batch id, a Result the exit
// code contract can use, and re-derivation of every safety decision at move time.
func TestResolveInteractive_ExecutesOnceThroughTheSameGate(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	out, err := ResolveInteractive(&buf, strings.NewReader("1\n"), root, res, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Actions) != 1 || out.Actions[0].Skill != "devkit" {
		t.Fatalf("keeping browse must quarantine devkit; got %+v", out.Actions)
	}
	if out.Batch == "" {
		t.Error("a run that moved something must report one batch id")
	}
	if !exists(t, root, "skills", "browse") {
		t.Error("the kept side must survive")
	}
	if _, err := Undo(&buf, root, out.Batch, false); err != nil {
		t.Fatal(err)
	}
	if !exists(t, root, "skills", "devkit") {
		t.Errorf("the batch id printed must undo exactly this run; output:\n%s", buf.String())
	}
}

// Keeping both writes the id to the baseline and moves nothing.
func TestResolveInteractive_KeepBothWritesTheBaselineAndMovesNothing(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	if _, err := ResolveInteractive(&buf, strings.NewReader("b\n"), root, res, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ignoreFile))
	if err != nil {
		t.Fatalf("the baseline should have been written: %v", err)
	}
	if !strings.Contains(string(body), "D-testpair") {
		t.Errorf("the id must be recorded; got %q", body)
	}
	for _, n := range []string{"browse", "devkit"} {
		if !exists(t, root, "skills", n) {
			t.Errorf("%s moved on a keep-both", n)
		}
	}
}
