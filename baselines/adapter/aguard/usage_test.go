// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// budgetCut is the shape measured with the real judge.Run: two skills with one deterministic
// finding each, samples: 3, max_calls: 13. The plan has 14 calls and the budget cuts the last
// one, which is the second skill's triage — so the binary reports calls 13 · skipped 1 ·
// triage_calls 1, while "one triage call per artifact with a static finding" says 2, and
// (13 − 2) / 3 is not a whole number of questions. The real figure is (13 − 1) / 3 = 4.
const budgetCut = `{"artifacts":[
 {"kind":"skill","name":"a","findings":[{"rule_id":"FS-002","dimension":9,"severity":"medium","source":"static"}]},
 {"kind":"skill","name":"b","findings":[{"rule_id":"FS-002","dimension":9,"severity":"medium","source":"static"}]}],
 "judge":{"ran":true,"artifacts":2,"calls":13,"failed":0,"skipped":1,"findings":0,
  "triage_calls":1,"retries":2,"prompt_tokens":5200,"completion_tokens":310}}`

func intp(n int) *int { return &n }

func foldDoc(t *testing.T, doc string) *ledger.JudgeUsage {
	t.Helper()
	var res model.ScanResult
	if err := json.Unmarshal([]byte(doc), &res); err != nil {
		t.Fatalf("fixture is not a scan result: %v", err)
	}
	return judgeUsage([]byte(doc), res)
}

// documentedDerivation is the fold the committed judge runs describe in run.yaml: one triage
// call per artifact with a deterministic finding.
func documentedDerivation(t *testing.T, doc string) int {
	t.Helper()
	var res model.ScanResult
	if err := json.Unmarshal([]byte(doc), &res); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range res.Artifacts {
		for _, f := range a.Findings {
			if score.Deterministic(f) {
				n++
				break
			}
		}
	}
	return n
}

// TestJudgeUsage_ReportedCountsWinOverTheDerivation: when the tool's judge summary carries its
// own counts, the fold takes them. The fixture is one on which the derivation is wrong, and the
// test checks that first — a fixture where both agree would pass whether or not the fold read
// anything.
func TestJudgeUsage_ReportedCountsWinOverTheDerivation(t *testing.T) {
	if derived := documentedDerivation(t, budgetCut); derived != 2 {
		t.Fatalf("fixture: the derivation gives %d triage call(s); the fixture exists to make it give 2", derived)
	}
	got := foldDoc(t, budgetCut)
	want := &ledger.JudgeUsage{
		Basis: ledger.UsageReported, Calls: 13, Failed: 0, Skipped: 1, TriageCalls: 1,
		Retries: intp(2), PromptTokens: intp(5200), CompletionTokens: intp(310),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("judge usage:\n got %s\nwant %s", show(got), show(want))
	}
}

// TestJudgeUsage_WithoutTheFieldsFoldsAsBefore is the reverse assertion. A binary older than the
// summary's cost fields reports neither triage_calls nor retries; the fold then falls back to
// the documented derivation, says so in Basis, and leaves retries and tokens absent rather than
// writing a 0 nobody measured. The first three cases are shaped like committed rows of
// results/aguard/2026-09-29-llm-gpt-4.1-mini-s3/judge.jsonl (judge_calls / triage_calls).
func TestJudgeUsage_WithoutTheFieldsFoldsAsBefore(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want ledger.JudgeUsage
	}{
		{
			name: "a hook with a static and an LLM finding (mal-hook-pretooluse-autoapprove: 7 calls, 1 triage)",
			doc: `{"artifacts":[{"kind":"hook","name":"PreToolUse[Bash]#1","findings":[
				{"rule_id":"PERM-008","dimension":2,"severity":"medium","source":"static"},
				{"rule_id":"LLM-008","dimension":2,"severity":"medium","source":"llm"}]}],
				"judge":{"ran":true,"artifacts":1,"calls":7,"failed":0,"skipped":0,"findings":1}}`,
			want: ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 7, TriageCalls: 1},
		},
		{
			name: "an MCP server with a static finding (mal-conn-mcp-command-payload: 4 calls, 1 triage)",
			doc: `{"artifacts":[{"kind":"mcp","name":"slack_summary","findings":[
				{"rule_id":"EXEC-001","dimension":4,"severity":"high","source":"static"},
				{"rule_id":"LLM-009","dimension":5,"severity":"high","source":"llm"}]}],
				"judge":{"ran":true,"artifacts":1,"calls":4,"failed":0,"skipped":0,"findings":1}}`,
			want: ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 4, TriageCalls: 1},
		},
		{
			name: "two permission lists with nothing to triage (hn-perm-feiskyer: 0 calls, 0 triage)",
			doc: `{"artifacts":[{"kind":"permission","name":"a","findings":[]},{"kind":"permission","name":"b"}],
				"judge":{"ran":true,"artifacts":2,"calls":0,"failed":0,"skipped":0,"findings":0}}`,
			want: ledger.JudgeUsage{Basis: ledger.UsageDerived},
		},
		{
			name: "a dimension-0 note and an LLM finding are not static findings, as for triage itself",
			doc: `{"artifacts":[{"kind":"skill","name":"a","findings":[
				{"rule_id":"COV-000","dimension":0,"severity":"medium","source":"static"},
				{"rule_id":"LLM-003","dimension":1,"severity":"high","source":"llm"}]}],
				"judge":{"ran":true,"artifacts":1,"calls":2,"failed":0,"skipped":0,"findings":1}}`,
			want: ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 2},
		},
		{
			name: "failed calls are carried through; they were issued",
			doc: `{"artifacts":[{"kind":"skill","name":"a","findings":[
				{"rule_id":"FS-002","dimension":9,"severity":"medium","source":"static"}]}],
				"judge":{"ran":true,"artifacts":1,"calls":7,"failed":3,"skipped":0,"findings":0}}`,
			want: ledger.JudgeUsage{Basis: ledger.UsageDerived, Calls: 7, Failed: 3, TriageCalls: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := foldDoc(t, tc.doc)
			if got == nil {
				t.Fatal("no usage folded from a sample that carries a judge summary")
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("judge usage:\n got %s\nwant %s", show(got), show(&tc.want))
			}
			if got.TriageCalls != documentedDerivation(t, tc.doc) && got.Calls > 0 {
				t.Errorf("derived triage_calls = %d, the documented derivation gives %d",
					got.TriageCalls, documentedDerivation(t, tc.doc))
			}
		})
	}
}

