// SPDX-License-Identifier: MIT
package clean

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func keySrc(in string) (*keyLines, *bytes.Buffer) {
	var buf bytes.Buffer
	return &keyLines{r: strings.NewReader(in), w: &buf}, &buf
}

// A whole sequence is consumed as a unit. This is THE defect that sank the first picker: the withdrawn
// version swallowed a fixed three bytes, so the tail of Shift+Down (ESC [ 1 ; 2 B) was re-read as
// the "keep both" key and wrote a suppression into the baseline from one cursor keypress.
//
// A modified arrow acting as its base arrow is fine and is what a terminal user expects; what must
// be impossible is any part of a sequence surfacing as a LITERAL. So the assertion is not "it does
// nothing" but "it never answers" — movement only, and only enter commits.
func TestKeyLines_ModifiedArrowsMoveButNeverAnswer(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"shift+down", "\x1b[1;2B\r", "1\n"},
		{"ctrl+down", "\x1b[1;5B\r", "1\n"},
		{"alt+up", "\x1b[1;3A\r", "2\n"},
	}
	for _, c := range cases {
		k, _ := keySrc(c.in)
		got, err := k.answer(2)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: expected a plain move then commit (%q), got %q", c.name, c.want, got)
		}
	}
	// The same sequences WITHOUT a following enter must produce no answer at all — proof that the
	// trailing bytes never became a keypress of their own.
	for _, in := range []string{"\x1b[1;2B", "\x1b[1;5B", "\x1b[1;3A"} {
		k, _ := keySrc(in)
		if got, err := k.answer(2); err != io.EOF {
			t.Errorf("%q alone must not answer anything; got (%q,%v)", in, got, err)
		}
	}
}

// A sequence whose final byte is not a cursor key means nothing, and must not leave a selection.
func TestKeyLines_UnknownSequencesSelectNothing(t *testing.T) {
	for _, in := range []string{"\x1b[H\r", "\x1b[15~\r", "\x1bZ\r", "\x1b[?1049h\r"} {
		k, _ := keySrc(in)
		got, err := k.answer(2)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != "\n" {
			t.Errorf("%q must leave nothing selected; got %q", in, got)
		}
	}
}

// Plain cursor keys still work, in both encodings a terminal may use. Handling only CSI is how
// arrow keys work until an application enables the alternate keypad and they stop.
func TestKeyLines_BothCursorEncodings(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"csi down", "\x1b[B\r", "1\n"},
		{"ss3 down", "\x1bOB\r", "1\n"},
		{"csi up (wraps to last)", "\x1b[A\r", "2\n"},
		{"ss3 up", "\x1bOA\r", "2\n"},
		{"down twice clamps", "\x1b[B\x1b[B\x1b[B\r", "2\n"},
		{"down then up", "\x1b[B\x1b[A\r", "1\n"},
	}
	for _, c := range cases {
		k, _ := keySrc(c.in)
		got, _ := k.answer(2)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// No default side, structurally: the cursor starts on nothing, so enter before any arrow key is a
// blank answer — the same thing a blank typed line means, which askOne reads as a skip.
func TestKeyLines_EnterBeforeAnyArrowSelectsNothing(t *testing.T) {
	k, _ := keySrc("\r")
	got, err := k.answer(2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "\n" {
		t.Errorf("enter with nothing selected must not choose a side; got %q", got)
	}
}

// Everything that means "stop" ends up as an abort or a quit, never as a selection.
func TestKeyLines_AbortKeys(t *testing.T) {
	for _, in := range []string{"\x03", "\x04"} {
		k, _ := keySrc(in)
		got, err := k.answer(2)
		if err != nil || got != "q\n" {
			t.Errorf("%q must quit, got (%q,%v)", in, got, err)
		}
	}
	k, _ := keySrc("") // EOF straight away
	if _, err := k.answer(2); err != io.EOF {
		t.Errorf("running out of input must surface as EOF, got %v", err)
	}
	k, _ = keySrc("\x1b[B") // arrow, then input ends before enter
	if _, err := k.answer(2); err != io.EOF {
		t.Errorf("input ending mid-answer must surface as EOF, got %v", err)
	}
}

// Typing still answers in raw mode, so the keyboard-only path is the same one the docs describe.
func TestKeyLines_LiteralsAnswerImmediately(t *testing.T) {
	for _, c := range []struct{ in, want string }{{"1", "1\n"}, {"2", "2\n"}, {"b", "b\n"}, {"s", "s\n"}, {"q", "q\n"}} {
		k, _ := keySrc(c.in)
		got, _ := k.answer(2)
		if got != c.want {
			t.Errorf("typing %q should answer %q, got %q", c.in, c.want, got)
		}
	}
}

// A reader that returns (0, nil) is legal and must not become a spin. The withdrawn picker looped
// on it at 100% CPU with the terminal left raw.
func TestKeyLines_ZeroLengthReadDoesNotSpin(t *testing.T) {
	k, _ := keySrc("")
	k.r = zeroReader{}
	done := make(chan struct{})
	go func() { _, _ = k.answer(2); close(done) }()
	select {
	case <-done:
	case <-timeAfter():
		t.Fatal("answer() never returned on a reader that yields nothing")
	}
}

type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

// Only a number ever reaches the status line, so there is no attacker-influenced text to draw and
// no multi-line rewind to desynchronise — the mechanism behind the withdrawn cursor forgery.
func TestKeyLines_StatusLineCarriesNoNames(t *testing.T) {
	k, out := keySrc("\x1b[B\r")
	if _, err := k.answer(2); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"keep", "skill", "browse"} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("the status line must show a number only; got %q", out.String())
		}
	}
}

func timeAfter() <-chan time.Time { return time.After(2 * time.Second) }
