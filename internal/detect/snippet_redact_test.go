// SPDX-License-Identifier: MIT
package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// Redaction has one exit point (invariant #3), and a finding that shows the same bytes twice
// must show them through it twice. These tests pin the two places that redacted one copy and
// printed the other: HOOK-002, whose resolved path after the arrow is the reference before it
// expanded, and SUP-006, whose explanation names the registry target its own snippet redacts.

// pathToken is an obviously fake GitHub token, the shape Redact's known-prefix table removes.
const pathToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

// legacyHookOutsideSnippet is the HOOK-002 snippet formula before this change, kept here as the
// oracle for the reverse assertion: where nothing in the path is secret-shaped, the snippet must
// stay byte-identical to what it was.
func legacyHookOutsideSnippet(ref, resolved string) string {
	return clip(redactClip(ref) + " → " + resolved)
}

// assertNoToken fails if tok appears in any human- or machine-facing string of the findings.
func assertNoToken(t *testing.T, tok string, fs []model.Finding) {
	t.Helper()
	for _, f := range fs {
		if strings.Contains(f.Title, tok) || strings.Contains(f.Why, tok) {
			t.Errorf("%s: the token reached Title/Why: %q", f.RuleID, f.Why)
		}
		for _, e := range f.Evidence {
			if strings.Contains(e.File, tok) || strings.Contains(e.Snippet, tok) {
				t.Errorf("%s: the token reached the evidence: file=%q snippet=%q", f.RuleID, e.File, e.Snippet)
			}
		}
	}
}

// TestHookOutside_SecretInPathIsRedactedOnBothSides: a hook names a script outside HOME whose
// path carries a token as a directory name. The reference before the arrow was redacted; the
// resolved path after it is the same bytes and was printed as they are.
func TestHookOutside_SecretInPathIsRedactedOnBothSides(t *testing.T) {
	script := filepath.Join(t.TempDir(), "opt", pathToken, "hook.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := hookHome(t, nil)
	fs, notes := runHook(t, root, model.Hook{Event: "PreToolUse", Matcher: "Bash", Command: "sh " + script})

	f, ok := ruleIDs(fs)["HOOK-002"]
	if !ok {
		t.Fatalf("outside-HOME script must still score HOOK-002; findings=%v", keys(ruleIDs(fs)))
	}
	if _, ok := ruleIDs(notes)["COV-000"]; !ok {
		t.Errorf("the coverage note must stay alongside HOOK-002; notes=%v", keys(ruleIDs(notes)))
	}
	assertNoToken(t, pathToken, fs)
	assertNoToken(t, pathToken, notes)

	// Still the arrow form, and still informative: both sides name the script, and the side after
	// the arrow is shown redacted rather than dropped.
	snip := f.Evidence[0].Snippet
	ref, resolved, found := strings.Cut(snip, " → ")
	if !found {
		t.Fatalf("HOOK-002 snippet lost its arrow: %q", snip)
	}
	if !strings.HasSuffix(ref, "/hook.sh") || !strings.HasSuffix(resolved, "/hook.sh") {
		t.Errorf("both sides of the arrow must still name the script: %q", snip)
	}
	if !strings.Contains(resolved, "<REDACTED>") {
		t.Errorf("the resolved side must be shown redacted: %q", snip)
	}

	// The same rule at unit level, with paths fixed so the whole string can be pinned.
	a := model.ArtifactReport{Name: "PreToolUse[Bash]#1", Hook: model.Hook{Event: "PreToolUse"}}
	cases := []struct{ name, ref, resolved, want string }{
		{"absolute reference, both sides the same",
			"/opt/vault/" + pathToken + "/hook.sh",
			"/opt/vault/" + pathToken + "/hook.sh",
			"/opt/vault/<REDACTED>/hook.sh → /opt/vault/<REDACTED>/hook.sh"},
		{"expanded reference, the sides differ",
			"$HOME/../vault/" + pathToken + "/hook.sh",
			"/home/vault/" + pathToken + "/hook.sh",
			"$HOME/../vault/<REDACTED>/hook.sh → /home/vault/<REDACTED>/hook.sh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hookOutsideFinding(a, c.ref, c.resolved).Evidence[0].Snippet
			if got != c.want {
				t.Errorf("snippet\n  got  %q\n  want %q", got, c.want)
			}
		})
	}
}

