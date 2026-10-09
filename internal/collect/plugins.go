// SPDX-License-Identifier: MIT
package collect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/redact"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// manifestSchema is the installed_plugins.json layout this build understands.
const manifestSchema = 2

// pluginManifestDoc is the shape of installed_plugins.json. It is a named type rather than
// an anonymous struct so that the load-time gate (internal/gate) resolves a `plugin:skill`
// name through THIS parser instead of writing a second reader of the same file — two
// readers of one attacker-adjacent config is how the scanner and the gate end up disagreeing
// about which directory a plugin actually lives in.
type pluginManifestDoc struct {
	Version int `json:"version"`
	Plugins map[string][]struct {
		InstallPath string `json:"installPath"`
		Version     string `json:"version"`
	} `json:"plugins"`
}

// PluginInstall is where an installed plugin bundle loads from, and which install it is.
type PluginInstall struct {
	Dir string
	// Marketplace is the marketplace name the installing channel recorded — the "@marketplace"
	// half of the installed_plugins.json key, or the desktop manifest's marketplaceName — as
	// written, unvalidated: it comes out of a config file, and a caller that prints it must
	// check it first. "" when the record does not say.
	Marketplace string
	// Desktop is true for a bundle from Claude Desktop's store, which updates through the
	// desktop app rather than `claude plugin`.
	Desktop bool
}

// PluginPaths maps an installed plugin's BUNDLE name (the part before "@marketplace") to the
// directory that actually gets loaded. It is the Dir projection of PluginInstalls.
func PluginPaths(root, home string) map[string]string {
	out := map[string]string{}
	for name, in := range PluginInstalls(root, home) {
		out[name] = in.Dir
	}
	return out
}

// PluginInstalls maps an installed plugin's BUNDLE name (the part before "@marketplace") to
// the directory that actually gets loaded and the install that put it there, applying the same
// containment collectPlugins applies: symlinks resolved, anything landing outside home dropped
// (§16.2, invariant #2).
//
// Callers get no notes back — this is a lookup, not a collection pass. A plugin it cannot
// resolve is simply absent from the map, and the caller decides what an absence means; for
// the gate that is a GATE-000 "loaded without an audit", never a silent pass.
func PluginInstalls(root, home string) map[string]PluginInstall {
	out := map[string]PluginInstall{}
	// Desktop-installed bundles first, so an entry from installed_plugins.json overwrites a
	// desktop one of the same bundle name: when both channels install the same plugin the CLI's
	// own loader owns the name, and the gate keeps resolving it exactly as it did before the
	// desktop store was known. A bundle only the desktop installed used to resolve to nothing
	// (GATE-000 on every load); now it resolves to what actually loads.
	for name, in := range desktopPluginInstalls(home) {
		out[name] = in
	}
	b, err := safeio.ReadFile(filepath.Join(root, "plugins", "installed_plugins.json"), safeio.MaxConfigBytes)
	if err != nil {
		return out
	}
	var doc pluginManifestDoc
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for name, insts := range doc.Plugins {
		bundle, marketplace := name, ""
		if i := strings.IndexByte(name, '@'); i > 0 {
			bundle, marketplace = name[:i], name[i+1:]
		}
		for _, inst := range insts {
			if inst.InstallPath == "" {
				continue
			}
			real, rerr := filepath.EvalSymlinks(inst.InstallPath)
			if rerr != nil {
				continue
			}
			if fi, serr := os.Stat(real); serr != nil || !fi.IsDir() {
				continue
			}
			if !withinDir(home, real) {
				continue
			}
			out[bundle] = PluginInstall{Dir: real, Marketplace: marketplace}
		}
	}
	return out
}

