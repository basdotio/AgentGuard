// SPDX-License-Identifier: MIT
// Package reputation is the D11 v1 client: an EMBEDDED, versioned reputation list keyed
// by an artifact's canonical hash. It is fully offline (shipped in the binary) and thus
// deterministic/reproducible — so its verdicts may feed the risk score (spec §5.3). The
// cloud lookup (v2) will layer on top as advisory-only. No content ever leaves the box.
package reputation

import (
	_ "embed"
	"encoding/json"
)

//go:embed data/reputation.json
var raw []byte

// SourceClaudeDesktop is the Source of an entry for one of Claude Desktop's built-in skills
// (the Anthropic-managed skills the app syncs into its own store; see collect/desktop.go).
// Those skills have no public repository to pin, so the entry pins the app's own version
// stamp instead: SHA holds the skill's `updatedAt` from the desktop manifest, Path is
// "skills/<name>", and Publisher names Anthropic. Everything else about a good entry —
// reviewed, reason, findings fingerprint, hash-keyed match — is unchanged: the review is
// still of exact bytes, and a re-synced skill with a new stamp and a changed finding set
// still fails closed in the refresh. What is weaker, and stated: renewal needs a machine
// that has the desktop store (the weekly CI job does not), so these entries go stale until
// someone runs `make reputation-refresh` on a Mac with Claude Desktop installed.
const SourceClaudeDesktop = "claude-desktop"

// Verdict values.
const (
	Good      = "good"      // known-trusted artifact — its findings are suppressed
	Malicious = "malicious" // known-bad artifact — raises a REP-BAD finding
)

// Entry is one reputation record, keyed by canonical hash (spec §8).
//
// A GOOD entry suppresses scoring findings, so it is a statement made on a human's behalf and
// has to be able to answer "why was this trusted?" later. Source/SHA/Reviewed/Reason/Findings
// carry that answer: which tree, at which commit, read by someone on which day, what they
// concluded, and the exact set of findings that conclusion covers. hack/reputation-refresh
// renews an entry when the marketplace moves to a new commit whose findings are the SAME set
// (the review still applies to every finding present); a changed set fails closed and asks
// for a new review.
type Entry struct {
	Hash      string `json:"hash"`
	Verdict   string `json:"verdict"`
	Name      string `json:"name,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Version   string `json:"version,omitempty"`
	Ref       string `json:"ref,omitempty"`
	// Source is the git URL the hashed tree came from; SHA the commit it was checked out at.
	// The one non-URL Source is SourceClaudeDesktop, where SHA is the desktop's own stamp.
	// Required on every good entry (TestCuratedEntriesCarryTheirReview): an allowlist entry
	// nobody can trace back to a commit and a review is a baseline, not a curation.
	Source string `json:"source,omitempty"`
	SHA    string `json:"sha,omitempty"`
	// Path is the subdirectory of Source that IS the plugin, for plugins the marketplace vendors
	// inside its own repository (`"source": "./plugins/receipts"`) rather than pinning by
	// (url, sha). Empty when the plugin is the whole repository.
	Path string `json:"path,omitempty"`
	// Reviewed is the date a human read the findings; Reason is what they concluded.
	Reviewed string `json:"reviewed,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Findings is the fingerprint the review covers: one "RULE-ID <file>" per scoring finding,
	// sorted, duplicates kept (a multiset). Same fingerprint at a new commit → the review still
	// covers every finding present; anything added or removed → a human looks again.
	Findings []string `json:"findings,omitempty"`
}

// DB is the loaded embedded list.
type DB struct {
	Version string
	byHash  map[string]Entry
}

// Load parses the embedded reputation list. It never fails at runtime for a valid build
// (the JSON is validated by tests); a parse error yields an empty DB (fail-open to
// static-only, never a crash).
func Load() *DB {
	var doc struct {
		Version string  `json:"version"`
		Entries []Entry `json:"entries"`
	}
	db := &DB{byHash: map[string]Entry{}}
	if json.Unmarshal(raw, &doc) != nil {
		return db
	}
	db.Version = doc.Version
	for _, e := range doc.Entries {
		if e.Hash != "" {
			db.byHash[e.Hash] = e
		}
	}
	return db
}

// New builds a DB from explicit entries instead of the embedded file. It exists so the
// verdict-handling code can be tested independently of what the shipped list happens to
// contain: the blocklist half is empty pending curation, and "no curated entry yet" must not
// silently become "the REP-BAD path is never exercised". Production code uses Load.
func New(version string, entries []Entry) *DB {
	db := &DB{Version: version, byHash: map[string]Entry{}}
	for _, e := range entries {
		if e.Hash != "" {
			db.byHash[e.Hash] = e
		}
	}
	return db
}

// Match returns the reputation entry for a canonical hash, if known.
func (d *DB) Match(hash string) (Entry, bool) {
	if hash == "" {
		return Entry{}, false
	}
	e, ok := d.byHash[hash]
	return e, ok
}

// Len reports how many entries are loaded (for diagnostics/version output).
func (d *DB) Len() int { return len(d.byHash) }
