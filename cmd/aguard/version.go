// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// Binary-behind-plugin detection, without a network call.
//
// The binary never talks to the network (invariant #1), so it cannot ask whether a newer
// release exists. The plugin can be updated for it, though: Claude Code refreshes marketplace
// plugins on its own, and every release bumps plugin.json to the binary version it ships with.
// So a plugin whose version is ahead of this binary is a machine that got the new skill text
// and not the new rules — and nothing in a scan's output would say so. `aguard version` says so,
// reading the installed plugin through the same collectors a scan uses (both install channels),
// and the skills relay the line when they check the binary.

// pluginBundleName is what the marketplace calls the bundle; PluginPaths keys by it.
const pluginBundleName = "agentguard"

// pluginVersionLine returns one line about the installed agentguard plugin relative to this
// binary's version, or "" when there is no plugin to compare with or the binary carries no
// release version (a dev build has nothing to be behind).
func pluginVersionLine(root, binaryVersion string) string {
	home := filepath.Dir(filepath.Clean(root))
	dir, ok := collect.PluginPaths(root, home)[pluginBundleName]
	if !ok {
		return ""
	}
	pv := bundleVersionOf(dir)
	if pv == "" {
		return ""
	}
	bv, ok := releaseVersion(binaryVersion)
	if !ok {
		return fmt.Sprintf("plugin %s %s installed at %s (binary is a dev build; not compared)", pluginBundleName, pv, dir)
	}
	switch compareSemver(bv, pv) {
	case -1:
		return fmt.Sprintf("plugin %s %s is newer than this binary (%s): new rules and allowlist entries are missing here — run /aguard-setup (or reinstall the binary) to upgrade", pluginBundleName, pv, bv)
	case 1:
		return fmt.Sprintf("plugin %s %s is older than this binary (%s) — update the plugin (`claude plugin update %s`)", pluginBundleName, pv, bv, pluginBundleName+"@guard")
	default:
		return fmt.Sprintf("plugin %s %s matches this binary", pluginBundleName, pv)
	}
}

func bundleVersionOf(dir string) string {
	b, err := safeio.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), safeio.MaxConfigBytes)
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return strings.TrimSpace(m.Version)
}

// releaseVersion extracts "X.Y.Z" from a build version like "v0.3.0", "v0.3.0-1-gabc-dirty"
// or "0.4.0". Anything without a leading semver ("dev", "none") is not a release.
func releaseVersion(v string) (string, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	if _, ok := semverParts(v); !ok {
		return "", false
	}
	return v, true
}

func semverParts(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// compareSemver returns -1, 0, 1 for a<b, a==b, a>b; an unparsable side compares as equal, so a
// malformed plugin.json version can never produce a false "you are behind".
func compareSemver(a, b string) int {
	pa, oka := semverParts(a)
	pb, okb := semverParts(b)
	if !oka || !okb {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}
