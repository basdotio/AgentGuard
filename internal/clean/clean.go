// SPDX-License-Identifier: MIT
// Package clean applies cleanup actions. It is deliberately conservative: the only auto-action is
// quarantining ZOMBIE skills (which already require the --zombie opt-in to even surface), and it
// MOVES them into a reversible trash dir rather than deleting — so an over-eager cleanup is always
// recoverable. Duplicates/bloat/stale stay report-only (the user must choose which of a pair to
// drop; a description can't be auto-edited).
//
// Every mutating entry point holds an exclusive lock, records its intent before acting, and
// re-derives its own safety decisions rather than trusting anything it reads back.
package clean

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/collect"
	"github.com/basdotio/agent-guard/internal/model"
)

// Action is one applied (or would-be) cleanup step.
type Action struct {
	Skill   string
	From    string
	To      string
	Applied bool
}

// Result reports what a mutating run did. Skipped is what the operator most needs: a run that moved
// nothing because every target was blocked or changed underfoot used to be indistinguishable from a
// clean success.
type Result struct {
	Actions []Action
	Skipped int
	Batch   string
}

// protectedNames are files a restore must never write, whatever a manifest row claims. Security
// configuration is the one thing this tool promises never to weaken; see Undo.
var protectedNames = map[string]bool{
	"settings.json": true, "settings.local.json": true, ".claude.json": true, ".mcp.json": true,
}

// protected reports whether a path is security configuration or lives under hooks/.
//
// Both ends are RESOLVED first, and segments are compared case- and trailing-punctuation-insensitively.
// Matching the written path let `<root>/hk → <root>/hooks` through: the containment check resolved the
// symlink while this one did not, so `--undo` happily installed a `curl … | sh` pre-tool-use hook and
// reported `restored`, exit 0. On macOS `Hooks` and on Windows `hooks.` name the same directory too.
func protected(root, dst string) bool {
	real := resolved(dst)
	if protectedNames[strings.ToLower(strings.TrimRight(filepath.Base(real), ". "))] {
		return true
	}
	rel, err := filepath.Rel(resolved(root), real)
	if err != nil {
		return true // unrelatable to root: refuse rather than guess
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if segEqual(seg, "hooks") {
			return true
		}
	}
	return false
}

// contentHash is the canonical identity of whatever sits at path — the same hashing the collector
// and the reputation list use, so "unchanged" means one thing across the tool.
func contentHash(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return collect.TreeHash(path, path)
	}
	return collect.FileHash(path)
}

// moveTarget is one thing a run intends to move, already reduced to the four facts the write path
// needs. Both entry points build a list of these and hand it to the same engine, so a duplicate
// resolution cannot end up judged by different rules than a zombie quarantine.
type moveTarget struct{ id, name, hash, kind string }

// Apply quarantines zombie skills into <root>/.aguard-trash/. When dryRun is true it only reports
// what it WOULD do. Skills are located via the scan artifacts (name→path).
func Apply(w io.Writer, root string, res model.ScanResult, dryRun bool) (Result, error) {
	var targets []moveTarget
	blocked := map[string]string{} // name → why it cannot be acted on
	for _, h := range res.Hygiene {
		if h.Kind != "zombie" {
			continue
		}
		// Executable() is the single gate: an item must name an action AND have no blocker. Reading
		// Kind alone would happily move a symlink-installed skill whose own item says it cannot be.
		if !h.Executable() {
			// A blocked item is still something the operator ASKED to clean. Dropping it silently
			// made a run that could act on nothing look identical to one with nothing to do — the
			// symlink-installed case vanished from the output entirely.
			if h.Actionable {
				for _, name := range h.Targets {
					blocked[name] = strings.Join(h.Blockers, ", ")
				}
			}
			continue
		}
		for i, name := range h.Targets {
			hash := ""
			if i < len(h.Locators) {
				hash = h.Locators[i].Hash
			}
			targets = append(targets, moveTarget{id: h.ID, name: name, hash: hash, kind: "zombie"})
		}
	}
	return quarantine(w, root, res, targets, blocked, dryRun,
		"Nothing to apply: no zombie skills (run `clean --zombie` to detect them).")
}

