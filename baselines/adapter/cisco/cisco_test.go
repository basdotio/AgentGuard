// SPDX-License-Identifier: MIT

package cisco

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// The tests here run the adapter against a STUB that plays back canned exit codes and streams.
// They prove the wiring and, above all, the one contract this scanner breaks: `sarif.IsRunFailure`
// assumes exit 1 means "the gate fired". For skill-scanner, exit 1 ALSO means "the directory does
// not exist", "the taxonomy failed to load" and "SkillLoadError" (cli.py:546-803 returns only 0 and
// 1). Read at v2.1.0, unexecuted. A fold that trusted the exit code would score every unreadable
// sample as a finding — or, worse, every missing SKILL.md as a clean bill.

const sarifOneError = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"skill-scanner","rules":[]}},` +
	`"results":[{"ruleId":"PI-001","level":"error","message":{"text":"x"}}]}]}`

// stub writes a fake skill-scanner. Documents go in files and are cat'd: Go's %q is not shell
// quoting, and the ccaudit stub found that out the hard way.
func stub(t *testing.T, exitCode int, stdoutDoc, stderrText string) string {
	return stub2(t, exitCode, stdoutDoc, `{"is_safe":false,"max_severity":"HIGH","findings":[]}`, stderrText)
}

// stub2 answers the sarif pass and the json pass with different documents.
func stub2(t *testing.T, exitCode int, sarifDoc, jsonDoc, stderrText string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub is a shell script; baselines is darwin/linux only")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(out, []byte(sarifDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	jout := filepath.Join(dir, "json.txt")
	if err := os.WriteFile(jout, []byte(jsonDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	errf := filepath.Join(dir, "stderr.txt")
	if err := os.WriteFile(errf, []byte(stderrText), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "skill-scanner")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'skill-scanner 2.1.0'; exit 0; fi\n" +
		"for a in \"$@\"; do if [ \"$a\" = json ]; then cat '" + jout + "'; cat '" + errf + "' >&2; exit " + itoa(exitCode) + "; fi; done\n" +
		"cat '" + out + "'\n" +
		"cat '" + errf + "' >&2\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func policyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(p, []byte("benign_dotfiles: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestScanAlwaysReturnsARow — the one rule adapter.Adapter says is not negotiable.
func TestScanAlwaysReturnsARow(t *testing.T) {
	ad := &Adapter{Bin: "skill-scanner-does-not-exist-p019", Policy: policyFile(t)}
	row := ad.Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Errored || row.Detail == "" || row.Verdict != "" {
		t.Errorf("Outcome=%q Detail=%q Verdict=%q; a run that could not start is an Errored row "+
			"with a detail and no opinion", row.Outcome, row.Detail, row.Verdict)
	}
	if !row.Attempted {
		t.Error("Attempted=false: a reason may not be written for a scanner that was not invoked")
	}
}

// TestAnExitOfOneIsNotAlwaysTheGate is this adapter's headline criterion. Exit 1 carries two meanings
// for this tool, so the row must be decided by what it PRINTED, never by the code alone.
func TestAnExitOfOneIsNotAlwaysTheGate(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		stdout  string
		stderr  string
		outcome ledger.Outcome
		reason  ledger.Reason
		verdict string
	}{
		{
			// The reverse assertion first: when exit 1 IS the gate, it must still score.
			name: "exit 1 with SARIF on stdout is the gate firing",
			code: 1, stdout: sarifOneError,
			outcome: ledger.Scored, verdict: "malicious",
		},
		{
			name: "exit 0 with an empty SARIF run is a clean bill (no artifacts emitted, ever)",
			code: 0, stdout: `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"skill-scanner","rules":[]}},"results":[]}]}`,
			outcome: ledger.Scored, verdict: "benign",
		},
		{
			name: "exit 1 with SkillLoadError and no SARIF is the tool refusing the input shape",
			code: 1, stderr: "Error loading skill: No SKILL.md found in directory\n",
			outcome: ledger.NoVerdict, reason: ledger.UnsupportedInput,
		},
		{
			name: "exit 1 with a missing-directory error is a run failure",
			code: 1, stderr: "Error: Directory does not exist: /x\n",
			outcome: ledger.Errored,
		},
		{
			name: "exit 1 with a taxonomy error is a run failure",
			code: 1, stderr: "Error loading taxonomy configuration: boom\n",
			outcome: ledger.Errored,
		},
		{
			name: "exit 1 with unparseable stdout is a run failure, not a finding",
			code: 1, stdout: "Traceback (most recent call last):\n  KeyError\n",
			outcome: ledger.Errored,
		},
		{
			name: "exit 2 is a run failure whatever was printed",
			code: 2, stdout: sarifOneError,
			outcome: ledger.Errored,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ad := &Adapter{Bin: stub(t, tt.code, tt.stdout, tt.stderr), Policy: policyFile(t)}
			row := ad.Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
			if row.Outcome != tt.outcome {
				t.Fatalf("Outcome=%q want %q (detail: %s)", row.Outcome, tt.outcome, row.Detail)
			}
			if row.Reason != tt.reason {
				t.Errorf("Reason=%q want %q", row.Reason, tt.reason)
			}
			if row.Verdict != tt.verdict {
				t.Errorf("Verdict=%q want %q", row.Verdict, tt.verdict)
			}
			if row.Outcome != ledger.Scored && row.Detail == "" {
				t.Error("a row that is not scored must say why")
			}
		})
	}
}

// TestNoSkillMdIsUnsupportedInputNotAMiss — four of the corpus's six surfaces have no SKILL.md.
// In strict mode the loader raises SkillLoadError. That is "I do not read this shape", which the
// ledger has a word for; scoring it as benign would hand this tool a perfect false-positive rate
// on 3,220 samples it never looked at, and scoring it as an error would hide the coverage gap
// the surface tripwire exists to surface.
func TestNoSkillMdIsUnsupportedInputNotAMiss(t *testing.T) {
	ad := &Adapter{Bin: stub(t, 1, "", "Error loading skill: SKILL.md not found\n"), Policy: policyFile(t)}
	row := ad.Scan(context.Background(), corpus.Sample{Sample: "mcp-1", Surface: []string{"mcp"}}, t.TempDir())
	if row.Outcome != ledger.NoVerdict || row.Reason != ledger.UnsupportedInput {
		t.Fatalf("Outcome=%q Reason=%q; want no-verdict/unsupported-input", row.Outcome, row.Reason)
	}
	if !strings.Contains(row.Detail, "SKILL.md") {
		t.Errorf("detail does not carry the loader's own words:\n%s", row.Detail)
	}
	if len(row.Surface) != 1 || row.Surface[0] != "mcp" {
		t.Errorf("Surface=%v; the tripwire needs it carried through", row.Surface)
	}
}

// TestSeverityIsItsOwnLadder — --fail-on-severity takes critical|high|medium|low|info. A SARIF
// level or a cc-audit tier is not on it, and accepting one would silently gate at a rung nobody
// chose.
func TestSeverityIsItsOwnLadder(t *testing.T) {
	for in, want := range map[string]Severity{
		"critical": SeverityCritical, " HIGH ": SeverityHigh, "medium": SeverityMedium,
		"low": SeverityLow, "info": SeverityInfo,
	} {
		if got := ParseSeverity(in); got != want {
			t.Errorf("ParseSeverity(%q)=%q want %q", in, got, want)
		}
	}
	for _, in := range []string{"error", "warning", "default", "strict", "safe", ""} {
		if got := ParseSeverity(in); got != "" {
			t.Errorf("ParseSeverity(%q)=%q; want a refusal", in, got)
		}
	}
}

// TestVersionIsReadNotAssumed — a figure attributed to the wrong version is worse than none.
func TestVersionIsReadNotAssumed(t *testing.T) {
	if got, err := (&Adapter{Bin: "skill-scanner-does-not-exist-p019"}).Version(context.Background()); err == nil || got != "" {
		t.Errorf("Version=%q err=%v with no binary; want an error and an empty string", got, err)
	}
	got, err := (&Adapter{Bin: stub(t, 0, "", "")}).Version(context.Background())
	if err != nil || got != "skill-scanner 2.1.0" {
		t.Errorf("Version=%q err=%v", got, err)
	}
}

// TestPlacementNamesTheStrictModeChoice — the placement sentence lands in run.yaml. It must say
// that strict mode was kept and therefore that non-skill surfaces are refused, because that is
// the single decision that empties four rows of the comparison.
func TestPlacementNamesTheStrictModeChoice(t *testing.T) {
	p := (&Adapter{}).Placement()
	for _, want := range []string{"SKILL.md", "strict", "lenient"} {
		if !strings.Contains(p, want) {
			t.Errorf("Placement() does not mention %q:\n%s", want, p)
		}
	}
}

// TestSeverityComesFromTheJsonPass — SARIF collapses CRITICAL and HIGH into `error`, so severity
// on the row must come from the tool's own ladder in the JSON pass. Reading it from SARIF would
// report every critical finding as high (probed 2026-09-24: the .pyc sample is HIGH in JSON and
// `error` in SARIF; a CRITICAL one would look identical there).
func TestSeverityComesFromTheJsonPass(t *testing.T) {
	bin := stub2(t, 1, sarifOneError, `{"is_safe":false,"max_severity":"CRITICAL","findings":[{"rule_id":"X","severity":"CRITICAL","category":"malware"}]}`, "")
	row := (&Adapter{Bin: bin, Policy: policyFile(t)}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Scored || row.Verdict != "malicious" {
		t.Fatalf("Outcome=%q Verdict=%q (%s)", row.Outcome, row.Verdict, row.Detail)
	}
	if row.Severity != "critical" {
		t.Errorf("Severity=%q, want critical from the JSON pass, not SARIF's error", row.Severity)
	}
	// open question 9: five categories mapped by name (n too small to measure); malware is not one of them.
	if len(row.Dimensions) != 0 {
		t.Errorf("Dimensions=%v; `malware` is unmeasured and unmapped", row.Dimensions)
	}
}

// TestTheTwoPassesMustAgree — flagged by SARIF yet is_safe=true in JSON is the two reporters
// disagreeing about the same scan. Errored carrying both; never one preferred.
func TestTheTwoPassesMustAgree(t *testing.T) {
	bin := stub2(t, 1, sarifOneError, `{"is_safe":true,"max_severity":"INFO","findings":[]}`, "")
	row := (&Adapter{Bin: bin, Policy: policyFile(t)}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Errored || row.Verdict != "" {
		t.Fatalf("Outcome=%q Verdict=%q; want Errored with no verdict", row.Outcome, row.Verdict)
	}
	for _, want := range []string{"disagree", "is_safe=true", "INFO"} {
		if !strings.Contains(row.Detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, row.Detail)
		}
	}
	// benign + is_safe=false is ordinary: MANIFEST_MISSING_LICENSE (INFO) flips is_safe on 19/20 skills.
	bin = stub2(t, 0, `{"version":"2.1.0","runs":[{"tool":{"driver":{"rules":[]}},"results":[]}]}`, `{"is_safe":false,"max_severity":"INFO","findings":[]}`, "")
	row = (&Adapter{Bin: bin, Policy: policyFile(t)}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Scored || row.Verdict != "benign" || row.Severity != "info" {
		t.Errorf("Outcome=%q Verdict=%q Severity=%q; want scored/benign/info", row.Outcome, row.Verdict, row.Severity)
	}
}

// TestDimensionsComeOnlyFromFlaggingFindingsAndOnlyFromTheFiveNamed — the five name-based
// mappings apply; the twelve nulls do not; and a MEDIUM finding never contributes, because the
// gate at `high` never showed it to anyone.
func TestDimensionsComeOnlyFromFlaggingFindingsAndOnlyFromTheFiveNamed(t *testing.T) {
	res := ScanResult{Findings: []Finding{
		{Severity: "CRITICAL", Category: "data_exfiltration"},
		{Severity: "HIGH", Category: "command_injection"},
		{Severity: "HIGH", Category: "obfuscation"},        // null
		{Severity: "MEDIUM", Category: "prompt_injection"}, // below the gate
	}}
	got := dimensionsOf(res)
	want := []string{"execution", "exfiltration"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("dimensionsOf=%v want %v", got, want)
	}
	if len(DimensionMap) != 17 {
		t.Errorf("DimensionMap has %d entries; models.py declares 17 categories", len(DimensionMap))
	}
	named := 0
	for _, v := range DimensionMap {
		if v != "" {
			named++
		}
	}
	if named != 5 {
		t.Errorf("%d categories mapped; open question 9 decided exactly five, by name", named)
	}
}
