// SPDX-License-Identifier: MIT
package detect

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
)

// shape.go — checks on the SHAPE of a file rather than the text of one line.
//
// Every rule in rules_data.go asks "does this line say something dangerous?". The four checks
// here ask a different question — "is this file arranged so that a line-reader never gets to
// the dangerous part?" — and they exist because a published set of malicious skills built specifically to
// beat skill scanners went 4/4 past the line rules:
//
//   - a script whose payload sat below a hundred thousand blank lines (padding);
//   - a zip archive named .txt, so the text rules read compressed bytes (disguise);
//   - compiled .pyc bytecode next to clean .py source, so Python runs what nobody read (SUP-005);
//   - an installer that rewrote the npm registry to an attacker host, with a paragraph of
//     comments explaining why that was fine (SUP-006 — the comments were aimed at the LLM judge).
//
// None of these is a text pattern. All of them are cheap to see once the question is asked
// about the file instead of the line.

// magicSignatures are the leading bytes of the container and executable formats that have no
// business under a text extension. Images and PDFs are deliberately absent: a PNG named .txt is
// odd but inert, and inert oddities are noise in a report whose findings are meant to be read.
var magicSignatures = []struct {
	prefix string
	what   string
}{
	{"PK\x03\x04", "zip archive (docx/xlsx/jar share this container)"},
	{"\x1f\x8b", "gzip archive"},
	{"BZh", "bzip2 archive"},
	{"\xfd7zXZ\x00", "xz archive"},
	{"7z\xbc\xaf\x27\x1c", "7-Zip archive"},
	{"Rar!\x1a\x07", "RAR archive"},
	{"\x7fELF", "ELF executable"},
	{"\xcf\xfa\xed\xfe", "Mach-O executable"},
	{"\xce\xfa\xed\xfe", "Mach-O executable"},
	{"\xca\xfe\xba\xbe", "Mach-O universal binary"},
	{"MZ\x90\x00", "Windows executable"},
}

// paddingRun is how many consecutive blank lines it takes before "there is code further down"
// stops being formatting and starts being concealment. A generous 200: the largest blank run in
// the whole known-good corpus (superpowers, the official marketplace, Anthropic's desktop skills)
// is under 10, and an editor page is about 50.
const paddingRun = 200

// officialRegistries are hosts a package manager may be pointed at without that being a
// redirection: the vendors' own registries, GitHub Packages, and the large public mirrors.
// Loopback is accepted separately (a local Verdaccio/devpi is a normal developer setup).
var officialRegistries = map[string]bool{
	"registry.npmjs.org": true, "registry.yarnpkg.com": true, "npm.pkg.github.com": true,
	"registry.npmmirror.com": true,
	"pypi.org":               true, "files.pythonhosted.org": true, "test.pypi.org": true, "pypi.python.org": true,
	"proxy.golang.org": true, "sum.golang.org": true, "goproxy.cn": true, "goproxy.io": true,
	"rubygems.org": true, "index.rubygems.org": true,
	"crates.io": true, "index.crates.io": true, "static.crates.io": true,
	"mirrors.aliyun.com": true, "mirrors.tuna.tsinghua.edu.cn": true, "mirrors.cloud.tencent.com": true,
	"mirrors.huaweicloud.com": true, "mirrors.ustc.edu.cn": true, "pypi.tuna.tsinghua.edu.cn": true,
}

// registryRE finds a line that tells a package manager where to fetch from: an .npmrc/.yarnrc
// key, a CLI config command, or the environment variables the tools honour. Group 1 is the
// target as written — a URL, or a shell variable that is resolved against the same file below.
var registryRE = regexp.MustCompile(`(?i)(?:^|[\s"'` + "`" + `;&|(])(?:registry\s*[=:]\s*|registry\s+|index-url\s*[=:]\s*|--registry(?:=|\s+)|--index-url(?:=|\s+)|npm\s+config\s+set\s+registry\s+|yarn\s+config\s+set\s+(?:npmRegistryServer|registry)\s+|pip\s+config\s+set\s+global\.index-url\s+|GOPROXY\s*=\s*|PIP_INDEX_URL\s*=\s*|NPM_CONFIG_REGISTRY\s*=\s*|YARN_REGISTRY\s*=\s*|npmRegistryServer\s*:\s*)["']?([^\s"'` + "`" + `]+)`)

