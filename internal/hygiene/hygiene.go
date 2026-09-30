// SPDX-License-Identifier: MIT
// Package hygiene finds "junk" — the cleanup half of AgentGuard (spec §6). All checks
// are deterministic (no LLM): oversized descriptions (context bloat), duplicate/near-
// duplicate skills, dangling file references, and — best-effort — never-used skills.
// This is the differentiated "360 cleanup" value; it quantifies recoverable context.
package hygiene

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basdotio/AgentGuard/internal/clean"
	"github.com/basdotio/AgentGuard/internal/detect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/parse"
)

// bloatThresholdTokens: a skill description above this is flagged as context bloat.
// Raised to 200 (from 120) — routing descriptions are legitimately long; only flag
// significant outliers to keep the cleanup report low-noise (review F2).
const bloatThresholdTokens = 200

// Options tunes which hygiene checks run. Zombie is OFF by default: usage-log name
// matching is a weak signal with a high false-positive rate, so it is opt-in (--zombie).
type Options struct {
	Zombie bool
}

// Blocker reasons are defined in model, because they cross package boundaries: written here, read
// by clean, printed by the report, parsed by anything consuming `clean --json`.
const (
	blockerOutsideRoot   = model.BlockerOutsideRoot
	blockerContentEdit   = model.BlockerContentEdit
	blockerSideSelection = model.BlockerSideSelection
)

// skillInfo is one skill artifact plus its parsed metadata, carried together because every check
// below needs the name for display, the path for addressing, and the canonical hash for change
// detection. The hash comes from the collector rather than being recomputed here — it is the same
// value the reputation list is keyed on, so an item and a scan agree about what "unchanged" means.
type skillInfo struct {
	name string
	meta parse.SkillMeta
	path string
	hash string
}

// moveBlocker asks the EXECUTOR whether this path could be quarantined, and returns its answer.
//
// Not a reimplementation of that rule, and deliberately not a subset of it. The plan used to ask a
// weaker question — "is it inside root" — while `clean --apply` asked "is it inside skills/", so a
// skill installed as skills/x -> shared/x appeared in the listing with NO blocker, was counted in
// "1 executable", and was refused only once the operator had committed to a run. Blockers exist so
// that "this cannot be moved" is visible while there is still a decision to make; that is only true
// while both sides ask one question.
func moveBlocker(root, path string) (blocker, why string) {
	return clean.QuarantineRefusal(root, path)
}

// locate builds a locator for a path under a skill, and reports whether it stayed inside root.
// Escaping root is the symlink-installed case (`~/.agents/skills/*`), which is reportable but not
// movable — the caller turns the false into a blocker rather than dropping the item.
func locate(root string, s skillInfo, sub string) (model.Locator, bool) {
	p := s.path
	if sub != "" {
		p = filepath.Join(s.path, sub)
	}
	rel, inside := relPath(root, p)
	return model.Locator{Path: rel, Name: s.name, Hash: s.hash}, inside
}

