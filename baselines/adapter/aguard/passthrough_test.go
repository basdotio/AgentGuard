// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubRecorder writes a shell script that records its argv and environment to a log and then
// answers like aguard would (an empty scan result), so the adapter's process plumbing can be
// checked without the real binary.
func stubRecorder(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	bin = filepath.Join(dir, "aguard")
	script := "#!/bin/sh\n" +
		"printf 'ARGS %s\\n' \"$*\" >> '" + log + "'\n" +
		"env | sort | sed 's/^/ENV /' >> '" + log + "'\n" +
		"echo '{\"artifacts\":[]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// TestJudgePassthroughIsExplicitAndScanOnly: measuring the judge needs three things the
// isolated run deliberately lacks — extra flags, one environment variable, more time — and each
// must be opt-in and narrow. ExtraArgs reach `scan` only (`check` and `version` reject --llm);
// ExtraEnv adds the named variables and nothing else, so the operator's real environment still
// cannot leak into a sample; with neither set, the invocation is byte-for-byte what it was.
func TestJudgePassthroughIsExplicitAndScanOnly(t *testing.T) {
	t.Setenv("BASELINE_CANARY", "leaked") // must never reach the child, with or without ExtraEnv
	bin, log := stubRecorder(t)
	tree := t.TempDir()

	a := &Adapter{Bin: bin, Work: t.TempDir(), XDG: t.TempDir(),
		ExtraArgs: []string{"--llm", "--config", "/x/judge.yaml"},
		ExtraEnv:  []string{"JUDGE_KEY=secret"}}
	if _, err := a.run(context.Background(), "scan", "--root", tree, "--json"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, err := a.run(context.Background(), "check", tree, "--json"); err != nil {
		t.Fatalf("check: %v", err)
	}
	plain := &Adapter{Bin: bin, Work: t.TempDir(), XDG: t.TempDir()}
	if _, err := plain.run(context.Background(), "scan", "--root", tree, "--json"); err != nil {
		t.Fatalf("plain scan: %v", err)
	}

	calls := strings.Split(strings.TrimSpace(readFile(t, log)), "ARGS ")
	if len(calls) != 4 { // leading empty + three calls
		t.Fatalf("recorded %d call(s), want 3:\n%s", len(calls)-1, readFile(t, log))
	}
	scan, check, plainScan := calls[1], calls[2], calls[3]

	if !strings.HasPrefix(scan, "scan --root "+tree+" --json --llm --config /x/judge.yaml\n") {
		t.Errorf("scan argv did not end with the extra args:\n%s", firstLine(scan))
	}
	if strings.Contains(firstLine(check), "--llm") {
		t.Errorf("check received the extra args, which it rejects:\n%s", firstLine(check))
	}
	if !strings.Contains(scan, "ENV JUDGE_KEY=secret\n") {
		t.Errorf("ExtraEnv did not reach the child")
	}
	if strings.Contains(scan, "BASELINE_CANARY") || strings.Contains(plainScan, "BASELINE_CANARY") {
		t.Errorf("the operator's environment leaked into the child")
	}
	if !strings.HasPrefix(plainScan, "scan --root "+tree+" --json\n") {
		t.Errorf("with nothing set the argv changed:\n%s", firstLine(plainScan))
	}
	if strings.Contains(plainScan, "JUDGE_KEY") {
		t.Errorf("with nothing set the environment gained a variable")
	}
	for _, want := range []string{"ENV HOME=", "ENV XDG_CONFIG_HOME=", "ENV PATH=", "ENV TMPDIR="} {
		if !strings.Contains(plainScan, want) {
			t.Errorf("the isolated environment lost %s", want)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
