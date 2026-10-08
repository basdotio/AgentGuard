// SPDX-License-Identifier: MIT

// Command baseline measures one scanner against agent-artifact-corpus and writes the four files
// that make the result citable: verdicts, ledger, scorecard input and run.yaml.
//
// It replaces hack/corpus-runner, which it supersedes rather than wraps. That runner returned
// before the binary was executed for any sample it could not place, so 127 test points were
// never handed to the scanner at all and the reason recorded was the runner's belief about
// aguard rather than aguard's behaviour. So every point in the corpus gets
// invoked, and whatever happens becomes a row.
//
//	go run ./baselines/cmd/baseline -tool aguard -aguard bin/aguard \
//	  -corpus ../agent-artifact-corpus -out baselines/results/aguard/2026-09-21
//
// The exit codes are the project's own contract: 0 clean, 1 a check refused to let the run be
// published, 2 a runtime error.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/basdotio/AgentGuard/baselines/adapter"
	aguardadapter "github.com/basdotio/AgentGuard/baselines/adapter/aguard"
	ccauditadapter "github.com/basdotio/AgentGuard/baselines/adapter/ccaudit"
	ciscoadapter "github.com/basdotio/AgentGuard/baselines/adapter/cisco"
	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/isolate"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/baselines/run"
	"github.com/basdotio/AgentGuard/baselines/scrub"
	"github.com/basdotio/AgentGuard/baselines/tripwire"
	"github.com/basdotio/AgentGuard/internal/model"
)

const (
	exitOK      = 0
	exitRefused = 1
	exitError   = 2
)

// toolRegistry is baselines/tools.yaml.
type toolRegistry struct {
	Tools []toolEntry `yaml:"tools"`
}

type toolEntry struct {
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Uploads      bool              `yaml:"uploads_samples"`
	UploadsBasis string            `yaml:"uploads_samples_basis"`
	Executes     bool              `yaml:"executes_scanned_content"`
	Provisional  bool              `yaml:"provisional"`
	Threshold    string            `yaml:"threshold"`
	Declared     map[string]string `yaml:"surfaces_declared_uncovered"`
}

func main() {
	var (
		toolID    = flag.String("tool", "aguard", "which tool in baselines/tools.yaml to measure")
		bin       = flag.String("aguard", "bin/aguard", "the aguard binary (tool=aguard only)")
		corpusDir = flag.String("corpus", "../agent-artifact-corpus", "corpus checkout; sample paths are relative to it")
		samples   = flag.String("samples", "", "work list from `corpus samples`; empty means generate it")
		outDir    = flag.String("out", "", "directory for verdicts.jsonl, ledger.jsonl and run.yaml")
		registry  = flag.String("tools", "baselines/tools.yaml", "the tool registry")
		threshold = flag.String("threshold", "", "override the registry's threshold")
		workers   = flag.Int("j", 4, "parallel scans")
		rawDir    = flag.String("raw", "", "keep each sample's scan JSON here (not committed)")
		policy    = flag.String("policy", "", "the tool's policy file, when its pass/fail is policy-driven; hashed into run.yaml")
		toolBin   = flag.String("bin", "", "binary for a non-aguard tool (default: the tool's own name on PATH)")
		agExtra   = flag.String("aguard-extra-args", "", "LOCAL measurement only: extra args appended to every aguard scan invocation (never to check), space separated (e.g. \"--llm --config /path\")")
		agEnv     = flag.String("aguard-env", "", "LOCAL measurement only: comma-separated NAME=VALUE pairs or NAMEs to copy from this process, added to aguard's isolated env")
		agTimeout = flag.Duration("aguard-timeout", 0, "LOCAL measurement only: per-sample timeout override for aguard (a judge run needs minutes)")
		scanArgv  = flag.String("scan-argv", "", "override the scan invocation, space separated, with {{sample}} for the path")
		verArgv   = flag.String("version-argv", "", "override the version invocation, space separated")
		skipFix   = flag.Bool("no-fixtures", false, "skip the six injected-fault fixtures (they need to materialise a hostile tree)")
		scoreTool = flag.String("score-tool", "", "pass -tool <id> to `corpus score`, so the scorecard carries that tool's own out_of_scope section and both denominators (needs a corpus whose scorer has -tool)")
	)
	flag.Parse()

	if err := realMain(opts{
		tool: *toolID, bin: *bin, corpusDir: *corpusDir, samplesPath: *samples,
		outDir: *outDir, registryPath: *registry, threshold: *threshold,
		rawDir: *rawDir, policyPath: *policy, workers: *workers, skipFixtures: *skipFix,
		toolBin: *toolBin, scanArgv: *scanArgv, versionArgv: *verArgv,
		agExtra: *agExtra, agEnv: *agEnv, agTimeout: *agTimeout, scoreTool: *scoreTool,
	}); err != nil {
		var refusal refused
		if ok := asRefusal(err, &refusal); ok {
			fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
			os.Exit(exitRefused)
		}
		fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
		os.Exit(exitError)
	}
	os.Exit(exitOK)
}

