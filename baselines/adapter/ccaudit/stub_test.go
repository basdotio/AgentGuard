// SPDX-License-Identifier: MIT

package ccaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
)

// The tests below run the adapter end to end against a STUB that emits canned documents. They
// prove the wiring — argv construction, two passes, which document decides what, raw files —
// and they prove NOTHING about cc-audit itself. Every claim about the real tool's behaviour is
// owed to W6 on the pinned binary; conflating the two is how a rig ends up measuring itself.
//
// A stub is used rather than a mock because the thing most likely to be wrong is the argv and
// the process contract (exit codes, which stream carries what), and an interface seam would
// test neither.

// stub writes a fake scanner that answers the sarif pass and the json pass differently, exits
// with the given code, and records the argv it was called with.
func stub(t *testing.T, exitCode int, sarifDoc, jsonDoc string) (bin, argvLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub is a shell script; baselines is darwin/linux only")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "cc-audit-stub")
	argvLog = filepath.Join(dir, "argv.log")
	// The documents go in files rather than inline in the script: a JSON document embedded in
	// shell needs shell quoting, and Go's %q is Go quoting — the difference silently turns the
	// canned SARIF into a parse error, which then looks exactly like the failure this package
	// is supposed to detect.
	sarifPath := filepath.Join(dir, "canned.sarif")
	jsonPath := filepath.Join(dir, "canned.json")
	for path, doc := range map[string]string{sarifPath: sarifDoc, jsonPath: jsonDoc} {
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatalf("write stub document %s: %v", path, err)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> '%s'
for a in "$@"; do
  if [ "$a" = "json" ]; then cat '%s'; exit %d; fi
done
cat '%s'
exit %d
`, argvLog, jsonPath, exitCode, sarifPath, exitCode)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return bin, argvLog
}

func sarifWith(level, ruleID string) string {
	return fmt.Sprintf(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"cc-audit"}},
	"artifacts":[{"location":{"uri":"SKILL.md"}}],
	"results":[{"ruleId":%q,"level":%q,"message":{"text":"x"}}]}]}`, ruleID, level)
}

func jsonWith(t *testing.T, total int, sum Summary) string {
	t.Helper()
	doc := map[string]any{
		"version": "0.2.0", "target": "./s/", "summary": sum,
		"findings": []any{}, "elapsed_ms": 1,
	}
	if total >= 0 {
		doc["risk_score"] = map[string]any{"total": total, "level": "high"}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal stub json: %v", err)
	}
	return string(b)
}

// TestTheTwoPassesAreWiredToTheRightDocument is the wiring assertion: the verdict must come from
// SARIF and the severity from JSON. Handing the stub a SARIF that says `error` and a JSON whose
// own ladder says `critical` is the only way to tell which document each field came from — if
// severity came from SARIF it would read "error", and cc-audit's critical findings would all be
// reported one rung low.
func TestTheTwoPassesAreWiredToTheRightDocument(t *testing.T) {
	// Exit 1 on purpose: it is the gate firing, which is the EXPECTED outcome on a malicious
	// sample. A run that treated it as failure would report zero recall.
	bin, argvLog := stub(t, 1, sarifWith("error", "EX-001"), jsonWith(t, 60, Summary{Critical: 1, High: 2}))
	ad := &Adapter{Bin: bin, RawDir: t.TempDir()}

	row := ad.Scan(context.Background(), corpus.Sample{Sample: "s1", Surface: []string{"skill"}}, t.TempDir())

	if row.Outcome != ledger.Scored {
		t.Fatalf("Outcome = %q (%s), want scored", row.Outcome, row.Detail)
	}
	if row.Verdict != "malicious" {
		t.Errorf("Verdict = %q, want malicious from the SARIF pass", row.Verdict)
	}
	if row.Severity != "critical" {
		t.Errorf("Severity = %q, want critical from the JSON pass — SARIF's `error` covers both "+
			"critical and high, so severity read from SARIF understates the tool", row.Severity)
	}
	if len(row.Rules) != 1 || row.Rules[0] != "EX-001" {
		t.Errorf("Rules = %v, want the rule that carried the flag", row.Rules)
	}
	if len(row.Surface) != 1 || row.Surface[0] != "skill" {
		t.Errorf("Surface = %v, want it carried through for the tripwire", row.Surface)
	}

	// Both passes ran, over the same tree, and the default tier added no flags.
	log, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 {
		t.Fatalf("stub was called %d times, want 2 (sarif then json):\n%s", len(lines), log)
	}
	if !strings.Contains(lines[0], "sarif") || !strings.Contains(lines[1], "json") {
		t.Errorf("passes ran in the wrong order or with the wrong formats:\n%s", log)
	}
	for _, line := range lines {
		if strings.Contains(line, "--strict") {
			t.Errorf("the default tier passed --strict: %q", line)
		}
		if !strings.HasPrefix(line, "check ") {
			t.Errorf("argv does not start with the check subcommand: %q", line)
		}
	}
}

