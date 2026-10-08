// SPDX-License-Identifier: MIT
package detect

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestConfigLines_SameLeavesAsConfigStrings: the judge's view of an MCP entry is ConfigLines, the
// scanner's is configStrings. They must be the SAME string leaves — only keyed and ordered — or a
// verdict could describe text the static pass never read. Keys are sorted and array elements
// keep their order, so the rendering is byte-stable where Go's map order is not.
func TestConfigLines_SameLeavesAsConfigStrings(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	cfg := `{"mcpServers":{"db":{"type":"stdio","command":"npx","args":["-y","@acme/db-mcp@1.2.3","--data","/srv/data"],` +
		`"env":{"DB_PASS":"hunter2","LOG_LEVEL":"debug","API_BASE":"https://db.example.com"},` +
		`"headers":{"Authorization":"Bearer abcdefghijklmnop"},"timeout":30,"disabled":false,` +
		`"extra":[{"name":"one"},{"name":"two"}]}}}`
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	lines := ConfigLines(p, "mcpServers", "db")
	want := []string{
		"args=-y", "args=@acme/db-mcp@1.2.3", "args=--data", "args=/srv/data",
		"command=npx",
		"env.API_BASE=https://db.example.com", "env.DB_PASS=hunter2", "env.LOG_LEVEL=debug",
		"extra.name=one", "extra.name=two",
		"headers.Authorization=Bearer abcdefghijklmnop",
		"type=stdio",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("ConfigLines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// Same leaves as the scanner's view, as a multiset.
	values := make([]string, len(lines))
	for i, l := range lines {
		_, v, _ := strings.Cut(l, "=")
		values[i] = v
	}
	strs := configStrings(p, "mcpServers", "db")
	sort.Strings(values)
	sort.Strings(strs)
	if strings.Join(values, "\n") != strings.Join(strs, "\n") {
		t.Errorf("leaves differ from the scanner's view:\njudge   %q\nscanner %q", values, strs)
	}

	for i := 0; i < 20; i++ {
		if again := ConfigLines(p, "mcpServers", "db"); strings.Join(again, "\n") != strings.Join(lines, "\n") {
			t.Fatalf("call %d rendered different bytes", i)
		}
	}
	if got := ConfigLines(p, "mcpServers", "absent"); got != nil {
		t.Errorf("an absent entry renders nothing, got %q", got)
	}
}
