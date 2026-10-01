// SPDX-License-Identifier: MIT
package collect

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// Claude Desktop's own plugin and skill store.
//
// The desktop app (Cowork, and the Code tab that runs Claude Code locally) does not install
// through <root>/plugins/installed_plugins.json. Plugins added in Customize → Plugins and the
// skills shown in Customize → Skills (the user's uploads plus the Anthropic-managed built-ins)
// are synced from the claude.ai account into the app's data directory and handed to the spawned
// CLI as `--plugin-dir` arguments at launch. They load in every Code-tab session on the machine,
// and it is the install path a non-technical user is most likely to have used — so a scan that
// reads only the config root misses exactly them.
//
// Layout, as observed on Claude Desktop for macOS, under
// <home>/Library/Application Support/Claude/local-agent-mode-sessions/:
//
//	<account>/<session>/rpm/manifest.json          {"plugins":[{"id","name","marketplaceName",…}]}
//	<account>/<session>/rpm/plugin_<id>/           one plugin bundle each (.claude-plugin/plugin.json)
//	skills-plugin/<session>/<account>/skills/<x>/  the desktop's skills, packaged as ONE bundle
//	skills-plugin/<session>/<account>/manifest.json {"skills":[{"skillId","creatorType","enabled",…}]}
//
// This is an undocumented internal layout and can move with an app update, so the collector is
// best-effort in ONE direction only. The base directory being absent is the normal case (no
// desktop app, a Linux host, a project-level root whose "home" is the project directory) and
// yields nothing. A directory that IS there and cannot be read or resolved is disclosed (IO-000
// / COV-000 / SCOPE-001, invariant #5): a layout change must show up as "nothing collected from
// the desktop store" in a report, never as a silent return to the blind spot this file closes.
// Nothing here is executed and nothing is followed outside home; every bundle path goes through
// the same containment as an installPath.
//
// The desktop's skills bundle is collected PER SKILL (through collectSkills), not as one plugin
// artifact: the bundle changes whenever the user uploads anything, so a tree hash over the whole
// bundle would never be stable enough to key reputation or approvals on, while each skill's own
// tree hash is. Its hooks, if it ever declares any, still get the per-command audit.
//
// Not collected here: the desktop's remote MCP connectors. Their tool descriptions are cached
// per session inside the session files next to this store, but those files also hold the
// user's own session state, and the MCP detection path reads mcpServers configs, not cached
// tool lists. A different shape and a different trust boundary.

// desktopSessionsDir is the store's location relative to home. Relative on purpose: tests build
// it under a temporary home, and a project-level root gets a "home" where it does not exist.
var desktopSessionsDir = filepath.Join("Library", "Application Support", "Claude", "local-agent-mode-sessions")

// desktopMarker is appended to every artifact this collector produces, so a report says which
// install channel a plugin or skill came through. It sits in the marketplace position of a plugin
// name ("aguard@AgentGuard via Claude Desktop (0.3.0)") so friendlyArtifact renders it as
// "aguard plugin, from AgentGuard via Claude Desktop (0.3.0)".
const desktopMarker = "Claude Desktop"

// desktopRPMManifest is <rpm>/manifest.json: the desktop's record of which bundle is which.
type desktopRPMManifest struct {
	Plugins []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		MarketplaceName string `json:"marketplaceName"`
	} `json:"plugins"`
}

// desktopBase returns the store's directory and whether it exists. An I/O error other than
// absence is returned as a note, because "unreadable" is a gap and "absent" is not.
func desktopBase(home string) (string, bool, []model.Finding) {
	base := filepath.Join(home, desktopSessionsDir)
	fi, err := os.Stat(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return base, false, nil
		}
		return base, false, []model.Finding{ioNote(base, err)}
	}
	if !fi.IsDir() {
		return base, false, nil
	}
	return base, true, nil
}

