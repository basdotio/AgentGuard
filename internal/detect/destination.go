// SPDX-License-Identifier: MIT
package detect

import (
	"regexp"
	"strings"

	"github.com/basdotio/bas-guard/internal/model"
)

// Destination consistency. A file that reads DEEPL_API_KEY and sends it to api.deepl.com
// looks byte-for-byte like one that sends it to an attacker; the difference is whether the
// destination IS the credential's own service. When every readable, non-loopback destination in a
// file carries the service token of a named credential the file reads, EXFIL-001 drops to the
// EXFIL-002 band — data left the machine, but for the place the credential exists to reach. This is
// deliberately conservative: it never downgrades a whole-environment dump (which names no service),
// a chain with any unreadable destination, or one whose destinations do not all match. Measured on
// the corpus: 15 of 74 benign EXFIL-001 samples downgrade, 0 of 26 malicious.

// secretIdentRE finds identifier-shaped names with at least one separator, e.g. DEEPL_API_KEY,
// WALLET_PRIVATE_KEY, github.token. A name must also carry a secret word (secretNamePat) to count.
var secretIdentRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*(?:[_.][A-Za-z0-9]+)+`)

// genericToken are the parts of a credential name or a host that name no service — dropping them is
// what stops `API` in `api.deepl.com` from matching `X_API_KEY`, or `key` from matching everything.
var genericToken = map[string]bool{
	"api": true, "com": true, "net": true, "org": true, "io": true, "www": true, "co": true,
	"dev": true, "app": true, "cloud": true, "ai": true, "v1": true, "v2": true, "v3": true,
	"http": true, "https": true, "key": true, "token": true, "secret": true, "password": true,
	"passwd": true, "apikey": true, "credential": true, "credentials": true, "bearer": true,
	"oauth": true, "auth": true, "access": true, "private": true, "my": true, "the": true,
	"get": true, "id": true, "url": true, "base": true, "default": true, "pro": true, "env": true,
}

// serviceTokens splits a name on _ - . into lowercase parts, keeping the ones longer than two
// characters that are not generic. `DEEPL_API_KEY` -> {deepl}; `TOKEN` -> {} (nothing to vouch for).
func serviceTokens(name string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	}) {
		if len(p) > 2 && !genericToken[p] {
			out = append(out, p)
		}
	}
	return out
}

// credentialServiceTokens returns the service tokens of every NAMED secret on a line. A
// whole-environment dump (`dict(os.environ)`, `printenv`) contains no such name and yields nothing,
// which is why it can never license a downgrade.
func credentialServiceTokens(line string) []string {
	var out []string
	for _, name := range secretIdentRE.FindAllString(line, -1) {
		if secretNameRE.MatchString(name) {
			out = append(out, serviceTokens(name)...)
		}
	}
	return out
}

// hostedSuffix are second-level domains that hand out names to anyone — a Tailscale tailnet, a
// GitHub Pages site, a Vercel deployment — so the platform's own label can never vouch for the
// party: `evil.github.io` is evil's, not GitHub's, and a GITHUB_TOKEN sent there is exfiltration.
var hostedSuffix = map[string]bool{
	"ts.net": true, "github.io": true, "gitlab.io": true, "vercel.app": true, "netlify.app": true,
	"herokuapp.com": true, "pages.dev": true, "workers.dev": true, "onrender.com": true,
	"railway.app": true, "fly.dev": true, "azurewebsites.net": true, "cloudfunctions.net": true,
	"amazonaws.com": true, "run.app": true, "web.app": true, "firebaseapp.com": true,
	"ngrok.io": true, "ngrok-free.app": true, "trycloudflare.com": true, "repl.co": true,
}

// tldLike are labels that end a domain name. A service token sitting immediately before one of
// them anywhere but at the host's own end is a whole domain embedded in a longer host —
// `api.openai.com.evil.net` carries `openai`, and the first version of this check let it vouch.
var tldLike = map[string]bool{
	"com": true, "net": true, "org": true, "io": true, "ai": true, "app": true, "dev": true, "co": true,
	"me": true, "info": true, "biz": true, "cloud": true, "xyz": true, "tech": true, "sh": true, "ly": true,
	"gg": true, "tv": true, "cc": true, "us": true, "uk": true, "de": true, "fr": true, "cn": true, "jp": true,
	"ru": true, "in": true, "eu": true, "it": true, "nl": true, "se": true, "au": true, "ca": true, "br": true,
}

// hostMatchesService reports whether one of the host's labels names a known credential service
// and is in a position that can vouch for the host. Any label may — `purdue.brightspace.com` is
// Purdue's tenant and `otx.alienvault.com` is OTX's product, which is why the match is not
// restricted to the registrable label — except two positions that only look like the service:
// a label followed by a TLD-like label short of the end (an embedded domain), and, under a hosted
// platform's suffix, any label to the right of the tenant's (the platform itself). Matching is
// exact: a label that merely begins with the token (`openaievil.net`) is a lookalike, not the
// service. The RAW host is used by the caller, so a homoglyph domain (`dееpl.com` with Cyrillic е)
// does not fold into the ASCII service name and cannot buy a downgrade.
func hostMatchesService(host string, services map[string]bool) bool {
	labels := strings.Split(strings.ToLower(strings.TrimSpace(host)), ".")
	n := len(labels)
	hosted := n >= 3 && hostedSuffix[labels[n-2]+"."+labels[n-1]]
	for i, p := range labels {
		if !services[p] {
			continue
		}
		if hosted && i > n-3 {
			continue // the platform's own label
		}
		if i+1 < n-1 && tldLike[labels[i+1]] && !(i+2 == n-1 && len(labels[n-1]) == 2) {
			continue // a whole domain embedded in a longer host (`coles.com.au` is a ccSLD, not one)
		}
		return true
	}
	return false
}

// A destination written as a variable — `curl "$API/credits"`, fetch(`${BASE}/v1`), `requests.get(url)`,
// `API_BASE + "/images"` — used to make the whole file unverifiable, and with it every skill whose
// one base URL is set at the top and used below, which is how most real scripts are written. The
// rule now resolves ONE level inside the same file, the way SUP-006 resolves a shell variable: if
// the file assigns the variable an http(s) literal, that literal's host is the destination. A
// variable the file never assigns a URL (a parameter, a field of a response) stays unreadable,
// and a reference followed by a host-extending character (`${API}.evil.net`) is not resolved,
// because the literal would then vouch for a host that is not the one contacted (P-061).

// urlAssignRE: NAME = … https://host…, in shell (`API=${API:-https://x}`, `API="https://x"`),
// JavaScript (`const API = process.env.X || 'https://x'`), Python (`API_BASE = os.environ.get("X", "https://x")`)
// and YAML-ish (`url: https://x`). The URL may follow other text on the line, but not another
// assignment: a line assigning two names is skipped by the non-greedy class stopping at `=`.
var urlAssignRE = regexp.MustCompile(`(?i)(?:^|[\s;{(,])(?:export\s+|const\s+|let\s+|var\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*[:=]\s*(?:\$\{[A-Za-z_][A-Za-z0-9_]*:-)?[^\n]*?(https?://[^\s"'<>(),;}` + "`" + `]+)`)

// The variables a destination expression refers to. Three spellings: `$NAME` / `${NAME}` / `{NAME}`
// (shell, JS templates, Python f-strings), the first argument of a client call (`fetch(url,`,
// `requests.get(url)`), and a name concatenated with a string (`API_BASE + "/x"`). Group 2 of the
// first two captures the character AFTER the reference so the caller can refuse `.`.
var (
	urlVarDollarRE = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?([^A-Za-z0-9_]|$)`)
	urlVarBraceRE  = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}([^A-Za-z0-9_]|$)`)
	urlVarArgRE    = regexp.MustCompile(`(?i)(?:fetch|get|post|put|patch|urlopen|Request|request)\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*[,)+]`)
	urlVarConcatRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\+\s*["'` + "`" + `]/`)
)

// urlAssignments returns NAME -> host for every same-file URL assignment. Loopback hosts are
// kept (so a `BASE=http://localhost:8000` resolves on-box) and the caller screens them.
func urlAssignments(text string) map[string]string {
	var out map[string]string
	for _, line := range strings.Split(text, "\n") {
		for _, m := range urlAssignRE.FindAllStringSubmatch(line, -1) {
			h := urlHost(m[2])
			if h == "?" {
				continue
			}
			if out == nil {
				out = map[string]string{}
			}
			if _, dup := out[m[1]]; !dup {
				out[m[1]] = h
			}
		}
	}
	return out
}

// resolvedHosts returns the hosts an unreadable destination line resolves to through the file's
// assignments, or nothing. A `$NAME`/`{NAME}` reference followed by `.` is not resolved.
func resolvedHosts(line string, assigns map[string]string) []string {
	if len(assigns) == 0 {
		return nil
	}
	var out []string
	add := func(name, next string) {
		if next == "." {
			return
		}
		if h, ok := assigns[name]; ok {
			out = append(out, h)
		}
	}
	for _, m := range urlVarDollarRE.FindAllStringSubmatch(line, -1) {
		add(m[1], m[2])
	}
	for _, m := range urlVarBraceRE.FindAllStringSubmatch(line, -1) {
		add(m[1], m[2])
	}
	for _, m := range urlVarArgRE.FindAllStringSubmatch(line, -1) {
		add(m[1], "")
	}
	for _, m := range urlVarConcatRE.FindAllStringSubmatch(line, -1) {
		add(m[1], "")
	}
	return out
}

// CredentialGoesHome reports whether a file that reads a NAMED credential sends it only where that
// credential belongs: every non-loopback destination the file names (after one level of same-file
// resolution) carries the service token of a credential the file reads, or every destination is
// loopback. It is the judge's question on the server pass (P-062): a tool that reads
// WEATHER_API_KEY to call api.weather.com has not read a credential in the sense `credential-read`
// means, and the model cannot tell that from the excerpt it is shown. The answer is computed over
// the FULL file, by the same chain EXFIL-001 runs, so the two passes cannot disagree about where a
// key went. A file that reads no named credential, or names no destination, answers false — the
// absence of a chain is not a verdict about one.
func CredentialGoesHome(path, text string) bool {
	comments := commentOnlyLines(langForFile(path), text)
	c := chain{literalIsEgress: true, assigns: urlAssignments(text)}
	for _, ll := range logicalLines(langForFile(path), text, comments) {
		if ll.comment {
			continue
		}
		c.observe(ll.raw, ll.norm, func() model.Evidence { return model.Evidence{} })
	}
	if !c.cred || !c.net || len(c.credService) == 0 {
		return false
	}
	if !c.netOffBox {
		return true
	}
	return c.destConsistent()
}

// FileText is one file of an artifact, by its path relative to the artifact root.
type FileText struct{ Path, Text string }

// CredentialGoesHomeAcross is CredentialGoesHome over a whole artifact: the credentials named
// anywhere in it against the destinations named anywhere in it. A server reads its key in
// config.py and spends it in client.py, so the per-file answer is false for both files while the
// server as a whole plainly talks only to its own service. The aggregate keeps every guard of the
// per-file chain: one unreadable destination in any file, one destination that matches no named
// credential, or no named credential at all, and the answer is false.
func CredentialGoesHomeAcross(files []FileText) bool {
	services := map[string]bool{}
	var hosts []string
	cred, net, offBox, unreadable := false, false, false, false
	for _, f := range files {
		comments := commentOnlyLines(langForFile(f.Path), f.Text)
		c := chain{literalIsEgress: true, assigns: urlAssignments(f.Text)}
		for _, ll := range logicalLines(langForFile(f.Path), f.Text, comments) {
			if ll.comment {
				continue
			}
			c.observe(ll.raw, ll.norm, func() model.Evidence { return model.Evidence{} })
		}
		cred, net = cred || c.cred, net || c.net
		offBox, unreadable = offBox || c.netOffBox, unreadable || c.destUnreadable
		for k := range c.credService {
			services[k] = true
		}
		hosts = append(hosts, c.destHosts...)
	}
	if !cred || !net || len(services) == 0 || unreadable {
		return false
	}
	if !offBox {
		return true
	}
	if len(hosts) == 0 {
		return false
	}
	for _, h := range hosts {
		if !hostMatchesService(h, services) {
			return false
		}
	}
	return true
}
