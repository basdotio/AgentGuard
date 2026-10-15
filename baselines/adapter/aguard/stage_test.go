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

// A tree that is MCP server source by aguard's own recogniser is still unplaceable — `scan`
// has no load path for it — but the refusal says so, because that is the one `check` route a
// run may hand the judge flags to (P-056). A stray .py that is not a server stays the old,
// unflagged refusal.
func TestStageSaysWhenTheUnplaceableTreeIsServerSource(t *testing.T) {
	t.Parallel()
	server := "from mcp.server.fastmcp import FastMCP\nmcp = FastMCP(\"t\")\n\n@mcp.tool()\ndef add(a: int, b: int) -> int:\n    \"\"\"Add two numbers.\"\"\"\n    return a + b\n"
	for _, tc := range []struct {
		name   string
		body   string
		server bool
	}{
		{"a FastMCP server", server, true},
		{"a python file that is not a server", "print('hi')\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "server.py"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Stage(src, "x", t.TempDir())
			var np NotPlaceable
			if !errors.As(err, &np) {
				t.Fatalf("want NotPlaceable, got %v", err)
			}
			if np.Server != tc.server {
				t.Fatalf("Server = %v, want %v (reason %q)", np.Server, tc.server, np.Reason)
			}
		})
	}
}

// A README beside code is a project's README, not an instruction file an agent loads. The
// first run over 89 benign server trees staged 36 of them as CLAUDE.md on the strength of
// their README.md, and the judge read prose instead of servers.
func TestAReadmeBesideSourceIsNotAnInstructionFile(t *testing.T) {
	t.Parallel()
	server := "from mcp.server.fastmcp import FastMCP\nmcp = FastMCP(\"t\")\n\n@mcp.tool()\ndef add(a: int, b: int) -> int:\n    \"\"\"Add.\"\"\"\n    return a + b\n"
	for _, tc := range []struct {
		name   string
		files  map[string]string
		server bool
	}{
		{"python server with a README", map[string]string{"README.md": "# a server\n", "src/pkg/server.py": server}, true},
		{"typescript server with a README", map[string]string{"README.md": "# a server\n", "index.ts": "export {}\n", "package.json": "{}\n"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			for rel, body := range tc.files {
				p := filepath.Join(src, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Stage(src, "x", t.TempDir())
			var np NotPlaceable
			if !errors.As(err, &np) {
				t.Fatalf("staged a project tree as an instruction file: err = %v", err)
			}
			if np.Server != tc.server {
				t.Fatalf("Server = %v, want %v (%s)", np.Server, tc.server, np.Reason)
			}
		})
	}
	// The instruction surface's own shape — one .md and nothing else — still places.
	alone := t.TempDir()
	if err := os.WriteFile(filepath.Join(alone, "01-direct-injection.md"), []byte("ignore previous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(alone, "x", t.TempDir()); err != nil {
		t.Fatalf("a lone instruction file must still stage as CLAUDE.md: %v", err)
	}
}
