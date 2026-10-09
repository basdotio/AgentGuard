// SPDX-License-Identifier: MIT
package collect

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// mcpView lists a collection's MCP artifacts as "Name|MCPServer|unwrapped", sorted.
func mcpView(res Result) []string {
	var out []string
	for _, a := range res.Artifacts {
		if a.Kind == model.KindMCP {
			out = append(out, fmt.Sprintf("%s|%s|%v", a.Name, a.MCPServer, a.MCPUnwrapped))
		}
	}
	sort.Strings(out)
	return out
}

// TestCollect_PluginMCPWithoutWrapper: Claude Code reads a plugin's MCP file as
// `doc.mcpServers || doc` (measured on 2.1.107, P-029): a file that lists its servers at the top
// level starts them exactly like a wrapped one, and so does one whose `mcpServers` is null, false,
// 0 or "". The collector decoded only the wrapper, so those servers were never artifacts — zero
// units, zero count, no note — while running in every session. And because Go matched the wrapper
// key case-insensitively, `MCPSERVERS: {}` hid a server Claude Code starts, and `McpServers` made up
// a server it never starts. Each row is a layout Claude Code was measured on, and what it started.
func TestCollect_PluginMCPWithoutWrapper(t *testing.T) {
	const node = `{"command":"node","args":["s.js"]}`
	cases := []struct {
		name, file, cfg string
		want            []string
	}{
		{"servers at the top level", ".mcp.json",
			`{"a":` + node + `,"b":{"type":"http","url":"https://mcp.example.com/mcp"}}`,
			[]string{"a (plugin p@mkt)|a|true", "b (plugin p@mkt)|b|true"}},
		{"top level with members that are not servers", ".mcp.json",
			`{"$schema":"https://example.com/s.json","version":2,"notaserver":{"description":"x"},"ok":` + node + `}`,
			[]string{"ok (plugin p@mkt)|ok|true"}},
		{"mcpServers null beside a server", ".mcp.json", `{"mcpServers":null,"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{"mcpServers false beside a server", ".mcp.json", `{"mcpServers":false,"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{"mcpServers 0 beside a server", ".mcp.json", `{"mcpServers":0,"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{"mcpServers 0.0 beside a server", ".mcp.json", `{"mcpServers":0.0,"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{`mcpServers "" beside a server`, ".mcp.json", `{"mcpServers":"","s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{"case-variant empty wrapper beside a server", ".mcp.json", `{"MCPSERVERS":{},"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		{"case-variant wrapper holding servers", ".mcp.json", `{"McpServers":{"s":` + node + `}}`, nil},
		{`top-level key ""`, ".mcp.json", `{"":` + node + `}`, []string{" (plugin p@mkt)||true"}},
		{"bare mcp.json at the top level", "mcp.json", `{"s":` + node + `}`, []string{"s (plugin p@mkt)|s|true"}},
		// The wrapped layout is today's, row for row.
		{"wrapped", ".mcp.json", `{"mcpServers":{"w":` + node + `}}`, []string{"w (plugin p@mkt)|w|false"}},
		{"wrapped entry with neither command nor type is still collected", ".mcp.json",
			`{"mcpServers":{"odd":{"description":"x"}}}`, []string{"odd (plugin p@mkt)|odd|false"}},
		{"wrapped beside a top-level sibling", ".mcp.json", `{"mcpServers":{"in":` + node + `},"out":` + node + `}`,
			[]string{"in (plugin p@mkt)|in|false"}},
		{"empty wrapper beside a sibling", ".mcp.json", `{"mcpServers":{},"out":` + node + `}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".claude")
			install := filepath.Join(root, "plugins", "cache", "mkt", "p", "1.0.0")
			mustWrite(t, filepath.Join(install, ".claude-plugin", "plugin.json"), `{"name":"p","version":"1.0.0"}`)
			mustWrite(t, filepath.Join(install, tc.file), tc.cfg)
			installedPlugins(t, root, "p@mkt", install)

			res := CollectAll(root)
			got := mcpView(res)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("MCP artifacts = %q, want %q (what Claude Code starts from this file)", got, tc.want)
			}
			if res.Env.MCPServers != len(tc.want) {
				t.Errorf("env.mcp_servers = %d, want %d", res.Env.MCPServers, len(tc.want))
			}
		})
	}
}

// TestCollect_UserAndProjectMCPAreNeverReadUnwrapped is the reverse: only a plugin's MCP file has
// the `doc.mcpServers || doc` fallback. Claude Code rejects a flat project .mcp.json (measured), and
// ~/.claude.json's top level is the CLI's own settings — reading either as a server map would make
// servers up.
func TestCollect_UserAndProjectMCPAreNeverReadUnwrapped(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	mustWrite(t, filepath.Join(home, ".claude.json"), `{"numStartups":3,"tool":{"command":"node"},"projects":{}}`)
	mustWrite(t, filepath.Join(home, ".mcp.json"), `{"projflat":{"command":"node"}}`)
	mustWrite(t, filepath.Join(root, ".mcp.json"), `{"rootflat":{"type":"http","url":"https://mcp.example.com"}}`)
	res := CollectAll(root)
	if got := mcpView(res); len(got) != 0 || res.Env.MCPServers != 0 {
		t.Errorf("MCP artifacts = %q (count %d), want none: only plugin MCP files are read without the wrapper", got, res.Env.MCPServers)
	}
}
