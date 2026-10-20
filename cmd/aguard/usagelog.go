// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/redact"
)

// The judge usage log (P-073). A judge run's usage — calls, failures, retries, tokens, which model
// and which judge build — reaches the JSON `judge` block only with --json, and otherwise one stderr
// line that --quiet suppresses (and that the Downloads pass never prints). Someone running
// `check --llm` / `scan --llm` by hand or from a script over days could not add up what their judge
// runs did. AGUARD_USAGE_LOG=<path> makes every judge run that issued at least one call append one
// JSON line to that file.
//
// What a line carries is decided here and nowhere else: the judge block's usage counts and the
// identity of the judge (tool, rules, prompt and excerpt versions, model, samples), plus the
// endpoint's ORIGIN. Never anything from the scanned content — no artifact name, path, finding,
// excerpt, reason text or count of artifacts or findings — and never the endpoint's path, query or
// userinfo, since a base_url can carry credentials as user:pass@. The values are the ones the judge
// block already holds; nothing is counted a second time.
//
// It is a local file write only: invariant #1 (no outbound connection outside the judge) is not
// touched, and TestZeroDial_OnlyTheJudgeConnects runs its positive control with the variable set.
// A failure to write prints one warning on stderr and changes neither the exit code nor a report.
// It records what the endpoint reported; it computes no money.

// usageLogEnv names the file the usage lines are appended to. Unset or empty: nothing is written.
// BAS_GUARD_USAGE_LOG is read first (P-082, config.LookupEnv).
const usageLogEnv = "AGUARD_USAGE_LOG"

// usageLogMode is the mode a usage log is CREATED with. A file that already exists keeps its own.
const usageLogMode = 0o600

// Sections of a `scan` run, one line each: the environment's judge block and the Downloads one.
const (
	usageSectionEnvironment = "environment"
	usageSectionDownloads   = "downloads"
)

// usageLine is one line of the log. The JSON names are the judge block's own, so a line can be
// read next to a report without a mapping; the token totals are omitted when the endpoint reported
// none, for the same reason the block omits them (a 0 would read as a measurement).
type usageLine struct {
	Time               string `json:"time"`
	Command            string `json:"command"`
	Section            string `json:"section,omitempty"`
	ToolVersion        string `json:"tool_version"`
	RulesVersion       string `json:"rules_version"`
	PromptVersion      string `json:"prompt_version,omitempty"`
	ExcerptVersion     int    `json:"excerpt_version,omitempty"`
	Model              string `json:"model,omitempty"`
	Endpoint           string `json:"endpoint,omitempty"`
	Samples            int    `json:"samples,omitempty"`
	Calls              int    `json:"calls"`
	Failed             int    `json:"failed"`
	Skipped            int    `json:"skipped"`
	Retries            int    `json:"retries"`
	TriageCalls        int    `json:"triage_calls"`
	Repaired           int    `json:"repaired"`
	Reasked            int    `json:"reasked"`
	TemperatureOmitted bool   `json:"temperature_omitted,omitempty"`
	PromptTokens       int    `json:"prompt_tokens,omitempty"`
	CompletionTokens   int    `json:"completion_tokens,omitempty"`
	CachedPromptTokens int    `json:"cached_prompt_tokens,omitempty"` // P-071: part of PromptTokens
	// UsageByPass is the block's breakdown by pass (P-072), copied as it is; absent on `llm test`,
	// whose one call is a connectivity check and no pass of the judge's.
	UsageByPass map[string]model.PassUsage `json:"usage_by_pass,omitempty"`
}

// usageBlock is one judge block of a run and the section it stands for ("" when a command has one).
type usageBlock struct {
	section string
	judge   *model.JudgeSummary
}

// scanUsageBlocks are the judge blocks of a `scan` report: the environment's and, when the
// Downloads pass ran, its own — already summed over the items by checkCandidate.
func scanUsageBlocks(out model.ScanResult) []usageBlock {
	blocks := []usageBlock{{usageSectionEnvironment, out.Judge}}
	if out.Inbox != nil {
		blocks = append(blocks, usageBlock{usageSectionDownloads, out.Inbox.Judge})
	}
	return blocks
}

