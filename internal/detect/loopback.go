// SPDX-License-Identifier: MIT
package detect

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// loopbackHost reports whether host is a literal loopback name or address. The names this
// tool accepts are the ones an operator can verify by reading the file: localhost, 127.0.0.1
// (and the rest of 127.0.0.0/8, via net.IP.IsLoopback), and ::1. Anything else — a name that
// happens to resolve to loopback, a dotted lookalike, an unparseable token — is NOT loopback.
// Fail-closed: the scan never opens a network connection to find out (spec §16.1).
func loopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackURL reports whether raw is an HTTP(S) URL whose host is loopback. Unparseable,
// schemeless, or hostless input is not loopback (fail-closed) — HOOK-003 uses this to decide
// whether posting the event payload stays on the machine.
func loopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	return loopbackHost(host)
}

// httpURLRE extracts http(s) and ws(s) URLs out of a line. Quotes, backticks, parens and the
// separators , ; are excluded so a call like requests.post('https://x/y', data=…) or a JS
// template literal `http://localhost:${port}/` yields the URL alone. IPv6 literals ([::1])
// are kept because [ ] are not excluded. WebSocket URLs are included because a sidecar that
// speaks ws://localhost is the same on-box conversation as one that speaks http://localhost.
var httpURLRE = regexp.MustCompile("(?i)(?:https?|wss?)://[^\\s\"'`<>(),;]+")

// urlAuthorityHostRE takes the host out of a URL's authority section by hand:
// scheme://[userinfo@]HOST[:port][/…]. It exists for URLs url.Parse rejects — see urlHost.
var urlAuthorityHostRE = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://(?:[^@/?#\s]*@)?(\[[^\]]*\]|[^:/?#\s]+)`)

// urlHost returns the host a URL literal names, or "?" when none can be read.
//
// url.Parse is the first answer, but it rejects any URL whose port is not a number — and
// `http://localhost:${PORT}/` is the ordinary way a JS sidecar call is written (measured on
// superpowers' brainstorm-server tests: every loopback call carries a template port). The
// host half of such a URL is perfectly readable, so a second pass takes the authority apart
// by hand. Unresolved syntax IN the host (`${HOST}`, `localhost${X}`) comes back verbatim and
// fails the loopback test — the fallback reads more, it never guesses more.
func urlHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if m := urlAuthorityHostRE.FindStringSubmatch(raw); m != nil {
		return strings.Trim(m[1], "[]")
	}
	return "?"
}

// devTCPHostRE is the host of a bash /dev/tcp/HOST/PORT redirect — a covert channel the
// network leg already counts, so the loopback screen has to see it too.
var devTCPHostRE = regexp.MustCompile(`/dev/tcp/([^/\s]+)`)

// hostKVRE is the options-object spelling of a destination — `http.get({ hostname: '127.0.0.1',
// port, path }, …)` in Node, `host="localhost"` in Python — where no URL literal exists on the
// line at all. Only a quoted literal value counts; `hostname: HOST` names nothing readable.
var hostKVRE = regexp.MustCompile(`(?i)\bhost(?:name)?\s*[:=]\s*['"]([^'"\s]+)['"]`)

// networkHosts returns every host literal a network-matching line names. An empty result
// means the line matched as egress (curl, fetch(, nc, …) but named no destination we can
// read — that is "unknown", not "loopback", and the caller must fail closed.
func networkHosts(line string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		out = append(out, h)
	}
	for _, raw := range httpURLRE.FindAllString(line, -1) {
		// A match whose host cannot be read comes back as "?": an unknown destination, not a
		// skip — the loopback screen rejects the sentinel.
		add(urlHost(raw))
	}
	for _, m := range devTCPHostRE.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range hostKVRE.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	return out
}

// lineLoopbackOnly reports whether every host named on the line is loopback AND at least
// one host was named. Zero hosts → false (the line is egress to somewhere we cannot see).
func lineLoopbackOnly(line string) bool {
	hosts := networkHosts(line)
	if len(hosts) == 0 {
		return false
	}
	for _, h := range hosts {
		if !loopbackHost(h) {
			return false
		}
	}
	return true
}
