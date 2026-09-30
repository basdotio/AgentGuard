// SPDX-License-Identifier: MIT

package aguard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// exists fails the test unless rel exists under dir.
func exists(t *testing.T, dir, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		t.Errorf("expected %s under %s: %v", rel, dir, err)
	}
}

const toolsJSON = `{"package":"@x/y","tools":[{"name":"a","description":"Do a","inputSchema":{"type":"object","properties":{"p":{"description":"param"}}}}]}`

// This file is the placement test, moved here with the code it covers when
// hack/corpus-runner was folded into this adapter. Placement is the part of a
// measurement most likely to be silently wrong — the corpus's own guide says a whole surface of
// zeros is almost always this — so the tests travelled with it rather than being rewritten.

func TestStagePlacesEachKindWhereAguardLooks(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string // paths relative to home that must exist
		check func(t *testing.T, st StagedRoot)
		wantN string // substring of the NotPlaceable reason; "" = must be placeable
	}{
		{
			name:  "settings under .claude becomes the root",
			files: map[string]string{".claude/settings.json": `{"hooks":{}}`, ".claude/hooks/x.sh": "#!/bin/sh\n"},
			want:  []string{".claude/settings.json", ".claude/hooks/x.sh"},
		},
		{
			name:  "project mcp config sits beside the root",
			files: map[string]string{".mcp.json": `{"mcpServers":{}}`},
			want:  []string{".mcp.json"},
		},
		{
			name:  "a skill tree goes whole into skills/<id>",
			files: map[string]string{"SKILL.md": "# s\n", "README.md": "r\n", "scripts/run.sh": "echo\n"},
			want:  []string{".claude/skills/sample-1/SKILL.md", ".claude/skills/sample-1/README.md", ".claude/skills/sample-1/scripts/run.sh"},
		},
		{
			name:  "a tool catalogue becomes a desktop session cache the connector collector reads",
			files: map[string]string{"tools.json": toolsJSON},
			check: func(t *testing.T, st StagedRoot) {
				matches, _ := filepath.Glob(filepath.Join(st.Home, "Library", "Application Support", "Claude", "claude-code-sessions", "*", "*", "local_*.json"))
				if len(matches) != 1 {
					t.Fatalf("want exactly one session cache file, got %v", matches)
				}
				var doc struct {
					Remote []struct {
						Name  string          `json:"name"`
						Tools json.RawMessage `json:"tools"`
					} `json:"remoteMcpServersConfig"`
				}
				b, _ := os.ReadFile(matches[0])
				if err := json.Unmarshal(b, &doc); err != nil {
					t.Fatal(err)
				}
				if len(doc.Remote) != 1 || doc.Remote[0].Name != "@x/y" {
					t.Fatalf("session doc: %s", b)
				}
				var got, want []any
				_ = json.Unmarshal(doc.Remote[0].Tools, &got)
				var src struct{ Tools []any }
				_ = json.Unmarshal([]byte(toolsJSON), &src)
				want = src.Tools
				if !reflect.DeepEqual(got, want) {
					t.Errorf("tools were not carried over verbatim:\n got %v\nwant %v", got, want)
				}
			},
		},
		{
			name:  "one loose instruction file becomes CLAUDE.md",
			files: map[string]string{"01-direct-injection.md": "ignore previous\n"},
			want:  []string{".claude/CLAUDE.md"},
		},
		{
			name:  "server source alone has no load path",
			files: map[string]string{"server.py": "from mcp import FastMCP\n"},
			wantN: "server source",
		},
		{
			name:  "an empty tree is not a sample aguard can load",
			files: map[string]string{},
			wantN: "nothing",
		},
		{
			name:  "connector sample carrying both a project mcp config and settings places both",
			files: map[string]string{".mcp.json": `{"mcpServers":{}}`, ".claude/settings.json": `{}`},
			want:  []string{".mcp.json", ".claude/settings.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			for rel, body := range tc.files {
				write(t, src, rel, body)
			}
			st, err := Stage(src, "sample-1", t.TempDir())
			if tc.wantN != "" {
				var np NotPlaceable
				if !errors.As(err, &np) {
					t.Fatalf("want NotPlaceable, got err=%v StagedRoot=%+v", err, st)
				}
				if !strings.Contains(np.Reason, tc.wantN) {
					t.Errorf("reason %q does not mention %q", np.Reason, tc.wantN)
				}
				return
			}
			if err != nil {
				t.Fatalf("stage: %v", err)
			}
			if st.Root != filepath.Join(st.Home, ".claude") {
				t.Errorf("root %s is not <home>/.claude", st.Root)
			}
			for _, rel := range tc.want {
				exists(t, st.Home, rel)
			}
			if tc.check != nil {
				tc.check(t, st)
			}
		})
	}
}