// recordJudgeUsage appends one line per block that issued at least one call, when AGUARD_USAGE_LOG
// is set. The first failure is warned about on stderr and ends the attempt; it is never an error
// of the command.
func recordJudgeUsage(stderr io.Writer, command, toolVersion, rulesVersion string, blocks []usageBlock) {
	path, from := config.LookupEnv(usageLogEnv)
	if path == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, b := range blocks {
		if b.judge == nil || b.judge.Calls == 0 {
			continue
		}
		line := usageLineFor(command, b.section, toolVersion, rulesVersion, now, b.judge)
		if err := appendUsageLine(path, line); err != nil {
			fmt.Fprintf(stderr, "aguard: warning: judge usage not recorded in %s=%s: %v\n", from, path, err)
			return
		}
	}
}

// usageLineFor copies the judge block's values into a line. Nothing is counted here.
func usageLineFor(command, section, toolVersion, rulesVersion, now string, j *model.JudgeSummary) usageLine {
	return usageLine{
		Time: now, Command: command, Section: section,
		ToolVersion: toolVersion, RulesVersion: rulesVersion,
		PromptVersion: j.PromptVersion, ExcerptVersion: j.ExcerptVersion,
		Model: j.Model, Endpoint: endpointOrigin(j.Endpoint), Samples: j.Samples,
		Calls: j.Calls, Failed: j.Failed, Skipped: j.Skipped, Retries: j.Retries,
		TriageCalls: j.TriageCalls, Repaired: j.Repaired, Reasked: j.Reasked,
		TemperatureOmitted: j.TemperatureOmitted,
		PromptTokens:       j.PromptTokens, CompletionTokens: j.CompletionTokens,
		CachedPromptTokens: j.CachedPromptTokens,
		UsageByPass:        j.UsageByPass,
	}
}

// endpointOrigin is scheme://host[:port] of a base_url, and "" for anything that does not parse
// into both. The path, query, fragment and userinfo are dropped: a base_url can carry credentials
// as user:pass@ or a key in its query, and the log is a file nobody thinks of as holding one.
//
// The judge block's endpoint is the shown form (P-076: userinfo as `<REDACTED>@`, which url.Parse
// refuses), so the host comes from redact.EndpointHost, the splitter that made it.
func endpointOrigin(endpoint string) string {
	i := strings.Index(endpoint, "://")
	host := redact.EndpointHost(endpoint)
	if i <= 0 || host == "" {
		return ""
	}
	u, err := url.Parse(strings.ToLower(endpoint[:i]) + "://" + host)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// errUsageLogNotRegular refuses a path that exists as anything but a regular file — a symlink
// (dangling or not), a directory, a FIFO (opening one for writing blocks), a device.
var errUsageLogNotRegular = errors.New("not a regular file (a symlink, directory or special file is never written through)")

// appendUsageLine appends one line in ONE write on an O_APPEND descriptor, so on a local file
// system concurrent runs add whole lines at the end of the file rather than splicing into each
// other. (POSIX states that guarantee for pipes up to PIPE_BUF; for a regular file it is what local
// file systems do, and a network file system may not.)
//
// The path is checked with Lstat before the open and again after it, against the opened file:
// a symlink or anything else put in its place between the two is refused before a byte is written.
func appendUsageLine(path string, line usageLine) error {
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := usageLogTarget(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, usageLogMode)
	if err != nil {
		return err
	}
	opened, err := f.Stat()
	if err == nil {
		err = sameRegularFile(path, opened)
	}
	if err == nil {
		_, err = f.Write(b)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// usageLogTarget accepts a path whose parent is a directory and that is absent or a regular file.
func usageLogTarget(path string) error {
	if fi, err := os.Stat(filepath.Dir(path)); err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Dir(path))
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%w (mode %s)", errUsageLogNotRegular, fi.Mode())
	}
	return nil
}

// sameRegularFile says the path still names, without a symlink, the file that was opened.
func sameRegularFile(path string, opened fs.FileInfo) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || !os.SameFile(fi, opened) {
		return fmt.Errorf("%w: the path changed while it was opened", errUsageLogNotRegular)
	}
	return nil
}
