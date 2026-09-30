// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basdotio/agent-guard/internal/model"
)

// skillTree writes n skills and returns the artifacts a scan would hand the judge. Every
// file carries at least one long, quotable line so a well-behaved model double can cite it
// verbatim — findings that cite nothing are discarded by design (ground.go).
func skillTree(t *testing.T, n int) []model.ArtifactReport {
	t.Helper()
	root := t.TempDir()
	arts := make([]model.ArtifactReport, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skill%02d", i)
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\nname: %s\ndescription: does thing %d\n---\n"+
			"This skill runs the project test suite and summarizes the failures.\n", name, i)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		script := "echo starting the deployment routine for " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		arts = append(arts, model.ArtifactReport{Kind: model.KindSkill, Name: name, Path: dir, Findings: []model.Finding{}})
	}
	return arts
}

// quotableFrom returns a verbatim line of what a request showed the model — what a compliant
// model returns as evidence, and what grounding requires in order to keep a finding.
func quotableFrom(r Request) string {
	for _, line := range strings.Split(r.Behavior, "\n") {
		if len(strings.TrimSpace(line)) >= minGroundedChars+4 && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return r.Behavior
}

// scriptedClient answers per request, optionally after a delay, so completion order can be
// scrambled on purpose. It holds no mutable state — the scripts are read-only closures — so
// concurrent calls need no locking here.
type scriptedClient struct {
	// delay returns how long this request should take; nil = instant.
	delay func(r Request) time.Duration
	// answer returns the verdict for a request.
	answer func(r Request) (Verdict, error)
}

func (s *scriptedClient) Judge(ctx context.Context, r Request) (Verdict, error) {
	if s.delay != nil {
		if d := s.delay(r); d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return Verdict{}, ctx.Err()
			}
		}
	}
	if s.answer != nil {
		return s.answer(r)
	}
	return Verdict{Flagged: true, Severity: "low", Summary: r.Artifact, Evidence: quotableFrom(r)}, nil
}

func (s *scriptedClient) Triage(context.Context, string, []TriageItem) ([]model.AdvisoryLabel, error) {
	return nil, nil
}

func fingerprint(arts []model.ArtifactReport) string {
	var b strings.Builder
	for _, a := range arts {
		for _, f := range a.Findings {
			fmt.Fprintf(&b, "%s|%s|%s|%s\n", a.Name, f.RuleID, f.Severity, f.Why)
		}
	}
	return b.String()
}

// TestRun_ConcurrencyDoesNotChangeOutput is the invariant that makes concurrency safe to
// have at all: results are merged by task index, so a scrambled completion order produces
// byte-identical output to a serial run (same guarantee as detect.Engine.Run).
func TestRun_ConcurrencyDoesNotChangeOutput(t *testing.T) {
	// Deliberately INVERTED delays, keyed on the artifact's index digit: skill00 answers last,
	// skill05 first. Completion order is therefore the reverse of plan order, so a merge that
	// depended on arrival order could not possibly match the serial run.
	newClient := func() *scriptedClient {
		return &scriptedClient{delay: func(r Request) time.Duration {
			idx := int(r.Artifact[len(r.Artifact)-1] - '0')
			return time.Duration(60-idx*10) * time.Millisecond
		}}
	}

	serial := skillTree(t, 6)
	if _, st := Run(context.Background(), newClient(), serial, Options{Concurrency: 1}); st.Calls == 0 {
		t.Fatal("no calls issued")
	}
	want := fingerprint(serial)

	for run := 0; run < 3; run++ {
		arts := skillTree(t, 6)
		Run(context.Background(), newClient(), arts, Options{Concurrency: 8})
		if got := fingerprint(arts); got != want {
			t.Fatalf("run %d differs from serial output:\n got:\n%s\nwant:\n%s", run, got, want)
		}
	}
}

