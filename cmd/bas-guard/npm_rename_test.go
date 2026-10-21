// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNpmPlatformPackageKeepsTheOldBinaryPath: `hook install` registers the absolute path of the
// binary it runs as, symlinks resolved (resolvedExe), and under npm that is the file inside the
// platform package, not the launcher. Through v0.29.0 that file was called `aguard`, so every gate
// an npm user installed reads `…/@bas.io/guard-<os>-<cpu>/aguard hook`. A platform package without
// that file turns each of those gates, on the next `npm update`, into a registration that runs
// nothing: every skill loads unaudited and the silence looks like a clean result. For the
// transition (P-082 Q3) each platform package carries `aguard` as a byte-identical copy of
// `bas-guard`, as the release assets do.
//
// It runs the real `npm-dist` recipe over stand-in binaries, because the template's `files` list
// and the Makefile's copy step have to agree, and each alone looks well-formed.
func TestNpmPlatformPackageKeepsTheOldBinaryPath(t *testing.T) {
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make not installed; the layout is the Makefile's npm-dist recipe")
	}
	root, err := filepath.Abs(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "npm"), filepath.Join(work, "npm")); err != nil {
		t.Fatal(err)
	}
	targets := strings.Fields(makeVar(t, "DIST_TARGETS"))
	for _, target := range targets {
		os_, arch, _ := strings.Cut(target, "/")
		// Distinct bytes per target, so a copy from the wrong platform cannot pass as identical.
		mustWriteFile(t, filepath.Join(work, "dist", "bas-guard-"+os_+"-"+arch), "stand-in binary "+target+"\n")
	}
	cmd := exec.Command(makeBin, "-s", "-f", filepath.Join(root, "Makefile"), "npm-dist", "NPM_VERSION=0.0.0-test")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make npm-dist: %v\n%s", err, out)
	}

	for i, suffix := range npmPlatformSuffixes(t) {
		os_, arch, _ := strings.Cut(targets[i], "/")
		want, err := os.ReadFile(filepath.Join(work, "dist", "bas-guard-"+os_+"-"+arch))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(work, "dist", "npm", suffix)
		for _, name := range []string{"bas-guard", "aguard"} {
			p := filepath.Join(dir, name)
			got, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("%s: %v — a gate registered at this path stops running on upgrade", suffix, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s/%s is not a copy of dist/bas-guard-%s-%s", suffix, name, os_, arch)
			}
			if fi, err := os.Stat(p); err == nil && fi.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s/%s is not executable (%v)", suffix, name, fi.Mode())
			}
		}
		var pkg struct {
			Files []string `json:"files"`
		}
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &pkg); err != nil {
			t.Fatal(err)
		}
		shipped := map[string]bool{}
		for _, f := range pkg.Files {
			shipped[f] = true
		}
		// `files` decides what reaches the tarball: a copy on disk that it does not list is not
		// published.
		if len(pkg.Files) != 2 || !shipped["bas-guard"] || !shipped["aguard"] {
			t.Errorf("%s package.json files = %v, want exactly bas-guard and aguard", suffix, pkg.Files)
		}
	}
}
