// SPDX-License-Identifier: MIT
package report

import (
	_ "embed"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

//go:embed report.html.tmpl
var htmlTmpl string

type htmlHygiene struct {
	ID            string
	Tier          string
	Kind          string
	Detail        string
	Targets       []string
	ReclaimTokens int
	Label         string // reader-facing name for Kind
}

// TargetsStr renders the target skill list for the template.
func (h htmlHygiene) TargetsStr() string { return strings.Join(h.Targets, ", ") }

type htmlData struct {
	Root        string
	ScannedAt   string
	ToolVersion string
	Overall     int
	Level       string
	GaugeDeg    int
	GaugeColor  template.CSS
	// The second gauge appears only when the judge actually moved the number; a duplicate
	// dial showing the same value would just look like a rendering bug (spec §9).
	ShowEffective  bool
	Effective      int
	EffectiveLevel string
	EffectiveDeg   int
	EffectiveColor template.CSS
	Env            model.EnvSummary
	// OWASPCovered / OWASPTotal / OWASPSilent carry the catalogue's COVERAGE, gaps included. The
	// terminal report prints this and the HTML did not, which is the kind of divergence that lets one
	// renderer quietly become the honest one. A reader looking at six category names cannot tell
	// whether the other four were clean or unexamined.
	OWASPCovered int
	OWASPTotal   int
	OWASPSilent  string
	// SandboxBanner is non-empty only when the scan ran in a managed cloud container, and holds
	// the same words the terminal prints. It renders at the very top of the summary so a reader
	// who opens the saved file — not the chat — cannot miss that the score is about a sandbox.
	SandboxBanner string
	SandboxWhy    string
	// Two lists, not one: the deterministic findings set the score and the gate, the judge's
	// never do, and a reader cannot tell them apart from the rule id alone.
	Groups      []htmlGroup // deterministic
	JudgeGroups []htmlGroup // LLM judge (advisory)
	// JudgeRequested renders the judge section even when it has no groups, with JudgeLine as its
	// body — so "ran, nothing to add" and "did not run, because…" each have a place on the page.
	// Without --llm both are false/empty and the section is absent: a report must not advertise a
	// feature the user did not turn on.
	JudgeRequested bool
	JudgeLine      string
	JudgeIdentity  string // the model, samples and judge versions, "" unless the judge ran (P-031)
	WorstLine      string // the lowest-scoring artifact beside the mean, "" when it would restate the headline
	// Inbox is the Downloads section: nil pointer when no inbox was scanned.
	Inbox *htmlInbox
	// Locations: where the scan looked, with each place's status (read / absent / off).
	Locations []model.Location
	Hygiene   []htmlHygiene
	// Notes split the same way the terminal report splits them: Trust decisions changed the
	// score and are shown in full; Coverage gaps are folded behind a disclosure.
	Trust    []htmlTrust
	Coverage []htmlNote
	Reclaim  int
	// The plain-language layer (plain.go), computed once and shared with the terminal report.
	Verdict string
	Checked string
	Actions []action
}

// htmlGroup is a Group plus the strings the template cannot compute: the readable artifact
// name, the dimension's plain label, shortened evidence paths (full path kept for hover) and
// an anchor so the "what to look at" list can jump to the card.
type htmlGroup struct {
	Group
	Friendly string
	Plain    string
	Action   string // what to do — fixed per dimension, see actionHint
	Anchor   string
	Ev       []htmlEv
}

// htmlInbox / htmlInboxItem render the Downloads scan. Each item carries its own score and a
// one-line advice derived from it; the environment score is untouched and the heading says so.
type htmlInbox struct {
	Dir       string
	Items     []htmlInboxItem
	Skipped   int
	Notes     []model.Finding
	JudgeLine string // "" unless --llm was given
}

type htmlInboxItem struct {
	model.InboxItem
	Level  string
	Color  template.CSS
	Advice string
	Worst  string
	Label  string // kind for display: "zip" for archives
}

func toHTMLInbox(ib *model.InboxReport) *htmlInbox {
	if ib == nil {
		return nil
	}
	out := &htmlInbox{Dir: ib.Dir, Skipped: ib.Skipped, Notes: ib.Notes, JudgeLine: judgeLine(ib.Judge)}
	for _, it := range ib.Items {
		h := htmlInboxItem{InboxItem: it, Label: it.Kind}
		if it.Archive {
			h.Label = "zip"
		}
		if it.Error == "" {
			h.Level = score.Level(it.Overall)
			h.Color = gaugeColor(it.Overall)
			h.Advice = inboxAdvice(h.Level, len(it.Findings), it.Judged)
			h.Worst = inboxWorst(it)
		}
		out.Items = append(out.Items, h)
	}
	return out
}

// htmlTrust is a REP-GOOD / IGN-000 note split for reading: the decision (who was trusted, how
// many findings it hid) stays visible; the reviewer's full reasoning folds underneath. The text
// applyReputation writes is one paragraph with the review after " Reviewed <date>: "; anything
// not in that shape is shown whole.
type htmlTrust struct {
	model.Finding
	Head   string
	Reason string
}

func toHTMLTrust(ns []model.Finding) []htmlTrust {
	out := make([]htmlTrust, 0, len(ns))
	for _, n := range ns {
		t := htmlTrust{Finding: n, Head: n.Why}
		if i := strings.Index(n.Why, " Reviewed "); i > 0 {
			t.Head, t.Reason = n.Why[:i], strings.TrimSpace(n.Why[i:])
		}
		out = append(out, t)
	}
	return out
}

// hygieneLabel names a cleanup item's kind for a reader who does not know the kind codes.
func hygieneLabel(kind string) string {
	switch kind {
	case "context_bloat":
		return "Long description"
	case "duplicate_fn":
		return "Two skills look alike"
	case "stale_ref":
		return "Broken link"
	case "zombie":
		return "Never used"
	case "baseline":
		return "Accepted earlier"
	}
	return kind
}

// htmlNote is a coverage note split for reading: the NAMES of what was skipped as chips (a
// non-specialist can scan twenty file names; nobody scans them inside a paragraph), and the
// rationale folded underneath. Notes whose Why does not open with "Not read: a, b, c." keep the
// whole text as rationale.
//
// File is the one file a single-instance note is about, under the same rule the terminal's
// --verbose block and the markdown report already apply (exactly one evidence entry). Without it a
// parse failure rendered here as "Parse failed, artifact not fully covered" and named no file —
// the file IS the finding for that note. A coalesced note lists its instances elsewhere and leaves
// File empty, as the other two renderers do.
type htmlNote struct {
	model.Finding
	File  string
	Items []string
	Rest  string
}

func toHTMLNotes(ns []model.Finding) []htmlNote {
	out := make([]htmlNote, 0, len(ns))
	for _, n := range ns {
		h := htmlNote{Finding: n, Rest: n.Why}
		if len(n.Evidence) == 1 {
			h.File = n.Evidence[0].File
		}
		if rest, ok := strings.CutPrefix(n.Why, "Not read: "); ok {
			list, tail, _ := strings.Cut(rest, ". ")
			for _, it := range strings.Split(list, ", ") {
				if it = strings.TrimSpace(it); it != "" {
					h.Items = append(h.Items, it)
				}
			}
			h.Rest = tail
		}
		out = append(out, h)
	}
	return out
}

type htmlEv struct {
	Short, Full string
	Line        int
	Snippet     string
}

func toHTMLGroups(gs []Group, prefix string) []htmlGroup {
	out := make([]htmlGroup, 0, len(gs))
	for i, g := range gs {
		h := htmlGroup{Group: g, Friendly: friendlyArtifact(g.Artifact), Plain: dimLabel(g.Dimension), Action: actionHint(g.Dimension),
			Anchor: prefix + "-" + itoa(i)}
		for _, e := range g.Evidence {
			h.Ev = append(h.Ev, htmlEv{Short: shortPath(e.File), Full: e.File, Line: e.Line, Snippet: e.Snippet})
		}
		out = append(out, h)
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }

// HTML renders a self-contained report to w (spec §9). Uses html/template so all
// artifact-derived text (already redacted at detect) is additionally HTML-escaped.
func HTML(w io.Writer, r model.ScanResult) error {
	t, err := template.New("report").Parse(htmlTmpl)
	if err != nil {
		return err
	}
	return t.Execute(w, buildHTMLData(sanitizeResult(r)))
}

func buildHTMLData(r model.ScanResult) htmlData {
	var groups, judge []Group // shared aggregation with the terminal report — one source of truth
	for _, g := range Aggregate(r) {
		if g.FromJudge() {
			judge = append(judge, g)
		} else {
			groups = append(groups, g)
		}
	}

	reclaim := 0
	hy := make([]htmlHygiene, 0, len(r.Hygiene))
	for _, h := range r.Hygiene {
		reclaim += h.ReclaimTokens
		tier := string(h.Tier)
		if tier == "" {
			tier = "info"
		}
		hy = append(hy, htmlHygiene{ID: h.ID, Tier: tier, Kind: h.Kind, Label: hygieneLabel(h.Kind), Detail: h.Detail,
			Targets: h.Targets, ReclaimTokens: h.ReclaimTokens})
	}

	scannedAt := "—"
	if r.ScannedAt > 0 {
		scannedAt = time.Unix(r.ScannedAt, 0).UTC().Format("2006-01-02 15:04 UTC")
	}
	trust, coverage := splitNotes(notesOf(r))
	var sbBanner, sbWhy string
	if r.Sandbox != nil {
		sbBanner = "This scan ran in a temporary cloud environment (Claude Cloud / Cowork), not on your own computer. The score below is about this throwaway sandbox — your real skills, plugins, hooks and connectors live on your machine and were NOT scanned from here. To check your computer, open the Code tab (</>) in the desktop app and run the scan there."
		sbWhy = strings.Join(r.Sandbox.Signals, "; ")
	}
	return htmlData{
		Root: r.Root, ScannedAt: scannedAt, ToolVersion: r.ToolVersion,
		OWASPCovered: owaspCovered(), OWASPTotal: len(detect.ASICatalogue()), OWASPSilent: owaspSilent(),
		Overall: r.Overall, Level: score.Level(r.Overall),
		GaugeDeg: r.Overall * 360 / 100, GaugeColor: gaugeColor(r.Overall),
		ShowEffective: r.OverallEffective != r.Overall,
		Effective:     r.OverallEffective, EffectiveLevel: score.Level(r.OverallEffective),
		EffectiveDeg: r.OverallEffective * 360 / 100, EffectiveColor: gaugeColor(r.OverallEffective),
		Env: r.Env, Groups: toHTMLGroups(groups, "f"), JudgeGroups: toHTMLGroups(judge, "j"), Hygiene: hy,
		Trust: toHTMLTrust(trust), Coverage: toHTMLNotes(coverage), Reclaim: reclaim,
		Verdict: coverageVerdict(score.Level(r.Overall), actionable(groups), len(groups), r),
		Checked: checkedSummary(r, plainGap), JudgeLine: judgeSummaryLine(r), JudgeIdentity: judgeIdentityLine(r.Judge), JudgeRequested: r.Judge != nil, WorstLine: worstLine(r),
		Inbox: toHTMLInbox(r.Inbox), Locations: r.Locations,
		Actions:       actions(groups, 3),
		SandboxBanner: sbBanner, SandboxWhy: sbWhy,
	}
}

func gaugeColor(score int) template.CSS {
	switch {
	case score >= 85:
		return "#3ecf8e"
	case score >= 70:
		return "#ffd76a"
	case score >= 50:
		return "#ffb454"
	default:
		return "#ff5f6d"
	}
}

// owaspCovered / owaspSilent split the catalogue into what this scanner has rules for and what it is
// silent about. Kept beside the renderer rather than in the template so the two outputs compute it the
// same way; the template only formats.
func owaspCovered() int {
	n := 0
	for _, c := range detect.ASICatalogue() {
		if c.Covered {
			n++
		}
	}
	return n
}

func owaspSilent() string {
	var ids []string
	for _, c := range detect.ASICatalogue() {
		if !c.Covered {
			ids = append(ids, c.ID)
		}
	}
	return strings.Join(ids, ", ")
}