// TestRun_SlowCallDoesNotStarveTheRest: the per-call deadline exists so one unresponsive
// endpoint answer costs ONE check, not the whole judge phase.
func TestRun_SlowCallDoesNotStarveTheRest(t *testing.T) {
	arts := skillTree(t, 4)
	client := &scriptedClient{delay: func(r Request) time.Duration {
		if strings.Contains(r.Artifact, "skill01") {
			return time.Hour // hangs until its own deadline
		}
		return 0
	}}

	notes, stats := Run(context.Background(), client, arts, Options{Concurrency: 4, CallTimeout: 60 * time.Millisecond})

	if stats.Failed != 2 { // both of skill01's calls time out
		t.Errorf("failed calls = %d, want 2 (only the hung artifact)", stats.Failed)
	}
	for _, a := range arts {
		if a.Name == "skill01" {
			continue
		}
		if len(a.Findings) == 0 {
			t.Errorf("%s produced no finding — a slow neighbour starved it", a.Name)
		}
	}
	if len(notes) == 0 || notes[0].RuleID != "LLM-000" {
		t.Errorf("timed-out calls must surface an LLM-000 note, got %+v", notes)
	}
}

// TestRun_BudgetCutsDeterministicallyAndSaysWhat: over-budget calls are dropped from the
// PLAN (not raced away by workers), so the same run always drops the same set — and the note
// names the artifacts left unchecked instead of only counting them.
func TestRun_BudgetCutsDeterministicallyAndSaysWhat(t *testing.T) {
	var firstNote string
	for run := 0; run < 3; run++ {
		arts := skillTree(t, 5)
		client := &scriptedClient{}
		notes, stats := Run(context.Background(), client, arts, Options{MaxCalls: 3, Concurrency: 4})

		if stats.Calls != 3 {
			t.Fatalf("run %d: issued %d call(s), want exactly the budget (3)", run, stats.Calls)
		}
		if stats.Skipped != 7 { // 5 skills x 2 modes = 10 planned
			t.Errorf("run %d: skipped = %d, want 7", run, stats.Skipped)
		}
		if len(notes) == 0 || !strings.Contains(notes[0].Why, "budget") {
			t.Fatalf("run %d: budget truncation not reported: %+v", run, notes)
		}
		if !strings.Contains(notes[0].Why, "skill") {
			t.Errorf("budget note should name what went unchecked, got %q", notes[0].Why)
		}
		// Same plan, same cut, every time.
		if run == 0 {
			firstNote = notes[0].Why
		} else if notes[0].Why != firstNote {
			t.Errorf("budget cut is not deterministic:\n%q\nvs\n%q", notes[0].Why, firstNote)
		}
	}
}

// TestRun_RetriesOnlyRetryableErrors: a rate-limited endpoint is asked again; a rejected
// request (bad key, bad model) is not — retrying a real answer just burns the budget.
func TestRun_RetriesOnlyRetryableErrors(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantRetries  int
		wantAttempts int32
	}{
		{"rate limited then ok", Retryable(errors.New("429"), 0), 2, 3},
		{"hard rejection", errors.New("401 bad key"), 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			arts := skillTree(t, 1)
			var attempts atomic.Int32
			client := &scriptedClient{answer: func(r Request) (Verdict, error) {
				if r.Mode != ModeIntent {
					return Verdict{}, nil // keep the second call out of the way
				}
				if attempts.Add(1) <= int32(c.wantRetries) {
					return Verdict{}, c.err
				}
				return Verdict{Flagged: true, Severity: "low", Summary: "ok"}, nil
			}}
			_, stats := Run(context.Background(), client, arts, Options{MaxRetries: 2, Concurrency: 1})

			if stats.Retries != c.wantRetries {
				t.Errorf("retries = %d, want %d", stats.Retries, c.wantRetries)
			}
			if got := attempts.Load(); got != c.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, c.wantAttempts)
			}
		})
	}
}