// Analyze runs the hygiene checks over the skill artifacts and returns cleanup items.
// It reads each skill's SKILL.md; non-skill artifacts are ignored.
//
// Every check emits ONE ITEM PER DECISION rather than one item per kind. The aggregate shape it
// replaced could be read but not acted on: with every zombie in a single finding's target list
// there was no way to say "that one, not the other four", and the item's identity changed whenever
// the environment gained an unrelated skill.
func Analyze(root string, arts []model.ArtifactReport, opts Options) []model.CleanItem {
	var skills []skillInfo
	for _, a := range arts {
		if a.Kind != model.KindSkill {
			continue
		}
		skills = append(skills, skillInfo{name: a.Name, meta: parse.ReadSkill(a.Path), path: a.Path, hash: a.Hash})
	}

	var out []model.CleanItem

	// zombie: skills that appear in no usage record. Opt-in, and never high confidence, which is what
	// keeps it out of any batch run — see usage.go for which records exist, what each can and cannot
	// see, and why their content never leaves that file.
	if opts.Zombie {
		names := make([]string, 0, len(skills))
		for _, sk := range skills {
			names = append(names, sk.name)
		}
		ev, src := usedNames(root, names)
		out = append(out, usageCoverageNotes(src)...)
		switch {
		case len(names) == 0:
			// Nothing to judge. Saying "no usage record" here would blame a missing log for an empty
			// answer that has nothing to do with it.
		case !src.found():
			// Addresses nothing, so it carries no ID and no action: "the check could not run" is
			// reported precisely so it cannot be mistaken for "the check found nothing".
			out = append(out, model.CleanItem{
				Kind: "zombie",
				Detail: "No usage record (history.jsonl or projects/*/*.jsonl); the zombie " +
					"(never-used) check was skipped.",
			})
		default:
			conf := model.ConfLow
			detail := "Never appears in the usage record — possible zombie (installed but unused). " +
				"Weak signal: only typed prompts were available, and a skill Claude invokes on its " +
				"own leaves no trace there."
			if src.confidence() == "medium" {
				// The verdict is only ever as wide as the history behind it, so the history is
				// STATED rather than implied. "Never appears in any session transcript" reads as
				// an exhaustive search; on a machine with 15 sessions it is a claim about 15
				// sessions, and a skill last used before them is indistinguishable from one that
				// was never used at all. Printing the number lets the operator apply the one piece
				// of context the tool does not have — when they last reached for the thing.
				conf = model.ConfMedium
				n := itoa(src.read)
				detail = "Never mentioned in the " + n + " session transcript(s) on this machine, " +
					"nor typed — possible zombie (installed but unused). Matching is by name " +
					"anywhere in the record, the weaker of the two predicates. Those " + n +
					" session(s) are the entire history here: anything last used before them reads as unused."
				if src.cal.active() {
					detail = "Never invoked in the " + n + " session transcript(s) on this machine, " +
						"and never typed — possible zombie (installed but unused). Invocation " +
						"records were matched by name after confirming here that the recorded name " +
						"is the directory name. Those " + n + " session(s) are the entire history " +
						"here: anything last used before them reads as unused."
				}
			}
			for _, sk := range skills {
				if ev.used(sk.name) {
					continue
				}
				l, _ := locate(root, sk, "")
				it := model.CleanItem{
					Kind: "zombie", Tier: model.TierAuto, Actionable: true,
					Confidence: conf, Action: model.ActionMove,
					Targets: []string{sk.name}, Locators: []model.Locator{l},
					Detail: detail,
				}
				if b, why := moveBlocker(root, sk.path); b != "" {
					it.Blockers = append(it.Blockers, b)
					// The blocker id is for scripts; the sentence is for the person reading the
					// listing. Printing only the id moved the explanation to a place they would
					// reach by running the command that was never going to work.
					it.Detail += " Cannot be moved: " + why + "."
				}
				out = append(out, it)
			}
		}
	}

	// context_bloat: quantify what each over-long description costs. Per skill, because a single
	// total cannot be acted on and its target set (hence its identity) shifted with every install.
	for _, s := range skills {
		if !s.meta.OK {
			continue
		}
		tok := estimateTokens(s.meta.Description)
		if tok <= bloatThresholdTokens {
			continue
		}
		l, _ := locate(root, s, "SKILL.md")
		l.Entry = "frontmatter:description"
		out = append(out, model.CleanItem{
			Kind: "context_bloat", Tier: model.TierContent, Actionable: true,
			Confidence: model.ConfHigh, Action: model.ActionLineDelete,
			Targets: []string{s.name}, Locators: []model.Locator{l},
			ReclaimTokens: tok - bloatThresholdTokens,
			Detail: "Description is ~" + itoa(tok) + " tokens (threshold " + itoa(bloatThresholdTokens) +
				"); it occupies system-prompt context every session, and trimming can reclaim ~" +
				itoa(tok-bloatThresholdTokens) + " tokens.",
			Blockers: []string{blockerContentEdit},
		})
	}

	// duplicate_fn / trigger_collision: pairwise description similarity. Tier A2 because the action
	// is not "remove this" but "choose which of the two to keep" — a question with no safe default,
	// so it can never join a batch no matter how confident the similarity measure is.
	for i := 0; i < len(skills); i++ {
		for j := i + 1; j < len(skills); j++ {
			if !skills[i].meta.OK || !skills[j].meta.OK {
				continue
			}
			sim := jaccard(tokenize(skills[i].meta.Description), tokenize(skills[j].meta.Description))
			if sim < 0.6 {
				continue
			}
			la, _ := locate(root, skills[i], "")
			lb, _ := locate(root, skills[j], "")
			it := model.CleanItem{
				Kind: "duplicate_fn", Tier: model.TierChoice, Actionable: true,
				Confidence: model.ConfMedium, Action: model.ActionMove,
				Targets: []string{skills[i].name, skills[j].name}, Locators: []model.Locator{la, lb},
				Detail: "Two skill descriptions are highly similar (" + pct(sim) +
					"); possible duplicate/trigger-word collision — consider merging or trimming.",
				Blockers: []string{blockerSideSelection},
			}
			// Either side unmovable blocks the pair: resolving it means moving one of them, and
			// which one is not known until the operator says.
			// An ALIAS install — skills/alias -> skills/real — produces two artifacts whose paths
			// resolve to one directory. Every later check waves it through, including the content
			// hash, because the two sides ARE the same tree: --keep alias computes drop=real, and
			// real is exactly what alias points at, so the run reports success and takes away the
			// skill the operator named as the survivor. There is no side to drop here; the pair is
			// still worth reporting (knowing two names address one skill is useful) but it can
			// never be acted on.
			if samePath(skills[i].path, skills[j].path) {
				it.Blockers = append(it.Blockers, model.BlockerSameTarget)
				it.Detail += " Both names resolve to ONE directory on disk (alias install), so " +
					"there is no second copy to move."
			}
			for _, sk := range []skillInfo{skills[i], skills[j]} {
				b, why := moveBlocker(root, sk.path)
				if b == "" || listedBlocker(it.Blockers, b) {
					continue
				}
				it.Blockers = append(it.Blockers, b)
				it.Detail += " " + sk.name + " cannot be moved: " + why + "."
			}
			out = append(out, it)
		}
	}

	// stale_ref: SKILL.md links to files that don't exist. One item PER SKILL (refs joined) rather
	// than one per ref, so the report stays readable. Deliberately NOT actionable: "repair a broken
	// link" has no deterministic right answer, so it is reported for a human and nothing else.
	for _, s := range skills {
		if !s.meta.OK {
			continue
		}
		refs := staleRefs(s.path, s.meta.Body)
		if len(refs) == 0 {
			continue
		}
		for i, r := range refs {
			refs[i] = detect.Redact(r) // body-derived text → redact before echoing (§16.3)
		}
		l, _ := locate(root, s, "SKILL.md")
		out = append(out, model.CleanItem{
			Kind: "stale_ref", Confidence: model.ConfMedium, Action: model.ActionNone,
			Targets: []string{s.name}, Locators: []model.Locator{l},
			Detail: "Links to missing file(s): " + strings.Join(refs, ", "),
		})
	}

	assignIDs(out)
	// The command goes in AFTER ids exist, which is the only reason this is a second pass rather
	// than part of building the item. Worth the pass: the pair used to render as
	// "blocked: side-selection-unimplemented", which told the operator there was nothing they could
	// do about it. There is — they are the only one who can — and the way to say so is to hand them
	// the exact line rather than a flag name to go and look up.
	for i := range out {
		if out[i].Kind != "duplicate_fn" || out[i].ID == "" || len(out[i].Targets) != 2 {
			continue
		}
		// Only offered when a side-selection is the ONLY thing standing in the way. A pair that is
		// also unmovable — alias install, symlinked out of skills/ — would otherwise be handed a
		// command that is guaranteed to be refused, which is worse than saying nothing: it reads as
		// an instruction from the tool.
		if !out[i].AnswerBlocker(model.BlockerSideSelection).Executable() {
			continue
		}
		out[i].Hint = "Answer it: `clean --resolve " + out[i].ID + " --keep " + out[i].Targets[0] +
			"` (or " + out[i].Targets[1] + ", or --keep-both to accept the pair). `clean --ask` walks them all."
	}
	return out
}

