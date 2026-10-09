// SPDX-License-Identifier: MIT
package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// noteToken is an obviously fake GitHub token, the shape the redactor's known-prefix table removes.
const noteToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

// unreadableWhy is the note's explanation as it was written before this change, applied to a list —
// the formula the reverse assertion compares against.
func unreadableWhy(n int, list string) string {
	return fmt.Sprintf("%d entry/entries exist in this artifact but could not be listed or opened by this scan: %s. "+
		"A directory that is executable but not readable (mode 0111) is the sharpest case — an agent told to run "+
		"a script inside it can, this scanner cannot — and the mode bits are the author's choice. Whatever is "+
		"in there was NOT checked; the score above does not cover it. Inspect it by hand before trusting this artifact.",
		n, list)
}

// TestUnreadableNote_SecretInEntryNameIsRedacted: the note for entries the walk saw but could not read
// put their names into Why and the snippet as they were, while its two siblings in the same walk —
// non-regular files, skipped vendored trees — list the same kind of names through the redactor. The
// names are the artifact author's (an entry is unreadable because its author chose the mode bits), so
// a token in one reached `check --md`, the output written to be pasted into a pull request.
func TestUnreadableNote_SecretInEntryNameIsRedacted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads 0111 directories; the note never fires for it")
	}
	root, art := skillArtifact(t, map[string]string{
		"SKILL.md":                 "---\nname: helper\n---\nRun the bundled helper.\n",
		noteToken + "/inner.sh":    "#!/bin/sh\nexit 0\n",
		"scripts/a-plain-name.txt": "nothing to see\n",
	})
	sub := filepath.Join(art.Path, noteToken)
	if err := os.Chmod(sub, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	_, notes := New().Run(root, []model.ArtifactReport{art})
	var n *model.Finding
	for i := range notes {
		if notes[i].RuleID == "COV-000" && strings.Contains(notes[i].Title, "could not be read") {
			n = &notes[i]
		}
	}
	if n == nil {
		t.Fatalf("the unreadable directory was not disclosed at all: %+v", notes)
	}
	if got, want := n.Evidence[0].Snippet, "unreadable: <REDACTED>"; got != want {
		t.Errorf("snippet:\n  got  %q\n  want %q", got, want)
	}
	if got, want := n.Why, unreadableWhy(1, "<REDACTED>"); got != want {
		t.Errorf("Why must carry the same redacted list as the snippet:\n  got  %q\n  want %q", got, want)
	}
	for _, s := range []string{n.Title, n.Why, n.Evidence[0].File, n.Evidence[0].Snippet} {
		if strings.Contains(s, noteToken) {
			t.Errorf("the token reached the note in clear: %q", s)
		}
	}

	// The list is redacted as one string, the way nonRegularNote does it: whatever Redact makes of
	// the joined names is what both fields print, ordinary names beside the secret untouched.
	direct := unreadableNote(art.Path, []string{"sub", noteToken + "/x.sh"})
	if got, want := direct.Evidence[0].Snippet, "unreadable: <REDACTED>/x.sh, sub"; got != want {
		t.Errorf("direct call snippet:\n  got  %q\n  want %q", got, want)
	}
	if got, want := direct.Why, unreadableWhy(2, "<REDACTED>/x.sh, sub"); got != want {
		t.Errorf("direct call Why:\n  got  %q\n  want %q", got, want)
	}
}

// TestUnreadableNote_OrdinaryNamesUnchanged is the reverse assertion: names the redactor has nothing
// to say about render exactly as before — same Why, same "unreadable: " snippet, no truncation added
// (the note was never clipped, and adding a clip would shorten a long but ordinary list).
func TestUnreadableNote_OrdinaryNamesUnchanged(t *testing.T) {
	eleven := []string{"a.sh", "b.sh", "c.sh", "d.sh", "e.sh", "f.sh", "g.sh", "h.sh", "i.sh", "j.sh", "k.sh"}
	cases := []struct {
		name  string
		names []string
		list  string
	}{
		{"one directory", []string{"sub"}, "sub"},
		{"sorted, nested, with a space", []string{"sub", "lib/helper.sh", "my tools/run.sh"}, "lib/helper.sh, my tools/run.sh, sub"},
		{"more than ten", eleven, "a.sh, b.sh, c.sh, d.sh, e.sh, f.sh, g.sh, h.sh, i.sh, j.sh, … (1 more)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := unreadableNote("/tmp/skill", append([]string(nil), c.names...))
			if got, want := n.Evidence[0].Snippet, "unreadable: "+c.list; got != want {
				t.Errorf("snippet:\n  got  %q\n  want %q", got, want)
			}
			if got, want := n.Why, unreadableWhy(len(c.names), c.list); got != want {
				t.Errorf("Why:\n  got  %q\n  want %q", got, want)
			}
			if n.Severity != model.SevMedium || n.Dimension != 0 {
				t.Errorf("severity/dimension = %s/%d, want medium/0", n.Severity, n.Dimension)
			}
		})
	}
}
