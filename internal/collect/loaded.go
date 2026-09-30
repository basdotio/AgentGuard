// SPDX-License-Identifier: MIT
package collect

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/parse"
)

// This file collects the surfaces Claude Code loads WITHOUT being asked: rules, workflows, output
// styles and auto memory. Each enters context at session start (an output style enters the system
// prompt itself), so each carries instructions the agent will follow — exactly what this tool
// exists to read. None of them were collected before, which meant a payload parked in
// `rules/notes.md` took effect every single session while the scan reported a clean environment.
//
// Two shapes recur here and are worth naming once. Discovery is RECURSIVE, because the documented
// behaviour is recursive and a first-level-only walk is precisely the blind spot being closed. And
// dot-prefixed directories are NOT skipped: "hidden" is a display convention, not a loading rule,
// so skipping them would re-open the same hole under a different name.

// maxTreeDepth bounds recursive discovery. Deep enough for any real layout, shallow enough that a
// pathological or looping tree cannot stall a scan — and when it does bite, it says so (COV-000)
// rather than quietly returning a short list.
const maxTreeDepth = 8

// acceptMarkdown / acceptAny decide which files in a tree are artifacts. Markdown-only where the
// documented format is markdown; anything-goes where the content is a script whose extension is
// not fixed (a workflow can be .js as easily as .md).
func acceptMarkdown(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

func acceptAny(string) bool { return true }

// bumpEnv keeps the per-kind counters in one place, so adding a kind cannot leave the overview
// line silently reporting zero of something the scan actually read.
func bumpEnv(env *model.EnvSummary, kind model.ArtifactKind) {
	switch kind {
	case model.KindSubagent:
		env.Subagents++
	case model.KindCommand:
		env.Commands++
	case model.KindRule:
		env.Rules++
	case model.KindWorkflow:
		env.Workflows++
	case model.KindOutputStyle:
		env.OutputStyles++
	case model.KindMemory:
		env.Memories++
	case model.KindQuarantined:
		env.Quarantined++
	}
}

// escapeNote reports entries dropped for resolving outside root. Silence would contradict this
// package's own contract: a symlinked rules/ directory (a standard dotfiles layout) collected ZERO
// rules and said nothing, which reads exactly like "you have no rules".
func escapeNote(base string, n int) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevMedium, Source: model.SrcStatic,
		Title: "Entries resolve outside the scanned root, not read",
		Why: "Files or directories here resolve outside --root (commonly a symlinked dotfiles layout). " +
			"They are loaded by Claude Code but were NOT scanned; point --root at the real location to cover them.",
		Evidence: []model.Evidence{{File: base, Line: 0, Snippet: itoa(n) + " entr(ies) outside root"}},
	}
}

// depthNote reports a tree that was cut off, so a bounded walk cannot pass for a complete one.
func depthNote(dir string) model.Finding {
	return model.Finding{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Directory nesting exceeded the scan depth limit (partial)",
		Why:   "Discovery stops at a bounded depth to keep a pathological tree from stalling a scan; anything deeper was NOT read.",
		Evidence: []model.Evidence{{File: dir, Line: 0,
			Snippet: "nesting deeper than " + itoa(maxTreeDepth) + " levels"}},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// collectTree enumerates every accepted file under <root>/<sub>, recursively.
//
// An artifact's Name is its slash-joined path relative to <sub> minus the extension, which is also
// how Claude Code namespaces a nested entry (`commands/foo/bar.md` → `/foo:bar`), so a finding
// names the thing the operator would type. Containment is enforced per file against root: a symlink
// pointing out of the tree is skipped rather than followed, matching the skill collector.
func collectTree(root, sub string, kind model.ArtifactKind, accept func(string) bool,
	env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {

	base := filepath.Join(root, sub)
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.Finding{ioNote(base, err)}
	}

	var out []model.ArtifactReport
	var notes []model.Finding
	// visited keys on the RESOLVED path. Without it a symlink pointing at an ancestor inside root
	// passes the containment check on every pass and the walk fans out: measured at 9 / 511 / 9841
	// artifacts for one / two / three such links, and hours for five. Bounding the depth turned an
	// infinite loop into an exponential one; this is what actually stops it — and it also keeps a
	// single file reachable by two paths from being collected twice.
	visited := map[string]bool{}
	truncated, escaped := false, 0

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxTreeDepth {
			truncated = true
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			notes = append(notes, ioNote(dir, err))
			return
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			// Resolve rather than trusting the dirent: a symlink reports as neither dir nor regular
			// file, and treating a symlinked subdirectory as a file would drop its whole contents —
			// the class of miss this collector exists to prevent.
			fi, serr := os.Stat(p)
			if serr != nil {
				continue // dangling symlink or vanished mid-walk; nothing to read
			}
			real := p
			if r, rerr := filepath.EvalSymlinks(p); rerr == nil {
				real = r
			}
			if fi.IsDir() {
				// Version-control metadata, vendored trees and build output are not authored
				// content. The exclusion uses the SAME list as the skill hash and the tree scan
				// (skip.go), so the three walks cannot disagree. Without it an `agents/` folder that
				// happens to be a git repo yielded 27 "subagents" and a high-severity finding from
				// git's own `.git/hooks/*.sample`. This does not contradict walking dot-directories:
				// `.stash/` is the operator's content, `.git/` is not.
				if ExcludedFromScan(e.Name()) || visited[real] {
					continue
				}
				if !withinDir(root, p) {
					escaped++
					continue
				}
				visited[real] = true
				walk(p, depth+1)
				continue
			}
			if !accept(e.Name()) {
				continue
			}
			if !withinDir(root, p) {
				escaped++
				continue
			}
			if visited[real] {
				continue
			}
			visited[real] = true
			rel, rerr := filepath.Rel(base, p)
			if rerr != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			out = append(out, artifact(kind, trimArtifactExt(rel), p, FileHash(p)))
			bumpEnv(env, kind)
		}
	}
	walk(base, 0)

	// One note per tree, not per directory: a fanned-out symlink loop produced 19683 identical depth
	// notes, which buries the report they were meant to inform.
	if truncated {
		notes = append(notes, depthNote(base))
	}
	if escaped > 0 {
		notes = append(notes, escapeNote(base, escaped))
	}
	return out, notes
}

// trimArtifactExt turns a relative path into an artifact name. A dotfile is all "extension" to
// filepath.Ext (".DS_Store"), which trimmed to an EMPTY name — an artifact nothing can refer to.
func trimArtifactExt(rel string) string {
	ext := filepath.Ext(rel)
	if name := strings.TrimSuffix(rel, ext); name != "" && !strings.HasSuffix(name, "/") {
		return name
	}
	return rel
}

// collectRules enumerates rules/**/*.md — user-level when root is ~/.claude, project-level when
// root is a project's .claude, the same way settings scopes are handled.
//
// A rule WITHOUT `paths:` frontmatter loads every session at the priority of .claude/CLAUDE.md; one
// with `paths:` loads only when Claude touches a matching file. Both are collected, because both
// are instructions the agent follows, but the name carries the distinction: "loads every session"
// and "loads when you open a matching file" deserve different reactions from a reader.
func collectRules(root string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	out, notes := collectTree(root, "rules", model.KindRule, acceptMarkdown, env)
	for i := range out {
		if parse.PathScoped(out[i].Path) {
			out[i].Name += " (path-scoped)"
		}
	}
	return out, notes
}

// collectMemory enumerates the auto-memory markdown Claude maintains for itself: the main
// conversation's memory under projects/<project>/memory/, and each subagent's under
// agent-memory/<agent>/.
//
// MEMORY.md is loaded at the start of every session (its first 200 lines / 25KB); topic files load
// on demand. Both are collected: an instruction that only loads when a related task comes up is
// still an instruction.
//
// Deliberately markdown-only. The same projects/ tree holds <session>.jsonl transcripts, which are
// large and — per Claude Code's own documentation — contain whatever a tool read, credentials
// included. Collecting those would pull secrets into a scan whose entire redaction contract is
// built on not needing to. They are out of scope here by construction, not by filter.
func collectMemory(root string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	var out []model.ArtifactReport
	var notes []model.Finding

	// projects/<project>/memory/*.md
	projects := filepath.Join(root, "projects")
	if ents, err := os.ReadDir(projects); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join("projects", e.Name(), "memory")
			mem, mn := collectTree(root, sub, model.KindMemory, acceptMarkdown, env)
			for i := range mem {
				mem[i].Name = "projects/" + e.Name() + "/" + mem[i].Name
			}
			out = append(out, mem...)
			notes = append(notes, mn...)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		notes = append(notes, ioNote(projects, err))
	}

	// agent-memory/<agent>/*.md
	agentMem := filepath.Join(root, "agent-memory")
	if ents, err := os.ReadDir(agentMem); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			mem, mn := collectTree(root, filepath.Join("agent-memory", e.Name()), model.KindMemory, acceptMarkdown, env)
			for i := range mem {
				mem[i].Name = "agent-memory/" + e.Name() + "/" + mem[i].Name
			}
			out = append(out, mem...)
			notes = append(notes, mn...)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		notes = append(notes, ioNote(agentMem, err))
	}

	return out, notes
}

// managedPolicyPaths are the OS locations an organisation deploys instructions to. Indexed by GOOS
// and declared as a var so a test can point them somewhere harmless — reading real machine paths
// during a test would make the suite depend on how the host is administered.
var managedPolicyPaths = defaultManagedPolicyPaths

// defaultManagedPolicyPaths is the real table, kept separate so a test that overrides the var above
// can restore it exactly rather than reconstructing it.
var defaultManagedPolicyPaths = map[string][]string{
	"darwin": {
		"/Library/Application Support/ClaudeCode/CLAUDE.md",
		"/Library/Application Support/ClaudeCode/managed-settings.json",
	},
	"linux": {
		"/etc/claude-code/CLAUDE.md",
		"/etc/claude-code/managed-settings.json",
	},
	"windows": {
		`C:\Program Files\ClaudeCode\CLAUDE.md`,
		`C:\Program Files\ClaudeCode\managed-settings.json`,
	},
}

// ManagedPolicyNotes DISCLOSES an organisation-deployed CLAUDE.md (or a managed-settings.json
// carrying the `claudeMd` key) without reading it.
//
// The disclosure rather than the scan is a deliberate trade, and the reason is the score. A managed
// policy file loads before everything else and cannot be excluded by the operator, so it genuinely
// belongs to the environment — but it lives at an absolute OS path that has nothing to do with
// --root. Folding its content into findings would make `overall` depend on how the machine is
// administered, and `overall` is the one number a third party must be able to recompute offline
// from the same artifacts. Reproducibility wins; the operator gets told where to look instead, and
// can audit it directly with `aguard check <path>`.
//
// Called by the CLI rather than by CollectAll. Stat-ing absolute OS paths during collection made
// every test that builds a root depend on how the HOST is administered: creating
// /etc/claude-code/CLAUDE.md failed two unrelated collector tests that assert "no notes".
func ManagedPolicyNotes() []model.Finding {
	var out []model.Finding
	for _, p := range managedPolicyPaths[runtime.GOOS] {
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			continue
		}
		out = append(out, model.Finding{
			RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
			Title: "Managed policy instructions present, not scanned",
			Why: "An organisation-managed instruction file loads before every other instruction and cannot be " +
				"excluded, but it sits outside --root; scanning it would make the reproducible score depend on " +
				"machine administration. Audit it directly with `aguard check`.",
			Evidence: []model.Evidence{{File: p, Line: 0, Snippet: "managed policy file exists"}},
		})
	}
	return out
}

// TrashDir is the directory `clean --apply` moves things into, relative to root. Named here rather
// than in the clean package so the collector and the mover cannot disagree about where it is.
//
// That this address is safe is MEASURED, not assumed. Against Claude Code 2.1.229, with an isolated
// CLAUDE_CONFIG_DIR and the outgoing request body captured, none of a quarantined skill, rule,
// subagent, output style, CLAUDE.md or MEMORY.md under <config-root>/.aguard-trash was opened or
// reached the system prompt. See docs/internals/measurement-startup-loading.md for the full matrix.
const TrashDir = ".aguard-trash"

