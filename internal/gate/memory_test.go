// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

// Four malicious samples cleared the gate on a medium finding each, and the gate then
// recorded their hashes as trusted — so the same bytes would never be looked at again. The
// pass itself is the threshold's decision and stays; what changes is memory. A pass with a
// deterministic finding at medium or above is NOT remembered: it is announced, and it is
// re-audited on every load until the content is clean or a human approves it explicitly.

func preLoad(name string) Event {
	return Event{HookEventName: EventPreToolUse, ToolName: SkillTool, ToolUseID: "c1",
		ToolInput: json.RawMessage(`{"skill":"` + name + `"}`)}
}

func TestPassWithMediumFindingIsNotRemembered(t *testing.T) {
	o, store := newOpts(t, nil)
	mustSkillDir(t, o.Root, "shapey")
	o.Scan = func(string) (model.ScanResult, error) {
		return result("shapey", "medium00", 88, finding("OBF-006", model.SevMedium, 6)), nil
	}

	out, dirty := Handle(preLoad("shapey"), o)
	if out.HookSpecificOutput != nil {
		t.Fatalf("a medium finding is below threshold high and must not block: %+v", out.HookSpecificOutput)
	}
	if _, ok := store.Approved("medium00"); ok {
		t.Fatal("content with a medium finding was recorded as trusted")
	}
	if dirty {
		t.Error("nothing was recorded, so the store must not be marked dirty")
	}
	msg := out.SystemMessage
	if strings.Contains(msg, "trusted from now on") {
		t.Errorf("the pass message promises memory it does not have: %q", msg)
	}
	for _, want := range []string{"OBF-006", "not recorded", "aguard approve"} {
		if !strings.Contains(msg, want) {
			t.Errorf("pass message lacks %q: %q", want, msg)
		}
	}

	// Second load of the same bytes: no silence, the same notice again.
	out2, _ := Handle(preLoad("shapey"), o)
	if out2.SystemMessage == "" {
		t.Fatal("an unremembered pass must be announced on every load, not once")
	}
}

func TestCleanPassIsStillRemembered(t *testing.T) {
	o, store := newOpts(t, nil)
	mustSkillDir(t, o.Root, "clean")
	o.Scan = func(string) (model.ScanResult, error) { return result("clean", "clean000", 100), nil }

	out, dirty := Handle(preLoad("clean"), o)
	if a, ok := store.Approved("clean000"); !ok || a.Verdict != VerdictClean || !dirty {
		t.Fatalf("a clean pass must be recorded as clean: ok=%v verdict=%q dirty=%v", ok, a.Verdict, dirty)
	}
	if !strings.Contains(out.SystemMessage, "trusted from now on") {
		t.Errorf("clean pass message = %q", out.SystemMessage)
	}
	if out2, _ := Handle(preLoad("clean"), o); out2.SystemMessage != "" || out2.HookSpecificOutput != nil {
		t.Errorf("remembered content must load silently, got %+v", out2)
	}
}

// The boundary is medium. A low-only finding is informational in every other part of the
// tool (it does not move the level), and treating it as unforgettable would make the gate
// re-announce most real skills on every load — the cost that gets gates uninstalled.
func TestLowOnlyPassIsRemembered(t *testing.T) {
	o, store := newOpts(t, nil)
	mustSkillDir(t, o.Root, "lowish")
	o.Scan = func(string) (model.ScanResult, error) {
		return result("lowish", "low00000", 97, finding("OBF-007", model.SevLow, 6)), nil
	}
	Handle(preLoad("lowish"), o)
	if _, ok := store.Approved("low00000"); !ok {
		t.Fatal("a low-only pass must still be remembered")
	}
}

// An LLM finding never reaches the gate, but if one is present in a result it must not decide
// memory either — the same predicate as blocking (score.Deterministic).
func TestLLMFindingDoesNotDecideMemory(t *testing.T) {
	o, store := newOpts(t, nil)
	mustSkillDir(t, o.Root, "judged")
	llm := finding("INJ-900", model.SevHigh, 1)
	llm.Source = model.SrcLLM
	o.Scan = func(string) (model.ScanResult, error) { return result("judged", "llm00000", 100, llm), nil }
	Handle(preLoad("judged"), o)
	if _, ok := store.Approved("llm00000"); !ok {
		t.Fatal("an LLM-only finding must not prevent remembering: the gate never runs the judge")
	}
}
