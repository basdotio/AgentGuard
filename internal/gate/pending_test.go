// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/basdotio/AgentGuard/internal/model"
)

// Every hook event Claude Code fires runs the hook command as a NEW process. A verdict that
// PreToolUse parks in Store.Pending therefore reaches PostToolUse only through the approvals
// file — and the tests above (TestGateAsksThenRemembers, TestApprovalOnlyCoversWhatWasShown)
// hand ONE in-memory Store to both events, so they could not see that LoadStore never read
// the pending entries back. The tests here give every event a store freshly loaded from disk,
// which is the only state a real hook process starts with.

// wallNow is the clock the cross-process tests run on: LoadStore judges expiry against the
// process's own clock, so the gate's injected clock has to agree with it.
func wallNow() int64 { return time.Now().Unix() }

// nextProcess is what the next hook invocation sees: nothing survives but the file.
func nextProcess(o Options) Options {
	o.Store = LoadStore(ApprovalsPath(o.Root))
	return o
}

// handleAndSave runs one hook event the way runHook does — Handle, then Save if dirty.
func handleAndSave(t *testing.T, ev Event, o Options) Output {
	t.Helper()
	out, dirty := Handle(ev, o)
	if dirty {
		if err := o.Store.Save(); err != nil {
			t.Fatalf("save after %s: %v", ev.HookEventName, err)
		}
	}
	return out
}

func preCall(skill, id string) Event {
	return Event{HookEventName: EventPreToolUse, ToolName: SkillTool, ToolUseID: id,
		ToolInput: json.RawMessage(`{"skill":"` + skill + `"}`)}
}

func postCall(skill, id string) Event {
	return Event{HookEventName: EventPostToolUse, ToolName: SkillTool, ToolUseID: id,
		ToolInput: json.RawMessage(`{"skill":"` + skill + `"}`), ToolResponse: json.RawMessage(`{"success":true}`)}
}

func riskyScan(hash string) Scanner {
	return func(string) (model.ScanResult, error) {
		return result("evil", hash, 42, finding("EXFIL-001", model.SevHigh, 3)), nil
	}
}

// crossProcessOpts is newOpts on the wall clock, with a skill directory to resolve.
func crossProcessOpts(t *testing.T, scan Scanner) Options {
	t.Helper()
	o, _ := newOpts(t, scan)
	o.Now = wallNow
	mustSkillDir(t, o.Root, "evil")
	return o
}

