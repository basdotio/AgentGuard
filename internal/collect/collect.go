// SPDX-License-Identifier: MIT
// Package collect enumerates and reads the artifacts in a Claude Code environment
// (spec §4): skills, MCP servers, hooks, permissions, subagents, commands, plugins,
// instruction files. It is READ-ONLY and NEVER executes anything (spec §16.1). It
// audits install-via-symlink skills (resolving to their real root) but refuses to
// read a skill's INTERNAL files whose targets escape that skill root (spec §16.2).
// Detection/scoring happen later; M1 only produces the artifact inventory.
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
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// Result bundles the collected artifacts, environment summary, and scan-level notes
// (I/O errors, skipped artifacts) — notes ensure "read 0 → all clear" can't hide a
// failed scan (spec §4 three-state / review B-2).
type Result struct {
	Artifacts []model.ArtifactReport
	Env       model.EnvSummary
	Notes     []model.Finding
}

func artifact(kind model.ArtifactKind, name, path, hash string) model.ArtifactReport {
	return model.ArtifactReport{Kind: kind, Name: name, Path: path, Hash: hash, Score: 100, Findings: []model.Finding{}}
}

// ioNote builds a scan-level warning for a non-ENOENT I/O error (dir unreadable, etc).
func ioNote(path string, err error) model.Finding {
	return model.Finding{
		RuleID: "IO-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
		Title:    "Path unreadable, scan incomplete (partial)",
		Why:      "The location exists but cannot be read (permission or I/O error); surfaced, not silently ignored, so a missed scan cannot masquerade as clean.",
		Evidence: []model.Evidence{{File: path, Line: 0, Snippet: err.Error()}},
	}
}

// unresolvedNote discloses entries inside a LOAD NAMESPACE that could not be resolved, so a scan
// that skipped them cannot read as a scan that found them clean (invariant #5).
//
// Aggregated into ONE finding per namespace, for the reason coalesceGeneratedDirNotes exists: a
// disclosure repeated per entry becomes wallpaper, and wallpaper is worth what silence is worth.
func unresolvedNote(where string, entries []string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Entries in a load namespace could not be resolved (incomplete coverage)",
		Why: fmt.Sprintf("%d entr(y/ies) under %s could not be resolved and were NOT scanned: %s. "+
			"A dangling link is not reported (the agent cannot load one either); this is the other case — "+
			"something is there and could not be read.", len(entries), where, strings.Join(entries, ", ")),
		Evidence: []model.Evidence{{File: where, Line: 0, Snippet: "unresolvable entries in a load namespace"}},
	}
}

// hidesContent separates the two failures filepath.EvalSymlinks and os.Stat collapse into one
// error, which is the whole reason this check exists.
//
// A DANGLING symlink — the target was uninstalled, the link stayed — means there is genuinely
// nothing on disk: Claude Code cannot load it either, so "not scanned" describes no blind spot and
// a note would be noise on every run of a real machine. Any OTHER failure (a permission denied
// somewhere on the path, a symlink loop, a name too long) means something IS there and this scan
// could not see it. Treating both as harmless is what made a skill able to vanish from the
// inventory with the report still printing "no risk findings".
func hidesContent(err error) bool {
	return err != nil && !errors.Is(err, fs.ErrNotExist)
}

// pluginManifest is the file that marks a directory as a plugin BUNDLE (as opposed to the
// root that installs it): <bundle>/.claude-plugin/plugin.json.
var pluginManifest = filepath.Join(".claude-plugin", "plugin.json")

