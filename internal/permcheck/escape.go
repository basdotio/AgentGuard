// SPDX-License-Identifier: MIT
package permcheck

import (
	"regexp"
	"strings"
)

// An allowlist is only worth what its narrowest entry is worth. `Bash(bash -c *)` is
// obviously unrestricted execution (PERM-002 already says so) — but so is `Bash(git *)`,
// because git will run a command for you via `-c core.pager=…`. Many ordinary developer
// tools carry such a lever (GTFOBins is the long-standing public inventory of them), so an
// entry naming one of them with open-ended arguments hands over the same capability as
// `Bash(*)` while reading like a narrow, well-behaved grant.
//
// This file is that inventory, reduced to what a Claude Code allowlist realistically
// contains. Findings are Source=permission, i.e. deterministic: they score and gate like
// any other static finding (spec §7) — no LLM involved.

// escapeMech is one binary that can spawn a process the grant never named.
type escapeMech struct {
	Bin string // command name as it appears in a Bash(<bin> …) grant
	Via string // the mechanism, quoted in the finding so it says WHY, not just "trust us"
	// lever matches the argument forms through which THIS binary escapes. It decides
	// reachability, not severity: when a grant pins a subcommand (`Bash(git status:*)`)
	// the escape is normally out of reach, unless the pinned part is itself the lever
	// (`Bash(npm run *)` — npm run executes package-supplied scripts).
	lever *regexp.Regexp
	// positionFree marks a binary that accepts its lever AFTER other arguments, so pinning
	// the first one does not put the escape out of reach: `Bash(make test *)` still reaches
	// arbitrary execution through `make test -f /tmp/evil.mk`.
	//
	// This exists because the reachability rule above was generalised from git, and git is
	// the one case where it holds — `git log -c core.pager=x` is not a config override, it
	// is git parsing `core.pager=x` as a revision and failing. Everything with GNU-style
	// permuting option parsing behaves the opposite way, and for those a pinned subcommand
	// is a disguise rather than a restriction.
	//
	// MEASURED, not assumed, and measured with a payload that WRITES A MARKER FILE rather
	// than echoing to stdout — a stdout probe is wrong in both directions here. It reads
	// false-positive when the marker text comes back inside the tool's own error message
	// (`sed`, whose `-e` after a file operand is taken as a filename) or when output leaks
	// from a command that then failed (`tar`), and false-negative when the lever's stdout is
	// a protocol stream rather than the terminal (`ssh`/`rsync`, where ProxyCommand output
	// goes into the transport). Re-measure that way before adding an entry.
	positionFree bool
}

// escapeMechs is the lookup table. Every entry documents a way to run a command the grant
// never named; tools whose only lever is writing files or reading secrets are deliberately
// absent — those are other dimensions, and keeping this list to EXECUTION keeps it precise.
var escapeMechs = []escapeMech{
	{Bin: "git", Via: "-c core.pager=… / -c alias.x=!cmd / hook config", lever: regexp.MustCompile(`(?i)-c\s*(core\.pager|core\.editor|alias\.|diff\.external|sequence\.editor)`)},
	{Bin: "find", Via: "-exec / -execdir", lever: regexp.MustCompile(`(?i)-exec(dir)?\b`), positionFree: true},
	{Bin: "awk", Via: `system() and "cmd" | getline`, lever: regexp.MustCompile(`(?i)\bsystem\s*\(|\|\s*&?\s*getline`)},
	{Bin: "gawk", Via: `system() and "cmd" | getline`, lever: regexp.MustCompile(`(?i)\bsystem\s*\(|\|\s*&?\s*getline`)},
	{Bin: "sed", Via: "the e (execute) flag/command", lever: regexp.MustCompile(`(?i)/e(['"\s]|$)`)},
	{Bin: "tar", Via: "--checkpoint-action=exec / --to-command / -I", lever: regexp.MustCompile(`(?i)--checkpoint-action|--to-command|--use-compress-program|(^|\s)-I\b`)},
	{Bin: "rsync", Via: "-e / --rsh (a remote-shell command)", lever: regexp.MustCompile(`(?i)(^|\s)(-e|--rsh|--rsync-path)\b`), positionFree: true},
	{Bin: "ssh", Via: "-o ProxyCommand / LocalCommand", lever: regexp.MustCompile(`(?i)-o\s*(Proxy|Local|PermitLocal)Command`), positionFree: true},
	{Bin: "scp", Via: "-o ProxyCommand / -S <program>", lever: regexp.MustCompile(`(?i)-o\s*ProxyCommand|(^|\s)-S\b`)},
	{Bin: "docker", Via: "docker run/exec (any image, any entrypoint — commonly with the host mounted)", lever: regexp.MustCompile(`(?i)(^|\s)(run|exec|compose)\b`)},
	{Bin: "podman", Via: "podman run/exec (any image, any entrypoint)", lever: regexp.MustCompile(`(?i)(^|\s)(run|exec)\b`)},
	{Bin: "kubectl", Via: "kubectl exec / run (a command inside a pod)", lever: regexp.MustCompile(`(?i)(^|\s)(exec|run)\b`)},
	{Bin: "npm", Via: "package lifecycle scripts (install and run execute package-supplied shell)", lever: regexp.MustCompile(`(?i)(^|\s)(run|run-script|exec|install|ci|i)\b`)},
	{Bin: "yarn", Via: "package lifecycle scripts", lever: regexp.MustCompile(`(?i)(^|\s)(run|add|install|dlx)\b`)},
	{Bin: "pnpm", Via: "package lifecycle scripts", lever: regexp.MustCompile(`(?i)(^|\s)(run|add|install|dlx|exec)\b`)},
	{Bin: "npx", Via: "fetching and running an arbitrary package binary"},
	{Bin: "make", Via: "-f <any makefile>, whose recipe lines are shell", lever: regexp.MustCompile(`(?i)(^|\s)(-f|--file|--makefile)\b`), positionFree: true},
	{Bin: "xargs", Via: "running the command it is handed"},
	{Bin: "env", Via: "running the command it is handed"},
	{Bin: "nohup", Via: "running the command it is handed"},
	{Bin: "timeout", Via: "running the command it is handed"},
	{Bin: "watch", Via: "re-running the command it is handed"},
	{Bin: "vim", Via: ":!cmd (also -c ':!cmd')", lever: regexp.MustCompile(`(?i)(^|\s)[+-]c?\s*['"]?:?!`), positionFree: true},
	{Bin: "nvim", Via: ":!cmd (also -c ':!cmd')", lever: regexp.MustCompile(`(?i)(^|\s)[+-]c?\s*['"]?:?!`), positionFree: true}, // inferred from vim, not separately measured
	{Bin: "less", Via: "!cmd at the pager prompt"},
	{Bin: "more", Via: "!cmd at the pager prompt"},
	{Bin: "man", Via: "!cmd via the pager it spawns"},
	{Bin: "gh", Via: "gh extension / alias (runs local executables)", lever: regexp.MustCompile(`(?i)(^|\s)(extension|ext|alias)\b`)},
}

