// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ccauditadapter "github.com/basdotio/agent-guard/baselines/adapter/ccaudit"
	ciscoadapter "github.com/basdotio/agent-guard/baselines/adapter/cisco"
)

// TestPickRefusesWithAnActionableReason — the driver's refusals are the only thing a person sees
// when a run cannot start, so each has to say which of several possible things is missing.
// "no adapter for cc-audit" and "cc-audit is not installed" send the reader to two very
// different places, and a message that conflates them wastes an afternoon.
func TestPickRefusesWithAnActionableReason(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		threshold string
		want      []string
	}{
		{
			name: "an unknown tool lists the ones that exist",
			tool: "skillspector", threshold: "high",
			want: []string{"no adapter", "skillspector", "aguard", "ccaudit", "skill-scanner"},
		},
		{
			name: "ccaudit with no binary says the adapter exists and the binary does not",
			tool: "ccaudit", threshold: "default",
			want: []string{"not on PATH", "adapter exists", "brew", "v3.23.9"},
		},
		{
			// Argument validation comes before the PATH check on purpose: a mistyped threshold
			// is worth hearing about while preparing the run, not after downloading a binary.
			//
			// cc-audit's gate is not a severity name. It is `default` (critical+high) or
			// `strict` (--strict, which adds medium/low AND promotes warnings to errors), and
			// which one counts as "fails the build" is a CHOICE that run.yaml records as one.
			// Accepting "high" here would silently measure a tier nobody chose.
			name: "ccaudit with a severity-shaped threshold names the two tiers it actually has",
			tool: "ccaudit", threshold: "high",
			want: []string{"default", "strict", "choice"},
		},
		{
			// A SARIF level is not on skill-scanner's ladder; --fail-on-severity would reject it
			// at run time, after the binary was installed. Hear about it now.
			name: "skill-scanner with a SARIF-shaped threshold names its own five-rung ladder",
			tool: "skill-scanner", threshold: "error",
			want: []string{"critical", "high", "medium", "low", "info", "choice"},
		},
		{
			name: "skill-scanner with no binary says the adapter exists and the binary does not",
			tool: "skill-scanner", threshold: "high",
			want: []string{"not on PATH", "adapter exists", "2.1.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A binary name nothing will resolve, so the PATH check fails deterministically on
			// any machine — including one where cc-audit is genuinely installed.
			o := opts{tool: tt.tool, toolBin: "cc-audit-does-not-exist-" + t.Name()}
			_, err := pick(o, tt.tool, tt.threshold, t.TempDir(), t.TempDir(), "")
			if err == nil {
				t.Fatalf("pick accepted tool=%q threshold=%q", tt.tool, tt.threshold)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
		})
	}
}