// looksLikeRoot reports whether dir is a .claude config root, i.e. a directory whose
// CONTENTS are audited by the per-kind collectors rather than read as one artifact. Defined
// next to CollectAll so the router and the collectors can never disagree about what a root is.
//
// Every marker here is STRUCTURAL and CLAUDE-SPECIFIC, deliberately. Routing to CollectAll
// narrows what gets read to the known sub-layouts, so a marker that fires on an ordinary
// directory would re-open the very false negative CollectTarget exists to close:
//
//   - skills/, agents/, commands/ are NOT markers — a plugin bundle has them too, and routing
//     a bundle here would collect its skills while ignoring its scripts/, hooks/ and .mcp.json.
//   - settings.json is NOT a marker, whatever it contains. Its content is the target author's,
//     and `check`'s target is the thing under audit, so nothing inside the target gets to decide
//     how much of the target is read (the same rule as "check does not auto-discover a
//     baseline"). Measured: two byte-identical payload trees, one with a 29-byte
//     `{"permissions":{"allow":[]}}` added, scored 26/100 with four findings and 100/100 with
//     none — the second was routed here, its lib/evil.sh became an "unowned directory" the root
//     collectors do not read, and the emptyRootNote told the reader "nothing to audit". The same
//     29 bytes work through the Downloads scan.
//
// What is left: the directory is literally named `.claude`, or it carries Claude Code's own
// plugin manifest. An adversary can ship those too — but then they have shaped the tree as a
// root, and a root's rules (unowned top-level directories are disclosed by name, never read)
// are the correct rules for it; the report says which directories it did not read. A
// CLAUDE_CONFIG_DIR with a custom name is a `scan --root` target, not a `check` target —
// `check` audits one thing, and its help says so.
func looksLikeRoot(dir string) bool {
	if filepath.Base(dir) == ".claude" {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "installed_plugins.json")); err == nil {
		return true
	}
	return false
}

// singleFileKind names a lone file target by where it sits. A .md directly under a commands/
// directory is a slash command — the kind a scan of the parent root gives it — so
// `aguard check ~/.claude/commands/deploy.md` runs the rules the scan runs, instead of reading
// the file as prose because its name is not SKILL.md. Anything else is an instruction file,
// classified by name downstream.
func singleFileKind(path string) model.ArtifactKind {
	if strings.EqualFold(filepath.Ext(path), ".md") && filepath.Base(filepath.Dir(path)) == "commands" {
		return model.KindCommand
	}
	return model.KindInstruction
}

// CollectTarget builds the artifact set for `aguard check <path>` — which is a SINGLE
// skill/dir/file, NOT a .claude root. Layout is detected (spec §3 gate contract; fixes
// the review B1 bug where a bare skill dir hit CollectAll, found no skills/, and scored
// a malicious skill as clean). Order matters — most specific first:
//
//   - a single file                       → one artifact (scanned per its role)
//   - a dir containing SKILL.md           → one skill artifact (whole-tree scanned)
//   - a dir containing a plugin manifest  → one plugin artifact (whole-tree scanned)
//   - a dir that looks like a root        → CollectAll (see looksLikeRoot)
//   - any other dir                       → one directory artifact (whole-tree scanned)
//
// The last branch used to fall through to CollectAll, which finds only known sub-layouts:
// a bare directory yielded ZERO artifacts and ZERO notes, so a malicious install.sh at its
// top level scored 100/100 and passed the gate. An unrecognized layout is now read whole.
//
// An unreadable target is an ERROR, not an empty result: a gate that cannot read what it was
// pointed at must fail loudly (`check <typo>` used to print 100/100 and exit 0).
func CollectTarget(path string) (Result, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	single := func(kind model.ArtifactKind, hash string, env model.EnvSummary) (Result, error) {
		a := artifact(kind, filepath.Base(path), path, hash)
		return Result{Artifacts: []model.ArtifactReport{a}, Env: env, Notes: []model.Finding{}}, nil
	}
	if !fi.IsDir() {
		// Single file: one unit; detect picks the rule set from the kind, or by name for an
		// instruction file.
		return single(singleFileKind(path), FileHash(path), model.EnvSummary{})
	}
	if _, serr := os.Stat(filepath.Join(path, "SKILL.md")); serr == nil {
		return single(model.KindSkill, TreeHash(path, path), model.EnvSummary{Skills: 1})
	}
	if _, perr := os.Stat(filepath.Join(path, pluginManifest)); perr == nil {
		return single(model.KindPlugin, TreeHash(path, path), model.EnvSummary{Plugins: 1, BundledSkills: bundledSkills(path)})
	}
	if looksLikeRoot(path) {
		return CollectAll(path), nil
	}
	return single(model.KindDirectory, TreeHash(path, path), model.EnvSummary{})
}

// ValidateRoot reports whether root is usable as a scan root: it must exist and be a
// directory. Callers must run it BEFORE CollectAll and treat a failure as a hard error.
//
// This is the CollectTarget rule (an unreadable target is an error, not an empty result)
// applied to the other entry point. Every collector below treats ENOENT as "this layout is
// absent", which is right per-collector and catastrophic for the root itself: a mistyped
// --root made all of them absent at once, and `scan --root ~/.clade` printed
// "Risk score 100/100 (Low) · ✅ No risk findings" and exited 0 — a perfect score for a
// directory that was never there. Pointing at the wrong root (project-level vs. user-level)
// is the single most likely first-run mistake, so it fails loudly instead (invariant #5).
func ValidateRoot(root string) error {
	fi, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("scan root %s is not a directory", root)
	}
	return nil
}

