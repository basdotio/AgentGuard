// SPDX-License-Identifier: MIT
package detect

import (
	"encoding/json"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// mcpServerMap is the server map of an MCP config document: its `mcpServers` member, or the
// document itself for a plugin file collect read without the wrapper (MCPUnwrapped, P-029 — Claude
// Code reads a plugin MCP file as `doc.mcpServers || doc`). Collect decides the layout once and
// records it; the rule units, MCPServerKey's "" check and the content hash all pick the map here,
// so the three cannot be reading different entries. A wrapper that is not an object reads as an
// empty map, as it always did.
func mcpServerMap(doc map[string]json.RawMessage, unwrapped bool) map[string]json.RawMessage {
	if unwrapped {
		return doc
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := doc["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &servers)
	}
	return servers
}

// mcpEntry reads path and returns the entry under key in its server map.
func mcpEntry(path string, unwrapped bool, key string) (json.RawMessage, bool) {
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return nil, false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	raw, ok := mcpServerMap(doc, unwrapped)[key]
	return raw, ok
}

// artifactMCPEntry is the entry an MCP artifact stands for.
func artifactMCPEntry(a model.ArtifactReport) (json.RawMessage, bool) {
	return mcpEntry(a.Path, a.MCPUnwrapped, MCPServerKey(a))
}

// mcpUnits are the rule units of an MCP artifact's entry: its string values, and its env block as
// KEY=VALUE lines.
func mcpUnits(a model.ArtifactReport) []unit {
	raw, ok := artifactMCPEntry(a)
	if !ok {
		return nil
	}
	return append(jsonStrings(a.Path, raw), entryEnvUnit(a.Path, raw)...)
}

// MCPConfigLines renders an MCP artifact's entry the way ConfigLines renders section[name]: the
// view the LLM judge's MCP config pass sends. Exported so the judge finds the entry where the rules
// and the content hash find it, wrapped or not.
func MCPConfigLines(a model.ArtifactReport) []string {
	raw, ok := artifactMCPEntry(a)
	if !ok {
		return nil
	}
	return entryLines(raw)
}