// TestHookOutside_OrdinaryPathSnippetUnchanged is the reverse assertion: a hook pointing outside
// HOME at an ordinary path still scores HOOK-002 at the same weight, and its snippet is
// byte-identical to what the old formula produced — including the 200-byte clip.
func TestHookOutside_OrdinaryPathSnippetUnchanged(t *testing.T) {
	long := "/opt/" + strings.Repeat("deep/", 45) + "guard.sh"
	app := "/Applications/unibase-partner 3.app/Contents/Resources/hooks/unibase-hook.js"
	cases := []struct{ name, ref, resolved, want string }{
		{"absolute", "/opt/acme/hooks/guard.sh", "/opt/acme/hooks/guard.sh",
			"/opt/acme/hooks/guard.sh → /opt/acme/hooks/guard.sh"},
		{"application bundle with a space", app, app, app + " → " + app},
		{"tilde expanded", "~/../shared/hooks/guard.sh", "/Users/shared/hooks/guard.sh",
			"~/../shared/hooks/guard.sh → /Users/shared/hooks/guard.sh"},
		{"longer than the clip", long, long, long[:200] + "…"},
	}
	for _, event := range []string{"PreToolUse", eventPermissionRequest} {
		wantSev := model.SevMedium
		if event == eventPermissionRequest {
			wantSev = model.SevHigh
		}
		a := model.ArtifactReport{Name: event + "[*]#1", Hook: model.Hook{Event: event}}
		for _, c := range cases {
			t.Run(event+"/"+c.name, func(t *testing.T) {
				f := hookOutsideFinding(a, c.ref, c.resolved)
				if f.RuleID != "HOOK-002" || f.Dimension != 4 || f.Severity != wantSev {
					t.Errorf("got %s dim %d %s, want HOOK-002 dim 4 %s", f.RuleID, f.Dimension, f.Severity, wantSev)
				}
				got := f.Evidence[0].Snippet
				if got != c.want {
					t.Errorf("snippet\n  got  %q\n  want %q", got, c.want)
				}
				if legacy := legacyHookOutsideSnippet(c.ref, c.resolved); got != legacy {
					t.Errorf("an ordinary path's snippet changed\n  got    %q\n  before %q", got, legacy)
				}
			})
		}
	}
}

// TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts: when SUP-006 cannot read a host out of the
// target, its explanation quotes the target as written — credentials and all — while the snippet
// of the same line has them redacted. A token-shaped host label goes the same way.
func TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts(t *testing.T) {
	lines := map[string]string{
		"port is a variable":     "registry=https://ci:" + pathToken + "@npm.corp:${PORT}/",
		"no host after userinfo": "registry=https://ci:" + pathToken + "@/",
		"broken default value":   "registry=${R:-https://ci:" + pathToken + "@}",
		"token-shaped host":      "registry=https://" + pathToken + ".evil.example/",
	}
	for name, line := range lines {
		t.Run(name, func(t *testing.T) {
			fs := registryRedirects(".npmrc", line+"\n", map[int]bool{})
			if len(fs) != 1 || fs[0].RuleID != "SUP-006" {
				t.Fatalf("want one SUP-006, got %+v", fs)
			}
			assertNoToken(t, pathToken, fs)
			if !strings.Contains(fs[0].Why, "<REDACTED>") {
				t.Errorf("the explanation must still name the destination, redacted: %q", fs[0].Why)
			}
		})
	}
}

// TestRegistryRedirect_OrdinaryWhyUnchanged is the reverse assertion for SUP-006: same rule, same
// weight, and an explanation byte-identical to before for targets with nothing to redact.
func TestRegistryRedirect_OrdinaryWhyUnchanged(t *testing.T) {
	const tail = ". Every later install on this machine resolves through that host, which can serve anything " +
		"under a familiar package name. A skill has no reason to change where software comes from, and a " +
		"comment explaining why this one is fine does not change what the line does."
	cases := []struct{ line, want string }{
		{"registry=${CORP_REGISTRY}",
			".npmrc points the package manager at ${CORP_REGISTRY} (a value this file does not define — the " +
				"destination cannot be read here, which is not the same as safe)" + tail},
		{"registry=https://npm.evil.example/",
			".npmrc points the package manager at npm.evil.example" + tail},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			fs := registryRedirects(".npmrc", c.line+"\n", map[int]bool{})
			if len(fs) != 1 {
				t.Fatalf("want one finding, got %+v", fs)
			}
			f := fs[0]
			if f.RuleID != "SUP-006" || f.Dimension != 5 || f.Severity != model.SevHigh {
				t.Errorf("got %s dim %d %s, want SUP-006 dim 5 high", f.RuleID, f.Dimension, f.Severity)
			}
			if f.Why != c.want {
				t.Errorf("Why changed\n  got  %q\n  want %q", f.Why, c.want)
			}
		})
	}
}
