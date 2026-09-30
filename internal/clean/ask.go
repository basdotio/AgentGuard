// SPDX-License-Identifier: MIT
package clean

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/report"
)

// `clean --ask` walks the duplicate pairs and asks about each one, because a listing of twenty
// pairs asks the operator to copy twenty ids into twenty command lines and the predictable outcome
// is that none get answered.
//
// A first version of this was withdrawn, and the shape of this file is what its three mistakes
// cost:
//
//  1. NOTHING here touches the filesystem while the walk runs. Answers are COLLECTED, then
//     reconciled, then executed once. The withdrawn walk executed each answer as it arrived,
//     against a scan taken before the walk started; three similar skills produce three
//     overlapping pairs, so keeping `a` (moving `b`) was followed by an offer to resolve `(b, c)`
//     for something already in the trash, and three Enters could empty skills/ while reporting
//     success. Executing inside the loop was also the cause of the batch-id collision, the
//     one-blocked-pair-aborts-everything failure and the broken exit-code contract.
//
//  2. Input is read as WHOLE LINES and has no default: nothing is selected until a digit is typed.
//     Hand-decoding terminal escape sequences got the framing wrong — Shift+Down (ESC [ 1 ; 2 B)
//     ended as a `B` keypress, "keep both", silently writing a suppression into the baseline —
//     and a cursor that starts on a side is confirmed by any stray carriage return.
//
//  3. Everything printed here goes through report.Sanitize. Skill names are directory names chosen
//     by whoever shipped the artifact, and printing them raw let a crafted name forge the cursor
//     (invariant #7).
//
// Arrow keys are a separate layer on top (rawline.go): as a pure keys→line layer they can only
// affect how an answer is spelled, not what happens to it.

// verdict is what the operator said about one pair. There is no "default" member: a verdict only
// exists because someone typed something.
type verdict int

const (
	verdictSkip  verdict = iota // said nothing usable; leave the pair alone
	verdictKeep                 // keep the named side
	verdictBoth                 // keep both, record the id
	verdictAbort                // stop the walk; nothing collected so far is executed
)

// answer pairs a verdict with the item it answers.
type answer struct {
	item model.CleanItem
	v    verdict
	keep string
}

// askOne prints one pair and reads one line. It has no side effects beyond w.
//
// EOF is an ABORT in every position, including a partial line: "1" followed by Ctrl-D is the
// absence of an answer, not the answer "1". The withdrawn version checked only for an EMPTY read
// and so turned a half-typed answer into a move.
func askOne(w io.Writer, src lineSource, item model.CleanItem) (verdict, string) {
	fmt.Fprintf(w, "\n%s\n", report.Sanitize(item.Detail))
	for i, name := range item.Targets {
		fmt.Fprintf(w, "  [%d] keep %s\n", i+1, report.Sanitize(name))
	}
	fmt.Fprint(w, "  [b] keep both   [s] skip   [q] quit\n> ")

	line, err := src.answer(len(item.Targets))
	if err != nil {
		// Anything unterminated at EOF is discarded on purpose; see the doc comment.
		return verdictAbort, ""
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "1":
		return verdictKeep, item.Targets[0]
	case "2":
		return verdictKeep, item.Targets[1]
	case "b":
		return verdictBoth, ""
	case "q":
		return verdictAbort, ""
	case "s", "":
		return verdictSkip, ""
	}
	fmt.Fprintln(w, "  (not one of the choices — skipping this pair)")
	return verdictSkip, ""
}

// askAll collects an answer per pair. Pure with respect to the filesystem, so the whole walk is
// testable from a byte fixture.
func askAll(w io.Writer, src lineSource, pairs []model.CleanItem) (out []answer, aborted bool) {
	for _, item := range pairs {
		v, keep := askOne(w, src, item)
		if v == verdictAbort {
			return out, true
		}
		out = append(out, answer{item: item, v: v, keep: keep})
	}
	return out, false
}

