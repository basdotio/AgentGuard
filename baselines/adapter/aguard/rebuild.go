// SPDX-License-Identifier: MIT

package aguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/basdotio/AgentGuard/baselines/corpus"
	"github.com/basdotio/AgentGuard/baselines/ledger"
	"github.com/basdotio/AgentGuard/internal/model"
)

// Rebuild returns the ledger row Scan returned for s, from the bytes Scan kept in raw/ — without
// executing anything. It exists for runs that never wrote a ledger (the driver writes one only
// when every sample has finished) and for merging shards and retries, which need one row per
// sample built the same way whichever run it came from.
//
// The route is decided the way Scan decided it: by staging the sample tree. Stage only copies
// files, and its NotPlaceable reason is part of a `check`-routed row's detail, so a cheaper guess
// (the shape of the raw file's root, say) would produce a different row. The decoded result is
// returned too, so a caller folding the judge's findings reads the same decode the row came from
// rather than parsing the bytes a second time.
//
// Bytes that do not decode as a scan result are an error, never a row: a raw file cut short by a
// dying run must not become a benign verdict.
func (a *Adapter) Rebuild(s corpus.Sample, tree string, raw []byte) (ledger.Row, model.ScanResult, error) {
	row := ledger.Row{Sample: s.Sample, Surface: s.Surface, Attempted: true}
	var res model.ScanResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return ledger.Row{}, model.ScanResult{}, fmt.Errorf("raw output of %s is not a scan result: %w", s.Sample, err)
	}

	dir := filepath.Join(a.Work, filepath.Base(s.Sample))
	defer func() { _ = os.RemoveAll(dir) }()
	_, err := Stage(tree, s.Sample, dir)
	var np NotPlaceable
	switch {
	case errors.As(err, &np):
		return checked(row, res, raw, np.Reason, a.Threshold), res, nil
	case err != nil:
		return ledger.Row{}, model.ScanResult{}, fmt.Errorf("staging %s to find its route: %w", s.Sample, err)
	}
	return scanned(row, res, raw, a.Threshold), res, nil
}
