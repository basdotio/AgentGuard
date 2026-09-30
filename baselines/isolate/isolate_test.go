// SPDX-License-Identifier: MIT

package isolate

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func spec() Spec {
	return Spec{
		Image:     "example/scanner@sha256:" + strings.Repeat("a", 64),
		SampleDir: "/host/corpus/malicious/mcp/ci-x",
		Command:   []string{"scanner", "scan", MountPoint},
	}
}

// TestArgsCarriesEveryContainmentFlag is the decided answer to the isolation question,
// written as assertions so that loosening one is a red test rather than a quiet edit. Each flag
// is here because a specific sample in this corpus exists to exercise what happens without it.
func TestArgsCarriesEveryContainmentFlag(t *testing.T) {
	got, err := Args(spec())
	if err != nil {
		t.Fatalf("Args: %v", err)
	}

	pairs := []struct {
		flag, value, why string
	}{
		{"--network", "none", "a malicious sample's purpose is often to reach a network, and it would do so from the operator's IP"},
		{"--cap-drop", "ALL", "a static scanner needs no capabilities"},
		{"--security-opt", "no-new-privileges", "no setuid escalation from inside"},
		{"--user", "65534:65534", "an escape should not begin as uid 0"},
		{"--memory", "2048m", "the corpus ships an 8 GiB sparse .mcp.json on purpose"},
		{"--pids-limit", "512", "a fork bomb in a scanned config is a resource-abuse sample"},
	}
	for _, p := range pairs {
		i := slices.Index(got, p.flag)
		if i < 0 {
			t.Errorf("missing %s — %s", p.flag, p.why)
			continue
		}
		if i+1 >= len(got) || got[i+1] != p.value {
			t.Errorf("%s = %q, want %q — %s", p.flag, got[i+1], p.value, p.why)
		}
	}
	for _, bare := range []string{"--rm", "--read-only"} {
		if !slices.Contains(got, bare) {
			t.Errorf("missing %s", bare)
		}
	}
}

// TestTheSampleIsMountedReadOnly — the sample is evidence. A scanner that "fixes" one has
// destroyed that test point for every later run, and the corpus is a shared denominator.
func TestTheSampleIsMountedReadOnly(t *testing.T) {
	got, err := Args(spec())
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	i := slices.Index(got, "--mount")
	if i < 0 || i+1 >= len(got) {
		t.Fatal("no --mount in the argv")
	}
	mount := got[i+1]
	for _, want := range []string{"type=bind", "source=" + spec().SampleDir, "target=" + MountPoint, "readonly"} {
		if !strings.Contains(mount, want) {
			t.Errorf("mount spec %q is missing %q", mount, want)
		}
	}
	// Writable scratch has to exist, or a scanner that writes a temp file fails for the wrong
	// reason and the run measures the container instead of the scanner.
	j := slices.Index(got, "--tmpfs")
	if j < 0 || !strings.Contains(got[j+1], "noexec") {
		t.Error("scratch tmpfs missing or executable; a payload that writes should get a place that vanishes")
	}
}

// TestArgsIsAnArgvNotAShellString — sample ids and paths come from deliberately hostile
// material. The one command line this project must never build is one a sample's own name can
// steer, so the check is that nothing is concatenated with a shell metacharacter in between.
func TestArgsIsAnArgvNotAShellString(t *testing.T) {
	s := spec()
	s.SampleDir = "/host/weird; rm -rf /"
	got, err := Args(s)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	var found bool
	for _, a := range got {
		if strings.Contains(a, "rm -rf /") {
			found = true
			// It must appear inside ONE argument, intact, never split into words.
			if !strings.HasPrefix(a, "type=bind,source=/host/weird; rm -rf /,") {
				t.Errorf("the path was reshaped instead of passed through: %q", a)
			}
		}
	}
	if !found {
		t.Error("the sample path vanished from the argv")
	}
	// Compared against the benign case rather than a constant: the claim is that the argv's
	// shape does not depend on the path's contents, and a magic number tests the number.
	benign, err := Args(spec())
	if err != nil {
		t.Fatalf("Args(benign): %v", err)
	}
	if len(got) != len(benign) {
		t.Errorf("argv length %d with a hostile path, %d with a benign one; word-splitting "+
			"would do exactly that", len(got), len(benign))
	}
}

func TestArgsRefusesAnUnpinnedOrEmptySpec(t *testing.T) {
	tests := []struct {
		name   string
		mangle func(*Spec)
		want   string
	}{
		{"no image", func(s *Spec) { s.Image = "" }, "unreproducible"},
		{"no sample directory", func(s *Spec) { s.SampleDir = "" }, "sample directory"},
		{"no command", func(s *Spec) { s.Command = nil }, "what to run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := spec()
			tt.mangle(&s)
			_, err := Args(s)
			if err == nil {
				t.Fatal("Args accepted an incomplete spec")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not mention %q: %v", tt.want, err)
			}
		})
	}
}

// TestAvailableIsARefusalNotADowngrade pins the one place in this project where a missing
// capability does not degrade to a disclosed gap. Everywhere else the cost of being wrong is a
// worse number; here it is the operator's machine.
func TestAvailableIsARefusalNotADowngrade(t *testing.T) {
	absent := func(string) (string, error) { return "", errors.New("not found") }
	err := Available(absent)
	if err == nil {
		t.Fatal("Available accepted a host with no container runtime")
	}
	if !errors.Is(err, ErrNoRuntime) {
		t.Errorf("error does not wrap ErrNoRuntime: %v", err)
	}
	for _, want := range []string{"refusal", "not a downgrade"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not say this is %q: %v", want, err)
		}
	}

	present := func(string) (string, error) { return "/usr/local/bin/docker", nil }
	if err := Available(present); err != nil {
		t.Errorf("Available rejected a host that has docker: %v", err)
	}
}
