// SPDX-License-Identifier: MIT

package cisco

import (
	"context"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/adapter"
)

func TestTraverseOnlyHasTwoWaysToPassAndOneToFail(t *testing.T) {
	// An INFO-only report is silence: MANIFEST_MISSING_LICENSE fires on 19 of 20 skills.
	fail := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{IsSafe: false, MaxSeverity: "INFO",
		Findings: []Finding{{RuleID: "MANIFEST_MISSING_LICENSE", Severity: "INFO"}}}, nil, nil)
	if fail.Status != adapter.FixtureFail || !strings.Contains(fail.Detail, "curl | sh") {
		t.Errorf("INFO-only: Status=%q Detail=%s", fail.Status, fail.Detail)
	}
	pass := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{MaxSeverity: "HIGH",
		Findings: []Finding{{RuleID: "X", Severity: "HIGH"}}}, nil, nil)
	if pass.Status != adapter.FixturePass {
		t.Errorf("a HIGH finding: Status=%q", pass.Status)
	}
	said := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{MaxSeverity: "INFO"}, nil, []byte("warning: private/: Permission denied\n"))
	if said.Status != adapter.FixturePass {
		t.Errorf("a disclosure on stderr: Status=%q", said.Status)
	}
}

// TestARefusedTreeIsUntestableNotAPass — strict mode refusing a fixture means the walker never
// ran. Calling that a pass would credit the tool with surviving a trap it never entered.
func TestARefusedTreeIsUntestableNotAPass(t *testing.T) {
	bin := stub2(t, 1, "", "", "Error loading skill: SKILL.md not found in /x\n")
	got := (&Adapter{Bin: bin, Policy: policyFile(t)}).Fixture(context.Background(), "symlink-cycle", t.TempDir())
	if got.Status != adapter.FixtureUntestable || !strings.Contains(got.Detail, "refused") {
		t.Errorf("Status=%q Detail=%s", got.Status, got.Detail)
	}
}

// TestARobustnessFixtureThatTerminatesPasses — the reverse assertion for the failures.
func TestARobustnessFixtureThatTerminatesPasses(t *testing.T) {
	bin := stub2(t, 0, "{}", `{"is_safe":true,"max_severity":"INFO","findings":[]}`, "")
	for _, name := range []string{"fifo-as-skill", "symlink-cycle", "symlink-escape", "deeply-nested", "sparse-huge-config"} {
		got := (&Adapter{Bin: bin, Policy: policyFile(t)}).Fixture(context.Background(), name, t.TempDir())
		if got.Status != adapter.FixturePass {
			t.Errorf("%s: Status=%q Detail=%s", name, got.Status, got.Detail)
		}
	}
	got := (&Adapter{Bin: bin, Policy: policyFile(t)}).Fixture(context.Background(), "a-fixture-from-the-future", t.TempDir())
	if got.Status != adapter.FixtureUntestable {
		t.Errorf("unknown fixture: Status=%q", got.Status)
	}
	if len(FixtureNames()) != 6 {
		t.Errorf("FixtureNames has %d entries; the corpus ships 6", len(FixtureNames()))
	}
}

// TestTheFifoRefusalIsThePassCondition — the corpus asks a scanner to "stat the entry, see it is
// not a regular file, and skip or time out". skill-scanner does exactly that, and the first
// judgement here called it untestable because the sentence began with "Error loading skill".
func TestTheFifoRefusalIsThePassCondition(t *testing.T) {
	bin := stub2(t, 1, "", "", "Error loading skill: Path is not a regular file: /x/SKILL.md\n")
	got := (&Adapter{Bin: bin, Policy: policyFile(t)}).Fixture(context.Background(), "fifo-as-skill", t.TempDir())
	if got.Status != adapter.FixturePass || !strings.Contains(got.Detail, "did not block") {
		t.Errorf("Status=%q Detail=%s", got.Status, got.Detail)
	}
	// The same refusal on any OTHER fixture is still untestable: only the FIFO one asks for it.
	got = (&Adapter{Bin: bin, Policy: policyFile(t)}).Fixture(context.Background(), "symlink-cycle", t.TempDir())
	if got.Status != adapter.FixtureUntestable {
		t.Errorf("symlink-cycle with a not-a-regular-file refusal: Status=%q", got.Status)
	}
}
