// SPDX-License-Identifier: MIT
package collect

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// A plugin's skills, commands and agents, collected as artifacts of their own (P-044, spec §4 B4).
//
// The plugin tree stays ONE artifact for the static rules, with the same name, path and tree hash as
// before. Its loadable contents are added NEXT to it so the judge's skill, command and subagent passes
// read them — before this, `scan --llm` and `check <plugin> --llm` never put a sentence of a plugin's
// SKILL.md to the model, because the judge has no pass for the plugin kind (issues/023). Each child
// carries Plugin = the plugin artifact's Name; score.Families links them through it, never through the
// " (plugin …)" name suffix.
//
// WHAT counts is Claude Code's plugin loader, read from the 2.1.107 build (P-044's table), and nothing
// else — collecting a copy that never loads repeats issues/017, and missing one that loads leaves it
// judged nowhere, so TestPluginContents_FollowClaudeCodesLoader pins one fixture row per case:
//
//   - skills:   <dir>/SKILL.md (the directory itself is one skill), otherwise every <dir>/<x>/SKILL.md
//     one level deep, <x> a directory OR a symlink;
//   - commands: every .md under commands/, recursively; a directory holding a SKILL.md (any case) is one
//     skill-style command and is not descended; symlinked entries are skipped (directory-entry types);
//   - agents:   every .md under agents/, recursively; symlinked entries skipped;
//   - manifest: `skills` / `commands` / `agents` in .claude-plugin/plugin.json — a path or a list of
//     paths relative to the plugin root, in ADDITION to the default directories; `commands` may also be
//     an object whose entries carry a `source` path (an inline `content` entry has no file and stays
//     plugin.json text). A path that leaves the plugin root lexically is refused, as Claude Code does.
//
// Containment is checked again per entry after resolving (invariant #2): a symlinked skill directory
// pointing outside the plugin loads in Claude Code and is NOT collected here, so it is disclosed in one
// COV-000. An entry that exists and cannot be resolved joins unresolvedNote; a dangling one is silent
// (hidesContent). A directory the walk cannot list is not noted here: it is inside the plugin tree,
// whose read already discloses it (detect.unreadableNote).

// pluginContentsNoteTitle names the children refused for resolving outside their plugin.
const pluginContentsNoteTitle = "Plugin skills resolve outside the plugin, not collected"

// pluginManifestComponents is the part of plugin.json that adds component paths.
type pluginManifestComponents struct {
	Skills   json.RawMessage `json:"skills"`
	Commands json.RawMessage `json:"commands"`
	Agents   json.RawMessage `json:"agents"`
}