// emptyRootNote is raised when a VALID root yielded no artifacts at all. 100/100 is honest
// for a fresh install with nothing configured, and indistinguishable from a root pointed one
// directory too high — so the report says which one it is looking at rather than letting the
// score speak alone (invariant #5: no omission stays silent).
func emptyRootNote(root string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title:    "Nothing to audit under this root",
		Why:      "The root exists but no skill, MCP server, hook, permission list, subagent, command, plugin or instruction file was found, so the score below reflects an empty inventory rather than a clean one. Expected for an unconfigured environment; otherwise check --root (a project's .claude and the user-level ~/.claude are different roots).",
		Evidence: []model.Evidence{{File: root, Line: 0, Snippet: "0 artifacts collected"}},
	}
}

// CollectAll runs every collector under root. home (the parent of root, e.g. ~ for
// ~/.claude) scopes the user-level MCP config and the install-symlink guard, so the
// whole scan is hermetic and honours --root.
//
// Callers reaching this from a CLI root flag must call ValidateRoot first: this function
// cannot distinguish "root absent" from "root empty", and only the caller knows whether a
// missing root was a typo.
func CollectAll(root string) Result {
	// Clean first. `--root ~/.claude/` (shell completion adds the slash) left home == root, which
	// collected CLAUDE.md twice, made every user-level MCP server vanish, and shrank the import
	// boundary to root. Harmless before, because home only scoped two things; it now also scopes the
	// project instruction files, the project MCP config and the import boundary.
	root = filepath.Clean(root)
	home := filepath.Dir(root)
	var res Result

	skills, sn := collectSkills(root, home, &res.Env)
	res.Artifacts = append(res.Artifacts, skills...)
	res.Notes = append(res.Notes, sn...)

	mcp, mn := collectMCP(home, &res.Env)
	res.Artifacts = append(res.Artifacts, mcp...)
	res.Notes = append(res.Notes, mn...)

	settings, cn := collectSettings(root, &res.Env)
	res.Artifacts = append(res.Artifacts, settings...)
	res.Notes = append(res.Notes, cn...)

	pmcp, pmn := mcpServersFrom(filepath.Join(home, ".mcp.json"), "", &res.Env)
	res.Artifacts = append(res.Artifacts, pmcp...)
	res.Notes = append(res.Notes, pmn...)

	// agents/ and commands/ are walked RECURSIVELY. The flat read they replaced skipped every
	// subdirectory outright, which quietly dropped namespaced commands (`commands/foo/bar.md`,
	// invoked as `/foo:bar`) — files that load and run like any other.
	sub, subn := collectTree(root, "agents", model.KindSubagent, acceptAny, &res.Env)
	res.Artifacts = append(res.Artifacts, sub...)
	res.Notes = append(res.Notes, subn...)

	cmd, cmdn := collectTree(root, "commands", model.KindCommand, acceptAny, &res.Env)
	res.Artifacts = append(res.Artifacts, cmd...)
	res.Notes = append(res.Notes, cmdn...)

	rules, rn := collectRules(root, &res.Env)
	res.Artifacts = append(res.Artifacts, rules...)
	res.Notes = append(res.Notes, rn...)

	wf, wfn := collectTree(root, "workflows", model.KindWorkflow, acceptAny, &res.Env)
	res.Artifacts = append(res.Artifacts, wf...)
	res.Notes = append(res.Notes, wfn...)

	styles, stn := collectTree(root, "output-styles", model.KindOutputStyle, acceptMarkdown, &res.Env)
	res.Artifacts = append(res.Artifacts, styles...)
	res.Notes = append(res.Notes, stn...)

	mem, memn := collectMemory(root, &res.Env)
	res.Artifacts = append(res.Artifacts, mem...)
	res.Notes = append(res.Notes, memn...)

	plug, pn := collectPlugins(root, home, &res.Env)
	res.Artifacts = append(res.Artifacts, plug...)
	res.Notes = append(res.Notes, pn...)

	// Claude Desktop keeps its own plugin and skill store outside the config root and hands it to
	// the CLI as --plugin-dir at launch; see desktop.go. Scoped by home like the MCP config: for a
	// project-level root "home" is the project directory, where this layout does not exist.
	dsk, dn := collectDesktop(home, &res.Env)
	res.Artifacts = append(res.Artifacts, dsk...)
	res.Notes = append(res.Notes, dn...)

	// Remote MCP connectors: the tool lists Claude Desktop cached from the servers attached to
	// the account (connectors.go). Same home scoping; absent on machines without the app.
	con, conn := collectConnectors(home, &res.Env)
	res.Artifacts = append(res.Artifacts, con...)
	res.Notes = append(res.Notes, conn...)

	quar, qn := collectQuarantine(root, &res.Env)
	res.Artifacts = append(res.Artifacts, quar...)
	res.Notes = append(res.Notes, qn...)

	instr, instrn := collectInstruction(root, home, res.Artifacts)
	res.Artifacts = append(res.Artifacts, instr...)
	res.Notes = append(res.Notes, instrn...)

	// Last, so it can speak about whatever the collectors above did NOT claim. Unlike the other
	// collectors this one both READS and DISCLOSES: a loose top-level file that looks like something
	// an interpreter runs becomes an artifact, while every directory and every unread entry is named
	// in one COV-000. See unowned.go for why the split falls on "does the agent have a load path in"
	// rather than on a name list or on file shape.
	un, unn := collectUnowned(root)
	res.Artifacts = append(res.Artifacts, un...)
	res.Notes = append(res.Notes, unn...)

	if len(res.Artifacts) == 0 {
		res.Notes = append(res.Notes, emptyRootNote(root))
	}

	if res.Artifacts == nil {
		res.Artifacts = []model.ArtifactReport{}
	}
	if res.Notes == nil {
		res.Notes = []model.Finding{}
	}
	return res
}

