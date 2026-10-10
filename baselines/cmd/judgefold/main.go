// SPDX-License-Identifier: MIT

// Command judgefold folds what the LLM judge found in benchmark runs of aguard into the files a
// judge run is cited by. It reads bytes runs already wrote — each input directory's raw/ (and its
// ledger.jsonl, when the driver got that far) — and never executes aguard, never calls a model,
// never re-asks one, and links no networking package (TestJudgefold_ImportsNoNetwork).
//
//	go run ./baselines/cmd/judgefold -samples samples.jsonl -corpus ../agent-artifact-corpus \
//	  -out /tmp/judge-fold /tmp/judge-run [/tmp/judge-run-2 ...]
//
// Several directories are one run in parts — shards, a resumed tail, whole-sample retries — merged
// by one rule: per sample, the first complete answer in argument order wins (judgefold.Select).
// It writes, into -out:
//
//	judge.jsonl        one row per work-list sample, the committed schema plus each vote's kind
//	verdicts.jsonl     what `corpus score` reads, folded at the judge's predicate
//	ledger.jsonl       the merged ledger, every row rebuilt by the aguard adapter's own code
//	per-kind-rule.txt  the generated per-(kind, rule) and per-source table
//	incomplete.jsonl   the work-list lines of every sample without a complete answer, for -samples
//
// Exit codes are the driver's: 0 written; 1 the ledger does not account for every work-list
// sample, so only incomplete.jsonl was written; 2 a runtime error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

const (
	exitOK      = 0
	exitRefused = 1
	exitError   = 2
)

// opts is the parsed command line.
type opts struct {
	samples, corpus, out, threshold string
	judgeSamples                    int
	dirs                            []string
}

func main() {
	var o opts
	flag.StringVar(&o.samples, "samples", "", "the run's work list (samples.jsonl); every sample in it must end somewhere")
	flag.StringVar(&o.corpus, "corpus", "../agent-artifact-corpus", "the corpus checkout the run read (for staging routes and each sample's source)")
	flag.StringVar(&o.out, "out", "", "directory to write the fold's files into")
	flag.StringVar(&o.threshold, "threshold", "high", "the verdict's severity bar; the run's own threshold")
	flag.IntVar(&o.judgeSamples, "judge-samples", 0, "the judge config's samples, for raw/ whose judge summary predates `samples`")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: judgefold -samples FILE -out DIR [flags] RUN_DIR...\n\n"+
			"Each RUN_DIR holds a raw/ written by baselines/cmd/baseline -raw. Earlier directories win ties.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	o.dirs = flag.Args()
	os.Exit(realMain(o, os.Stderr))
}

func (o opts) validate() error {
	var missing []string
	if o.samples == "" {
		missing = append(missing, "-samples")
	}
	if o.out == "" {
		missing = append(missing, "-out")
	}
	if len(o.dirs) == 0 {
		missing = append(missing, "at least one run directory")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	if model.Severity(o.threshold).Rank() == 0 {
		return fmt.Errorf("-threshold %q is not a severity (low, medium, high, critical)", o.threshold)
	}
	if o.judgeSamples < 0 {
		return fmt.Errorf("-judge-samples %d is negative", o.judgeSamples)
	}
	for _, d := range o.dirs {
		if fi, err := os.Stat(rawDir(d)); err != nil || !fi.IsDir() {
			return fmt.Errorf("%s has no raw/ directory: a run folds from what the driver kept with -raw", d)
		}
	}
	return nil
}

func realMain(o opts, stderr io.Writer) int {
	if err := o.validate(); err != nil {
		fmt.Fprintf(stderr, "judgefold: %v\n", err)
		return exitError
	}
	f, err := fold(o)
	if err != nil {
		fmt.Fprintf(stderr, "judgefold: %v\n", err)
		return exitError
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		fmt.Fprintf(stderr, "judgefold: %v\n", err)
		return exitError
	}
	if err := writeIncomplete(o.out, f); err != nil {
		fmt.Fprintf(stderr, "judgefold: %v\n", err)
		return exitError
	}
	if len(f.ledgerErrs) > 0 {
		for _, e := range f.ledgerErrs {
			fmt.Fprintf(stderr, "  ledger: %v\n", e)
		}
		fmt.Fprintf(stderr, "judgefold: the ledger does not account for every test point (%d problems); only "+
			"incomplete.jsonl was written — run the driver on it (-samples) and fold again with both directories\n",
			len(f.ledgerErrs))
		warnStale(stderr, o.out)
		return exitRefused
	}
	if err := writeAll(o.out, f, o); err != nil {
		fmt.Fprintf(stderr, "judgefold: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stderr, "judgefold: %d samples · %d complete · %d incomplete; wrote %s\n",
		len(f.answers), len(f.answers)-len(f.incomplete), len(f.incomplete), o.out)
	return exitOK
}

// warnStale says when -out still holds files of an earlier fold, which this one did not replace.
func warnStale(stderr io.Writer, out string) {
	for _, name := range []string{"judge.jsonl", "verdicts.jsonl", "ledger.jsonl", "per-kind-rule.txt"} {
		if _, err := os.Stat(out + string(os.PathSeparator) + name); err == nil {
			fmt.Fprintf(stderr, "judgefold: %s in %s is from an earlier fold and does not describe this one\n", name, out)
		}
	}
}
