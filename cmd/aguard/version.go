// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
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

// pluginBundleName is what the marketplace calls the bundle; PluginInstalls keys by it.
const pluginBundleName = "agentguard"

// The marketplace this repository publishes: the name .claude-plugin/marketplace.json declares
// (TestMarketplaceEntryVersionMatchesPlugin pins it) and the repo that serves it. Any other
// marketplace name on an install — above all `guard`, the old basdotio/guard distribution repo,
// which stopped at 0.9.0 — is one an in-place update may never bring level with this binary.
const (
	homeMarketplace     = "AgentGuard"
	homeMarketplaceRepo = "basdotio/AgentGuard"
)

// plainMarketplaceName is what a marketplace name must look like before it is repeated back.
// The name comes out of a config file, and the line it lands in is printed raw and relayed by
// the skills to the model — so a backtick, newline or shell metacharacter is never echoed.
var plainMarketplaceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// pluginVersionLine returns one line about the installed agentguard plugin relative to this
// binary's version, or "" when there is no plugin to compare with or the binary carries no
// release version (a dev build has nothing to be behind).
func pluginVersionLine(root, binaryVersion string) string {
	home := filepath.Dir(filepath.Clean(root))
	in, ok := collect.PluginInstalls(root, home)[pluginBundleName]
	if !ok {
		return ""
	}
	pv := bundleVersionOf(in.Dir)
	if pv == "" {
		return ""
	}
	bv, ok := releaseVersion(binaryVersion)
	if !ok {
		return fmt.Sprintf("plugin %s %s installed at %s (binary is a dev build; not compared)", pluginBundleName, pv, in.Dir)
	}
	switch compareSemver(bv, pv) {
	case -1:
		return fmt.Sprintf("plugin %s %s is newer than this binary (%s): new rules and allowlist entries are missing here — run /aguard-setup (or reinstall the binary) to upgrade", pluginBundleName, pv, bv)
	case 1:
		return fmt.Sprintf("plugin %s %s is older than this binary (%s) — %s", pluginBundleName, pv, bv, updateHint(in, bv))
	default:
		return fmt.Sprintf("plugin %s %s matches this binary", pluginBundleName, pv)
	}
}

// updateHint says how to update THIS install: the channel decides where (the desktop app has
// its own store; the CLI has `claude plugin`), and the recorded marketplace decides whether an
// in-place update can reach binaryVersion at all. It used to name `agentguard@guard` for every
// install, which is a marketplace an install from this repository does not have, and one that
// an install from the old repository can update against forever without catching up.
func updateHint(in collect.PluginInstall, binaryVersion string) string {
	home := pluginBundleName + "@" + homeMarketplace
	if in.Marketplace == homeMarketplace {
		if in.Desktop {
			return "update it from Claude Desktop's Customize panel"
		}
		return fmt.Sprintf("update the plugin (`claude plugin update %s`)", home)
	}
	from := "a marketplace this project does not publish to"
	old := ""
	if plainMarketplaceName.MatchString(in.Marketplace) {
		from = fmt.Sprintf("marketplace %q, not this project's (%s)", in.Marketplace, homeMarketplace)
		old = pluginBundleName + "@" + in.Marketplace
	}
	if in.Desktop {
		return fmt.Sprintf("it was installed from %s, so updating it there may never reach %s: in Claude Desktop's Customize panel, add the marketplace %s, install %s from it and remove the old one",
			from, binaryVersion, homeMarketplaceRepo, pluginBundleName)
	}
	cmd := fmt.Sprintf("claude plugin marketplace add %s && claude plugin install %s", homeMarketplaceRepo, home)
	if old != "" {
		cmd += " && claude plugin uninstall " + old
	}
	return fmt.Sprintf("it was installed from %s, so updating it there may never reach %s; switch: `%s`", from, binaryVersion, cmd)
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
