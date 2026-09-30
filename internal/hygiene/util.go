// SPDX-License-Identifier: MIT
package hygiene

import (
	"os"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func pct(f float64) string { return strconv.Itoa(int(f*100+0.5)) + "%" }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