// refused marks a failure that is a check declining to publish, not a malfunction. The two get
// different exit codes because "the run worked and the answer is do not publish this" is not an
// error to be retried.
type refused struct{ error }

func asRefusal(err error, target *refused) bool {
	if r, ok := err.(refused); ok {
		*target = r
		return true
	}
	return false
}

// opts is the parsed command line. A struct rather than ten positional parameters because the
// last two reorderings of that signature were both silent.
type opts struct {
	tool, bin, corpusDir, samplesPath string
	outDir, registryPath, threshold   string
	rawDir, policyPath                string
	toolBin, scanArgv, versionArgv    string
	agExtra, agEnv                    string
	scoreTool                         string
	agTimeout                         time.Duration
	workers                           int
	skipFixtures                      bool
}

func realMain(o opts) error {
	toolID, corpusDir := o.tool, o.corpusDir
	samplesPath, outDir, registryPath := o.samplesPath, o.outDir, o.registryPath
	thresholdOverride, rawDir, workers := o.threshold, o.rawDir, o.workers

	entry, err := loadTool(registryPath, toolID)
	if err != nil {
		return err
	}

	// The isolation gate. aguard's invariant #1 forbids executing scanned content for aguard;
	// it says nothing about anybody else's tool, and pointing one that executes MCP configs at
	// 300 malicious samples executes 300 payloads. No container is a refusal, never a
	// downgrade: a run that quietly proceeded without isolation would be the one case where
	// being wrong costs the operator their machine rather than their number.
	if entry.Executes {
		if err := isolate.Available(nil); err != nil {
			return refused{fmt.Errorf("%s is marked executes_scanned_content: %w", toolID, err)}
		}
		// Isolation is available; what is missing is an adapter that uses it. Saying so
		// precisely matters — "not implemented" and "your host cannot do this" send whoever
		// hits it to two different places.
		return refused{fmt.Errorf("%s is marked executes_scanned_content and this host can "+
			"isolate, but no containerised adapter exists yet; running it on the host is not "+
			"the fallback", toolID)}
	}

	threshold := entry.Threshold
	if thresholdOverride != "" {
		threshold = thresholdOverride
	}
	if threshold == "" {
		return fmt.Errorf("no threshold for %s: the registry leaves it unset, which is "+
			"deliberate for a policy-driven tool — state it with -threshold and it lands in "+
			"run.yaml as a choice rather than an inherited default", toolID)
	}

	work, err := os.MkdirTemp("", "baseline-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	xdg := filepath.Join(work, "xdg")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		return err
	}
	if rawDir != "" {
		if err := os.MkdirAll(rawDir, 0o755); err != nil {
			return err
		}
	}

	list, err := workList(corpusDir, samplesPath)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return fmt.Errorf("the work list is empty; there is nothing to measure")
	}

	ad, err := pick(o, toolID, threshold, work, xdg, rawDir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	version, err := ad.Version(ctx)
	if err != nil {
		return err
	}

	policyHash, err := hashPolicy(o.policyPath)
	if err != nil {
		return err
	}

	started := time.Now().UTC()
	rows := scanAll(ctx, ad, list, corpusDir, workers)

	// Invariant first, numbers second. A ledger that lost a sample makes every figure below it
	// a rate over an unknown denominator, so nothing is written until it holds.
	if errs := ledger.Check(rows, corpus.IDs(list)); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  ledger: %v\n", e)
		}
		return refused{fmt.Errorf("the ledger does not account for every test point (%d problems)", len(errs))}
	}

	findings := tripwire.Check(rows, tripwireSamples(list), threshold, entry.Declared)
	reportTripwire(findings)
	if blocking := tripwire.Blocking(findings); len(blocking) > 0 {
		return refused{fmt.Errorf("%d surface(s) need a declaration in baselines/tools.yaml "+
			"before this run can be published", len(blocking))}
	}

	// The six injected-fault fixtures. They are test points in this corpus too — the part of
	// layer 1 that cannot be a file — and they had never been run for any tool before this driver existed.
	// They go in their own file rather than the ledger: they are not in `corpus samples`, so a
	// ledger row for one would fail ledger.Check's "not in the work list" rule, and weakening
	// that check to admit them would blunt the one invariant this whole directory rests on.
	var fixtures []adapter.FixtureResult
	if !o.skipFixtures {
		if fixtures, err = runFixtures(ctx, ad, corpusDir, work); err != nil {
			return err
		}
		reportFixtures(os.Stderr, fixtures)
	}

	// Paths first, numbers second: from here on nothing written names this machine.
	scrubResults(rows, fixtures, corpusDir, work)

	counts := ledger.Tally(rows)
	extra := splitArgv(o.agExtra)
	if toolID != "aguard" {
		extra = nil // the passthrough is wired into the aguard adapter only
	}
	uploads, uploadsBasis := uploadsFor(entry, extra)
	meta := run.Run{
		Tool: toolID, ToolVersion: version, Threshold: threshold,
		PolicyHash: policyHash, ToolExtraArgs: extra,
		Uploads: uploads, UploadsBasis: uploadsBasis,
		CorpusCommit: corpusCommit(corpusDir), StartedAt: started,
		Adapter:      "agent-guard baselines/adapter/" + toolID,
		Placement:    ad.Placement(),
		Provisional:  entry.Provisional,
		Declarations: entry.Declared,
		Counts:       counts, WorkListSize: len(list),
	}
	if err := meta.Validate(); err != nil {
		return err
	}

	if outDir != "" {
		if err := writeAll(outDir, rows, meta); err != nil {
			return err
		}
		if err := writeFixtures(outDir, fixtures); err != nil {
			return err
		}
		// The scorecard is captured verbatim, never paraphrased. `corpus score` is built to
		// make dishonest readings hard — it prints a count instead of a rate when the
		// interval is too wide, keeps collected and constructed apart, and states what was
		// left uncovered. Every one of those guards is lost the moment a human retypes the
		// numbers into prose, so the file on disk is the scorer's own bytes.
		if err := writeScorecard(outDir, corpusDir, o.scoreTool, meta); err != nil {
			return err
		}
	} else if err := corpus.WriteVerdicts(os.Stdout, verdicts(rows)); err != nil {
		return err
	}

	summarise(os.Stderr, meta, rows, classes(list), time.Since(started))
	return nil
}

// pick chooses the adapter for a tool. Adding one is a case here plus a package under
// baselines/adapter/; the driver stays ignorant of how any of them work.
//
// A tool with no adapter is an error naming BOTH things that could be missing — the adapter or
// the binary — because "not implemented" and "not installed" send whoever hits it to two very
// different places, and the message that conflates them wastes an afternoon.
func pick(o opts, toolID, threshold, work, xdg, rawDir string) (adapter.Adapter, error) {
	switch toolID {
	case "aguard":
		var extraEnv []string
		for _, kv := range strings.Split(o.agEnv, ",") {
			kv = strings.TrimSpace(kv)
			if kv == "" {
				continue
			}
			if !strings.Contains(kv, "=") {
				kv = kv + "=" + os.Getenv(kv)
			}
			extraEnv = append(extraEnv, kv)
		}
		return &aguardadapter.Adapter{
			Bin: o.bin, Threshold: model.Severity(threshold),
			Work: work, XDG: xdg, RawDir: rawDir,
			ExtraArgs: splitArgv(o.agExtra), ExtraEnv: extraEnv, Timeout: o.agTimeout,
		}, nil

	case "ccaudit":
		// Argument validation before environment checks: a mistyped threshold is worth hearing
		// about while preparing the run, not after downloading a binary to find out.
		//
		// cc-audit's gate is not a severity name. It has two tiers — the default, and --strict,
		// which adds medium/low AND promotes warnings to errors — and which one counts as
		// "fails the build" is a CHOICE that run.yaml records as one. Accepting "high" here
		// would silently measure a tier nobody chose.
		tier := ccauditadapter.ParseTier(threshold)
		if tier == "" {
			return nil, fmt.Errorf("threshold %q is not one of cc-audit's two tiers %v — it "+
				"gates on tiers, not severity words, and which tier counts as \"fails the "+
				"build\" is a choice run.yaml records as one",
				threshold, ccauditadapter.KnownTiers())
		}
		bin := o.toolBin
		if bin == "" {
			bin = "cc-audit"
		}
		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("cc-audit is not on PATH (looked for %q): the adapter exists "+
				"(baselines/adapter/ccaudit) but the binary does not. Install the pinned "+
				"release %s — `brew install ryo-ebata/tap/cc-audit` does not give a hash to "+
				"record, so the adapter takes the GitHub release tarball and verifies its .sha256",
				bin, ccauditadapter.PinnedVersion)
		}
		// Measured 2026-09-23: `cc-audit check` with no config exits 2 with "Configuration file
		// not found" and scans nothing at all. The config is also policy — it sets per-rule
		// error/warn/ignore — so it goes through -policy, which the driver already hashes into
		// run.yaml. Requiring it here rather than letting the tool discover one keeps a stray
		// .cc-audit.yaml in the corpus checkout from silently changing 3,539 answers.
		if o.policyPath == "" {
			return nil, fmt.Errorf("cc-audit needs a config file and refuses to scan without " +
				"one (exit 2, \"Configuration file not found\"). Generate the default with " +
				"`cc-audit init` and pass it with -policy, which also hashes it into run.yaml " +
				"— the file decides per-rule error/warn/ignore, so it is policy, not setup")
		}
		return &ccauditadapter.Adapter{
			Bin: bin, Tier: tier, RawDir: rawDir, Config: o.policyPath,
			ScanArgv: splitArgv(o.scanArgv), VersionArgv: splitArgv(o.versionArgv),
		}, nil
	case "skill-scanner":
		// Argument validation before environment checks, as for the others. skill-scanner's
		// --fail-on-severity takes its OWN five rungs; a SARIF level would be rejected at run
		// time, after a venv was built to find out.
		sev := ciscoadapter.ParseSeverity(threshold)
		if sev == "" {
			return nil, fmt.Errorf("threshold %q is not on skill-scanner's ladder %v — that is "+
				"what --fail-on-severity takes, and which rung counts as \"fails the build\" is a "+
				"choice run.yaml records as one", threshold, ciscoadapter.KnownSeverities())
		}
		bin := o.toolBin
		if bin == "" {
			bin = "skill-scanner"
		}
		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("skill-scanner is not on PATH (looked for %q): the adapter exists "+
				"(baselines/adapter/cisco) but the binary does not. Install the pinned PyPI release "+
				"%s into a venv (uv venv; uv pip install --require-hashes) and pass its console "+
				"script with -bin", bin, ciscoadapter.PinnedVersion)
		}
		// The built-in default policy is dumped to a file and
		// hashed, so "default" in run.yaml is a hash rather than "whatever 2.1.0 shipped".
		if o.policyPath == "" {
			return nil, fmt.Errorf("skill-scanner needs -policy: dump the built-in default policy to a " +
				"file first and pass it here, so run.yaml carries its hash rather than a version " +
				"number")
		}
		return &ciscoadapter.Adapter{
			Bin: bin, Threshold: sev, Policy: o.policyPath, RawDir: rawDir,
			ScanArgv: splitArgv(o.scanArgv), VersionArgv: splitArgv(o.versionArgv),
		}, nil
	}
	return nil, fmt.Errorf("no adapter for %q; implemented: aguard, ccaudit, skill-scanner", toolID)
}

