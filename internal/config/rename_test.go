// SPDX-License-Identifier: MIT
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigDirFallback: the config directory is bas-guard after the rename (P-082). An operator
// who only has the aguard directory keeps using it — their config and key file are read where they
// are and nothing is moved — and when both exist the new one wins and the old one is named as
// ignored, so a stale key in it cannot be mistaken for the one in use.
func TestConfigDirFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mk          []string
		want        string
		wantIgnored string
	}{
		{"neither", nil, "bas-guard", ""},
		{"only new", []string{"bas-guard"}, "bas-guard", ""},
		{"only old", []string{"aguard"}, "aguard", ""},
		{"both", []string{"aguard", "bas-guard"}, "bas-guard", "aguard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", base)
			for _, d := range tc.mk {
				if err := os.MkdirAll(filepath.Join(base, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ConfigDir()
			if err != nil || got != filepath.Join(base, tc.want) {
				t.Fatalf("ConfigDir() = %q, %v; want %q", got, err, filepath.Join(base, tc.want))
			}
			ignored, ok := IgnoredLegacyConfigDir()
			if tc.wantIgnored == "" {
				if ok {
					t.Errorf("IgnoredLegacyConfigDir() = %q, want none", ignored)
				}
			} else if !ok || ignored != filepath.Join(base, tc.wantIgnored) {
				t.Errorf("IgnoredLegacyConfigDir() = %q %v, want %q", ignored, ok, filepath.Join(base, tc.wantIgnored))
			}
			if p, _ := DefaultKeyPath(); p != filepath.Join(base, tc.want, "llm.key") {
				t.Errorf("DefaultKeyPath() = %q, not under the directory in use", p)
			}
		})
	}
}

// TestEnvAliases: every environment variable the tool read before the rename has a BAS_GUARD_
// form, read first; the old name keeps working (P-082).
func TestEnvAliases(t *testing.T) {
	for _, old := range []string{"AGUARD_LLM_KEY", "AGUARD_USAGE_LOG"} {
		alias := "BAS_GUARD_" + old[len("AGUARD_"):]
		t.Run(old, func(t *testing.T) {
			t.Setenv(old, "")
			t.Setenv(alias, "")
			if v, from := LookupEnv(old); v != "" || from != "" {
				t.Fatalf("both unset: %q from %q", v, from)
			}
			t.Setenv(old, "old")
			if v, from := LookupEnv(old); v != "old" || from != old {
				t.Errorf("old only: %q from %q", v, from)
			}
			t.Setenv(alias, "new")
			if v, from := LookupEnv(old); v != "new" || from != alias {
				t.Errorf("both set: %q from %q, want the new name to win", v, from)
			}
			t.Setenv(old, "")
			if v, from := LookupEnv(old); v != "new" || from != alias {
				t.Errorf("new only: %q from %q", v, from)
			}
		})
	}
	if v, from := LookupEnv("SOME_OTHER_VAR_NOT_RENAMED"); v != "" || from != "" {
		t.Errorf("an unrelated unset variable: %q from %q", v, from)
	}
}

// TestAPIKeyFromRenamedEnv: with the default api_key_env, a key in BAS_GUARD_LLM_KEY is used and
// llm status names that variable; AGUARD_LLM_KEY still works on its own.
func TestAPIKeyFromRenamedEnv(t *testing.T) {
	c := Default()
	t.Setenv("AGUARD_LLM_KEY", "")
	t.Setenv("BAS_GUARD_LLM_KEY", "k-new")
	if k, err := c.ResolveAPIKey(); err != nil || k != "k-new" {
		t.Fatalf("ResolveAPIKey() = %q, %v", k, err)
	}
	if s := c.KeySource(); s != "env var BAS_GUARD_LLM_KEY" {
		t.Errorf("KeySource() = %q", s)
	}
	t.Setenv("BAS_GUARD_LLM_KEY", "")
	t.Setenv("AGUARD_LLM_KEY", "k-old")
	if k, err := c.ResolveAPIKey(); err != nil || k != "k-old" {
		t.Fatalf("old name: ResolveAPIKey() = %q, %v", k, err)
	}
	if s := c.KeySource(); s != "env var AGUARD_LLM_KEY" {
		t.Errorf("old name: KeySource() = %q", s)
	}
}
