// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// parseFailedHook is the scan a Skill event gets when its name resolves to a directory shaped
// like a config root (it holds plugins/installed_plugins.json) whose settings.json does not
// parse: the target routes to the root collectors, and the worst artifact is the PARSE-000
// hook. The collector gives it hash "" — content that was not read has no identity to
// approve; the hash is a parameter only so the reverse assertion can vary that one field.
func parseFailedHook(hash string) model.ScanResult {
	const file = "/cfg/skills/x/settings.json"
	return model.ScanResult{Artifacts: []model.ArtifactReport{{
		Kind: model.KindHook, Name: "settings.json", Path: file, Hash: hash, Score: 100,
		Findings: []model.Finding{{
			RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
			Title:    "Parse failed, artifact not fully covered",
			Evidence: []model.Evidence{{File: file, Snippet: "parse error"}},
		}},
	}}}
}

// TestCleanLoadWithNoHashIsNotClaimedTrusted: the clean branch of PreToolUse records the load
// as trusted and says so. With an empty hash the record is dropped by the store (no key, no
// approval), but the branch used to say "trusted from now on for content (none)" anyway and
// report the store as changed, so the runner rewrote — or created — an approvals file holding
// nothing new. The same contract as `aguard approve` applies: never say "trusted" over an
// empty hash, say it was not remembered and why, and leave the store alone. The load itself
// still passes: no decision is returned, as for any clean load (fail-open is not in question —
// this content WAS scanned).
func TestCleanLoadWithNoHashIsNotClaimedTrusted(t *testing.T) {
	cases := []struct {
		name string
		res  model.ScanResult
		want string
	}{
		{
			name: "PARSE-000 hook from a root-shaped skill directory",
			res:  parseFailedHook(""),
			want: `AgentGuard: hook "settings.json" 100/100 (Low) · no finding at or above the threshold · ` +
				`not remembered, it has no content hash: "/cfg/skills/x/settings.json" did not parse, so it was not fully read ` +
				`[PARSE-000: Parse failed, artifact not fully covered] · it is audited again on every load`,
		},
		{
			name: "artifact the scanner could not hash, with no note of its own",
			res: model.ScanResult{Artifacts: []model.ArtifactReport{{
				Kind: model.KindInstruction, Name: "notes.md", Path: "/cfg/skills/x/notes.md", Score: 100,
			}}},
			want: `AgentGuard: instruction "notes.md" 100/100 (Low) · no finding at or above the threshold · ` +
				`not remembered, it has no content hash: the scanner could not compute one · it is audited again on every load`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, store := newOpts(t, func(string) (model.ScanResult, error) { return tc.res, nil })
			mustSkillDir(t, o.Root, "x")
			ev := Event{HookEventName: EventPreToolUse, ToolName: SkillTool, ToolUseID: "c1",
				ToolInput: json.RawMessage(`{"skill":"x"}`)}

			out, dirty := Handle(ev, o)
			if out.HookSpecificOutput != nil {
				t.Errorf("a clean load with no hash got a decision; it must pass like any clean load: %+v", out.HookSpecificOutput)
			}
			if strings.Contains(out.SystemMessage, "trusted") {
				t.Errorf("claimed trust over an empty hash:\n%s", out.SystemMessage)
			}
			if out.SystemMessage != tc.want {
				t.Errorf("notice\n got: %s\nwant: %s", out.SystemMessage, tc.want)
			}
			if dirty {
				t.Error("reported the store as changed, so the runner would rewrite (or create) it for an approval that was never recorded")
			}
			if n := len(store.Approvals); n != 0 {
				t.Errorf("store holds %d approval(s), want 0", n)
			}
		})
	}
}

// TestCleanLoadWithAHashIsStillRemembered is the reverse assertion: the same artifact WITH a
// hash is recorded, announced as trusted, and reported as a store change — exactly as before.
func TestCleanLoadWithAHashIsStillRemembered(t *testing.T) {
	const hash = "feedface00112233"
	o, store := newOpts(t, func(string) (model.ScanResult, error) { return parseFailedHook(hash), nil })
	mustSkillDir(t, o.Root, "x")
	ev := Event{HookEventName: EventPreToolUse, ToolName: SkillTool, ToolUseID: "c1",
		ToolInput: json.RawMessage(`{"skill":"x"}`)}

	out, dirty := Handle(ev, o)
	if !dirty {
		t.Error("a clean load with a hash must be recorded, and the store saved")
	}
	if a, ok := store.Approved(hash); !ok || a.Verdict != VerdictClean {
		t.Errorf("approval not recorded as clean: %+v (found %v)", a, ok)
	}
	if !strings.Contains(out.SystemMessage, "trusted from now on for content "+shortHash(hash)) {
		t.Errorf("notice does not announce the trust it recorded:\n%s", out.SystemMessage)
	}
}
