// SPDX-License-Identifier: MIT
package main

// The judge usage log (P-073): AGUARD_USAGE_LOG=<path> appends one JSON line per judge block that
// issued a call. The contract tests drive the built binary, because the line is appended from the
// commands' RunE; the shapes that need no endpoint run in process.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/basdotio/bas-guard/internal/model"
)

// usageMarker names the fixture skill's directory and its frontmatter name. A line that carried
// anything from the scanned target — its path, the artifact's name — would carry this string.
const usageMarker = "usagelog-marker-q7x3k9"

// Token counts the stub (usageServer) reports per call, so a line's totals are a measurement and not a 0.
const (
	stubPromptTokens     = 17
	stubCompletionTokens = 5
)

// markerSkill is a skill whose directory, path and name all carry usageMarker.
func markerSkill(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), usageMarker+"-tree", usageMarker)
	mustWriteFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+usageMarker+
		"\ndescription: Formats the changelog.\n---\nRun the formatter over CHANGELOG.md and report what changed.\n")
	return dir
}

// credentialedBaseURL is the stub's URL with userinfo, a path and a query added — every part of a
// base_url the line must not carry.
func credentialedBaseURL(srv *httptest.Server) string {
	return strings.Replace(srv.URL, "http://", "http://usr:pa55word@", 1) + "/v1?api-key=qs-secret-123"
}

// runAguardEnv runs the built binary in dir with an isolated HOME, AGUARD_USAGE_LOG removed from
// the inherited environment, and extra appended.
func runAguardEnv(t *testing.T, dir string, extra []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(builtAguard(t), args...)
	cmd.Dir = dir
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "CLAUDE_CONFIG_DIR="}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, usageLogEnv+"=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, extra...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
	default:
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

// readUsageLines returns the log's raw lines and their decoded form; every line must be one JSON
// object ending in a newline.
func readUsageLines(t *testing.T, path string) ([]string, []usageLine) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("usage log: %v", err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("usage log does not end in a newline: %q", b)
	}
	raw := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	lines := make([]usageLine, len(raw))
	for i, r := range raw {
		dec := json.NewDecoder(strings.NewReader(r))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&lines[i]); err != nil {
			t.Fatalf("line %d is not a usage line (%v): %s", i+1, err, r)
		}
	}
	return raw, lines
}

// assertLineIsBlock holds a line to the judge block it records: every field the two share is equal.
func assertLineIsBlock(t *testing.T, line usageLine, j *model.JudgeSummary, out model.ScanResult) {
	t.Helper()
	if j == nil || j.Calls == 0 {
		t.Fatalf("the judge block made no call, so the comparison proves nothing: %+v", j)
	}
	want := usageLine{
		Time: line.Time, Command: line.Command, Section: line.Section, Endpoint: line.Endpoint,
		ToolVersion: out.ToolVersion, RulesVersion: out.RulesVersion,
		PromptVersion: j.PromptVersion, ExcerptVersion: j.ExcerptVersion, Model: j.Model, Samples: j.Samples,
		Calls: j.Calls, Failed: j.Failed, Skipped: j.Skipped, Retries: j.Retries, TriageCalls: j.TriageCalls,
		Repaired: j.Repaired, Reasked: j.Reasked, TemperatureOmitted: j.TemperatureOmitted,
		PromptTokens: j.PromptTokens, CompletionTokens: j.CompletionTokens,
		CachedPromptTokens: j.CachedPromptTokens, UsageByPass: j.UsageByPass,
	}
	if !reflect.DeepEqual(line, want) {
		t.Errorf("the line is not the judge block:\n line  %+v\n block %+v", line, want)
	}
	if j.PromptTokens == 0 || j.CompletionTokens == 0 {
		t.Errorf("the stub reports tokens, so the block must carry them: %+v", j)
	}
	// P-072: the breakdown by pass is in the line as in the block, and it adds up to the totals.
	if len(line.UsageByPass) == 0 {
		t.Errorf("a block that made calls has a usage_by_pass, and the line must carry it: %+v", line)
	}
	if sum := sumUsageByPass(line.UsageByPass); sum.Calls != j.Calls || sum.PromptTokens != j.PromptTokens ||
		sum.CompletionTokens != j.CompletionTokens || sum.CachedPromptTokens != j.CachedPromptTokens || sum.Reasked != j.Reasked {
		t.Errorf("the line's usage_by_pass sums to %+v, not the block's totals %+v", sum, j)
	}
	if ts, err := time.Parse(time.RFC3339, line.Time); err != nil || !strings.HasSuffix(line.Time, "Z") || time.Since(ts) > time.Hour {
		t.Errorf("time %q is not a recent RFC 3339 UTC timestamp (%v)", line.Time, err)
	}
}