// splitArgv turns a space-separated override into an argv, or nil for "use the default".
// uploadsFor decides the run's upload claim from what was actually passed to the tool. The
// registry's `uploads_samples: false` for aguard rests on "the LLM judge, which a baseline run
// does not enable" — true of the default invocation and false the moment -aguard-extra-args
// carries --llm. run.yaml is the file people cite, so it states the claim for THIS run in its
// own words instead of inheriting a sentence that no longer describes it. Any other extra flag
// is recorded (Run.ToolExtraArgs) and changes nothing here.
func uploadsFor(entry toolEntry, extra []string) (bool, string) {
	for _, a := range extra {
		if a == "--llm" {
			return true, "this run passed --llm to aguard: redacted excerpts of every judged artifact " +
				"were sent to the endpoint named in the judge config. The verdicts fold deterministic " +
				"findings only, so verdicts.jsonl and the scorecard are unaffected; the judge's output " +
				"exists only in raw/ (kept as a release asset, never in the tree) and is folded by hand."
		}
	}
	return entry.Uploads, entry.UploadsBasis
}

// scrubResults replaces the corpus checkout, the work directory, the operator's home and the
// temp directory in every free-text field about to be written, whichever adapter produced it.
// Doing it here rather than in each adapter means a new adapter cannot leak by forgetting; the
// aguard adapter scrubs its own raw/ output the same way (it is the only one that writes any).
func scrubResults(rows []ledger.Row, fixtures []adapter.FixtureResult, corpusDir, work string) {
	home, _ := os.UserHomeDir()
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	s := scrub.New(map[string]string{
		"<corpus>": abs(corpusDir), "<work>": abs(work), "<home>": home, "<tmp>": os.TempDir(),
	})
	for i := range rows {
		rows[i].Detail = s.String(rows[i].Detail)
	}
	for i := range fixtures {
		fixtures[i].Detail = s.String(fixtures[i].Detail)
	}
}

