// SPDX-License-Identifier: MIT
package hygiene

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/safeio"
)

// Deciding whether a skill was ever used needs a record of what ran. There are two on disk and they
// are not equivalent:
//
//   - history.jsonl is "every prompt you've typed, with timestamp and project path", kept for
//     up-arrow recall. It contains NO tool or skill invocations. Matching skill names against it
//     answers "did you ever type this word", which is a different question: a skill Claude loaded on
//     its own — the ordinary case, since routing is what descriptions are for — leaves no trace in
//     it at all, and any incidental mention of the word counts as a use.
//   - projects/<project>/<session>.jsonl is the full conversation transcript: every message, tool
//     call and tool result. A skill invocation appears here.
//
// Both are read, because each covers what the other misses: a slash invocation is something the
// operator typed, and an automatic one exists only in the transcript.
//
// PRIVACY. Transcripts are not encrypted and, per Claude Code's own documentation, contain whatever
// a tool printed — credentials included. So this file MEMBERSHIP-TESTS and nothing else: it never
// returns content, never stores a line, and nothing it reads can reach a finding, a report or the
// JSON output. The two names it does extract (the roster and the invoked skill) are skill names,
// not message content, and even those are only ever counted — never echoed. It also streams under a
// byte cap rather than loading files that are routinely tens of megabytes.

// maxUsageBytes caps how much of one usage file is read — from the END of it.
//
// Both halves of that sentence were wrong once and each cost something.
//
// The cap was justified as keeping "a multi-gigabyte transcript out of memory", but the read is a
// streaming bufio scan: memory is bounded by the line buffer regardless, so what this limits is
// time, not footprint. It was paid for with a real loss.
//
// And it read the file from the START. Measured on a live 16.5 MB transcript: the cap fell at 51%,
// the one Skill invocation in the session sat at 13.60 MB, and so the evidence CALIBRATION EXISTS
// TO FIND was in the file and never read. Worse, the error direction is the harmful one — less read
// means fewer names matched as used, which means MORE skills reported as zombies.
//
// So the window is now the most recent maxUsageBytes, matching the newest-first ordering of
// transcriptFiles for the same reason: when only part of the history fits, the part that matters is
// the part nearest now. Hitting the cap is disclosed, like every other narrowing (invariant #5).
const maxUsageBytes = 8 << 20

// maxUsageFiles caps how many transcripts are consulted. A busy user accumulates thousands; reading
// every one to answer a weak-signal question is not a trade worth making.
//
// The cap changed meaning when the roster fix landed. While every installed skill matched every
// transcript (see isSkillListing), reading more files could not change an answer, so which files
// were read did not matter. Now each additional file can flip a verdict, which forces two things
// that were previously irrelevant: transcripts are read NEWEST FIRST (ReadDir order is lexical by
// session UUID, i.e. arbitrary — "the 64 whose name sorts first" was never the intended set), and
// hitting the cap is DISCLOSED rather than silently narrowing the evidence (invariant #5).
const maxUsageFiles = 64

// usageSource records WHICH evidence was available, because it changes how much the answer is worth.
type usageSource struct {
	prompts     bool // history.jsonl present
	transcripts bool // at least one session transcript present
	read        int  // transcripts actually read — the width of any "never used" verdict
	clipped     int  // transcripts read only in part, because they exceed maxUsageBytes
	skipped     int  // transcripts left unread because maxUsageFiles was reached
	cal         calibration
}

func (u usageSource) found() bool { return u.prompts || u.transcripts }

// confidence maps evidence to how far the verdict may be trusted. Transcripts record invocations, so
// they earn medium; typed prompts alone stay low, since they cannot see an automatic invocation.
//
// Neither reaches high, and calibration (below) does not raise it either. Establishing that the
// recorded name IS the directory name removes a false-NEGATIVE source; it says nothing about the
// two limits that actually cap this check — it is one machine's history, and at most maxUsageFiles
// of it. "Not used HERE, in the last 64 sessions" is not "not used". model.UnattendedSafe() stays
// unreachable by construction.
func (u usageSource) confidence() string {
	if u.transcripts {
		return "medium"
	}
	return "low"
}

// skillListingMarker is the cheap pre-filter for the roster record. Nearly every transcript line
// lacks it, so the expensive JSON parse runs on a handful of lines out of thousands.
const skillListingMarker = "skill_listing"

// skillToolMarker is the same trick for an invocation record: the tool is literally named "Skill".
const skillToolMarker = `"Skill"`

// listingRecord is Claude Code's roster of AVAILABLE skills. It is not evidence of use — and it is
// the calibration reference; see calibration below.
type listingRecord struct {
	Type       string `json:"type"`
	Attachment struct {
		Type  string   `json:"type"`
		Names []string `json:"names"`
	} `json:"attachment"`
}

