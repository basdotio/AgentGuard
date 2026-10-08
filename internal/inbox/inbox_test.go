// SPDX-License-Identifier: MIT
package inbox

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// zipWith builds a zip at path from name → content; a trailing "/" makes a directory entry.
func zipWith(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, path, buf.String())
}

func kinds(d Discovery) map[string]string {
	out := map[string]string{}
	for _, c := range d.Trim() {
		out[c.Name] = c.Kind
	}
	return out
}

// TestDiscover_FindsAgentShapesAndNothingElse: a skill folder, a plugin, a loose CLAUDE.md, a zip
// holding a skill and a skill nested inside an unpacked repo are candidates; a PDF, a photo, a
// hidden file, a zip of holiday photos and a tarball are counted and never read.
func TestDiscover_FindsAgentShapesAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cool-skill", "SKILL.md"), "---\nname: cool\n---\n")
	write(t, filepath.Join(dir, "cool-skill", "run.sh"), "curl x | sh\n")
	write(t, filepath.Join(dir, "some-plugin", ".claude-plugin", "plugin.json"), `{"name":"p"}`)
	write(t, filepath.Join(dir, "CLAUDE.md"), "# rules\n")
	write(t, filepath.Join(dir, "repo-main", "README.md"), "hi\n")
	write(t, filepath.Join(dir, "repo-main", "skills", "nested", "SKILL.md"), "---\nname: nested\n---\n")
	write(t, filepath.Join(dir, "invoice.pdf"), "%PDF")
	write(t, filepath.Join(dir, "photo.jpg"), "\xff\xd8")
	write(t, filepath.Join(dir, ".DS_Store"), "x")
	write(t, filepath.Join(dir, "backup.tar.gz"), "x")
	zipWith(t, filepath.Join(dir, "skill.zip"), map[string]string{"skill/SKILL.md": "---\nname: z\n---\n", "skill/a.sh": "echo\n"})
	zipWith(t, filepath.Join(dir, "photos.zip"), map[string]string{"a.jpg": "x", "b.jpg": "y"})

	d := Discover(dir)
	got := kinds(d)
	want := map[string]string{"cool-skill": "skill", "some-plugin": "plugin", "CLAUDE.md": "instructions", "nested": "skill", "skill.zip": "archive"}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("%s: kind %q, want %q (all: %v)", n, got[n], k, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("candidates = %v, want exactly %v", got, want)
	}
	// invoice.pdf, photo.jpg, .DS_Store, backup.tar.gz, photos.zip, repo-main/README.md
	if d.Skipped != 6 {
		t.Errorf("skipped = %d, want 6", d.Skipped)
	}
	var tarNote bool
	for _, n := range d.Notes {
		if strings.Contains(n.Title, "other types") && strings.Contains(n.Why, ".tar.gz") {
			tarNote = true
		}
	}
	if !tarNote {
		t.Errorf("tarball must be disclosed as not read: %+v", d.Notes)
	}
}

// TestDiscover_MissingDirIsANote: the caller decides whether a missing inbox matters; here it is
// disclosed and nothing else happens.
func TestDiscover_MissingDirIsANote(t *testing.T) {
	d := Discover(filepath.Join(t.TempDir(), "nope"))
	if len(d.Trim()) != 0 || len(d.Notes) != 1 {
		t.Errorf("missing dir: %+v", d)
	}
}

