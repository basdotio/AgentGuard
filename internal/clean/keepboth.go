// SPDX-License-Identifier: MIT
package clean

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// "Keep both" is a real answer, not an evasion. Two skills with identical descriptions are
// sometimes deliberate — an alias entry point, a stable name kept alongside a renamed one — and a
// tool that only offers "drop one of these" turns a correct setup into a permanent nag. A nag the
// operator learns to scroll past is worse than no check.
//
// It is also the only cleanup action that WRITES CONFIGURATION rather than moving a directory, so
// it gets its own file and its own boundary:
//
//   - The destination is computed from root, never taken from item data.
//   - A symlinked .aguardignore is refused, on the same reasoning as the trash directory and the
//     manifest: following it would write somewhere the operator never named.
//   - Append only. The file is the operator's; this never rewrites or reorders what is there.
//   - NOTHING attacker-controlled is written verbatim. See ignoreComment.

// ignoreFile is the baseline's fixed name. Also spelled in cmd/aguard; kept here because this is
// the only place that WRITES it.
const ignoreFile = ".aguardignore"

// commentSafe keeps the characters a skill name legitimately uses and drops everything else.
//
// This is the load-bearing line of the file. Skill names are DIRECTORY names, chosen by whoever
// authored or installed the artifact, and on Unix a directory name may contain a newline. Writing
// one into a comment would end the comment and start a new baseline line — and a name of
//
//	evil\n*  **
//
// appends a rule suppressing every finding under every path, from a command whose entire purpose
// the operator understood as "stop asking me about these two skills". The baseline is the one file
// where an injected line silently disables the tool, so nothing reaches it unfiltered.
var commentSafe = regexp.MustCompile(`[^A-Za-z0-9._/@+-]+`)

// ignoreComment renders the pair as a trailing comment: sanitised, length-capped, and never able to
// introduce a line break.
func ignoreComment(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		s := commentSafe.ReplaceAllString(n, "_")
		if len(s) > 64 {
			s = s[:64] + "…"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// KeepBoth records that the operator has accepted a cleanup item as-is, by appending its ID to
// <root>/.aguardignore. The item stops being listed until its targets change, at which point the
// content-addressed ID changes too and it comes back.
func KeepBoth(w io.Writer, root string, item model.CleanItem, dryRun bool) error {
	if item.ID == "" {
		return fmt.Errorf("this item addresses nothing and cannot be accepted into a baseline")
	}
	path := filepath.Join(anchored(root), ignoreFile)
	if isSymlink(path) {
		return fmt.Errorf("refusing to write %s: it is a symlink, so the write would land somewhere "+
			"other than the root you named", path)
	}
	line := fmt.Sprintf("%s  # keep both: %s\n", item.ID, ignoreComment(item.Targets))
	if dryRun {
		fmt.Fprintf(w, "[dry-run] would append to %s: %s", path, line)
		return nil
	}
	// A baseline whose last line has no terminating newline is ordinary — an editor without a final
	// newline, an earlier `echo -n`. Appending blind fused the two: "EXEC-001" + "D-1a2b3c4d …"
	// became one token, which destroyed the operator's existing suppression AND failed to record the
	// id (the fused token does not parse as one), while stdout reported success both ways.
	needsNL := false
	if fi, serr := os.Stat(path); serr == nil && fi.Size() > 0 {
		f, oerr := safeio.Open(path)
		if oerr != nil {
			return fmt.Errorf("read %s: %w", path, oerr)
		}
		last := make([]byte, 1)
		_, rerr := f.ReadAt(last, fi.Size()-1)
		_ = f.Close()
		if rerr != nil {
			return fmt.Errorf("read %s: %w", path, rerr)
		}
		needsNL = last[0] != '\n'
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("append to %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if needsNL {
		line = "\n" + line
	}
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("append to %s: %w", path, err)
	}
	fmt.Fprintf(w, "kept both; %s will not be listed again until either side changes (recorded in %s)\n",
		item.ID, path)
	return nil
}
