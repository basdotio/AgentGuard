// SPDX-License-Identifier: MIT
// Package inbox finds agent-shaped things where downloads land — a skill folder, a plugin, an
// MCP config, an instructions file, or a zip holding any of those — so each can be checked
// BEFORE it is installed. It answers "is this safe to install", one item at a time; it says
// nothing about the environment, and nothing it finds enters the environment score.
//
// The directory it walks is the user's own. That sets the boundaries:
//
//   - Only agent-shaped candidates are read. Everything else is counted, never opened and
//     never named in a report: a Downloads folder is the most personal directory on a machine,
//     and listing its contents would trade a blind spot for a leak (the same reasoning that
//     keeps sessions/ and history.jsonl out of the root scan).
//   - Discovery is bounded: MaxDepth levels, MaxEntriesWalked entries, MaxCandidates items.
//     Hitting a cap is a note, never a silent stop (invariant #5).
//   - Archives are inspected by their index first; only a zip whose entry names carry a marker
//     is extracted, into a private temporary directory, under per-file and total byte caps,
//     with unsafe paths and non-regular entries refused. Nothing extracted is ever executed,
//     and the directory is removed when the check ends.
package inbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/model"
)

// Discovery limits. Generous for a Downloads folder, tight enough that a pathological one
// (tens of thousands of files, deeply nested extractions) cannot turn a scan into a crawl.
const (
	MaxDepth         = 3    // Downloads/<repo>/skills/<name> is depth 3
	MaxEntriesWalked = 5000 // directory entries looked at, across all levels
	MaxCandidates    = 50   // items handed on for a check
)

// Candidate is one agent-shaped thing found in the inbox directory.
type Candidate struct {
	Path    string
	Name    string // base name, what a person sees in the folder
	Kind    string // skill | plugin | marketplace | agent-config | instructions | archive
	Archive bool
}

// Discovery is what a walk found, and what it deliberately did not read.
type Discovery struct {
	Candidates []Candidate
	Skipped    int             // entries that were not agent-shaped and were not read
	Notes      []model.Finding // caps hit, unreadable directories, archive types not read
}

// dirMarkers name the file that makes a directory a candidate, most specific first.
var dirMarkers = []struct{ rel, kind string }{
	{"SKILL.md", "skill"},
	{filepath.Join(".claude-plugin", "plugin.json"), "plugin"},
	{filepath.Join(".claude-plugin", "marketplace.json"), "marketplace"},
	{".mcp.json", "agent-config"},
	{"CLAUDE.md", "agent-config"},
	{"CLAUDE.local.md", "agent-config"},
	{"AGENTS.md", "agent-config"},
	{".cursorrules", "agent-config"},
}

// fileMarkers are loose files that are themselves an agent artifact.
var fileMarkers = map[string]bool{
	"SKILL.md": true, "CLAUDE.md": true, "CLAUDE.local.md": true, "AGENTS.md": true,
	".cursorrules": true, ".mcp.json": true,
}

// MarkerName reports whether a file name (any path) is one of the markers — used by the archive
// index check too, so a zip is a candidate for exactly the reasons a directory is.
func MarkerName(name string) bool {
	base := filepath.Base(filepath.ToSlash(name))
	if fileMarkers[base] {
		return true
	}
	slash := filepath.ToSlash(name)
	return strings.HasSuffix(slash, ".claude-plugin/plugin.json") || strings.HasSuffix(slash, ".claude-plugin/marketplace.json")
}

