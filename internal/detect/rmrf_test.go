// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestDetect_RecursiveDeleteOfATreeRoot: FS-003 is titled "rm -rf against root/home" but
// matched on the path's first character, so every concrete subpath starting with / or ~ counted as the
// root — the npx cache, ~/Library/Caches, the Dockerfile idiom rm -rf /var/lib/apt/lists/* — and on the
// corpus seven benign skills were stopped at the gate by it alone, against zero malicious hits. The
// target is now the root of a tree, followed by a word boundary; equivalent flag spellings count, so
// `rm -fr /` is not a one-letter way around it.
func TestDetect_RecursiveDeleteOfATreeRoot(t *testing.T) {
	fire := []string{
		"rm -rf /", "rm -rf /*", "sudo rm -rf ~", "rm -rf ~/", "rm -rf ~/*",
		"rm -rf $HOME", `rm -rf "$HOME"`, `rm -rf "$HOME/"`, "rm -rf ${HOME}/", "rm -rf *",
		"rm -fr /", "rm -Rf ~", "rm -rfv /", "rm -r -f ~", "rm --recursive --force /",
		"rm -rf --no-preserve-root /", "cd /tmp/x && rm -rf ~ ; echo done",
	}
	for _, cmd := range fire {
		t.Run("fires/"+cmd, func(t *testing.T) {
			f, ok := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})["FS-003"]
			if !ok {
				t.Fatalf("FS-003 missing on %q", cmd)
			}
			if f.Severity != model.SevHigh || f.Dimension != 9 {
				t.Errorf("FS-003 = %s / dim %d, want high / 9", f.Severity, f.Dimension)
			}
		})
	}
	quiet := []string{
		"rm -rf ~/.npm/_npx", "rm -rf ~/Library/Caches/*", "rm -rf /var/lib/apt/lists/*",
		"rm -rf ~/.bun/install/cache", "rm -rf ./dist", "rm -rf node_modules",
		`rm -rf "$HOME"/.cache/build`, `rm -rf "$HOME/.cache"`, "rm -rf /tmp/session-*", "rm -rf ${HOME}/.cache/pip",
	}
	for _, cmd := range quiet {
		t.Run("quiet/"+cmd, func(t *testing.T) {
			if _, ok := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})["FS-003"]; ok {
				t.Errorf("FS-003 fired on a scoped path: %q", cmd)
			}
		})
	}
}