// loadedParents are directory names Claude Code walks RECURSIVELY to find instruction surfaces.
//
// This list exists for one reason: the quarantine directory's ADDRESS. <root>/.aguard-trash is safe
// only because a config root is not itself a tree Claude Code recurses into. Point --root at
// ~/.claude/rules and the very same code parks the quarantined file at
// ~/.claude/rules/.aguard-trash/…, which rules/**/*.md discovery picks straight back up — the report
// says "quarantined" while the payload keeps loading every session. That is the worst failure this
// tool can have: not a miss, but a false assurance the operator acted on.
//
// A dot prefix does NOT save it. Measured on 2.1.229: rules/.aguard-trash/x.md,
// agents/.aguard-trash/x.md and commands/.aguard-trash/x.md were all read into the system prompt,
// and so was the dot-free rules/aguard-trash/x.md — the recursion neither stops at one level nor
// skips hidden directories. Only the file EXTENSION stopped it (x.md loaded, x.md.quarantined,
// x.markdown and extension-less x did not), which is why this is a hard refusal rather than a
// rename: refusing an address is verifiable, and a per-file rename scheme has a half-renamed
// directory as its failure mode.
var loadedParents = map[string]bool{
	"rules": true, "agents": true, "commands": true, "skills": true,
	"output-styles": true, "workflows": true, "plugins": true,
}

// QuarantineUnsafe returns the path segment that makes <root>/TrashDir a place Claude Code would
// keep loading from, or "" when the address is safe.
//
// The path is made ABSOLUTE and SYMLINK-RESOLVED before it is split, and segments are folded the way
// a case-insensitive or Win32 filesystem would fold them. Judging the path as written was bypassed
// three ways, two of them verified end-to-end with the payload re-collected as a live rule:
//
//	cd <root>/rules/inner && aguard clean --root . --apply    # Clean(".") == ".", no segment to match
//	ln -s <root>/rules/inner <root>/qroot; --root <root>/qroot # the symlink was never resolved
//	--root <root>/Rules                                        # same directory on macOS, different string
func QuarantineUnsafe(root string) string {
	p := root
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Clean(p)), "/") {
		if loadedParents[strings.ToLower(strings.TrimRight(seg, ". "))] {
			return seg
		}
	}
	return ""
}

// manifestFile is clean's own record inside the trash directory; bookkeeping, not content.
const manifestFile = "manifest.jsonl"

// collectQuarantine enumerates whatever `clean --apply` has moved into <root>/.aguard-trash.
//
// Quarantined content is COLLECTED AND SCORED. Leaving it out was measured: quarantining a
// known-malicious skill took an environment from 69/100 (Elevated) to a clean 100/100 while the file
// sat untouched in the config root, which made `clean --apply` the shortest path to turning a red
// `--fail-on high` green. A move is not a deletion; the score improves when the content is actually
// gone, which is a deliberate act the operator performs.
//
// Each top-level entry is one artifact, read as a whole tree the way a skill is, so a payload cannot
// hide in a subdirectory of something already flagged.
func collectQuarantine(root string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	dir := filepath.Join(root, TrashDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.Finding{ioNote(dir, err)}
	}
	var out []model.ArtifactReport
	var notes []model.Finding
	for _, e := range ents {
		if e.Name() == manifestFile || (!e.IsDir() && strings.HasPrefix(e.Name(), ".")) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		fi, serr := os.Stat(p)
		if serr != nil {
			continue
		}
		if !withinDir(root, p) {
			// Never silent. A quarantined entry that resolves out of root is the symlinked-trash case:
			// `clean --apply` moved content somewhere this scan cannot see, so it stopped being scored
			// and an environment went 50 → 100 with --fail-on high flipping 1 → 0. Dropping it with a
			// bare continue is what made that invisible.
			esc := model.Finding{
				RuleID: "COV-000", Dimension: 0, Severity: model.SevHigh, Source: model.SrcStatic,
				Title: "Quarantined content resolves outside the scanned root, not scored",
				Why: "An entry under " + TrashDir + " resolves outside --root, so it is NOT scanned and NOT " +
					"scored. Quarantine must never improve a score by moving content out of view; treat this " +
					"as content still present and audit it directly.",
				Evidence: []model.Evidence{{File: p, Line: 0, Snippet: "quarantined entry escapes root"}},
			}
			notes = append(notes, esc)
			continue
		}
		hash := FileHash(p)
		if fi.IsDir() {
			hash = TreeHash(p, p)
		}
		out = append(out, artifact(model.KindQuarantined, e.Name(), p, hash))
		env.Quarantined++
	}
	return out, notes
}