// reconcile turns answers into a plan, and is where the walk's remaining hazard is handled.
//
// Pairs overlap: with skills a, b, c the check emits (a,b), (a,c) and (b,c), so one skill can be
// KEPT in one answer and DROPPED in another. Executing answers in order would honour both and take
// away something the operator explicitly protected. A keep therefore WINS over any drop of the same
// name, anywhere in the session, and the conflict is reported rather than resolved quietly — the
// operator gave two answers that cannot both hold, and needs to know which one survived.
//
// Duplicate drops (two pairs both dropping c) collapse to one move.
func reconcile(answers []answer) (drops []moveTarget, both []model.CleanItem, conflicts []string) {
	kept := map[string]bool{}
	for _, a := range answers {
		if a.v == verdictKeep {
			kept[a.keep] = true
		}
	}
	seen := map[string]bool{}
	conflicted := map[string]bool{}
	for _, a := range answers {
		switch a.v {
		case verdictBoth:
			both = append(both, a.item)
		case verdictKeep:
			t, err := chooseSide(a.item, a.keep)
			if err != nil {
				continue // structurally impossible from askOne, and silence here would be a drop
			}
			if kept[t.name] {
				conflicted[t.name] = true
				continue
			}
			if seen[t.name] {
				continue
			}
			seen[t.name] = true
			drops = append(drops, t)
		}
	}
	for name := range conflicted {
		conflicts = append(conflicts, name)
	}
	sort.Strings(conflicts) // deterministic output; the map order is not
	return drops, both, conflicts
}

// answerSource picks how answers arrive: keystrokes on a real terminal, typed lines otherwise.
//
// The choice is a COURTESY, not a safety boundary. Correctness does not rest on getting it right:
// either source yields the same small set of strings, EOF is an abort in both, and an unrecognised
// answer is a skip in both — so a run behind a pipe moves nothing whichever branch is taken. The
// withdrawn version made a terminal check load bearing, and a check that has to be right is worse
// than a design that does not need one.
func answerSource(w io.Writer, in io.Reader) (lineSource, func()) {
	f, ok := in.(*os.File)
	if !ok || !isTerminal(f) {
		fmt.Fprintln(w, "note: stdin is not a terminal, so answers are read as typed lines; end of "+
			"input stops the walk without changing anything. To decide non-interactively use "+
			"--resolve <id> --keep <name>.")
		return bufLines{bufio.NewReader(in)}, func() {}
	}
	restore, raw := makeRaw(f)
	if !raw {
		fmt.Fprintln(w, "note: this terminal could not be switched to raw mode; type your answers.")
		return bufLines{bufio.NewReader(in)}, func() {}
	}
	fmt.Fprintln(w, "Use ↑/↓ then enter, or type the number. Nothing is selected until you choose.")
	return &keyLines{r: in, w: w}, restore
}

// askablePairs are the duplicate items a choice can actually settle: two targets, an id, and
// nothing standing in the way except the choice itself.
//
// Filtering here rather than at execution is the fix for a pair with another blocker aborting the
// whole walk after earlier pairs had already moved. A question nobody can act on is not asked.
func askablePairs(items []model.CleanItem) (ask []model.CleanItem, unanswerable int) {
	for _, h := range items {
		if h.Kind != duplicateKind || h.ID == "" || len(h.Targets) != 2 {
			continue
		}
		if h.AnswerBlocker(model.BlockerSideSelection).Executable() {
			ask = append(ask, h)
			continue
		}
		unanswerable++
	}
	return ask, unanswerable
}

// ResolveInteractive asks about every answerable duplicate pair, then executes the answers once.
func ResolveInteractive(w io.Writer, in io.Reader, root string, res model.ScanResult, dryRun bool) (Result, error) {
	pairs, unanswerable := askablePairs(res.Hygiene)
	if unanswerable > 0 {
		fmt.Fprintf(w, "%d duplicate pair(s) cannot be settled by choosing a side and are not asked "+
			"about; `clean` lists them with the reason.\n", unanswerable)
	}
	if len(pairs) == 0 {
		fmt.Fprintln(w, "Nothing to ask: no duplicate pair in this scan can be settled by a choice.")
		return Result{}, nil
	}
	src, restore := answerSource(w, in)
	defer restore()

	answers, aborted := askAll(w, src, pairs)
	if aborted {
		// Everything collected so far is discarded: an abort in the middle of a set of decisions
		// means the set was never completed, and half a set was never what the operator asked for.
		fmt.Fprintf(w, "\nStopped after %d of %d pair(s). Nothing was changed.\n", len(answers), len(pairs))
		return Result{}, nil
	}

	drops, both, conflicts := reconcile(answers)
	for _, name := range conflicts {
		fmt.Fprintf(w, "keeping %s: you kept it in one pair and dropped it in another, and a keep wins.\n",
			report.Sanitize(name))
	}
	for _, item := range both {
		if err := KeepBoth(w, root, item, dryRun); err != nil {
			return Result{}, err
		}
	}
	if len(drops) == 0 {
		fmt.Fprintln(w, "No side was chosen for quarantine.")
		return Result{}, nil
	}
	// ONE call, so one batch id, one Result for the exit-code contract, and per-item refusals
	// reported without ending the run. Every safety decision is re-derived inside, exactly as it is
	// for --apply; the answers above carry no authority of their own.
	return quarantine(w, root, res, drops, nil, dryRun, "Nothing to resolve.")
}