// desktopRPMDirs lists every <account>/<session>/rpm directory under base, sorted so the artifact
// order is reproducible. Glob only fails on a malformed pattern, which this is not.
func desktopRPMDirs(base string) []string {
	dirs, _ := filepath.Glob(filepath.Join(base, "*", "*", "rpm"))
	sort.SliceStable(dirs, func(i, j int) bool {
		ai, aj := bundleLastUpdated(dirs[i]), bundleLastUpdated(dirs[j])
		if ai != aj {
			return ai > aj
		}
		return dirs[i] < dirs[j]
	})
	return dirs
}

// desktopSkillBundles lists every skills-plugin/<session>/<account> directory under base,
// newest first by the manifest's lastUpdated (ties and unreadable manifests fall back to path
// order). The desktop keeps one bundle PER SESSION and does not delete the old ones when a new
// session starts, so a machine that has been used for a while holds several byte-identical
// copies of the same eleven skills. Only the newest is handed to the CLI; walking them all
// counted 22 skills where there were 11, listed every allowlist decision twice and turned the
// cleanup section into a wall of "docx duplicates docx". collectDesktop keeps the first copy of
// each (name, hash) it sees, which with this ordering is the current one.
func desktopSkillBundles(base string) []string {
	dirs, _ := filepath.Glob(filepath.Join(base, "skills-plugin", "*", "*"))
	sort.SliceStable(dirs, func(i, j int) bool {
		ai, aj := bundleLastUpdated(dirs[i]), bundleLastUpdated(dirs[j])
		if ai != aj {
			return ai > aj
		}
		return dirs[i] < dirs[j]
	})
	return dirs
}