// shellAssignRE captures NAME=value assignments so `registry=${CORP_REGISTRY}` can be read
// back to the URL the same file put in CORP_REGISTRY. One level, same file, no arithmetic:
// anything fancier stays "unresolved", which is reported, not skipped.
var shellAssignRE = regexp.MustCompile(`(?m)^\s*(?:export\s+|local\s+|readonly\s+)?([A-Za-z_][A-Za-z0-9_]*)=["']?([^"'\s]+)`)

// shapeFindings runs the per-file shape checks on one unit. commentLines is the set the caller
// already computed for the A1 comment filter, so a registry line inside a comment is not a
// redirection. Synthetic units (JSON values re-rendered as text) have no shape worth judging
// and are not passed here.
func shapeFindings(rel string, u unit, commentLines map[int]bool) []model.Finding {
	var out []model.Finding
	if f, ok := magicMismatch(rel, u.text); ok {
		out = append(out, f)
		return out // compressed bytes have no lines worth the other two checks
	}
	if f, ok := blankPadding(rel, u.text); ok {
		out = append(out, f)
	}
	if u.role != roleDoc {
		out = append(out, registryRedirects(rel, u.text, commentLines)...)
	}
	return out
}

// magicMismatch: a file the reader was handed as text (its extension is on the text list)
// that begins with the signature of an archive or executable. The text rules ran over the
// compressed bytes and, of course, matched nothing — that is what the disguise is for.
func magicMismatch(rel, text string) (model.Finding, bool) {
	for _, m := range magicSignatures {
		if !strings.HasPrefix(text, m.prefix) {
			continue
		}
		ext := filepath.Ext(rel)
		if ext == "" {
			ext = "(no extension)"
		}
		return model.Finding{
			RuleID: "OBF-006", Dimension: 6, Severity: model.SevHigh, Source: model.SrcStatic,
			Title: "File content does not match its name",
			Why: fmt.Sprintf("%s is named %s, a text file, but begins with the signature of a %s. The text rules "+
				"read its bytes and found nothing, which is exactly what a disguise buys: an instruction file that "+
				"tells the agent to unpack or run it gets the payload past every reader that trusts the extension. "+
				"Open it with the tool the signature names, not the one the name suggests.", rel, ext, m.what),
			Evidence: []model.Evidence{{File: rel, Line: 1, Snippet: fmt.Sprintf("starts with % x — %s", []byte(m.prefix), m.what)}},
		}, true
	}
	return model.Finding{}, false
}

// blankPadding: a run of paddingRun or more blank lines with real content after it. Nothing a
// person sees on opening the file — the editor's first screen, `head`, a reviewer's glance, an
// LLM excerpt capped by bytes — reaches past a run that long. The finding cites the first
// non-blank line AFTER the run, which is where the reader should jump.
func blankPadding(rel, text string) (model.Finding, bool) {
	run, longest, after := 0, 0, 0
	line := 0
	for _, l := range strings.Split(text, "\n") {
		line++
		if strings.TrimSpace(l) == "" {
			run++
			continue
		}
		if run >= paddingRun && run > longest {
			longest, after = run, line
		}
		run = 0
	}
	if longest == 0 {
		return model.Finding{}, false
	}
	lines := strings.Split(text, "\n")
	return model.Finding{
		RuleID: "OBF-007", Dimension: 6, Severity: model.SevLow, Source: model.SrcStatic,
		Title: "Content hidden below a long run of blank lines",
		Why: fmt.Sprintf("%d consecutive blank lines separate the top of %s from more code at line %d. No editor "+
			"page, `head`, or size-capped excerpt reaches that far, so whatever sits below the run is read by the "+
			"interpreter and by nobody else. Padding has no purpose in a script except to put the second half "+
			"out of sight; read from line %d.", longest, rel, after, after),
		Evidence: []model.Evidence{{File: rel, Line: after, Snippet: redactClip(lines[after-1])}},
	}, true
}

