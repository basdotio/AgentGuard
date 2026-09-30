// SPDX-License-Identifier: MIT

// Package scrub replaces machine-identifying path prefixes in measurement output with stable
// placeholders. A run's ledger, fixtures and raw scan JSON quote paths the tools printed: the
// corpus checkout, the staging directory under the operator's temp directory, sometimes the
// operator's home. Committed once, those name a person and a machine forever (one committed
// ledger carried 1,669 copies of a maintainer's home directory). The prefixes are fixed per run,
// so a placeholder loses nothing a reader needs — <work>/<sample>/home/.claude says exactly
// where in the staged tree something was.
package scrub

import (
	"path/filepath"
	"sort"
	"strings"
)

// Scrubber rewrites a fixed set of prefixes. Build one per run with New.
type Scrubber struct {
	pairs []pair // longest prefix first
}

type pair struct{ prefix, placeholder string }

// New maps placeholder → path. Each path is registered as given and, when it resolves
// differently, in its symlink-resolved form too (macOS reports /private/var/… for /var/…).
// Empty paths are ignored: an empty prefix would match everything.
func New(placeholders map[string]string) *Scrubber {
	s := &Scrubber{}
	for ph, p := range placeholders {
		p = strings.TrimRight(p, "/")
		if p == "" {
			continue
		}
		s.pairs = append(s.pairs, pair{p, ph})
		if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
			s.pairs = append(s.pairs, pair{r, ph})
		}
	}
	sort.SliceStable(s.pairs, func(i, j int) bool { return len(s.pairs[i].prefix) > len(s.pairs[j].prefix) })
	return s
}

// String replaces every registered prefix in in.
func (s *Scrubber) String(in string) string {
	for _, p := range s.pairs {
		in = strings.ReplaceAll(in, p.prefix, p.placeholder)
	}
	return in
}

// Bytes is String on a byte slice, for a JSON document written as-is.
func (s *Scrubber) Bytes(in []byte) []byte {
	return []byte(s.String(string(in)))
}
