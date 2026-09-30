// SPDX-License-Identifier: MIT
package collect

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// The root collectors are an ALLOWLIST of known layouts: skills/, agents/, commands/,
// settings*.json, plugins/, CLAUDE.md. Anything else under a root is read by nobody, and this
// file exists so that it is at least announced by somebody. Without it a root holding
//
//	~/.claude/install.sh          → curl … | bash ; rm -rf /
//	~/.claude/skills/x/run.sh     → curl … | bash   (no SKILL.md, so not a "skill")
//
// scanned to 100/100 with "✅ No risk findings" and exit code 0.
//
// A config root's unowned half is mostly not artifact content at all: on a real machine it is
// `sessions/`, `projects/`, `history.jsonl`, `backups/`, `file-history/`, `shell-snapshots/` —
// the user's own transcripts, config backups and shell state. Reading those into a report (or,
// with --llm, toward an endpoint) trades a blind spot for a worse leak, so the question is which
// parts to read. Neither a name list nor a shape test answers it: a hand-written list of
// runtime-state names is a promise to track someone else's directory layout forever, and every
// entry it misses is read when it must not be; "read anything containing an executable-looking
// file" fails because `shell-snapshots/` and `file-history/` are FULL of shell scripts — they are
// snapshots OF the user's code. What separates authored content from machine state is whether
// the agent has a LOAD PATH in:
//
//   - `skills/`, `agents/` and `commands/` are load namespaces. Content sitting there with the
//     expected manifest missing is still where the agent looks, so it is read (see collectSkills
//     and collectDir).
//   - A loose FILE at the top of a root is small, bounded and authored-looking; read it if it
//     looks like something an interpreter runs, or like instructions (`.md`). Claude Code's own
//     top-level entries are `.json`/`.jsonl` state, which this leaves alone.
//   - A DIRECTORY at the top of a root is never read. That is where the volume and the user's
//     data live, and — by the same argument `ExcludeFromHash` already rests on — an unreferenced
//     tree is inert: the agent reaches it only if something points at it, and the things that
//     can point (a hook command, a permission grant, a SKILL.md instruction) are followed
//     already. Announced by name, so the decision is visible.
//
// The disclosure is a NOTE, not an artifact, for a scoring reason: the environment score is an
// average, so turning every unowned entry into a 100-scoring artifact moved a real machine's
// headline UP (86 → 97) purely by diluting findings that were already there. And it is ONE
// finding per scan, for the reason coalesceGeneratedDirNotes exists: a disclosure repeated per
// entry becomes wallpaper, and wallpaper is worth what silence is worth.

// rootOwned are the top-level names the per-kind collectors already read. Listed here rather
// than inferred so that adding a collector without updating this list produces a duplicate
// artifact (visible, annoying) instead of a silent gap (invisible).
var rootOwned = map[string]bool{
	"skills": true, "agents": true, "commands": true, "plugins": true,
	"settings.json": true, "settings.local.json": true, "CLAUDE.md": true,
	// Added when this merged with the auto-loaded-surface collectors. Leaving them out produced
	// exactly the lie this note exists to prevent, in reverse: a scan that reported
	// "rules was not read" on the same run whose header said `Auto-loaded: rules=1`. An overclaimed
	// gap costs the operator's attention on nothing and teaches them to ignore the disclosure.
	"rules": true, "workflows": true, "output-styles": true,
	"projects": true, "agent-memory": true,
	// hooks/ is read INDIRECTLY: a hook command that points at a script has that script followed and
	// scanned (collect/hooks.go), so calling the directory "not read" describes a blind spot that is
	// not there. Found by building a representative environment for the README screenshot — the drift
	// test existed but its fixture had no hooks/, which is why a list maintained by hand needs a
	// fixture that mirrors a real root rather than the layouts one happens to think of.
	"hooks":     true,
	".mcp.json": true, ".claude.json": true, "CLAUDE.local.md": true,
	TrashDir: true,
}

// codeExts are extensions whose content an interpreter runs. Deliberately narrower than
// detect's textExts: this decides whether to READ part of the user's config root, so data and
// config formats (.json, .jsonl, .yaml, .txt) stay on the other side of the line — a root is
// full of them and they are the user's state, not authored artifact code.
var codeExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".py": true,
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".rb": true,
	".pl": true, ".ps1": true, ".psm1": true, ".command": true,
}

