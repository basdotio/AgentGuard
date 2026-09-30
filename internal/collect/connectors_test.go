// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

func writeSession(t *testing.T, home, account, session, name, body string) {
	t.Helper()
	dir := filepath.Join(home, desktopCodeSessionsDir, account, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestConnectors_NewestListPerNameAndNothingElse: two sessions carry the same connector with
// different tool lists — the more recently active one wins; a second connector is its own
// artifact; parameter descriptions are part of the list; the rest of the session file (title,
// cwd, turns) never becomes a value.
func TestConnectors_NewestListPerNameAndNothingElse(t *testing.T) {
	home := t.TempDir()
	old := `{"title":"my private chat","cwd":"/Users/x/secret","lastActivityAt":100,
	  "remoteMcpServersConfig":[{"name":"Figma","uuid":"u1","tools":[{"name":"get_file","description":"old text"}]}]}`
	newer := `{"title":"another","lastActivityAt":200,
	  "remoteMcpServersConfig":[
	    {"name":"Figma","uuid":"u1","tools":[{"name":"get_file","description":"Reads a Figma file.","inputSchema":{"type":"object","properties":{"fileKey":{"type":"string","description":"The file key from the URL."}}}}]},
	    {"name":"Notion","uuid":"u2","tools":[{"name":"search","description":"Searches pages."}]}]}`
	writeSession(t, home, "acct", "s1", "local_a.json", old)
	writeSession(t, home, "acct", "s2", "local_b.json", newer)
	writeSession(t, home, "acct", "s2", "scheduled-tasks.json", `{"not":"a session"}`)

	var env model.EnvSummary
	arts, notes := collectConnectors(home, &env)
	if len(notes) != 0 {
		t.Errorf("unexpected notes: %+v", notes)
	}
	if env.Connectors != 2 || len(arts) != 2 {
		t.Fatalf("connectors = %d (%d artifacts), want 2", env.Connectors, len(arts))
	}
	figma := arts[0]
	if figma.Name != "Figma" || figma.Kind != model.KindConnector || figma.Connector == nil {
		t.Fatalf("first artifact = %+v", figma)
	}
	if len(figma.Connector.Tools) != 1 || figma.Connector.Tools[0].Description != "Reads a Figma file." {
		t.Errorf("newest list must win: %+v", figma.Connector.Tools)
	}
	if p := figma.Connector.Tools[0].Params; len(p) != 1 || p[0].Name != "fileKey" || p[0].Description != "The file key from the URL." {
		t.Errorf("parameter descriptions missing: %+v", p)
	}
	if figma.Hash == "" || figma.Hash == arts[1].Hash {
		t.Errorf("hashes must be set and differ per list: %q vs %q", figma.Hash, arts[1].Hash)
	}
	if filepath.Base(figma.Path) != "local_b.json" {
		t.Errorf("path should name the session file the list came from: %s", figma.Path)
	}
}

// TestConnectors_AbsentStoreIsNothing_BadFileIsANote: no directory → no artifacts, no notes
// (Linux, project roots); a file that is not JSON is disclosed once, and does not stop the
// others from being read.
func TestConnectors_AbsentStoreIsNothing_BadFileIsANote(t *testing.T) {
	var env model.EnvSummary
	if arts, notes := collectConnectors(t.TempDir(), &env); len(arts) != 0 || len(notes) != 0 {
		t.Errorf("absent store: %+v %+v", arts, notes)
	}
	home := t.TempDir()
	writeSession(t, home, "acct", "s1", "local_bad.json", `{"remoteMcpServersConfig": [`)
	writeSession(t, home, "acct", "s1", "local_ok.json", `{"lastActivityAt":5,"remoteMcpServersConfig":[{"name":"Slack","uuid":"u","tools":[{"name":"post","description":"Posts a message."}]}]}`)
	arts, notes := collectConnectors(home, &env)
	if len(arts) != 1 || arts[0].Name != "Slack" {
		t.Errorf("good file must still be read: %+v", arts)
	}
	if len(notes) != 1 || notes[0].RuleID != "PARSE-000" {
		t.Errorf("bad file must be disclosed as PARSE-000: %+v", notes)
	}
}
