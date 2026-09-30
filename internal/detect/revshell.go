// SPDX-License-Identifier: MIT
package detect

import (
	"regexp"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
)

// A reverse shell opens an outbound connection and binds an interpreter's standard streams to it,
// handing the machine to the far end. BD-003 knew three shell idioms at low; BD-004 raises the
// whole shape to high. The one-line shell form is a line rule (revShellLineRE, rules_data.go). The
// language-native form has no single line to match — a socket is opened here, its file descriptor
// duplicated onto 0/1/2 there — so it is a per-file co-occurrence check, this file. The distinction
// that keeps it off ordinary code is the HANDOFF: a plain client opens a socket and writes to it; a
// reverse shell attaches a shell's stdin/stdout/stderr TO it. Requiring the handoff is why a metrics
// reporter, a health check, or a socket beside an unrelated `git status` stays quiet.
const revShellWhy = "An interactive shell whose standard input, output and error are bound to a network socket — the machine is handed to the far end. Static sees the shape, not the run (advisory)."

var (
	// Python: a socket is opened, then a shell takes its standard fds — dup2 onto 0/1/2, pty.spawn,
	// or a subprocess whose std* are the socket's fileno.
	pySocketRE   = regexp.MustCompile(`socket\.(?:socket|create_connection)\s*\(`)
	pyDup2StdRE  = regexp.MustCompile(`os\.dup2\s*\([^,]*,\s*[0-2]\s*\)`)
	pyPtySpawnRE = regexp.MustCompile(`pty\.spawn\s*\(`)
	pySubStdRE   = regexp.MustCompile(`std(?:in|out|err)\s*=\s*[A-Za-z_]\w*\.fileno\s*\(`)

	// Node: a socket is opened, a shell is spawned, and the two are piped together.
	nodeConnRE    = regexp.MustCompile(`net\.(?:connect|createConnection|Socket)\b`)
	nodeSpawnShRE = regexp.MustCompile(`spawn\s*\(\s*['"][^'"]*(?:/bin/|(?:ba|z)?sh)\b`)
	nodePipeStdRE = regexp.MustCompile(`\.pipe\s*\(\s*\w+\.std(?:in|out|err)\b|\.std(?:in|out|err)\s*\.pipe\s*\(`)

	// Go: net.Dial, exec.Command, and the command's std streams assigned to the connection (the
	// triple assignment `cmd.Stdin, cmd.Stdout, cmd.Stderr = c, c, c` — a bare var, not os.Stdout).
	goDialRE    = regexp.MustCompile(`net\.Dial\b`)
	goExecRE    = regexp.MustCompile(`exec\.Command\s*\(`)
	goStdConnRE = regexp.MustCompile(`\.Std(?:in|out|err)\b[^\n=]*=\s*[A-Za-z_]\w*\s*,`)
)

// reverseShellStructural fires BD-004 when one file both opens a socket and hands an interpreter's
// standard streams to it. Only real script files reach here (roleScript, non-synthetic): a socket
// bound to a shell is a code behaviour, and an instruction file or a config value describing one is
// not running it. The evidence cites the handoff — the fd duplication, the pipe, the assignment —
// because that is the line that turns a client into a shell.
func reverseShellStructural(rel string, u unit) (model.Finding, bool) {
	text := u.text
	var handoff *regexp.Regexp
	switch {
	case pySocketRE.MatchString(text):
		for _, r := range []*regexp.Regexp{pyDup2StdRE, pyPtySpawnRE, pySubStdRE} {
			if r.MatchString(text) {
				handoff = r
			}
		}
	case nodeConnRE.MatchString(text) && nodeSpawnShRE.MatchString(text):
		if nodePipeStdRE.MatchString(text) {
			handoff = nodePipeStdRE
		}
	case goDialRE.MatchString(text) && goExecRE.MatchString(text):
		if goStdConnRE.MatchString(text) {
			handoff = goStdConnRE
		}
	}
	if handoff == nil {
		return model.Finding{}, false
	}
	loc := handoff.FindStringIndex(text)
	line := 1 + strings.Count(text[:loc[0]], "\n")
	snippet := lineAt(text, line)
	return model.Finding{
		RuleID: "BD-004", Dimension: 7, Severity: model.SevHigh, Source: model.SrcStatic, Advisory: true,
		Title: "Reverse shell", Why: revShellWhy,
		Evidence: []model.Evidence{{File: rel, Line: line, Snippet: redactClip(snippet)}},
	}, true
}

// lineAt returns the 1-indexed line of text, or "" if out of range.
func lineAt(text string, n int) string {
	lines := strings.Split(text, "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}