// collectPlugins enumerates INSTALLED plugins (spec §4). Claude Code records them in
// <root>/plugins/installed_plugins.json; each entry's installPath is the directory that
// actually gets auto-loaded — that directory, not the marketplace mirror sitting next to
// it, is what a scan must cover (a marketplace listing is not loaded into any session).
//
// installPath comes out of a config file, i.e. it is attacker-influenceable: it is resolved
// and required to stay under home — the same containment (and SCOPE-001 note) the
// install-symlink skill guard applies (§16.2). A stale entry whose directory is gone is
// skipped: there is nothing loaded and nothing to audit.
//
// Coverage boundary: the plugin tree is scanned as ONE artifact (its bundled skills, commands and
// MCP config are read as text by the detect engine). Spec §4 asks for those to be re-collected under
// their own kinds; that finer attribution is still not done for skills/commands/MCP.
//
// HOOKS ARE THE EXCEPTION, and now get the same per-(event, matcher, command) treatment settings.json
// hooks do — see collectPluginHooks.
func collectPlugins(root, home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	var out []model.ArtifactReport
	var notes []model.Finding
	// The sandbox (Cowork/Cloud) syncs plugins into plugins/synced/<uuid>/<plugin>/ with NO
	// installed_plugins.json entry — real, loaded plugins the manifest path never sees. Walk that
	// first so an absent or unfamiliar manifest does not hide them.
	syncOut, syncNotes := collectSyncedPlugins(root, home, env)
	out = append(out, syncOut...)
	notes = append(notes, syncNotes...)

	path := filepath.Join(root, "plugins", "installed_plugins.json")
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, notes
		}
		return out, append(notes, ioNote(path, err))
	}
	var doc pluginManifestDoc
	if json.Unmarshal(b, &doc) != nil {
		return append(out, withParseError(model.KindPlugin, "installed_plugins", path)), notes
	}
	// A schema we don't know parses into an empty map — which is indistinguishable from
	// "no plugins installed" unless we say so. Plugins were invisible to this scanner until
	// recently; a silent schema drift would quietly recreate that blind spot.
	if doc.Version != manifestSchema && len(doc.Plugins) == 0 {
		return out, append(notes, model.Finding{
			RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
			Title: "Plugin manifest schema not recognized, plugins not collected (partial)",
			Why: fmt.Sprintf("installed_plugins.json declares schema version %d (this build reads %d) and yielded no entries; "+
				"installed plugins may exist but were not scanned.", doc.Version, manifestSchema),
			Evidence: []model.Evidence{{File: path, Line: 0, Snippet: "unrecognized manifest schema"}},
		})
	}

	names := make([]string, 0, len(doc.Plugins))
	for name := range doc.Plugins {
		names = append(names, name)
	}
	sort.Strings(names) // map order is random; the artifact list must be reproducible

	var unresolved []string
	for _, name := range names {
		for _, inst := range doc.Plugins[name] {
			if inst.InstallPath == "" {
				continue
			}
			real, rerr := filepath.EvalSymlinks(inst.InstallPath)
			if rerr != nil {
				// A stale entry pointing at an uninstalled plugin loads nothing, so it is not a
				// coverage gap and saying so every run would be noise. Any other failure means the
				// install path IS there and this scan could not read it — see hidesContent.
				if hidesContent(rerr) {
					unresolved = append(unresolved, name)
				}
				continue
			}
			if fi, serr := os.Stat(real); serr != nil || !fi.IsDir() {
				if hidesContent(serr) {
					unresolved = append(unresolved, name)
				}
				continue
			}
			if !withinDir(home, real) {
				// The key is installed_plugins.json's text: config values reach a snippet only
				// through the redactor (invariant #3).
				notes = append(notes, model.Finding{
					RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
					Title:    "Plugin install path points outside HOME, skipped",
					Why:      "An installed plugin resolves to a location outside the user home; that is not a normal install layout, and its contents were not collected.",
					Evidence: []model.Evidence{{File: path, Line: 0, Snippet: "install path escapes HOME: " + redact.Secrets(name)}},
				})
				continue
			}
			out = append(out, artifact(model.KindPlugin, pluginName(name, inst.Version), real, TreeHash(real, real)))
			env.Plugins++
			env.BundledSkills += bundledSkills(real)

			// The plugin's OWN hooks, re-collected per (event, matcher, command) through the same
			// builder settings.json goes through. The tree is already scanned as text above, but a hook
			// read as text is a hook that misses everything hook-SPECIFIC: HOOK-001 (shell chaining,
			// unremarkable in a script and telling in a hook), the followed script, the judge's hook
			// capability pass, and a finding that names PreToolUse[Bash]#1 instead of "somewhere in this
			// plugin". Plugins are the channel that installs external content in bulk, and hooks are the
			// surface that runs shell silently — that intersection had the coarsest audit in the tool.
			//
			// Sharing collectHooks is the point, not a convenience: two builders would drift, and the
			// one nobody looks at would be the plugin one.
			//
			// A hook command therefore appears TWICE — once inside the plugin tree read as text, once as
			// its own artifact — and that is deliberate. The report folds by (artifact, rule), so it is
			// two rows saying different things at different grains: "this plugin contains curl | bash"
			// and "PreToolUse[Bash]#1 runs curl | bash on every Bash call". The second is the actionable
			// one. Suppressing the first would mean excluding hook files from the tree read, which makes
			// the tree scan depend on hook collection succeeding — so a parse failure would turn a
			// double report into NO report. Erring toward the duplicate is the safe direction.
			ph, phn := collectPluginHooks(real, " (plugin "+name+")", env)
			out = append(out, ph...)
			notes = append(notes, phn...)
			pm, pmn := collectPluginMCP(real, " (plugin "+name+")", env)
			out = append(out, pm...)
			notes = append(notes, pmn...)
		}
	}
	if len(unresolved) > 0 {
		notes = append(notes, unresolvedNote(path, unresolved))
	}
	return out, notes
}