func splitArgv(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}

// scanAll runs the adapter over every sample. It preserves position rather than appending from
// goroutines, so the ledger's order is the work list's order regardless of scheduling.
func scanAll(ctx context.Context, ad adapter.Adapter, list []corpus.Sample, corpusDir string, workers int) []ledger.Row {
	rows := make([]ledger.Row, len(list))
	jobs := make(chan int)
	var wg sync.WaitGroup
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				s := list[idx]
				tree := filepath.Join(corpusDir, filepath.FromSlash(s.Path))
				rows[idx] = ad.Scan(ctx, s, tree)
			}
		}()
	}
	for i := range list {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return rows
}

// workList reads the work list, or asks the corpus for one.
func workList(corpusDir, samplesPath string) ([]corpus.Sample, error) {
	if samplesPath != "" {
		f, err := os.Open(samplesPath)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return corpus.ReadSamples(f)
	}
	cmd := exec.Command("go", "run", "./cmd/corpus", "samples")
	cmd.Dir = filepath.Join(corpusDir, "harness")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("generate the work list in %s: %w", cmd.Dir, err)
	}
	return corpus.ReadSamples(strings.NewReader(string(out)))
}

func tripwireSamples(list []corpus.Sample) []tripwire.Sample {
	out := make([]tripwire.Sample, 0, len(list))
	for _, s := range list {
		out = append(out, tripwire.Sample{
			Sample: s.Sample, Surface: s.Surface, Class: s.Class, Severity: s.Severity,
		})
	}
	return out
}