// quarantine is the shared write path: validate the trash address, take the lock, and for each
// target re-derive every safety decision, record intent, rename, confirm.
//
// It is deliberately the ONLY place that moves anything. Apply and Resolve differ solely in which
// items they turn into targets — the checks below, the manifest rows they write and the wording the
// operator reads are identical, so "which command did this" can never mean "which rules applied".
func quarantine(w io.Writer, root string, res model.ScanResult, targets []moveTarget,
	blocked map[string]string, dryRun bool, nothingMsg string) (Result, error) {
	// Checked before anything else, and in dry-run too: the operator reading a plan deserves to know
	// the plan is void. If root sits inside a tree Claude Code recurses into, the trash directory
	// lands in that same tree and quarantined content keeps loading — a report saying "quarantined"
	// over a payload that still enters context every session. Measured, see collect.QuarantineUnsafe.
	if bad := collect.QuarantineUnsafe(root); bad != "" {
		return Result{}, fmt.Errorf("refusing to quarantine: --root is inside a %q directory, so %s/ "+
			"would sit in a tree Claude Code loads from and the moved content would keep loading. "+
			"Point --root at a config root (~/.claude or <project>/.claude)", bad, collect.TrashDir)
	}
	reportBlocked := func(w io.Writer) int {
		names := make([]string, 0, len(blocked))
		for n := range blocked {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(w, "skip %s: %s\n", n, blocked[n])
		}
		return len(names)
	}
	// Reported BEFORE the early return, not after. An interrupted move leaves nothing in skills/, so
	// the post-crash state has zero targets — which is precisely when the warning was suppressed. The
	// tool held from, to and hash, could restore, and instead said "nothing to apply", exit 0.
	reportPending(w, root)
	if st, cerr := verifyChain(root); cerr == nil {
		reportChain(w, st)
	}

	if len(targets) == 0 {
		n := reportBlocked(w)
		if n == 0 {
			fmt.Fprintln(w, nothingMsg)
		}
		return Result{Skipped: n}, nil
	}

	// Past the early return, so this run WILL move something (or, in dry-run, describe moving it).
	reportSessionHazard(w)

	pathByName := map[string]string{}
	for _, a := range res.Artifacts {
		if a.Kind == model.KindSkill {
			pathByName[a.Name] = a.Path
		}
	}

	// resolveTarget is shared by the dry-run and the real pass so the plan the operator approves and
	// the moves that follow cannot be judged by different rules.
	resolveTarget := func(w io.Writer, out *Result, t moveTarget) (string, bool) {
		src, ok := pathByName[t.name]
		if !ok {
			// Unreachable from the CLI today (hygiene only builds zombie items from skill artifacts),
			// but a plan read from stored JSON could name anything. Silence plus an unincremented
			// counter would report a clean run that did nothing.
			fmt.Fprintf(w, "skip %s: no scanned skill by that name\n", t.name)
			out.Skipped++
			return "", false
		}
		if _, why := quarantinable(root, src); why != "" {
			fmt.Fprintf(w, "skip %s: %s\n", t.name, why)
			out.Skipped++
			return "", false
		}
		// Re-derive identity from DISK immediately before acting. Comparing the item's locator hash
		// against the artifact's compared two copies of one value — hygiene.Analyze fills the locator
		// from that same artifact — so the branch could not fire outside a hand-built fixture while the
		// real window, scan → rename, went unchecked.
		if t.hash != "" && contentHash(src) != t.hash {
			fmt.Fprintf(w, "skip %s: content changed since it was listed; re-run `clean` and review again\n", t.name)
			out.Skipped++
			return "", false
		}
		return src, true
	}

	if dryRun {
		trash, terr := safeTrash(root, false) // validate the address without creating it
		if terr != nil {
			return Result{}, terr
		}
		out := Result{Skipped: reportBlocked(w)}
		for _, t := range targets {
			src, ok := resolveTarget(w, &out, t)
			if !ok {
				continue
			}
			dst := freeTrashPath(trash, t.name)
			fmt.Fprintf(w, "[dry-run] would quarantine %s → %s\n", t.name, dst)
			out.Actions = append(out.Actions, Action{Skill: t.name, From: src, To: dst})
		}
		return out, nil
	}

	trash, terr := safeTrash(root, true)
	if terr != nil {
		return Result{}, terr
	}
	release, err := lock(root)
	if err != nil {
		return Result{}, err
	}
	defer release()

	now := time.Now()
	out := Result{Batch: batchID(now, len(targets)), Skipped: reportBlocked(w)}

	for _, t := range targets {
		src, ok := resolveTarget(w, &out, t)
		if !ok {
			continue
		}
		dst := freeTrashPath(trash, t.name) // avoid clobbering a prior run's quarantine

		// Hash what is about to move, from disk. Undo's identity check is only as good as this value,
		// and an EMPTY hash used to disable that check silently — so a record without one is refused
		// outright rather than written.
		hash := contentHash(src)
		if hash == "" {
			fmt.Fprintf(w, "skip %s: cannot hash %s, so the move would not be verifiably reversible\n", t.name, src)
			out.Skipped++
			continue
		}
		rec := Record{
			Batch: out.Batch, Item: t.id, Kind: t.kind, State: stateIntended,
			Name: t.name, From: src, To: dst, Hash: hash,
			Unix: now.Unix(), Version: res.ToolVersion,
		}
		if err := appendRecord(root, rec); err != nil {
			return out, undoHandle(w, out, trash, fmt.Errorf("record intent: %w", err))
		}
		if err := os.Rename(src, dst); err != nil {
			// The move did not happen, so the intent row must not be left looking like a crash: a
			// failed rename used to fabricate a permanent "an earlier run was interrupted" warning
			// that grew by one line per run and told the operator to hand-restore a file that never
			// moved. A `failed` row closes the intent without claiming the move succeeded.
			rec.State = stateFailed
			_ = appendRecord(root, rec)
			fmt.Fprintf(w, "skip %s: %v\n", t.name, err)
			out.Skipped++
			continue
		}
		rec.State = stateDone
		if err := appendRecord(root, rec); err != nil {
			out.Actions = append(out.Actions, Action{Skill: t.name, From: src, To: dst, Applied: true})
			return out, undoHandle(w, out, trash, fmt.Errorf("confirm move of %s: %w", t.name, err))
		}
		out.Actions = append(out.Actions, Action{Skill: t.name, From: src, To: dst, Applied: true})
		fmt.Fprintf(w, "quarantined %s → %s\n", t.name, dst)
	}

	if len(out.Actions) > 0 {
		fmt.Fprintf(w, "\nBatch %s. Undo with:  aguard clean --undo %s\n", out.Batch, out.Batch)
		fmt.Fprintln(w, "Quarantined content is still inside the config root, so it still scores;"+
			" delete "+trash+" yourself once you are sure.")
	}
	if out.Skipped > 0 {
		fmt.Fprintf(w, "%d item(s) skipped — see the reasons above.\n", out.Skipped)
	}
	return out, nil
}

