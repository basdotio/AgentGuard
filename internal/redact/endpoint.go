// SPDX-License-Identifier: MIT
package redact

import (
	"regexp"
	"strings"
)

// Endpoint is a configured endpoint (llm.base_url) as bas-guard shows it — in a report, a note, an
// error, a command's output (P-076). The request uses the URL as written; only its display goes
// through here.
//
// A URL carries a credential in three places by its structure, and those are replaced:
//
//   - the userinfo, whole: `https://<REDACTED>@host`. The user name goes too — a token is often
//     written AS the user (`https://<token>@host`), and Secrets' URL pattern, made for connection
//     strings in skill text, keeps the user;
//   - every query value, the keys kept: `?api-key=<REDACTED>&deployment=<REDACTED>`; a part with
//     no `=` is all value. Gateways take their key in the query, and which parameter is set is
//     what the operator needs to recognise the configuration;
//   - a fragment.
//
// Scheme, host, port and path — where the requests go — are kept, and the result then goes
// through Secrets like any other quoted text, so a key-shaped path segment is caught by the same
// patterns as everywhere else (there is no second pattern list here).
//
// It splits the raw string rather than parsing it (splitEndpoint), so a base_url that url.Parse
// rejects — the one a "not a valid URL" error quotes — is redacted the same way. An endpoint with
// none of the three parts and nothing Secrets matches comes back byte for byte. Idempotent.
func Endpoint(raw string) string {
	p := splitEndpoint(raw)
	var b strings.Builder
	b.WriteString(p.scheme)
	if p.hasUser {
		b.WriteString(marker + "@")
	}
	b.WriteString(p.host)
	b.WriteString(p.path)
	if p.hasQuery {
		b.WriteString("?" + redactQuery(p.query))
	}
	if p.hasFragment {
		b.WriteString("#")
		if p.fragment != "" {
			b.WriteString(marker)
		}
	}
	return Secrets(b.String())
}

// EndpointHost is the host[:port] of an endpoint, raw or as Endpoint showed it, or "" when there
// is none. `<REDACTED>@` is not a userinfo url.Parse accepts, so a reader of a shown endpoint asks
// here, where the split is the one Endpoint made.
func EndpointHost(s string) string { return splitEndpoint(s).host }

// endpointParts is an endpoint split at its delimiters. Every part keeps its bytes as written.
type endpointParts struct {
	scheme      string // "https://", or "" when the string names none
	hasUser     bool   // an `@` in the authority; the userinfo itself is never kept
	host        string // host[:port] as written
	path        string
	query       string
	hasQuery    bool
	fragment    string
	hasFragment bool
}

// schemeRE is RFC 3986's scheme followed by `://`. Anchored, so a `://` inside a query value
// (`?next=https://…`) is not taken for one.
var schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*://`)

// splitEndpoint splits s the way RFC 3986 delimits a URL, without validating it: the fragment
// from the first `#`, the query from the first `?` before it, the authority up to the first `/`,
// and in the authority the userinfo up to its LAST `@` (what url.Parse does). A string without a
// scheme is read as starting with its authority — `user:pass@host/v1` is still a credential.
func splitEndpoint(s string) endpointParts {
	var p endpointParts
	if m := schemeRE.FindString(s); m != "" {
		p.scheme, s = m, s[len(m):]
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s, p.fragment, p.hasFragment = s[:i], s[i+1:], true
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s, p.query, p.hasQuery = s[:i], s[i+1:], true
	}
	authority := s
	if i := strings.IndexByte(s, '/'); i >= 0 {
		authority, p.path = s[:i], s[i:]
	}
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		p.hasUser, authority = true, authority[i+1:]
	}
	p.host = authority
	return p
}

// redactQuery replaces the value of every `&`-separated part of a raw query, keeping its key and
// its order. An empty value has nothing to hide and stays; a part without `=` is all value.
func redactQuery(q string) string {
	parts := strings.Split(q, "&")
	out := make([]string, len(parts))
	for i, part := range parts {
		key, value, hasValue := strings.Cut(part, "=")
		switch {
		case !hasValue && part != "":
			out[i] = marker
		case hasValue && value != "":
			out[i] = key + "=" + marker
		default:
			out[i] = part
		}
	}
	return strings.Join(out, "&")
}
