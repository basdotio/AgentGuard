// SPDX-License-Identifier: MIT
package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

func TestFingerprint(t *testing.T) {
	arts := []model.ArtifactReport{{Findings: []model.Finding{
		{RuleID: "EXFIL-001", Dimension: 3, Evidence: []model.Evidence{{File: "tests/b.js", Line: 63}, {File: "tests/b.js", Line: 62}}},
		{RuleID: "COV-000", Dimension: 0, Evidence: []model.Evidence{{File: "x"}}},
		{RuleID: "EXEC-005", Dimension: 4, Evidence: []model.Evidence{{File: "tests/h.js", Line: 10}}},
		{RuleID: "EXEC-005", Dimension: 4, Evidence: []model.Evidence{{File: "tests/h.js", Line: 40}}},
		{RuleID: "EXEC-003", Dimension: 4, Evidence: []model.Evidence{{File: "skills/s.cjs"}}},
	}}}
	want := []string{
		"EXEC-003 skills/s.cjs",
		"EXEC-005 tests/h.js",
		"EXEC-005 tests/h.js", // two findings in one file stay two lines: a count is a fact
		"EXFIL-001 tests/b.js",
	}
	if got := fingerprint(arts); !reflect.DeepEqual(got, want) {
		t.Errorf("fingerprint = %q, want %q", got, want)
	}
}

func TestDiff(t *testing.T) {
	old := []string{"A f1", "B f2", "B f2", "C f3"}
	cases := []struct {
		name        string
		cur         []string
		add, remove []string
	}{
		{"identical", []string{"A f1", "B f2", "B f2", "C f3"}, nil, nil},
		{"one more B", []string{"A f1", "B f2", "B f2", "B f2", "C f3"}, []string{"B f2"}, nil},
		{"one fewer B", []string{"A f1", "B f2", "C f3"}, nil, []string{"B f2"}},
		{"new rule", []string{"A f1", "B f2", "B f2", "C f3", "EXFIL-001 evil.js"}, []string{"EXFIL-001 evil.js"}, nil},
		{"file renamed", []string{"A f9", "B f2", "B f2", "C f3"}, []string{"A f9"}, []string{"A f1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			add, remove := diff(old, c.cur)
			if !reflect.DeepEqual(add, c.add) || !reflect.DeepEqual(remove, c.remove) {
				t.Errorf("diff = +%q -%q, want +%q -%q", add, remove, c.add, c.remove)
			}
		})
	}
}

const marketplaceJSON = `{"plugins":[
	{"name":"superpowers","source":{"source":"url","url":"https://github.com/obra/superpowers.git","sha":"b36e0829c6d0140e93cfef2ca599b1b07d4a7797"}},
	{"name":"code-review","source":"./plugins/code-review"},
	{"name":"github","source":"./external_plugins/github"},
	{"name":"subdir-obj","source":{"source":"git-subdir","url":"https://x.example/r.git","path":"p"}},
	{"name":"unpinned","source":{"source":"url","url":"https://x.example/u.git"}}
]}`

// TestParsePins: an external repo is pinned by its own (url, sha); a vendored directory is
// pinned by the MARKETPLACE's (url, sha) plus its path. A git-subdir object and a url without
// a sha pin nothing.
func TestParsePins(t *testing.T) {
	mkURL, mkSHA := "https://github.com/anthropics/claude-plugins-official.git", "0123456789abcdef0123456789abcdef01234567"
	pins, err := parsePins([]byte(marketplaceJSON), mkURL, mkSHA)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]pin{
		"superpowers": {URL: "https://github.com/obra/superpowers.git", SHA: "b36e0829c6d0140e93cfef2ca599b1b07d4a7797"},
		"code-review": {URL: mkURL, SHA: mkSHA, Path: "plugins/code-review"},
		"github":      {URL: mkURL, SHA: mkSHA, Path: "external_plugins/github"},
	}
	if !reflect.DeepEqual(pins, want) {
		t.Errorf("pins = %+v, want %+v", pins, want)
	}
}

