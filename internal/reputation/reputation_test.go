// SPDX-License-Identifier: MIT
package reputation

import (
	"strings"
	"testing"
)

// TestEmbeddedListValid: the shipped reputation.json parses and every entry is well-formed
// (a bad seed file would silently disable reputation in production — catch it here).
func TestEmbeddedListValid(t *testing.T) {
	db := Load()
	if db.Len() == 0 {
		t.Fatal("embedded reputation list is empty (expected at least the seed entries)")
	}
	for h, e := range db.byHash {
		if len(h) != 64 { // sha256 hex
			t.Errorf("entry hash %q is not a 64-char sha256 hex", h)
		}
		if e.Verdict != Good && e.Verdict != Malicious {
			t.Errorf("entry %s has invalid verdict %q", h, e.Verdict)
		}
	}
}

// TestEmbeddedListHasNoPlaceholders: this file feeds the score — a GOOD entry suppresses
// findings and a MALICIOUS one raises a critical — so a demonstration record shipped here is
// a false record. An all-zero hash matches nothing and was therefore harmless in effect, but
// it still inflated `aguard version`'s entry count and made the blocklist look populated when
// it was not. An empty half is disclosed by being empty; it is not filled with an example.
func TestEmbeddedListHasNoPlaceholders(t *testing.T) {
	db := Load()
	for h, e := range db.byHash {
		if h == "0000000000000000000000000000000000000000000000000000000000000000" {
			t.Errorf("entry %s ships an all-zero placeholder hash", e.Name)
		}
		for _, s := range []string{e.Name, e.Publisher, e.Ref} {
			if strings.Contains(strings.ToLower(s), "placeholder") ||
				strings.Contains(strings.ToLower(s), "example") {
				t.Errorf("entry %s (%s) reads as a placeholder, not a curated entry", h, s)
			}
		}
	}
}

func TestMatch(t *testing.T) {
	db := Load()
	if _, ok := db.Match(""); ok {
		t.Error("empty hash must not match")
	}
	if _, ok := db.Match("deadbeef"); ok {
		t.Error("unknown hash must not match")
	}
	// superpowers is curated by NAME here, not by a hash literal: hack/reputation-refresh
	// rewrites the hash every time the marketplace pin moves with an unchanged finding set,
	// and the refresh workflow runs this package's tests right after writing. A pinned hash
	// would make every legitimate renewal red.
	var sp *Entry
	for _, e := range db.byHash {
		if e.Name == "superpowers" {
			e := e
			sp = &e
		}
	}
	if sp == nil {
		t.Fatal("superpowers entry missing from the embedded list")
	}
	if e, ok := db.Match(sp.Hash); !ok || e.Verdict != Good || e.Source == "" {
		t.Errorf("superpowers should match as good with a source; got ok=%v %+v", ok, e)
	}
}

// TestCuratedEntriesCarryTheirReview: a GOOD entry suppresses findings, so the record must be
// able to say which findings a human read and why they are benign. Every good entry needs
// Source, SHA, Reviewed, Reason and a non-empty Findings fingerprint — an unreasoned allowlist
// entry is a baseline nobody can audit. No exemptions: the one seed entry that had none (and
// was not an official-marketplace plugin) was removed on 2026-09-03 for exactly that reason.
func TestCuratedEntriesCarryTheirReview(t *testing.T) {
	db := Load()
	for h, e := range db.byHash {
		if e.Verdict != Good {
			continue
		}
		if e.Source == "" || e.SHA == "" || e.Reviewed == "" || e.Reason == "" || len(e.Findings) == 0 {
			t.Errorf("good entry %s (%s) is missing source/sha/reviewed/reason/findings — an allowlist entry must carry its review", h, e.Name)
		}
		if e.Source == SourceClaudeDesktop && (!strings.HasPrefix(e.Path, "skills/") || e.Publisher == "") {
			t.Errorf("claude-desktop entry %s must name the skill (path skills/<name>) and its publisher", e.Name)
		}
		for _, f := range e.Findings {
			if !strings.Contains(f, " ") {
				t.Errorf("entry %s fingerprint line %q is not \"RULE-ID <file>\"", e.Name, f)
			}
		}
	}
}
