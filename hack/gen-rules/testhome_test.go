// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"testing"

	"github.com/basdotio/bas-guard/internal/testhome"
)

// TestMain runs this package's tests with a home of their own, never the developer's (P-075, P-077).
func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }
