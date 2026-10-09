// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/judge"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/report"
)

// `aguard llm preview` — what `--llm` would send, shown before anything is sent (P-027).
//
// A user choosing a hosted endpoint accepts that redacted excerpts of their files go to that
// vendor; until now the only way to see those excerpts was a capture server. The preview runs the
// command it previews — scanEnv / scanInbox for `scan --llm`, checkTarget for `check --llm` — and at
// the point analyze() would run the judge it takes judge.Plan instead: the same plan, the same
// payload renderers the client uses. runJudge is never reached, so no client is built, no key is
// read and nothing connects, whatever the config says; the zero-dial test holds it to that.

// previewSink receives, from analyze(), what the judge would have been given.
type previewSink struct {
	item    string // the Downloads item the next plans belong to; "" for the target or environment
	batches *[]previewBatch
}

// previewBatch is one analyze() call's plan: the artifacts the judge would get and its calls.
type previewBatch struct {
	item  string
	arts  []previewArtifact
	calls []judge.PlannedCall
}

func newPreviewSink() *previewSink { return &previewSink{batches: &[]previewBatch{}} }

// forItem is the same sink, labelling what it records with a Downloads item's name. Nil stays nil,
// so a run that is not a preview passes nothing on.
func (p *previewSink) forItem(name string) *previewSink {
	if p == nil {
		return nil
	}
	return &previewSink{item: name, batches: p.batches}
}

// record plans the judge's run over arts with the options runJudge would use.
func (p *previewSink) record(cfg config.Config, arts []model.ArtifactReport, home string) {
	b := previewBatch{item: p.item, calls: judge.Plan(arts, judgePlanOptions(cfg, home))}
	for _, a := range arts {
		b.arts = append(b.arts, previewArtifact{Kind: string(a.Kind), Name: a.Name, Hash: a.Hash, Requests: []previewRequest{}})
	}
	*p.batches = append(*p.batches, b)
}

// judgePlanOptions are the judge options that decide WHAT is sent — how often each question is
// asked, the call budget, the home replaced by `~`. runJudge adds the ones that decide how it is
// sent; the preview uses these alone, so the two cannot plan different runs.
func judgePlanOptions(cfg config.Config, home string) judge.Options {
	return judge.Options{MaxCalls: cfg.LLM.MaxCalls, Samples: cfg.LLM.Samples, Home: home}
}

// previewOpts are the inputs of one preview: a target (`check --llm`), or none (`scan --llm` over
// root, with the Downloads candidates under inbox unless it is off).
type previewOpts struct {
	cfgPath, ignorePath string
	noReputation        bool
	root, target        string
	inbox               string
	inboxExplicit       bool
	json                bool
}

// previewReport is `llm preview --json`. It carries no time and nothing random, so the same input
// on the same machine gives the same bytes.
type previewReport struct {
	Previews     string            `json:"previews"` // the command whose calls these are
	Target       string            `json:"target"`
	Endpoint     string            `json:"endpoint"`
	Model        string            `json:"model"`
	JudgeEnabled bool              `json:"judge_enabled"`
	MaxCalls     int               `json:"max_calls"`
	Calls        int               `json:"calls"`          // calls that would be sent
	NotSent      int               `json:"calls_not_sent"` // calls the budget refuses
	Instructions map[string]string `json:"instructions"`   // pass → its system message's task text
	Artifacts    []previewArtifact `json:"artifacts"`
	Downloads    []previewItem     `json:"downloads,omitempty"`
}

type previewItem struct {
	Item      string            `json:"item"`
	Artifacts []previewArtifact `json:"artifacts"`
}

type previewArtifact struct {
	Kind     string           `json:"kind"`
	Name     string           `json:"name"`
	Hash     string           `json:"hash"`
	Requests []previewRequest `json:"requests"`
}

type previewRequest struct {
	Pass        string          `json:"pass"`
	Rule        string          `json:"rule,omitempty"`
	Temperature float64         `json:"temperature"`
	Calls       int             `json:"calls"`
	NotSent     int             `json:"not_sent"`
	Payload     string          `json:"payload"`
	Sources     []previewSource `json:"sources,omitempty"`
	Shortened   string          `json:"shortened,omitempty"`
}

