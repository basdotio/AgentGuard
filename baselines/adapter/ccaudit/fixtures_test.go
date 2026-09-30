// SPDX-License-Identifier: MIT

package ccaudit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/baselines/adapter"
)

// The real warning line cc-audit printed on the traverse-only-dir fixture, 2026-09-23, verbatim
// including the ANSI codes and the Japanese prose. Kept whole rather than paraphrased: every
// property this file asserts — that the disclosure is on stdout, that the codes must be
// stripped, that the prose is localised while the errno tail is not — is a property of THIS
// string, and a tidied-up copy would stop testing any of them.
const realWarning = "\x1b[2m2026-09-23T16:13:05.753768Z\x1b[0m \x1b[33m WARN\x1b[0m " +
	"\x1b[2mcc_audit::engine::scanners::walker\x1b[0m\x1b[2m:\x1b[0m " +
	"ディレクトリエントリの読み取りに失敗。スキップします " +
	"\x1b[3merror\x1b[0m\x1b[2m=\x1b[0mIO error for operation on /tmp/x/private: " +
	"Permission denied (os error 13)"

// TestJsonPayloadSurvivesALogLineOnStdout — cc-audit writes tracing to STDOUT, ahead of the
// document, so `--format json` is unparseable whenever a warning fires. Nothing in the
// 3,539-sample run hit this, which is the danger: it appears only on trees with something
// unreadable in them, exactly the trees whose answers matter most.
func TestJsonPayloadSurvivesALogLineOnStdout(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "a clean document is untouched", in: `{"a":1}`, want: `{"a":1}`},
		{name: "an array document is untouched", in: `[1,2]`, want: `[1,2]`},
		{
			name: "the real warning line is skipped",
			in:   realWarning + "\n" + `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "several log lines are skipped",
			in:   "one\ntwo\nthree\n" + `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			// Conservative on purpose: a brace mid-line is not a document boundary, so a tool
			// that interleaves logging INTO its output still fails to parse rather than being
			// silently repaired into something that parses but is not what it emitted.
			name: "a brace that does not start a line is not a boundary",
			in:   "WARN something {not json}\n",
			want: "WARN something {not json}\n",
		},
		{
			name: "no document at all is returned unchanged for the error message",
			in:   "only a log line",
			want: "only a log line",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(jsonPayload([]byte(tt.in))); got != tt.want {
				t.Errorf("jsonPayload = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTheDisclosureCheckDoesNotDependOnTheOperatorsLocale — cc-audit's log prose is Japanese.
// Matching it would make a published pass/fail depend on whose machine ran the scan. The errno
// tail comes from the Rust standard library and does not translate, so that is what is matched.
func TestTheDisclosureCheckDoesNotDependOnTheOperatorsLocale(t *testing.T) {
	got := mentionsUnreadable([]byte(realWarning), nil)
	if got == "" {
		t.Fatal("the real warning line was not recognised as a disclosure")
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("ANSI escapes survived into a string bound for fixtures.jsonl:\n%q", got)
	}
	if !strings.Contains(got, "Permission denied (os error 13)") {
		t.Errorf("the matched line lost the part that says what went wrong:\n%s", got)
	}

	// Either stream, because the disclosure was measured on stdout and stderr is where one
	// would expect it.
	if mentionsUnreadable(nil, []byte("could not read /x")) == "" {
		t.Error("a disclosure on stderr was missed")
	}
	// And silence stays silence — the whole fail condition rests on this.
	if got := mentionsUnreadable([]byte("Collecting files to scan..."), nil); got != "" {
		t.Errorf("progress chatter read as a disclosure: %q", got)
	}
}

// TestTraverseOnlyHasTwoWaysToPassAndOneToFail applies the corpus's own sentence for this
// fixture rather than aguard's mechanism: "report that `private/` could not be enumerated — do
// not score the readable part as if it were the whole skill."
func TestTraverseOnlyHasTwoWaysToPassAndOneToFail(t *testing.T) {
	t.Run("findings mean it did not score the readable part as the whole", func(t *testing.T) {
		got := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{
			Summary: Summary{Critical: 1},
			Finding: []Finding{{ID: "EX-001", Severity: "critical"}},
		}, nil, nil)
		if got.Status != adapter.FixturePass {
			t.Errorf("Status = %q, want pass; detail: %s", got.Status, got.Detail)
		}
	})

	t.Run("saying so counts even with no findings", func(t *testing.T) {
		got := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{}, []byte(realWarning), nil)
		if got.Status != adapter.FixturePass {
			t.Fatalf("Status = %q, want pass; detail: %s", got.Status, got.Detail)
		}
		// The caveat is not optional. A pass here is materially weaker than a pass that put the
		// disclosure in the result, and the row travels into baselines/results/ where somebody
		// reads it out of context.
		for _, want := range []string{"not a field of the result", "passed=true"} {
			if !strings.Contains(got.Detail, want) {
				t.Errorf("detail does not carry %q:\n%s", want, got.Detail)
			}
		}
	})

	t.Run("silence is the fail", func(t *testing.T) {
		got := judgeTraverseOnly(adapter.FixtureResult{}, ScanResult{
			Summary: Summary{Passed: true},
			Risk:    &Risk{Total: 0, Level: "safe"},
		}, []byte("Collecting files to scan..."), nil)
		if got.Status != adapter.FixtureFail {
			t.Fatalf("Status = %q, want fail; detail: %s", got.Status, got.Detail)
		}
		if !strings.Contains(got.Detail, "curl | sh") {
			t.Errorf("detail does not say what was missed:\n%s", got.Detail)
		}
	})
}

// TestDescribeScoreKeepsAbsentApartFromZero — the same distinction Score.Present exists for. A
// fixture row saying "0/100 safe" for a tool that emitted no score would invent a clean bill.
func TestDescribeScoreKeepsAbsentApartFromZero(t *testing.T) {
	if got := describeScore(Score{}); got != "not scored" {
		t.Errorf("describeScore(absent) = %q", got)
	}
	if got := describeScore(Score{Total: 0, Level: "safe", Present: true}); got != "0/100 safe" {
		t.Errorf("describeScore(zero) = %q", got)
	}
}

// TestAnUnknownFixtureIsRecordedNotDropped — a fixture the corpus adds and this adapter has not
// been taught must appear as an untestable row. An absent section reads as "the fixtures do not
// apply here" when it means nobody checked.
func TestAnUnknownFixtureIsRecordedNotDropped(t *testing.T) {
	bin, _ := stub(t, 0, "{}", `{"summary":{},"findings":[]}`)
	got := (&Adapter{Bin: bin}).Fixture(context.Background(), "a-fixture-from-the-future", t.TempDir())
	if got.Status != adapter.FixtureUntestable {
		t.Errorf("Status = %q, want untestable", got.Status)
	}
	if !strings.Contains(got.Detail, "a-fixture-from-the-future") {
		t.Errorf("detail does not name the fixture:\n%s", got.Detail)
	}
}

// TestTheOversizedConfigGetsCreditForTheHalfItMet — the fixture's expectation has two halves,
// and cc-audit met one cleanly: it refused at a declared 10 MB limit instead of allocating
// 8 GiB. The verdict is still fail, because it aborted the tree rather than reporting the
// oversized config as a finding, but a row that said only "exit 2" would misrepresent it.
func TestTheOversizedConfigGetsCreditForTheHalfItMet(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cc-audit-stub")
	script := "#!/bin/sh\n" +
		"echo 'Error scanning /x: File too large to scan: /x/.mcp.json " +
		"(8589934594 bytes exceeds limit of 10485760 bytes)' >&2\nexit 2\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	got := (&Adapter{Bin: bin}).Fixture(context.Background(), "sparse-huge-config", dir)
	if got.Status != adapter.FixtureFail {
		t.Fatalf("Status = %q, want fail", got.Status)
	}
	for _, want := range []string{"bounded the read and said so", "no verdict", "too large"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail does not carry %q:\n%s", want, got.Detail)
		}
	}
}

