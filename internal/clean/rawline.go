// SPDX-License-Identifier: MIT
package clean

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

// Arrow keys, added as a layer that can only change HOW AN ANSWER IS SPELLED.
//
// The withdrawn picker put terminal decoding underneath everything: a misframed escape
// sequence became a "keep both", and the redraw arithmetic ran over attacker-chosen skill names.
// The lesson was not "escape parsing is hard" but "escape parsing must not be load bearing". So the
// only thing this file does is turn keystrokes into one of the strings a person could have typed —
// "1", "2", "b", "s", "q", or "" — and hand it to the same askOne that reads a typed line. Nothing
// downstream can tell which one produced the answer.
//
// Three properties follow structurally rather than by care:
//
//   - NO DEFAULT SIDE. The cursor starts on nothing. Enter before any arrow key yields "", which
//     askOne treats as a skip, exactly as a blank typed line would.
//   - NOTHING ATTACKER-INFLUENCED IS DRAWN HERE. askOne has already printed the menu (through
//     report.Sanitize); the status line this file rewrites shows a NUMBER and nothing else, so
//     there is no name to forge a cursor with and no multi-line rewind to desynchronise.
//   - A SEQUENCE IS CONSUMED WHOLE. The old code swallowed a fixed three bytes, which left the tail
//     of a six-byte Shift+Down (ESC [ 1 ; 2 B) to be read as the "keep both" key. A CSI sequence
//     ends at its final byte, wherever that is, and an unrecognised one yields nothing at all.

// lineSource yields one answer per prompt. Two implementations: a typed line, and keystrokes.
type lineSource interface {
	// answer returns what the operator said, spelled the way they would have typed it. options is
	// how many sides the prompt offers, so a cursor cannot run past them.
	answer(options int) (string, error)
}

// bufLines is the typed-line source: the answer is whatever was on the line.
type bufLines struct{ r *bufio.Reader }

func (b bufLines) answer(int) (string, error) { return b.r.ReadString('\n') }

// key is a decoded keystroke.
type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	keyAbort
	keyLiteral // a character that stands for itself ("1", "b", …)
)

// keyLines is the keystroke source.
type keyLines struct {
	r io.Reader
	w io.Writer
	b []byte // one-byte scratch, so a sequence is never over-read
}

// readByte pulls exactly one byte. Reading one at a time is deliberate: nothing can be buffered
// past the end of an answer, which is how the withdrawn version silently dropped type-ahead.
func (k *keyLines) readByte() (byte, error) {
	if k.b == nil {
		k.b = make([]byte, 1)
	}
	n, err := k.r.Read(k.b)
	if n == 1 {
		return k.b[0], nil
	}
	if err != nil {
		return 0, err
	}
	// n == 0 with no error is legal for an io.Reader and must not become a spin: treat a
	// reader that yields nothing as exhausted rather than polling it forever. (This was a
	// for-loop whose every path returned — the shape of a retry that was then decided
	// against; the dead skeleton read as if it might spin, and staticcheck agreed.)
	return 0, io.EOF
}

// readKey decodes one keystroke, consuming a whole escape sequence when it sees one.
func (k *keyLines) readKey() (key, byte, error) {
	c, err := k.readByte()
	if err != nil {
		return keyAbort, 0, err
	}
	switch c {
	case 0x03, 0x04: // Ctrl-C, Ctrl-D
		return keyAbort, 0, nil
	case '\r', '\n':
		return keyEnter, 0, nil
	case 0x1b:
		return k.readEscape()
	}
	return keyLiteral, c, nil
}

// readEscape consumes the rest of an escape sequence and reports what it meant, if anything.
//
// Two cursor encodings exist and terminals switch between them: CSI (ESC [ … A/B) in the normal
// mode and SS3 (ESC O A/B) once an application enables the alternate keypad. Handling only the
// first is how arrow keys work until they suddenly do not.
func (k *keyLines) readEscape() (key, byte, error) {
	c, err := k.readByte()
	if err != nil {
		return keyAbort, 0, err
	}
	switch c {
	case 'O': // SS3: exactly one more byte
		f, ferr := k.readByte()
		if ferr != nil {
			return keyAbort, 0, ferr
		}
		return cursorKey(f), 0, nil
	case '[': // CSI: parameters and intermediates, then a final byte in 0x40..0x7e
		for {
			f, ferr := k.readByte()
			if ferr != nil {
				return keyAbort, 0, ferr
			}
			if f >= 0x40 && f <= 0x7e {
				return cursorKey(f), 0, nil
			}
			// Still inside the sequence. Anything that is not a legal parameter or intermediate
			// byte means this is not a sequence we understand; stop rather than scan forever, and
			// report nothing — a half-understood sequence must never become a keypress.
			if f < 0x20 || f > 0x3f {
				return keyNone, 0, nil
			}
		}
	}
	return keyNone, 0, nil // ESC followed by anything else: ignored entirely
}

// cursorKey maps a sequence's final byte to a movement, or to nothing.
func cursorKey(final byte) key {
	switch final {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	}
	return keyNone
}

// status rewrites the single line showing the current selection. One line, one number: no cursor
// arithmetic and no attacker-influenced text.
func (k *keyLines) status(sel int) {
	shown := " "
	if sel >= 0 {
		shown = strconv.Itoa(sel + 1)
	}
	fmt.Fprintf(k.w, "\r\x1b[K> %s", shown)
}

// answer collects one keystroke answer.
func (k *keyLines) answer(options int) (string, error) {
	sel := -1 // nothing is selected; there is no default
	k.status(sel)
	for {
		kk, lit, err := k.readKey()
		if err != nil {
			fmt.Fprintln(k.w)
			return "", err
		}
		switch kk {
		case keyAbort:
			fmt.Fprintln(k.w)
			return "q\n", nil
		case keyUp:
			if sel < 0 {
				sel = options - 1
			} else if sel > 0 {
				sel--
			}
		case keyDown:
			if sel < 0 {
				sel = 0
			} else if sel < options-1 {
				sel++
			}
		case keyEnter:
			fmt.Fprintln(k.w)
			if sel < 0 {
				return "\n", nil // no selection: the same as a blank typed line, i.e. a skip
			}
			return strconv.Itoa(sel+1) + "\n", nil
		case keyLiteral:
			// Typing still works in raw mode. A digit or a menu letter answers immediately, which
			// keeps the keyboard-only path identical to the typed one.
			fmt.Fprintf(k.w, "%c\n", lit)
			return string(lit) + "\n", nil
		case keyNone:
			continue
		}
		k.status(sel)
	}
}
