// SPDX-License-Identifier: MIT
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoad_InvalidLLMSectionIsSetAside (P-084): an llm section that fails its own validation no
// longer fails the file. Load sets it aside — the judge off, the defaults standing in — and keeps
// the reason, word for word what Load used to return, for every path that needs the judge. The
// gate section of the same file is kept: it is what the load-time gate and `approve` read.
func TestLoad_InvalidLLMSectionIsSetAside(t *testing.T) {
	cases := []struct{ name, llm, want string }{
		{"unknown provider", "  enabled: true\n  provider: not-a-provider\n", `config llm.provider "not-a-provider"`},
		{"unknown authority", "  authority: maybe\n", `config llm.authority "maybe"`},
		{"unparsable timeout", "  timeout: \"60 seconds\"\n", `config llm.timeout "60 seconds"`},
		{"unparsable total_timeout", "  total_timeout: soon\n", `config llm.total_timeout "soon"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, "gate:\n  action: deny\n  fail_on: medium\nllm:\n"+tc.llm))
			if err != nil {
				t.Fatalf("Load = %v; an invalid llm section must not fail the file", err)
			}
			p := cfg.LLM.Problem()
			if p == nil || !strings.HasPrefix(p.Error(), tc.want) {
				t.Errorf("Problem() = %v, want the load-time message starting %q", p, tc.want)
			}
			if err := cfg.LLM.GateReady(); err == nil || err.Error() != p.Error() {
				t.Errorf("GateReady() = %v, want the same problem", err)
			}
			want := Default().LLM
			if cfg.LLM.Enabled || cfg.LLM.Provider != want.Provider || cfg.LLM.BaseURL != want.BaseURL ||
				cfg.LLM.Model != want.Model || cfg.LLM.Authority != want.Authority {
				t.Errorf("the set-aside section is not the default one: %+v", cfg.LLM)
			}
			if cfg.Gate.Action != GateDeny || cfg.Gate.FailOn != "medium" {
				t.Errorf("the gate section of the same file was lost: %+v", cfg.Gate)
			}
		})
	}
}

// TestLoad_StillRefusesWhatEveryRunReads is the reverse assertion: only the llm section's own
// checks are deferred. An unknown gate value decides whether a load is interrupted, and a file
// that does not parse cannot be trusted for its gate section either — both still fail Load.
func TestLoad_StillRefusesWhatEveryRunReads(t *testing.T) {
	for name, body := range map[string]string{
		"gate.action":        "gate:\n  action: maybe\n",
		"gate.fail_on":       "gate:\n  fail_on: severe\n",
		"unparsable YAML":    "llm: [not-a-map",
		"wrong type in llm":  "llm:\n  samples: many\n",
		"gate wins over llm": "gate:\n  action: maybe\nllm:\n  provider: not-a-provider\n",
	} {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Errorf("%s: Load accepted it; want a load error", name)
		}
	}
	cfg, err := Load(writeConfig(t, "llm:\n  enabled: true\n  provider: deepseek\n  authority: escalate\n"))
	if err != nil || cfg.LLM.Problem() != nil || !cfg.LLM.Enabled || cfg.LLM.Authority != AuthorityEscalate {
		t.Errorf("a valid llm section: cfg=%+v problem=%v err=%v", cfg.LLM, cfg.LLM.Problem(), err)
	}
	if Default().LLM.Problem() != nil {
		t.Error("the default section has a problem")
	}
}