// TestParsePinsWithoutMarketplaceCommit: given a bare marketplace.json there is no commit to
// pin vendored plugins to, so they are left out rather than pinned to a guess.
func TestParsePinsWithoutMarketplaceCommit(t *testing.T) {
	pins, err := parsePins([]byte(marketplaceJSON), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins["superpowers"].SHA == "" {
		t.Errorf("only the (url, sha) plugin should be pinned; got %+v", pins)
	}
}

// TestMirrorIdentity: Claude Code's marketplace mirror is a downloaded tree with a `.gcs-sha`,
// not a git checkout. The official one is recognised by its directory name; any other mirror
// yields a commit but no repository, and is therefore not pinned.
func TestMirrorIdentity(t *testing.T) {
	base := t.TempDir()
	official := filepath.Join(base, "claude-plugins-official")
	other := filepath.Join(base, "somebody-else")
	for _, d := range []string{official, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".gcs-sha"), []byte("0120fb83da5d7cdaa52dd11979690f2dc5f76052\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if u, s := mirrorIdentity(official); u != defaultMarketplace || s != "0120fb83da5d7cdaa52dd11979690f2dc5f76052" {
		t.Errorf("official mirror = (%q, %q)", u, s)
	}
	if u, s := mirrorIdentity(other); u != "" || s == "" {
		t.Errorf("unknown mirror must yield a commit but no repository; got (%q, %q)", u, s)
	}
	if u, s := mirrorIdentity(t.TempDir()); u != "" || s != "" {
		t.Errorf("plain directory = (%q, %q), want empty", u, s)
	}
}

// TestClonerCleanupNeverRemovesAliasedDirs is the regression test for a real deletion: the
// first cloner kept one map, the operator's marketplace mirror was aliased into it so vendored
// plugins could be served offline, and cleanup() removed the mirror from ~/.claude. A directory
// the tool was HANDED must survive cleanup; only directories the tool created may go.
func TestClonerCleanupNeverRemovesAliasedDirs(t *testing.T) {
	handed := t.TempDir()
	marker := filepath.Join(handed, ".gcs-sha")
	if err := os.WriteFile(marker, []byte("0120fb83da5d7cdaa52dd11979690f2dc5f76052\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := os.MkdirTemp("", "aguard-refresh-test-*")
	if err != nil {
		t.Fatal(err)
	}

	cl := newCloner()
	cl.alias(defaultMarketplace, "0120fb83da5d7cdaa52dd11979690f2dc5f76052", handed)
	// Simulate a checkout the cloner made itself, without touching the network.
	cl.dirs["x@y"] = created
	cl.owned[created] = true

	if d, err := cl.get(defaultMarketplace, "0120fb83da5d7cdaa52dd11979690f2dc5f76052"); err != nil || d != handed {
		t.Fatalf("alias not served: %q %v", d, err)
	}
	cl.cleanup()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("cleanup removed a directory it was handed: %v", err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("cleanup left the directory it created: %v", err)
	}
}

func TestNormURLAndPublisher(t *testing.T) {
	for _, u := range []string{
		"https://github.com/anthropics/claude-plugins-official.git",
		"https://github.com/anthropics/claude-plugins-official",
		"https://github.com/anthropics/claude-plugins-official/",
		"HTTPS://GitHub.com/anthropics/claude-plugins-official.git",
	} {
		if got := normURL(u); got != "https://github.com/anthropics/claude-plugins-official" {
			t.Errorf("normURL(%q) = %q", u, got)
		}
	}
	if got := publisherFor(pin{URL: "https://github.com/obra/superpowers.git", SHA: "x"}); got != "obra via claude-plugins-official" {
		t.Errorf("external publisher = %q", got)
	}
	if got := publisherFor(pin{URL: "https://github.com/anthropics/claude-plugins-official.git", SHA: "x", Path: "plugins/receipts"}); got != "claude-plugins-official" {
		t.Errorf("vendored publisher = %q", got)
	}
}

// TestLocateDesktopSkill: Claude Desktop entries renew from the local store, so finding the skill
// there — with the stamp the entry pins — is the whole mechanism; and a machine without the store
// must come back as "not present", which renewDesktop reports and does NOT count as stale.
func TestLocateDesktopSkill(t *testing.T) {
	home := t.TempDir()
	if _, err := locateDesktopSkill(home, "skills/docx"); !errors.Is(err, errNoDesktopStore) {
		t.Fatalf("no store: err = %v, want errNoDesktopStore", err)
	}

	bundle := filepath.Join(home, "Library", "Application Support", "Claude", "local-agent-mode-sessions", "skills-plugin", "sess", "acct")
	write := func(rel, content string) {
		p := filepath.Join(bundle, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", `{"skills":[{"skillId":"docx","creatorType":"anthropic","updatedAt":"2026-08-31T20:00:31Z"},{"skillId":"mine","creatorType":"user","updatedAt":"2026-09-01T00:00:00Z"}]}`)
	write("skills/docx/SKILL.md", "---\nname: docx\n---\n")
	write("skills/mine/SKILL.md", "---\nname: mine\n---\n")

	sk, err := locateDesktopSkill(home, "skills/docx")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Stamp != "2026-08-31T20:00:31Z" || sk.Creator != "anthropic" || filepath.Base(sk.Dir) != "docx" {
		t.Errorf("located = %+v", sk)
	}
	if sk2, err := locateDesktopSkill(home, "mine"); err != nil || sk2.Creator != "user" {
		t.Errorf("bare name lookup = %+v, %v", sk2, err)
	}
	if _, err := locateDesktopSkill(home, "skills/absent"); err == nil || errors.Is(err, errNoDesktopStore) {
		t.Errorf("absent skill in a present store: err = %v, want a not-in-store error", err)
	}
}