// TestExtractZip_RefusesWhatWouldEscapeOrBloat: a traversal name, an absolute name and an
// over-cap entry are refused and counted; the safe entries land under the private directory
// with owner-only modes; cleanup removes everything.
func TestExtractZip_RefusesWhatWouldEscapeOrBloat(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("A", MaxArchiveFileBytes+10)
	zipWith(t, filepath.Join(dir, "evil.zip"), map[string]string{
		"skill/SKILL.md":        "---\nname: e\n---\n",
		"../escape.sh":          "rm -rf /\n",
		"/abs/path.sh":          "x\n",
		"skill/huge.bin":        big,
		"skill/nested/../ok.md": "fine\n", // would clean to skill/ok.md, but any ".." segment is refused
	})
	out, notes, cleanup, err := ExtractZip(filepath.Join(dir, "evil.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "skill", "SKILL.md")); err != nil {
		t.Errorf("safe entry missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "skill", "ok.md")); err == nil {
		t.Error("an entry with a .. segment was extracted; the policy is to refuse them all, not to clean them")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "escape.sh")); err == nil {
		t.Error("traversal entry escaped the destination")
	}
	if _, err := os.Stat(filepath.Join(out, "skill", "huge.bin")); err == nil {
		t.Error("over-cap entry was extracted")
	}
	fi, _ := os.Stat(out)
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("destination mode = %v, want 0700", fi.Mode().Perm())
	}
	fi, _ = os.Stat(filepath.Join(out, "skill", "SKILL.md"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	var refused bool
	for _, n := range notes {
		if strings.Contains(n.Why, "3 with paths that escape") && strings.Contains(n.Why, "1 over 1 MiB") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("refusals not disclosed: %+v", notes)
	}
	cleanup()
	if _, err := os.Stat(out); err == nil {
		t.Error("cleanup left the extraction directory behind")
	}
}

// TestPeekZip_ReadsOnlyTheIndex: candidacy is decided from entry names; a zip of photos is not
// a candidate, a zip with a plugin manifest anywhere inside is.
func TestPeekZip_ReadsOnlyTheIndex(t *testing.T) {
	dir := t.TempDir()
	zipWith(t, filepath.Join(dir, "p.zip"), map[string]string{"x/y/.claude-plugin/plugin.json": "{}"})
	zipWith(t, filepath.Join(dir, "n.zip"), map[string]string{"a.jpg": "x"})
	if ok, err := PeekZip(filepath.Join(dir, "p.zip")); err != nil || !ok {
		t.Errorf("plugin zip: ok=%v err=%v", ok, err)
	}
	if ok, err := PeekZip(filepath.Join(dir, "n.zip")); err != nil || ok {
		t.Errorf("photo zip: ok=%v err=%v", ok, err)
	}
}

// TestExtractZip_FolderNamedAfterTheArchive: entries land in a folder named after the archive,
// inside a private temporary directory that holds nothing else, on a symlink-resolved path. The
// folder's name becomes the artifact's name and its path the base every evidence path is taken
// relative to, so neither may be random; the private parent is the "home" a root-shaped archive
// is collected under, so it must not be the machine's shared temp dir. Cleanup removes the parent.
func TestExtractZip_FolderNamedAfterTheArchive(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "My Skill.zip")
	zipWith(t, zp, map[string]string{"SKILL.md": "---\nname: m\n---\n", "run.sh": "echo hi\n"})
	out, _, cleanup, err := ExtractZip(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(out) != "My Skill.zip" {
		t.Errorf("extraction folder = %q, want it named after the archive", filepath.Base(out))
	}
	if real, err := filepath.EvalSymlinks(out); err != nil || real != out {
		t.Errorf("extraction folder %q is not symlink-resolved (resolves to %q, err %v)", out, real, err)
	}
	if _, err := os.Stat(filepath.Join(out, "SKILL.md")); err != nil {
		t.Errorf("entries are not at the folder's top: %v", err)
	}
	parent := filepath.Dir(out)
	if filepath.Clean(parent) == filepath.Clean(os.TempDir()) {
		t.Fatalf("extraction folder sits directly in the shared temp dir %s", parent)
	}
	ents, err := os.ReadDir(parent)
	if err != nil || len(ents) != 1 || ents[0].Name() != "My Skill.zip" {
		t.Errorf("private parent holds %v (err %v), want only the extraction folder", ents, err)
	}
	if fi, err := os.Stat(parent); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("private parent mode: %v, %v — want 0700", fi, err)
	}
	cleanup()
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Errorf("cleanup left the private parent behind: %v", err)
	}
}
