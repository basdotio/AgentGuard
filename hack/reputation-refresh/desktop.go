// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/reputation"
)

// Claude Desktop built-in skills: the one allowlist source that is not a repository.
//
// The desktop app syncs a set of Anthropic-managed skills into its own store (see
// internal/collect/desktop.go). Several of them are not published anywhere a commit could be
// pinned, so their entries pin the app's own per-skill `updatedAt` stamp as `sha`, and renewal
// reads the skill back from the local store instead of cloning. The rule is the marketplace
// rule unchanged — same bytes, or same finding fingerprint, else a human — with one honest
// limit: it can only run where the store exists. A machine without it (the weekly CI job runs
// on ubuntu) reports these entries as not checkable and leaves them alone; that is "cannot
// check", not "stale", and it must not turn the job red.

const (
	desktopEntrySuffix = " (Claude Desktop)"
	desktopPublisher   = "Anthropic (Claude Desktop built-in)"
	desktopCreator     = "anthropic"
)

var errNoDesktopStore = errors.New("no Claude Desktop store on this machine")

// desktopSkill is one built-in skill as found in the local store.
type desktopSkill struct {
	Dir     string // the skill directory (what gets hashed and checked)
	Stamp   string // the desktop's updatedAt for it — the entry's sha
	Creator string // creatorType from the desktop manifest
}

// locateDesktopSkill finds skills/<name> in the desktop store under home. entryPath is the
// entry's Path ("skills/<name>") or a bare skill name.
func locateDesktopSkill(home, entryPath string) (desktopSkill, error) {
	name := strings.TrimPrefix(entryPath, "skills/")
	bundles := collect.DesktopSkillBundles(home)
	if len(bundles) == 0 {
		return desktopSkill{}, errNoDesktopStore
	}
	for _, b := range bundles {
		dir := filepath.Join(b, "skills", name)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		stamp, creator, ok := collect.DesktopSkillRecord(b, name)
		if !ok || stamp == "" {
			return desktopSkill{}, fmt.Errorf("skill %s is in the desktop store but its manifest carries no updatedAt stamp to pin", name)
		}
		return desktopSkill{Dir: dir, Stamp: stamp, Creator: creator}, nil
	}
	return desktopSkill{}, fmt.Errorf("skill %s is not in this machine's desktop store", name)
}

// renewDesktop is run()'s second pass: Claude Desktop entries, checked against the local store.
// Returns how many entries changed and which need a human. An absent store is neither.
func renewDesktop(aguard, home string, list *listFile, out io.Writer) (int, []string, error) {
	changed := 0
	var stale []string
	for i := range list.Entries {
		e := &list.Entries[i]
		if e.Verdict != reputation.Good || e.Source != reputation.SourceClaudeDesktop {
			continue
		}
		sk, err := locateDesktopSkill(home, e.Path)
		if err != nil {
			fmt.Fprintf(out, "  %s: %v — not checkable here, left as is\n", e.Name, err)
			continue
		}
		if sk.Stamp == e.SHA {
			fmt.Fprintf(out, "  %s: up to date at %s\n", e.Name, e.SHA)
			continue
		}
		hash, err := hashOf(aguard, sk.Dir)
		if err != nil {
			return 0, nil, fmt.Errorf("%s: %w", e.Name, err)
		}
		if hash == e.Hash {
			fmt.Fprintf(out, "✓ %s: %s → %s, content unchanged\n", e.Name, e.SHA, sk.Stamp)
			e.SHA = sk.Stamp
			changed++
			continue
		}
		res, err := checkJSON(aguard, sk.Dir)
		if err != nil {
			return 0, nil, fmt.Errorf("%s: %w", e.Name, err)
		}
		got := fingerprint(res.Artifacts)
		added, removed := diff(e.Findings, got)
		if len(added)+len(removed) > 0 {
			fmt.Fprintf(out, "! %s: %s → %s changes the finding set — review needed, entry left at %s\n",
				e.Name, e.SHA, sk.Stamp, e.SHA)
			for _, f := range added {
				fmt.Fprintf(out, "    + %s\n", f)
			}
			for _, f := range removed {
				fmt.Fprintf(out, "    - %s\n", f)
			}
			stale = append(stale, e.Name)
			continue
		}
		fmt.Fprintf(out, "✓ %s: %s → %s, findings unchanged (%d); hash %s\n", e.Name, e.SHA, sk.Stamp, len(got), short(hash))
		e.Hash, e.SHA = hash, sk.Stamp
		changed++
	}
	return changed, stale, nil
}

// runAddDesktop drafts an entry for one Claude Desktop built-in skill, mirroring runAdd: locate,
// scan, hash, fingerprint, print the review sheet, and leave the reason to the human.
func runAddDesktop(aguard, home, skill string, write bool, out io.Writer) error {
	name := strings.TrimPrefix(skill, "skills/") + desktopEntrySuffix
	list, err := loadList()
	if err != nil {
		return err
	}
	for _, e := range list.Entries {
		if e.Name == name {
			return fmt.Errorf("%s is already listed (hash %s); edit or remove that entry instead of adding a second", name, short(e.Hash))
		}
	}
	sk, err := locateDesktopSkill(home, skill)
	if err != nil {
		return err
	}
	if sk.Creator != desktopCreator {
		return fmt.Errorf("%s has creatorType %q, not %q: only the app's built-ins belong in the shipped allowlist — a user's own upload is theirs to baseline with .aguardignore", skill, sk.Creator, desktopCreator)
	}
	res, err := checkJSON(aguard, sk.Dir)
	if err != nil {
		return err
	}
	fp := fingerprint(res.Artifacts)
	if len(fp) == 0 {
		fmt.Fprintf(out, "%s at %s scans clean (100/100): an allowlist entry would add nothing. Not written.\n", name, sk.Stamp)
		return nil
	}
	hash, err := hashOf(aguard, sk.Dir)
	if err != nil {
		return err
	}
	entry := reputation.Entry{
		Hash: hash, Verdict: reputation.Good, Name: name, Publisher: desktopPublisher,
		Source: reputation.SourceClaudeDesktop, SHA: sk.Stamp, Path: "skills/" + strings.TrimPrefix(skill, "skills/"),
		Reviewed: today(), Reason: "", Findings: fp,
	}

	printReviewSheet(out, name, res)
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDraft entry (reason is yours to write — one sentence per finding class, saying why it is benign):\n%s\n", b)

	if !write {
		fmt.Fprintf(out, "\nNot written. Re-run with -write to append it to %s; the reputation tests stay red until reason is filled in.\n", listPath)
		return nil
	}
	list.Entries = append(list.Entries, entry)
	if err := saveList(list); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nAppended to %s with an EMPTY reason. Fill it in, then `go test ./internal/reputation/`.\n", listPath)
	return nil
}
