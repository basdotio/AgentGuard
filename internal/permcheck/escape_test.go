// SPDX-License-Identifier: MIT
package permcheck

import (
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestEscapes covers the whole point of PERM-006: `Bash(git *)` is `Bash(*)` wearing a
// disguise, while a fully-specified or properly-pinned grant must stay clean.
func TestEscapes(t *testing.T) {
	cases := []struct {
		entry string
		want  bool
	}{
		// Open-ended arguments over an escapable binary.
		{"Bash(git *)", true},
		{"Bash(git:*)", true},
		{"Bash(find *)", true},
		{"Bash(awk *)", true},
		{"Bash(tar *)", true},
		{"Bash(docker *)", true},
		{"Bash(ssh *)", true},
		{"Bash(npm *)", true},
		{"Bash(make *)", true},
		{"Bash(xargs *)", true},
		{"Bash(rsync *)", true},
		{"Bash(/usr/bin/git *)", true},    // absolute path is the same binary
		{"Bash(env FOO=bar git *)", true}, // env prefix doesn't change what runs
		{"Bash(  git   *  )", true},       // whitespace tolerance
		// The pinned part is itself the lever.
		{"Bash(npm run *)", true},
		{"Bash(docker run *)", true},
		{"Bash(git -c core.pager=sh *)", true},
		{"Bash(find . -exec *)", true},
		{"Bash(tar --checkpoint-action=exec=sh *)", true},
		{"Bash(ssh -o ProxyCommand=sh *)", true},
		// positionFree: the binary takes its lever after a pinned argument, so pinning one
		// buys nothing. Each of these was measured — see TestPositionFreeIsMeasuredNotGuessed.
		{"Bash(make test *)", true},
		{"Bash(make test:*)", true},
		{"Bash(find . -name *)", true},
		{"Bash(rsync -a src dst *)", true},
		{"Bash(ssh myhost *)", true},
		{"Bash(vim notes.txt *)", true},
		// Pinned to a subcommand that does NOT expose the lever: the escape is out of reach.
		{"Bash(git status:*)", false},
		{"Bash(git log --oneline:*)", false},
		{"Bash(docker ps:*)", false},
		// Fully specified: nothing to smuggle in.
		{"Bash(git status)", false},
		{"Bash(find . -name x)", false},
		// Not an escapable binary, or not a Bash grant at all.
		{"Bash(ls *)", false},
		{"Bash(go build ./...)", false},
		{"Read(~/git/**)", false},
		{"WebFetch(domain:github.com)", false},
	}
	for _, c := range cases {
		t.Run(c.entry, func(t *testing.T) {
			if got := escapes(c.entry) != nil; got != c.want {
				t.Errorf("escapes(%q) = %v, want %v", c.entry, got, c.want)
			}
		})
	}
}

// TestPositionFreeIsMeasuredNotGuessed pins, per binary, whether pinning the first argument
// actually restricts the grant. Every row is an observed result, not a reading of a man page.
//
// The reachability rule this table corrects was generalised from git, where it is true: `git
// log -c core.pager=x` does not override config, it makes git parse `core.pager=x` as a
// revision and fail ("ambiguous argument"). Tools with permuting option parsing do the
// opposite, and there a pinned subcommand is a disguise, not a restriction.
//
// Method note, because it changed three of these answers: the probe payload must WRITE A
// MARKER FILE. A stdout probe reads false-positive when the marker comes back inside the
// tool's own error text (sed) or leaks from a command that then failed (tar), and
// false-negative when the lever's stdout is a transport rather than the terminal (ssh,
// rsync — ProxyCommand output goes into the protocol stream). Re-measure that way before
// changing a row.
func TestPositionFreeIsMeasuredNotGuessed(t *testing.T) {
	cases := []struct {
		grant string
		want  bool
		note  string
	}{
		// Measured position-FREE: lever accepted after the pinned argument.
		{"Bash(make test *)", true, "make test -f /tmp/evil.mk executed"},
		{"Bash(find . -name x *)", true, "find . -name x -exec <cmd> ; executed"},
		{"Bash(rsync -a src/ h:dst *)", true, "rsync -a src/ h:dst -e <cmd> executed"},
		{"Bash(ssh myhost *)", true, "ssh myhost -o ProxyCommand=<cmd> executed"},
		{"Bash(vim f.txt *)", true, "vim -es f.txt -c :!<cmd> executed"},
		{"Bash(nvim f.txt *)", true, "inferred from vim; not separately measured"},

		// Measured position-RELEVANT or lever unreachable from the open segment: the existing
		// precision rule is right for these, and loosening them would be a false positive.
		{"Bash(git status:*)", false, "git log -c k=v parses k=v as a revision and fails"},
		{"Bash(scp f.txt h:/tmp *)", false, "scp f.txt h:/tmp -o ProxyCommand=<cmd> did NOT execute"},
		{"Bash(sed -n p *)", false, "sed -n p file -e ... takes the -e as a filename"},
		{"Bash(awk {print} *)", false, "BSD awk rejects options after the program operand"},
		{"Bash(kubectl get pods *)", false, "subcommand IS the lever; trailing args cannot rewrite it"},
		{"Bash(docker ps:*)", false, "same: docker ps cannot become docker run"},

		// A wildcard is still required. With no open segment there is nowhere to put the
		// lever, so a fully specified grant stays clean even for a positionFree binary —
		// which is also the fix a user is being told to apply.
		{"Bash(make test)", false, "fully specified"},
		{"Bash(find . -name x)", false, "fully specified"},
		{"Bash(ssh myhost uptime)", false, "fully specified"},
	}
	for _, c := range cases {
		t.Run(c.grant, func(t *testing.T) {
			if got := escapes(c.grant) != nil; got != c.want {
				t.Errorf("escapes(%q) = %v, want %v\n  observed: %s", c.grant, got, c.want, c.note)
			}
		})
	}
}

// TestPositionFreeFindingSaysWhyPinningFailed: the operator has to be told that the thing
// they would naturally try — pinning the subcommand — is not the fix here. Without that the
// advice reads as something they already did, and the finding gets dismissed.
func TestPositionFreeFindingSaysWhyPinningFailed(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(make test *)"],"deny":["Read(~/.ssh/**)"]}}`)
	got, ok := ids(Audit(p))["PERM-006"]
	if !ok {
		t.Fatal("Bash(make test *) produced no PERM-006 — the pinned target is not a restriction")
	}
	for _, want := range []string{"ANYWHERE", "pinning", "make"} {
		if !strings.Contains(got.Why, want) {
			t.Errorf("PERM-006 text omits %q: %s", want, got.Why)
		}
	}
}

// TestAudit_EscapableBinary: PERM-006 is Source=permission, i.e. deterministic — it scores
// and gates like any other static finding, and it names the mechanism rather than asserting.
func TestAudit_EscapableBinary(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(git *)"],"deny":["Read(~/.ssh/**)"]}}`)
	got := ids(Audit(p))
	f, ok := got["PERM-006"]
	if !ok {
		t.Fatalf("Bash(git *) not flagged PERM-006; got %v", got)
	}
	if f.Source != model.SrcPermission || f.Dimension != 2 || f.Severity != model.SevMedium {
		t.Errorf("PERM-006 = %s/dim%d/%s, want permission/dim2/medium", f.Source, f.Dimension, f.Severity)
	}
	if !strings.Contains(f.Why, "core.pager") {
		t.Errorf("finding should name the escape mechanism, got %q", f.Why)
	}
}

// TestAudit_EscapeDoesNotShadowStrongerRules: the switch order matters — an interpreter
// wildcard stays PERM-002 and an inline secret stays PERM-001.
func TestAudit_EscapeDoesNotShadowStrongerRules(t *testing.T) {
	p := write(t, `{"permissions":{"allow":["Bash(env X=1 python3 -c '*)","Bash(*)"],"deny":["x"]}}`)
	got := ids(Audit(p))
	if _, ok := got["PERM-002"]; !ok {
		t.Error("wildcard interpreter must still report PERM-002")
	}
	if _, ok := got["PERM-005"]; !ok {
		t.Error("Bash(*) must still report PERM-005")
	}
}