// TestARobustnessFixtureThatTerminatesPasses is the reverse assertion for the failures above:
// the judging must not be so eager that a tool which behaves correctly still fails.
func TestARobustnessFixtureThatTerminatesPasses(t *testing.T) {
	bin, _ := stub(t, 0, "{}", jsonWith(t, 0, Summary{}))
	for _, name := range []string{"symlink-cycle", "symlink-escape", "deeply-nested", "fifo-as-skill"} {
		got := (&Adapter{Bin: bin}).Fixture(context.Background(), name, t.TempDir())
		if got.Status != adapter.FixturePass {
			t.Errorf("%s: Status = %q, want pass; detail: %s", name, got.Status, got.Detail)
		}
	}

	// sparse-huge-config passes the same way when the tool does not abort, and carries the
	// limit of what this runner can observe.
	got := (&Adapter{Bin: bin}).Fixture(context.Background(), "sparse-huge-config", t.TempDir())
	if got.Status != adapter.FixturePass {
		t.Fatalf("Status = %q, want pass", got.Status)
	}
	if !strings.Contains(got.Detail, "peak memory is not measured here") {
		t.Errorf("detail drops the limit of what was actually checked:\n%s", got.Detail)
	}
}

// TestFixtureNamesMatchesWhatThisFileJudges — the list and the switch drifting apart would turn
// a fixture into an untestable row without anybody deciding that.
func TestFixtureNamesMatchesWhatThisFileJudges(t *testing.T) {
	bin, _ := stub(t, 0, "{}", jsonWith(t, 0, Summary{}))
	for _, name := range FixtureNames() {
		got := (&Adapter{Bin: bin}).Fixture(context.Background(), name, t.TempDir())
		if got.Status == adapter.FixtureUntestable {
			t.Errorf("%s is in FixtureNames but has no judgement: %s", name, got.Detail)
		}
	}
	if len(FixtureNames()) != 6 {
		t.Errorf("FixtureNames has %d entries; the corpus ships 6 — if it added one, say what "+
			"passing means rather than letting it become an untestable row", len(FixtureNames()))
	}
}
