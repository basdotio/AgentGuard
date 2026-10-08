// SPDX-License-Identifier: MIT
// Command aguard is AgentGuard's CLI: static, local, read-only audit of a Claude Code
// agent environment (skills/MCP/hooks/permissions) + junk cleanup. M2 wires the static
// detection engine, deterministic scoring, and the terminal report.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/internal/clean"
	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/hygiene"
	"github.com/basdotio/AgentGuard/internal/ignore"
	"github.com/basdotio/AgentGuard/internal/inbox"
	"github.com/basdotio/AgentGuard/internal/judge"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/permcheck"
	"github.com/basdotio/AgentGuard/internal/report"
	"github.com/basdotio/AgentGuard/internal/reputation"
	"github.com/basdotio/AgentGuard/internal/score"
	"github.com/spf13/cobra"
)

// Injected via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// nowUnix is the gate's clock. A single indirection because approvals carry a timestamp and
// the tests need it fixed; scan results already take their time from the caller for the same
// reason (model.ScanResult.ScannedAt).
func nowUnix() int64 { return time.Now().Unix() }

// defaultRoot is where `--root` points when the operator does not say: $CLAUDE_CONFIG_DIR when
// set, else ~/.claude. Claude Code itself resolves its config directory in exactly this order (the
// desktop app exposes the variable as a setting for relocating the whole directory), so a scanner
// that hard-coded ~/.claude audited an EMPTY directory on any machine that had moved it — and an
// empty root scores 100/100. The variable is trusted as a location only: it is a path the operator
// set in their own environment, and everything under it goes through the same containment as any
// other root.
func defaultRoot() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// scanOpts carries the knobs shared by scan/check/clean.
type scanOpts struct {
	cfgPath      string
	ignorePath   string
	zombie       bool
	noReputation bool // disable the embedded reputation list (offline, on by default)
	llm          bool // enable the M5 intent judge (also requires config llm.enabled)
	quiet        bool // suppress the judge's stderr usage line (report output is separate)
	// autoBaseline allows <root>/.aguardignore to be picked up without --ignore. TRUE for
	// scan/clean, where root is the operator's OWN environment, and FALSE for check, where
	// root is the untrusted target — see resolveIgnorePath.
	autoBaseline bool
}

// scanEnv audits a whole .claude root (the `scan` and `clean` commands).
//
// The root is validated first, and an invalid one is an ERROR (exit 2), never an empty
// report: the collectors all read ENOENT as "layout absent", so a mistyped --root used to
// render as 100/100 + "No risk findings" + exit 0. Same contract as checkTarget below.
func scanEnv(root string, o scanOpts) (model.ScanResult, error) {
	if err := collect.ValidateRoot(root); err != nil {
		return model.ScanResult{}, err
	}
	o.autoBaseline = true
	return analyze(root, collect.CollectAll(root), o)
}

// checkTarget audits a single skill/dir/file (the `check` gate) — NOT a root.
// Zombie never applies to a single target (no usage log to test against).
func checkTarget(path string, o scanOpts) (model.ScanResult, error) {
	o.zombie = false
	o.autoBaseline = false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() && inbox.IsZip(path) {
		// A downloaded zip is checked as the folder it would unpack to: extracted into a private
		// temporary directory under the archive caps (see internal/inbox), never executed, removed
		// when the check ends.
		dir, notes, cleanup, err := inbox.ExtractZip(path)
		if err != nil {
			return model.ScanResult{}, fmt.Errorf("%s: %w", path, err)
		}
		defer cleanup()
		out, err := checkTarget(dir, o)
		if err != nil {
			return out, err
		}
		out.Root = path
		out.Notes = append(out.Notes, notes...)
		return out, nil
	}
	res, err := collect.CollectTarget(path)
	if err != nil {
		return model.ScanResult{}, err
	}
	return analyze(path, res, o)
}

