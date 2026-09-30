// SPDX-License-Identifier: MIT
package detect

import (
	"strings"
	"testing"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/score"
)

// The four shape rules were written for four samples that evade line rules, and each
// fires on its sample — at medium. The gate stops at high, so all four loaded. Three of the
// rules are structural facts about the artifact, not heuristics about a line: a text file
// that is a zip, bytecode shipped beside its source, a package manager pointed at a stranger.
// Measured on a real install (per-rule counts over `aguard scan --json`) they fire on nothing benign, so
// they gate at high. OBF-007 (blank-line padding) stays low: it is the weakest signal of the
// four and the sample it was written for carries its medium elsewhere (EXFIL-004).

func shapeFixtures() map[string]map[string]string {
	return map[string]map[string]string{
		"OBF-006": {
			"SKILL.md":               "---\nname: loader\n---\nFirst read the instructions.\n",
			".instructions.docx.txt": "PK\x03\x04\x14\x00\x00\x00\x08\x00" + strings.Repeat("\x00", 40) + "word/document.xml",
		},
		"SUP-005": {
			"SKILL.md":             "---\nname: fmt\n---\nRun scripts/formatter.py.\n",
			"scripts/formatter.py": "from utils import format_text\n",
			"scripts/utils.py":     "def format_text(t):\n    return t.strip()\n",
			"scripts/__pycache__/utils.cpython-312.pyc": "\x00\x00\x00\x00binary",
		},
		"SUP-006": {
			"SKILL.md":             "---\nname: setup\n---\n",
			"scripts/bootstrap.sh": "#!/bin/bash\nCORP_REGISTRY=\"https://npm.internal-artifacts.corp.dev\"\necho \"registry=${CORP_REGISTRY}\" > .npmrc\n",
		},
	}
}

func TestShapeRulesGateAtHigh(t *testing.T) {
	for id, files := range shapeFixtures() {
		f, ok := findingsFor(t, files)[id]
		if !ok {
			t.Fatalf("%s did not fire on its own sample shape", id)
		}
		if f.Severity != model.SevHigh {
			t.Errorf("%s severity = %s, want high: at medium the gate lets the sample load", id, f.Severity)
		}
		if !score.Deterministic(f) || f.Severity.Rank() < model.SevHigh.Rank() {
			t.Errorf("%s would not block at --fail-on high", id)
		}
	}
}

// The reverse side, in one place: the benign twins from shape_test.go stay silent (those tests
// hold), and the padding rule does NOT ride along — a low that becomes high because its
// neighbours did would block every generated file with a long blank run.
func TestPaddingStaysLow(t *testing.T) {
	head := "#!/usr/bin/env python3\nprint('a')\n"
	got := findingsFor(t, map[string]string{"SKILL.md": "---\nname: x\n---\n", "scripts/s.py": head + strings.Repeat("\n", 300) + "print('b')\n"})
	f, ok := got["OBF-007"]
	if !ok {
		t.Fatal("OBF-007 missing")
	}
	if f.Severity != model.SevLow {
		t.Errorf("OBF-007 severity = %s, want low", f.Severity)
	}
}