// assertNothingFromTheTarget: no byte of the line names the scanned target or the endpoint's
// credentials.
func assertNothingFromTheTarget(t *testing.T, raw string, forbidden ...string) {
	t.Helper()
	for _, s := range append([]string{usageMarker, "usr:", "pa55word", "qs-secret", "/v1"}, forbidden...) {
		if strings.Contains(raw, s) {
			t.Errorf("the usage line carries %q:\n%s", s, raw)
		}
	}
}

// TestUsageLog_CheckLLMAppendsTheJudgeBlock: with AGUARD_USAGE_LOG set, `check --llm` appends
// exactly one line whose fields equal the JSON judge block's; a second run appends a second line;
// the endpoint is the origin alone though base_url has userinfo, a path and a query; and nothing in
// a line comes from the target.
func TestUsageLog_CheckLLMAppendsTheJudgeBlock(t *testing.T) {
	srv := usageServer(t, stubPromptTokens, stubCompletionTokens)
	cfg := writeJudgeConfig(t, credentialedBaseURL(srv), "advisory")
	skill := markerSkill(t)
	logPath := filepath.Join(t.TempDir(), "usage.jsonl")
	env := []string{usageLogEnv + "=" + logPath}

	var outs []model.ScanResult
	for run := 1; run <= 2; run++ {
		so, se, code := runAguardEnv(t, t.TempDir(), env, "check", skill, "--llm", "--config", cfg, "--json")
		if code != 0 {
			t.Fatalf("run %d: exit %d, want 0; stderr:\n%s", run, code, se)
		}
		if strings.Contains(se, "usage not recorded") {
			t.Fatalf("run %d warned about the log:\n%s", run, se)
		}
		outs = append(outs, decodeScan(t, so))
		raw, lines := readUsageLines(t, logPath)
		if len(lines) != run {
			t.Fatalf("after run %d the log has %d line(s), want %d:\n%s", run, len(lines), run, strings.Join(raw, "\n"))
		}
	}

	raw, lines := readUsageLines(t, logPath)
	wantOrigin := srv.URL // http://127.0.0.1:<port>
	for i, line := range lines {
		if line.Command != "check" || line.Section != "" {
			t.Errorf("line %d: command %q section %q, want check and none", i+1, line.Command, line.Section)
		}
		if line.Endpoint != wantOrigin {
			t.Errorf("line %d: endpoint %q, want the origin %q alone", i+1, line.Endpoint, wantOrigin)
		}
		assertLineIsBlock(t, line, outs[i].Judge, outs[i])
		assertNothingFromTheTarget(t, raw[i], skill)
	}
	if fi, err := os.Stat(logPath); err != nil || fi.Mode().Perm() != usageLogMode {
		t.Errorf("a created log must be mode %o: %v %v", usageLogMode, fi.Mode(), err)
	}
}

