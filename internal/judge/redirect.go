// SPDX-License-Identifier: MIT
package judge

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects is net/http's own limit, kept on purpose: a CheckRedirect replaces the default
// policy whole, and without a limit a same-origin loop would be followed until the call's
// deadline. Same count, same message as the default.
const maxRedirects = 10

// redirectRefused is a redirect the judge's client did not follow. It is the endpoint's answer,
// not a transient fault, so chat returns it without the retry wrapper.
//
// It names origins only. The Location is written by the endpoint, and its path or query can carry
// a one-time credential (a signed URL); what the operator needs is where the call was sent, to
// decide whether that is the endpoint they meant to configure.
type redirectRefused struct {
	status   int
	to, from string // scheme://host[:port], as written
}

func (e *redirectRefused) Error() string {
	return fmt.Sprintf("the endpoint answered %d with a redirect to %q, outside the configured origin %s: "+
		"not followed, and nothing was sent there (if that address is the real endpoint, set llm.base_url to it)",
		e.status, e.to, e.from)
}

// sameOriginOnly is the redirect policy of the client NewHTTP builds (P-023). CheckEndpoint vets
// the configured base_url — remote means https, because the API key is a bearer header and the
// body is the redacted excerpts — but a 30x is the endpoint choosing a new address, which nothing
// had vetted. Under net/http's default policy that address got the key whenever the host NAME
// matched (an https→http downgrade on the same host, another port, a subdomain), and a 307/308
// resent the excerpts to any host at all.
//
// So a hop is sent only when it stays in the origin the user configured: the first request's
// scheme, host and port. It is compared with via[0], not with the previous hop, so a chain cannot
// walk away one same-origin step at a time. A refused hop is never sent.
func sameOriginOnly(req *http.Request, via []*http.Request) error {
	configured := via[0].URL
	if origin(req.URL) != origin(configured) {
		status := 0
		if req.Response != nil {
			status = req.Response.StatusCode
		}
		return &redirectRefused{status: status, to: shownOrigin(req.URL), from: shownOrigin(configured)}
	}
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return nil
}

// origin is u's (scheme, host, port) in a comparable form: scheme and host name lower-cased, and
// the port the scheme implies written out, so https://h and https://h:443 are one origin.
func origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// shownOrigin is u's origin as it was written, for a message.
func shownOrigin(u *url.URL) string { return u.Scheme + "://" + u.Host }