type previewSource struct {
	File  string `json:"file"`
	Lines string `json:"lines,omitempty"`
}

// runLLMPreview runs the previewed command's analysis with a preview sink and prints the plan.
func runLLMPreview(w io.Writer, o previewOpts) error {
	cfg, err := config.LoadUser(o.cfgPath)
	if err != nil {
		return err
	}
	sink := newPreviewSink()
	rep := previewReport{Endpoint: cfg.LLM.BaseURL, Model: judge.RequestModel(cfg.LLM.Model),
		JudgeEnabled: cfg.JudgeReady(), MaxCalls: cfg.LLM.MaxCalls, Instructions: map[string]string{}}
	// The options each command passes, minus llm: the preview never runs the judge, it plans it.
	if o.target != "" {
		rep.Previews, rep.Target = "check --llm", o.target
		if _, err := checkTarget(o.target, scanOpts{cfgPath: o.cfgPath, ignorePath: o.ignorePath, noReputation: o.noReputation, preview: sink}); err != nil {
			return err
		}
	} else {
		rep.Previews, rep.Target = "scan --llm", o.root
		if _, err := scanEnv(o.root, scanOpts{cfgPath: o.cfgPath, ignorePath: o.ignorePath, noReputation: o.noReputation, preview: sink}); err != nil {
			return err
		}
		if o.inbox != inboxOff && o.inbox != "" {
			if _, err := scanInbox(o.inbox, o.inboxExplicit, scanOpts{cfgPath: o.cfgPath, noReputation: o.noReputation, preview: sink}); err != nil {
				return err
			}
		}
	}
	for _, b := range *sink.batches {
		arts := rep.add(b)
		if b.item == "" {
			rep.Artifacts = append(rep.Artifacts, arts...)
		} else {
			rep.Downloads = append(rep.Downloads, previewItem{Item: b.item, Artifacts: arts})
		}
	}
	if rep.Artifacts == nil {
		rep.Artifacts = []previewArtifact{}
	}
	if o.json {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false) // the payload's bytes, as the client encodes them
		return enc.Encode(rep)
	}
	writePreviewText(w, rep)
	return nil
}

// add files one batch's calls under their artifacts and counts them.
func (r *previewReport) add(b previewBatch) []previewArtifact {
	arts := append([]previewArtifact(nil), b.arts...)
	for _, c := range b.calls {
		r.Calls += c.Calls - c.NotSent
		r.NotSent += c.NotSent
		r.Instructions[c.Pass] = c.Instruction
		req := previewRequest{Pass: c.Pass, Rule: c.Rule, Temperature: c.Temperature, Calls: c.Calls,
			NotSent: c.NotSent, Payload: c.Payload, Shortened: c.Shortened}
		for _, s := range c.Sources {
			req.Sources = append(req.Sources, previewSource{File: s.File, Lines: s.Lines})
		}
		arts[c.Artifact].Requests = append(arts[c.Artifact].Requests, req)
	}
	return arts
}

// previewGutter starts every payload line of the terminal view. A payload is the scanned
// artifact's text, so it could print a line that looks like the end of its block; with every one
// of its lines behind the gutter, it cannot.
const previewGutter = "    │ "

// writePreviewText is the terminal view: for reading, not for diffing. Everything that came from
// the scanned files goes through report.Sanitize (invariant #7), tabs become four spaces; --json
// carries the exact bytes.
func writePreviewText(w io.Writer, r previewReport) {
	clean := func(s string) string { return report.Sanitize(strings.ReplaceAll(s, "\t", "    ")) }
	enabled := "enabled"
	if !r.JudgeEnabled {
		enabled = "not enabled (a real run needs llm.enabled and --llm)"
	}
	fmt.Fprintf(w, "What `%s` would send for %s. Nothing was sent.\n", r.Previews, clean(r.Target))
	fmt.Fprintf(w, "Endpoint %s, model %s; the judge is %s in this config.\n", clean(r.Endpoint), clean(r.Model), enabled)
	fmt.Fprintf(w, "%d call(s) would be made", r.Calls)
	if r.NotSent > 0 {
		fmt.Fprintf(w, "; %d more are refused by llm.max_calls=%d", r.NotSent, r.MaxCalls)
	}
	fmt.Fprintln(w, ". Each block below is the text inside one call's nonce fence; --json has its exact bytes and the instruction each pass is given.")
	silent := writePreviewArtifacts(w, r.Artifacts, clean)
	for _, it := range r.Downloads {
		fmt.Fprintf(w, "\nDownloads item %s\n", clean(it.Item))
		silent += writePreviewArtifacts(w, it.Artifacts, clean)
	}
	if silent > 0 {
		fmt.Fprintf(w, "\n%d other artifact(s) send nothing: no pass applies to them, or there was nothing to excerpt.\n", silent)
	}
}

