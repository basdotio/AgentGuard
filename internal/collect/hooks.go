// SPDX-License-Identifier: MIT
package collect

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/redact"
)

// hookGroup mirrors one entry of a settings.json hooks event:
//
//	"hooks": { "<event>": [ { "matcher": "Bash", "hooks": [ {"type":"command","command":"…"} ] } ] }
//
// Command-type entries are (matcher, command). HTTP-type entries are (matcher, url) —
// spec §4 models both, because an HTTP hook still receives the full event payload.
type hookGroup struct {
	Matcher string `json:"matcher"`
	// Raw, so each entry can be kept as written (model.Hook.Entry); hookEntries decodes them.
	Hooks []json.RawMessage `json:"hooks"`
}

// hookEntry is the part of one hooks entry the scan reads.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	URL     string `json:"url"`
}

// hookEntries decodes every entry of every group, or reports that one did not decode. All or
// nothing on purpose: that is what decoding the event into typed groups in one call used to do, and
// an event that cannot be read whole is a PARSE-000 for the event, not a partial artifact list.
func hookEntries(groups []hookGroup) ([][]hookEntry, bool) {
	out := make([][]hookEntry, len(groups))
	for i, g := range groups {
		out[i] = make([]hookEntry, len(g.Hooks))
		for j, raw := range g.Hooks {
			if json.Unmarshal(raw, &out[i][j]) != nil {
				return nil, false
			}
		}
	}
	return out, true
}

// collectHooks turns a settings file's hooks section into ONE ARTIFACT PER ENTRY
// (spec §4). Per-event artifacts cannot say WHICH command or URL is at fault, and cannot
// pair an entry with the interception point that triggers it — both of which a hook
// finding needs, since a hook runs silently on every matching tool call.
//
// Events are visited in sorted order: map iteration order is random, and the artifact
// list must be reproducible (the scan output is meant to be byte-stable). Entries whose
// shape yields neither a command nor an HTTP URL produce a coverage note, never silence (§12).
// ownerRoot is the tree the hooks ship in, and is passed through to every artifact so the
// engine can resolve a plugin-relative script reference (see model.Hook.OwnerRoot). Empty for
// the user's own settings.json: those hooks ship in no tree.
func collectHooks(path, nameSuffix, ownerRoot string, hooks map[string]json.RawMessage, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	events := make([]string, 0, len(hooks))
	for e := range hooks {
		events = append(events, e)
	}
	sort.Strings(events)

	var out []model.ArtifactReport
	var notes []model.Finding
	for _, event := range events {
		var groups []hookGroup
		if json.Unmarshal(hooks[event], &groups) != nil {
			notes = append(notes, hookShapeNote(path, event))
			continue
		}
		entries, ok := hookEntries(groups)
		if !ok {
			notes = append(notes, hookShapeNote(path, event))
			continue
		}
		n, skipped := 0, 0
		for gi, g := range groups {
			for hi, h := range entries[gi] {
				hk, ok := hookFromEntry(event, g.Matcher, ownerRoot, h.Type, h.Command, h.URL)
				if !ok {
					skipped++ // unknown/future hook type, or http with no url
					continue
				}
				hk.Entry = string(g.Hooks[hi])
				n++
				a := artifact(model.KindHook, hookName(hk, n)+nameSuffix, path, "")
				a.Hook = hk
				out = append(out, a)
				env.Hooks++
			}
		}
		if skipped > 0 {
			notes = append(notes, hookShapeNote(path, event))
		}
	}
	return out, notes
}

// hookFromEntry builds a Hook from one settings entry, or reports that the shape is not a
// scannable surface. type=http with a URL is a first-class artifact (the URL is the target);
// any other type still needs a command. Empty type is treated as command — that is the
// historical shape, and a missing "type" field is how most settings.json files are written.
func hookFromEntry(event, matcher, ownerRoot, typ, command, rawURL string) (model.Hook, bool) {
	typ = strings.ToLower(strings.TrimSpace(typ))
	cmd := strings.TrimSpace(command)
	u := strings.TrimSpace(rawURL)
	hk := model.Hook{Event: event, Matcher: matcher, OwnerRoot: ownerRoot}
	if typ == "http" {
		if u == "" {
			return model.Hook{}, false
		}
		hk.Type = "http"
		hk.URL = u
		return hk, true
	}
	if cmd == "" {
		return model.Hook{}, false
	}
	hk.Command = cmd
	return hk, true
}

// hookName labels a hook artifact by its interception point plus its index within the
// event, e.g. "PreToolUse[Bash]#1" — two identical commands stay distinguishable.
func hookName(h model.Hook, idx int) string {
	matcher := h.Matcher
	if matcher == "" {
		matcher = "*" // no matcher = fires for every tool
	}
	return fmt.Sprintf("%s[%s]#%d", h.Event, matcher, idx)
}

// hookShapeNote reports a hooks entry whose command could not be extracted. It is a
// coverage gap, not a risk: the command was NOT scanned, and that must be visible
// (invariant "any gap produces a dimension-0 note").
func hookShapeNote(path, event string) model.Finding {
	return model.Finding{
		RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcParseError,
		Title: "Hook entry not understood, command not scanned (partial)",
		Why:   "A hooks entry does not match the (matcher, command) or (type=http, url) shape, so neither a command nor a URL could be extracted from it; it was skipped rather than silently treated as clean.",
		// The event is a key out of the settings file; config text reaches a snippet only through
		// the redactor (invariant #3).
		Evidence: []model.Evidence{{File: filepath.Base(path), Line: 0,
			Snippet: "hooks." + redact.Secrets(event), Scope: ScopeFor(path)}},
	}
}
