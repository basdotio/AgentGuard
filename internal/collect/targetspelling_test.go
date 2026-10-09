// SPDX-License-Identifier: MIT
package collect

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// `check` and `hash` route a directory through looksLikeRoot, and looksLikeRoot asked
// filepath.Base of the path AS TYPED whether it is `.claude`. Base(".") is ".", and so is
// Base("<abs>/.claude/."), so `check .` from inside a config root read the whole tree as one
// directory artifact while `check ../.claude` and the absolute path took the root layout. The router
// must answer about the directory, not about the string.

// targetSpellings extends anchorSpellings with the three spellings whose last element is not the
// directory's name: a trailing dot after a relative path, a way up and back, and the parent of a
// subdirectory.
func targetSpellings(e anchorEnv) []anchorSpelling {
	return append(anchorSpellings(e),
		anchorSpelling{"relative, trailing dot", e.home, ".claude/.", true},
		anchorSpelling{"up and back", e.root, filepath.Join("..", ".claude"), true},
		anchorSpelling{"parent of a subdirectory", filepath.Join(e.root, "skills"), "..", true},
	)
}

// TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget: every spelling of a config root takes
// the root layout, with the inventory the absolute spelling produces.
func TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget(t *testing.T) {
	e := anchorInstallShape(t)
	want, err := CollectTarget(e.root)
	if err != nil {
		t.Fatal(err)
	}
	// The reverse assertion: the absolute target takes the root layout, before the fix and after.
	if want.Root != e.root {
		t.Fatalf("absolute target: Root %q, want %q (routed as a single target?)", want.Root, e.root)
	}
	for _, a := range want.Artifacts {
		if a.Kind == model.KindDirectory {
			t.Fatalf("absolute target: a %s artifact %q, want the root layout", a.Kind, a.Name)
		}
	}
	for _, sp := range targetSpellings(e) {
		t.Run(sp.name, func(t *testing.T) {
			anchorChdir(t, sp.dir)
			got, err := CollectTarget(sp.root)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root == "" {
				t.Fatalf("target %q was routed as a single target: %v", sp.root, anchorInventory(got, true))
			}
			if sp.sameFrame && got.Root != want.Root {
				t.Errorf("target %q: Root %q, want %q", sp.root, got.Root, want.Root)
			}
			if g, w := anchorInventory(got, sp.sameFrame), anchorInventory(want, sp.sameFrame); !reflect.DeepEqual(g, w) {
				t.Errorf("target %q: inventory\n got %v\nwant %v", sp.root, g, w)
			}
			if g, w := anchorNotes(got, sp.sameFrame), anchorNotes(want, sp.sameFrame); !reflect.DeepEqual(g, w) {
				t.Errorf("target %q: notes\n got %v\nwant %v", sp.root, g, w)
			}
			if got.Env != want.Env {
				t.Errorf("target %q: env %+v, want %+v", sp.root, got.Env, want.Env)
			}
		})
	}
}

// TestCollectTarget_SingleTargetsKeepTheirSpelling is the other side. A single skill is still one
// artifact under the path as typed, and a directory that is not named .claude is still read whole
// whichever way it is typed — anchoring the router is not loosening it (.claude/rules/pipeline.md).
func TestCollectTarget_SingleTargetsKeepTheirSpelling(t *testing.T) {
	e := anchorInstallShape(t)
	for _, target := range []string{filepath.Join("skills", "plain"), "./" + filepath.Join("skills", "plain")} {
		t.Run(target, func(t *testing.T) {
			anchorChdir(t, e.root)
			got, err := CollectTarget(target)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != "" || len(got.Artifacts) != 1 {
				t.Fatalf("target %q: Root %q, %d artifact(s); want one single-target artifact", target, got.Root, len(got.Artifacts))
			}
			if a := got.Artifacts[0]; a.Kind != model.KindSkill || a.Name != "plain" || a.Path != target {
				t.Errorf("target %q: got %s %q at %q, want skill %q at the path as typed", target, a.Kind, a.Name, a.Path, "plain")
			}
		})
	}

	other := filepath.Join(e.base, "notaroot")
	anchorWrite(t, filepath.Join(other, "skills", "x", "SKILL.md"), "---\nname: x\n---\nHello.\n")
	anchorWrite(t, filepath.Join(other, "settings.json"), `{"permissions":{"allow":[]}}`)
	for _, sp := range []anchorSpelling{
		{"absolute", "", other, true},
		{"absolute, trailing dot", "", other + "/.", true},
		{"dot", other, ".", true},
		{"dot slash", other, "./", true},
		{"parent of a subdirectory", filepath.Join(other, "skills"), "..", true},
	} {
		t.Run("not a root, "+sp.name, func(t *testing.T) {
			anchorChdir(t, sp.dir)
			got, err := CollectTarget(sp.root)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != "" || len(got.Artifacts) != 1 || got.Artifacts[0].Kind != model.KindDirectory {
				t.Errorf("target %q: Root %q, inventory %v; want one directory artifact", sp.root, got.Root, anchorInventory(got, true))
			}
		})
	}
}
