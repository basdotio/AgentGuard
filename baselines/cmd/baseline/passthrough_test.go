// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
	"time"

	aguardadapter "github.com/basdotio/AgentGuard/baselines/adapter/aguard"
)

// TestPickPassesJudgeFlagsThroughOnlyWhenAsked: the three measurement-only flags reach
// the aguard adapter exactly as given — a NAME without '=' is copied from this process, because
// an API key typed on a command line ends up in shell history — and with none of them set the
// adapter is configured exactly as before.
func TestPickPassesJudgeFlagsThroughOnlyWhenAsked(t *testing.T) {
	t.Setenv("JUDGE_KEY", "from-process")
	o := opts{tool: "aguard", bin: "bin/aguard",
		agExtra: "--llm --config /x/judge.yaml", agEnv: "A=1, JUDGE_KEY ,", agTimeout: 3 * time.Minute}
	ad, err := pick(o, "aguard", "high", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused aguard: %v", err)
	}
	got := ad.(*aguardadapter.Adapter)
	if s := strings.Join(got.ExtraArgs, " "); s != "--llm --config /x/judge.yaml" {
		t.Errorf("ExtraArgs = %q", s)
	}
	if s := strings.Join(got.ExtraEnv, " "); s != "A=1 JUDGE_KEY=from-process" {
		t.Errorf("ExtraEnv = %q, want the pair as given and the bare name resolved from this process", s)
	}
	if got.Timeout != 3*time.Minute {
		t.Errorf("Timeout = %s", got.Timeout)
	}

	ad, err = pick(opts{tool: "aguard", bin: "bin/aguard"}, "aguard", "high", t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("pick refused aguard: %v", err)
	}
	plain := ad.(*aguardadapter.Adapter)
	if len(plain.ExtraArgs) != 0 || len(plain.ExtraEnv) != 0 || plain.Timeout != 0 {
		t.Errorf("with no flags the adapter is not the plain one: %+v", plain)
	}
}

// TestAJudgeRunDisclosesTheUpload: the registry says aguard uploads nothing "except the
// explicitly opted-in LLM judge, which a baseline run does not enable". The moment a run passes
// --llm that sentence is false for that run, and run.yaml — the file people cite — must say so
// in its own words rather than inherit the registry's. Any other extra flag is recorded but
// changes no claim.
func TestAJudgeRunDisclosesTheUpload(t *testing.T) {
	entry := toolEntry{Uploads: false, UploadsBasis: "invariant #1 … which a baseline run does not enable."}

	up, basis := uploadsFor(entry, []string{"--llm", "--config", "/x/judge.yaml"})
	if !up {
		t.Fatal("a --llm run claimed uploads_samples: false")
	}
	for _, want := range []string{"--llm", "redacted", "raw/", "verdicts"} {
		if !strings.Contains(basis, want) {
			t.Errorf("basis does not say %q:\n%s", want, basis)
		}
	}

	up, basis = uploadsFor(entry, []string{"--no-reputation"})
	if up || basis != entry.UploadsBasis {
		t.Errorf("an unrelated extra flag changed the upload claim: %v %q", up, basis)
	}
	up, basis = uploadsFor(entry, nil)
	if up || basis != entry.UploadsBasis {
		t.Errorf("no extra flags changed the upload claim: %v %q", up, basis)
	}
}
