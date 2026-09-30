// SPDX-License-Identifier: MIT

// Package aguard is the adapter that measures this project's own scanner against
// agent-artifact-corpus.
//
// The placement half below was MOVED here from hack/corpus-runner, not rewritten. That
// matters: the reverse assertion is that the 3,412 samples aguard already placed keep
// byte-identical verdicts while the remaining 127 gain rows, and a rewrite would have made that
// assertion meaningless. hack/corpus-runner now calls these functions instead of holding its own
// copy, so there is one implementation of "where does a sample have to sit for aguard to load
// it" rather than two that agree until they do not.
//
// What IS new here is what happens when placement finds nothing. The old runner returned before
// the binary was ever executed, so 127 samples were never handed to the scanner at all and the
// reason recorded was the runner's opinion rather than the tool's behaviour. So
// every test point gets invoked. See Adapter.Scan.
package aguard

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// StagedRoot is a sample laid out where aguard looks: Root is the config root to scan and Home its
// parent, which scopes the project MCP config and the desktop session cache.
type StagedRoot struct{ Home, Root string }

// NotPlaceable says a sample tree has no shape aguard loads. It is a distinct type so the caller
// can tell "we chose not to scan this" from an I/O failure — the two go into different tallies.
type NotPlaceable struct{ Reason string }

func (e NotPlaceable) Error() string { return e.Reason }

// desktopSessionsDir mirrors collect.desktopCodeSessionsDir (unexported there). The connector
// collector globs <home>/<this>/*/*/local_*.json and decodes remoteMcpServersConfig.
const desktopSessionsDir = "Library/Application Support/Claude/claude-code-sessions"

// stage copies the sample tree at src into a fresh fake home under work and returns the root to
// scan. Placement is by content, see the package comment. Top-level names that were placed are
// not copied a second time into a skill tree.
func Stage(src, id, work string) (StagedRoot, error) {
	home := filepath.Join(work, "home")
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return StagedRoot{}, err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return StagedRoot{}, err
	}
	if len(entries) == 0 {
		return StagedRoot{}, NotPlaceable{"nothing in the sample tree"}
	}
	placed := map[string]bool{}
	has := func(name string, dir bool) bool {
		for _, e := range entries {
			if e.Name() == name && e.IsDir() == dir {
				return true
			}
		}
		return false
	}

	if has(".claude", true) {
		if err := copyTree(filepath.Join(src, ".claude"), root); err != nil {
			return StagedRoot{}, err
		}
		placed[".claude"] = true
	}
	if has(".mcp.json", false) {
		if err := copyFile(filepath.Join(src, ".mcp.json"), filepath.Join(home, ".mcp.json")); err != nil {
			return StagedRoot{}, err
		}
		placed[".mcp.json"] = true
	}
	if has("tools.json", false) {
		if err := stageToolCatalogue(filepath.Join(src, "tools.json"), id, home); err != nil {
			return StagedRoot{}, err
		}
		placed["tools.json"] = true
	}
	if has("SKILL.md", false) {
		dst := filepath.Join(root, "skills", filepath.Base(id))
		if err := copyTreeExcept(src, dst, placed); err != nil {
			return StagedRoot{}, err
		}
		placed["SKILL.md"] = true
	}
	if len(placed) > 0 {
		return StagedRoot{Home: home, Root: root}, nil
	}

	// Nothing on a load path yet. One loose instruction file is what the corpus's instruction
	// surface ships for its fuzz-derived samples; aguard reads it as CLAUDE.md.
	// Several would have no single load path, and guessing one would measure the guess.
	var loose []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			loose = append(loose, e.Name())
		}
	}
	switch {
	case len(loose) == 1:
		if err := copyFile(filepath.Join(src, loose[0]), filepath.Join(root, "CLAUDE.md")); err != nil {
			return StagedRoot{}, err
		}
		return StagedRoot{Home: home, Root: root}, nil
	case len(loose) > 1:
		return StagedRoot{}, NotPlaceable{"several loose instruction files; no single load path"}
	}
	if hasExt(src, ".py") {
		return StagedRoot{}, NotPlaceable{"server source (.py) has no load path aguard reads"}
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return StagedRoot{}, NotPlaceable{"no file on a load path aguard reads (top level: " + strings.Join(names, ", ") + ")"}
}

// stageToolCatalogue writes a tool catalogue as the one shape the connector collector decodes:
// a desktop session file whose remoteMcpServersConfig carries the tools verbatim. Only the
// `tools` array is lifted; the catalogue's own top-level fields (package name, counts) are not
// something Claude Desktop would ever hand the model.
func stageToolCatalogue(path, id, home string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cat struct {
		Package string          `json:"package"`
		Tools   json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(b, &cat); err != nil {
		return NotPlaceable{"tools.json is not JSON: " + err.Error()}
	}
	if len(cat.Tools) == 0 || string(cat.Tools) == "null" {
		return NotPlaceable{"tools.json without a tools array"}
	}
	name := cat.Package
	if name == "" {
		name = id
	}
	doc := map[string]any{
		"lastActivityAt": 1,
		"createdAt":      1,
		"remoteMcpServersConfig": []map[string]any{{
			"name":  name,
			"uuid":  id,
			"tools": cat.Tools,
		}},
	}
	dir := filepath.Join(home, desktopSessionsDir, "corpus", "sample")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "local_0.json"), out, 0o644)
}

// hasExt reports whether any regular file under dir has the extension.
func hasExt(dir, ext string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ext) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// copyTree copies src into dst, preserving symlinks AS symlinks: the corpus will ship fixtures
// whose whole point is a link, and the symlink boundary (invariant #2) is aguard's to enforce,
// not the runner's to launder away. Modes are kept so an unreadable-directory fixture survives.
func copyTree(src, dst string) error { return copyTreeExcept(src, dst, nil) }

func copyTreeExcept(src, dst string, skipTop map[string]bool) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if skipTop[rel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			return copyFile(p, target)
		}
		return nil // FIFOs and devices are fixture material built on demand, never copied
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