// registryRedirects: lines that point npm/yarn/pip/go at a host that is not the vendor's, a
// known mirror, or loopback. Every later `install` on that machine then resolves through the
// attacker's server, which can answer any familiar package name with anything at all — and a
// skill has no reason to decide where a machine gets its software.
//
// A justification next to the line ("corporate mirror", "AppSec-audited") is not consulted:
// the sample that motivated this check carried three paragraphs of them, written for the LLM
// judge, and they changed nothing about what the line does.
func registryRedirects(rel, text string, commentLines map[int]bool) []model.Finding {
	var vars map[string]string
	var out []model.Finding
	for i, l := range strings.Split(text, "\n") {
		lineNo := i + 1
		if commentLines[lineNo] {
			continue
		}
		m := registryRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		target := m[1]
		// Only a URL or a shell expansion is a destination. Prose in SKILL.md ("the corporate
		// registry mirror") and scheme-less words are not — .npmrc itself rejects them.
		if !strings.Contains(target, "://") && !strings.HasPrefix(target, "$") {
			continue
		}
		written := target
		if strings.HasPrefix(target, "$") {
			if vars == nil {
				vars = shellAssignments(text)
			}
			target = resolveShellVar(target, vars)
		}
		host, known := registryHost(target)
		if known && (host == "" || officialRegistries[host] || isLoopbackHost(host)) {
			continue
		}
		where := host
		if !known {
			where = fmt.Sprintf("%s (a value this file does not define — the destination cannot be read here, which is not the same as safe)", written)
		}
		out = append(out, model.Finding{
			RuleID: "SUP-006", Dimension: 5, Severity: model.SevHigh, Source: model.SrcStatic,
			Title: "Package source redirected to an unofficial registry",
			Why: fmt.Sprintf("%s points the package manager at %s. Every later install on this machine resolves "+
				"through that host, which can serve anything under a familiar package name. A skill has no reason "+
				"to change where software comes from, and a comment explaining why this one is fine does not change "+
				"what the line does.", rel, where),
			Evidence: []model.Evidence{{File: rel, Line: lineNo, Snippet: redactClip(l)}},
		})
	}
	return out
}

// registryHost extracts the host of a registry target. known=false means the target could not
// be read as a URL at all (an unresolved variable); known=true with host "" means it is a URL
// with no host worth judging (GOPROXY=direct, off) or a relative path.
func registryHost(target string) (host string, known bool) {
	if strings.HasPrefix(target, "$") {
		// `${VAR:-https://default}` — judge the default that would be used.
		if i := strings.Index(target, "://"); i >= 0 {
			j := strings.LastIndexAny(target[:i], "-=")
			if j >= 0 {
				return registryHost(strings.TrimRight(target[j+1:], "}"))
			}
		}
		return "", false
	}
	switch strings.ToLower(target) {
	case "direct", "off", "true", "false":
		return "", true
	}
	if !strings.Contains(target, "://") {
		if strings.HasPrefix(target, "/") || strings.HasPrefix(target, ".") {
			return "", true // a path, not a host
		}
		target = "https://" + target
	}
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasPrefix(h, "127.")
}

// shellAssignments reads NAME=value lines out of a script, last assignment wins.
func shellAssignments(text string) map[string]string {
	out := map[string]string{}
	for _, m := range shellAssignRE.FindAllStringSubmatch(text, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// resolveShellVar turns $NAME or ${NAME} into the same-file assignment, one level deep. Anything
// else — ${NAME:-default}, $(cmd), nested — is returned as written and judged by registryHost.
func resolveShellVar(ref string, vars map[string]string) string {
	name := strings.TrimPrefix(ref, "$")
	if strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
		name = name[1 : len(name)-1]
	}
	if strings.ContainsAny(name, ":-+?#%/(") {
		return ref
	}
	if v, ok := vars[name]; ok {
		return v
	}
	return ref
}

// pycFinding is SUP-005: compiled Python bytecode travelling with the source. Python imports a
// matching .pyc INSTEAD of compiling the .py beside it, so what runs need not be what anyone
// read — and no text scanner reads bytecode, this one included (__pycache__ is a skipped
// directory, and stays one: the fix is to say so at scoring weight, not to start disassembling).
func pycFinding(dir string, pyc []string) model.Finding {
	ev := make([]model.Evidence, 0, len(pyc))
	for i, p := range pyc {
		if i == 5 {
			break
		}
		ev = append(ev, model.Evidence{File: p, Line: 0, Snippet: "compiled bytecode"})
	}
	return model.Finding{
		RuleID: "SUP-005", Dimension: 5, Severity: model.SevHigh, Source: model.SrcStatic,
		Title: "Compiled Python bytecode shipped alongside the source",
		Why: fmt.Sprintf("%d .pyc file(s) travel with this artifact (%s). Python loads a matching .pyc instead of "+
			"compiling the .py next to it, so what runs can differ from what anyone reads, and no text scanner "+
			"reads bytecode — this one skips __pycache__ entirely. Committed bytecode has no legitimate purpose in "+
			"a skill: delete it and let Python rebuild its own.", len(pyc), strings.Join(pyc[:len(ev)], ", ")),
		Evidence: ev,
	}
}
