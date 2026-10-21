// SPDX-License-Identifier: MIT
package redact

import "testing"

// TestEndpoint: what a configured judge endpoint looks like wherever bas-guard shows it (P-076). The
// credentials a URL can carry by its structure — the userinfo, the query values, a fragment — are
// replaced; the scheme, host, port and path, which say where the requests go, are not.
func TestEndpoint(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"user and password, query key", "https://alice:s3cret@llm.example/v1?api-key=k123",
			"https://<REDACTED>@llm.example/v1?api-key=<REDACTED>"},
		{"user only: a token written as the user", "https://ghp0token0value@llm.example/v1",
			"https://<REDACTED>@llm.example/v1"},
		{"empty user, password only", "http://:s3cret@127.0.0.1:8080/v1", "http://<REDACTED>@127.0.0.1:8080/v1"},
		{"every query value, keys kept, order kept", "https://gw.example/v1?deployment=prod&api-version=2024&key=k123",
			"https://gw.example/v1?deployment=<REDACTED>&api-version=<REDACTED>&key=<REDACTED>"},
		{"a query part without = is all value", "https://gw.example/v1?k123", "https://gw.example/v1?<REDACTED>"},
		{"an empty value has nothing to hide", "https://gw.example/v1?a=&b", "https://gw.example/v1?a=&<REDACTED>"},
		{"fragment", "https://gw.example/v1#k123", "https://gw.example/v1#<REDACTED>"},
		{"query then fragment", "https://gw.example/v1?key=k123#frag", "https://gw.example/v1?key=<REDACTED>#<REDACTED>"},
		{"@ in the path is not userinfo", "https://gw.example/v1/@team", "https://gw.example/v1/@team"},
		{"@ in the query is not userinfo", "https://gw.example/v1?to=a@b", "https://gw.example/v1?to=<REDACTED>"},
		{"the last @ of the authority ends the userinfo", "https://a@b:c@llm.example/v1", "https://<REDACTED>@llm.example/v1"},
		{"no scheme", "alice:s3cret@llm.example/v1?api-key=k123", "<REDACTED>@llm.example/v1?api-key=<REDACTED>"},
		{"a :// inside the query is not a scheme", "llm.example/v1?next=https://x", "llm.example/v1?next=<REDACTED>"},
		{"unparseable port", "https://alice:s3cret@llm.example:99999x/v1?api-key=k123",
			"https://<REDACTED>@llm.example:99999x/v1?api-key=<REDACTED>"},
		{"a key-shaped path segment goes through Secrets", "https://gw.example/sk-proj0123456789abcdefABCDEF/v1",
			"https://gw.example/<REDACTED>/v1"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Endpoint(c.in)
			if got != c.want {
				t.Errorf("Endpoint(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
			if again := Endpoint(got); again != got {
				t.Errorf("not idempotent: Endpoint(%q) = %q", got, again)
			}
		})
	}
}

// TestEndpoint_PlainURLsUnchanged is the reverse half: an endpoint that carries no credentials is
// shown exactly as it is written, so every report and message about one reads as before.
func TestEndpoint_PlainURLsUnchanged(t *testing.T) {
	for _, u := range []string{
		"http://localhost:11434/v1", // config.Default
		"https://api.deepseek.com/v1",
		"https://api.openai.com/v1",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
		"http://127.0.0.1:9",
		"http://[::1]:8080/v1",
		"https://llm.internal.example:8443/openai/v1/",
		"https://gw.example/v1/@team",
		"localhost:11434",
		"not a url at all",
	} {
		if got := Endpoint(u); got != u {
			t.Errorf("Endpoint(%q) = %q, want it unchanged", u, got)
		}
	}
}

// TestEndpointHost: the host of an endpoint, from the raw string or from what Endpoint showed —
// `<REDACTED>@` is not a valid URL userinfo, so url.Parse cannot be asked for it.
func TestEndpointHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://alice:s3cret@llm.example/v1?api-key=k123":    "llm.example",
		"https://<REDACTED>@llm.example:8443/v1?k=<REDACTED>": "llm.example:8443",
		"http://127.0.0.1:9":   "127.0.0.1:9",
		"http://[::1]:8080/v1": "[::1]:8080",
		"alice@llm.example/v1": "llm.example",
		"":                     "",
		"https:///v1":          "",
	} {
		if got := EndpointHost(in); got != want {
			t.Errorf("EndpointHost(%q) = %q, want %q", in, got, want)
		}
	}
}