// manifestPaths reads a manifest component member: a string, a list of strings, or (commands only) an
// object whose entries name a `source`. Anything else adds no path, as in Claude Code.
func manifestPaths(raw json.RawMessage, objects bool) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []json.RawMessage
	if json.Unmarshal(raw, &many) == nil {
		var out []string
		for _, m := range many {
			if json.Unmarshal(m, &one) == nil {
				out = append(out, one)
			}
		}
		return out
	}
	if !objects {
		return nil
	}
	var obj map[string]struct {
		Source *string `json:"source"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys) // map order is random; the artifact list must be reproducible
	var out []string
	for _, k := range keys {
		if s := obj[k].Source; s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// pluginChildren accumulates one plugin's children.
type pluginChildren struct {
	root, real     string // the plugin directory as collected, and resolved
	parent, bundle string
	suffix         string
	seen           map[string]bool
	out            []model.ArtifactReport
	unresolved     []string
	escaped        int
	skills, cmds   int
	agents         int
}

// inPlugin joins a manifest path to the plugin root and reports whether it stays inside lexically.
func (c *pluginChildren) inPlugin(rel string) (string, bool) {
	p := filepath.Join(c.root, filepath.FromSlash(rel))
	r, err := filepath.Rel(c.root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// add records one child at p unless it was already collected, cannot be resolved, or resolves outside
// the plugin.
func (c *pluginChildren) add(kind model.ArtifactKind, leaf, p string) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if hidesContent(err) {
			c.unresolved = append(c.unresolved, leaf)
		}
		return
	}
	if !withinDir(c.real, real) {
		c.escaped++
		return
	}
	if c.seen[real] {
		return // the same real path loads once per plugin
	}
	c.seen[real] = true
	hash := FileHash(p)
	switch kind {
	case model.KindSkill:
		hash = TreeHash(p, p)
		c.skills++
	case model.KindCommand:
		c.cmds++
	case model.KindSubagent:
		c.agents++
	}
	a := artifact(kind, c.bundle+":"+leaf+c.suffix, p, hash)
	a.Plugin = c.parent
	c.out = append(c.out, a)
}

// skillsAt is the skill loader: dir itself when it holds SKILL.md, otherwise each entry one level down
// (a directory or a symlink) that holds one.
func (c *pluginChildren) skillsAt(dir string) {
	if regularFile(filepath.Join(dir, "SKILL.md")) {
		c.add(model.KindSkill, filepath.Base(dir), dir)
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() && e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if regularFile(filepath.Join(p, "SKILL.md")) {
			c.add(model.KindSkill, e.Name(), p)
		}
	}
}

// markdownTree is the command and agent loader's walk over base: directory-entry types (a symlink is
// neither a directory nor a file), .md only. With stopAtSkill, a directory holding a SKILL.md is one
// skill-style command and is not descended.
func (c *pluginChildren) markdownTree(base string, kind model.ArtifactKind, stopAtSkill bool) {
	leaf := func(p string) string {
		rel, err := filepath.Rel(base, p)
		if err != nil {
			rel = filepath.Base(p)
		}
		if rel == "." {
			rel = filepath.Base(p)
		}
		rel = filepath.ToSlash(rel)
		if strings.EqualFold(filepath.Ext(rel), ".md") {
			rel = rel[:len(rel)-len(".md")]
		}
		return strings.ReplaceAll(rel, "/", ":")
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxTreeDepth {
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		if stopAtSkill {
			for _, e := range ents {
				if e.Type().IsRegular() && strings.EqualFold(e.Name(), "SKILL.md") {
					c.add(model.KindSkill, leaf(dir), dir)
					return
				}
			}
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			switch {
			case e.IsDir():
				walk(p, depth+1)
			case e.Type().IsRegular() && strings.EqualFold(filepath.Ext(e.Name()), ".md"):
				c.add(kind, leaf(p), p)
			}
		}
	}
	walk(base, 0)
}

// component collects one manifest-declared path: a directory is walked like the default one, a file
// is one entry.
func (c *pluginChildren) component(p string, kind model.ArtifactKind) {
	fi, err := os.Stat(p)
	if err != nil {
		if hidesContent(err) {
			c.unresolved = append(c.unresolved, filepath.Base(p))
		}
		return
	}
	if fi.IsDir() {
		c.markdownTree(p, kind, kind == model.KindCommand)
		return
	}
	if fi.Mode().IsRegular() && strings.EqualFold(filepath.Ext(p), ".md") {
		c.add(kind, strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), p)
	}
}

// pluginContents returns the skills, commands and agents the plugin at root loads, as artifacts named
// "<bundle>:<leaf><suffix>" with Plugin = parent, and counts them in env's Bundled* counters.
func pluginContents(root, parent, bundle, suffix string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil // the caller resolved it a moment ago; nothing loads from a plugin that vanished
	}
	c := &pluginChildren{root: root, real: real, parent: parent, bundle: bundle, suffix: suffix, seen: map[string]bool{}}
	var man pluginManifestComponents
	if b, rerr := safeio.ReadFile(filepath.Join(root, pluginManifest), safeio.MaxConfigBytes); rerr == nil {
		_ = json.Unmarshal(b, &man) // an unreadable manifest adds no path; the defaults still load
	}

	c.skillsAt(filepath.Join(root, "skills"))
	for _, rel := range manifestPaths(man.Skills, false) {
		if p, ok := c.inPlugin(rel); ok {
			c.skillsAt(p)
		}
	}
	c.markdownTree(filepath.Join(root, "commands"), model.KindCommand, true)
	for _, rel := range manifestPaths(man.Commands, true) {
		if p, ok := c.inPlugin(rel); ok {
			c.component(p, model.KindCommand)
		}
	}
	c.markdownTree(filepath.Join(root, "agents"), model.KindSubagent, false)
	for _, rel := range manifestPaths(man.Agents, false) {
		if p, ok := c.inPlugin(rel); ok {
			c.component(p, model.KindSubagent)
		}
	}

	env.BundledSkills += c.skills
	env.BundledCommands += c.cmds
	env.BundledAgents += c.agents
	var notes []model.Finding
	if len(c.unresolved) > 0 {
		notes = append(notes, unresolvedNote(root, c.unresolved))
	}
	if c.escaped > 0 {
		notes = append(notes, model.Finding{
			RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
			Title: pluginContentsNoteTitle,
			Why: "A plugin links skills, commands or agents to a location outside its own directory. Claude Code may " +
				"load them; they were NOT collected as artifacts of their own, so neither the rules nor the judge read them here.",
			Evidence: []model.Evidence{{File: root, Line: 0, Snippet: itoa(c.escaped) + " entr(ies) outside the plugin"}},
		})
	}
	return c.out, notes
}

// regularFile reports whether p resolves to a regular file.
func regularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
