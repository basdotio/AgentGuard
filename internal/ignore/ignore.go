// SPDX-License-Identifier: MIT
// Package ignore implements the baseline / suppression list (.aguardignore): a user's
// "I've reviewed these and they're acceptable" file, so repeat scans of a known-noisy
// environment don't keep surfacing the same findings. Suppression is COUNTED and
// reported (never silent) so a baseline can't hide a genuinely new risk.
package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/safeio"
)

// itemIDRE tells a CLEANUP ITEM id (Z-87845f60, D-121d192b) from a RULE id (INJ-004, COV-000).
//
// The two live in one file on purpose: an operator keeps one "I have reviewed these" list, not two.
// They are distinguishable without ambiguity — a rule id is letters then exactly three digits, an
// item id is a single letter then at least eight hex characters — so nothing needs a sigil and an
// old baseline keeps parsing unchanged.
var itemIDRE = regexp.MustCompile(`^[A-Z]-[0-9a-f]{8,}$`)

// rule is one .aguardignore line: a RuleID plus an optional path glob. A finding is
// suppressed when its RuleID matches AND (no glob, or some evidence file matches the
// glob). RuleID "*" matches any rule (glob-only suppression).
type rule struct {
	ruleID string
	glob   string // "" = any path
}

// Matcher suppresses findings against a loaded baseline.
type Matcher struct {
	rules []rule
	items map[string]bool // cleanup item IDs the operator has accepted
}

// None returns a matcher that suppresses nothing. Used where a baseline must NOT be
// auto-discovered — a `check` target is the thing under audit, so a .aguardignore shipped
// inside it is written by whoever wrote the artifact, not by the operator running the gate.
func None() *Matcher { return &Matcher{} }

// Load reads .aguardignore from path. A missing file yields an empty (no-op) matcher —
// not an error. Format, one rule per line (blank lines and `#` comments ignored):
//
//	RULE-ID                 # suppress this rule everywhere
//	RULE-ID  path/glob/*     # suppress this rule only under matching evidence paths
//	*        vendor/**       # suppress ANY rule under a path glob
func Load(path string) (*Matcher, error) {
	f, err := safeio.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Matcher{}, nil
		}
		return nil, err
	}
	defer f.Close()
	var m Matcher
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if itemIDRE.MatchString(fields[0]) {
			if m.items == nil {
				m.items = map[string]bool{}
			}
			m.items[fields[0]] = true
			continue
		}
		r := rule{ruleID: fields[0]}
		if len(fields) > 1 {
			r.glob = fields[1]
		}
		m.rules = append(m.rules, r)
	}
	return &m, sc.Err()
}

// Empty reports whether the matcher has no rules (nothing to suppress).
func (m *Matcher) Empty() bool { return m == nil || (len(m.rules) == 0 && len(m.items) == 0) }

// Suppresses reports whether a finding is covered by the baseline.
func (m *Matcher) Suppresses(f model.Finding) bool {
	if m == nil {
		return false
	}
	for _, r := range m.rules {
		if r.ruleID != "*" && r.ruleID != f.RuleID {
			continue
		}
		if r.glob == "" {
			return true
		}
		for _, e := range f.Evidence {
			if globMatch(r.glob, e.File) {
				return true
			}
		}
	}
	return false
}

// globMatch matches an evidence path against a baseline glob. Two forms, both
// path-boundary aware (a `*` never crosses `/`, so `a*` cannot match `abc/def`):
//   - "dir/**"  → recursive: any path under dir/ (prefix match on "dir/")
//   - otherwise → filepath.Match (single path segment for `*`)
func globMatch(glob, path string) bool {
	if glob == "**" {
		return true
	}
	if strings.HasSuffix(glob, "/**") {
		return strings.HasPrefix(path, strings.TrimSuffix(glob, "**"))
	}
	ok, _ := filepath.Match(glob, path)
	return ok
}

// Result reports what a baseline suppressed: how many findings, and the highest
// severity among them — so a baseline can't silently drop a critical off the report
// (the caller surfaces MaxSeverity in the note; §12 honesty).
type Result struct {
	Count       int
	MaxSeverity model.Severity
}

// Apply removes suppressed findings from every artifact in place and returns the count
// plus the highest suppressed severity.
func (m *Matcher) Apply(arts []model.ArtifactReport) Result {
	var res Result
	if m.Empty() {
		return res
	}
	for i := range arts {
		kept := arts[i].Findings[:0:0]
		for _, f := range arts[i].Findings {
			if m.Suppresses(f) {
				res.Count++
				if f.Severity.Rank() > res.MaxSeverity.Rank() {
					res.MaxSeverity = f.Severity
				}
				continue
			}
			kept = append(kept, f)
		}
		arts[i].Findings = kept
	}
	return res
}

// suppressesItem reports whether the operator has already answered this cleanup item.
//
// Item IDs are content-addressed over kind and target paths, which gives this suppression a
// property a rule-id baseline does not have: rename or move either side of a resolved duplicate
// pair and the ID changes, so the entry stops matching and the item COMES BACK. Going stale in the
// direction of showing more is the only acceptable direction for a list whose purpose is to hide
// things.
func (m *Matcher) suppressesItem(id string) bool {
	return m != nil && id != "" && m.items[id]
}

// ApplyItems drops accepted cleanup items and reports how many went. The caller must surface that
// count: suppression is never silent (invariant #5), and unlike findings these carry no severity to
// mirror, so the count IS the whole disclosure.
func (m *Matcher) ApplyItems(items []model.CleanItem) ([]model.CleanItem, int) {
	if m.Empty() || len(m.items) == 0 {
		return items, 0
	}
	kept := items[:0:0]
	n := 0
	for _, it := range items {
		if it.ID != "" && m.items[it.ID] {
			n++
			continue
		}
		kept = append(kept, it)
	}
	return kept, n
}