// Discover walks dir and returns the agent-shaped candidates in it. A missing or unreadable
// directory is a note, not an error: the caller decides whether that matters (an explicit path
// that does not exist should fail loudly; a default ~/Downloads that does not exist is nothing).
func Discover(dir string) Discovery {
	var d Discovery
	walked := 0
	otherArchives := map[string]int{}
	var walk func(cur string, depth int)
	walk = func(cur string, depth int) {
		ents, err := os.ReadDir(cur)
		if err != nil {
			d.Notes = append(d.Notes, note("Inbox directory not read", fmt.Sprintf("%s: %v", cur, err), cur))
			return
		}
		// Directory-level markers: the folder itself is the artifact; do not descend into it.
		if depth > 0 {
			if kind := dirKind(cur); kind != "" {
				d.add(Candidate{Path: cur, Name: filepath.Base(cur), Kind: kind})
				return
			}
		}
		for _, e := range ents {
			walked++
			if walked > MaxEntriesWalked {
				d.Notes = append(d.Notes, note("Inbox walk stopped at its cap",
					fmt.Sprintf("More than %d entries under %s; the rest were not looked at. Point --inbox at a smaller folder to check them.", MaxEntriesWalked, dir), dir))
				return
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				d.Skipped++
				continue
			}
			p := filepath.Join(cur, name)
			switch {
			case e.IsDir():
				if collect.ExcludedFromHash(name) || depth+1 > MaxDepth {
					d.Skipped++
					continue
				}
				walk(p, depth+1)
			case e.Type().IsRegular():
				switch {
				case fileMarkers[name]:
					d.add(Candidate{Path: p, Name: name, Kind: "instructions"})
				case IsZip(p):
					ok, err := PeekZip(p)
					if err != nil {
						d.Notes = append(d.Notes, note("Archive index not readable", fmt.Sprintf("%s: %v", name, err), p))
						d.Skipped++
					} else if ok {
						d.add(Candidate{Path: p, Name: name, Kind: "archive", Archive: true})
					} else {
						d.Skipped++
					}
				case otherArchiveExt(name) != "":
					otherArchives[otherArchiveExt(name)]++
					d.Skipped++
				default:
					d.Skipped++
				}
			default:
				d.Skipped++ // symlinks, sockets, devices: never followed, never opened
			}
		}
	}
	walk(dir, 0)
	if len(otherArchives) > 0 {
		exts := make([]string, 0, len(otherArchives))
		n := 0
		for e, c := range otherArchives {
			exts = append(exts, e)
			n += c
		}
		sort.Strings(exts)
		d.Notes = append(d.Notes, note("Archives of other types not read",
			fmt.Sprintf("%d archive(s) with extension %s were not opened: only .zip is inspected. Extract one yourself and point aguard check at the folder.", n, strings.Join(exts, ", ")), dir))
	}
	sort.Slice(d.Candidates, func(i, j int) bool { return d.Candidates[i].Path < d.Candidates[j].Path })
	return d
}

func (d *Discovery) add(c Candidate) {
	if len(d.Candidates) >= MaxCandidates {
		if len(d.Candidates) == MaxCandidates {
			d.Notes = append(d.Notes, note("Too many agent-shaped items",
				fmt.Sprintf("More than %d candidates; the rest were not checked. Clear out old downloads or point --inbox at a sub-folder.", MaxCandidates), c.Path))
			d.Candidates = append(d.Candidates, Candidate{}) // sentinel so the note is added once
		}
		return
	}
	d.Candidates = append(d.Candidates, c)
}

// dirKind returns the kind a directory's own markers make it, or "".
func dirKind(dir string) string {
	for _, m := range dirMarkers {
		if fi, err := os.Stat(filepath.Join(dir, m.rel)); err == nil && !fi.IsDir() {
			return m.kind
		}
	}
	return ""
}

func otherArchiveExt(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".tgz", ".tar", ".7z", ".rar", ".gz", ".bz2", ".xz"} {
		if strings.HasSuffix(lower, ext) {
			return ext
		}
	}
	return ""
}

func note(title, why, file string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: title, Why: why, Evidence: []model.Evidence{{File: file}},
	}
}

// Trim drops the sentinel add() leaves behind when the candidate cap is hit.
func (d Discovery) Trim() []Candidate {
	out := make([]Candidate, 0, len(d.Candidates))
	for _, c := range d.Candidates {
		if c.Path != "" {
			out = append(out, c)
		}
	}
	return out
}
