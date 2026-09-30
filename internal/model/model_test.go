// SPDX-License-Identifier: MIT
package model

import "testing"

func TestSeverityRank(t *testing.T) {
	if !(SevLow.Rank() < SevMedium.Rank() && SevMedium.Rank() < SevHigh.Rank() && SevHigh.Rank() < SevCritical.Rank()) {
		t.Error("severity ordering must be low<medium<high<critical")
	}
	if Severity("bogus").Rank() != 0 {
		t.Error("unknown severity should rank 0")
	}
}
