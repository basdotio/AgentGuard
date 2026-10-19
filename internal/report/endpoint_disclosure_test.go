// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestDisclosure_HostFromARedactedEndpoint (P-076): judge.endpoint arrives with its credentials
// replaced — `https://<REDACTED>@host/…` — and `<REDACTED>@` is no URL userinfo url.Parse accepts.
// The disclosure line still names the host, in the terminal and in Markdown.
func TestDisclosure_HostFromARedactedEndpoint(t *testing.T) {
	r := targetResult()
	r.Judge.Endpoint = "https://<REDACTED>@llm.example:8443/v1?api-key=<REDACTED>"
	r.Notes = []model.Finding{{RuleID: "LLM-002", Severity: model.SevMedium, Source: model.SrcLLM, Title: "LLM judge endpoint is not local",
		Why: "base_url is not loopback: best-effort-redacted skill excerpts are sent off this machine."}}
	if brief := termOut(r, false); !strings.Contains(brief, "Sent to llm.example:8443: redacted excerpts") {
		t.Errorf("terminal disclosure does not name the host:\n%s", brief)
	}
	var md bytes.Buffer
	if err := Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "Sent to llm.example:8443: redacted excerpts") {
		t.Errorf("markdown disclosure does not name the host:\n%s", md.String())
	}
}
