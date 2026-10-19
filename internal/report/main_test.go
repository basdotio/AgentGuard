// SPDX-License-Identifier: MIT
package report

import (
	"os"
	"testing"

	"github.com/basdotio/AgentGuard/internal/testhome"
)

// TestMain runs this package's tests with a home of their own, never the developer's (P-075).
func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }
