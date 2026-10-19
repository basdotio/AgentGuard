// SPDX-License-Identifier: MIT
package judge

import (
	"net/url"
	"strings"
)

// chatPath is what every OpenAI-compatible root is extended by.
const chatPath = "/chat/completions"

// chatURL is the URL every request of a client is sent to: the configured base_url with
// /chat/completions appended to its PATH (P-078).
//
// It used to be appended to the whole string, which is the path only when nothing follows it. A
// gateway that takes ?api-version=… or ?api-key=… in base_url was then called at the base path,
// with the suffix swallowed by the last query value; a fragment swallowed it the same way and was
// never sent. Now the string is cut where net/url cuts it — the fragment at the first '#', then the
// query at the first '?' — the fragment is dropped (no HTTP client sends one), trailing slashes are
// trimmed from the rest (so …/v1/ and …/v1 stay one URL), and the query goes back as written.
// Splitting the string instead of re-serializing a url.URL keeps every other byte as configured.
//
// A base_url with neither '?' nor '#', or one net/url refuses, takes the old expression, byte for
// byte: the first is every configuration that ever worked, and the second is refused by
// CheckEndpoint before any request is built (a caller that skips it gets the same build error).
func chatURL(baseURL string) string {
	if !strings.ContainsAny(baseURL, "?#") {
		return joinChatPath(baseURL)
	}
	if _, err := url.Parse(baseURL); err != nil {
		return joinChatPath(baseURL)
	}
	rest, _, _ := strings.Cut(baseURL, "#")
	rest, query, hasQuery := strings.Cut(rest, "?")
	if !hasQuery {
		return joinChatPath(rest)
	}
	return joinChatPath(rest) + "?" + query
}

// joinChatPath is the expression the client always used, now applied to the part before the query.
func joinChatPath(s string) string { return strings.TrimRight(s, "/") + chatPath }