// TestUsageLog_ScanLLMOneLinePerJudgeBlock: `scan --llm` with a Downloads folder appends one line
// for the environment's judge block and one for the Downloads block, each equal to its block —
// the Downloads one already summed over the items, not one line per item.
func TestUsageLog_ScanLLMOneLinePerJudgeBlock(t *testing.T) {
	srv := usageServer(t, stubPromptTokens, stubCompletionTokens)
	cfg := writeJudgeConfig(t, srv.URL, "advisory")
	root := buildTestRunnerSkill(t, false)
	downloads := filepath.Join(t.TempDir(), usageMarker+"-downloads")
	for _, name := range []string{usageMarker + "-a", usageMarker + "-b"} {
		mustWriteFile(t, filepath.Join(downloads, name, "SKILL.md"), "---\nname: "+name+
			"\ndescription: Formats the changelog.\n---\nRun the formatter over CHANGELOG.md.\n")
	}
	logPath := filepath.Join(t.TempDir(), "usage.jsonl")

	so, se, code := runAguardEnv(t, t.TempDir(), []string{usageLogEnv + "=" + logPath},
		"scan", "--root", root, "--inbox", downloads, "--llm", "--config", cfg, "--json")
	if code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, se)
	}
	out := decodeScan(t, so)
	if out.Inbox == nil || len(out.Inbox.Items) != 2 {
		t.Fatalf("the Downloads pass must have checked both items: %+v", out.Inbox)
	}
	raw, lines := readUsageLines(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (environment, downloads), got %d:\n%s", len(lines), strings.Join(raw, "\n"))
	}
	for i, want := range []struct {
		section string
		judge   *model.JudgeSummary
	}{{usageSectionEnvironment, out.Judge}, {usageSectionDownloads, out.Inbox.Judge}} {
		if lines[i].Command != "scan" || lines[i].Section != want.section {
			t.Errorf("line %d: command %q section %q, want scan / %s", i+1, lines[i].Command, lines[i].Section, want.section)
		}
		assertLineIsBlock(t, lines[i], want.judge, out)
		assertNothingFromTheTarget(t, raw[i], root, downloads, "test-runner")
	}
}

// maskScannedAt removes the one field of a report that is a clock reading.
var scannedAtRE = regexp.MustCompile(`"scanned_at": \d+`)

// TestUsageLog_UnsetWritesNothingAndChangesNoOutput is the reverse assertion: without the variable
// nothing is created — not in the working directory, not in HOME — and with it set, stdout, stderr
// and the exit code are the same as without it; a run that made no call writes nothing.
func TestUsageLog_UnsetWritesNothingAndChangesNoOutput(t *testing.T) {
	srv := usageServer(t, stubPromptTokens, stubCompletionTokens)
	cfg := writeJudgeConfig(t, srv.URL, "advisory")
	skill := markerSkill(t)
	args := []string{"check", skill, "--llm", "--config", cfg, "--json", "--quiet"}

	cwd := t.TempDir()
	soOff, seOff, codeOff := runAguardEnv(t, cwd, nil, args...)
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Errorf("without %s the run left %d file(s) in its working directory", usageLogEnv, len(entries))
	}
	soEmpty, seEmpty, codeEmpty := runAguardEnv(t, cwd, []string{usageLogEnv + "="}, args...)
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Errorf("with %s empty the run left %d file(s) in its working directory", usageLogEnv, len(entries))
	}

	logPath := filepath.Join(t.TempDir(), "usage.jsonl")
	soOn, seOn, codeOn := runAguardEnv(t, cwd, []string{usageLogEnv + "=" + logPath}, args...)
	if _, lines := readUsageLines(t, logPath); len(lines) != 1 {
		t.Fatalf("with the variable set, want 1 line, got %d", len(lines))
	}
	if decodeScan(t, soOff).Judge.Calls == 0 {
		t.Fatal("the judge made no call, so the comparison proves nothing")
	}
	for _, other := range []struct {
		name   string
		so, se string
		code   int
	}{{"empty", soEmpty, seEmpty, codeEmpty}, {"set", soOn, seOn, codeOn}} {
		if scannedAtRE.ReplaceAllString(other.so, "") != scannedAtRE.ReplaceAllString(soOff, "") {
			t.Errorf("stdout with the variable %s differs from without it:\n%s\n---\n%s", other.name, other.so, soOff)
		}
		if other.se != seOff || other.code != codeOff {
			t.Errorf("stderr/exit with the variable %s: %q/%d, without: %q/%d", other.name, other.se, other.code, seOff, codeOff)
		}
	}

	// No call, no line, no file: the judge not enabled, and a static check.
	off := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, off, "llm:\n  enabled: false\n")
	for _, a := range [][]string{
		{"check", skill, "--llm", "--config", off, "--json"},
		{"check", skill, "--config", cfg, "--json"},
	} {
		noCall := filepath.Join(t.TempDir(), "usage.jsonl")
		if _, se, code := runAguardEnv(t, cwd, []string{usageLogEnv + "=" + noCall}, a...); code != 0 {
			t.Fatalf("%v: exit %d; stderr:\n%s", a, code, se)
		}
		if _, err := os.Lstat(noCall); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%v made no judge call, yet the log exists (%v)", a, err)
		}
	}
}

