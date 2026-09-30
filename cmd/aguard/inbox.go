// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/inbox"
	"github.com/basdotio/AgentGuard/internal/model"
)

// The Downloads scan: `aguard scan` also looks where downloads land for agent-shaped things
// that are not installed yet, and checks each with the same static pipeline `check` uses.
//
// Kept apart from the environment scan on purpose. Overall means "risk in what your agent
// loads"; a malicious zip that was downloaded and never installed is not that — it must not
// cap the environment at 49, and a folder of clean downloads must not dilute a real finding.
// So the inbox has its own section, its own per-item scores, and never touches Overall.
//
// defaultInbox is ~/Downloads. An absent default directory is nothing (CI runners have no
// Downloads); an absent EXPLICIT --inbox path is an error, like a mistyped --root.

const inboxOff = "off"

func defaultInbox() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// scanInbox discovers candidates under dir and checks each. explicit says whether the user named
// the directory; it decides how a missing directory is treated.
func scanInbox(dir string, explicit bool, o scanOpts) (*model.InboxReport, error) {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, dir[2:])
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		if explicit {
			if err == nil {
				return nil, fmt.Errorf("--inbox %s is not a directory", dir)
			}
			return nil, fmt.Errorf("--inbox %s: %w", dir, err)
		}
		return nil, nil
	}
	d := inbox.Discover(dir)
	rep := &model.InboxReport{Dir: dir, Skipped: d.Skipped, Notes: d.Notes, Items: []model.InboxItem{}}
	// The deep check covers the CANDIDATES — things already read and scored as agent-shaped —
	// never the rest of the folder, which stays uncounted and unread as before. A candidate is
	// exactly the kind of content the judge exists for (a downloaded skill nobody has vetted),
	// and the same privacy note applies: with a non-local endpoint, its redacted excerpts leave
	// the machine too, so that is said here as it is said for the environment.
	if o.llm {
		c, err := config.LoadUser(o.cfgPath)
		if err != nil {
			return nil, err
		}
		if !c.JudgeReady() {
			why := "--llm was passed but config llm.enabled is false (or endpoint unset); the Downloads items had the static check only."
			rep.Notes = append(rep.Notes, model.Finding{RuleID: "LLM-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcLLM,
				Title: "LLM judge requested but not enabled", Why: why})
			rep.Judge = &model.JudgeSummary{Reason: why}
		} else {
			rep.Judge = &model.JudgeSummary{Endpoint: c.LLM.BaseURL}
			if !isLoopbackEndpoint(c.LLM.BaseURL) {
				rep.Notes = append(rep.Notes, model.Finding{RuleID: "LLM-002", Dimension: 0, Severity: model.SevMedium, Source: model.SrcLLM,
					Title: "LLM judge endpoint is not local",
					Why:   fmt.Sprintf("base_url %q is not loopback: best-effort-redacted excerpts of the Downloads items above are sent off this machine.", c.LLM.BaseURL)})
			}
		}
	}
	for _, c := range d.Trim() {
		rep.Items = append(rep.Items, checkCandidate(c, o, rep.Judge))
	}
	if rep.Notes == nil {
		rep.Notes = []model.Finding{}
	}
	return rep, nil
}

// checkCandidate runs the static check on one candidate. Archives are extracted into a private
// temporary directory for the duration and removed afterwards; the item's hash is the archive's
// own bytes, so the same download is recognisable however it is unpacked.
func checkCandidate(c inbox.Candidate, o scanOpts, judge *model.JudgeSummary) model.InboxItem {
	it := model.InboxItem{Name: c.Name, Path: c.Path, Kind: c.Kind, Archive: c.Archive, Findings: []model.Finding{}, Notes: []model.Finding{}}
	o.autoBaseline = false // the target is untrusted; it must not bring its own baseline (as for check)
	o.ignorePath = ""
	target := c.Path
	if c.Archive {
		dir, notes, cleanup, err := inbox.ExtractZip(c.Path)
		if err != nil {
			it.Error = "archive could not be opened: " + err.Error()
			return it
		}
		defer cleanup()
		it.Notes = append(it.Notes, notes...)
		it.Hash = collect.FileHash(c.Path)
		target = dir
	}
	res, err := checkTarget(target, o)
	if err != nil {
		it.Error = err.Error()
		return it
	}
	it.Overall = res.Overall
	it.Notes = append(it.Notes, res.Notes...)
	// analyze() already ran the judge over this item when o.llm was set (the same path the
	// environment scan takes); its account is res.Judge. Nothing is run a second time here —
	// the first version of this code did, and paid for every item twice.
	if res.Judge != nil {
		it.Judged = res.Judge.Ran
		if judge != nil {
			judge.Ran = judge.Ran || res.Judge.Ran
			judge.Artifacts += res.Judge.Artifacts
			judge.Calls += res.Judge.Calls
			judge.Failed += res.Judge.Failed
			judge.Skipped += res.Judge.Skipped
			judge.Findings += res.Judge.Findings
			if res.Judge.Reason != "" && judge.Reason == "" {
				judge.Reason = res.Judge.Reason
			}
		}
	}
	// The section-level LLM-002 covers every item; repeating it per item buries the notes that
	// are about the item.
	kept := it.Notes[:0]
	for _, n := range it.Notes {
		if n.RuleID != "LLM-002" {
			kept = append(kept, n)
		}
	}
	it.Notes = kept
	for _, a := range res.Artifacts {
		if !c.Archive && it.Hash == "" {
			it.Hash = a.Hash
		}
		for _, f := range a.Findings {
			if f.Dimension == 0 {
				it.Notes = append(it.Notes, f)
				continue
			}
			it.Findings = append(it.Findings, f)
		}
	}
	sort.SliceStable(it.Findings, func(i, j int) bool { return it.Findings[i].Severity.Rank() > it.Findings[j].Severity.Rank() })
	return it
}
