// SPDX-License-Identifier: MIT
package clean

import (
	"fmt"
	"io"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
)

// Resolving a duplicate pair is the first cleanup action whose subject is a CHOICE rather than a
// verdict. A zombie item says "this is unused"; a duplicate item says "these two overlap" and stops
// — the useful half, which of them survives, is not something a similarity score can answer.
//
// That shape dictates the whole design here:
//
//   - The operator names the survivor. There is no default side, no "keep the newer one", no
//     "keep the shorter description". Inventing one would silently answer the only question the
//     item was raising.
//   - One pair per invocation. --resolve takes a single ID; there is no --resolve all. The tier is
//     A2 ("a human must choose"), and a command that resolves twenty pairs in one shot is a command
//     that got past the human.
//   - The loser is QUARANTINED, not deleted, through the same engine and the same manifest rows as
//     a zombie. `clean --undo` puts it back with no knowledge that a human was involved.
//
// The interactive picker in select.go sits ON TOP of this and calls the same function per pair, so
// the arrow keys are a convenience over the primitive rather than a second implementation of it.

// duplicateKind is the item kind this file acts on. Named once: the string appears in the manifest
// row too, where `clean --undo` and any audit of the log read it back.
const duplicateKind = "duplicate_fn"

// findResolvable locates the duplicate item an operator addressed by ID.
//
// The error paths matter more than the success path. "No such item" and "that ID is a zombie, use
// --apply" are different mistakes with different fixes, and collapsing them into one message sends
// the operator looking in the wrong place.
func findResolvable(items []model.CleanItem, id string) (model.CleanItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.CleanItem{}, fmt.Errorf("--resolve needs an item ID (the D-… shown by `clean`)")
	}
	for _, h := range items {
		if h.ID == "" || !strings.EqualFold(h.ID, id) {
			continue
		}
		if h.Kind != duplicateKind {
			return model.CleanItem{}, fmt.Errorf("item %s is a %q item, not a duplicate pair; "+
				"--resolve only answers the \"which of these two survives\" question", h.ID, h.Kind)
		}
		return h, nil
	}
	return model.CleanItem{}, fmt.Errorf("no item %q in this scan — IDs are content-addressed, so "+
		"one copied from an older run stops matching once the pair changes; re-run `clean` and take a fresh ID", id)
}

// chooseSide turns the operator's --keep into the target that will be moved.
//
// Matching is EXACT — not case-folded, not prefix-matched, and NOT whitespace-trimmed. The last one
// looks like pure friendliness until the two sides are "browse" and "browse " (a directory name may
// end in a space on Unix): trimming makes --keep "browse " select the wrong one and quarantine the
// skill the operator asked to keep. A near-miss returns both names instead, because the cost of
// making someone retype a name is nothing next to the cost of moving the wrong directory.
func chooseSide(item model.CleanItem, keep string) (moveTarget, error) {
	if len(item.Targets) != 2 {
		// Structural: the check builds pairs. A future n-way group would need a different flag shape
		// (one --keep cannot express "keep these three of five"), so refuse rather than half-handle it.
		return moveTarget{}, fmt.Errorf("item %s names %d skill(s); --resolve handles pairs only",
			item.ID, len(item.Targets))
	}
	if keep == "" {
		return moveTarget{}, fmt.Errorf("--resolve %s needs --keep <name> saying which side survives: %s or %s. "+
			"There is no default: choosing IS the decision this item is asking you to make",
			item.ID, item.Targets[0], item.Targets[1])
	}
	idx := -1
	for i, name := range item.Targets {
		if name == keep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return moveTarget{}, fmt.Errorf("--keep %q names neither side of %s; it must be exactly %q or %q",
			keep, item.ID, item.Targets[0], item.Targets[1])
	}
	drop := 1 - idx
	t := moveTarget{id: item.ID, name: item.Targets[drop], kind: duplicateKind}
	if drop < len(item.Locators) {
		t.hash = item.Locators[drop].Hash
	}
	return t, nil
}

// Resolve quarantines the side of a duplicate pair the operator did not keep.
//
// The blocker is ANSWERED, not bypassed: --keep supplies exactly what BlockerSideSelection was
// asking for, so it is removed and the item is then put through Executable() like everything else.
// A pair where one side is symlink-installed outside root therefore still refuses — one gate, not a
// second path that has to be kept in step with the first.
func Resolve(w io.Writer, root string, res model.ScanResult, itemID, keep string, dryRun bool) (Result, error) {
	item, err := findResolvable(res.Hygiene, itemID)
	if err != nil {
		return Result{}, err
	}
	t, err := chooseSide(item, keep)
	if err != nil {
		return Result{}, err
	}
	answered := item.AnswerBlocker(model.BlockerSideSelection)
	if !answered.Executable() {
		return Result{}, fmt.Errorf("item %s cannot be acted on even with a side chosen: %s",
			item.ID, strings.Join(answered.Blockers, ", "))
	}
	kept := item.Targets[0]
	if kept == t.name {
		kept = item.Targets[1]
	}
	fmt.Fprintf(w, "Resolving %s: keeping %s, quarantining %s\n", item.ID, kept, t.name)
	return quarantine(w, root, res, []moveTarget{t}, nil, dryRun,
		"Nothing to resolve: the chosen side is not movable.")
}

// Item exposes the lookup for callers that need the item itself rather than a move — --keep-both
// records an id and never touches the filesystem, but must still fail loudly on an id that names
// nothing or names the wrong kind of thing.
func Item(items []model.CleanItem, id string) (model.CleanItem, error) {
	return findResolvable(items, id)
}
