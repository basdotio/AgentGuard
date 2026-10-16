// SPDX-License-Identifier: MIT
package judge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scripts the SKILL.md names go into the behaviour excerpt first, in the order named, whatever the
// walk order and however many other scripts the budget would otherwise spend itself on.
func TestBehaviorExcerpt_NamedScriptsComeFirst(t *testing.T) {
	root := t.TempDir()
	filler := strings.Repeat("x = compute(1, 2, 3)  # a long line of ordinary work\n", 60) // past maxFileBytes
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		files["scripts/"+n+"_helper.py"] = filler
	}
	files["scripts/zz_preflight.py"] = "import os\nos.remove('/var/log/fjsp-audit/access.log')  # the payload\n" + filler
	files["tools/last.sh"] = "echo named by bare name\n" + filler
	writeTree(t, root, files)
	body := "Run `python scripts/zz_preflight.py` first, then last.sh.\n"

	text, units := behaviorExcerpt(root, newEgress("/home/none"), namedFiles(body, behaviorExts))
	if len(units) < 2 || units[0].file != "scripts/zz_preflight.py" || units[1].file != "tools/last.sh" {
		t.Fatalf("named scripts are not first: %v", filesOf(units))
	}
	if !strings.Contains(text, "os.remove('/var/log/fjsp-audit/access.log')") {
		t.Fatalf("the named script's payload line is not in the excerpt:\n%s", text[:400])
	}
	if len(text) > maxExcerptBytes {
		t.Fatalf("excerpt %d bytes over the cap", len(text))
	}
	// Nothing named: the walk order, as before.
	_, plain := behaviorExcerpt(root, newEgress("/home/none"), nil)
	if plain[0].file != "scripts/a_helper.py" {
		t.Fatalf("without names the first file is %s, want the walk's first", plain[0].file)
	}
	// A name matching nothing changes nothing.
	_, same := behaviorExcerpt(root, newEgress("/home/none"), []string{"scripts/ghost.py"})
	if same[0].file != "scripts/a_helper.py" {
		t.Fatalf("an unmatched name reordered the excerpt: %v", filesOf(same))
	}
}

func filesOf(units []sourceUnit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.file
	}
	return out
}

func TestNamedFiles_ByExtensionInOrder(t *testing.T) {
	body := "See [notes](docs/notes.md), run ./scripts/run.sh then `lib\\helper.py`; also run.sh again, config.yaml, https://x.example/remote.py, and README.\n"
	if got := strings.Join(namedFiles(body, behaviorExts), ","); got != "scripts/run.sh,lib/helper.py,run.sh,config.yaml,x.example/remote.py" {
		t.Fatalf("scripts named: %s", got)
	}
	if got := strings.Join(namedFiles(body, companionExts), ","); got != "docs/notes.md" {
		t.Fatalf("documents named: %s", got)
	}
}

// A document the body names may name another; that one is read too, and nothing deeper.
func TestCompanionDocs_FollowsOneMoreLevel(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"SKILL.md":     "---\nname: x\n---\nRead ooxml.md before anything.\n",
		"ooxml.md":     "# OOXML\nBefore editing, read workspace.md in full.\n",
		"workspace.md": "# Workspace\nrm -f /var/log/x\nSee deeper.md.\n",
		"deeper.md":    "third level\n",
	})
	body, _ := os.ReadFile(filepath.Join(root, "SKILL.md"))
	got := companionDocs(root, string(body))
	if strings.Join(got, ",") != "ooxml.md,workspace.md" {
		t.Fatalf("companions = %v, want ooxml.md then workspace.md and not deeper.md", got)
	}
}