// isCodeOrDoc reports whether a top-level file is worth reading: something an interpreter runs,
// or instructions the agent might be told to follow. A file with no extension is judged by its
// shebang — an extensionless `bootstrap` is the oldest way to look like data and behave like code.
func isCodeOrDoc(path string, fi os.FileInfo) bool {
	if !fi.Mode().IsRegular() {
		return false // a symlink is handled by the boundary check, not here
	}
	ext := strings.ToLower(filepath.Ext(path))
	if codeExts[ext] || ext == ".md" {
		return true
	}
	if ext != "" {
		return false
	}
	return hasShebang(path)
}

// hasShebang reads the first two bytes only — cheap, and it never holds a file whose size it
// has not checked.
func hasShebang(path string) bool {
	f, err := safeio.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [2]byte
	if n, _ := f.Read(head[:]); n < 2 {
		return false
	}
	return head[0] == '#' && head[1] == '!'
}

// collectUnowned collects the top-level FILES of root that no other collector owns and that look
// like code or instructions, and returns one COV-000 naming everything left unread.
// aguardApprovalsFile mirrors gate.ApprovalsFile. It is a literal rather than an import
// because collect must not depend on the gate — the gate is built ON collect — and a
// one-word constant is a cheaper coupling than an inverted dependency. The gate's test
// asserts the two agree.
const aguardApprovalsFile = ".aguard-approvals.json"

// aguardSettingsBackup mirrors the suffix `aguard hook install` appends when it backs up
// settings.json. Same reasoning as above: reporting "I did not read the backup I just made"
// is noise, and it shows up on every scan of a machine where the gate is installed. Matched
// as a PREFIX: the gate keeps two slots (the original, and .prev for the last change).
const aguardSettingsBackup = "settings.json.aguard-bak"

func collectUnowned(root string) ([]model.ArtifactReport, []model.Finding) {
	ents, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.Finding{ioNote(root, err)}
	}

	var out []model.ArtifactReport
	var unread []string
	for _, e := range ents {
		name := e.Name()
		// The tool's own state files are not a gap in the user's environment: reporting
		// "I did not read my own baseline / approvals store" is noise that trains people to
		// skim past the note that matters.
		if rootOwned[name] || name == ".aguardignore" || name == aguardApprovalsFile || strings.HasPrefix(name, aguardSettingsBackup) {
			continue
		}
		p := filepath.Join(root, name)
		// Boundary first, as everywhere: resolve before reading, and a target that leaves the
		// root is not read at all (invariant #2). Announced, because a symlink out of a config
		// root is a more interesting fact than an ordinary unread file.
		if !withinDir(root, p) {
			unread = append(unread, name+" (symlink leaves the root)")
			continue
		}
		fi, serr := os.Stat(p)
		if serr != nil {
			unread = append(unread, name+" (unreadable)")
			continue
		}
		if fi.IsDir() || !isCodeOrDoc(p, fi) {
			unread = append(unread, name)
			continue
		}
		// Read the way `check <file>` reads one file: detect picks its rules by extension, and
		// an extension it does not know already produces its own COV-000.
		out = append(out, artifact(model.KindInstruction, name, p, FileHash(p)))
	}

	if len(unread) == 0 {
		return out, nil
	}
	ev := make([]model.Evidence, 0, len(unread))
	for _, n := range unread {
		// No snippet: Why already lists every name, and File repeats it. A label here would be
		// a third restatement (see the oversized note in detect.go).
		ev = append(ev, model.Evidence{File: filepath.Join(root, n), Line: 0})
	}
	return out, []model.Finding{{
		RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Unowned entries under the root were not read",
		// The names go in Why, not only in Evidence: the terminal renderer prints ONE evidence
		// line per note, so a list that lives only in Evidence is a list the reader never sees.
		// Same convention as the unknown-extension COV-000.
		Why: "Not read: " + strings.Join(unread, ", ") + ". No collector owns these: on a real " +
			"machine they are the user's own transcripts, config backups and caches, so reading them " +
			"into a report (or, with --llm, toward an endpoint) would trade a blind spot for a leak. " +
			"Top-level directories are left alone even when they contain scripts, because an " +
			"unreferenced tree is inert and anything that DOES point into one — a hook command, a " +
			"permission grant, a SKILL.md instruction — is followed and scanned already. The " +
			"residual gap is stated rather than assumed: a payload parked in one of these and " +
			"referenced by nothing is not scanned.",
		Evidence: ev,
	}}
}
