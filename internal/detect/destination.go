// SPDX-License-Identifier: MIT
package detect

import (
	"regexp"
	"strings"
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

// hostMatchesService reports whether a host shares a token with any known credential service. The
// host is split on dots; the RAW host is used by the caller, so a homoglyph domain (`dееpl.com`
// with Cyrillic е) does not fold into the ASCII service name and cannot buy a downgrade.
func hostMatchesService(host string, services map[string]bool) bool {
	for _, p := range strings.Split(strings.ToLower(host), ".") {
		if services[p] {
			return true
		}
	}
	return false
}
