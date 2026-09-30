// SPDX-License-Identifier: MIT
package gate

import (
	"strings"
	"testing"
	"time"
)

// TestUnderDeadline pins the deadline mechanism: a step that never returns yields an error naming
// the step once the deadline passes; a fast step passes its result through untouched.
func TestUnderDeadline(t *testing.T) {
	start := time.Now()
	_, err := underDeadline(50*time.Millisecond, "resolving the skill", func() (string, error) {
		select {} // never returns
	})
	if err == nil || !strings.Contains(err.Error(), "resolving the skill") || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("stalled step must fail naming the step, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("deadline did not fire promptly")
	}
	got, err := underDeadline(time.Second, "x", func() (int, error) { return 7, nil })
	if err != nil || got != 7 {
		t.Fatalf("fast step must pass through: %d %v", got, err)
	}
}
