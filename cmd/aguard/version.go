// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/reputation"
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

// pluginBundleName is what the marketplace calls the bundle; PluginInstalls keys by it. It must
// equal plugin.json's name, which is the skill namespace the gate resolves, and must differ up to
// case from every marketplace name (TestPluginNameCannotCollideInTheInstallCache, issues/022).
const pluginBundleName = "aguard"

// legacyBundleName is what the plugin was called through v0.16.0. `agentguard` in marketplace
// `AgentGuard` is one directory on a case-insensitive volume, and Claude Code's install moved the
// plugin into itself (issues/022). The marketplace no longer lists the old name, so an install
// under it never updates again — pluginVersionLine tells it to switch.
const legacyBundleName = "agentguard"

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

// binaryVersionLine is the first line `aguard version` prints. New fields go at the END: the release
// workflow reads `awk '{print $2}'` as the version and refuses a release that does not match its
// tag, and the baselines adapter records the whole line as tool_version. version, commit and date
// are the -ldflags stamps, or what applyBuildInfo filled in when there were none; neither source
// puts a space in them, so $2 is the version either way. (versionLine, below, is the plugin's line.)
func binaryVersionLine(version, commit, date string, reputationEntries int, rulesVersion string) string {
	return fmt.Sprintf("aguard %s (commit %s, built %s) · reputation entries=%d · rules=%s",
		version, commit, date, reputationEntries, rulesVersion)
}

// runVersion is the whole body of `aguard version`, a function so the zero-dial test runs what
// the command runs rather than one helper of it: the build line, then — when the plugin is
// installed, under its current name or the old one — one line comparing it with this binary.
// version, commit and date are printed as they stand: the -ldflags stamps, or what
// applyBuildInfo filled in from the build info at init when the stamps were absent.
//
// Offline by construction: compares against the plugin already on disk, never a release feed.
// The plugin auto-updates through Claude Code; the binary does not.
func runVersion(w io.Writer, root string) {
	fmt.Fprintln(w, binaryVersionLine(version, commit, date, reputation.Load().Len(), detect.RulesVersion()))
	if line := pluginVersionLine(root, version); line != "" {
		fmt.Fprintln(w, line)
	}
}

// pluginVersionLine returns one line about the installed plugin relative to this binary's
// version, or "" when there is no plugin to compare with. An install under legacyBundleName is
// told to switch whatever the versions say: it can never again report "newer than this binary",
// which is how a user learns the binary fell behind.
func pluginVersionLine(root, binaryVersion string) string {
	// The parent of the anchored root, as the gate takes it: from a relative root, Dir gave a relative
	// home that no install path could be related to, and the line silently disappeared.
	root = collect.AnchorRoot(root)
	home := filepath.Dir(root)
	installs := collect.PluginInstalls(root, home)
	cur, hasCur := installs[pluginBundleName]
	old, hasOld := installs[legacyBundleName]
	if !hasCur {
		if hasOld {
			return renamedLine(old)
		}
		return ""
	}
	line := versionLine(cur, binaryVersion)
	if line != "" && hasOld {
		line += "; " + leftoverHint(old)
	}
	return line
}

// versionLine compares one install under pluginBundleName with this binary, or returns "" when
// its plugin.json carries no version. A dev build is reported, not compared.
func versionLine(in collect.PluginInstall, binaryVersion string) string {
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
	if in.Marketplace == homeMarketplace {
		if in.Desktop {
			return "update it from Claude Desktop's Customize panel"
		}
		return fmt.Sprintf("update the plugin (`claude plugin update %s@%s`)", pluginBundleName, homeMarketplace)
	}
	from := "a marketplace this project does not publish to"
	if plainMarketplaceName.MatchString(in.Marketplace) {
		from = fmt.Sprintf("marketplace %q, not this project's (%s)", in.Marketplace, homeMarketplace)
	}
	if in.Desktop {
		return fmt.Sprintf("it was installed from %s, so updating it there may never reach %s: in Claude Desktop's Customize panel, add the marketplace %s, install %s from it and remove the old one",
			from, binaryVersion, homeMarketplaceRepo, pluginBundleName)
	}
	return fmt.Sprintf("it was installed from %s, so updating it there may never reach %s; switch: `%s`",
		from, binaryVersion, switchCommand(in.Marketplace, pluginBundleName))
}

// renamedLine reports an install under legacyBundleName, with no current install next to it.
func renamedLine(old collect.PluginInstall) string {
	pv := bundleVersionOf(old.Dir)
	if pv != "" {
		pv = " " + pv
	}
	return fmt.Sprintf("plugin %s%s is installed under the plugin's old name: since 0.17.0 it is %s, and the old name gets no further updates — %s",
		legacyBundleName, pv, pluginBundleName, renameHint(old))
}

// renameHint says how to move an install under legacyBundleName to pluginBundleName.
func renameHint(old collect.PluginInstall) string {
	if old.Desktop {
		return fmt.Sprintf("in Claude Desktop's Customize panel, install %s from the %s marketplace (add %s first if it is not listed) and remove %s",
			pluginBundleName, homeMarketplace, homeMarketplaceRepo, legacyBundleName)
	}
	return fmt.Sprintf("switch: `%s`", switchCommand(old.Marketplace, legacyBundleName))
}

// leftoverHint is appended when an install under legacyBundleName sits next to the current one.
// It duplicates every skill, and a bare skill name two plugins provide is ambiguous to the gate.
func leftoverHint(old collect.PluginInstall) string {
	const lead = "the old " + legacyBundleName + " plugin is still installed too and duplicates every skill — remove it"
	switch {
	case old.Desktop:
		return lead + " in Claude Desktop's Customize panel"
	case plainMarketplaceName.MatchString(old.Marketplace):
		return fmt.Sprintf("%s: `claude plugin uninstall %s@%s`", lead, legacyBundleName, old.Marketplace)
	default:
		return lead
	}
}

// switchCommand is the CLI line that installs pluginBundleName from this project's marketplace
// and removes oldBundle as installed from marketplace. It adds the marketplace first unless the
// old install already came from it, and names the old install only when its marketplace is a
// plain name (an unrecognizable one is not repeated back).
func switchCommand(marketplace, oldBundle string) string {
	var parts []string
	if marketplace != homeMarketplace {
		parts = append(parts, "claude plugin marketplace add "+homeMarketplaceRepo)
	}
	parts = append(parts, "claude plugin install "+pluginBundleName+"@"+homeMarketplace)
	if plainMarketplaceName.MatchString(marketplace) {
		parts = append(parts, "claude plugin uninstall "+oldBundle+"@"+marketplace)
	}
	return strings.Join(parts, " && ")
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