// bundledSkills counts the skills a plugin bundle ships (<bundle>/skills/*/SKILL.md). They are
// scanned inside the plugin's tree and never become artifacts of their own — this is a count
// for the inventory line, not a change of attribution.
func bundledSkills(bundle string) int {
	ents, err := os.ReadDir(filepath.Join(bundle, "skills"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if fi, err := os.Stat(filepath.Join(bundle, "skills", e.Name(), "SKILL.md")); err == nil && fi.Mode().IsRegular() {
			n++
		}
	}
	return n
}

// pluginName labels a plugin artifact "<name>@<marketplace> (version)" — the version
// keeps two installed versions of the same plugin distinguishable in the report.
func pluginName(name, version string) string {
	if version == "" {
		return name
	}
	return name + " (" + version + ")"
}

// pluginHookFiles are where a plugin declares hooks. Claude Code reads `hooks/hooks.json` in a
// plugin; `.claude/settings.json` is accepted too because a plugin that vendors a settings file is a
// plugin whose hooks load, and refusing to look would be a gap chosen for tidiness.
var pluginHookFiles = []string{
	filepath.Join("hooks", "hooks.json"),
	"hooks.json",
	filepath.Join(".claude", "settings.json"),
}

// collectPluginHooks reads a plugin's own hook declarations and returns ONE ARTIFACT PER COMMAND,
// through the same builder settings.json uses.
//
// Containment is re-checked per file even though the plugin root already passed: the paths are joined
// from a manifest-supplied installPath, and a check that ran once on the parent is a check that a
// symlink inside the tree walks around.
func collectPluginHooks(pluginRoot, nameSuffix string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	var out []model.ArtifactReport
	var notes []model.Finding
	for _, rel := range pluginHookFiles {
		p := filepath.Join(pluginRoot, rel)
		if !withinDir(pluginRoot, p) {
			continue
		}
		b, err := safeio.ReadFile(p, safeio.MaxConfigBytes)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				// Present but not readable as a bounded regular file (a FIFO, an oversized
				// file): the hooks it may declare were not audited, and that is said.
				notes = append(notes, ioNote(p, err))
			}
			continue // absent is the normal case: most plugins declare no hooks
		}
		// Both shapes seen in the wild: a bare {"PreToolUse": …} map, and a settings-style file with
		// the hooks nested under a "hooks" key. Trying the nested form first and falling back means a
		// plugin using either layout is audited, rather than one of them silently yielding nothing.
		var wrapper struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		hooks := map[string]json.RawMessage{}
		if json.Unmarshal(b, &wrapper) == nil && len(wrapper.Hooks) > 0 {
			hooks = wrapper.Hooks
		} else if json.Unmarshal(b, &hooks) != nil {
			notes = append(notes, model.Finding{
				RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
				Title: "Plugin hook file could not be parsed, hooks not audited (partial)",
				Why: "A plugin declares hooks in this file but it is not readable as JSON, so its hook " +
					"commands were NOT audited per (event, command). The plugin tree is still scanned as text.",
				Evidence: []model.Evidence{{File: p, Line: 0, Snippet: "unparseable plugin hook file"}},
			})
			continue
		}
		if len(hooks) == 0 {
			continue
		}
		// pluginRoot is the hook's OwnerRoot: a plugin hook names its scripts relative to the
		// plugin root, which the manifest already resolved and contained.
		h, hn := collectHooks(p, nameSuffix, pluginRoot, hooks, env)
		out = append(out, h...)
		notes = append(notes, hn...)
	}
	return out, notes
}