// TestRawDocumentsAreKeptForBothPasses — the raw files are what makes a published figure
// arguable after the fact. Keeping only one of the two would leave the score unverifiable.
func TestRawDocumentsAreKeptForBothPasses(t *testing.T) {
	raw := t.TempDir()
	bin, _ := stub(t, 0, sarifWith("note", "OP-001"), jsonWith(t, 5, Summary{Low: 1}))
	ad := &Adapter{Bin: bin, RawDir: raw}

	ad.Scan(context.Background(), corpus.Sample{Sample: "s2"}, t.TempDir())

	for _, name := range []string{"s2.sarif.json", "s2.json"} {
		if _, err := os.Stat(filepath.Join(raw, name)); err != nil {
			t.Errorf("raw document %s not kept: %v", name, err)
		}
	}
}

// TestTheStrictTierChangesBothTheArgvAndTheThreshold — a `note` finding is below the default
// gate and at the strict one. If the tier only changed the flag and not the fold (or the other
// way round), the two tiers would report the same numbers and the choice would be decorative.
func TestTheStrictTierChangesBothTheArgvAndTheThreshold(t *testing.T) {
	doc := sarifWith("note", "OP-001")
	scoreDoc := jsonWith(t, 5, Summary{Low: 1})

	binDefault, _ := stub(t, 0, doc, scoreDoc)
	got := (&Adapter{Bin: binDefault}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if got.Verdict != "benign" {
		t.Errorf("default tier flagged a `note` finding: %q", got.Verdict)
	}

	binStrict, argvLog := stub(t, 0, doc, scoreDoc)
	got = (&Adapter{Bin: binStrict, Tier: TierStrict}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if got.Verdict != "malicious" {
		t.Errorf("strict tier did not flag a `note` finding: %q", got.Verdict)
	}
	log, _ := os.ReadFile(argvLog)
	if !strings.Contains(string(log), "--strict") {
		t.Errorf("strict tier did not reach the command line:\n%s", log)
	}
}

// TestOutputThatIsNotSarifIsAnErrorNotAVerdict — an unparseable document must never fold to
// benign. On 3,220 benign samples that would look like a perfect false-positive rate; on 300
// malicious ones, like total blindness. Either way the number would be about our parser.
func TestOutputThatIsNotSarifIsAnErrorNotAVerdict(t *testing.T) {
	bin, _ := stub(t, 0, "not json at all", "{}")
	row := (&Adapter{Bin: bin}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Errored {
		t.Fatalf("Outcome = %q, want %q", row.Outcome, ledger.Errored)
	}
	if !strings.Contains(row.Detail, "not SARIF") {
		t.Errorf("detail does not say what was wrong with the output:\n%s", row.Detail)
	}
	if row.Verdict != "" {
		t.Errorf("Verdict = %q on an unparseable document", row.Verdict)
	}
}

// TestAFailedJsonPassDiscardsTheVerdict — the verdict alone is not what this adapter promises. If the
// score cannot be paired with it, saying so beats publishing half the row, because a missing
// score is invisible in the aggregate while a wrong one is not.
func TestAFailedJsonPassDiscardsTheVerdict(t *testing.T) {
	// Exit 2 on the json pass only: a run failure, not a gate firing.
	dir := t.TempDir()
	bin := filepath.Join(dir, "cc-audit-stub")
	sarifPath := filepath.Join(dir, "canned.sarif")
	if err := os.WriteFile(sarifPath, []byte(sarifWith("error", "EX-001")), 0o644); err != nil {
		t.Fatalf("write stub document: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "json" ]; then echo "boom" >&2; exit 2; fi
done
cat '%s'
exit 1
`, sarifPath)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	row := (&Adapter{Bin: bin}).Scan(context.Background(), corpus.Sample{Sample: "s"}, t.TempDir())
	if row.Outcome != ledger.Errored {
		t.Fatalf("Outcome = %q, want %q", row.Outcome, ledger.Errored)
	}
	if row.Verdict != "" {
		t.Errorf("Verdict = %q survived a failed JSON pass", row.Verdict)
	}
	for _, want := range []string{"SARIF pass succeeded", "JSON pass did not", "boom"} {
		if !strings.Contains(row.Detail, want) {
			t.Errorf("detail does not carry %q:\n%s", want, row.Detail)
		}
	}
}

// TestVersionIsTheFirstLineOfWhateverTheBinarySays — with a release a day, the version string is
// the only thing tying a published figure to a reproducible artifact.
func TestVersionIsTheFirstLineOfWhateverTheBinarySays(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cc-audit-stub")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'cc-audit 3.23.9'\necho 'extra noise'\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	got, err := (&Adapter{Bin: bin}).Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got != "cc-audit 3.23.9" {
		t.Errorf("Version = %q, want the first line only", got)
	}
}
