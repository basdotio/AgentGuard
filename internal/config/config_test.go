// SPDX-License-Identifier: MIT
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_MissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.LLM.Enabled {
		t.Error("default must have LLM disabled")
	}
	if cfg.JudgeReady() {
		t.Error("default must not be judge-ready (offline-safe)")
	}
}

func TestLoad_EmptyPathReturnsDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil || cfg.LLM.Enabled {
		t.Fatalf("empty path → disabled default; got cfg=%+v err=%v", cfg, err)
	}
}

func TestLoad_CorruptErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(p, []byte("llm: [not-a-map"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Error("corrupt config must return an error, not silently default")
	}
}

func TestResolveAPIKey_EnvThenFile(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKeyEnv = "AGUARD_TEST_KEY_XYZ"
	t.Setenv("AGUARD_TEST_KEY_XYZ", "secret-value")
	if k, err := cfg.ResolveAPIKey(); err != nil || k != "secret-value" {
		t.Errorf("env var set: key=%q err=%v", k, err)
	}

	// Env unset → the file. Trailing newline trimmed, ~ expanded.
	t.Setenv("AGUARD_TEST_KEY_XYZ", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "llm.key")
	if err := WriteKey(keyPath, "from-file\n"); err != nil {
		t.Fatal(err)
	}
	cfg.LLM.APIKeyFile = keyPath
	if k, err := cfg.ResolveAPIKey(); err != nil || k != "from-file" {
		t.Errorf("file: key=%q err=%v", k, err)
	}
	if fi, _ := os.Stat(keyPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("WriteKey mode = %04o, want 0600", fi.Mode().Perm())
	}

	// A key file others can read is refused, and the message names the fix.
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveAPIKey(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("world-readable key file: err = %v, want a refusal naming chmod 600", err)
	}

	cfg.LLM.APIKeyFile = filepath.Join(dir, "absent.key")
	if _, err := cfg.ResolveAPIKey(); err == nil || !strings.Contains(err.Error(), "aguard llm setup") {
		t.Errorf("missing key file: err = %v, want a pointer to `aguard llm setup`", err)
	}
	if err := WriteKey(filepath.Join(dir, "empty.key"), "  \n"); err == nil {
		t.Error("WriteKey accepted an empty key")
	}

	cfg.LLM.APIKeyEnv, cfg.LLM.APIKeyFile = "", ""
	if k, err := cfg.ResolveAPIKey(); err != nil || k != "" {
		t.Errorf("nothing configured: key=%q err=%v, want empty and nil", k, err)
	}
}

// TestLoad_PresetFillsEndpoint: a named provider is three lines of config; the user's own
// model/base_url still win, and a name nobody knows is an error rather than localhost.
func TestLoad_PresetFillsEndpoint(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("a.yaml", "llm:\n  enabled: true\n  provider: deepseek\n  api_key_file: ~/x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.BaseURL != Presets["deepseek"].BaseURL || cfg.LLM.Model != Presets["deepseek"].Model {
		t.Errorf("preset not applied: %+v", cfg.LLM)
	}
	cfg, err = Load(write("b.yaml", "llm:\n  provider: deepseek\n  model: deepseek-reasoner\n  base_url: https://proxy.example/v1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "deepseek-reasoner" || cfg.LLM.BaseURL != "https://proxy.example/v1" {
		t.Errorf("explicit model/base_url must win over the preset: %+v", cfg.LLM)
	}
	if _, err := Load(write("c.yaml", "llm:\n  provider: deepsek\n")); err == nil || !strings.Contains(err.Error(), "deepseek") {
		t.Errorf("unknown provider: err = %v, want an error listing the presets", err)
	}
	cfg, err = Load(write("d.yaml", "llm:\n  provider: openai_compatible\n  base_url: http://localhost:8000/v1\n"))
	if err != nil || cfg.LLM.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("generic provider must be left alone: %+v, %v", cfg.LLM, err)
	}
}

