// SPDX-License-Identifier: MIT
package score

import (
	"fmt"
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// TestExplain_IsApply: the working a report prints under "Why N" (P-058) is the working Apply did — the
// same units, the same mean, the same cap — so the sentence cannot disagree with the number.
func TestExplain_IsApply(t *testing.T) {
	high := `{"rule_id":"EXFIL-001","dimension":3,"severity":"high","source":"static","evidence":[{"file":"a","line":1}]}`
	crit := `{"rule_id":"REP-BAD","dimension":3,"severity":"critical","source":"static","evidence":[{"file":"a","line":1}]}`
	low := `{"rule_id":"EXEC-003","dimension":4,"severity":"low","source":"static","evidence":[{"file":"a","line":1}]}`
	note := `{"rule_id":"COV-000","dimension":0,"severity":"high","source":"static","evidence":[]}`
	skill := func(name, fs string) string {
		return fmt.Sprintf(`{"kind":"skill","name":%q,"path":"/h/%s","findings":[%s]}`, name, name, fs)
	}
	for _, c := range []struct {
		name              string
		arts              string
		units, mean, ceil int
		worst             model.Severity
	}{
		{"empty", `[]`, 0, 100, 100, ""},
		{"one high", `[` + skill("a", high) + `]`, 1, 75, 69, model.SevHigh},
		{"high among clean", `[` + skill("a", high) + `,` + skill("b", "") + `,` + skill("c", "") + `]`, 3, 91, 69, model.SevHigh},
		{"critical", `[` + skill("a", crit) + `,` + skill("b", "") + `]`, 2, 80, 49, model.SevCritical},
		{"low and a note", `[` + skill("a", low+`,`+note) + `,` + skill("b", "") + `]`, 2, 97, 100, model.SevLow},
		{"plugin family is one unit", `[` + pluginP + `,` + hookOfP + kids(3) + `]`, 2, 94, 100, model.SevMedium},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := unitResult(t, c.arts)
			Apply(&r)
			b := Explain(r.Artifacts)
			if b.Overall != r.Overall {
				t.Fatalf("Explain says %d, Apply set %d", b.Overall, r.Overall)
			}
			if b.Units != c.units || b.Mean != c.mean || b.Ceiling != c.ceil || b.Worst != c.worst {
				t.Errorf("got units=%d mean=%d ceiling=%d worst=%q, want %d %d %d %q", b.Units, b.Mean, b.Ceiling, b.Worst, c.units, c.mean, c.ceil, c.worst)
			}
		})
	}
	for _, s := range []model.Severity{model.SevCritical, model.SevHigh, model.SevMedium, model.SevLow} {
		if Points(s) != penalty[s] {
			t.Errorf("Points(%s) = %d, the formula uses %d", s, Points(s), penalty[s])
		}
	}
}
