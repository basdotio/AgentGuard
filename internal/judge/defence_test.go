// SPDX-License-Identifier: MIT
package judge

import (
	"strings"
	"testing"
)

// P-088: the barrier rule says that an artifact's own prompt-injection defence is addressed to its
// agent, not to the analyzer — on every pass, since the rule is in every system message.
func TestBarrierRule_ADefenceIsNotAnAddress(t *testing.T) {
	for _, m := range passModes {
		sys := systemPrompt(m, "n")
		for _, want := range []string{"prompt-injection defence", "addressed to that agent, not to you", "NOT barrier_evidence"} {
			if !strings.Contains(sys, want) {
				t.Errorf("mode %d: the system message does not say %q", m, want)
			}
		}
	}
}