// writeStoreFile writes an approvals file by hand, for the rows the gate itself would never write.
func writeStoreFile(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(ApprovalsPath(root), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pendingJSON(id, hash string, askedAt int64) string {
	b, err := json.Marshal(map[string]Pending{id: {Hash: hash, Name: "evil", Kind: "skill", Path: "/tmp/evil", Score: 42, AskedAt: askedAt}})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestPendingSurvivesSaveAndLoad: a parked verdict has to come back out of the file intact,
// because the process that parked it is gone by the time anyone answers the prompt.
func TestPendingSurvivesSaveAndLoad(t *testing.T) {
	p := ApprovalsPath(t.TempDir())
	s := LoadStore(p)
	now := wallNow()
	s.pend("toolu_1", Verdict{Hash: "h1", Name: "evil", Kind: "skill", Path: "/tmp/evil", Score: 42}, now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadStore(p).pendingFor("toolu_1")
	if !ok {
		t.Fatal("a pending verdict written to the store was not read back — PostToolUse runs in a new process and will find nothing")
	}
	want := Pending{Hash: "h1", Name: "evil", Kind: "skill", Path: "/tmp/evil", Score: 42, AskedAt: now}
	if got != want {
		t.Errorf("pending verdict changed on the way through the file:\n got %+v\nwant %+v", got, want)
	}
}

// TestPostPromotesAcrossProcesses is the user-visible contract — "answer yes once and the same
// bytes load silently from then on" — with each event in its own process.
func TestPostPromotesAcrossProcesses(t *testing.T) {
	o := crossProcessOpts(t, riskyScan("aaaa1111"))

	out := handleAndSave(t, preCall("evil", "toolu_1"), o)
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.PermissionDecision != DecisionAsk {
		t.Fatalf("risky skill did not stop the load: %+v", out)
	}

	o = nextProcess(o)
	out = handleAndSave(t, postCall("evil", "toolu_1"), o)
	o = nextProcess(o)
	a, ok := o.Store.Approved("aaaa1111")
	if !ok {
		t.Fatal("the operator said yes at the prompt and nothing was recorded — the same bytes will be asked about again")
	}
	if a.Verdict != VerdictAccepted {
		t.Errorf("verdict = %q, want %q", a.Verdict, VerdictAccepted)
	}
	if !strings.Contains(out.SystemMessage, "risk accepted") {
		t.Errorf("PostToolUse must tell the operator the answer was recorded, got %q", out.SystemMessage)
	}
	if _, still := o.Store.pendingFor("toolu_1"); still {
		t.Error("a promoted verdict stayed parked")
	}

	if out = handleAndSave(t, preCall("evil", "toolu_2"), nextProcess(o)); out.HookSpecificOutput != nil || out.SystemMessage != "" {
		t.Errorf("approved bytes must load silently in the next process, got %+v", out)
	}
}

// TestPendingHygieneOnLoad: the file is written by this gate on every risky load, but it is
// also a file anyone can edit. Rows the gate's own clock could not have written are dropped on
// the way in, the same way approvals whose key disagrees with their hash are — dropping one
// costs a prompt, keeping one keeps a record no rule will ever expire.
func TestPendingHygieneOnLoad(t *testing.T) {
	now := wallNow()
	rows := map[string]Pending{
		"fresh":     {Hash: "h1", AskedAt: now},
		"near-ttl":  {Hash: "h2", AskedAt: now - pendingTTL + 60},
		"expired":   {Hash: "h3", AskedAt: now - pendingTTL - 60},
		"no-hash":   {Hash: "", AskedAt: now},
		"undated":   {Hash: "h4", AskedAt: 0},
		"from-2099": {Hash: "h5", AskedAt: now + 3600},
		"":          {Hash: "h6", AskedAt: now},
	}
	pending, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeStoreFile(t, dir, `{"version":1,"approvals":{"forged":{"hash":"real"},"real":{"hash":"real"}},"pending":`+string(pending)+`}`)

	s := LoadStore(ApprovalsPath(dir))
	if s.Corrupt != "" {
		t.Fatalf("a well-formed file read as corrupt: %s", s.Corrupt)
	}
	for id, keep := range map[string]bool{
		"fresh": true, "near-ttl": true,
		"expired": false, "no-hash": false, "undated": false, "from-2099": false, "": false,
	} {
		if _, ok := s.pendingFor(id); ok != keep && id != "" {
			t.Errorf("pending %q: kept=%v, want %v", id, ok, keep)
		}
		if _, ok := s.Pending[id]; ok != keep {
			t.Errorf("pending row %q in the loaded map: %v, want %v", id, ok, keep)
		}
	}
	// The approvals half of the same file is untouched by this change.
	if _, ok := s.Approved("forged"); ok {
		t.Error("an approval whose key disagrees with its hash was honoured")
	}
	if _, ok := s.Approved("real"); !ok {
		t.Error("a consistent approval was dropped")
	}

	// A pending section that does not parse spoils the whole file, exactly like a broken
	// approvals section: the store reads as empty and is not overwritten.
	writeStoreFile(t, dir, `{"version":1,"approvals":{"real":{"hash":"real"}},"pending":"not a map"}`)
	s = LoadStore(ApprovalsPath(dir))
	if s.Corrupt == "" || len(s.Approvals) != 0 || len(s.Pending) != 0 {
		t.Errorf("a malformed pending section must make the store corrupt-and-empty: corrupt=%q approvals=%d pending=%d",
			s.Corrupt, len(s.Approvals), len(s.Pending))
	}
}

// TestPendingHashIsComparedNotTrusted: PostToolUse records a hash only after re-scanning and
// finding the same one. A pending row that names a hash the scan does not produce — a swap
// after the prompt, or a hand-written row — approves nothing at all.
func TestPendingHashIsComparedNotTrusted(t *testing.T) {
	o := crossProcessOpts(t, riskyScan("ondisk00"))
	writeStoreFile(t, o.Root, `{"version":1,"approvals":{},"pending":`+pendingJSON("toolu_1", "claimed0", wallNow())+`}`)

	out := handleAndSave(t, postCall("evil", "toolu_1"), nextProcess(o))
	s := LoadStore(ApprovalsPath(o.Root))
	if len(s.Approvals) != 0 {
		t.Fatalf("approved a hash this process did not confirm: %v", s.Approvals)
	}
	// Not vacuous: the message is only written once the pending row was found and compared.
	if !strings.Contains(out.SystemMessage, "changed between the prompt and the load") {
		t.Errorf("the mismatch must be reported, got %q", out.SystemMessage)
	}
}

// TestChangedBytesAreNotPromotedAcrossProcesses is TestApprovalOnlyCoversWhatWasShown with a
// process boundary between the prompt and the answer.
func TestChangedBytesAreNotPromotedAcrossProcesses(t *testing.T) {
	o := crossProcessOpts(t, riskyScan("before00"))
	handleAndSave(t, preCall("evil", "toolu_1"), o)

	o = nextProcess(o)
	o.Scan = riskyScan("after000") // swapped underneath, between prompt and load
	out := handleAndSave(t, postCall("evil", "toolu_1"), o)

	s := LoadStore(ApprovalsPath(o.Root))
	if len(s.Approvals) != 0 {
		t.Fatalf("approved bytes the operator was never shown: %v", s.Approvals)
	}
	if !strings.Contains(out.SystemMessage, "changed between the prompt and the load") {
		t.Errorf("the swap must be reported, got %q", out.SystemMessage)
	}
}

// TestExpiredPendingIsNotPromoted: the scan reproduces the parked hash exactly, so the ONLY
// thing that differs between the two cases is the age of the prompt.
func TestExpiredPendingIsNotPromoted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		age     int64
		promote bool
	}{
		{"answered within the hour", pendingTTL - 60, true},
		{"answered after the hour", pendingTTL + 60, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := crossProcessOpts(t, riskyScan("same0000"))
			writeStoreFile(t, o.Root, `{"version":1,"approvals":{},"pending":`+pendingJSON("toolu_1", "same0000", wallNow()-tc.age)+`}`)

			handleAndSave(t, postCall("evil", "toolu_1"), nextProcess(o))
			if _, ok := LoadStore(ApprovalsPath(o.Root)).Approved("same0000"); ok != tc.promote {
				t.Errorf("prompt %ds old: promoted=%v, want %v", tc.age, ok, tc.promote)
			}
		})
	}
}

// TestPostForAnotherCallPromotesNothing: pending is keyed by tool_use_id so that each answer
// resolves against its own call. Someone else's PostToolUse neither approves this prompt's
// bytes nor consumes the prompt.
func TestPostForAnotherCallPromotesNothing(t *testing.T) {
	o := crossProcessOpts(t, riskyScan("aaaa1111"))
	handleAndSave(t, preCall("evil", "toolu_1"), o)

	handleAndSave(t, postCall("evil", "toolu_2"), nextProcess(o))
	s := LoadStore(ApprovalsPath(o.Root))
	if len(s.Approvals) != 0 {
		t.Fatalf("a PostToolUse for another call approved this one: %v", s.Approvals)
	}
	if _, ok := s.pendingFor("toolu_1"); !ok {
		t.Error("another call's PostToolUse consumed this call's pending verdict")
	}
}

// TestMediumPassIsNotRememberedAcrossProcesses: a pass below the threshold with a medium
// finding parks nothing, so no PostToolUse — in this process or the next — can remember it.
func TestMediumPassIsNotRememberedAcrossProcesses(t *testing.T) {
	o := crossProcessOpts(t, func(string) (model.ScanResult, error) {
		return result("evil", "medium00", 88, finding("OBF-006", model.SevMedium, 6)), nil
	})
	out := handleAndSave(t, preCall("evil", "toolu_1"), o)
	if out.HookSpecificOutput != nil {
		t.Fatalf("a medium finding is below threshold high and must not block: %+v", out.HookSpecificOutput)
	}

	handleAndSave(t, postCall("evil", "toolu_1"), nextProcess(o))
	if _, ok := LoadStore(ApprovalsPath(o.Root)).Approved("medium00"); ok {
		t.Fatal("content with a medium finding was remembered after its PostToolUse")
	}
}
