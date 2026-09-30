// SPDX-License-Identifier: MIT
package hygiene

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basdotio/AgentGuard/internal/model"
)

// idHexLen is the starting length of an item ID's hex body, grown on collision.
//
// Not shorter, for two reasons that compound. A 16-bit body collides at roughly 2% once a listing
// holds fifty items — unacceptable when the ID names a destructive action. And the hash input
// contains DIRECTORY NAMES chosen by whoever wrote the artifact, with an algorithm anyone can read
// here, so a short body is not merely unlucky but forgeable: an attacker who wants their skill to
// share an ID with something the operator means to remove only has to try names until one matches.
const idHexLen = 8

// idPrefixes give an ID a readable kind marker, so a copied command line is recognisable
// ("Z-…" is a zombie) without being parseable — the body is what identifies the target.
var idPrefixes = map[string]string{
	"zombie":        "Z",
	"duplicate_fn":  "D",
	"context_bloat": "B",
	"stale_ref":     "S",
	"mcp_unused":    "M",
}

func idPrefix(kind string) string {
	if p, ok := idPrefixes[kind]; ok {
		return p
	}
	return "X"
}

// itemDigest hashes an item's IDENTITY: its kind plus the set of targets it addresses.
//
// Locator keys are SORTED before hashing, which is what makes a duplicate pair (a,b) and (b,a) the
// same item rather than two. Display names and content hashes are excluded on purpose: a name is
// attacker-chosen and cosmetic, and folding content in would renumber the whole listing every time
// the operator edited a file — the ID answers "which target", never "which version of it".
func itemDigest(it model.CleanItem) string {
	keys := make([]string, 0, len(it.Locators))
	for _, l := range it.Locators {
		keys = append(keys, l.Key())
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(it.Kind + "\x00" + strings.Join(keys, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// assignIDs stamps every locator-bearing item with an ID that is stable across runs for the same
// targets and unique within this listing.
//
// Two properties are worth stating because they are the ones a caller depends on. Stability: the ID
// derives only from kind and target paths, so adding an unrelated skill to the environment cannot
// renumber existing items — the operator can read a listing, walk away, and still have valid IDs.
// Uniqueness: a colliding prefix grows rather than being reused, so an ID never addresses two
// different targets. Items whose target sets are genuinely identical do end up sharing an ID; that
// is a producer bug, and a resolver seeing two matches must refuse to act rather than pick one.
//
// Items that address nothing — a notice explaining why a check could not run — get no ID, because
// there is nothing to act on and a handle would imply otherwise.
func assignIDs(items []model.CleanItem) {
	taken := map[string]bool{}
	for i := range items {
		if len(items[i].Locators) == 0 {
			continue
		}
		full := itemDigest(items[i])
		prefix := idPrefix(items[i].Kind)
		id := prefix + "-" + full
		for n := idHexLen; n < len(full); n += 4 {
			if cand := prefix + "-" + full[:n]; !taken[cand] {
				id = cand
				break
			}
		}
		taken[id] = true
		items[i].ID = id
	}
}

// relPath renders an absolute artifact path as a root-relative, slash-separated locator path and
// reports whether it stayed inside root.
//
// Both sides are symlink-resolved before comparison. Resolving only the artifact side produces
// false "escapes root" answers whenever root itself sits under a symlinked prefix (macOS
// /tmp → /private/tmp is the everyday case), and an escape answer is load-bearing here: it decides
// whether an item may be moved at all.
func relPath(root, abs string) (rel string, inside bool) {
	rr, ra := root, abs
	if r, err := filepath.EvalSymlinks(root); err == nil {
		rr = r
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		ra = r
	}
	rel, err := filepath.Rel(filepath.Clean(rr), filepath.Clean(ra))
	if err != nil {
		// Unrelatable (different volumes on Windows): keep the absolute path so the item is still
		// reportable, and let the caller mark it unmovable.
		return filepath.ToSlash(abs), false
	}
	rel = filepath.ToSlash(rel)
	return rel, rel != ".." && !strings.HasPrefix(rel, "../")
}