// TestUsageLog_RefusesWhatIsNotARegularFile: a symlink (to a file, or dangling), a directory, or a
// path under a non-directory is refused with one warning on stderr; nothing is written through it,
// and the exit code is the one the run has without the variable.
func TestUsageLog_RefusesWhatIsNotARegularFile(t *testing.T) {
	srv := usageServer(t, stubPromptTokens, stubCompletionTokens)
	cfg := writeJudgeConfig(t, srv.URL, "advisory")
	skill := markerSkill(t)
	args := []string{"check", skill, "--llm", "--config", cfg, "--json"}
	_, _, wantCode := runAguardEnv(t, t.TempDir(), nil, args...)

	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	mustWriteFile(t, target, "")
	linked := filepath.Join(dir, "linked.jsonl")
	dangling := filepath.Join(dir, "dangling.jsonl")
	notCreated := filepath.Join(dir, "not-created.jsonl")
	asDir := filepath.Join(dir, "a-directory")
	if err := os.Symlink(target, linked); err != nil {
		t.Skip("cannot create a symlink here: ", err)
	}
	if err := os.Symlink(notCreated, dangling); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, path string }{
		{"symlink to a file", linked},
		{"dangling symlink", dangling},
		{"directory", asDir},
		{"parent is a file", filepath.Join(target, "usage.jsonl")},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, se, code := runAguardEnv(t, t.TempDir(), []string{usageLogEnv + "=" + c.path}, args...)
			if code != wantCode {
				t.Errorf("exit %d, want %d (the run's exit without the variable)", code, wantCode)
			}
			if n := strings.Count(se, "judge usage not recorded"); n != 1 {
				t.Errorf("want exactly one warning on stderr, got %d:\n%s", n, se)
			}
			if b, err := os.ReadFile(target); err != nil || len(b) != 0 {
				t.Errorf("something was written through to the symlink's target: %q %v", b, err)
			}
			if _, err := os.Lstat(notCreated); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the dangling symlink's target was created (%v)", err)
			}
		})
	}
}