// TestPickDispatchesToCCAudit — every refusal above is a NEGATIVE case, and a pick() that
// returned an error for everything would pass all of them. This is the positive one: the tool
// this adapter exists to measure must actually come back, configured from the flags rather than from
// defaults nobody chose.
//
// The criteria named this test before it existed, which is a mistake this directory has made before: a
// criterion that names a test is only worth something if the test is there to run.
func TestPickDispatchesToCCAudit(t *testing.T) {
	// A binary that resolves, because this path gets past the PATH check.
	dir := t.TempDir()
	bin := filepath.Join(dir, "cc-audit")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	cfg := filepath.Join(dir, ".cc-audit.yaml")
	if err := os.WriteFile(cfg, []byte("severity:\n  default: error\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	o := opts{tool: "ccaudit", toolBin: bin, policyPath: cfg}
	ad, err := pick(o, "ccaudit", "default", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused ccaudit: %v", err)
	}
	if ad.Tool() != "ccaudit" {
		t.Fatalf("pick returned a %q adapter", ad.Tool())
	}
	got, ok := ad.(*ccauditadapter.Adapter)
	if !ok {
		t.Fatalf("pick returned %T", ad)
	}
	// Each of these was a separate measured surprise; a default silently standing in for one of
	// them is how a run measures something nobody chose.
	if got.Bin != bin {
		t.Errorf("Bin = %q, want the -bin flag %q", got.Bin, bin)
	}
	if got.Tier != ccauditadapter.TierDefault {
		t.Errorf("Tier = %q, want %q", got.Tier, ccauditadapter.TierDefault)
	}
	if got.Config != cfg {
		t.Errorf("Config = %q, want the -policy file %q — cc-audit exits 2 without one", got.Config, cfg)
	}

	// The other tier must reach the adapter too, or -threshold would be decorative.
	ad, err = pick(o, "ccaudit", "strict", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused the strict tier: %v", err)
	}
	if got := ad.(*ccauditadapter.Adapter).Tier; got != ccauditadapter.TierStrict {
		t.Errorf("Tier = %q, want %q", got, ccauditadapter.TierStrict)
	}
}

// TestPickRefusesCCAuditWithoutAConfig — measured 2026-09-23: `cc-audit check` with no config
// exits 2 with "Configuration file not found" and scans nothing. Refusing up front turns 3,539
// identical runtime errors into one sentence naming the fix.
func TestPickRefusesCCAuditWithoutAConfig(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cc-audit")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	_, err := pick(opts{tool: "ccaudit", toolBin: bin}, "ccaudit", "default", t.TempDir(), t.TempDir(), "")
	if err == nil {
		t.Fatal("pick accepted ccaudit with no -policy; every sample would then exit 2")
	}
	for _, want := range []string{"config", "cc-audit init", "-policy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q:\n%v", want, err)
		}
	}
}

// TestPickBuildsTheAguardAdapter is the reverse assertion for the refusals above: they must not
// be so eager that the tool that does work stops working.
func TestPickBuildsTheAguardAdapter(t *testing.T) {
	ad, err := pick(opts{tool: "aguard", bin: "bin/aguard"}, "aguard", "high", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused aguard: %v", err)
	}
	if ad == nil || ad.Tool() != "aguard" {
		t.Errorf("pick returned %v", ad)
	}
}

// TestSplitArgv — an empty override must mean "use the default", not "run with no arguments".
// The difference is a scan that never happens versus one that happens wrongly.
func TestSplitArgv(t *testing.T) {
	for _, in := range []string{"", "   ", "\t"} {
		if got := splitArgv(in); got != nil {
			t.Errorf("splitArgv(%q) = %v, want nil so the adapter falls back to its default", in, got)
		}
	}
	got := splitArgv("check {{sample}} --format sarif")
	if len(got) != 4 || got[1] != "{{sample}}" {
		t.Errorf("splitArgv = %v, want the four fields with the placeholder intact", got)
	}
}

// TestPickDispatchesToCisco — the positive case, so a pick() that refused everything could not
// pass the table above. Bin, Threshold and Policy must all come from flags.
func TestPickDispatchesToCisco(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "skill-scanner")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pol := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(pol, []byte("benign_dotfiles: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ad, err := pick(opts{tool: "skill-scanner", toolBin: bin, policyPath: pol}, "skill-scanner", "high", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused cisco: %v", err)
	}
	got, ok := ad.(*ciscoadapter.Adapter)
	if !ok || got.Tool() != "skill-scanner" {
		t.Fatalf("pick returned %T %v", ad, ad)
	}
	if got.Bin != bin || got.Policy != pol || got.Threshold != ciscoadapter.SeverityHigh {
		t.Errorf("Bin=%q Policy=%q Threshold=%q; each must come from its flag", got.Bin, got.Policy, got.Threshold)
	}
}

// TestPickRefusesCiscoWithoutAPolicy — the built-in default policy is dumped to a file and hashed into run.yaml, so "default" is a hash and not a version
// number. A run without -policy would publish an unhashed default.
func TestPickRefusesCiscoWithoutAPolicy(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "skill-scanner")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := pick(opts{tool: "skill-scanner", toolBin: bin}, "skill-scanner", "high", t.TempDir(), t.TempDir(), "")
	if err == nil {
		t.Fatal("pick accepted cisco with no -policy; run.yaml would carry an unhashed default")
	}
	for _, want := range []string{"-policy", "policy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q:\n%v", want, err)
		}
	}
}