// collectSkills enumerates skills/*. A skill dir that is itself a symlink (install
// mechanism, e.g. -> ~/.agents/skills/x) is a legitimate audit target: it is resolved
// to its real root and scanned there. The resolved target must stay under home so a
// skill symlinked to a system path (/etc, ~/.ssh) is refused with a note (review B-1).
func collectSkills(root, home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	dir := filepath.Join(root, "skills")
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.Finding{ioNote(dir, err)}
	}
	var out []model.ArtifactReport
	var notes []model.Finding
	var unresolved []string
	for _, e := range ents {
		entry := filepath.Join(dir, e.Name())
		real, rerr := filepath.EvalSymlinks(entry)
		if rerr != nil {
			if hidesContent(rerr) {
				unresolved = append(unresolved, e.Name())
			}
			continue
		}
		fi, serr := os.Stat(real)
		if serr != nil {
			if hidesContent(serr) {
				unresolved = append(unresolved, e.Name())
			}
			continue
		}
		if !fi.IsDir() {
			continue
		}
		// Install-symlink guard: resolved skill root must stay under HOME.
		if !withinDir(home, real) {
			notes = append(notes, model.Finding{
				RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
				Title:    "Skill dir symlink points outside HOME, skipped",
				Why:      "Install symlinks should point inside the user home; one pointing to a system path is suspicious and its contents were not collected.",
				Evidence: []model.Evidence{{File: entry, Line: 0, Snippet: "symlink target escapes HOME"}},
			})
			continue
		}
		if _, mderr := os.Stat(filepath.Join(real, "SKILL.md")); mderr != nil {
			// `synced` is a RESERVED directory name inside a skills folder: it is where the skills
			// enabled on claude.ai are downloaded, which puts each one a level deeper than an
			// ordinary entry. A first-level-only walk finds no SKILL.md here and moves on, so every
			// synced skill was invisible — real, loaded skills that no rule ever ran against.
			if strings.EqualFold(e.Name(), "synced") {
				// synced can be one level deep (`synced/<name>/SKILL.md`, claude.ai) or two
				// (`synced/<uuid>/<name>/SKILL.md`, the Cowork/Cloud sandbox groups a whole
				// account's skills under a session uuid). One depth budget covers the uuid.
				nested, nn, nu := collectNestedSkills(real, home, e.Name(), 1, env)
				out = append(out, nested...)
				notes = append(notes, nn...)
				// Merged rather than noted separately: one namespace, one disclosure.
				unresolved = append(unresolved, nu...)
				continue
			}
			// Not a skill by layout — but it is sitting IN the skills namespace, which is where an
			// agent gets pointed. Skipping it outright was a silent false negative: a directory of
			// scripts under skills/ with the manifest left out read as nothing at all. Collected as a
			// plain directory (whole tree), and it counts as no skill.
			out = append(out, artifact(model.KindDirectory, filepath.Join("skills", e.Name()), real, TreeHash(real, real)))
			continue
		}
		out = append(out, artifact(model.KindSkill, e.Name(), real, TreeHash(real, real)))
		env.Skills++
	}
	if len(unresolved) > 0 {
		notes = append(notes, unresolvedNote(dir, unresolved))
	}
	return out, notes
}

