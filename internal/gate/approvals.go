// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/basdotio/AgentGuard/internal/safeio"
)

// ApprovalsFile is the store's name under a scan root. It sits beside .aguardignore and is
// excluded from the unowned-entries note the same way (collect/unowned.go): the tool's own
// state is not a coverage gap in the user's environment.
const ApprovalsFile = ".aguard-approvals.json"

// storeVersion guards the on-disk shape. A file from the future is treated as unreadable
// (i.e. as empty), never as partially understood — see Load.
const storeVersion = 1

// Approval records that a specific piece of CONTENT was accepted for loading.
//
// The map key is the canonical hash and the record repeats it, so a hand-edited file that
// disagrees with itself can be detected rather than silently trusted (Load drops such rows).
// Name and Path are for the human reading `aguard approvals`; they are never matched on —
// approving "pdf-export" would survive that skill being replaced wholesale, which is the one
// thing this store exists to prevent.
type Approval struct {
	Hash        string `json:"hash"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Score       int    `json:"score"`
	Verdict     string `json:"verdict"` // VerdictClean | VerdictAccepted
	ApprovedAt  int64  `json:"approved_at"`
	ToolVersion string `json:"tool_version"`
}

// Verdict values recorded on an approval. The distinction is worth keeping: "nothing was
// found" and "something was found and a human accepted it anyway" look identical in a
// score, and only the second is a decision someone may want to revisit.
const (
	VerdictClean    = "clean"
	VerdictAccepted = "accepted-risk"
)

// Pending is a verdict parked against one tool call while the operator answers the prompt.
//
// It exists so an approval can only ever cover the bytes the prompt described: PostToolUse
// re-reads the target and promotes the entry only if the hash still matches (see handlePost).
// Without the correlation the gate would record whatever is on disk after the load, which is
// not necessarily what anyone was shown.
//
// It only works if it survives the FILE. Claude Code starts the hook afresh for every event, so
// the process that parked the verdict has exited before anyone answers; PostToolUse finds it
// only because LoadStore reads it back. A test that hands one in-memory Store to both events
// cannot see whether that happens, which is why the cross-process tests (pending_test.go)
// give every event a store freshly loaded from disk.
type Pending struct {
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Score   int    `json:"score"`
	AskedAt int64  `json:"asked_at"`
}

// pendingTTL bounds how long a parked verdict stays meaningful. A prompt nobody answers
// leaves an entry behind, and an unbounded map in a file written on every skill load grows
// without a ceiling; an hour is far longer than any prompt stays on screen.
const pendingTTL = 3600

// expired is THE expiry rule for a parked verdict: pend prunes with it on the way in and
// LoadStore drops with it on the way back out of the file, so an entry cannot be fresh in one
// place and stale in the other. A zero on either side never expires anything, which is why
// LoadStore refuses undated rows outright (see pendingRowOK) rather than keeping them forever.
func expired(askedAt, now int64) bool {
	return now > 0 && askedAt > 0 && now-askedAt > pendingTTL
}

// Store is the approvals set. The zero value is a usable empty store.
type Store struct {
	Version   int                 `json:"version"`
	Approvals map[string]Approval `json:"approvals"`
	// Pending is keyed by tool_use_id, not by hash: two sessions can be answering prompts
	// about the same content at once, and each answer must resolve against its own call.
	Pending map[string]Pending `json:"pending,omitempty"`
	// Corrupt is set when the file existed but could not be understood. It is NOT an error
	// return: a corrupt store must degrade to "ask about everything", never to "allow
	// everything", and the caller surfaces the fact rather than dropping it (invariant #5).
	Corrupt string `json:"-"`
	path    string
}

// ApprovalsPath returns the store location for a root.
func ApprovalsPath(root string) string { return filepath.Join(root, ApprovalsFile) }

// LoadStore reads the approvals file. A MISSING file is an empty store and not an error —
// the gate must work on a machine that has never approved anything.
//
// A file that exists but does not parse, or carries an unknown version, degrades to an empty
// store with Corrupt set. That direction is deliberate and is the opposite of the tool's usual
// fail-closed rule, because here the two failure directions are not symmetric: forgetting
// approvals costs the operator some prompts, while honouring a store we cannot parse would
// hand a silent allow to whatever wrote the garbage.
//
// Parked verdicts are read back too (readPending), judged against this process's own wall
// clock — the clock the hook runner also hands the gate as Options.Now, so a verdict is aged
// by one clock from the moment it is parked to the moment it is answered.
func LoadStore(path string) *Store {
	s := &Store{Version: storeVersion, Approvals: map[string]Approval{}, path: path}
	b, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.Corrupt = fmt.Sprintf("approvals file %s is unreadable (%v); every artifact will be re-checked", path, err)
		}
		return s
	}
	var on Store
	if err := json.Unmarshal(b, &on); err != nil {
		s.Corrupt = fmt.Sprintf("approvals file %s did not parse (%v); every artifact will be re-checked", path, err)
		return s
	}
	if on.Version != storeVersion {
		s.Corrupt = fmt.Sprintf("approvals file %s has version %d, this build understands %d; every artifact will be re-checked",
			path, on.Version, storeVersion)
		return s
	}
	for k, v := range on.Approvals {
		// A row whose key disagrees with its own hash was edited by something that did not
		// understand the format. Dropping it costs one prompt; keeping it would let a
		// rename-the-key edit approve content nobody audited.
		if k == "" || k != v.Hash {
			continue
		}
		s.Approvals[k] = v
	}
	s.Pending = readPending(on.Pending, time.Now().Unix())
	return s
}

// readPending keeps the parked verdicts from the file that PostToolUse could still honour.
// It builds a new map (nil when nothing survives, so a store with no open prompt writes no
// "pending" key) rather than filtering the decoded one in place.
func readPending(rows map[string]Pending, now int64) map[string]Pending {
	var out map[string]Pending
	for id, p := range rows {
		if !pendingRowOK(id, p, now) {
			continue
		}
		if out == nil {
			out = make(map[string]Pending, len(rows))
		}
		out[id] = p
	}
	return out
}

// pendingRowOK is the read-back hygiene for one parked verdict, in the spirit of the approvals
// rule above: a row this gate could not have written is dropped, at the cost of one more
// prompt, rather than kept.
//
//   - no tool_use_id: nothing to correlate an answer with;
//   - no hash: nothing PostToolUse could compare its re-scan against;
//   - undated, or dated in the future: not a time this gate's clock wrote, and expired() would
//     keep it forever (a clock stepped back between the two events costs one prompt);
//   - expired: the same rule pend prunes with.
func pendingRowOK(id string, p Pending, now int64) bool {
	if id == "" || p.Hash == "" {
		return false
	}
	if p.AskedAt <= 0 || p.AskedAt > now {
		return false
	}
	return !expired(p.AskedAt, now)
}

// Approved reports whether these exact bytes have been accepted before. An empty hash is
// never approved: a target the collector could not hash is a target nobody audited.
func (s *Store) Approved(hash string) (Approval, bool) {
	if s == nil || hash == "" {
		return Approval{}, false
	}
	a, ok := s.Approvals[hash]
	return a, ok
}

// pend parks a verdict against a tool call. Expired entries are pruned on the way in, so
// the map is bounded by the number of prompts actually open rather than by history.
func (s *Store) pend(toolUseID string, v Verdict, now int64) {
	if toolUseID == "" || v.Hash == "" {
		return
	}
	if s.Pending == nil {
		s.Pending = map[string]Pending{}
	}
	for id, p := range s.Pending {
		if expired(p.AskedAt, now) {
			delete(s.Pending, id)
		}
	}
	s.Pending[toolUseID] = Pending{Hash: v.Hash, Name: v.Name, Kind: v.Kind, Path: v.Path, Score: v.Score, AskedAt: now}
}

// pendingFor returns the verdict parked against a tool call.
//
// It does not judge age itself: expiry is applied where an entry enters a store — when it is
// parked (pend) and when it is read back from the file (LoadStore). Every hook event is its own
// process, so no store lives long enough for an entry to expire inside it.
func (s *Store) pendingFor(toolUseID string) (Pending, bool) {
	if s == nil || toolUseID == "" {
		return Pending{}, false
	}
	p, ok := s.Pending[toolUseID]
	return p, ok
}

func (s *Store) dropPending(toolUseID string) { delete(s.Pending, toolUseID) }

// Approve records an approval. Callers must only reach this with a hash they computed in
// this call (invariant #2) — there is deliberately no API taking a hash string from input.
func (s *Store) Approve(a Approval) {
	if s.Approvals == nil {
		s.Approvals = map[string]Approval{}
	}
	if a.Hash == "" {
		return
	}
	s.Approvals[a.Hash] = a
}

// Forget removes an approval, returning whether anything was removed.
func (s *Store) Forget(hash string) bool {
	if _, ok := s.Approvals[hash]; !ok {
		return false
	}
	delete(s.Approvals, hash)
	return true
}

// List returns the approvals sorted by name then hash, for stable output.
func (s *Store) List() []Approval {
	out := make([]Approval, 0, len(s.Approvals))
	for _, a := range s.Approvals {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Hash < out[j].Hash
	})
	return out
}

// Save writes the store atomically (temp file in the same directory, then rename) with mode
// 0600. Atomicity matters more here than the small size suggests: this file is written from
// a hook that fires on every skill load, so a session that is interrupted mid-write must
// leave the previous store intact rather than a truncated one — and a truncated one, per
// LoadStore, would silently discard every approval the operator has made.
//
// Refusing to write a store that failed to load is the other half of that: overwriting a
// corrupt file would destroy whatever a human could still recover from it.
func (s *Store) Save() error {
	if s.path == "" {
		return errors.New("approvals store has no path")
	}
	if s.Corrupt != "" {
		return fmt.Errorf("refusing to overwrite an unreadable approvals store: %s", s.Corrupt)
	}
	s.Version = storeVersion
	if s.Approvals == nil {
		s.Approvals = map[string]Approval{}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".aguard-approvals-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