// applyReputation matches each artifact's canonical hash against a reputation list. GOOD →
// suppress that artifact's scoring findings (record a REP-GOOD note mirroring the highest
// suppressed severity, §12 honesty). MALICIOUS → append a REP-BAD critical finding. Returns
// scan-level notes to surface. Deterministic (embedded, versioned).
//
// db is a parameter rather than a Load() call inside, so that the verdict handling is testable
// against a synthetic list: the shipped blocklist is empty pending curation, and it must not
// take a fake entry in a scoring data file to keep this branch covered.
func applyReputation(db *reputation.DB, arts []model.ArtifactReport) []model.Finding {
	var notes []model.Finding
	for i := range arts {
		e, ok := db.Match(arts[i].Hash)
		if !ok {
			continue
		}
		switch e.Verdict {
		case reputation.Good:
			var kept []model.Finding
			maxSev, n := model.Severity(""), 0
			for _, f := range arts[i].Findings {
				if f.Dimension == 0 { // keep coverage/parse notes
					kept = append(kept, f)
					continue
				}
				n++
				if f.Severity.Rank() > maxSev.Rank() {
					maxSev = f.Severity
				}
			}
			if kept == nil {
				kept = []model.Finding{} // keep JSON `[]`, never null
			}
			arts[i].Findings = kept
			arts[i].Reputation = &model.ReputationMark{
				Verdict: e.Verdict, Entry: e.Name, Publisher: e.Publisher, Version: e.Version,
				Source: e.Source, SHA: e.SHA, Path: e.Path, Reviewed: e.Reviewed, Suppressed: n,
			}
			if n > 0 {
				sev := maxSev
				if sev == "" {
					sev = model.SevLow
				}
				// Name the ENTRY, not just the match: which publisher, which repository and
				// commit the review was done on. "superpowers@claude-plugins-official" in the
				// artifact name is where the user installed it from; this is who we trusted.
				why := fmt.Sprintf("%s:%s matches reputation allowlist entry %s", arts[i].Kind, arts[i].Name, entryLabel(e))
				why += fmt.Sprintf("; %d finding(s) suppressed, highest severity=%s.", n, maxSev)
				// The entry's review travels with the suppression, so "why was this trusted?"
				// is answered in the report and not in a maintainer's memory.
				if e.Reason != "" {
					if e.Reviewed != "" {
						why += " Reviewed " + e.Reviewed + ": " + e.Reason
					} else {
						why += " Review: " + e.Reason
					}
				}
				notes = append(notes, model.Finding{
					RuleID: "REP-GOOD", Dimension: 0, Severity: sev, Source: model.SrcStatic,
					Title: "Findings suppressed: known-trusted artifact",
					Why:   why,
					// No snippet: the Title already reads "known-trusted artifact <name>".
					Evidence: []model.Evidence{{File: arts[i].Name, Line: 0}},
				})
			}
		case reputation.Malicious:
			// A curated, hash-exact blocklist hit is the highest-confidence signal the tool
			// has (unlike heuristic static findings), so it is CRITICAL: it forces the env
			// overall to ≤49 ("High" band) and trips every gate including --fail-on critical.
			arts[i].Reputation = &model.ReputationMark{
				Verdict: e.Verdict, Entry: e.Name, Publisher: e.Publisher, Version: e.Version,
				Source: e.Source, SHA: e.SHA, Path: e.Path, Reviewed: e.Reviewed,
			}
			arts[i].Findings = append(arts[i].Findings, model.Finding{
				RuleID: "REP-BAD", Dimension: 3, Severity: model.SevCritical, Source: model.SrcStatic,
				Title: "Known-malicious artifact (reputation list)",
				Why:   "This artifact's content hash matches a known-malicious entry in the reputation list.",
				Evidence: []model.Evidence{{File: arts[i].Name, Line: 0,
					Snippet: fmt.Sprintf("hash=%s ref=%s", short(arts[i].Hash), e.Ref)}},
			})
		}
	}
	return notes
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// entryLabel renders a reputation entry for a note: name, version, publisher, and the
// repository + commit (+ vendored path) the review was performed on — enough for a reader to
// tell an official-marketplace review from any other hash that happens to be listed.
func entryLabel(e reputation.Entry) string {
	s := e.Name
	if e.Version != "" {
		s += " " + e.Version
	}
	if e.Publisher != "" {
		s += " (publisher " + e.Publisher + ")"
	}
	if e.Source == reputation.SourceClaudeDesktop {
		// The stamp is the desktop's updatedAt, not a commit: cutting it to seven characters
		// would print a year and a month.
		s += ", reviewed from the Claude Desktop built-in " + e.Path + " synced " + e.SHA
		return s
	}
	if e.Source != "" {
		s += ", reviewed from " + e.Source
		if e.SHA != "" {
			s += "@" + e.SHA[:min(7, len(e.SHA))]
		}
		if e.Path != "" {
			s += " :" + e.Path
		}
	}
	return s
}

// runJudge runs the M5 intent judge, cancel deferred so the context can't leak on a panic.
// Returns any advisory notes it produced.
//
// Two clocks doing different jobs: the per-call timeout keeps one slow endpoint response from
// starving the rest of the plan, while the total timeout is only a backstop against an endpoint
// that is neither answering nor failing. The budget — not the clock — is what bounds cost, and
// whatever either one cuts short is reported, never silently dropped.
func runJudge(cfg config.Config, arts []model.ArtifactReport, quiet bool) ([]model.Finding, *model.JudgeSummary) {
	summary := &model.JudgeSummary{Artifacts: len(arts), Endpoint: cfg.LLM.BaseURL}
	notRun := func(title string, err error) ([]model.Finding, *model.JudgeSummary) {
		summary.Reason = err.Error()
		return []model.Finding{{
			RuleID: "LLM-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM,
			Title: title, Why: err.Error(),
		}}, summary
	}
	call, total, err := cfg.LLM.Durations()
	if err != nil { // already validated at load; treat as unconfigured rather than crashing
		return notRun("LLM judge: partial coverage", err)
	}
	if err := cfg.LLM.CheckEndpoint(); err != nil {
		return notRun("LLM judge did not run: endpoint refused", err)
	}
	key, err := cfg.ResolveAPIKey()
	if err != nil {
		// A key file that is missing or readable by others is a configuration fault, and it
		// is reported the way every other judge shortfall is: as an LLM-000 the operator sees,
		// not as N opaque 401s. Static results are untouched — the judge only ever adds.
		return notRun("LLM judge did not run: API key unavailable", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()
	client := judge.NewHTTP(cfg.LLM.BaseURL, key, cfg.LLM.Model, nil)

	started := time.Now()
	notes, stats := judge.Run(ctx, client, arts, judge.Options{
		Progress:    progressPrinter(quiet),
		Concurrency: cfg.LLM.Concurrency,
		CallTimeout: call,
		MaxCalls:    cfg.LLM.MaxCalls,
		MaxRetries:  cfg.LLM.MaxRetries,
		Samples:     cfg.LLM.Samples,
	})
	summary.Ran = true
	summary.Calls, summary.Failed, summary.Skipped = stats.Calls, stats.Failed, stats.Skipped
	// Cost goes into the summary whether or not anyone is watching stderr: quiet is how every
	// Downloads item is judged, and a driver that reads --json (the baseline adapter) never sees
	// stderr at all — those are the runs a cost is read from afterwards.
	summary.TriageCalls, summary.Retries = stats.TriageCalls, stats.Retries
	summary.PromptTokens, summary.CompletionTokens = client.Usage()
	for _, a := range arts {
		for _, f := range a.Findings {
			if f.Source == model.SrcLLM && f.Dimension > 0 {
				summary.Findings++
			}
		}
	}
	if !quiet && stats.Calls > 0 {
		// The same numbers as the JSON summary, for the operator at the terminal; the human
		// report deliberately carries no cost line.
		prompt, completion := summary.PromptTokens, summary.CompletionTokens
		fmt.Fprintf(os.Stderr,
			"LLM judge: %d call(s) in %s (p50 %s, p95 %s) · %d retry · %d failed · %d tokens in / %d out\n",
			stats.Calls, time.Since(started).Round(time.Millisecond),
			stats.Percentile(0.5).Round(time.Millisecond), stats.Percentile(0.95).Round(time.Millisecond),
			stats.Retries, stats.Failed, prompt, completion)
	}
	return notes, summary
}

// progressPrinter reports judge progress on stderr — stdout may be carrying --json, and a
// scan that prints nothing for a minute is indistinguishable from one that has hung.
//
// On a terminal it rewrites one line in place. Anywhere else (a pipe, a CI log) it prints the
// plan size once and then stays quiet: a log full of counter updates is worse than no counter.
func progressPrinter(quiet bool) func(done, total int) {
	if quiet {
		return nil
	}
	tty := false
	if fi, err := os.Stderr.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	return func(done, total int) {
		switch {
		case done == 0:
			fmt.Fprintf(os.Stderr, "LLM judge: %d call(s) planned…\n", total)
		case tty && done < total:
			fmt.Fprintf(os.Stderr, "\rLLM judge: %d/%d…", done, total)
		case tty:
			fmt.Fprint(os.Stderr, "\r\033[K") // clear the counter; the summary line follows
		}
	}
}

// isLoopbackEndpoint reports whether a base_url points at the local machine. Used to warn
// when skill excerpts would leave the box (privacy disclosure). Unparseable → treated as
// non-loopback (warn on the safe side).
func isLoopbackEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// resolveIgnorePath returns the explicit --ignore path, else <root>/.aguardignore, else "".
//
// A baseline is only auto-discovered for scan/clean, where root is the operator's own
// environment. `check` gets "" instead, for two reasons that both come down to root being
// the UNTRUSTED TARGET there:
//
//   - Trust: a skill shipping its own `.aguardignore` listing its own rule IDs used to
//     suppress itself to 100/100 and exit 0. The target must not get to write the baseline
//     it is judged against; only the operator can, via --ignore.
//   - Correctness: for `check <file>`, root is a FILE, so <root>/.aguardignore was
//     "<file>/.aguardignore" — ENOTDIR, which aborted the whole gate with exit 2 on a target
//     the scanner could otherwise read perfectly well.
func resolveIgnorePath(root, explicit string, auto bool) string {
	if explicit != "" {
		return explicit // operator-supplied: honoured wherever it points
	}
	if !auto {
		return ""
	}
	return filepath.Join(root, ".aguardignore")
}

// analyze runs detect → permcheck → baseline-suppress → hygiene → score.
func analyze(root string, res collect.Result, o scanOpts) (model.ScanResult, error) {
	cfg, err := config.LoadUser(o.cfgPath)
	if err != nil {
		return model.ScanResult{}, err
	}
	arts, covNotes := detect.New().Run(root, res.Artifacts)
	// Managed policy loads before everything and cannot be excluded, but it lives at an absolute OS
	// path unrelated to --root, so it is disclosed here rather than collected: folding it into
	// findings would make the reproducible score depend on how the machine is administered.
	covNotes = append(covNotes, collect.ManagedPolicyNotes()...)
	// Permission audit: attach findings to the KindPermission artifact so they score.
	// The settings env block is also KindPermission and sits at the same path; auditing it would
	// attach every PERM-* finding a second time.
	for i := range arts {
		if arts[i].Kind == model.KindPermission && !strings.HasPrefix(arts[i].Name, collect.SettingsEnvName) {
			arts[i].Findings = append(arts[i].Findings, permcheck.Audit(arts[i].Path)...)
		}
	}
	// Reputation (D11 v1): the embedded, versioned list is offline + deterministic, so it
	// may feed the score. Known-GOOD artifacts have their findings suppressed (cuts the
	// trusted-tool noise, e.g. superpowers); known-MALICIOUS ones get a REP-BAD critical finding.
	if !o.noReputation {
		covNotes = append(covNotes, applyReputation(reputation.Load(), arts)...)
	}
	// Baseline suppression (.aguardignore): drop reviewed-acceptable findings BEFORE
	// scoring, and record the count as a note (never silent).
	ign := ignore.None()
	if p := resolveIgnorePath(root, o.ignorePath, o.autoBaseline); p != "" {
		if ign, err = ignore.Load(p); err != nil {
			return model.ScanResult{}, err
		}
	}
	if sup := ign.Apply(arts); sup.Count > 0 {
		// The note mirrors the highest suppressed severity (§12 honesty): a baseline that
		// silences a critical must not read as a low-severity footnote, since suppression
		// happens before scoring and would otherwise lift the leaky-bucket cap invisibly.
		sev := sup.MaxSeverity
		if sev == "" {
			sev = model.SevLow
		}
		covNotes = append(covNotes, model.Finding{
			RuleID: "IGN-000", Dimension: 0, Severity: sev, Source: model.SrcStatic,
			Title: "Findings suppressed by .aguardignore",
			Why: fmt.Sprintf("Baseline suppressed %d finding(s), highest severity=%s; review the ignore file if this is unexpected.",
				sup.Count, sup.MaxSeverity),
		})
	}
	// LLM intent judge (M5): OFF unless BOTH config enables it AND --llm is passed. It only
	// adds advisory (Source=llm) findings — never scored, never gates — so scoring stays
	// deterministic. Runs after suppression so an advisory can surface even on a trusted tool.
	var judgeSummary *model.JudgeSummary
	if o.llm {
		if cfg.JudgeReady() {
			if !isLoopbackEndpoint(cfg.LLM.BaseURL) {
				// Privacy disclosure (§2 red line): a non-local endpoint means best-effort-
				// redacted skill excerpts leave the machine. Redaction is not a guarantee.
				covNotes = append(covNotes, model.Finding{
					RuleID: "LLM-002", Dimension: 0, Severity: model.SevMedium, Source: model.SrcLLM,
					Title: "LLM judge endpoint is not local",
					Why: fmt.Sprintf("base_url %q is not loopback: best-effort-redacted skill excerpts are sent off this machine.",
						cfg.LLM.BaseURL),
				})
			}
			jn, js := runJudge(cfg, arts, o.quiet)
			covNotes = append(covNotes, jn...)
			judgeSummary = js
		} else {
			why := "--llm was passed but config llm.enabled is false (or endpoint unset); ran static-only. `aguard llm setup` configures it."
			covNotes = append(covNotes, model.Finding{
				RuleID: "LLM-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM,
				Title: "LLM intent judge requested but not enabled",
				Why:   why,
			})
			judgeSummary = &model.JudgeSummary{Artifacts: len(arts), Reason: why}
		}
	}
	hyg := hygiene.Analyze(root, arts, hygiene.Options{Zombie: o.zombie})
	// The same baseline answers cleanup items, addressed by ID rather than by rule. It is how
	// `--keep-both` is recorded: an item the operator has decided about stops being listed. Like
	// every other suppression it is COUNTED and stated (invariant #5) — and unlike a finding, a
	// cleanup item carries no severity to mirror, so the count is the entire disclosure.
	if kept, n := ign.ApplyItems(hyg); n > 0 {
		hyg = append(kept, model.CleanItem{
			Kind: "baseline",
			Detail: fmt.Sprintf("%d cleanup item(s) suppressed by the baseline (accepted earlier, e.g. "+
				"via --keep-both). They return automatically if either side changes, since item IDs "+
				"are derived from their targets.", n),
		})
	}
	if hyg == nil {
		hyg = []model.CleanItem{}
	}
	notes := append(res.Notes, covNotes...)
	out := model.ScanResult{
		Root:         root,
		ScannedAt:    time.Now().Unix(),
		ToolVersion:  version,
		RulesVersion: detect.RulesVersion(),
		Env:          res.Env,
		Artifacts:    arts,
		Hygiene:      hyg,
		Notes:        notes,
		Judge:        judgeSummary,
		Locations:    scanLocations(root),
	}
	if env := collect.DetectEnvironment(root); env.Kind == collect.EnvSandbox {
		out.Sandbox = &model.SandboxInfo{Signals: env.Signals}
	}
	score.Apply(&out)
	return out, nil
}

// writeSARIF renders the scan as a SARIF 2.1.0 log. Written to a path rather than stdout so a CI job
// can emit machine-readable results AND an annotated pull request from one run.
func writeSARIF(path string, out model.ScanResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return report.SARIF(f, out, version, "https://github.com/basdotio/AgentGuard")
}

// writeMarkdown renders the scan as GitHub-flavoured markdown, to a file or — for "-" — to
// stdout, for pasting into a pull request comment or an issue. Written before the gate runs,
// like SARIF: the run that fails the gate is the one whose report most needs pasting.
func writeMarkdown(path string, out model.ScanResult) error {
	if path == "-" {
		return report.Markdown(os.Stdout, out)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return report.Markdown(f, out)
}

// writeHTML renders the scan result to an HTML file.
func writeHTML(path string, out model.ScanResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return report.HTML(f, out)
}

// mdFlagHelp is shared by scan and check: the two must describe the same flag the same way.
const mdFlagHelp = "also write the report as GitHub-flavoured markdown to this path, for a pull request comment or an issue (\"-\" = stdout, replacing the terminal report); same content as the terminal, --verbose does not change it"

// emitMarkdown writes the --md report when asked, and says where it went unless it went to
// stdout. One helper for scan and check, so the two cannot diverge on the "-" convention.
func emitMarkdown(path string, out model.ScanResult, quiet bool) error {
	if path == "" {
		return nil
	}
	if err := writeMarkdown(path, out); err != nil {
		return err
	}
	if path != "-" && !quiet {
		fmt.Fprintf(os.Stderr, "Markdown written to %s\n", path)
	}
	return nil
}

// writeReport picks the terminal report's mode. One helper rather than an if at each call
// site: `scan` and `check` must render the same result the same way, and two copies of the
// choice is how they would stop.
func writeReport(w io.Writer, out model.ScanResult, verbose bool) {
	if verbose {
		report.TextVerbose(w, out)
		return
	}
	report.Text(w, out)
}

func main() {
	var root, cfgPath, ignorePath, scanFailOn, scanFailOnLLM, checkFailOn, checkFailOnLLM, htmlOut, inboxDir string
	var autoReport bool
	var asJSON, quiet, verbose, useLLM, zombie, noRep bool
	var sarifOut, mdOut string

	rootCmd := &cobra.Command{
		Use:           "aguard",
		Short:         "AgentGuard — local, read-only security scan + junk cleanup for AI agent environments",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&root, "root", defaultRoot(), "Claude Code config root directory ($CLAUDE_CONFIG_DIR when set, else ~/.claude)")
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default: $XDG_CONFIG_HOME/aguard/config.yaml or ~/.config/aguard/config.yaml; for the LLM judge and the gate)")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "quiet: errors only")
	// The pair to --quiet, and persistent for the same reason: "how much do you want to read"
	// is a property of the reader, not of the subcommand. It widens the terminal report only —
	// --json and --html are machine/archive surfaces and carry everything in both modes, so a
	// CI job's output never depends on which one a human happened to pass.
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose: also print the full text of every coverage note")
	rootCmd.PersistentFlags().StringVar(&ignorePath, "ignore", "", "baseline file to suppress reviewed findings (scan/clean default: <root>/.aguardignore; check requires this flag — it never reads a baseline from inside the audited target)")
	rootCmd.PersistentFlags().BoolVar(&noRep, "no-reputation", false, "disable the embedded reputation allowlist/blocklist (offline, on by default)")

	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Full scan: enumerate and check skills/MCP/hooks/permissions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Both gates are checked before the scan, so a refusal costs no judge request and
			// prints no report (validateFailGates).
			cfg, err := config.LoadUser(cfgPath)
			if err != nil {
				return err
			}
			if _, _, err := validateFailGates(scanFailOn, scanFailOnLLM, cfg.LLM.MayEscalate()); err != nil {
				return err
			}
			out, err := scanEnv(root, scanOpts{cfgPath: cfgPath, ignorePath: ignorePath, zombie: zombie, noReputation: noRep, llm: useLLM, quiet: quiet})
			if err != nil {
				return err
			}
			// The Downloads scan rides along AFTER scoring and never touches it (see inbox.go).
			dl := model.Location{Name: "Downloads", Path: inboxDir, Status: model.LocOff}
			if inboxDir != inboxOff && inboxDir != "" {
				ib, err := scanInbox(inboxDir, cmd.Flags().Changed("inbox"), scanOpts{cfgPath: cfgPath, noReputation: noRep, llm: useLLM, quiet: true})
				if err != nil {
					return err
				}
				out.Inbox = ib
				dl.Status = model.LocAbsent
				if ib != nil {
					dl.Path, dl.Status = ib.Dir, model.LocRead
				}
			}
			out.Locations = append(out.Locations, dl)
			// Appended AFTER scoring, which is safe and deliberate: it is a dimension-0 note,
			// so score.Deterministic excludes it from both the number and --fail-on. The gate's
			// liveness is a fact about the REPORT's trustworthiness, not about any artifact —
			// folding it into the score would move a number that has to keep meaning "what the
			// scan found in your artifacts".
			out.Notes = append(out.Notes, gateLivenessNote(root)...)
			if autoReport && htmlOut == "" {
				p, err := defaultReportPath(time.Now())
				if err != nil {
					return err
				}
				htmlOut = p
			}
			if htmlOut != "" {
				if err := writeHTML(htmlOut, out); err != nil {
					return err
				}
				if !quiet {
					fmt.Fprintf(os.Stderr, "HTML report written to %s\n", htmlOut)
				}
			}
			// SARIF goes to a FILE, never stdout: --json already owns stdout, and a CI step that
			// wants both (a machine-readable result and an annotated pull request) must not have to
			// choose. Written before the gate runs, so a failing scan still produces its annotations.
			if sarifOut != "" {
				if err := writeSARIF(sarifOut, out); err != nil {
					return err
				}
				if !quiet {
					fmt.Fprintf(os.Stderr, "SARIF written to %s\n", sarifOut)
				}
			}
			if err := emitMarkdown(mdOut, out, quiet); err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
			} else if !quiet && mdOut != "-" {
				writeReport(os.Stdout, out, verbose)
			}
			return failGate(out, scanFailOn, scanFailOnLLM, cfg.LLM.MayEscalate())
		},
	}
	scanCmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	scanCmd.Flags().StringVar(&htmlOut, "html", "", "also write a self-contained HTML report to this path")
	scanCmd.Flags().StringVar(&inboxDir, "inbox", defaultInbox(), "also check agent-shaped items (skill folders, plugins, MCP configs, instruction files, and .zip files holding them) found under this directory, each on its own — reported in a separate Downloads section, never part of the score; '"+inboxOff+"' disables")
	scanCmd.Flags().BoolVar(&autoReport, "report", false, "also write a self-contained HTML report to "+reportsDirHint+" (timestamped; the path is printed)")
	scanCmd.Flags().StringVar(&sarifOut, "sarif", "", "also write a SARIF 2.1.0 log to this path (for GitHub Code Scanning / any SARIF viewer)")
	scanCmd.Flags().StringVar(&mdOut, "md", "", mdFlagHelp)
	scanCmd.Flags().BoolVar(&useLLM, "llm", false, "enable the LLM intent judge — advisory only; set it up once with `aguard llm setup` (redacted excerpts are sent to the configured endpoint; never moves the deterministic score)")
	scanCmd.Flags().BoolVar(&zombie, "zombie", false, "also flag never-used skills via the usage log (weak signal, opt-in)")
	scanCmd.Flags().StringVar(&scanFailOn, "fail-on", "", "exit 1 when a DETERMINISTIC finding is at/above this level (low|medium|high|critical) — for CI/gates")
	scanCmd.Flags().StringVar(&scanFailOnLLM, "fail-on-llm", "", "exit 1 when a finding INCLUDING qualified LLM ones is at/above this level; needs config llm.authority: escalate (default: off)")

	checkCmd := &cobra.Command{
		Use:   "check <path>",
		Short: "Pre-install gate: scan a single skill/dir/file (static; --llm adds the judge)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// The same two gates as scan (spec §3): --fail-on reads deterministic findings only,
			// whether or not --llm ran, and --fail-on-llm needs the config's explicit authority.
			// Both are checked before the target is read, as on scan: checked only at the end,
			// check's default --fail-on high let a deterministic hit hide a bad --fail-on-llm
			// behind exit 1. What check takes and the load-time gate never does is --llm itself
			// — see gateOptions.
			cfg, err := config.LoadUser(cfgPath)
			if err != nil {
				return err
			}
			if _, _, err := validateFailGates(checkFailOn, checkFailOnLLM, cfg.LLM.MayEscalate()); err != nil {
				return err
			}
			out, err := checkTarget(args[0], scanOpts{cfgPath: cfgPath, ignorePath: ignorePath, noReputation: noRep, llm: useLLM, quiet: quiet})
			if err != nil {
				return err
			}
			if sarifOut != "" {
				if err := writeSARIF(sarifOut, out); err != nil {
					return err
				}
				if !quiet {
					fmt.Fprintf(os.Stderr, "SARIF written to %s\n", sarifOut)
				}
			}
			if err := emitMarkdown(mdOut, out, quiet); err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
			} else if !quiet && mdOut != "-" {
				writeReport(os.Stdout, out, verbose)
			}
			return failGate(out, checkFailOn, checkFailOnLLM, cfg.LLM.MayEscalate())
		},
	}
	checkCmd.Flags().StringVar(&sarifOut, "sarif", "", "also write a SARIF 2.1.0 log to this path (for GitHub Code Scanning / any SARIF viewer)")
	checkCmd.Flags().StringVar(&mdOut, "md", "", mdFlagHelp)
	checkCmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	checkCmd.Flags().StringVar(&checkFailOn, "fail-on", "high", "exit 1 when a DETERMINISTIC finding is at/above this level (low|medium|high|critical)")
	checkCmd.Flags().BoolVar(&useLLM, "llm", false, "also run the LLM intent judge on this target — advisory only, same opt-in as scan --llm (redacted excerpts are sent to the configured endpoint; never moves the deterministic score or --fail-on)")
	checkCmd.Flags().StringVar(&checkFailOnLLM, "fail-on-llm", "", "exit 1 when a finding INCLUDING qualified LLM ones is at/above this level; needs config llm.authority: escalate (default: off)")

	var apply, dryRun, keepBoth, ask bool
	var undoBatch, resolveID, keepSide string
	cleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Cleanup: list junk skills and reclaimable context; quarantine or restore zombies",
		Long: "Cleanup: list junk skills and reclaimable context; quarantine or restore zombies.\n\n" +
			"clean NEVER DELETES. Without --apply it only reports. With --apply it MOVES never-used\n" +
			"skills into <root>/.aguard-trash and records each move so --undo can replay it; deleting\n" +
			"is yours to do (rm -rf <root>/.aguard-trash) once you are sure.\n\n" +
			"Boundaries, all enforced at run time:\n" +
			"  · settings.json, settings.local.json, .claude.json, .mcp.json and anything under hooks/\n" +
			"    are never moved and never restored over (checked after resolving symlinks).\n" +
			"  · Only skills/ is quarantined. A symlink pointing out of it is reported, not followed.\n" +
			"  · Quarantined content is still scanned and scored, so cleanup cannot turn a failing\n" +
			"    score green.\n" +
			"  · --root must be a config root (~/.claude or <project>/.claude). Inside rules/, agents/,\n" +
			"    commands/ and friends the trash directory would itself be auto-loaded, so it is\n" +
			"    refused (exit 2) rather than producing a false 'quarantined'.\n" +
			"  · --undo re-derives every safety decision; the manifest is data, not authority.\n\n" +
			"Run it when no Claude Code session is active: Claude Code watches skills/, commands/ and\n" +
			"agents/ live, and this tool's lock only protects two aguard runs from each other.\n\n" +
			"Exit codes: 0 done · 2 refused, nothing changed · 3 acted partially, reasons printed.\n" +
			"Full guide: docs/clean-guide.zh-CN.md",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Undo needs no scan: it replays a recorded batch and re-derives every safety decision
			// from the filesystem rather than from what the manifest claims.
			if undoBatch != "" {
				res, err := clean.Undo(os.Stdout, root, undoBatch, dryRun)
				if err != nil {
					return err
				}
				return partialGate(res)
			}
			out, err := scanEnv(root, scanOpts{cfgPath: cfgPath, ignorePath: ignorePath, zombie: zombie, noReputation: noRep})
			if err != nil {
				return err
			}
			if apply {
				res, aerr := clean.Apply(os.Stdout, root, out, dryRun)
				if aerr != nil {
					return aerr
				}
				return partialGate(res)
			}
			if ask || resolveID != "" {
				res, rerr := runResolve(root, out, resolveID, keepSide, keepBoth, ask, dryRun)
				if rerr != nil {
					return rerr
				}
				return partialGate(res)
			}
			if keepSide != "" || keepBoth {
				return fmt.Errorf("--keep/--keep-both answer a specific pair; name it with --resolve <id> " +
					"(or --ask to be walked through every pair)")
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				// An ENVELOPE, not the bare array this used to be. A bare array cannot carry a
				// version, so any change to the item shape or to an enum value — the blocker
				// strings especially — broke consumers silently and undetectably. See
				// model.CleanPlanSchema.
				return enc.Encode(model.CleanPlan{
					Schema:      model.CleanPlanSchema,
					ToolVersion: out.ToolVersion,
					Root:        out.Root,
					Items:       out.Hygiene,
				})
			}
			report.Hygiene(os.Stdout, out.Hygiene)
			return nil
		},
	}
	cleanCmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cleanCmd.Flags().BoolVar(&apply, "apply", false, "MOVE never-used skills to <root>/.aguard-trash (never deletes; reversible with --undo; needs --zombie)")
	cleanCmd.Flags().StringVar(&undoBatch, "undo", "", "restore a quarantined batch by id, or \"last\" for the most recent outstanding one (re-checks every safety rule)")
	cleanCmd.Flags().BoolVar(&dryRun, "dry-run", false, "with --apply/--undo: preview without touching anything")
	cleanCmd.Flags().BoolVar(&zombie, "zombie", false, "also flag never-used skills via the usage log (weak signal, opt-in)")
	cleanCmd.Flags().StringVar(&resolveID, "resolve", "", "answer one duplicate pair by item id (D-…), together with --keep or --keep-both")
	// Its own flag, never a bare --resolve: pflag's NoOptDefVal would make `--resolve <id>` stop
	// binding, so the id would land in positional args and "answer THIS pair" would silently become
	// "walk every pair". Measured on a real run before the first version was withdrawn.
	cleanCmd.Flags().BoolVar(&ask, "ask", false, "walk every answerable duplicate pair and ask which side to keep")
	cleanCmd.Flags().StringVar(&keepSide, "keep", "", "with --resolve <id>: the side that SURVIVES; the other is quarantined. No default — choosing is the decision")
	cleanCmd.Flags().BoolVar(&keepBoth, "keep-both", false, "with --resolve <id>: keep both and stop listing the pair (records the id in <root>/.aguardignore)")
	// --apply with --undo used to run the undo and drop the apply without a word, so a mistyped
	// command reported success for work it never attempted. Two opposite mutations in one invocation
	// is a mistake worth naming, not resolving by precedence.
	cleanCmd.MarkFlagsMutuallyExclusive("apply", "undo")
	// Two opposite answers to one pair, or a choice attached to a command that has no pair to apply
	// it to. Both are mistakes worth naming rather than resolving by precedence — the cost of
	// guessing here is moving the skill the operator meant to keep.
	cleanCmd.MarkFlagsMutuallyExclusive("keep", "keep-both")
	cleanCmd.MarkFlagsMutuallyExclusive("resolve", "apply")
	cleanCmd.MarkFlagsMutuallyExclusive("resolve", "undo")
	// A choice attached to a command that has no pair to apply it to. These four were missing, so
	// `clean --apply --keep x` ran the apply and dropped the --keep without a word — the same shape
	// as the apply/undo collision the comment above says was fixed, left open on the two flags that
	// carry the operator's actual decision.
	cleanCmd.MarkFlagsMutuallyExclusive("keep", "apply")
	cleanCmd.MarkFlagsMutuallyExclusive("keep", "undo")
	cleanCmd.MarkFlagsMutuallyExclusive("keep-both", "apply")
	cleanCmd.MarkFlagsMutuallyExclusive("keep-both", "undo")
	cleanCmd.MarkFlagsMutuallyExclusive("ask", "apply")
	cleanCmd.MarkFlagsMutuallyExclusive("ask", "undo")
	cleanCmd.MarkFlagsMutuallyExclusive("ask", "resolve")
	cleanCmd.MarkFlagsMutuallyExclusive("ask", "keep")
	cleanCmd.MarkFlagsMutuallyExclusive("ask", "keep-both")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "version info",
		Run:   func(_ *cobra.Command, _ []string) { runVersion(os.Stdout, root) },
	}

	// hash computes the canonical hash of a skill/dir/file — the key a maintainer adds to
	// the reputation list (skill=tree hash, single-file=sha256; same hashing as scan).
	hashCmd := &cobra.Command{
		Use:   "hash <path>",
		Short: "Print the canonical reputation hash of a skill/dir/file",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := collect.CollectTarget(args[0])
			if err != nil {
				return err
			}
			for _, a := range res.Artifacts {
				fmt.Printf("%s  %s:%s\n", a.Hash, a.Kind, a.Name)
			}
			return nil
		},
	}

	rootCmd.AddCommand(scanCmd, checkCmd, cleanCmd, hashCmd, versionCmd)
	// The load-time gate: `hook` runs as a Claude Code hook, `approve` and
	// `approvals` are its manual half. They are assembled elsewhere because they need the
	// persistent flags by reference, and because main() is already the longest thing here.
	rootCmd.AddCommand(newGateCommands(&root, &cfgPath, &quiet)...)
	rootCmd.AddCommand(newLLMCommand(&cfgPath))
	if err := rootCmd.Execute(); err != nil {
		if ee, ok := err.(*failExit); ok {
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}

// partialGate turns "some of what you asked for did not happen" into a distinct exit code. Every
// skip is printed with its reason, but a script cannot read those: returning 0 after moving nothing
// let `aguard clean --apply && deploy` proceed believing the cleanup ran.
func partialGate(res clean.Result) error {
	if res.Skipped > 0 {
		return &failExit{code: 3}
	}
	return nil
}

type failExit struct{ code int }

func (e *failExit) Error() string {
	if e.code == 3 {
		return "some items were skipped (exit 3)"
	}
	return fmt.Sprintf("findings at/above threshold (exit %d)", e.code)
}

// failGate returns a code-1 sentinel error when findings meet either threshold.
//
// TWO gates, deliberately separate. --fail-on reads deterministic findings ONLY: it is the
// contract a CI pipeline can rely on, reproducible and untouched by any model output.
// --fail-on-llm reads the effective set as well, and is opt-in twice over — the flag must be
// passed AND the config must grant authority — because it is the only switch here that can
// fail someone's build on a probabilistic opinion.
//
// Both gates are validated before either is evaluated. Returning exit 1 on a deterministic hit
// first used to mean a typo or a missing grant on --fail-on-llm was never looked at — under
// check's default --fail-on high that was the common case — so the pipeline read a findings
// failure where it had a broken gate.
func failGate(out model.ScanResult, failOn, failOnLLM string, mayEscalate bool) error {
	sev, llmSev, err := validateFailGates(failOn, failOnLLM, mayEscalate)
	if err != nil {
		return err
	}
	if sev != "" && report.HasAtLeast(out, sev) {
		return &failExit{code: 1}
	}
	if llmSev != "" && report.HasAtLeastEffective(out, llmSev) {
		return &failExit{code: 1}
	}
	return nil
}

// validateFailGates parses both thresholds and checks the --fail-on-llm grant, returning the
// parsed levels ("" = that gate is off). scan and check call it BEFORE anything is collected or
// sent, so a gate the run cannot honour is refused with exit 2 — which spec §3 defines as "the
// scan did not happen" — instead of after the judge has been paid for and a report printed.
// failGate calls it again, so the up-front check and the final one cannot disagree.
func validateFailGates(failOn, failOnLLM string, mayEscalate bool) (sev, llmSev model.Severity, err error) {
	if sev, err = parseFailOn("--fail-on", failOn); err != nil {
		return "", "", err
	}
	if llmSev, err = parseFailOn("--fail-on-llm", failOnLLM); err != nil {
		return "", "", err
	}
	if llmSev != "" && !mayEscalate {
		// Refused, not ignored. A gate that silently never fires is worse than no gate: the
		// pipeline goes green forever and everyone believes they are covered.
		return "", "", fmt.Errorf("--fail-on-llm requires config llm.authority: %s (currently %q) — "+
			"the judge may only gate a build when you have explicitly granted it that",
			config.AuthorityEscalate, config.AuthorityAdvisory)
	}
	return sev, llmSev, nil
}

// parseFailOn validates a threshold flag. Empty means the gate is off.
func parseFailOn(flag, level string) (model.Severity, error) {
	if level == "" {
		return "", nil
	}
	sev := model.Severity(level)
	if sev.Rank() == 0 {
		return "", fmt.Errorf("invalid %s %q (want low|medium|high|critical)", flag, level)
	}
	return sev, nil
}

// runResolve dispatches the three shapes of "answer a duplicate pair": walk them interactively,
// keep one side, or accept the pair as-is.
//
// The flag parsing lives here rather than in clean so that package's entry points stay callable
// with an explicit decision already made — which is what makes them testable without a terminal.
func runResolve(root string, out model.ScanResult, id, keep string, keepBoth, ask, dryRun bool) (clean.Result, error) {
	if ask {
		return clean.ResolveInteractive(os.Stdout, os.Stdin, root, out, dryRun)
	}
	if keepBoth {
		item, err := clean.Item(out.Hygiene, id)
		if err != nil {
			return clean.Result{}, err
		}
		return clean.Result{}, clean.KeepBoth(os.Stdout, root, item, dryRun)
	}
	return clean.Resolve(os.Stdout, root, out, id, keep, dryRun)
}

// scanLocations names the places a root scan looks, with the state each was found in. It is the
// answer to "where did you look?", which a reader cannot reconstruct from the inventory: a machine
// without Claude Desktop and a machine whose desktop store was read and found empty both show
// eleven skills, and only this list tells them apart.
func scanLocations(root string) []model.Location {
	root = filepath.Clean(root)
	home := filepath.Dir(root)
	present := func(p string) string {
		if _, err := os.Stat(p); err == nil {
			return model.LocRead
		}
		return model.LocAbsent
	}
	locs := []model.Location{
		{Name: "Config root", Path: root, Status: model.LocRead},
		{Name: "User MCP config", Path: filepath.Join(home, ".claude.json"), Status: present(filepath.Join(home, ".claude.json"))},
	}
	if p := filepath.Join(home, ".mcp.json"); present(p) == model.LocRead {
		locs = append(locs, model.Location{Name: "Project MCP config", Path: p, Status: model.LocRead})
	}
	store, ok := collect.DesktopStore(home)
	st := model.LocAbsent
	if ok {
		st = model.LocRead
	}
	locs = append(locs, model.Location{Name: "Claude Desktop store", Path: store, Status: st})
	sessions, ok := collect.DesktopSessions(home)
	st = model.LocAbsent
	if ok {
		st = model.LocRead
	}
	locs = append(locs, model.Location{Name: "Desktop session cache", Path: sessions, Status: st})
	return locs
}