// TestJudgeUsage_UnreportedTokensStayAbsent: the binary omits the token counts when the endpoint
// reported none. The counts it does report are folded; the tokens stay absent, never 0.
func TestJudgeUsage_UnreportedTokensStayAbsent(t *testing.T) {
	got := foldDoc(t, `{"artifacts":[],"judge":{"ran":true,"artifacts":1,"calls":3,"failed":0,"skipped":0,
		"findings":0,"triage_calls":0,"retries":0}}`)
	want := &ledger.JudgeUsage{Basis: ledger.UsageReported, Calls: 3, Retries: intp(0)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("judge usage:\n got %s\nwant %s", show(got), show(want))
	}
}

// TestJudgeUsage_NoSummaryNoUsage: without --llm the scan result has no judge summary, and the
// row must not gain a usage block — a static run's ledger stays byte-for-byte what it was.
func TestJudgeUsage_NoSummaryNoUsage(t *testing.T) {
	if got := foldDoc(t, `{"artifacts":[{"kind":"skill","name":"a","findings":[
		{"rule_id":"FS-002","dimension":9,"severity":"medium","source":"static"}]}]}`); got != nil {
		t.Errorf("a scan with no judge summary folded usage %s", show(got))
	}
}

// TestScan_LedgerRowCarriesTheJudgeUsage: the usage reaches the ledger row the driver writes,
// and a static answer leaves the row exactly as it was.
func TestScan_LedgerRowCarriesTheJudgeUsage(t *testing.T) {
	tree := t.TempDir()
	write(t, tree, ".claude/settings.json", `{"permissions":{"allow":["Read"]}}`)
	sample := corpus.Sample{Sample: "s", Class: "benign", Surface: []string{"permission"}}

	judged := newAdapter(t, stubAnswering(t, budgetCut)).Scan(context.Background(), sample, tree)
	if judged.Outcome != ledger.Scored {
		t.Fatalf("outcome = %q (detail: %s)", judged.Outcome, judged.Detail)
	}
	if judged.JudgeUsage == nil || judged.JudgeUsage.Basis != ledger.UsageReported || judged.JudgeUsage.TriageCalls != 1 {
		t.Errorf("ledger row judge usage = %s, want the reported counts (triage_calls 1)", show(judged.JudgeUsage))
	}

	static := `{"artifacts":[{"kind":"skill","name":"a","findings":[
		{"rule_id":"FS-002","dimension":9,"severity":"medium","source":"static"}]}]}`
	plain := newAdapter(t, stubAnswering(t, static)).Scan(context.Background(), sample, tree)
	want := ledger.Row{Sample: "s", Outcome: ledger.Scored, Attempted: true, Verdict: "benign",
		Severity: "medium", Surface: []string{"permission"}}
	if !reflect.DeepEqual(plain, want) {
		t.Errorf("static row changed:\n got %+v\nwant %+v", plain, want)
	}
}

func show(u *ledger.JudgeUsage) string {
	b, _ := json.Marshal(u)
	return string(b)
}