// collectNestedSkills enumerates skill directories inside dir, naming them "<group>/<skill>" so a
// finding says where the skill came from. Containment is the same as the top level: a resolved
// target must stay under home. extraDepth is how many intermediate grouping directories (ones with
// no SKILL.md of their own) may sit between dir and a skill — 0 means skills are direct children,
// 1 allows one grouping level (the sandbox's session-uuid directory). The group label does NOT grow
// with the uuid: a report reads "synced/<skill>", not "synced/<uuid>/<skill>".
func collectNestedSkills(dir, home, group string, extraDepth int, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding, []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, []model.Finding{ioNote(dir, err)}, nil
	}
	var out []model.ArtifactReport
	var notes []model.Finding
	var unresolved []string
	for _, e := range ents {
		entry := filepath.Join(dir, e.Name())
		real, rerr := filepath.EvalSymlinks(entry)
		if rerr != nil {
			if hidesContent(rerr) {
				unresolved = append(unresolved, group+"/"+e.Name())
			}
			continue
		}
		fi, serr := os.Stat(real)
		if serr != nil {
			if hidesContent(serr) {
				unresolved = append(unresolved, group+"/"+e.Name())
			}
			continue
		}
		if !fi.IsDir() {
			continue
		}
		if !withinDir(home, real) {
			notes = append(notes, model.Finding{
				RuleID: "SCOPE-001", Dimension: 9, Severity: model.SevMedium, Source: model.SrcStatic,
				Title:    "Skill dir symlink points outside HOME, skipped",
				Why:      "Install symlinks should point inside the user home; one pointing to a system path is suspicious and its contents were not collected.",
				Evidence: []model.Evidence{{File: entry, Line: 0, Snippet: "symlink target escapes HOME"}},
			})
			continue
		}
		if _, mderr := os.Stat(filepath.Join(real, "SKILL.md")); mderr != nil {
			// No manifest here: if there is depth budget left, this is a grouping directory (the
			// session uuid) — descend one more level under the SAME group label.
			if extraDepth > 0 {
				sub, sn, su := collectNestedSkills(real, home, group, extraDepth-1, env)
				out = append(out, sub...)
				notes = append(notes, sn...)
				unresolved = append(unresolved, su...)
			}
			continue
		}
		out = append(out, artifact(model.KindSkill, group+"/"+e.Name(), real, TreeHash(real, real)))
		env.Skills++
	}
	return out, notes, unresolved
}

func collectMCP(home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	return mcpServersFrom(filepath.Join(home, ".claude.json"), "", env)
}

// mcpServersFrom reads one MCP config file and yields an artifact per declared server. Shared by
// the user-level config (~/.claude.json) and the project-level one (<project>/.mcp.json), which
// declare servers identically and were previously only half read.
//
// Server names are SORTED. Ranging over the decoded map directly made artifact order vary between
// runs of an unchanged environment: harmless for the score, which averages, but it churned every
// report diff and would have made any content-addressed handle for a server unstable.
//
// The artifact Name is the BARE server name, never decorated with a scope suffix. Name is not just
// a label: detect and the judge use it as the key to look the server up inside the JSON
// (`jsonStrings(a.Path, "mcpServers", a.Name)`). A decorated name misses, yields zero units, and the
// artifact scores a clean 100 — so labelling a project-level server " (project)" made the newly
// collected servers strictly worse than not collecting them: unscanned, unflagged, and averaged
// into `overall` as perfect. The two scopes stay distinguishable by Path.
func mcpServersFrom(path, nameSuffix string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.Finding{ioNote(path, err)}
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return []model.ArtifactReport{withParseError(model.KindMCP, "mcpServers"+nameSuffix, path)}, nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []model.ArtifactReport
	for _, name := range names {
		out = append(out, artifact(model.KindMCP, name+nameSuffix, path, ""))
		env.MCPServers++
	}
	return out, nil
}

