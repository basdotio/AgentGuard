// SPDX-License-Identifier: MIT

// Package isolate builds the container invocation for a scanner that executes what it scans,
// and refuses to produce one when the host cannot provide isolation.
//
// aguard's invariant #1 — never execute scanned content, never reach the network — protects
// aguard. It says nothing about anybody else's tool. Snyk's agent-scan documents that scanning
// an MCP config executes the commands defined in it; pointing a tool like that at this corpus
// runs 300 malicious payloads. So `executes_scanned_content: true` in baselines/tools.yaml is
// not a note, it is a precondition.
//
// No container runtime is a REFUSAL, never a downgrade. Everywhere else in
// this project a missing capability degrades to a disclosed gap, because the cost of being wrong
// is a worse number. Here the cost of being wrong is the operator's machine, and a number is not
// worth that. Available() is therefore checked before a run starts rather than per sample — a
// run that got halfway and then found no docker would already have executed whatever it
// executed.
//
// # Nothing consumes Args yet, on purpose
//
// No adapter for a tool that executes scanned content exists today, and none is scheduled.
// Args is written anyway because it IS the decided policy, and the choices
// in it — no network, read-only sample mount, bounded memory — are the kind that get improvised
// badly at the moment somebody actually needs them. Reviewing them while nothing depends on them
// is the cheap time to do it.
package isolate

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// MountPoint is where a sample tree appears inside the container. Fixed rather than derived from
// the host path: a scanner's own output then quotes a stable path, and two runs on machines with
// different checkout locations produce comparable reports.
const MountPoint = "/sample"

// Limits bound what one scan may consume. They are not tidiness — the corpus ships an 8 GiB
// sparse .mcp.json and a zip-bomb sample precisely to see what a scanner does with them, and
// without a cap the answer is "takes the host down with it".
type Limits struct {
	// Memory in mebibytes.
	MemoryMiB int
	// PIDs caps process count; a fork bomb in a scanned config is a resource-abuse sample.
	PIDs int
}

// DefaultLimits are deliberately generous enough for a real scanner and far below a machine.
var DefaultLimits = Limits{MemoryMiB: 2048, PIDs: 512}

// Spec is one containerised scan.
type Spec struct {
	// Image is the container image holding the scanner. Pinned by digest by the caller; this
	// package does not invent one, because a floating tag would make a run unreproducible
	// while looking fine.
	Image string
	// SampleDir is the host path of the sample tree. Mounted read-only.
	SampleDir string
	// Command is the scanner's own argv inside the container, already referring to MountPoint.
	Command []string
	// Limits bounds the run; the zero value means DefaultLimits.
	Limits Limits
}

// ErrNoRuntime is returned when no container runtime is usable.
var ErrNoRuntime = errors.New("no container runtime")

// Available reports whether isolation can be provided. lookPath is injectable so the refusal
// path is testable without uninstalling docker.
func Available(lookPath func(string) (string, error)) error {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("docker"); err != nil {
		return fmt.Errorf("%w: docker is not on PATH. This is a refusal, not a downgrade: a "+
			"scanner that executes what it scans would run this corpus's malicious payloads on "+
			"the host, and no measurement is worth that", ErrNoRuntime)
	}
	return nil
}

// Args builds the full docker argv for one scan. It returns an argv, never a shell string:
// sample ids and paths come from a corpus of deliberately hostile material, and the one place
// this project must never build is a command line a sample's own name can steer.
func Args(s Spec) ([]string, error) {
	switch {
	case s.Image == "":
		return nil, errors.New("no image: a floating tag or an empty image makes the run " +
			"unreproducible, so the caller has to pin one")
	case s.SampleDir == "":
		return nil, errors.New("no sample directory to mount")
	case len(s.Command) == 0:
		return nil, errors.New("no command: the adapter has to say what to run inside")
	}
	lim := s.Limits
	if lim.MemoryMiB == 0 {
		lim.MemoryMiB = DefaultLimits.MemoryMiB
	}
	if lim.PIDs == 0 {
		lim.PIDs = DefaultLimits.PIDs
	}

	args := []string{
		"run", "--rm",
		// No network at all. A malicious sample's whole purpose is often to reach one, and a
		// scanner that executes it would do so from the operator's IP.
		"--network", "none",
		// The sample is evidence; nothing may edit it. A scanner that "fixes" a sample has
		// destroyed the test point for every later run.
		"--mount", "type=bind,source=" + s.SampleDir + ",target=" + MountPoint + ",readonly",
		// Read-only root with an explicit scratch tmpfs, so a payload that writes gets a
		// writable place that vanishes rather than a container layer that persists.
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=256m",
		// Drop everything, add nothing back. A static scanner needs no capabilities; one that
		// does is telling you something.
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		// Not root inside, so a container escape does not start as uid 0.
		"--user", "65534:65534",
		"--memory", strconv.Itoa(lim.MemoryMiB) + "m",
		"--pids-limit", strconv.Itoa(lim.PIDs),
		s.Image,
	}
	return append(args, s.Command...), nil
}