// bundleLastUpdated reads manifest.json's lastUpdated (a millisecond timestamp); 0 when absent.
func bundleLastUpdated(bundle string) int64 {
	b, err := safeio.ReadFile(filepath.Join(bundle, "manifest.json"), safeio.MaxConfigBytes)
	if err != nil {
		return 0
	}
	var doc struct {
		LastUpdated int64 `json:"lastUpdated"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return 0
	}
	return doc.LastUpdated
}

// desktopPluginLabels reads an rpm manifest into bundle-directory → "name@marketplace via Claude
// Desktop". A missing manifest is an empty map and no note: the bundles are still found by
// directory and named by directory. An unparseable one is a PARSE-000 — the bundles are still
// collected, only their names degrade — because the safe direction is to scan more, never less.
func desktopPluginLabels(path string) (map[string]string, []model.Finding) {
	labels := map[string]string{}
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return labels, nil
		}
		return labels, []model.Finding{ioNote(path, err)}
	}
	var doc desktopRPMManifest
	if json.Unmarshal(b, &doc) != nil {
		return labels, []model.Finding{{
			RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
			Title: "Claude Desktop plugin manifest could not be parsed, plugins named by directory (partial)",
			Why: "The desktop app's record of which plugin bundle is which is not readable as JSON. The bundles " +
				"themselves were still collected and scanned; they are just labelled by directory rather than by name.",
			Evidence: []model.Evidence{{File: path, Line: 0, Snippet: "unparseable desktop plugin manifest"}},
		}}
	}
	for _, p := range doc.Plugins {
		if p.ID == "" || p.Name == "" {
			continue
		}
		label := p.Name
		if p.MarketplaceName != "" {
			label += "@" + p.MarketplaceName + " via " + desktopMarker
		} else {
			label += "@" + desktopMarker
		}
		labels[p.ID] = label
	}
	return labels, nil
}

// bundleVersion reads a plugin bundle's own manifest for its version; "" when absent or
// unreadable. Cosmetic — it keeps two versions of one plugin apart in a report — so a failure
// here is not a finding.
func bundleVersion(bundle string) string {
	b, err := safeio.ReadFile(filepath.Join(bundle, pluginManifest), safeio.MaxConfigBytes)
	if err != nil {
		return ""
	}
	var doc struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	return doc.Version
}

// resolveDesktopEntry applies the plugin-install containment to one store entry: symlinks
// resolved, must be a directory, must stay under home. It returns the real path and true when the
// entry is usable; otherwise it has already recorded why (unresolved for hidesContent errors, a
// SCOPE-001 note for an escape) and returns false. A dangling link is neither: nothing loads.
func resolveDesktopEntry(entry, home, what string, unresolved *[]string, notes *[]model.Finding) (string, bool) {
	real, err := filepath.EvalSymlinks(entry)
	if err != nil {
		if hidesContent(err) {
			*unresolved = append(*unresolved, filepath.Base(entry))
		}
		return "", false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		if hidesContent(err) {
			*unresolved = append(*unresolved, filepath.Base(entry))
		}
		return "", false
	}
	if !withinDir(home, real) {
		*notes = append(*notes, model.Finding{
			RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
			Title:    what + " resolves outside HOME, skipped",
			Why:      "An entry in Claude Desktop's plugin store resolves to a location outside the user home; that is not a layout the app produces, and its contents were not collected.",
			Evidence: []model.Evidence{{File: entry, Line: 0, Snippet: "store entry escapes HOME"}},
		})
		return "", false
	}
	return real, true
}

// collectDesktop enumerates the desktop store described at the top of this file: each rpm
// bundle as a plugin artifact (with its hooks audited per command, exactly like an installed
// plugin), and each skill in the desktop's skills bundle as a skill artifact.
func collectDesktop(home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	base, ok, notes := desktopBase(home)
	if !ok {
		return nil, notes
	}
	var out []model.ArtifactReport
	seen := map[string]bool{}        // one artifact per resolved path, however many sessions point at it
	seenContent := map[string]bool{} // one artifact per (kind, name, hash) across session bundles

	for _, rpm := range desktopRPMDirs(base) {
		labels, ln := desktopPluginLabels(filepath.Join(rpm, "manifest.json"))
		notes = append(notes, ln...)
		ents, err := os.ReadDir(rpm)
		if err != nil {
			notes = append(notes, ioNote(rpm, err))
			continue
		}
		var unresolved []string
		for _, e := range ents {
			if !strings.HasPrefix(e.Name(), "plugin_") {
				continue
			}
			real, ok := resolveDesktopEntry(filepath.Join(rpm, e.Name()), home, "Claude Desktop plugin", &unresolved, &notes)
			if !ok || seen[real] {
				continue
			}
			seen[real] = true
			label := labels[e.Name()]
			if label == "" {
				label = e.Name() + "@" + desktopMarker
			}
			hash := TreeHash(real, real)
			// Same bundle in an older session's rpm directory (the desktop keeps one per session):
			// one artifact, the first — newest — copy wins, exactly as for the skills bundles below.
			key := string(model.KindPlugin) + "\x00" + label + "\x00" + hash
			if seenContent[key] {
				continue
			}
			seenContent[key] = true
			out = append(out, artifact(model.KindPlugin, pluginName(label, bundleVersion(real)), real, hash))
			env.BundledSkills += bundledSkills(real)
			env.Plugins++
			// Same per-(event, matcher, command) treatment installed plugins get; see collectPlugins
			// for why a hook read only as text is a hook that misses everything hook-specific.
			ph, phn := collectPluginHooks(real, " (plugin "+label+")", env)
			out = append(out, ph...)
			notes = append(notes, phn...)
			pm, pmn := collectPluginMCP(real, " (plugin "+label+")", env)
			out = append(out, pm...)
			notes = append(notes, pmn...)
		}
		if len(unresolved) > 0 {
			notes = append(notes, unresolvedNote(rpm, unresolved))
		}
	}

	for _, bundle := range desktopSkillBundles(base) {
		var unresolved []string
		real, ok := resolveDesktopEntry(bundle, home, "Claude Desktop skills bundle", &unresolved, &notes)
		if len(unresolved) > 0 {
			notes = append(notes, unresolvedNote(filepath.Dir(bundle), unresolved))
		}
		if !ok || seen[real] {
			continue
		}
		seen[real] = true
		sk, sn := collectSkills(real, home, env)
		for _, a := range sk {
			a.Name += " (" + desktopMarker + ")"
			// Same skill, same bytes, older session bundle: one artifact, not one per copy.
			// A copy whose content differs is kept — it is a different thing to audit.
			key := string(a.Kind) + "\x00" + a.Name + "\x00" + a.Hash
			if seenContent[key] {
				if a.Kind == model.KindSkill {
					env.Skills--
				}
				continue
			}
			seenContent[key] = true
			out = append(out, a)
		}
		notes = append(notes, sn...)
		ph, phn := collectPluginHooks(real, " (plugin skills@"+desktopMarker+")", env)
		out = append(out, ph...)
		notes = append(notes, phn...)
		pm, pmn := collectPluginMCP(real, " (plugin skills@"+desktopMarker+")", env)
		out = append(out, pm...)
		notes = append(notes, pmn...)
	}
	return out, notes
}

// desktopPluginInstalls maps a desktop-installed bundle's name (the part before "@marketplace")
// to its directory and marketplace, for the load-time gate's `plugin:skill` resolution — the
// same lookup PluginInstalls does over installed_plugins.json, over the other install channel.
// Same containment, no notes: a bundle it cannot resolve is simply absent, and the gate turns an
// absence into GATE-000 rather than a silent pass.
func desktopPluginInstalls(home string) map[string]PluginInstall {
	out := map[string]PluginInstall{}
	base, ok, _ := desktopBase(home)
	if !ok {
		return out
	}
	for _, rpm := range desktopRPMDirs(base) {
		b, err := safeio.ReadFile(filepath.Join(rpm, "manifest.json"), safeio.MaxConfigBytes)
		if err != nil {
			continue
		}
		var doc desktopRPMManifest
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		for _, p := range doc.Plugins {
			if p.ID == "" || p.Name == "" {
				continue
			}
			real, err := filepath.EvalSymlinks(filepath.Join(rpm, p.ID))
			if err != nil {
				continue
			}
			if fi, serr := os.Stat(real); serr != nil || !fi.IsDir() {
				continue
			}
			if !withinDir(home, real) {
				continue
			}
			out[p.Name] = PluginInstall{Dir: real, Marketplace: p.MarketplaceName, Desktop: true}
		}
	}
	return out
}

// DesktopSkillBundles returns the desktop store's skills bundles under home — each holds
// skills/<x> and a manifest.json — for tooling that needs the desktop's copy of a skill: the
// reputation refresh drafts and renews Claude Desktop allowlist entries from it. Discovery only,
// exactly what the collector does; containment and disclosure stay with collectDesktop.
func DesktopSkillBundles(home string) []string {
	base, ok, _ := desktopBase(home)
	if !ok {
		return nil
	}
	return desktopSkillBundles(base)
}

// DesktopSkillRecord returns what the desktop's manifest says about one skill in a bundle: the
// `updatedAt` stamp the app moves when it re-syncs the skill, and its `creatorType` ("anthropic"
// for the built-ins). The stamp is what a Claude Desktop reputation entry pins as its `sha` —
// the app has no commit to offer, and this is the value that changes when the content does.
// ok is false when the manifest is unreadable or does not list the skill.
func DesktopSkillRecord(bundle, skill string) (updatedAt, creatorType string, ok bool) {
	b, err := safeio.ReadFile(filepath.Join(bundle, "manifest.json"), safeio.MaxConfigBytes)
	if err != nil {
		return "", "", false
	}
	var doc struct {
		Skills []struct {
			SkillID     string `json:"skillId"`
			UpdatedAt   string `json:"updatedAt"`
			CreatorType string `json:"creatorType"`
		} `json:"skills"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return "", "", false
	}
	for _, s := range doc.Skills {
		if s.SkillID == skill {
			return s.UpdatedAt, s.CreatorType, true
		}
	}
	return "", "", false
}

// DesktopStore returns the desktop store's path under home and whether it exists — for the
// report's "where the scan looked" list, which must tell an absent store from an empty one.
func DesktopStore(home string) (string, bool) {
	base, ok, _ := desktopBase(home)
	return base, ok
}