// undoHandle prints the batch id before an error aborts the run, when the run has ALREADY moved
// something.
//
// Without it a manifest write that fails part-way returned an error, main discarded the Result, and
// cobra exited 2 — which this command's own help defines as "refused, nothing changed". Directories
// had moved, and the operator was told nothing happened and given no handle to reverse it. An error
// may end a run; it may not retract what the run already did.
func undoHandle(w io.Writer, out Result, trash string, err error) error {
	if len(out.Actions) == 0 {
		return err
	}
	fmt.Fprintf(w, "\n%d item(s) WERE moved before this failed. Batch %s — undo with:  aguard clean --undo %s\n",
		len(out.Actions), out.Batch, out.Batch)
	fmt.Fprintln(w, "They are in "+trash+".")
	return err
}

// Undo restores a quarantined batch. batch may be a batch ID or "last".
//
// Every safety decision is RE-DERIVED here, never taken from the manifest, because the manifest is a
// plain file in the user's config root: anything able to write there — a malicious skill's install
// script, a hook — can append a row. Trusting `from`/`to` as written turned undo into an arbitrary
// file write: one crafted row and `clean --undo last` would drop an attacker's settings.json into
// place, complete with a permissive allowlist. Hence three independent checks, each of which alone
// is enough to refuse.
func Undo(w io.Writer, root, batch string, dryRun bool) (Result, error) {
	// The trash address is validated here too. A symlinked or escaped trash directory means the rows
	// point at content this scan cannot see, and restoring from there is not a restore.
	trash, terr := safeTrash(root, false)
	if terr != nil {
		return Result{}, terr
	}
	// Verify the chain and SAY SO — but do not refuse. Refusing to restore because the record looks
	// damaged does not make anyone safer: the file is already out of skills/ and sitting in the trash,
	// so a refusal guarantees the loss the manifest exists to prevent. Safety of the restore itself
	// comes from the five checks below, each of which re-derives its answer from the filesystem. The
	// chain's job is to tell the operator that history was lost or edited, which they cannot otherwise
	// see, and to make them read the previews that follow with suspicion.
	if st, cerr := verifyChain(root); cerr == nil {
		reportChain(w, st)
	}
	recs, err := readManifest(root)
	if err != nil {
		return Result{}, err
	}
	id, rows := restorable(recs, batch)
	if len(rows) == 0 {
		// An interrupted move is exactly the state someone reaches for undo in, and it used to answer
		// "nothing is outstanding", exit 0, while holding every field needed to put the file back.
		if p := pending(recs); len(p) > 0 {
			reportPending(w, root)
			fmt.Fprintln(w, "These were left half-moved and are NOT part of a restorable batch; "+
				"restore them by hand from the paths above.")
			return Result{Skipped: len(p)}, nil
		}
		fmt.Fprintln(w, "Nothing to undo: no quarantine batch is outstanding.")
		return Result{}, nil
	}

	// Same scoping as Apply: only once there is something to restore. This is the worse direction —
	// a running session GAINS a skill, and the operator thinks they restored a file of their own.
	reportSessionHazard(w)

	if !dryRun {
		release, lerr := lock(root)
		if lerr != nil {
			return Result{}, lerr
		}
		defer release()
	}

	out := Result{Batch: id}
	for _, r := range rows {
		switch {
		// 1. Containment. Both ends must stay inside the root this run was pointed at.
		case !withinDir(root, r.To) || !withinDirAllowMissing(root, r.From):
			fmt.Fprintf(w, "refuse %s: manifest names a path outside root\n", r.Name)
			out.Skipped++
			continue
		// 1b. The SOURCE of a restore must be inside the trash. Checking only "inside root" made undo a
		// general move primitive: one appended row with `to: <root>/settings.json` moved the operator's
		// deny-list away and planted it at the row's `from` — reported as `restored`, exit 0 — and the
		// same row shape moved the whole hooks/ tree. A restore is only ever "take something out of the
		// trash"; a symlink in the trash pointing elsewhere is not either.
		case !withinTrash(trash, r.To) || isSymlink(r.To):
			fmt.Fprintf(w, "refuse %s: manifest names a source outside the quarantine directory\n", r.Name)
			out.Skipped++
			continue
		// 2. Exclusion. Security configuration is never a restore destination — nor a restore SOURCE,
		// since moving it away is the same loss as overwriting it.
		case protected(root, r.From) || protected(root, r.To):
			fmt.Fprintf(w, "refuse %s: restoring security configuration is not permitted\n", r.Name)
			out.Skipped++
			continue
		// 3a. Identity is MANDATORY. A row with no hash used to skip the identity check entirely, so an
		// attacker simply omitted the field. No hash, no restore.
		case r.Hash == "":
			fmt.Fprintf(w, "refuse %s: manifest row carries no content hash, so the restore is not verifiable\n", r.Name)
			out.Skipped++
			continue
		}
		if _, err := os.Lstat(r.From); err == nil {
			fmt.Fprintf(w, "skip %s: %s already exists\n", r.Name, r.From)
			out.Skipped++
			continue
		}
		if _, serr := os.Stat(r.To); serr != nil {
			fmt.Fprintf(w, "skip %s: quarantined copy is gone (%v)\n", r.Name, serr)
			out.Skipped++
			continue
		}
		// 3b. What sits in the trash now must be what was put there — otherwise this is a swap:
		// quarantine the malicious skill, replace the quarantined copy, wait for the operator to change
		// their mind, and the restore installs the replacement.
		if contentHash(r.To) != r.Hash {
			fmt.Fprintf(w, "refuse %s: quarantined content changed since it was moved\n", r.Name)
			out.Skipped++
			continue
		}
		if dryRun {
			fmt.Fprintf(w, "[dry-run] would restore %s → %s\n", r.Name, r.From)
			preview(w, r.To)
			out.Actions = append(out.Actions, Action{Skill: r.Name, From: r.To, To: r.From})
			continue
		}
		// BEFORE the move, not after: the five checks say the row may be acted on, they do not say what
		// is in it. Showing the content is what turns "an attacker replaced the quarantined copy" from
		// an invisible substitution into something the operator reads on the way past. See preview.go.
		fmt.Fprintf(w, "restoring %s ← %s\n", r.Name, r.To)
		preview(w, r.To)
		if err := os.MkdirAll(filepath.Dir(r.From), 0o755); err != nil {
			fmt.Fprintf(w, "skip %s: %v\n", r.Name, err)
			out.Skipped++
			continue
		}
		if err := os.Rename(r.To, r.From); err != nil {
			fmt.Fprintf(w, "skip %s: %v\n", r.Name, err)
			out.Skipped++
			continue
		}
		undone := r
		undone.State = stateUndone
		undone.Unix = time.Now().Unix()
		if err := appendRecord(root, undone); err != nil {
			return out, fmt.Errorf("record undo of %s: %w", r.Name, err)
		}
		out.Actions = append(out.Actions, Action{Skill: r.Name, From: r.To, To: r.From, Applied: true})
		fmt.Fprintf(w, "restored %s → %s\n", r.Name, r.From)
	}
	if out.Skipped > 0 {
		fmt.Fprintf(w, "%d item(s) skipped — see the reasons above.\n", out.Skipped)
	}
	return out, nil
}

