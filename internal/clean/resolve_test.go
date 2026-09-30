// SPDX-License-Identifier: MIT
package clean

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// dupSetup builds a real pair on disk plus the item hygiene would emit for it.
func dupSetup(t *testing.T, names ...string) (string, model.ScanResult) {
	t.Helper()
	root := t.TempDir()
	var arts []model.ArtifactReport
	var locs []model.Locator
	for _, n := range names {
		dir := filepath.Join(root, "skills", n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body of "+n), 0o644); err != nil {
			t.Fatal(err)
		}
		arts = append(arts, model.ArtifactReport{Kind: model.KindSkill, Name: n, Path: dir})
		locs = append(locs, model.Locator{Path: "skills/" + n, Name: n})
	}
	item := model.CleanItem{
		ID: "D-testpair", Kind: duplicateKind, Tier: model.TierChoice, Actionable: true,
		Confidence: model.ConfMedium, Action: model.ActionMove,
		Targets: names, Locators: locs,
		Detail:   "Two skill descriptions are highly similar.",
		Blockers: []string{model.BlockerSideSelection},
	}
	return root, model.ScanResult{Root: root, Artifacts: arts, Hygiene: []model.CleanItem{item}}
}

func exists(t *testing.T, parts ...string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(parts...))
	return err == nil
}

// The whole feature in one assertion: the operator names the survivor, and the OTHER one moves.
func TestResolve_QuarantinesTheSideNotKept(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	out, err := Resolve(&buf, root, res, "D-testpair", "browse", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Actions) != 1 || out.Actions[0].Skill != "devkit" || !out.Actions[0].Applied {
		t.Fatalf("expected devkit to be the one moved; got %+v", out.Actions)
	}
	if !exists(t, root, "skills", "browse") {
		t.Error("the KEPT side must still be in place — moving it is the one unrecoverable mistake here")
	}
	if exists(t, root, "skills", "devkit") {
		t.Error("the side not kept should have left skills/")
	}
	if !exists(t, root, TrashDirForTest, "devkit", "SKILL.md") {
		t.Error("it must be recoverable from the trash, like any other quarantine")
	}
	// The manifest row records what KIND of decision this was, so an audit of the log can tell a
	// human's choice from an automatic zombie sweep.
	recs, err := readManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	var sawKind bool
	for _, r := range recs {
		if r.Name == "devkit" && r.Kind == duplicateKind {
			sawKind = true
		}
	}
	if !sawKind {
		t.Errorf("manifest should record kind=%q; got %+v", duplicateKind, recs)
	}
}

// The load-bearing refusal. An item whose entire content is "somebody has to choose" must never be
// acted on by a command that did not carry a choice — no default side, no first-listed, no newest.
func TestResolve_WithoutAChoiceMovesNothing(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	_, err := Resolve(&buf, root, res, "D-testpair", "", false)
	if err == nil {
		t.Fatal("resolving without --keep must fail, not pick a side")
	}
	if !strings.Contains(err.Error(), "browse") || !strings.Contains(err.Error(), "devkit") {
		t.Errorf("the error should name both candidates so the operator can copy one; got %v", err)
	}
	for _, n := range []string{"browse", "devkit"} {
		if !exists(t, root, "skills", n) {
			t.Errorf("%s moved despite no choice being made", n)
		}
	}
	if exists(t, root, TrashDirForTest) {
		t.Error("a refused run must not even create the trash directory")
	}
}

// A near-miss is a typo, and the only safe response to a typo about which skill to take away is to
// hand both names back. Prefix or case matching here would eventually move the wrong one.
func TestResolve_RefusesAKeepThatNamesNeitherSide(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	for _, bad := range []string{"brows", "BROWSE", "browse "} {
		if _, err := Resolve(&buf, root, res, "D-testpair", bad, false); err == nil {
			t.Errorf("--keep %q is not either name and must be refused", bad)
		}
	}
	if !exists(t, root, "skills", "browse") || !exists(t, root, "skills", "devkit") {
		t.Error("nothing may move on a rejected --keep")
	}
}

// Answering the side-selection blocker must not answer any OTHER blocker. This is the reason
// Resolve removes exactly one reason and then asks Executable(), instead of skipping the gate.
func TestResolve_StillRefusesWhenAnotherBlockerRemains(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	res.Hygiene[0].Blockers = []string{model.BlockerSideSelection, model.BlockerOutsideRoot}
	var buf bytes.Buffer
	_, err := Resolve(&buf, root, res, "D-testpair", "browse", false)
	if err == nil {
		t.Fatal("a pair with a target outside root must stay refused even once a side is chosen")
	}
	if !strings.Contains(err.Error(), model.BlockerOutsideRoot) {
		t.Errorf("the remaining blocker should be named; got %v", err)
	}
	if exists(t, root, TrashDirForTest) {
		t.Error("nothing may move")
	}
}