// TestUsageLog_LLMTestRecordsItsOneCall: `llm test` appends one line for its one call — answered
// or not — with what the endpoint reported, and no samples.
func TestUsageLog_LLMTestRecordsItsOneCall(t *testing.T) {
	srv := usageServer(t, stubPromptTokens, stubCompletionTokens)
	logPath := filepath.Join(t.TempDir(), "usage.jsonl")
	t.Setenv(usageLogEnv, logPath)

	if err := runLLMTest(io.Discard, writeJudgeConfig(t, credentialedBaseURL(srv), "advisory")); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	down.Close() // a closed port: the call is issued and fails
	if err := runLLMTest(io.Discard, writeJudgeConfig(t, down.URL, "advisory")); err == nil {
		t.Fatal("the closed endpoint answered")
	}

	raw, lines := readUsageLines(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), strings.Join(raw, "\n"))
	}
	ok, failed := lines[0], lines[1]
	if ok.Command != "llm test" || ok.Calls != 1 || ok.Failed != 0 || ok.Samples != 0 ||
		ok.PromptTokens != stubPromptTokens || ok.CompletionTokens != stubCompletionTokens ||
		ok.Endpoint != srv.URL || ok.Model != "fake" || ok.ToolVersion != version || ok.RulesVersion == "" {
		t.Errorf("answered llm test line: %+v", ok)
	}
	if failed.Command != "llm test" || failed.Calls != 1 || failed.Failed != 1 || failed.PromptTokens != 0 || failed.Endpoint != down.URL {
		t.Errorf("failed llm test line: %+v", failed)
	}
	// The connectivity check is no pass of the judge's (P-072): neither line breaks its call down.
	for i, r := range raw {
		if strings.Contains(r, "usage_by_pass") {
			t.Errorf("llm test line %d carries usage_by_pass: %s", i+1, r)
		}
	}
	assertNothingFromTheTarget(t, raw[0])
}

// TestEndpointOrigin: scheme://host[:port], and nothing for what does not parse into both.
func TestEndpointOrigin(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://api.example.com/v1", "https://api.example.com"},
		{"https://user:pass@api.example.com:8443/v1/?key=abc#frag", "https://api.example.com:8443"},
		{"http://127.0.0.1:11434/v1", "http://127.0.0.1:11434"},
		{"http://[::1]:8080/v1", "http://[::1]:8080"},
		{"https://tok@example.com", "https://example.com"},
		{"", ""},
		{"localhost:11434/v1", ""},
		{"/v1/chat", ""},
		{"http://%zz", ""},
	} {
		if got := endpointOrigin(c.in); got != c.want {
			t.Errorf("endpointOrigin(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUsageLog_ExistingFileKeepsItsModeAndLines: an existing regular file is appended to, never
// truncated, and keeps the mode its owner gave it.
func TestUsageLog_ExistingFileKeepsItsModeAndLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	mustWriteFile(t, path, "{\"earlier\":true}\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv(usageLogEnv, path)
	var warn bytes.Buffer
	j := &model.JudgeSummary{Calls: 3, Failed: 1, Model: "m", Endpoint: "https://u:p@h.example/v1"}
	recordJudgeUsage(&warn, "check", "v9", "r1", []usageBlock{{judge: j}, {judge: &model.JudgeSummary{}}, {}})
	if warn.Len() != 0 {
		t.Fatalf("warned: %s", warn.String())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(got) != 2 || got[0] != `{"earlier":true}` || !strings.Contains(got[1], `"endpoint":"https://h.example"`) {
		t.Errorf("want the earlier line kept and one new line (blocks with no call skipped):\n%s", b)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("an existing file's mode must not change: %v %v", fi.Mode(), err)
	}
}

// TestUsageLog_CarriesCachedPromptTokens: an endpoint that reports cached prompt tokens (P-071) has
// them in the line, equal to the judge block's.
func TestUsageLog_CarriesCachedPromptTokens(t *testing.T) {
	srv := cachedUsageServer(t, 100, 7, 64)
	cfg := writeJudgeConfig(t, srv.URL, "advisory")
	logPath := filepath.Join(t.TempDir(), "usage.jsonl")
	so, se, code := runAguardEnv(t, t.TempDir(), []string{usageLogEnv + "=" + logPath}, "check", markerSkill(t),
		"--llm", "--config", cfg, "--json")
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr:\n%s", code, se)
	}
	out := decodeScan(t, so)
	_, lines := readUsageLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("%d line(s), want 1", len(lines))
	}
	if out.Judge == nil || out.Judge.CachedPromptTokens == 0 || lines[0].CachedPromptTokens != out.Judge.CachedPromptTokens {
		t.Fatalf("cached prompt tokens: line %d, block %+v", lines[0].CachedPromptTokens, out.Judge)
	}
	assertLineIsBlock(t, lines[0], out.Judge, out)
}
