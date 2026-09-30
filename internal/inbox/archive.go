// SPDX-License-Identifier: MIT
package inbox

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// Archive limits. A downloaded zip is attacker-authored bytes: the index can lie about sizes,
// entries can be nested bombs, names can point outside the destination. Every limit here is
// enforced by what is actually read or written, not by what the index claims.
const (
	MaxArchiveEntries    = 2000
	MaxArchiveFileBytes  = 1 << 20  // 1 MiB per entry — the same cap the scanner puts on any file
	MaxArchiveTotalBytes = 64 << 20 // 64 MiB extracted in all
)

// IsZip reports whether the name has a .zip extension.
func IsZip(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".zip") }

// PeekZip reads only the archive's index and reports whether any entry name is an agent marker.
// No entry is decompressed here.
func PeekZip(path string) (bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer r.Close()
	for i, f := range r.File {
		if i >= MaxArchiveEntries {
			break
		}
		if MarkerName(f.Name) {
			return true, nil
		}
	}
	return false, nil
}

// ExtractZip unpacks a zip into a fresh private temporary directory and returns it with a cleanup
// function the caller must run. Refused entries are counted and disclosed in notes; nothing is
// made executable; the destination is 0700 and every file 0600.
func ExtractZip(path string) (dir string, notes []model.Finding, cleanup func(), err error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, func() {}, err
	}
	defer r.Close()
	dir, err = os.MkdirTemp("", "aguard-inbox-")
	if err != nil {
		return "", nil, func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	var total int64
	unsafe, special, oversize := 0, 0, 0
	stopped := ""
	for i, f := range r.File {
		if i >= MaxArchiveEntries {
			stopped = fmt.Sprintf("more than %d entries; the rest were not extracted", MaxArchiveEntries)
			break
		}
		// Any ".." segment is refused outright, even one that would clean to a path inside the
		// destination: no archiver writes such names, and cleaning them is where extractors
		// historically went wrong.
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || name == ".." || strings.Contains(name, "\x00") {
			unsafe++
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || strings.HasPrefix(clean, "..") {
			unsafe++
			continue
		}
		dst := filepath.Join(dir, clean)
		mode := f.Mode()
		if mode.IsDir() || strings.HasSuffix(name, "/") {
			if e := os.MkdirAll(dst, 0o700); e != nil {
				cleanup()
				return "", nil, func() {}, e
			}
			continue
		}
		if !mode.IsRegular() {
			special++ // symlinks, devices, sockets — never recreated
			continue
		}
		if f.UncompressedSize64 > MaxArchiveFileBytes {
			oversize++
			continue
		}
		if total+int64(f.UncompressedSize64) > MaxArchiveTotalBytes {
			stopped = fmt.Sprintf("more than %d MiB in all; the rest were not extracted", MaxArchiveTotalBytes>>20)
			break
		}
		if e := os.MkdirAll(filepath.Dir(dst), 0o700); e != nil {
			cleanup()
			return "", nil, func() {}, e
		}
		n, e := writeCapped(dst, f)
		total += n
		if e != nil {
			if errors.Is(e, errOversize) {
				oversize++ // the index lied about the size; the partial file is removed
				_ = os.Remove(dst)
				continue
			}
			cleanup()
			return "", nil, func() {}, e
		}
	}
	base := filepath.Base(path)
	if unsafe+special+oversize > 0 {
		notes = append(notes, note("Archive entries not extracted",
			fmt.Sprintf("%s: %d entr%s refused — %d with paths that escape the archive, %d symlinks or special files, %d over %d MiB. What was not extracted was not checked.",
				base, unsafe+special+oversize, plural(unsafe+special+oversize), unsafe, special, oversize, MaxArchiveFileBytes>>20), path))
	}
	if stopped != "" {
		notes = append(notes, note("Archive extraction stopped at its cap", base+": "+stopped, path))
	}
	return dir, notes, cleanup, nil
}

var errOversize = errors.New("entry larger than its declared size")

// writeCapped copies one entry to dst, refusing to write past the per-file cap even when the
// index under-reports the size (the classic zip-bomb shape).
func writeCapped(dst string, f *zip.File) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(rc, MaxArchiveFileBytes+1))
	if err != nil {
		return n, err
	}
	if n > MaxArchiveFileBytes {
		return n, errOversize
	}
	return n, nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
