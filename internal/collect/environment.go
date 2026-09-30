// SPDX-License-Identifier: MIT
package collect

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// A scan run inside a Cloud / Cowork sandbox reads that sandbox's own throwaway ~/.claude, not
// the user's real machine, and comes back ~100 because the sandbox is nearly empty. A
// non-technical colleague who opens the saved report sees "100/100, looks safe" with no way to
// know it describes a temporary cloud room and not their computer. DetectEnvironment names the
// run's location so the report can say so — the reasons are carried too, so the claim can be
// checked rather than trusted.
//
// Direction of error is deliberately one-way: several independent signals must agree before a
// run is called a sandbox, and the worst a wrong call does is print one extra line on a real
// Linux box. It never suppresses findings or changes a score — it only annotates.

// EnvKind is where a scan ran.
type EnvKind string

const (
	EnvLocal   EnvKind = "local"   // an ordinary user machine
	EnvSandbox EnvKind = "sandbox" // a managed cloud container (Claude Cloud / Cowork)
)

// Environment is the detected run location plus the signals behind the call, so a report can
// explain WHY it thinks it is in a sandbox instead of asserting it.
type Environment struct {
	Kind    EnvKind
	Signals []string // human-readable reasons, e.g. "running as root (home /root)"
}

// DetectEnvironment inspects the machine the scan is running on. root is the scan root, used
// only to notice the sandbox-specific runtime files Cowork writes under ~/.claude.
func DetectEnvironment(root string) Environment {
	var sig []string

	// Container filesystem markers — present in a Linux container, absent on macOS and on a
	// bare Linux install.
	for _, m := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(m); err == nil {
			sig = append(sig, "container marker "+m)
			break
		}
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		c := string(b)
		for _, needle := range []string{"docker", "containerd", "kubepods", "lxc"} {
			if strings.Contains(c, needle) {
				sig = append(sig, "PID 1 cgroup names "+needle)
				break
			}
		}
	}

	// Running as root with home /root is normal in a container and abnormal on a user's own
	// machine — nobody runs their editor as root. Only counts on Linux (macOS root is a
	// different animal and users never reach this path there).
	if runtime.GOOS == "linux" {
		if home, err := os.UserHomeDir(); err == nil && (home == "/root" || strings.HasPrefix(home, "/root/")) {
			sig = append(sig, "running as root (home "+home+")")
		}
	}

	// Application-level marker: Cowork writes its own runtime state under the sandbox's
	// ~/.claude — files that never exist in a real user config. Any two of them together is a
	// strong sign of the managed launcher, independent of the OS signals above.
	hits := 0
	for _, name := range []string{"policy-limits.json", "launcher-settings.json", "environment-manager", "session-env"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			hits++
		}
	}
	if hits >= 2 {
		sig = append(sig, "managed-launcher runtime files under the config root")
	}

	// The call needs corroboration: a single signal (a lone /proc cgroup line on a
	// developer's Linux desktop, say) is not enough to tell someone their scan does not
	// describe their machine. Two independent signals is the bar.
	if len(sig) >= 2 {
		return Environment{Kind: EnvSandbox, Signals: sig}
	}
	return Environment{Kind: EnvLocal, Signals: sig}
}
