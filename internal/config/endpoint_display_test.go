// SPDX-License-Identifier: MIT
package config

import (
	"strings"
	"testing"
)

// TestCheckEndpoint_ErrorsDoNotEchoCredentials (P-076): every refusal quotes the base_url it
// refused, and that text reaches the terminal (setup, `llm test`) and the report (the LLM-000 note,
// judge.reason). A URL carrying userinfo or a query secret is quoted with them redacted — the one
// that does not parse as well, since failing url.Parse says nothing about what the string holds.
func TestCheckEndpoint_ErrorsDoNotEchoCredentials(t *testing.T) {
	for _, c := range []struct{ name, url, want string }{
		{"not a valid URL", "https://alice:s3cret@llm.example:99999x/v1?api-key=k123", "is not a valid URL"},
		{"no host", "https://alice:s3cret@/v1?api-key=k123", "is not a valid URL"},
		{"plain http to a remote host", "http://alice:s3cret@llm.example/v1?api-key=k123", "plain http to a remote host"},
		{"wrong scheme", "ftp://alice:s3cret@llm.example/v1?api-key=k123", "scheme must be https"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := (LLMConfig{BaseURL: c.url}).CheckEndpoint()
			if err == nil {
				t.Fatalf("%s: accepted, want a refusal", c.url)
			}
			msg := err.Error()
			if !strings.Contains(msg, c.want) {
				t.Errorf("error %q does not say %q", msg, c.want)
			}
			for _, secret := range []string{"alice", "s3cret", "k123"} {
				if strings.Contains(msg, secret) {
					t.Errorf("error quotes %q from the base_url: %s", secret, msg)
				}
			}
			if c.name != "no host" && !strings.Contains(msg, "llm.example") {
				t.Errorf("error no longer names the host: %s", msg)
			}
		})
	}
}
