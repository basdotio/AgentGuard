// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	aguardadapter "github.com/basdotio/AgentGuard/baselines/adapter/aguard"
	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/judgefold"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
)

// folded is everything the outputs are written from.
type folded struct {
	list       []corpus.Sample
	answers    []judgefold.Answer // one per work-list sample, in work-list order
	rows       []ledger.Row       // the ledger rows the answers carry
	ledgerErrs []error            // ledger.Check against the full work list
	incomplete []corpus.Sample
	inputs     []string
}

func rawDir(dir string) string { return filepath.Join(dir, "raw") }

// fold reads every input and selects one answer per work-list sample.
func fold(o opts) (*folded, error) {
	list, err := readWorkList(o.samples)
	if err != nil {
		return nil, err
	}
	if err := refuseStrays(o.dirs, list); err != nil {
		return nil, err
	}
	ledgers, err := readLedgers(o.dirs)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "judgefold-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	// Bin is left empty on purpose: Rebuild reads bytes and stages trees, and nothing here may
	// execute the binary.
	ad := &aguardadapter.Adapter{Threshold: model.Severity(o.threshold), Work: work}
	opt := judgefold.Options{Threshold: model.Severity(o.threshold), Samples: o.judgeSamples}

	f := &folded{list: list}
	for _, d := range o.dirs {
		f.inputs = append(f.inputs, base(d))
	}
	for _, s := range list {
		a, err := answer(ad, opt, o, s, ledgers)
		if err != nil {
			return nil, err
		}
		f.answers = append(f.answers, a)
		if a.Ledger.Sample != "" {
			f.rows = append(f.rows, a.Ledger)
		}
		if a.Row.Incomplete != "" {
			f.incomplete = append(f.incomplete, s)
		}
	}
	f.ledgerErrs = ledger.Check(f.rows, corpus.IDs(list))
	return f, nil
}

// answer folds every attempt at one sample and selects one.
func answer(ad *aguardadapter.Adapter, opt judgefold.Options, o opts, s corpus.Sample, ledgers map[string]ledger.Row) (judgefold.Answer, error) {
	ann, err := corpus.ReadAnnotation(o.corpus, s)
	if err != nil {
		return judgefold.Answer{}, err
	}
	tree := filepath.Join(o.corpus, filepath.FromSlash(s.Path))
	name := filepath.Base(s.Sample) + ".json"
	var attempts []judgefold.Answer
	for i, d := range o.dirs {
		raw, err := os.ReadFile(filepath.Join(rawDir(d), name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return judgefold.Answer{}, err
		}
		lrow, res, rerr := ad.Rebuild(s, tree, raw)
		if rerr != nil {
			// A file a dying run cut short: an errored attempt, never a benign one.
			lrow = ledger.Row{Sample: s.Sample, Surface: s.Surface, Attempted: true, Outcome: ledger.Errored,
				Detail: "raw/" + name + " does not decode as a scan result"}
			res = model.ScanResult{}
		}
		a, err := judgefold.Fold(s, lrow, res, opt)
		if err != nil {
			return judgefold.Answer{}, fmt.Errorf("%s/raw/%s: %w", base(o.dirs[i]), name, err)
		}
		attempts = append(attempts, withOrigin(a, o.dirs[i], ann))
	}
	if len(attempts) == 0 {
		a, err := fromLedger(s, ledgers, opt)
		if err != nil {
			return judgefold.Answer{}, err
		}
		attempts = append(attempts, withOrigin(a, "", ann))
	}
	sel, _ := judgefold.Select(attempts)
	return sel, nil
}

func base(dir string) string { return filepath.Base(filepath.Clean(dir)) }

func withOrigin(a judgefold.Answer, dir string, ann corpus.Annotation) judgefold.Answer {
	if dir != "" {
		a.Dir = base(dir)
	}
	a.Source = ann.Source()
	return a
}

// fromLedger is the answer for a sample no raw/ holds. The driver keeps no raw/ for a tree aguard
// read nothing in, so a no-verdict (or errored) row from an input's ledger.jsonl is that sample's
// row. A scored row without raw/ has nothing the judge's findings could be folded from: it stays
// out of the ledger rows, so ledger.Check reports the hole instead of a static-only answer filling it.
func fromLedger(s corpus.Sample, ledgers map[string]ledger.Row, opt judgefold.Options) (judgefold.Answer, error) {
	lr, ok := ledgers[s.Sample]
	if !ok || lr.Outcome == ledger.Scored {
		a, err := judgefold.Fold(s, ledger.Row{}, model.ScanResult{}, opt)
		if err == nil && ok {
			row := a.Row
			row.Incomplete = "no answer: ledger.jsonl scored it but no raw/ holds its output, so the judge's findings cannot be folded"
			a.Row = row
		}
		return a, err
	}
	return judgefold.Fold(s, lr, model.ScanResult{}, opt)
}

func readWorkList(path string) ([]corpus.Sample, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	list, err := corpus.ReadSamples(fh)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("the work list %s is empty", path)
	}
	return list, nil
}

// refuseStrays fails on a raw/ file no work-list sample owns: raw/ from another work list is an
// operator mistake, and folding around it would leave its sample out of every count unannounced.
func refuseStrays(dirs []string, list []corpus.Sample) error {
	owned := map[string]bool{}
	for _, s := range list {
		owned[filepath.Base(s.Sample)+".json"] = true
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(rawDir(d))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !owned[e.Name()] {
				return fmt.Errorf("%s/raw/%s names no sample in the work list; fold with the work list the run used", base(d), e.Name())
			}
		}
	}
	return nil
}

// readLedgers reads each input's ledger.jsonl, when there is one; the first directory's row wins.
func readLedgers(dirs []string) (map[string]ledger.Row, error) {
	out := map[string]ledger.Row{}
	for _, d := range dirs {
		fh, err := os.Open(filepath.Join(d, "ledger.jsonl"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if strings.TrimSpace(sc.Text()) == "" {
				continue
			}
			var r ledger.Row
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				_ = fh.Close()
				return nil, fmt.Errorf("%s/ledger.jsonl:%d: %w", base(d), n, err)
			}
			if _, seen := out[r.Sample]; !seen {
				out[r.Sample] = r
			}
		}
		err = sc.Err()
		_ = fh.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