// samePath reports whether two artifact paths name one directory once symlinks are resolved.
// Resolution failure counts as "same": refusing to act is the safe answer when the question of
// whether two things are the same thing cannot be settled.
func samePath(a, b string) bool {
	ra, aerr := filepath.EvalSymlinks(a)
	rb, berr := filepath.EvalSymlinks(b)
	if aerr != nil || berr != nil {
		return true
	}
	return ra == rb
}

// listedBlocker keeps a pair from printing the same reason twice when both sides fail the same way.
func listedBlocker(list []string, want string) bool {
	for _, b := range list {
		if b == want {
			return true
		}
	}
	return false
}

// estimateTokens is a cheap token proxy (~4 chars/token, English/CJK mixed is rougher
// but adequate for a bloat heuristic).
func estimateTokens(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return (len([]rune(s)) + 3) / 4
}

var wordRE = regexp.MustCompile(`[a-z0-9]+`)

// tokenize lowercases and splits into a word set for similarity.
func tokenize(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range wordRE.FindAllString(strings.ToLower(s), -1) {
		if len(w) >= 3 { // drop tiny stopword-ish tokens
			set[w] = true
		}
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// refRE matches ONLY markdown links `[text](path)` whose target is a relative file with
// a known text/script extension. Bare backtick paths are NOT matched — in prose they are
// almost always illustrative examples (src/foo.ts, e.ts), a big false-positive source.
var refRE = regexp.MustCompile(`\]\(([A-Za-z0-9_./-]+\.(md|sh|py|js|ts|txt|json|yaml|yml))\)`)

// staleRefs returns markdown-linked relative paths that don't exist. A reference is
// resolved against the skill dir FIRST, then against ancestor roots (the enclosing
// monorepo / .claude / .git boundary) — an install-bundled skill commonly links to a
// path relative to its repo root, which is present even though it isn't under the skill
// dir. Only when it exists under NONE of those is it reported stale.
func staleRefs(skillDir, body string) []string {
	roots := resolveRoots(skillDir)
	seen := map[string]bool{}
	var out []string
	for _, m := range refRE.FindAllStringSubmatch(stripCode(body), -1) {
		ref := m[1]
		if strings.HasPrefix(ref, "http") || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") || seen[ref] {
			continue
		}
		seen[ref] = true
		found := false
		for _, base := range roots {
			if fileExists(filepath.Join(base, ref)) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, ref)
		}
	}
	return out
}

