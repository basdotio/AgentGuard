// SPDX-License-Identifier: MIT

// Package safeio is the one way this program opens a file it did not write: configuration,
// manifests, approvals, instruction text. It exists because the same bug was fixed three
// times on three paths and found on a fourth.
//
// The invariant it carries: reading a file the scanned party controls must be BOUNDED in two
// ways, and both bounds must be enforced by the read itself.
//
//   - Non-regular files are refused before Open. os.Open on a FIFO blocks until a writer
//     appears; a character device never reaches EOF; a socket errors only after opening. All
//     three are things an artifact author can put in a tree, and `tar` will faithfully carry a
//     FIFO into ~/Downloads without anyone being hostile. The scan then does not fail — it
//     HANGS, and a hung scan reports nothing. The load-time gate hangs with it, past the
//     editor's hook timeout, after which the skill loads unaudited in silence.
//   - Size is capped by the read, never by a Stat the read does not consult. os.ReadFile
//     re-Stats after opening and sizes its buffer from that, then appends to EOF: a 2 GiB
//     settings.json cost 3.19 GB of RSS and exited 0 without mentioning the file. Reading
//     through io.LimitReader(cap+1) makes the over-cap case a one-byte proof, and makes the
//     "file grows between two Stats" race unreachable by construction rather than tested by
//     luck.
//
// Stat, not Lstat: a symlink to a regular file is a supported install layout (invariant #2 is
// enforced by the callers' boundary checks, not here). Errors from Stat and Open are returned
// unwrapped so errors.Is(err, fs.ErrNotExist) keeps working at every call site; the two
// refusals this package adds are ErrNotRegular and ErrTooLarge.
package safeio

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// MaxConfigBytes is the cap for configuration and manifest files (settings.json, .mcp.json,
// installed_plugins.json, hooks.json, approvals, desktop manifests). The largest real one
// seen is well under 1 MiB; 8 MiB leaves room without letting the scanned party choose the
// scanner's memory footprint.
const MaxConfigBytes = 8 << 20

// ErrNotRegular is returned for a path that exists but is not a regular file (FIFO, socket,
// device, directory). The file was NOT opened.
var ErrNotRegular = errors.New("not a regular file")

// ErrTooLarge is returned when the file holds more than the cap. The bytes read are discarded:
// a caller that wants a prefix uses ReadPrefix.
var ErrTooLarge = errors.New("file exceeds the read cap")

// Stat returns the FileInfo for a regular file, or ErrNotRegular (wrapped with the path and
// mode) for anything else that exists. Stat errors pass through unchanged.
func Stat(path string) (fs.FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w (mode %s)", path, ErrNotRegular, fi.Mode())
	}
	return fi, nil
}

// Open opens a regular file for streaming readers that need a *os.File (seeking, line
// scanning). It refuses non-regular files before the Open that would block.
func Open(path string) (*os.File, error) {
	if _, err := Stat(path); err != nil {
		return nil, err
	}
	return os.Open(path)
}

// ReadFile reads a regular file of at most capBytes. Over the cap it returns ErrTooLarge and
// no bytes — strict, for files that must be read whole to mean anything (JSON, YAML).
func ReadFile(path string, capBytes int64) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, capBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > capBytes {
		return nil, fmt.Errorf("%s: %w (%d bytes)", path, ErrTooLarge, capBytes)
	}
	return b, nil
}

// ReadPrefix reads at most n bytes of a regular file and returns them even when the file is
// larger — for text whose interesting part is at the top (frontmatter, @imports) or that a
// caller excerpts anyway. Same non-regular refusal as ReadFile.
func ReadPrefix(path string, n int64) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, n))
}