// TestLoadUser_DefaultLocation: with no --config, the user-level file is read when present and
// its absence is defaults — so `aguard scan --llm` needs no path once `aguard llm setup` ran.
func TestLoadUser_DefaultLocation(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", x)
	cfg, err := LoadUser("")
	if err != nil || cfg.LLM.Enabled {
		t.Fatalf("no file: cfg=%+v err=%v, want defaults", cfg.LLM, err)
	}
	def, _ := DefaultPath()
	if def != filepath.Join(x, "aguard", "config.yaml") {
		t.Fatalf("DefaultPath = %q", def)
	}
	if err := WriteLLM(def, LLMConfig{Enabled: true, Provider: "qwen", APIKeyFile: "~/k"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadUser("")
	if err != nil || !cfg.LLM.Enabled || cfg.LLM.Provider != "qwen" || cfg.LLM.Model != Presets["qwen"].Model {
		t.Errorf("default location not read: %+v err=%v", cfg.LLM, err)
	}
	b, _ := os.ReadFile(def)
	if strings.Contains(string(b), "base_url") || strings.Contains(string(b), "concurrency") || strings.Contains(string(b), "gate") {
		t.Errorf("WriteLLM wrote defaults it should have left implicit:\n%s", b)
	}
	// An existing unrelated section survives a rewrite.
	if err := os.WriteFile(def, []byte("gate:\n  action: deny\nllm:\n  provider: openai\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteLLM(def, LLMConfig{Enabled: true, Provider: "deepseek", APIKeyFile: "~/k"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadUser("")
	if err != nil || cfg.Gate.Action != "deny" || cfg.LLM.Provider != "deepseek" {
		t.Errorf("rewrite clobbered another section: %+v err=%v", cfg, err)
	}
}

// TestLoad_CallBudgetDefaults: an omitted knob keeps its default, while an explicitly zeroed
// one means zero — the reason Load unmarshals ONTO Default() instead of into a blank struct.
func TestLoad_CallBudgetDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("llm:\n  enabled: true\n  max_retries: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Concurrency != 4 {
		t.Errorf("omitted concurrency = %d, want the default 4", cfg.LLM.Concurrency)
	}
	if cfg.LLM.MaxRetries != 0 {
		t.Errorf("explicit max_retries: 0 = %d, want 0 (not the default)", cfg.LLM.MaxRetries)
	}
	if cfg.LLM.MaxCalls != 0 {
		t.Errorf("max_calls default = %d, want 0 (unlimited — truncating coverage by default would be silent)", cfg.LLM.MaxCalls)
	}
}

// TestLoad_BadDurationIsAnError: a typo'd timeout must fail loudly at load, not quietly fall
// back to a default the operator did not ask for.
func TestLoad_BadDurationIsAnError(t *testing.T) {
	for _, bad := range []string{"60 seconds", "-5s", "0s"} {
		p := filepath.Join(t.TempDir(), "c.yaml")
		if err := os.WriteFile(p, []byte("llm:\n  timeout: \""+bad+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("timeout %q accepted; want a config error", bad)
		}
	}
}

func TestDurations_Defaults(t *testing.T) {
	call, total, err := LLMConfig{}.Durations()
	if err != nil {
		t.Fatal(err)
	}
	if call != 60*time.Second || total != 10*time.Minute {
		t.Errorf("empty durations = %v/%v, want 60s/10m", call, total)
	}
}

// TestCheckEndpoint: the key travels as a bearer header, so a remote endpoint must be https; a
// local model on plain http is the one exception, and only for loopback hosts.
func TestCheckEndpoint(t *testing.T) {
	ok := []string{"https://api.deepseek.com/v1", "http://localhost:11434/v1", "http://127.0.0.1:8000/v1", "http://[::1]:8000/v1"}
	for _, u := range ok {
		if err := (LLMConfig{BaseURL: u}).CheckEndpoint(); err != nil {
			t.Errorf("%s: unexpected refusal: %v", u, err)
		}
	}
	bad := []string{"http://api.example.com/v1", "http://10.0.0.5:8000/v1", "ftp://x/v1", "not a url", ""}
	for _, u := range bad {
		if err := (LLMConfig{BaseURL: u}).CheckEndpoint(); err == nil {
			t.Errorf("%q: accepted, want a refusal", u)
		}
	}
}