// reportChain tells the operator what verification concluded. Silent when the chain holds and every
// row is chained, because a line printed on every run is a line nobody reads.
//
// A break is stated in terms of what it MEANS rather than what it is: "line 4 does not match line 3"
// is a fact about a file format, "rows may have been deleted or edited" is the thing the operator has
// to act on.
func reportChain(w io.Writer, st ChainStatus) {
	if st.Corrupt > 0 {
		fmt.Fprintf(w, "warning: the quarantine record has %d unreadable line(s); history before line %d "+
			"is verified, after it is not.\n", st.Corrupt, st.BrokenAt)
		return
	}
	if !st.OK() {
		fmt.Fprintf(w, "warning: the quarantine record's hash chain breaks at line %d — rows may have been "+
			"deleted or edited since they were written.\n"+
			"  Restores are still permitted (the file is already out of place, so refusing would only "+
			"guarantee the loss), but every safety check is re-derived from disk and each restore is "+
			"previewed below. Read those previews.\n", st.BrokenAt)
		return
	}
	if st.Unchained > 0 {
		fmt.Fprintf(w, "note: %d quarantine record row(s) predate the hash chain and cannot be verified "+
			"(written by an older aguard). Newer rows are chained.\n", st.Unchained)
	}
}

// reportSessionHazard states the one concurrency risk the lock does NOT cover.
//
// lock() makes two aguard runs mutually exclusive. It says nothing about Claude Code, which was
// MEASURED to watch skills/, commands/ and agents/ live (docs/internals/measurement-startup-loading.md) — so a
// session open right now sees this run's moves at once. The dangerous direction is undo: apply takes
// a capability away from a running agent, undo hands one back, and the operator believes they only
// restored a file of their own.
//
// This is a caution, not a check, and it is worded that way on purpose. There is no signal here that
// can be trusted: transcript mtimes would answer "a session wrote recently", which is not the same
// question, and whether Claude Code flushes them continuously or at session end has not been
// measured. Guessing would produce a warning that fires at the wrong moments, which is worse than
// one that fires every time.
//
// SCOPING is the whole reason this is worth printing at all. It appears only on a run that is about
// to MOVE something — never on plain `clean`, which is the command people actually run, and never on
// a run with nothing to do. A caution shown at the moment of a rare, deliberate act is read; the same
// text on every invocation is wallpaper, and this report has just finished having a permanent
// zero removed from it for exactly that reason.
func reportSessionHazard(w io.Writer) {
	fmt.Fprintln(w, "note: Claude Code watches skills/, commands/ and agents/ live, so a session open "+
		"right now sees this\n      immediately. The lock here only keeps two aguard runs apart — it "+
		"cannot see a session.")
}