// bashGrant extracts the command text of a Bash(...) allow entry — only Bash grants can
// execute anything, so nothing else is in scope here.
var bashGrant = regexp.MustCompile(`(?i)^\s*Bash\((.*)\)\s*$`)

// escapes reports the escape mechanism an allow entry exposes, or nil if it exposes none.
//
// Precision rule — a grant only qualifies when the operator cannot see every argument that
// will run, AND the escape lever is inside the open part:
//
//	Bash(git *) / Bash(git:*)   → escapable: the whole argument list is the caller's
//	Bash(npm run *)             → escapable: the pinned part IS the lever
//	Bash(make test *)           → escapable: make takes -f after the target (positionFree)
//	Bash(git status:*)          → not flagged: -c must precede the subcommand, so it is out of reach
//	Bash(git status)            → not flagged: fully specified, nothing to smuggle in
//
// The last line is the reason a wildcard is still required for a positionFree binary too:
// with no open segment there is nowhere to put the lever, so `Bash(make test)` stays clean
// while `Bash(make test *)` does not.
//
// That last pair is what keeps this off a well-written allowlist.
func escapes(entry string) *escapeMech {
	m := bashGrant.FindStringSubmatch(entry)
	if m == nil {
		return nil
	}
	// Claude Code writes prefix grants as `Bash(git log:*)`; the trailing ":*" means "any
	// arguments from here on", so it is a wildcard like any other.
	cmd := strings.TrimSuffix(strings.TrimSpace(m[1]), ":*")
	if !strings.Contains(m[1], "*") {
		return nil // fully specified command — nothing to smuggle in
	}
	bin, args := splitCommand(strings.TrimSuffix(cmd, "*"))
	if bin == "" {
		return nil
	}
	for i, e := range escapeMechs {
		if e.Bin != bin {
			continue
		}
		// Open from the first argument on, the pinned arguments are themselves the lever, or
		// the binary takes its lever anywhere in the command line (positionFree), in which
		// case whatever is pinned is beside the point.
		if strings.TrimSpace(args) == "" || e.positionFree || (e.lever != nil && e.lever.MatchString(args)) {
			return &escapeMechs[i]
		}
		return nil
	}
	return nil
}

// splitCommand returns the invoked binary (basename, so /usr/bin/git == git) and the rest
// of the command line. A leading `env VAR=val` prefix is stepped over: it changes the
// environment, not which binary runs.
func splitCommand(cmd string) (bin, args string) {
	fields := strings.Fields(cmd)
	for len(fields) > 0 {
		head := strings.Trim(fields[0], `"'`)
		if strings.Contains(head, "=") || (head == "env" && len(fields) > 1 && strings.Contains(fields[1], "=")) {
			fields = fields[1:] // `env FOO=bar git …` → judge git, not env
			continue
		}
		return strings.ToLower(pathBase(head)), strings.Join(fields[1:], " ")
	}
	return "", ""
}

// pathBase strips a directory prefix. Permission entries are shell text (always
// slash-separated), not host paths, so this does not use path/filepath.
func pathBase(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