// resolveRoots returns the skill dir plus ancestor "project roots" to try when resolving
// a reference: any ancestor containing a .git / .claude / package.json / go.mod marker,
// up to the filesystem root. This absorbs monorepo-relative references (review DEFERRED
// base-path fix) without ever reading file CONTENT — only existence is checked.
func resolveRoots(skillDir string) []string {
	roots := []string{skillDir}
	seen := map[string]bool{skillDir: true}
	dir := skillDir
	for i := 0; i < 8; i++ { // bounded climb
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		for _, marker := range []string{".git", ".claude", "package.json", "go.mod"} {
			if fileExists(filepath.Join(parent, marker)) && !seen[parent] {
				roots = append(roots, parent)
				seen[parent] = true
				break
			}
		}
		dir = parent
	}
	return roots
}

// stripCode blanks fenced code blocks and inline code spans so a link-shaped example inside them
// is not read as a link. Anthropic's own consolidate-memory skill documents an index format as
// `- [Title](file.md) — one-line hook`, and the report told every desktop user that skill had a
// broken link to file.md. Text in code is shown, not followed — by the model as much as by us.
// Blanked rather than removed so nothing else that cares about offsets ever sees a shifted body.
func stripCode(body string) string {
	b := []byte(body)
	blank := func(from, to int) {
		for i := from; i < to && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	// Fenced blocks first: a backtick inside a fence must not open an inline span.
	for i := 0; i < len(b); {
		j := strings.Index(string(b[i:]), "```")
		if j < 0 {
			break
		}
		start := i + j
		k := strings.Index(string(b[start+3:]), "```")
		if k < 0 {
			blank(start, len(b)) // unterminated fence runs to the end of the file
			break
		}
		end := start + 3 + k + 3
		blank(start, end)
		i = end
	}
	// Inline spans: a single backtick to the next one on the same line.
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		j := i + 1
		for j < len(b) && b[j] != '`' && b[j] != '\n' {
			j++
		}
		if j < len(b) && b[j] == '`' {
			blank(i, j+1)
			i = j
		}
	}
	return string(b)
}
