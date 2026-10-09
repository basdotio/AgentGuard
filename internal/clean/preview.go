// SPDX-License-Identifier: MIT
package clean

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// Undo's five checks answer "may this row be acted on". They do not answer the question the operator
// actually has, which is "what am I about to put back". Those came apart in a measured attack:
//
//	quarantine a harmless skill
//	replace the quarantined copy with `curl … | bash`
//	rewrite the manifest row's `hash` field to the new content's hash
//	→ clean --undo last  →  "restored dead → skills/dead"
//
// The identity check passed, because it compares the trash content against a hash read FROM THE
// MANIFEST — a file anything with write access to the config directory can edit. That is a circular
// verification, and no amount of cryptography inside the same file breaks the circle.
//
// What the attack really buys is not privilege — anything able to write .aguard-trash/ could already
// write skills/ directly — it is CONCEALMENT: the payload arrives dressed as the operator restoring
// their own file. So the cheap and effective answer is not a stronger check, it is showing the
// operator what is in the box before it goes back on the shelf. A restore is a deliberate act; it can
// afford three lines of output.
//
// Printed for dry-run too, since previewing is the whole point of dry-run.

const (
	// previewLines is how many lines of a file to show. Enough to recognise a payload's first
	// command, few enough that restoring twenty items does not scroll the reason off screen.
	previewLines = 3
	// previewWidth clips each line. Evidence elsewhere is clipped the same way.
	previewWidth = 100
	// previewEntries is how many names to list for a directory.
	previewEntries = 5
)

// preview writes a short description of what sits at path: size, content hash prefix, and either the
// first few lines (a file) or the entry names (a directory).
//
// Content is REDACTED whole before it is cut and printed (redactClip). It comes from a quarantined
// tree, which is exactly the content this tool exists to be suspicious of, and a restore preview that
// leaked a credential into a terminal or CI log would be a self-inflicted version of the leak the
// scanner reports.
func preview(w io.Writer, path string) {
	fi, err := os.Lstat(path)
	if err != nil {
		fmt.Fprintf(w, "      (cannot inspect: %v)\n", err)
		return
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		// A symlink in the trash is refused before this point; describing it rather than following it
		// keeps the preview from becoming a way to read through one.
		target, _ := os.Readlink(path)
		fmt.Fprintf(w, "      symlink → %s\n", redactClip(target))
	case fi.IsDir():
		previewDir(w, path)
		previewRisk(w, path)
	default:
		previewFile(w, path, fi.Size())
		previewRisk(w, path)
	}
}

// previewRisk runs the static engine over what is about to be restored and prints what it would find.
//
// This is the part that actually answers the operator's question. A file listing and a hash tell them
// the copy CHANGED (if they remember the old hash); running the rules tells them WHAT it changed into.
// In the measured attack the payload sat in SKILL.md's body — a listing showed `SKILL.md`, a
// three-line head showed the frontmatter, and neither showed `curl … | bash`. The rules do.
//
// It is the same pipeline `aguard check <path>` uses, so a preview cannot disagree with what the
// operator would get by auditing the quarantined copy by hand. Failures are swallowed on purpose: a
// preview that cannot run must not block a restore, because refusing to restore does not make anyone
// safer — it only guarantees the loss the manifest exists to prevent.
func previewRisk(w io.Writer, path string) {
	res, err := collect.CollectTarget(path)
	if err != nil {
		return
	}
	arts, _ := detect.New().Run(path, res.Artifacts)
	counts := map[model.Severity]int{}
	var worst []string
	for _, a := range arts {
		for _, f := range a.Findings {
			if f.Dimension == 0 || f.Source == model.SrcLLM {
				continue // coverage notes and judge output are not what a restore decision turns on
			}
			counts[f.Severity]++
			if (f.Severity == model.SevHigh || f.Severity == model.SevCritical) && len(worst) < 3 {
				worst = append(worst, f.RuleID+" "+f.Title)
			}
		}
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		fmt.Fprintln(w, "      scan of the quarantined copy: no static findings")
		return
	}
	var parts []string
	for _, sev := range []model.Severity{model.SevCritical, model.SevHigh, model.SevMedium, model.SevLow} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	fmt.Fprintf(w, "      ⚠ scan of the quarantined copy: %s\n", strings.Join(parts, ", "))
	for _, t := range worst {
		fmt.Fprintf(w, "      ⚠   %s\n", redactClip(t))
	}
}

func previewFile(w io.Writer, path string, size int64) {
	// A quarantined copy is content the operator is about to restore; opening it is the same
	// bounded read as everywhere else (safeio): a FIFO planted in the trash must not stall a restore.
	fmt.Fprintf(w, "      %s, %s\n", humanSize(size), shortHash(contentHash(path)))
	f, err := safeio.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	shown := 0
	for sc.Scan() && shown < previewLines {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fmt.Fprintf(w, "      │ %s\n", redactClip(line))
		shown++
	}
	if shown == 0 {
		fmt.Fprintln(w, "      │ (no text content)")
	}
}

func previewDir(w io.Writer, dir string) {
	var names []string
	var files, bytes int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		if fi, e := d.Info(); e == nil {
			bytes += fi.Size()
		}
		if rel, e := filepath.Rel(dir, p); e == nil {
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(names)
	fmt.Fprintf(w, "      directory, %d file(s), %s, %s\n", files, humanSize(bytes), shortHash(contentHash(dir)))
	for i, n := range names {
		if i >= previewEntries {
			fmt.Fprintf(w, "      │ … and %d more\n", len(names)-previewEntries)
			break
		}
		fmt.Fprintf(w, "      │ %s\n", redactClip(n))
	}
}

// redactClip is every preview line that quotes text: REDACT the whole value, THEN cut it to the width —
// the order of invariant #3 and the same shape as detect.redactClip (which cuts at 200, not 100).
// The other order was here first: a token that starts before byte 100 and ends after it was cut to a
// head shorter than its pattern and than the entropy pass's floor, and the head of the key reached the
// terminal. Redacted whole, what gets cut is already <REDACTED>.
func redactClip(s string) string { return clip(detect.Redact(s)) }

func clip(s string) string {
	if len(s) <= previewWidth {
		return s
	}
	return s[:previewWidth] + "…"
}

// shortHash renders a content hash for a human to compare by eye against a `aguard hash` output.
func shortHash(h string) string {
	if h == "" {
		return "hash unavailable"
	}
	if len(h) > 16 {
		return "sha256:" + h[:16] + "…"
	}
	return "sha256:" + h
}

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	}
}