// reportPending surfaces moves recorded but never confirmed — a process killed between the rename
// and its confirmation. Silence would recreate the exact hole the manifest closes: the file is in
// the trash and nothing knows how to put it back.
func reportPending(w io.Writer, root string) {
	recs, err := readManifest(root)
	if err != nil {
		return
	}
	for _, r := range pending(recs) {
		fmt.Fprintf(w, "warning: an earlier run was interrupted while moving %s.\n"+
			"  It may be at %s; restore it by hand if %s is missing.\n", r.Name, r.To, r.From)
	}
}

// freeTrashPath returns trash/<name>, or trash/<name>.N for the first N whose path is free, so a
// second quarantine of a same-named skill never clobbers the first (M-4). The name is flattened to a
// single path element first: a skill named `synced/websearch` used to produce a path whose parent
// nobody created, and every apply failed with ENOENT after already writing its intent row.
func freeTrashPath(trash, name string) string {
	base := filepath.Join(trash, trashName(name))
	if _, err := os.Lstat(base); os.IsNotExist(err) {
		return base
	}
	for i := 1; ; i++ {
		p := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// withinDir reports whether path stays under base after resolving symlinks (a move never escapes
// root). Resolving both sides avoids false refusals when base is itself under a symlinked prefix
// (e.g. macOS /tmp → /private/tmp) — the artifact path is already symlink-resolved, so base must be.
func withinDir(base, path string) bool {
	if rb, err := filepath.EvalSymlinks(base); err == nil {
		base = rb
	}
	if rp, err := filepath.EvalSymlinks(path); err == nil {
		path = rp
	}
	return relWithin(base, path)
}

// withinDirAllowMissing is withinDir for a path that does not exist yet — a restore destination.
// EvalSymlinks fails on a missing path, and treating that failure as "outside root" would refuse
// every legitimate restore, so the nearest existing ancestor is resolved instead.
func withinDirAllowMissing(base, path string) bool {
	if rb, err := filepath.EvalSymlinks(base); err == nil {
		base = rb
	}
	dir, rest := filepath.Dir(path), filepath.Base(path)
	for {
		if rd, err := filepath.EvalSymlinks(dir); err == nil {
			return relWithin(base, filepath.Join(rd, rest))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

func relWithin(base, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