// Addressing the wrong kind of item is a different mistake from addressing nothing, and sends the
// operator to a different command.
func TestResolve_RefusesANonDuplicateID(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	res.Hygiene = append(res.Hygiene, zombieItem("deadskill", "skills/deadskill"))
	var buf bytes.Buffer
	_, err := Resolve(&buf, root, res, "Z-testdeadskill", "browse", false)
	if err == nil || !strings.Contains(err.Error(), "zombie") {
		t.Errorf("a zombie id passed to --resolve should say so; got %v", err)
	}
	if _, err := Resolve(&buf, root, res, "D-nosuchthing", "browse", false); err == nil {
		t.Error("an unknown id must fail rather than resolve nothing quietly")
	}
}

// Undo knows nothing about how a row got there. A human's choice is reversible on exactly the same
// terms as an automatic sweep.
func TestResolve_UndoRestoresTheQuarantinedSide(t *testing.T) {
	root, res := dupSetup(t, "browse", "devkit")
	var buf bytes.Buffer
	if _, err := Resolve(&buf, root, res, "D-testpair", "browse", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Undo(&buf, root, "last", false); err != nil {
		t.Fatal(err)
	}
	if !exists(t, root, "skills", "devkit", "SKILL.md") {
		t.Errorf("undo must put the pair back together; output:\n%s", buf.String())
	}
}

// --keep-both writes to the baseline, which is the one file where an injected line silently
// disables the tool. Skill names are directory names chosen by whoever shipped the artifact, and on
// Unix a directory name may contain a newline: without sanitising, a name of "evil\n*  **" appends
// a rule suppressing every finding under every path.
func TestKeepBoth_CannotInjectABaselineRule(t *testing.T) {
	root := t.TempDir()
	item := model.CleanItem{
		ID: "D-testpair", Kind: duplicateKind,
		Targets: []string{"innocent", "evil\n*  **"},
	}
	var buf bytes.Buffer
	if err := KeepBoth(&buf, root, item, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ignoreFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("a name with a newline produced %d baseline lines: %q", len(lines), lines)
	}
	if strings.Contains(string(body), "**") {
		t.Errorf("a wildcard suppression reached the baseline through a skill name: %q", body)
	}
	if !strings.HasPrefix(lines[0], "D-testpair") {
		t.Errorf("the id must still be recorded; got %q", lines[0])
	}
}

// Appending must never follow a symlink out of the root the operator named — the same rule the
// trash directory and the manifest already hold to.
func TestKeepBoth_RefusesASymlinkedBaseline(t *testing.T) {
	root := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(elsewhere, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, ignoreFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var buf bytes.Buffer
	if err := KeepBoth(&buf, root, model.CleanItem{ID: "D-x", Targets: []string{"a", "b"}}, false); err == nil {
		t.Fatal("a symlinked baseline must be refused")
	}
	body, _ := os.ReadFile(elsewhere)
	if string(body) != "original\n" {
		t.Errorf("the symlink target was written through: %q", body)
	}
}

func TestKeepBoth_DryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	if err := KeepBoth(&buf, root, model.CleanItem{ID: "D-x", Targets: []string{"a", "b"}}, true); err != nil {
		t.Fatal(err)
	}
	if exists(t, root, ignoreFile) {
		t.Error("dry-run created the baseline file")
	}
}

// An alias install — skills/alias -> skills/real — makes two artifacts out of one directory. Every
// check downstream agrees to the move, the content hash included, because the two sides ARE the
// same tree: --keep alias computes drop=real, and real is what alias points at. The run reports
// success and takes away the skill the operator named as the survivor.
func TestResolve_RefusesAPairThatIsOneDirectory(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "skills", "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "skills", "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	item := model.CleanItem{
		ID: "D-alias", Kind: duplicateKind, Tier: model.TierChoice, Actionable: true,
		Confidence: model.ConfMedium, Action: model.ActionMove,
		Targets:  []string{"alias", "real"},
		Locators: []model.Locator{{Path: "skills/real", Name: "alias"}, {Path: "skills/real", Name: "real"}},
		Blockers: []string{model.BlockerSideSelection, model.BlockerSameTarget},
	}
	res := model.ScanResult{Root: root, Hygiene: []model.CleanItem{item},
		Artifacts: []model.ArtifactReport{
			{Kind: model.KindSkill, Name: "alias", Path: real},
			{Kind: model.KindSkill, Name: "real", Path: real},
		}}

	var buf bytes.Buffer
	if _, err := Resolve(&buf, root, res, "D-alias", "alias", false); err == nil {
		t.Fatal("a pair that is one directory must be refused; there is no second copy to move")
	}
	if _, err := os.Stat(filepath.Join(real, "SKILL.md")); err != nil {
		t.Errorf("the one real directory must still be there: %v", err)
	}
}

// A baseline whose last line lacks a newline is ordinary. Appending blind fused the operator's
// existing rule with the new id, destroying the rule AND failing to record the id, while stdout
// reported success for both.
func TestKeepBoth_DoesNotFuseWithAnUnterminatedLastLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ignoreFile), []byte("EXEC-001"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := KeepBoth(&buf, root, model.CleanItem{ID: "D-1a2b3c4d", Targets: []string{"a", "b"}}, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ignoreFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "EXEC-001" {
		t.Fatalf("the existing rule must survive intact; got %q", body)
	}
	if !strings.HasPrefix(lines[1], "D-1a2b3c4d") {
		t.Errorf("the id must be recorded on its own line; got %q", lines[1])
	}
}