// verdicts derives the corpus's four-field records from the ledger. Only scored rows produce
// one: a no-verdict row must stay absent from this file so the scorer reports it as uncovered
// rather than counting silence as a benign call.
func verdicts(rows []ledger.Row) []corpus.Verdict {
	var out []corpus.Verdict
	for _, r := range rows {
		if r.Outcome != ledger.Scored {
			continue
		}
		out = append(out, corpus.Verdict{
			Sample: r.Sample, Verdict: r.Verdict, Severity: r.Severity, Dimensions: r.Dimensions,
		})
	}
	return out
}

func writeAll(dir string, rows []ledger.Row, meta run.Run) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	vf, err := os.Create(filepath.Join(dir, "verdicts.jsonl"))
	if err != nil {
		return err
	}
	if err := corpus.WriteVerdicts(vf, verdicts(rows)); err != nil {
		_ = vf.Close()
		return err
	}
	if err := vf.Close(); err != nil {
		return err
	}

	lf, err := os.Create(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		return err
	}
	sorted := make([]ledger.Row, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sample < sorted[j].Sample })
	enc := json.NewEncoder(lf)
	for _, r := range sorted {
		if err := enc.Encode(r); err != nil {
			_ = lf.Close()
			return err
		}
	}
	if err := lf.Close(); err != nil {
		return err
	}

	b, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	header := "# Written by baselines/cmd/baseline. Read baselines/README.md before citing any\n" +
		"# figure produced alongside this file.\n#\n# " + meta.Signature() + "\n\n"
	return os.WriteFile(filepath.Join(dir, "run.yaml"), append([]byte(header), b...), 0o644)
}

// runFixtures materialises the hostile trees, judges each, and ALWAYS restores. The restore is
// not optional and not best-effort: the traverse-only fixture leaves a `0111` directory that
// blocks the enumeration `rm -rf` needs, so skipping it leaves an undeletable tree in the
// operator's temp directory.
func runFixtures(ctx context.Context, ad adapter.Adapter, corpusDir, work string) ([]adapter.FixtureResult, error) {
	// FixtureRunner is optional. A tool that cannot be pointed at the injected-fault trees gets
	// six untestable rows naming why — never zero rows, because an absent section reads as "the
	// fixtures do not apply here" when it actually means nobody checked.
	runner, ok := ad.(adapter.FixtureRunner)
	if !ok {
		out := make([]adapter.FixtureResult, 0, len(aguardadapter.FixtureNames()))
		for _, name := range aguardadapter.FixtureNames() {
			out = append(out, adapter.FixtureResult{
				Fixture: name, Status: adapter.FixtureUntestable,
				Detail: "the " + ad.Tool() + " adapter does not implement FixtureRunner, so " +
					"nobody has pointed it at this fixture",
			})
		}
		return out, nil
	}
	dir := filepath.Join(work, "fixtures")
	harness := filepath.Join(corpusDir, "harness")

	mat := exec.Command("go", "run", "./cmd/corpus", "fixtures", "--materialize", dir)
	mat.Dir = harness
	if out, err := mat.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("materialise the fixtures: %w\n%s", err, out)
	}
	defer func() {
		res := exec.Command("go", "run", "./cmd/corpus", "fixtures", "--restore", dir)
		res.Dir = harness
		if out, err := res.CombinedOutput(); err != nil {
			// Loud, because the consequence is a directory the operator cannot delete.
			fmt.Fprintf(os.Stderr, "baseline: FIXTURE RESTORE FAILED — %s may not be "+
				"removable by hand: %v\n%s\n", dir, err, out)
		}
	}()

	names, err := fixtureNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]adapter.FixtureResult, 0, len(names))
	for _, name := range names {
		out = append(out, runner.Fixture(ctx, name, filepath.Join(dir, name)))
	}
	return out, nil
}

// fixtureNames reads the list from what `corpus fixtures --materialize` actually created.
//
// The list used to be aguardadapter.FixtureNames() — one tool's idea of the set, driving every
// tool's run. The fixtures belong to the CORPUS, so a fixture it adds must appear in every
// tool's results as an untestable row (both adapters return one for a name they do not know)
// rather than being invisible because our own adapter had not been taught it yet. That is the
// same defect as the hardcoded placement line, in the other direction: a list that silently
// under-reports instead of a label that silently misdescribes.
func fixtureNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the materialised fixtures in %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("`corpus fixtures --materialize %s` created no fixture "+
			"directories; an empty fixture section reads as \"they do not apply\" when it "+
			"means nobody ran them", dir)
	}
	sort.Strings(names)
	return names, nil
}