// skillListing reports whether a transcript line is the roster record, and returns the names it
// carries.
//
// MEASURED, and it invalidated the whole check. Claude Code writes
// {"type":"attachment","attachment":{"type":"skill_listing","skillCount":31,"names":[…]}} whenever
// the roster changes — 21 times in one real session — and `names` holds EVERY installed skill.
// Since the zombie check asks "does this name appear anywhere in the transcript", every installed
// skill answered yes, always. The check did not merely have a high false-negative rate; on any
// machine that keeps transcripts it could not report a zombie at all, because being installed was
// sufficient to look used.
//
// The measurement is worth restating because it inverts the confidence ladder: with transcripts
// present (the tier confidence() calls "medium") a fixture of three never-invoked skills produced
// ZERO zombies, while with only history.jsonl (the "low" tier) it produced all three. The tier the
// code trusted more was the one that was structurally inert. See docs/internals/measurement-usage-records.md.
func skillListing(line string) (listingRecord, bool) {
	var rec listingRecord
	if !strings.Contains(line, skillListingMarker) {
		return rec, false
	}
	if json.Unmarshal([]byte(line), &rec) != nil {
		// Unparseable: fall through to matching rather than dropping the line. Skipping something
		// we cannot identify would silently shrink the evidence, which is the failure mode this
		// whole function exists to correct.
		return rec, false
	}
	return rec, rec.Type == "attachment" && rec.Attachment.Type == skillListingMarker
}