// pluginMCPFiles are where a plugin declares the MCP servers it ships. Claude Code reads `.mcp.json`
// at the plugin root; a bare `mcp.json` is accepted for the same reason `hooks.json` is.
var pluginMCPFiles = []string{".mcp.json", "mcp.json"}

// collectPluginMCP turns a plugin's bundled MCP servers into MCP artifacts, one per server, through
// the same reader the user-level and project-level configs go through.
//
// Until this existed the bundle's `.mcp.json` was read only as text inside the plugin tree, so a
// plugin's servers were invisible where it mattered: the inventory said `mcp=0`, and a summary built
// on that inventory told an operator with the Figma plugin installed that they had "zero MCP
// servers, nothing exposed" — while its server was live from the first turn of every session, on
// exactly the surface the load-time gate cannot cover. A count the operator reasons from has to
// include the servers a plugin brings; the gate's own message already says plugin MCP is not gated,
// and that sentence is only useful next to a number that is not zero when it is not.
//
// Containment is re-checked per file, as for hooks: the path is joined from a manifest-supplied
// installPath, and a symlink inside the tree is the cheapest way to walk around a parent check.
func collectPluginMCP(pluginRoot, nameSuffix string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	var out []model.ArtifactReport
	var notes []model.Finding
	for _, rel := range pluginMCPFiles {
		p := filepath.Join(pluginRoot, rel)
		if !withinDir(pluginRoot, p) {
			continue
		}
		m, mn := mcpServersFrom(p, nameSuffix, env) // absent file → nil, nil
		out = append(out, m...)
		notes = append(notes, mn...)
	}
	return out, notes
}

// collectSyncedPlugins walks plugins/synced/<uuid>/<plugin>/ — the layout the Cowork/Cloud
// sandbox uses, where synced plugins carry no installed_plugins.json entry. Each <plugin>
// directory with a .claude-plugin/plugin.json is collected exactly like a manifest plugin:
// the tree as one artifact, plus its own hooks and MCP servers. The session uuid is a grouping
// level and is not part of the plugin name.
func collectSyncedPlugins(root, home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	base := filepath.Join(root, "plugins", "synced")
	uuids, err := os.ReadDir(base)
	if err != nil {
		return nil, nil // absent on an ordinary machine
	}
	var out []model.ArtifactReport
	var notes []model.Finding
	var unresolved []string
	for _, u := range uuids {
		if !u.IsDir() {
			continue
		}
		group := filepath.Join(base, u.Name())
		plugins, perr := os.ReadDir(group)
		if perr != nil {
			continue
		}
		for _, pl := range plugins {
			if !pl.IsDir() {
				continue
			}
			entry := filepath.Join(group, pl.Name())
			real, rerr := filepath.EvalSymlinks(entry)
			if rerr != nil {
				if hidesContent(rerr) {
					unresolved = append(unresolved, "synced/"+pl.Name())
				}
				continue
			}
			if fi, serr := os.Stat(real); serr != nil || !fi.IsDir() {
				if hidesContent(serr) {
					unresolved = append(unresolved, "synced/"+pl.Name())
				}
				continue
			}
			if !withinDir(home, real) {
				notes = append(notes, model.Finding{
					RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
					Title:    "Synced plugin resolves outside HOME, skipped",
					Why:      "A synced plugin directory resolves outside the user home; its contents were not collected.",
					Evidence: []model.Evidence{{File: entry, Line: 0, Snippet: "synced plugin escapes HOME"}},
				})
				continue
			}
			if _, mderr := os.Stat(filepath.Join(real, ".claude-plugin", "plugin.json")); mderr != nil {
				continue // not a plugin by layout
			}
			out = append(out, artifact(model.KindPlugin, pluginName(pl.Name(), ""), real, TreeHash(real, real)))
			env.Plugins++
			env.BundledSkills += bundledSkills(real)
			ph, phn := collectPluginHooks(real, " (synced plugin "+pl.Name()+")", env)
			out = append(out, ph...)
			notes = append(notes, phn...)
			pm, pmn := collectPluginMCP(real, " (synced plugin "+pl.Name()+")", env)
			out = append(out, pm...)
			notes = append(notes, pmn...)
		}
	}
	if len(unresolved) > 0 {
		notes = append(notes, unresolvedNote(base, unresolved))
	}
	return out, notes
}
