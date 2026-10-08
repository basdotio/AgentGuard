// SPDX-License-Identifier: MIT
package report

import "github.com/basdotio/AgentGuard/internal/model"

// sanitizeResult returns a copy of r with every string that originated on disk — artifact
// names and paths, finding titles and evidence, notes, cleanup items, Downloads items, scan
// locations — or from the judge's model (finding reasons, triage labels) passed through Sanitize.
// The markdown renderer relies on it as much as HTML does: its code spans assume newlines are
// gone, and a triage reason carrying "\n\n" used to end its span and render the rest as markup.
// The HTML renderer builds from this copy: html/template
// escapes markup but passes bidi and zero-width characters straight through, so without it
// the HTML report could be spoofed by a file name exactly as the terminal could. JSON and
// SARIF never see this copy: they are for machines, and the bytes they carry must be the
// bytes on disk (a consumer diffing two reports must not see a phantom change).
func sanitizeResult(r model.ScanResult) model.ScanResult {
	out := r
	out.Root = Sanitize(r.Root)
	out.Artifacts = make([]model.ArtifactReport, len(r.Artifacts))
	for i, a := range r.Artifacts {
		a.Name, a.Path = Sanitize(a.Name), Sanitize(a.Path)
		a.Findings = sanitizeFindings(a.Findings)
		a.Advisory = sanitizeLabels(a.Advisory)
		out.Artifacts[i] = a
	}
	out.Notes = sanitizeFindings(r.Notes)
	out.Hygiene = make([]model.CleanItem, len(r.Hygiene))
	for i, h := range r.Hygiene {
		h.Detail = Sanitize(h.Detail)
		if h.Targets != nil {
			t := make([]string, len(h.Targets))
			for j, x := range h.Targets {
				t[j] = Sanitize(x)
			}
			h.Targets = t
		}
		out.Hygiene[i] = h
	}
	if r.Inbox != nil {
		ib := *r.Inbox
		ib.Dir = Sanitize(ib.Dir)
		ib.Notes = sanitizeFindings(ib.Notes)
		ib.Items = make([]model.InboxItem, len(r.Inbox.Items))
		for i, it := range r.Inbox.Items {
			it.Name, it.Path, it.Error = Sanitize(it.Name), Sanitize(it.Path), Sanitize(it.Error)
			it.Findings = sanitizeFindings(it.Findings)
			it.Notes = sanitizeFindings(it.Notes)
			ib.Items[i] = it
		}
		out.Inbox = &ib
	}
	if r.Locations != nil {
		out.Locations = make([]model.Location, len(r.Locations))
		for i, l := range r.Locations {
			l.Path = Sanitize(l.Path)
			out.Locations[i] = l
		}
	}
	if r.Judge != nil {
		j := *r.Judge
		j.Reason = Sanitize(j.Reason)
		out.Judge = &j
	}
	return out
}

// sanitizeLabels copies triage labels with every field sanitized: all three are model output.
func sanitizeLabels(ls []model.AdvisoryLabel) []model.AdvisoryLabel {
	if ls == nil {
		return nil
	}
	out := make([]model.AdvisoryLabel, len(ls))
	for i, l := range ls {
		out[i] = model.AdvisoryLabel{RuleID: Sanitize(l.RuleID), Label: Sanitize(l.Label), Reason: Sanitize(l.Reason)}
	}
	return out
}

func sanitizeFindings(fs []model.Finding) []model.Finding {
	if fs == nil {
		return nil
	}
	out := make([]model.Finding, len(fs))
	for i, f := range fs {
		f.Title, f.Why = Sanitize(f.Title), Sanitize(f.Why)
		if f.Evidence != nil {
			ev := make([]model.Evidence, len(f.Evidence))
			for j, e := range f.Evidence {
				e.File, e.Snippet = Sanitize(e.File), Sanitize(e.Snippet)
				ev[j] = e
			}
			f.Evidence = ev
		}
		out[i] = f
	}
	return out
}