func writeFixtures(outDir string, results []adapter.FixtureResult) error {
	if len(results) == 0 {
		return nil
	}
	f, err := os.Create(filepath.Join(outDir, "fixtures.jsonl"))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// reportFixtures prints them one per line. Never a rate: a single counterexample settles a
// robustness claim, so an average over six would destroy the only information they carry.
func reportFixtures(w *os.File, results []adapter.FixtureResult) {
	fmt.Fprintln(w, "injected-fault fixtures — per fixture, pass/fail, never a rate:")
	for _, r := range results {
		fmt.Fprintf(w, "  %-20s %-11s %s\n", r.Fixture, r.Status, r.Detail)
	}
}

// hashPolicy fingerprints a policy file so that "the tool's default policy" cannot quietly
// become a different default between two runs that both claim the same threshold.
func hashPolicy(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the policy file: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// scoreArgs is the `go` argument list for grading verdicts. With scoreTool set, the corpus applies
// that tool's declared out_of_scope and appends a section with both denominators; empty keeps the
// invocation exactly as it was, because a tool the corpus has not registered would make it exit 2.
func scoreArgs(verdicts, scoreTool string) []string {
	args := []string{"run", "./cmd/corpus", "score"}
	if scoreTool != "" {
		args = append(args, "-tool", scoreTool)
	}
	return append(args, verdicts)
}

// writeScorecard asks the corpus to grade the verdicts and stores its output unedited, under the
// signature line. The signature goes first on purpose: whoever reads a figure here should have
// read whose adapter produced it before they reach the number.
func writeScorecard(outDir, corpusDir, scoreTool string, meta run.Run) error {
	abs, err := filepath.Abs(filepath.Join(outDir, "verdicts.jsonl"))
	if err != nil {
		return err
	}
	cmd := exec.Command("go", scoreArgs(abs, scoreTool)...)
	cmd.Dir = filepath.Join(corpusDir, "harness")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("score the verdicts in %s: %w", cmd.Dir, err)
	}
	header := fmt.Sprintf("# `corpus score` output, verbatim. Do not paraphrase it: the scorer's\n"+
		"# refusals (a count where a rate would mislead, collected kept apart from constructed,\n"+
		"# the uncovered tally) are the point, and retyping loses them.\n#\n"+
		"# %s\n# tool=%s version=%s threshold=%s corpus=%s at %s\n\n",
		meta.Signature(), meta.Tool, meta.ToolVersion, meta.Threshold, meta.CorpusCommit,
		meta.StartedAt.Format(time.RFC3339))
	return os.WriteFile(filepath.Join(outDir, "scorecard.txt"), append([]byte(header), out...), 0o644)
}

// corpusCommit records which corpus produced the work list. Without it the denominator is
// unknown, and raw/ is deliberately not committed precisely because this makes a rerun possible.
func corpusCommit(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func loadTool(path, id string) (toolEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return toolEntry{}, err
	}
	var reg toolRegistry
	if err := yaml.Unmarshal(b, &reg); err != nil {
		return toolEntry{}, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, t := range reg.Tools {
		if t.ID == id {
			return t, nil
		}
	}
	var have []string
	for _, t := range reg.Tools {
		have = append(have, t.ID)
	}
	return toolEntry{}, fmt.Errorf("no tool %q in %s (have: %s)", id, path, strings.Join(have, ", "))
}

func reportTripwire(fs []tripwire.Finding) {
	fmt.Fprintln(os.Stderr, "surfaces — did any come back with nothing flagged:")
	for _, f := range fs {
		line := fmt.Sprintf("  %-14s %-18s %d flagged of %d", f.Surface, f.Status, f.Flagged, f.N)
		if f.Advice != "" {
			line += "\n      " + f.Advice
		}
		if f.Declaration != "" {
			line += "\n      declared: " + f.Declaration
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

// summarise prints what the numbers rest on, then the tallies the scorer does not produce.
// Counts, never rates: `corpus score` owns the rates, and a second place computing them is a
// second place they can disagree.
func summarise(w *os.File, meta run.Run, rows []ledger.Row, classOf map[string]string, elapsed time.Duration) {
	fmt.Fprintf(w, "\n%s\n", meta.Signature())
	fmt.Fprintf(w, "tool=%s version=%s threshold=%s corpus=%s uploads=%v · %s\n",
		meta.Tool, meta.ToolVersion, meta.Threshold, meta.CorpusCommit, meta.Uploads,
		elapsed.Round(time.Millisecond))
	fmt.Fprintf(w, "ledger: %d scored · %d no-verdict · %d error = %d of %d test points\n",
		meta.Counts.Scored, meta.Counts.NoVerdict, meta.Counts.Errored,
		meta.Counts.Total(), meta.WorkListSize)

	reasons := map[ledger.Reason]int{}
	flags := map[ledger.Flag]int{}
	for _, r := range rows {
		if r.Reason != "" {
			reasons[r.Reason]++
		}
		for _, f := range r.Flags {
			flags[f]++
		}
	}
	if len(reasons) > 0 {
		fmt.Fprintln(w, "no verdict, by reason — the scorer lists these as uncovered:")
		for _, r := range ledger.KnownReasons() {
			if reasons[r] > 0 {
				fmt.Fprintf(w, "  %5d  %s — %s\n", reasons[r], r, ledger.Explain(r))
			}
		}
	}
	if len(flags) > 0 {
		fmt.Fprintln(w, "disclosure flags — each of these owes the published figure a second denominator:")
		for f, n := range flags {
			fmt.Fprintf(w, "  %5d  %s\n", n, f)
		}
	}
	reportRules(w, rows, classOf)
}

// classes maps sample id to class, for the per-class tallies below.
func classes(list []corpus.Sample) map[string]string {
	out := make(map[string]string, len(list))
	for _, s := range list {
		out[s.Sample] = s.Class
	}
	return out
}

// reportRules prints, per class, which of the tool's own rules carried a flag. `corpus score`
// cannot produce this — it knows nothing about any tool's rule ids — and it is the diagnostic a
// rule author acts on: "EXFIL-001 fired on 144 benign samples" names a work item, while "false
// positives are 5.5%" only names a status. Counts, never rates: the scorer owns the rates, and
// a second place computing them is a second place they can disagree.
func reportRules(w *os.File, rows []ledger.Row, classOf map[string]string) {
	flaggedBy := map[string]map[string]int{}
	flagged := map[string]int{}
	perClass := map[string]int{}
	for _, r := range rows {
		class := classOf[r.Sample]
		perClass[class]++
		if r.Outcome != ledger.Scored || r.Verdict != "malicious" {
			continue
		}
		flagged[class]++
		if flaggedBy[class] == nil {
			flaggedBy[class] = map[string]int{}
		}
		for _, id := range r.Rules {
			flaggedBy[class][id]++
		}
	}

	fmt.Fprintln(w, "flagged per class (counts over this run, not rates — corpus score owns the rates):")
	for _, c := range sortedKeys(perClass) {
		fmt.Fprintf(w, "  %-14s %d of %d\n", c, flagged[c], perClass[c])
	}
	fmt.Fprintln(w, "rules that carried a flag, per class (a sample counts once per rule):")
	for _, c := range sortedKeys(flaggedBy) {
		byRule := flaggedBy[c]
		ids := sortedKeys(byRule)
		sort.Slice(ids, func(i, j int) bool {
			if byRule[ids[i]] != byRule[ids[j]] {
				return byRule[ids[i]] > byRule[ids[j]]
			}
			return ids[i] < ids[j]
		})
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf("%s %d", id, byRule[id]))
		}
		fmt.Fprintf(w, "  %-14s %s\n", c, strings.Join(parts, " · "))
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