// TestRun_DeadlineSkipsAreReported: a run that runs out of time must say how much it never
// got to — silence here would read as "judged everything, found nothing".
func TestRun_DeadlineSkipsAreReported(t *testing.T) {
	arts := skillTree(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already expired

	notes, stats := Run(ctx, &scriptedClient{}, arts, Options{Concurrency: 2})
	if stats.Skipped == 0 && stats.Failed == 0 {
		t.Fatal("an expired run must record skipped or failed calls, not look clean")
	}
	found := false
	for _, n := range notes {
		if n.RuleID == "LLM-000" {
			found = true
		}
	}
	if !found {
		t.Errorf("expired run produced no coverage note: %+v", notes)
	}
}

// TestHTTPClient_ClassifiesRetryable: 429/5xx are the endpoint saying "not now"; 4xx is a
// real answer. Only the former may be retried.
func TestHTTPClient_ClassifiesRetryable(t *testing.T) {
	cases := []struct {
		status    int
		retryable bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusUnauthorized, false},
		{http.StatusBadRequest, false},
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}))
			defer srv.Close()
			_, err := NewHTTP(srv.URL, "", "m", srv.Client()).Judge(context.Background(), Request{Mode: ModeIntent})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := isRetryable(err); got != c.retryable {
				t.Errorf("status %d: retryable = %v, want %v (err: %v)", c.status, got, c.retryable, err)
			}
		})
	}
}

// TestHTTPClient_CountsTokens: the usage counters are what make the cost of --llm visible.
func TestHTTPClient_CountsTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false}"}}],` +
			`"usage":{"prompt_tokens":120,"completion_tokens":30}}`))
	}))
	defer srv.Close()

	c := NewHTTP(srv.URL, "", "m", srv.Client())
	for i := 0; i < 3; i++ {
		if _, err := c.Judge(context.Background(), Request{Mode: ModeIntent}); err != nil {
			t.Fatal(err)
		}
	}
	if p, comp := c.Usage(); p != 360 || comp != 90 {
		t.Errorf("usage = %d/%d, want 360/90", p, comp)
	}
}

func TestRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{
		"":         0,
		"2":        2 * time.Second,
		"0":        0,
		"-5":       0,
		"nonsense": 0,
		"99999":    maxBackoff, // a hostile endpoint must not park the scan
	}
	for in, want := range cases {
		if got := retryAfter(in); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStats_Percentile(t *testing.T) {
	s := Stats{Latencies: []time.Duration{40, 10, 30, 20}}
	if got := s.Percentile(0.5); got != 20 {
		t.Errorf("p50 = %v, want 20", got)
	}
	if got := (Stats{}).Percentile(0.5); got != 0 {
		t.Errorf("empty p50 = %v, want 0", got)
	}
}

// TestRun_ProgressIsReportedAndSerialized: a judge run is the slowest thing the tool does, and
// silence for a minute is indistinguishable from a hang. The plan size is announced before any
// call goes out, then the count advances monotonically — and it must do so under concurrency
// without the caller needing a lock of its own.
func TestRun_ProgressIsReportedAndSerialized(t *testing.T) {
	arts := skillTree(t, 5)
	var seen [][2]int // no mutex here on purpose: Run must serialize the callback itself
	opts := Options{Concurrency: 8, Progress: func(done, total int) {
		seen = append(seen, [2]int{done, total})
	}}

	_, stats := Run(context.Background(), &scriptedClient{}, arts, opts)

	if len(seen) == 0 {
		t.Fatal("no progress reported at all")
	}
	if seen[0][0] != 0 {
		t.Errorf("first report was %v, want the plan size announced as (0, total) before any call", seen[0])
	}
	total := seen[0][1]
	if total != stats.Calls {
		t.Errorf("announced %d calls, actually made %d", total, stats.Calls)
	}
	for i, s := range seen {
		if s[1] != total {
			t.Errorf("report %d changed the total: %v", i, s)
		}
		if i > 0 && s[0] != seen[i-1][0]+1 {
			t.Errorf("count is not monotonic by one: %v then %v", seen[i-1], s)
		}
	}
	if last := seen[len(seen)-1][0]; last != total {
		t.Errorf("final count %d, want %d — the caller would leave a stale counter on screen", last, total)
	}
}
