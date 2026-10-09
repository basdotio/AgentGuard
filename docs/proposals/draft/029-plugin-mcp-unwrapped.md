<!-- SPDX-License-Identifier: MIT -->
# 029 — A plugin's MCP servers written without the `mcpServers` wrapper are invisible to the scan: Claude Code starts them, aguard counts zero and says nothing

- **Source**: follow-up recorded in P-021 open question 4 ([021](../complete/021-plugin-mcp-unscanned.md)); one plugin
  installed on the maintainer's machine writes its `.mcp.json` this way
- **Depends on**: P-021 (`detect.MCPServerKey`), P-009 (`ArtifactReport.MCPServer`, MCP content hash)
- **Branch**: `p/029-plugin-mcp-unwrapped`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`collect.collectPluginMCP` reads a plugin's `.mcp.json` / `mcp.json` through `mcpServersFrom`, which decodes only the
`mcpServers` member. A plugin file that lists its servers directly at the top level (`{"name": {"command": …}}`, no
wrapper) decodes to an empty map: zero MCP artifacts, `mcp_servers` does not count them, no note says the file was not
understood, and the plugin scores what its tree scores. The rules, the content hash and the judge's MCP config pass never
see those servers. A server whose risk sits in a key the tree's text reading cannot see (`NODE_OPTIONS` in `env`, P-021)
passes with a clean 100.

Whether this is a false negative depends on what Claude Code itself does with such a file; that is measured first.

## Initial direction

Measure Claude Code (the installed CLI, isolated `CLAUDE_CONFIG_DIR`) with and without the wrapper. If it starts the
unwrapped servers, collect them exactly like wrapped ones (same naming, `MCPServer` key, content hash, rules, judge pass);
if it does not, do not invent servers, but say so with a coverage note (invariant #5).