// settingsScopes are the settings files read per root, in precedence order. Project-level
// scope is covered by pointing --root at a project's .claude (spec §4 B4). The Name suffix
// keeps the two scopes as DISTINCT report groups; permcheck derives Scope from the filename.
// SettingsEnvName is the artifact name for a settings file's `env` block. It is a
// KindPermission artifact — the block configures the agent's process, which is a permission
// decision — but it is NOT a permissions list, so the permission auditor must not be run on it a
// second time; cmd/aguard.analyze and detect key on this name to tell the two apart.
const SettingsEnvName = "settings env"

var settingsScopes = []struct{ file, nameSuffix string }{
	{"settings.json", ""},
	{"settings.local.json", " (local)"},
}

// ScopeFor labels which settings file a finding came from (spec §4 B4). Defined here,
// next to settingsScopes, so the collector and the permission auditor can never disagree
// about what a scope name means.
func ScopeFor(path string) string {
	if filepath.Base(path) == "settings.local.json" {
		return "local"
	}
	return "base"
}

func collectSettings(root string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	var out []model.ArtifactReport
	var notes []model.Finding
	for _, sc := range settingsScopes {
		path := filepath.Join(root, sc.file)
		b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			notes = append(notes, ioNote(path, err))
			continue
		}
		var doc struct {
			Hooks       map[string]json.RawMessage `json:"hooks"`
			Permissions struct {
				Allow []string `json:"allow"`
				Deny  []string `json:"deny"`
			} `json:"permissions"`
			// Env used to be dropped here entirely, so ANTHROPIC_BASE_URL, NODE_OPTIONS and a
			// plaintext key in the block never reached a rule. Only its presence is
			// decided here; detect renders the block as KEY=VALUE lines.
			Env map[string]json.RawMessage `json:"env"`
		}
		if json.Unmarshal(b, &doc) != nil {
			out = append(out, withParseError(model.KindHook, sc.file, path))
			continue
		}
		// No OwnerRoot: a settings.json hook ships in no tree, so there is nothing to resolve
		// a relative reference against beyond the root and home candidates hookUnits already tries.
		hooks, hn := collectHooks(path, sc.nameSuffix, "", doc.Hooks, env)
		out = append(out, hooks...)
		notes = append(notes, hn...)
		if perms := len(doc.Permissions.Allow) + len(doc.Permissions.Deny); perms > 0 {
			out = append(out, artifact(model.KindPermission, "permissions"+sc.nameSuffix, path, ""))
			env.Permissions += perms
		}
		if len(doc.Env) > 0 {
			out = append(out, artifact(model.KindPermission, SettingsEnvName+sc.nameSuffix, path, ""))
		}
	}
	return out, notes
}

func collectInstruction(root, home string, known []model.ArtifactReport) ([]model.ArtifactReport, []model.Finding) {
	candidates := []struct{ path, name string }{
		{filepath.Join(root, "CLAUDE.md"), "CLAUDE.md"},
		{filepath.Join(root, "CLAUDE.local.md"), "CLAUDE.local.md"},
		{filepath.Join(home, "CLAUDE.md"), "CLAUDE.md (project)"},
		{filepath.Join(home, "CLAUDE.local.md"), "CLAUDE.local.md (project)"},
	}
	var out []model.ArtifactReport
	for _, c := range candidates {
		fi, err := os.Stat(c.path)
		if err != nil || fi.IsDir() || !withinDir(home, c.path) {
			continue
		}
		out = append(out, artifact(model.KindInstruction, c.name, c.path, FileHash(c.path)))
	}
	out, imported, notes := expandImports(home, out, known)
	return append(out, imported...), notes
}

// withParseError returns an artifact carrying a single parse_error finding (spec §4:
// corrupt config is surfaced, not silently skipped).
func withParseError(kind model.ArtifactKind, name, path string) model.ArtifactReport {
	a := artifact(kind, name, path, "")
	a.Findings = append(a.Findings, model.Finding{
		RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
		Title:    "Parse failed, artifact not fully covered",
		Why:      "Config file is corrupt or malformed; its content checks were skipped (partial).",
		Evidence: []model.Evidence{{File: path, Line: 0, Snippet: "parse error"}},
	})
	return a
}