// writePreviewArtifacts prints the artifacts that have calls and returns how many have none.
func writePreviewArtifacts(w io.Writer, arts []previewArtifact, clean func(string) string) int {
	silent := 0
	for _, a := range arts {
		if len(a.Requests) == 0 {
			silent++
			continue
		}
		hash := a.Hash
		if hash == "" {
			hash = "no content hash"
		}
		fmt.Fprintf(w, "\n%s %s  %s\n", a.Kind, clean(a.Name), hash)
		for _, q := range a.Requests {
			fmt.Fprintf(w, "  %s\n", previewCallLine(q, clean))
			for _, l := range strings.Split(q.Payload, "\n") {
				fmt.Fprintln(w, previewGutter+clean(l))
			}
		}
	}
	return silent
}

// previewCallLine is the line above a payload: the pass, how often it is sent, where it is from.
func previewCallLine(q previewRequest, clean func(string) string) string {
	line := q.Pass
	if q.Rule != "" {
		line += " (" + q.Rule + ")"
	}
	line += fmt.Sprintf(" · %d call(s)", q.Calls-q.NotSent)
	if q.NotSent > 0 {
		line += fmt.Sprintf(", %d refused by the call budget", q.NotSent)
	}
	var from []string
	for _, s := range q.Sources {
		f := clean(s.File)
		if s.Lines != "" {
			f += ":" + s.Lines
		}
		from = append(from, f)
	}
	if len(from) > 0 {
		line += " · from " + strings.Join(from, ", ")
	}
	if q.Shortened != "" {
		line += " · shortened: " + clean(q.Shortened)
	}
	return line
}

// newLLMPreviewCommand is `aguard llm preview [path]`. root, the config, the baseline and the
// reputation switch are the persistent flags `scan` and `check` read, so the preview reads them too.
func newLLMPreviewCommand(root, cfgPath, ignorePath *string, noRep *bool) *cobra.Command {
	var asJSON bool
	var inboxDir string
	cmd := &cobra.Command{
		Use:   "preview [path]",
		Short: "Print exactly what --llm would send — for check <path>, or with no path for scan — without sending anything",
		Long: "Print exactly what --llm would send, without sending anything and without needing a key.\n\n" +
			"With a path: the calls `aguard check <path> --llm` would make. Without one: the calls `aguard scan --llm`\n" +
			"would make for --root and, as scan does, for the Downloads items under --inbox. Per artifact: its kind,\n" +
			"name and content hash; per call: the pass, how often it is sent, the text that goes inside the call's\n" +
			"nonce fence, the file lines it came from, and what was shortened. The nonce itself is drawn per call\n" +
			"when a real run sends it, so it is not shown. Uses the config's samples, max_calls, endpoint and model.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := previewOpts{cfgPath: *cfgPath, ignorePath: *ignorePath, noReputation: *noRep, root: *root,
				inbox: inboxDir, inboxExplicit: cmd.Flags().Changed("inbox"), json: asJSON}
			if len(args) == 1 {
				o.target = args[0]
			}
			return runLLMPreview(os.Stdout, o)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON: each payload's exact bytes, and the instruction each pass is given")
	cmd.Flags().StringVar(&inboxDir, "inbox", defaultInbox(), "without a path: also preview the Downloads items under this directory, as scan --llm judges them; '"+inboxOff+"' disables")
	return cmd
}
