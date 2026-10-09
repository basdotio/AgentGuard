// SPDX-License-Identifier: MIT
package clean

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// One config root, every way a person types it. clean used to judge containment against the root AS
// TYPED: withinDir resolves its base with EvalSymlinks but never makes it absolute, EvalSymlinks(".")
// is ".", and "." cannot be related to the resolved trash directory — so `cd ~/.claude && aguard
// clean --root . --undo last` refused the root's own .aguard-trash as "outside the scanned root",
// exactly when an operator reaches for undo. The absolute spelling is the reference for every other.

// spelledEnv is <base>/home/.claude holding one unused skill, plus <base>/sibling and <base>/via → home
// for the spellings typed from elsewhere. base has its symlinks resolved (macOS: /var → /private/var),
// so the absolute spelling is the path the kernel reports after a chdir.
type spelledEnv struct{ base, home, root string }

func spelledShape(t *testing.T) spelledEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := spelledEnv{base: base, home: filepath.Join(base, "home")}
	e.root = filepath.Join(e.home, ".claude")
	dead := filepath.Join(e.root, "skills", "deadskill")
	for _, d := range []string{dead, filepath.Join(base, "sibling")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dead, "SKILL.md"), []byte("---\nname: deadskill\n---\nUnused.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.home, filepath.Join(base, "via")); err != nil {
		t.Fatal(err)
	}
	return e
}

// spelledScan is the scan clean is handed: collect anchors the root, so the artifact path is absolute
// whatever the operator typed.
func spelledScan(e spelledEnv) model.ScanResult {
	return model.ScanResult{
		Root:      e.root,
		Artifacts: []model.ArtifactReport{{Kind: model.KindSkill, Name: "deadskill", Path: filepath.Join(e.root, "skills", "deadskill")}},
		Hygiene:   []model.CleanItem{zombieItem("deadskill", "skills/deadskill")},
	}
}

type spelling struct{ name, dir, root string }

// spelledRoots lists the spellings of e.root and the working directory each is typed from ("" = where
// the test runs). Concatenation, not filepath.Join, for the trailing slash and dot: Join would clean
// away exactly the thing under test.
func spelledRoots(e spelledEnv) []spelling {
	return []spelling{
		{"absolute", "", e.root},
		{"absolute, trailing slash", "", e.root + "/"},
		{"absolute, trailing dot", "", e.root + "/."},
		{"relative", e.home, ".claude"},
		{"relative, trailing dot", e.home, ".claude/."},
		{"dot", e.root, "."},
		{"dot slash", e.root, "./"},
		{"up and back", e.root, "../.claude"},
		{"parent of a subdirectory", filepath.Join(e.root, "skills"), ".."},
		{"relative through the parent", e.base, "home/.claude"},
		{"relative from a sibling", filepath.Join(e.base, "sibling"), "../home/.claude"},
		{"relative, working directory through a symlink", filepath.Join(e.base, "via"), ".claude"},
	}
}

// spelledChdir moves the test into dir the way a shell's cd does — PWD names dir as typed — and
// restores both when the subtest ends. Go 1.23 has no t.Chdir; nothing in this package calls
// t.Parallel. dir == "" leaves both alone.
func spelledChdir(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		return
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD", dir)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restoring the working directory: %v", err)
		}
	})
}

