// SPDX-License-Identifier: MIT
package collect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/safeio"
)

// Remote MCP connectors (the app's Connectors tab: Figma, Notion, Slack, …) are servers this
// tool never contacts. What it CAN read is what Claude Desktop already fetched from them: the
// tool list each connector advertised, cached per session in
// ~/Library/Application Support/Claude/claude-code-sessions/<account>/<session>/local_*.json
// under "remoteMcpServersConfig". That list — every tool's description and parameter
// descriptions — is text the model reads and acts on in every session the connector is attached
// to, here and in the cloud sandbox alike, and the server can change it at will. It is the
// tool-poisoning surface (Invariant Labs, 2025), and this cache is the one place it can be read
// without opening a connection.
//
// Two boundaries, both deliberate:
//
//   - The session file also holds the user's own session state (title, cwd, turns). Only the
//     connector list is decoded — json.Unmarshal into a struct that names nothing else — and
//     nothing else in the file is read into memory as structure. Same reason sessions/ under
//     the config root is never read.
//   - Coverage is what this machine has SEEN: a connector used only on claude.ai in the browser
//     and never in a desktop session is not in the cache and is not reported. The report says
//     "N connectors seen in desktop sessions", never "all your connectors".
//
// The layout is observed on macOS Claude Desktop, not documented. A change shows up
// as "nothing collected" plus the Locations row saying the directory is absent — never as an
// error the operator has to understand.

const desktopCodeSessionsDir = "Library/Application Support/Claude/claude-code-sessions"

// Caps: a session directory can hold hundreds of files over time; each is small (~100 KB) but
// attacker-influenced only through connector text, so the caps guard cost, not safety.
const (
	maxSessionFiles     = 2000
	maxSessionFileBytes = 8 << 20
)

// DesktopSessions returns the desktop session cache directory for home and whether it exists.
func DesktopSessions(home string) (string, bool) {
	base := filepath.Join(home, desktopCodeSessionsDir)
	fi, err := os.Stat(base)
	return base, err == nil && fi.IsDir()
}

// sessionDoc is the ONLY shape decoded from a session file. Unknown fields are dropped by the
// decoder; nothing else in the file becomes a value this program holds.
type sessionDoc struct {
	LastActivityAt int64 `json:"lastActivityAt"`
	CreatedAt      int64 `json:"createdAt"`
	Remote         []struct {
		Name  string `json:"name"`
		UUID  string `json:"uuid"`
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	} `json:"remoteMcpServersConfig"`
}

// collectConnectors reads every desktop session file under home and returns one artifact per
// connector NAME, carrying the tool list from the most recently active session that had it.
// Newest wins because that is what the next session will be handed; older lists are history.
func collectConnectors(home string, env *model.EnvSummary) ([]model.ArtifactReport, []model.Finding) {
	base, ok := DesktopSessions(home)
	if !ok {
		return nil, nil
	}
	files, err := filepath.Glob(filepath.Join(base, "*", "*", "local_*.json"))
	if err != nil || len(files) == 0 {
		return nil, nil
	}
	sort.Strings(files)
	var notes []model.Finding
	var unreadable, unparsable []string
	type seen struct {
		art  model.ArtifactReport
		when int64
	}
	newest := map[string]seen{}
	for i, f := range files {
		if i >= maxSessionFiles {
			notes = append(notes, model.Finding{
				RuleID: "COV-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
				Title: "Claude Desktop session cache: file cap reached",
				Why: fmt.Sprintf("%d session files under %s; only the first %d (by name) were read for connector tool lists.",
					len(files), base, maxSessionFiles),
				Evidence: []model.Evidence{{File: base}},
			})
			break
		}
		doc, err := readSessionDoc(f)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // sessions are pruned while we look; not a gap
			}
			var se *json.SyntaxError
			var te *json.UnmarshalTypeError
			if errors.As(err, &se) || errors.As(err, &te) {
				unparsable = append(unparsable, filepath.Base(f))
			} else {
				unreadable = append(unreadable, filepath.Base(f))
			}
			continue
		}
		when := doc.LastActivityAt
		if when == 0 {
			when = doc.CreatedAt
		}
		for _, r := range doc.Remote {
			if r.Name == "" || len(r.Tools) == 0 {
				continue
			}
			if prev, ok := newest[r.Name]; ok && prev.when >= when {
				continue
			}
			c := &model.Connector{UUID: r.UUID}
			for _, t := range r.Tools {
				ct := model.ConnectorTool{Name: t.Name, Description: t.Description}
				params := make([]string, 0, len(t.InputSchema.Properties))
				for p := range t.InputSchema.Properties {
					params = append(params, p)
				}
				sort.Strings(params)
				for _, p := range params {
					if d := t.InputSchema.Properties[p].Description; d != "" {
						ct.Params = append(ct.Params, model.ConnectorParam{Name: p, Description: d})
					}
				}
				c.Tools = append(c.Tools, ct)
			}
			sort.Slice(c.Tools, func(i, j int) bool { return c.Tools[i].Name < c.Tools[j].Name })
			newest[r.Name] = seen{when: when, art: model.ArtifactReport{
				Kind: model.KindConnector, Name: r.Name, Path: f, Hash: connectorHash(c), Connector: c,
			}}
		}
	}
	names := make([]string, 0, len(newest))
	for n := range newest {
		names = append(names, n)
	}
	sort.Strings(names)
	arts := make([]model.ArtifactReport, 0, len(names))
	for _, n := range names {
		arts = append(arts, newest[n].art)
	}
	env.Connectors += len(arts)
	if len(unreadable) > 0 {
		notes = append(notes, model.Finding{
			RuleID: "IO-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
			Title:    "Claude Desktop session files not readable",
			Why:      fmt.Sprintf("%d session file(s) under %s could not be read; any connector tool list only in those was not checked: %s", len(unreadable), base, strings.Join(clipList(unreadable, 5), ", ")),
			Evidence: []model.Evidence{{File: base}},
		})
	}
	if len(unparsable) > 0 {
		notes = append(notes, model.Finding{
			RuleID: "PARSE-000", Dimension: 0, Severity: model.SevLow, Source: model.SrcStatic,
			Title:    "Claude Desktop session files not parsable",
			Why:      fmt.Sprintf("%d session file(s) under %s were not valid JSON in the expected shape; any connector tool list only in those was not checked: %s", len(unparsable), base, strings.Join(clipList(unparsable, 5), ", ")),
			Evidence: []model.Evidence{{File: base}},
		})
	}
	return arts, notes
}

// readSessionDoc decodes one session file into the connector-only shape, bounded.
func readSessionDoc(path string) (sessionDoc, error) {
	var doc sessionDoc
	b, err := safeio.ReadFile(path, maxSessionFileBytes)
	if err != nil {
		return doc, err
	}
	err = json.Unmarshal(b, &doc)
	return doc, err
}

// connectorHash is the canonical identity of a tool list: tool names, descriptions and
// parameter descriptions in sorted order. It changes when the server changes what it tells the
// model, which is exactly the event a reputation entry or an approval must not survive.
func connectorHash(c *model.Connector) string {
	h := sha256.New()
	for _, t := range c.Tools {
		h.Write([]byte(t.Name))
		h.Write([]byte{0})
		h.Write([]byte(t.Description))
		h.Write([]byte{0})
		for _, p := range t.Params {
			h.Write([]byte(p.Name))
			h.Write([]byte{1})
			h.Write([]byte(p.Description))
			h.Write([]byte{1})
		}
		h.Write([]byte{2})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func clipList(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(xs[:n:n], fmt.Sprintf("… %d more", len(xs)-n))
}