// invocationRecord is an assistant turn carrying tool calls. MEASURED shape of an invocation:
//
//	{"type":"assistant","message":{"content":[
//	   {"type":"tool_use","name":"Skill","input":{"skill":"dataviz"}} ]}}
//
// `content` is sometimes a bare string rather than an array, so the unmarshal fails on those lines.
// That failure direction is the safe one: an unread invocation shrinks the observed set, which can
// only make calibration decline, never wrongly pass.
type invocationRecord struct {
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				Skill string `json:"skill"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// invokedSkills returns the skill names a transcript line records as ACTUALLY INVOKED.
func invokedSkills(line string) []string {
	if !strings.Contains(line, skillToolMarker) {
		return nil
	}
	var rec invocationRecord
	if json.Unmarshal([]byte(line), &rec) != nil {
		return nil
	}
	var out []string
	for _, c := range rec.Message.Content {
		if c.Type == "tool_use" && c.Name == "Skill" && c.Input.Skill != "" {
			out = append(out, c.Input.Skill)
		}
	}
	return out
}

// baseName reduces a roster name, an invocation name or an artifact name to the leaf that
// identifies the skill, so the three can be compared at all.
//
// Two prefixes have to come off. The measured roster carries "product-management:brainstorm" for a
// plugin's skill and a bare "dataviz" for a top-level one, and which shape an INVOCATION uses was
// not observed. Separately, skills nest: a real environment holds skills/synced/docx, whose
// artifact name is "synced/docx" while the roster calls it "docx" — without this the whole
// calibration is inert on any nested layout, which is how the check would have quietly done nothing
// on the first machine it was run against.
//
// Collapsing both prefixes makes distinct same-leaf skills indistinguishable here (a bundled "docx"
// and an installed "synced/docx"). That ambiguity resolves to "invoked", i.e. to NOT reporting a
// zombie, which is the safe side. It does not widen the hole described on calibration: the harmful
// case still requires an invocation recorded under a leaf belonging to some other skill entirely.
func baseName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexAny(s, `:/\`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// calibration decides whether the PRECISE predicate ("this skill appears as input.skill on a Skill
// tool_use") may replace the loose one ("this name appears somewhere in the transcript").
//
// The two predicates fail in opposite directions:
//
//	loose   errs toward counting an unused skill as used   → under-reports zombies (harmless)
//	precise errs toward counting a used skill as unused    → reports a skill you are USING as
//	                                                          deletable (harmful)
//
// A change that fails in the harmful direction needs its premise proven, not assumed. The premise
// is "the name recorded at invocation is the skill's DIRECTORY name", and it has only ever been
// observed on a bundled skill, not on one installed under <root>/skills/. So it is proven on the
// operator's own machine, at scan time, from evidence already being read: the roster record —
// discarded above as evidence of use — names every available skill in whatever namespace Claude
// Code uses, and is the bridge:
//
//	step 1 (per skill): the skill's directory name appears in the roster
//	                    → the roster speaks directory names FOR THIS SKILL
//	step 2 (global):    every observed invocation name appears in the roster
//	                    → invocations speak the roster's namespace
//
// Both hold ⇒ invocation names are directory names, for these skills. Either fails ⇒ that skill
// keeps the loose predicate. The fallback is per-skill so one freshly installed skill (absent from
// every roster written so far) cannot switch the whole run back.
//
// Where step 2 catches the danger: if skill X was used but recorded under some other name, X's
// invocation is IN the transcript under that other name, so the mismatch is observable. The
// residual hole is a collision — X recorded under a name that is another INSTALLED skill's name —
// in which case Y reads as used (safe) and X
// reads as unused (harmful). It needs a deliberate name clash to occur and is not defended against.
type calibration struct {
	roster    map[string]bool // base names from every skill_listing record seen
	invoked   map[string]bool // base names from every Skill tool_use seen
	unmatched int             // invocation names absent from the roster (step 2 failures)
	fellBack  int             // installed skills absent from the roster (step 1 failures)
}

// active reports whether step 2 passed, i.e. whether precise matching is admissible at all.
// No roster or no observed invocation means no proof, which means no change in behaviour.
func (c calibration) active() bool {
	return len(c.roster) > 0 && len(c.invoked) > 0 && c.unmatched == 0
}

// usageEvidence is everything one pass over the usage records yields. The two loose maps are kept
// APART because precise matching replaces only the transcript leg: a skill the operator typed is
// used whatever the transcript says, and dropping that would reintroduce the harmful direction for
// every slash invocation.
type usageEvidence struct {
	prompt     map[string]bool // loose match in history.jsonl
	transcript map[string]bool // loose match in a transcript
	cal        calibration
}

// used answers the question the zombie check actually asks, per skill.
func (e usageEvidence) used(name string) bool {
	if e.prompt[name] {
		return true
	}
	if e.cal.active() && e.cal.roster[baseName(name)] {
		return e.cal.invoked[baseName(name)] // precise: proven admissible for this skill
	}
	return e.transcript[name]
}

// usedNames streams the usage records and returns the evidence for every name. The needles are
// compiled once and every line is tested against all of them, so the cost is one pass over the
// capped bytes regardless of how many skills are installed.
func usedNames(root string, names []string) (usageEvidence, usageSource) {
	ev := usageEvidence{
		prompt:     map[string]bool{},
		transcript: map[string]bool{},
		cal:        calibration{roster: map[string]bool{}, invoked: map[string]bool{}},
	}
	var src usageSource
	if len(names) == 0 {
		return ev, src
	}

	pats := make(map[string]*regexp.Regexp, len(names))
	for _, n := range names {
		if n == "" {
			ev.prompt[n] = true // cannot judge → never flag
			continue
		}
		pats[n] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(n) + `\b`)
	}

	// scan reads one usage file. loose is the map its ordinary text matches land in; structural
	// extraction (roster, invocations) runs only for transcripts.
	scan := func(path string, loose map[string]bool, structural bool) bool {
		f, err := safeio.Open(path)
		if err != nil {
			return false
		}
		defer func() { _ = f.Close() }()
		// Seek to the TAIL when the file is oversized, and drop the partial line that lands on.
		// Reading the head of a transcript answers "was this used a long time ago", which is the
		// question nobody asked.
		clipped := false
		if fi, serr := f.Stat(); serr == nil && fi.Size() > maxUsageBytes {
			if _, seerr := f.Seek(fi.Size()-maxUsageBytes, io.SeekStart); seerr == nil {
				clipped = true
			}
		}
		sc := bufio.NewScanner(f)
		// Transcript lines are whole JSON messages and routinely exceed the default 64KiB.
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		if clipped {
			sc.Scan() // the seek landed mid-record; that fragment is not a line
			src.clipped++
		}
		for sc.Scan() {
			line := sc.Text()
			if structural {
				if rec, ok := skillListing(line); ok {
					for _, n := range rec.Attachment.Names {
						ev.cal.roster[baseName(n)] = true
					}
					continue // the roster is not evidence of use
				}
				for _, n := range invokedSkills(line) {
					ev.cal.invoked[baseName(n)] = true
				}
			}
			for n, re := range pats {
				if !loose[n] && re.MatchString(line) {
					loose[n] = true
				}
			}
		}
		return true
	}

	if _, err := os.Stat(filepath.Join(root, "history.jsonl")); err == nil {
		src.prompts = scan(filepath.Join(root, "history.jsonl"), ev.prompt, false)
	}

	files := transcriptFiles(root)
	budget := maxUsageFiles
	for _, t := range files {
		if budget <= 0 {
			src.skipped++
			continue
		}
		if scan(t.path, ev.transcript, true) {
			src.transcripts = true
			src.read++
			budget--
		}
	}

	// Step 2 is global and must be settled before step 1 is counted, since both feed one note.
	for n := range ev.cal.invoked {
		if !ev.cal.roster[n] {
			ev.cal.unmatched++
		}
	}
	if ev.cal.active() {
		for _, n := range names {
			if n != "" && !ev.cal.roster[baseName(n)] {
				ev.cal.fellBack++
			}
		}
	}
	src.cal = ev.cal
	return ev, src
}

// usageCoverageNotes turns what the pass could NOT establish into items the operator can read.
// Both cases narrow the evidence behind a "never used" verdict, and invariant #5 says a narrowed
// scan says so out loud. They carry no locator, so they get no ID and address nothing — the same
// shape as the "no usage record" note.
//
// Neither note names a skill. The calibration inputs are read under the membership-test rule in
// this file's header, so they are counted and never echoed.
func usageCoverageNotes(src usageSource) []model.CleanItem {
	var out []model.CleanItem
	if src.skipped > 0 {
		out = append(out, model.CleanItem{
			Kind: "zombie",
			Detail: "Only the " + itoa(maxUsageFiles) + " most recent session transcripts were read; " +
				itoa(src.skipped) + " older one(s) were not. A skill last used before them reads as unused.",
		})
	}
	if src.clipped > 0 {
		out = append(out, model.CleanItem{
			Kind: "zombie",
			Detail: itoa(src.clipped) + " transcript(s) are larger than " + itoa(maxUsageBytes>>20) +
				" MiB and only their most recent part was read. Anything used earlier in those " +
				"sessions reads as unused — this narrowing errs toward reporting MORE zombies, so " +
				"treat those entries with more suspicion, not less.",
		})
	}
	switch {
	case src.cal.unmatched > 0:
		out = append(out, model.CleanItem{
			Kind: "zombie",
			Detail: "Precise invocation matching was declined: " + itoa(src.cal.unmatched) +
				" invoked skill name(s) are absent from Claude Code's own roster of available " +
				"skills, so the recorded name cannot be assumed to be the directory name. " +
				"Fell back to name-appears-anywhere matching, which under-reports zombies.",
		})
	case src.cal.active() && src.cal.fellBack > 0:
		out = append(out, model.CleanItem{
			Kind: "zombie",
			Detail: itoa(src.cal.fellBack) + " installed skill(s) do not appear in any roster " +
				"record (newly installed, or not loaded), so precise invocation matching was not " +
				"applied to them; they kept name-appears-anywhere matching, which under-reports zombies.",
		})
	}
	return out
}

// transcriptFile is one candidate usage file with the timestamp used to rank it.
type transcriptFile struct {
	path string
	mod  time.Time
}

// transcriptFiles lists projects/<project>/<session>.jsonl and the subagent transcripts beside
// them, NEWEST FIRST. Ordering is load-bearing now that the file budget can change an answer: the
// 64 most recent sessions are the ones whose silence means something, while ReadDir order is
// session-UUID lexical, i.e. unrelated to anything.
func transcriptFiles(root string) []transcriptFile {
	projects := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil {
		return nil
	}
	var out []transcriptFile
	add := func(p string) {
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return
		}
		out = append(out, transcriptFile{path: p, mod: fi.ModTime()})
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projects, e.Name())
		sessions, serr := os.ReadDir(dir)
		if serr != nil {
			continue
		}
		for _, s := range sessions {
			// A SESSION DIRECTORY, not a stray folder. Beside <session>.jsonl Claude Code keeps
			// <session>/subagents/<agent>.jsonl, and `if s.IsDir() { continue }` skipped every one
			// of them — measured: 5 subagent transcripts, 1.6 MB, next to a single 13 MB main one.
			// A skill invoked only inside a subagent left no evidence this function could see, so
			// it read as never used. One level down, no deeper: the shape is documented and a
			// general walk would turn a bounded read into an unbounded one.
			if s.IsDir() {
				subDir := filepath.Join(dir, s.Name(), "subagents")
				agents, aerr := os.ReadDir(subDir)
				if aerr != nil {
					continue
				}
				for _, a := range agents {
					if !a.IsDir() && strings.EqualFold(filepath.Ext(a.Name()), ".jsonl") {
						add(filepath.Join(subDir, a.Name()))
					}
				}
				continue
			}
			if strings.EqualFold(filepath.Ext(s.Name()), ".jsonl") {
				add(filepath.Join(dir, s.Name()))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].mod.Equal(out[j].mod) {
			return out[i].path < out[j].path // ties broken deterministically
		}
		return out[i].mod.After(out[j].mod)
	})
	return out
}