// TestClean_RootSpellingIsTheAbsoluteRun walks one quarantine and its undo under every spelling, each
// step held to what the absolute spelling prints and decides for the same tree.
func TestClean_RootSpellingIsTheAbsoluteRun(t *testing.T) {
	for i, row := range spelledRoots(spelledShape(t)) {
		t.Run(row.name, func(t *testing.T) {
			e := spelledShape(t) // a fresh tree per row: the real apply below moves the skill
			sp := spelledRoots(e)[i]
			res := spelledScan(e)
			src := filepath.Join(e.root, "skills", "deadskill")
			var wantPlan bytes.Buffer
			wantA, err := Apply(&wantPlan, e.root, res, true)
			if err != nil {
				t.Fatalf("absolute --root: the preview was refused: %v", err)
			}
			// The reverse assertion: the absolute spelling plans exactly this move, before the fix and after.
			if want := "[dry-run] would quarantine deadskill → " + filepath.Join(e.root, TrashDirForTest, "deadskill"); !strings.Contains(wantPlan.String(), want) {
				t.Fatalf("absolute --root: the preview does not say %q:\n%s", want, wantPlan.String())
			}
			wantBlocker, wantWhy := QuarantineRefusal(e.root, src)
			var wantKeep bytes.Buffer
			item := model.CleanItem{ID: "D-spelled", Kind: "duplicate", Targets: []string{"deadskill", "other"}}
			if err := KeepBoth(&wantKeep, e.root, item, true); err != nil {
				t.Fatal(err)
			}

			spelledChdir(t, sp.dir)
			// The row typed through a symlinked working directory names the same files in the link's
			// frame (P-012 未决 5); it is compared after mapping the link back onto home.
			inFrame := func(s string) string { return strings.ReplaceAll(s, filepath.Join(e.base, "via"), e.home) }
			var gotPlan bytes.Buffer
			gotA, err := Apply(&gotPlan, sp.root, res, true)
			if err != nil {
				t.Fatalf("--root %q: the preview was refused: %v", sp.root, err)
			}
			if inFrame(gotPlan.String()) != wantPlan.String() || !reflect.DeepEqual(gotA, wantA) {
				t.Errorf("--root %q: the preview differs from the absolute spelling\n got: %s\nwant: %s", sp.root, gotPlan.String(), wantPlan.String())
			}
			if b, w := QuarantineRefusal(sp.root, src); b != wantBlocker || w != wantWhy || b != "" {
				t.Errorf("--root %q: QuarantineRefusal = (%q, %q), the absolute spelling says (%q, %q)", sp.root, b, w, wantBlocker, wantWhy)
			}
			var gotKeep bytes.Buffer
			if err := KeepBoth(&gotKeep, sp.root, item, true); err != nil {
				t.Fatalf("--root %q: keep-both preview: %v", sp.root, err)
			}
			if inFrame(gotKeep.String()) != wantKeep.String() {
				t.Errorf("--root %q: keep-both preview %q, the absolute spelling says %q", sp.root, gotKeep.String(), wantKeep.String())
			}

			// The real move under this spelling, then the undo it hands back.
			if _, err := Apply(io.Discard, sp.root, res, false); err != nil {
				t.Fatalf("--root %q: apply: %v", sp.root, err)
			}
			if _, err := os.Stat(src); !os.IsNotExist(err) {
				t.Fatalf("--root %q: the skill was not quarantined", sp.root)
			}
			var wantUndo, gotUndo bytes.Buffer
			wantU, err := Undo(&wantUndo, e.root, "last", true)
			if err != nil {
				t.Fatalf("absolute --root: the undo preview was refused: %v", err)
			}
			gotU, err := Undo(&gotUndo, sp.root, "last", true)
			if err != nil {
				t.Fatalf("--root %q: the undo preview was refused: %v", sp.root, err)
			}
			if inFrame(gotUndo.String()) != wantUndo.String() || !reflect.DeepEqual(gotU, wantU) {
				t.Errorf("--root %q: the undo preview differs from the absolute spelling\n got: %s\nwant: %s", sp.root, gotUndo.String(), wantUndo.String())
			}
			if _, err := Undo(io.Discard, sp.root, "last", false); err != nil {
				t.Fatalf("--root %q: undo: %v", sp.root, err)
			}
			if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
				t.Errorf("--root %q: undo did not put the skill back: %v", sp.root, err)
			}
		})
	}
}

// TestClean_RootSpellingKeepsTheRefusals: anchoring the root must not open what the resolved checks
// close. A trash directory that is a symlink out of the root is refused under every spelling, and a
// root typed as "." from inside rules/ is still refused as a tree Claude Code loads from.
func TestClean_RootSpellingKeepsTheRefusals(t *testing.T) {
	for i, row := range spelledRoots(spelledShape(t)) {
		t.Run(row.name, func(t *testing.T) {
			e := spelledShape(t)
			sp := spelledRoots(e)[i]
			outside := filepath.Join(e.base, "outside")
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(e.root, TrashDirForTest)); err != nil {
				t.Fatal(err)
			}
			spelledChdir(t, sp.dir)
			_, err := Apply(io.Discard, sp.root, spelledScan(e), false)
			if err == nil || !strings.Contains(err.Error(), "it is a symlink") {
				t.Errorf("--root %q: a symlinked trash directory was not refused as one: %v", sp.root, err)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Errorf("--root %q: content landed outside the root: %v", sp.root, entries)
			}
			if _, err := os.Stat(filepath.Join(e.root, "skills", "deadskill", "SKILL.md")); err != nil {
				t.Errorf("--root %q: the skill moved despite the refusal: %v", sp.root, err)
			}
		})
	}
	t.Run("dot inside rules", func(t *testing.T) {
		e := spelledShape(t)
		rules := filepath.Join(e.root, "rules")
		if err := os.MkdirAll(rules, 0o755); err != nil {
			t.Fatal(err)
		}
		spelledChdir(t, rules)
		_, err := Apply(io.Discard, ".", spelledScan(e), true)
		if err == nil || !strings.Contains(err.Error(), `inside a "rules" directory`) {
			t.Errorf(`--root . inside rules/: not refused as a loaded tree: %v`, err)
		}
	})
}
