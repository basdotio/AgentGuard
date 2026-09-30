// SPDX-License-Identifier: MIT
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
)

// ResolveSkill turns the name in a Skill tool call into the directory to audit.
//
// The hook payload carries only `{"skill": "<name>"}` — Claude Code does not hand over the
// resolved path — so the gate has to redo the lookup. That is the weak joint in this design
// and it is stated rather than hidden: a name this function cannot place is reported as
// UNAUDITED (GATE-000), never treated as clean.
//
// Search order, most specific first:
//
//	plugin:skill   → <installPath>/skills/<skill>  (bundle names come from the same manifest
//	                 parser collect uses, so the gate and the scanner cannot disagree about
//	                 where a plugin lives)
//	dir:skill      → <cwd>/<dir>/.claude/skills/<skill>  (a directory-scoped project skill)
//	name           → <cwd>/.claude/skills/<name>, then <root>/skills/<name>, then any
//	                 installed plugin's skills/<name>
//
// The project root is consulted BEFORE the user root for the same reason Claude Code
// prefers it: when both define a skill of one name, the project copy is the one that loads,
// and auditing the other one would produce a correct verdict about the wrong bytes.
func ResolveSkill(root, home, cwd, name string) (string, error) {
	if err := validSkillName(name); err != nil {
		return "", err
	}
	scope, leaf := "", name
	if i := strings.IndexByte(name, ':'); i > 0 {
		scope, leaf = name[:i], name[i+1:]
	}
	if scope != "" {
		if err := validSkillName(leaf); err != nil {
			return "", err
		}
		if dir, ok := collect.PluginPaths(root, home)[scope]; ok {
			if p, ok := existingDir(filepath.Join(dir, "skills", leaf)); ok {
				return p, nil
			}
		}
		if cwd != "" {
			if p, ok := existingDir(filepath.Join(cwd, filepath.FromSlash(scope), ".claude", "skills", leaf)); ok {
				return p, nil
			}
		}
		return "", fmt.Errorf("scoped skill %q: no plugin bundle or project directory %q provides it", name, scope)
	}

	var candidates []string
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, ".claude", "skills", leaf))
	}
	candidates = append(candidates, filepath.Join(root, "skills", leaf))
	for _, p := range candidates {
		if p, ok := existingDir(p); ok {
			return p, nil
		}
	}
	// A bare name can still be a plugin skill when it is unambiguous. Ambiguity is an ERROR
	// rather than a pick: auditing one of two same-named skills and approving its hash would
	// record an approval for content that may not be what loads.
	var hits []string
	for _, dir := range collect.PluginPaths(root, home) {
		if p, ok := existingDir(filepath.Join(dir, "skills", leaf)); ok {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("skill %q not found under the project, %s or any installed plugin", name, root)
	case 1:
		return hits[0], nil
	default:
		return "", fmt.Errorf("skill %q is provided by %d installed plugins; cannot tell which one loads", name, len(hits))
	}
}

// validSkillName rejects anything that is not a plain identifier segment.
//
// The name arrives from a tool call, i.e. from the model, i.e. ultimately from text an
// artifact could have influenced. Path separators and `..` are refused so a crafted name
// cannot steer the gate at a directory outside the skill namespaces — and, worse, get that
// directory's hash written into the approvals store as though a skill had been audited.
func validSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("empty skill name")
	}
	if len(name) > 256 {
		return fmt.Errorf("skill name is implausibly long (%d bytes)", len(name))
	}
	for _, seg := range strings.Split(name, ":") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("skill name %q has an empty or relative segment", name)
		}
		if strings.ContainsAny(seg, `/\`) && !strings.Contains(name, ":") {
			return fmt.Errorf("skill name %q contains a path separator", name)
		}
		if strings.Contains(seg, "..") {
			return fmt.Errorf("skill name %q contains a parent-directory reference", name)
		}
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("skill name %q is an absolute path", name)
	}
	return nil
}

// existingDir reports whether p is a directory, returning it unchanged. Symlink containment
// is NOT re-implemented here: whatever this returns is handed to collect.CollectTarget,
// which resolves and bounds every path it reads (invariant #2). Adding a second boundary
// check here would be a second place to weaken.
func existingDir(p string) (string, bool) {
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return "", false
	}
	return p, true
}
