// SPDX-License-Identifier: MIT

package aguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
)

// TestRawOutputNamesNoWorkDirectory: aguard reports every path under the staging root,
// which lives in the operator's per-user temp directory. Four committed judge runs carried
// about twelve thousand copies of that directory. keepRaw writes <work> in its place, so a raw
// file says where in the STAGED tree something was without saying whose machine staged it.
func TestRawOutputNamesNoWorkDirectory(t *testing.T) {
	a := &Adapter{Work: t.TempDir(), RawDir: t.TempDir()}
	res := model.ScanResult{Root: a.Work + "/s/home/.claude", Artifacts: []model.ArtifactReport{{
		Kind: model.KindSkill, Name: "x", Path: a.Work + "/s/home/.claude/skills/x"}}}

	a.keepRaw("s", res)

	b, err := os.ReadFile(filepath.Join(a.RawDir, "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, a.Work) {
		t.Errorf("raw output names the work directory:\n%s", got)
	}
	if !strings.Contains(got, `"<work>/s/home/.claude"`) || !strings.Contains(got, `"<work>/s/home/.claude/skills/x"`) {
		t.Errorf("raw output lost the staged path:\n%s", got)
	}
	_ = context.Background
}
