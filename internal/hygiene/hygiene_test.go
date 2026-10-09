// SPDX-License-Identifier: MIT
package hygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basdotio/AgentGuard/internal/model"
)

func skill(t *testing.T, root, name, skillMD string, extra map[string]string) model.ArtifactReport {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, c := range extra {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return model.ArtifactReport{Kind: model.KindSkill, Name: name, Path: dir}
}

func kinds(hs []model.CleanItem) map[string]model.CleanItem {
	m := map[string]model.CleanItem{}
	for _, h := range hs {
		m[h.Kind] = h
	}
	return m
}

func TestContextBloat(t *testing.T) {
	root := t.TempDir()
	long := "---\nname: big\ndescription: "
	for i := 0; i < 200; i++ {
		long += "verbose word "
	}
	long += "\n---\n# Big\n"
	a := skill(t, root, "big", long, nil)
	hs := Analyze(root, []model.ArtifactReport{a}, Options{})
	h, ok := kinds(hs)["context_bloat"]
	if !ok {
		t.Fatal("long description not flagged as context_bloat")
	}
	if h.ReclaimTokens <= 0 {
		t.Error("context_bloat should quantify reclaimable tokens")
	}
}

// Context bloat is measured on the description Claude Code puts in the listing. Behind a BOM it reads
// no frontmatter at all and lists "---" (P-024, 2.1.107), so the long text costs that session nothing.
func TestContextBloat_OnlyForADescriptionClaudeCodeLists(t *testing.T) {
	root := t.TempDir()
	md := "---\nname: x\ndescription: " + strings.Repeat("verbose word ", 200) + "\n---\n# X\n"
	listed := skill(t, root, "listed", md, nil)
	ignored := skill(t, root, "ignored", "\uFEFF"+md, nil)
	var got []string
	for _, it := range allOf(Analyze(root, []model.ArtifactReport{listed, ignored}, Options{}), "context_bloat") {
		got = append(got, it.Targets...)
	}
	if strings.Join(got, ",") != "listed" {
		t.Errorf("context_bloat targets = %v, want only [listed]", got)
	}
}

func TestDuplicateSkills(t *testing.T) {
	root := t.TempDir()
	desc := "description: opens a browser and takes screenshots to test the website end to end"
	a := skill(t, root, "browse", "---\nname: browse\n"+desc+"\n---\n", nil)
	b := skill(t, root, "devkit", "---\nname: devkit\n"+desc+"\n---\n", nil)
	hs := Analyze(root, []model.ArtifactReport{a, b}, Options{})
	if _, ok := kinds(hs)["duplicate_fn"]; !ok {
		t.Error("near-identical descriptions not flagged as duplicate_fn")
	}
}

func TestStaleRef(t *testing.T) {
	root := t.TempDir()
	// Only markdown links are checked now (bare backtick paths are illustrative, not refs).
	// [helper] exists → not stale; [gone] is a link to a missing file → stale.
	md := "---\nname: s\ndescription: x\n---\nSee [helper](scripts/helper.sh) and [gone](refs/missing.md).\n"
	a := skill(t, root, "s", md, map[string]string{"scripts/helper.sh": "echo hi\n"})
	hs := Analyze(root, []model.ArtifactReport{a}, Options{})
	h, ok := kinds(hs)["stale_ref"]
	if !ok {
		t.Fatal("missing referenced file not flagged as stale_ref")
	}
	if h.Detail == "" || !contains(h.Detail, "missing.md") {
		t.Errorf("stale_ref should name missing.md; got %q", h.Detail)
	}
}

func TestBenignSkillNoJunk(t *testing.T) {
	root := t.TempDir()
	a := skill(t, root, "clean", "---\nname: clean\ndescription: short and tidy helper\n---\n# Clean\n", nil)
	// A usage log that mentions the skill → no zombie note, no junk expected.
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"display":"used clean skill"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if hs := Analyze(root, []model.ArtifactReport{a}, Options{}); len(hs) != 0 {
		t.Errorf("tidy skill produced junk findings: %+v", hs)
	}
}

func TestZombie_NoLogNoted(t *testing.T) {
	root := t.TempDir()
	a := skill(t, root, "x", "---\nname: x\ndescription: y\n---\n", nil)
	h, ok := kinds(Analyze(root, []model.ArtifactReport{a}, Options{Zombie: true}))["zombie"]
	if !ok || !contains(h.Detail, "No usage record") {
		t.Error("missing usage log should emit a zombie-skipped note")
	}
}

func TestZombie_UnusedFlagged(t *testing.T) {
	root := t.TempDir()
	used := skill(t, root, "used", "---\nname: used\ndescription: a\n---\n", nil)
	dead := skill(t, root, "deadskill", "---\nname: deadskill\ndescription: b\n---\n", nil)
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"x":"ran used today"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h, ok := kinds(Analyze(root, []model.ArtifactReport{used, dead}, Options{Zombie: true}))["zombie"]
	if !ok {
		t.Fatal("unused skill not flagged zombie")
	}
	if len(h.Targets) != 1 || h.Targets[0] != "deadskill" {
		t.Errorf("zombie targets = %v, want [deadskill]", h.Targets)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// allOf returns every item of one kind, since checks now emit one item per decision rather than
// one per kind — the assertion that matters is usually "how many, and which".
func allOf(hs []model.CleanItem, kind string) []model.CleanItem {
	var out []model.CleanItem
	for _, h := range hs {
		if h.Kind == kind {
			out = append(out, h)
		}
	}
	return out
}

func idsOf(hs []model.CleanItem) map[string]string {
	m := map[string]string{}
	for _, h := range hs {
		if h.ID != "" {
			m[h.ID] = h.Kind + ":" + strings.Join(h.Targets, ",")
		}
	}
	return m
}

func bloatedSkill(t *testing.T, root, name, word string) model.ArtifactReport {
	t.Helper()
	md := "---\nname: " + name + "\ndescription: "
	for i := 0; i < 200; i++ {
		md += word + " "
	}
	return skill(t, root, name, md+"\n---\n# "+name+"\n", nil)
}

// The operator reads a listing, walks away, then applies by ID. That only works if an ID means the
// same target on the next run, so this is the load-bearing property of the whole addressing scheme.
func TestItemIDs_StableAcrossRuns(t *testing.T) {
	root := t.TempDir()
	a := bloatedSkill(t, root, "big", "verbose")
	first := idsOf(Analyze(root, []model.ArtifactReport{a}, Options{}))
	second := idsOf(Analyze(root, []model.ArtifactReport{a}, Options{}))
	if len(first) == 0 {
		t.Fatal("no addressable items produced")
	}
	if !sameMap(first, second) {
		t.Errorf("IDs changed between identical runs:\n%v\n%v", first, second)
	}
}

// Installing something unrelated must not renumber what is already listed. A list index would fail
// this; a content-derived ID is what makes it hold.
func TestItemIDs_UnaffectedByUnrelatedSkill(t *testing.T) {
	root := t.TempDir()
	a := bloatedSkill(t, root, "big", "verbose")
	before := idsOf(Analyze(root, []model.ArtifactReport{a}, Options{}))

	// A wholly different vocabulary, so this cannot pair with `big` as a duplicate.
	newcomer := skill(t, root, "unrelated", "---\nname: unrelated\ndescription: tiny helper\n---\n", nil)
	after := idsOf(Analyze(root, []model.ArtifactReport{a, newcomer}, Options{}))

	for id, what := range before {
		if after[id] != what {
			t.Errorf("ID %s (%s) shifted after an unrelated install; now %q", id, what, after[id])
		}
	}
}

// `--root ~/.claude` and `--root /home/u/.claude` name the same environment, so they must produce
// the same IDs — otherwise a command line copied from one invocation silently misses in the other.
func TestItemIDs_IndependentOfRootForm(t *testing.T) {
	root := t.TempDir()
	a := bloatedSkill(t, root, "big", "verbose")
	viaReal := idsOf(Analyze(root, []model.ArtifactReport{a}, Options{}))

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink := idsOf(Analyze(link, []model.ArtifactReport{a}, Options{}))
	if !sameMap(viaReal, viaLink) {
		t.Errorf("root spelling changed the IDs:\n real: %v\n link: %v", viaReal, viaLink)
	}
	// And with a trailing separator, which callers pass more often than they admit.
	if viaSlash := idsOf(Analyze(root+string(filepath.Separator), []model.ArtifactReport{a}, Options{})); !sameMap(viaReal, viaSlash) {
		t.Errorf("trailing separator changed the IDs: %v", viaSlash)
	}
}

// One item per bloated skill, each carrying its OWN token estimate. The aggregate this replaced
// reported a single sum whose target set — and therefore identity — moved with every install.
func TestContextBloat_PerSkillItems(t *testing.T) {
	root := t.TempDir()
	a := bloatedSkill(t, root, "big", "verbose")
	b := bloatedSkill(t, root, "huge", "windy")
	items := allOf(Analyze(root, []model.ArtifactReport{a, b}, Options{}), "context_bloat")
	if len(items) != 2 {
		t.Fatalf("want one context_bloat item per skill, got %d", len(items))
	}
	seen := map[string]bool{}
	for _, it := range items {
		if it.ReclaimTokens <= 0 {
			t.Errorf("%s: per-item reclaim estimate missing", it.ID)
		}
		if it.ID == "" || seen[it.ID] {
			t.Errorf("per-skill items need distinct IDs; got %q", it.ID)
		}
		seen[it.ID] = true
		if it.Tier != model.TierContent || it.Confidence != model.ConfHigh {
			t.Errorf("%s: tier/confidence = %s/%s", it.ID, it.Tier, it.Confidence)
		}
	}
}

func TestZombie_PerSkillItems(t *testing.T) {
	root := t.TempDir()
	x := skill(t, root, "deadone", "---\nname: deadone\ndescription: alpha thing\n---\n", nil)
	y := skill(t, root, "deadtwo", "---\nname: deadtwo\ndescription: beta gadget\n---\n", nil)
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"x":"nothing relevant"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	items := allOf(Analyze(root, []model.ArtifactReport{x, y}, Options{Zombie: true}), "zombie")
	if len(items) != 2 {
		t.Fatalf("want one zombie item per unused skill, got %d", len(items))
	}
	for _, it := range items {
		if !it.Executable() {
			t.Errorf("%s: an in-root zombie should be executable; blockers=%v", it.ID, it.Blockers)
		}
		if it.Confidence != model.ConfLow {
			t.Errorf("%s: prompt-history matching is a weak signal and must stay low confidence", it.ID)
		}
	}
}

// A skill installed by symlink resolves outside root. It is still worth reporting, and must never
// be offered for a move — the blocker is what carries that distinction to the executor.
func TestZombie_OutsideRootIsBlocked(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	outside := skill(t, elsewhere, "faraway", "---\nname: faraway\ndescription: z\n---\n", nil)
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"x":"unrelated"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	items := allOf(Analyze(root, []model.ArtifactReport{outside}, Options{Zombie: true}), "zombie")
	if len(items) != 1 {
		t.Fatalf("want 1 zombie item, got %d", len(items))
	}
	if items[0].Executable() {
		t.Error("a target outside root must not be executable")
	}
	if !hasBlocker(items[0], blockerOutsideRoot) {
		t.Errorf("want %s blocker, got %v", blockerOutsideRoot, items[0].Blockers)
	}
}

// Swapping the order the artifacts arrive in must not mint a second identity for the same pair.
func TestDuplicatePair_IDOrderIndependent(t *testing.T) {
	root := t.TempDir()
	desc := "description: opens a browser and takes screenshots to test the website end to end"
	a := skill(t, root, "browse", "---\nname: browse\n"+desc+"\n---\n", nil)
	b := skill(t, root, "devkit", "---\nname: devkit\n"+desc+"\n---\n", nil)
	fwd := allOf(Analyze(root, []model.ArtifactReport{a, b}, Options{}), "duplicate_fn")
	rev := allOf(Analyze(root, []model.ArtifactReport{b, a}, Options{}), "duplicate_fn")
	if len(fwd) != 1 || len(rev) != 1 {
		t.Fatalf("want exactly one pair item each way, got %d and %d", len(fwd), len(rev))
	}
	if fwd[0].ID != rev[0].ID {
		t.Errorf("pair identity depends on argument order: %s vs %s", fwd[0].ID, rev[0].ID)
	}
	if fwd[0].Tier != model.TierChoice {
		t.Errorf("a pair needs a side chosen, so it cannot be tier %s", fwd[0].Tier)
	}
	if fwd[0].UnattendedSafe() {
		t.Error("a pair must never qualify for an unattended batch")
	}
}

func TestStaleRef_ReportedButNotActionable(t *testing.T) {
	root := t.TempDir()
	md := "---\nname: s\ndescription: x\n---\nSee [gone](refs/missing.md).\n"
	a := skill(t, root, "s", md, nil)
	items := allOf(Analyze(root, []model.ArtifactReport{a}, Options{}), "stale_ref")
	if len(items) != 1 {
		t.Fatalf("want 1 stale_ref item, got %d", len(items))
	}
	if items[0].Actionable || items[0].Action != model.ActionNone {
		t.Error("repairing a broken link has no deterministic answer; it must stay report-only")
	}
	if items[0].ID == "" {
		t.Error("a report-only item still addresses a file, so it should be citable by ID")
	}
}

// The notice emitted when a check cannot run addresses nothing, so it must carry no handle: an ID
// would imply there is something here to act on.
func TestNotice_HasNoIDAndNoAction(t *testing.T) {
	root := t.TempDir()
	a := skill(t, root, "x", "---\nname: x\ndescription: y\n---\n", nil)
	items := allOf(Analyze(root, []model.ArtifactReport{a}, Options{Zombie: true}), "zombie")
	if len(items) != 1 || !contains(items[0].Detail, "No usage record") {
		t.Fatalf("want the skipped-check notice, got %+v", items)
	}
	if items[0].ID != "" || items[0].Actionable || items[0].Action != model.ActionNone {
		t.Errorf("a notice must carry no ID and no action; got %+v", items[0])
	}
}

// Records the current, deliberate state: NO detection qualifies for an unattended run, because it
// demands high confidence and the two reversible checks are low (prompt-history zombie) and
// choice-bound (duplicate pairs). Asserted so that raising a confidence fails here and forces the
// decision to be made on purpose rather than inherited.
//
// This one SAMPLES — it builds a fixture and checks what came out. The structural claim is proved
// by TestConfidenceCeiling_KeepsUnattendedUnreachable below; both are kept because they fail for
// different reasons: this one catches a new detection wired up at TierAuto+ConfHigh, that one
// catches the ceiling itself moving.
func TestNoDetectionIsUnattendedSafeYet(t *testing.T) {
	root := t.TempDir()
	a := bloatedSkill(t, root, "big", "verbose")
	b := bloatedSkill(t, root, "huge", "verbose")
	dead := skill(t, root, "deadskill", "---\nname: deadskill\ndescription: unique widget\n---\n", nil)
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"x":"nothing"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, it := range Analyze(root, []model.ArtifactReport{a, b, dead}, Options{Zombie: true}) {
		if it.UnattendedSafe() {
			t.Errorf("item %s (%s) became unattended-safe — confirm that is intended, then update this test",
				it.ID, it.Kind)
		}
	}
}

func hasBlocker(it model.CleanItem, want string) bool {
	for _, b := range it.Blockers {
		if b == want {
			return true
		}
	}
	return false
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestConfidenceCeiling_KeepsUnattendedUnreachable proves — rather than samples — why
// model.UnattendedSafe cannot fire today.
//
// That predicate needs TierAuto AND ConfHigh. TierAuto is produced in exactly one place, the zombie
// check, and its confidence is whatever usageSource.confidence() returns. That function's entire
// input space is two booleans, so iterating all four states is exhaustive: if none of them yields
// "high", then no item this package can construct is ever unattended-safe, for any fixture.
//
// INVERTED ASSERTION. The day someone decides transcript evidence is strong enough to call high,
// this fails — and that is the point, because three decisions then have to be made on purpose
// instead of inherited:
//
//  1. does `clean --apply` keep gating on Executable(), or move to UnattendedSafe()?
//  2. does the report's suppressed "N qualify for an unattended batch" line come back?
//  3. should an unattended command exist at all — there is none today, which is why the predicate
//     is named for the situation rather than for a flag.
func TestConfidenceCeiling_KeepsUnattendedUnreachable(t *testing.T) {
	for _, src := range []usageSource{
		{},
		{prompts: true},
		{transcripts: true},
		{prompts: true, transcripts: true},
	} {
		if got := src.confidence(); got == "high" {
			t.Errorf("usageSource%+v now reports %q. Zombie is the only TierAuto detection, so a high "+
				"confidence here makes model.UnattendedSafe reachable for the first time. Decide on "+
				"purpose: (1) does --apply still gate on Executable? (2) does the report's batch line "+
				"come back? (3) should an unattended command exist? Then update this test.", src, got)
		}
	}
}

// TestUsage_SkillListingIsNotEvidenceOfUse is the measured defect that made the zombie check inert.
//
// Claude Code writes an attachment record holding the roster of every AVAILABLE skill each time that
// roster changes — 21 of them in one real 13 MB session. The check asks "does this name appear
// anywhere in the transcript", so being INSTALLED was enough to look USED, and on any machine that
// keeps transcripts no skill could ever be reported as a zombie.
//
// The line below is the real record shape, trimmed. The assertion is that a roster mentioning a
// skill does not count as that skill having been used.
func TestUsage_SkillListingIsNotEvidenceOfUse(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	listing := `{"type":"attachment","attachment":{"type":"skill_listing","skillCount":2,` +
		`"names":["dataviz","docx"]},"sessionId":"s","timestamp":"t"}`
	if err := os.WriteFile(filepath.Join(sessions, "s.jsonl"), []byte(listing+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, src := usedNames(root, []string{"dataviz", "docx"})
	if !src.transcripts {
		t.Fatal("a transcript is present, so the source must record it")
	}
	for _, n := range []string{"dataviz", "docx"} {
		if ev.used(n) {
			t.Errorf("%q was only LISTED as available, never invoked — counting that as use makes the "+
				"zombie check unable to report anything on a machine that keeps transcripts", n)
		}
	}
}

// The inverse: a line that merely CONTAINS the marker, without being one of those records, must
// still be searched. Dropping anything unidentified would silently shrink the evidence, which is the
// same class of error in the opposite direction.
func TestUsage_OnlyTheListingRecordIsSkipped(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"user","message":{"content":"remind me how skill_listing works with dataviz"}}` + "\n" +
		`not json at all but mentions skill_listing and docx` + "\n"
	if err := os.WriteFile(filepath.Join(sessions, "s.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, _ := usedNames(root, []string{"dataviz", "docx"})
	for _, n := range []string{"dataviz", "docx"} {
		if !ev.used(n) {
			t.Errorf("%q appears in an ordinary line that happens to contain the marker; only the "+
				"attachment record itself may be skipped", n)
		}
	}
}

// TestUsage_SubagentTranscriptsAreRead: beside <session>.jsonl Claude Code keeps
// <session>/subagents/<agent>.jsonl, and the walk skipped every directory — so a skill invoked only
// inside a subagent left evidence nothing could see. Measured on a real session: 5 subagent
// transcripts, 1.6 MB, beside one 13 MB main transcript.
func TestUsage_SubagentTranscriptsAreRead(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "projects", "proj", "sess", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","message":{"content":"invoking dataviz now"}}` + "\n"
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, src := usedNames(root, []string{"dataviz", "docx"})
	if !src.transcripts {
		t.Error("a subagent transcript is a transcript; the evidence tier must reflect that")
	}
	if !ev.used("dataviz") {
		t.Error("a skill used only inside a subagent read as never used")
	}
	if ev.used("docx") {
		t.Error("docx appears nowhere and must stay unused")
	}
}

// --- calibration -------------------------------------------------------------------------------
//
// The precise predicate ("appears as input.skill on a Skill tool_use") is strictly better than the
// loose one, and strictly more dangerous: it errs toward calling a skill you are USING deletable.
// It is therefore gated on a proof assembled from the operator's own transcripts. These four tests
// pin the gate; the fifth pins the file ordering the gate made load-bearing.

// listingLine is the measured roster record, trimmed to the fields that matter.
func listingLine(names ...string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = `"` + n + `"`
	}
	return `{"type":"attachment","attachment":{"type":"skill_listing","skillCount":` +
		itoa(len(names)) + `,"names":[` + strings.Join(q, ",") + `]},"sessionId":"s"}`
}

// invokeLine is the measured invocation record.
func invokeLine(skill string) string {
	return `{"type":"assistant","message":{"content":[` +
		`{"type":"tool_use","name":"Skill","input":{"skill":"` + skill + `"}}]}}`
}

func writeTranscript(t *testing.T, root, name string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// An invocation recorded under a name Claude Code's own roster does not contain is the observable
// signature of "the recorded name is NOT the directory name". That is the premise precise matching
// rests on, so seeing it fail must switch precise matching OFF — and say so, because falling back
// narrows nothing but silently changes what a "never used" verdict means.
func TestUsage_CalibrationDeclinesOnUnknownInvocationName(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl",
		listingLine("dataviz", "docx"),
		invokeLine("some-alias-we-cannot-account-for"),
		`{"type":"user","message":{"content":"use dataviz for this"}}`,
	)

	ev, src := usedNames(root, []string{"dataviz", "docx"})
	if src.cal.active() {
		t.Fatal("an invocation name absent from the roster disproves the premise; precise matching " +
			"must not engage")
	}
	if !ev.used("dataviz") {
		t.Error("with calibration declined the loose predicate must still apply; dataviz is mentioned " +
			"and must read as used, or the fallback is not a fallback")
	}
	notes := usageCoverageNotes(src)
	if len(notes) != 1 || !strings.Contains(notes[0].Detail, "declined") {
		t.Errorf("declining to use the sharper predicate is a change in what the answer means and "+
			"must be disclosed (invariant #5); got %d note(s): %+v", len(notes), notes)
	}
	if notes[0].ID != "" || len(notes[0].Locators) != 0 {
		t.Error("a note addresses nothing and must carry no ID and no locator")
	}
}

// The other side of the same gate: every invocation name accounted for by the roster proves, on
// this machine, that invocations speak the roster's namespace — and the roster was measured to
// speak directory names. Precise matching then engages, and a skill that is merely MENTIONED stops
// counting as used. That is the entire point of the change.
func TestUsage_CalibrationEnablesExactMatch(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl",
		listingLine("dataviz", "docx", "pptx"),
		invokeLine("docx"),
		`{"type":"user","message":{"content":"maybe dataviz would help here"}}`,
	)

	ev, src := usedNames(root, []string{"dataviz", "docx", "pptx"})
	if !src.cal.active() {
		t.Fatal("every invoked name appears in the roster; the premise holds and precise matching " +
			"must engage")
	}
	if !ev.used("docx") {
		t.Error("docx was actually invoked")
	}
	if ev.used("dataviz") {
		t.Error("dataviz was only TALKED ABOUT. Counting a mention as a use is the imprecision " +
			"calibration exists to remove")
	}
	if ev.used("pptx") {
		t.Error("pptx appears nowhere at all")
	}
}

// A skill absent from every roster record (installed after the last session, say) keeps the loose
// predicate even while calibration is active. The fallback is per-skill so that one new install
// cannot silently switch the whole run back — and, in the other direction, so that a skill the
// roster never vouched for is never judged by the sharp predicate.
func TestUsage_UnrosteredSkillKeepsTheLoosePredicate(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl",
		listingLine("docx"),
		invokeLine("docx"),
		`{"type":"user","message":{"content":"brand-new-skill looks useful"}}`,
	)

	ev, src := usedNames(root, []string{"docx", "brand-new-skill"})
	if !src.cal.active() {
		t.Fatal("calibration should be active: the only invoked name is in the roster")
	}
	if !ev.used("brand-new-skill") {
		t.Error("no roster record vouches for this name, so the proof does not cover it and the " +
			"loose predicate must still apply")
	}
	notes := usageCoverageNotes(src)
	if len(notes) != 1 || !strings.Contains(notes[0].Detail, "roster record") {
		t.Errorf("skills excluded from the sharper predicate must be disclosed; got %+v", notes)
	}
}

// Precise matching replaces only the TRANSCRIPT leg. A skill the operator typed is used whatever
// the transcript records, and dropping that would reintroduce the harmful error direction for every
// slash invocation.
func TestUsage_TypedPromptSurvivesExactMatching(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl", listingLine("docx", "pptx"), invokeLine("docx"))
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"),
		[]byte(`{"display":"/pptx make me a deck","project":"/tmp"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, src := usedNames(root, []string{"docx", "pptx"})
	if !src.cal.active() {
		t.Fatal("calibration should be active")
	}
	if !ev.used("pptx") {
		t.Error("the operator typed it; precise matching governs the transcript leg only")
	}
}

// With no invocations at all — an older Claude Code, or a machine whose transcripts predate the
// record — there is no proof, so behaviour must be exactly what it was.
func TestUsage_NoInvocationsChangesNothing(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl",
		listingLine("dataviz", "docx"),
		`{"type":"user","message":{"content":"dataviz please"}}`,
	)

	ev, src := usedNames(root, []string{"dataviz", "docx"})
	if src.cal.active() {
		t.Fatal("no invocation was observed, so nothing was proven")
	}
	if !ev.used("dataviz") {
		t.Error("loose matching must still apply")
	}
	if ev.used("docx") {
		t.Error("docx is only in the roster, which is not evidence of use")
	}
	if n := usageCoverageNotes(src); len(n) != 0 {
		t.Errorf("nothing was declined and nothing was truncated; no note is warranted: %+v", n)
	}
}

// maxUsageFiles changed meaning when the roster fix landed: while every installed skill matched
// every transcript, WHICH files were read could not change an answer. Now it can, which makes
// ReadDir's session-UUID lexical order a bug — the 64 files whose names sort first have nothing to
// do with the 64 most recent sessions — and makes hitting the cap something the operator must be
// told about.
func TestUsage_NewestTranscriptsWinTheBudget(t *testing.T) {
	root := t.TempDir()
	// Lexically first, chronologically last: exactly the file the old order read and the new one
	// must not.
	old := writeTranscript(t, root, "aaa-oldest.jsonl",
		`{"type":"user","message":{"content":"dataviz was great"}}`)
	base := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(old, base, base); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxUsageFiles; i++ {
		p := writeTranscript(t, root, "zzz-"+itoa(i)+".jsonl", `{"type":"user","message":{"content":"hi"}}`)
		ts := time.Now().Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	ev, src := usedNames(root, []string{"dataviz"})
	if src.skipped != 1 {
		t.Fatalf("one transcript should have been left unread, got skipped=%d", src.skipped)
	}
	if ev.used("dataviz") {
		t.Error("the only mention is in the OLDEST transcript, which the budget must spend on the " +
			"newest sessions instead — lexical order let it in by accident")
	}
	notes := usageCoverageNotes(src)
	if len(notes) != 1 || !strings.Contains(notes[0].Detail, "most recent") {
		t.Errorf("a truncated read narrows what 'never used' means and must be disclosed; got %+v", notes)
	}
}

// Skills nest. A real environment has skills/synced/docx, whose artifact name is "synced/docx"
// while Claude Code's roster calls it "docx" — measured on the first machine this ran against,
// where the mismatch made calibration inert without a single test noticing.
func TestUsage_NestedSkillNameMatchesTheRoster(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl",
		listingLine("docx", "pptx"),
		invokeLine("docx"),
		`{"type":"user","message":{"content":"pptx sounds handy"}}`,
	)

	ev, src := usedNames(root, []string{"synced/docx", "synced/pptx"})
	if !src.cal.active() {
		t.Fatal("calibration should be active")
	}
	if src.cal.fellBack != 0 {
		t.Fatalf("both skills are in the roster under their leaf name; none should have fallen "+
			"back, got %d", src.cal.fellBack)
	}
	if !ev.used("synced/docx") {
		t.Error("skills/synced/docx WAS invoked, recorded by the roster's leaf name")
	}
	if ev.used("synced/pptx") {
		t.Error("only mentioned, never invoked — precise matching must apply here too")
	}
}

// A "never used" verdict is only as wide as the history behind it, and on a real machine that
// history was 15 sessions — while the wording said "any session transcript", which reads as an
// exhaustive search. The operator knows something the tool does not (when they last reached for the
// skill) and can only apply it if the number is on the page.
func TestZombie_DetailStatesHowMuchHistoryItSaw(t *testing.T) {
	root := t.TempDir()
	dead := skill(t, root, "deadskill", "---\nname: deadskill\ndescription: b\n---\n", nil)
	for i := 0; i < 3; i++ {
		writeTranscript(t, root, "s"+itoa(i)+".jsonl", `{"type":"user","message":{"content":"hello"}}`)
	}

	items := allOf(Analyze(root, []model.ArtifactReport{dead}, Options{Zombie: true}), "zombie")
	var found bool
	for _, it := range items {
		if len(it.Targets) == 1 && it.Targets[0] == "deadskill" {
			found = true
			if !strings.Contains(it.Detail, "3 session transcript(s)") {
				t.Errorf("the verdict covers 3 sessions and must say so; got %q", it.Detail)
			}
			if !strings.Contains(it.Detail, "entire history here") {
				t.Errorf("the wording must say the number IS the whole history, not a sample; got %q", it.Detail)
			}
		}
	}
	if !found {
		t.Fatal("deadskill appears in no transcript and must be reported")
	}
}

// The plan and the executor must answer ONE question. A skill installed as skills/x -> shared/x
// resolves inside root but outside skills/: the listing used to show it with no blocker, count it
// in "N executable", and let `--apply` refuse it afterwards. Blockers exist so that "this cannot be
// moved" is visible while the operator still has a decision to make.
func TestZombie_SymlinkOutOfSkillsIsBlockedInThePlan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	real := filepath.Join(root, "shared", "x")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "skills", "x")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"display":"unrelated"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// The artifact carries the RESOLVED path, which is what the collector produces.
	art := model.ArtifactReport{Kind: model.KindSkill, Name: "x", Path: real}
	items := allOf(Analyze(root, []model.ArtifactReport{art}, Options{Zombie: true}), "zombie")
	var found bool
	for _, it := range items {
		if len(it.Targets) != 1 || it.Targets[0] != "x" {
			continue
		}
		found = true
		if it.Executable() {
			t.Error("the executor refuses this; the plan must not advertise it as executable")
		}
		if !listedBlocker(it.Blockers, model.BlockerNotUnderSkills) {
			t.Errorf("blockers = %v, want %s", it.Blockers, model.BlockerNotUnderSkills)
		}
		if !strings.Contains(it.Detail, "only skills/ is quarantined") {
			t.Errorf("the reason belongs in the listing, not only in the failed run: %q", it.Detail)
		}
	}
	if !found {
		t.Fatal("the skill should still be REPORTED; unmovable is not undetected")
	}
}

// maxUsageBytes read the HEAD of an oversized transcript. Measured on a live 16.5 MB one: the cap
// fell at 51%, the session's only Skill invocation sat at 13.60 MB, and so the evidence calibration
// exists to find was in the file and never read. The error direction is the harmful one — less read
// means fewer names matched as used, which means MORE skills reported as zombies.
func TestUsage_OversizedTranscriptIsReadFromTheEnd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// oldskill is mentioned only at the very start, newskill only at the very end, and the filler
	// between them pushes the file past the cap.
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"content":"oldskill was handy"}}` + "\n")
	filler := `{"type":"user","message":{"content":"` + strings.Repeat("x", 4096) + `"}}` + "\n"
	for b.Len() < maxUsageBytes+(1<<20) {
		b.WriteString(filler)
	}
	b.WriteString(`{"type":"user","message":{"content":"newskill was handy"}}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, src := usedNames(root, []string{"oldskill", "newskill"})
	if !ev.used("newskill") {
		t.Error("the most recent part of the transcript must be the part that is read")
	}
	if ev.used("oldskill") {
		t.Error("a mention beyond the cap cannot be found; if this passes the fixture is too small")
	}
	if src.clipped != 1 {
		t.Fatalf("clipped = %d, want 1", src.clipped)
	}
	notes := usageCoverageNotes(src)
	var told bool
	for _, n := range notes {
		if strings.Contains(n.Detail, "most recent part") && strings.Contains(n.Detail, "MORE zombies") {
			told = true
		}
	}
	if !told {
		t.Errorf("reading part of a file narrows the verdict and must say so, including which way "+
			"it errs; got %+v", notes)
	}
}

// A file under the cap is read whole and says nothing about clipping.
func TestUsage_SmallTranscriptIsNotReportedAsClipped(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "s.jsonl", `{"type":"user","message":{"content":"tiny"}}`)
	_, src := usedNames(root, []string{"anything"})
	if src.clipped != 0 {
		t.Errorf("clipped = %d on a small file", src.clipped)
	}
	for _, n := range usageCoverageNotes(src) {
		if strings.Contains(n.Detail, "most recent part") {
			t.Error("a file read in full must not be announced as partial")
		}
	}
}

// TestStaleRef_IgnoresLinksInCode: a link-shaped example inside backticks or a fenced block is
// documentation of a format, not a reference — the desktop's consolidate-memory skill spells out
// `[Title](file.md)` and was reported as broken on every machine. A real link stays flagged.
func TestStaleRef_IgnoresLinksInCode(t *testing.T) {
	body := "Index entries look like `- [Title](file.md) — hook`.\n\n```md\n- [Other](template.md)\n```\n\nSee [the guide](guide.md) for details.\n"
	got := staleRefs(t.TempDir(), body)
	if len(got) != 1 || got[0] != "guide.md" {
		t.Fatalf("staleRefs = %v, want only guide.md (file.md and template.md are inside code)", got)
	}
	in := "a `x` b ```c\nd``` e `unterminated"
	if s := stripCode(in); len(s) != len(in) || strings.Count(s, "`") != 1 || !strings.Contains(s, "\n") || !strings.HasSuffix(s, "e `unterminated") {
		t.Errorf("stripCode = %q: want same length, the newline kept, only the unterminated backtick left", s)
	}
}
