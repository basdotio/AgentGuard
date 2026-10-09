<!-- SPDX-License-Identifier: MIT -->
# 015 — A licence comment pushes the frontmatter of ten rule files off line 1: path-scoped loading stops working, and the scope test no longer checks the globs

- **Source**: new finding (2026-10-09). The repository's first public commit `2b4c2a7` added a licence comment above the
  `paths:` frontmatter of ten path-scoped rule files; since then the scope test checks not a single glob, and Claude Code
  no longer loads these ten files by path
- **Depends on**: none
- **Branch**: `p/015-rules-frontmatter-first`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Ten files under `.claude/rules/` — `detect` `gate` `hash` `judge` `npm` `pipeline` `plugin` `report` `reputation` `score` —
have `<!-- SPDX-License-Identifier: MIT -->` on line 1 and `---` only on line 2. The line came in with `2b4c2a7` (this
repository's first commit); the ten files already had this shape in that commit. `conventions.md` and `invariants.md`
have no frontmatter, load every session by design, and are not affected.

**Claude Code accepts only frontmatter that starts on line 1.** Its glossary's definition of frontmatter says
"The opening `---` must be the file's first line." (code.claude.com/docs/en/glossary#frontmatter);
the description of rule-file frontmatter (code.claude.com/docs/en/memory#rules-frontmatter-reference) also says it sits
between two `---` lines at the top of the file; the same page says that rules without `paths` load at startup.

**Measured on this machine (Claude Code 2.1.107)**: an `InstructionsLoaded` hook records the `file_path` and
`load_reason` of every load, and `claude -p` runs in a copy extracted from `git archive origin/main` (dec64ca). **All** 12
rule files load with `session_start`, these ten included. (The command exits before it sends a request to the model — the
CLI is not logged in on this machine — and all load events have fired before that, so this measurement does not depend on
the model.) Several shapes were also measured in an empty directory holding only rule files, each with `paths:` pointing
at a directory that does not exist:

| What comes before the opening `---` | Loaded with `session_start`? |
|---|---|
| One HTML comment line (this repository's shape) | Yes — `paths:` is ignored |
| One blank line | Yes |
| A UTF-8 BOM | Yes |
| One `# Title` line | Yes |
| Nothing, `---` on line 1 (LF) | No |
| Nothing, `---` on line 1 (CRLF) | No |
| No frontmatter | Yes (control) |

So these ten files — 797 lines, about 84 KB of guard points meant to be "read only when the matching files are touched" —
today enter the context in full at the start of every session in this repository; yet the "When loaded" table in
`CLAUDE.md` and `docs/README.md` both say they load by path.

**The scope test went blind at the same time.** `frontmatterPaths` in `cmd/aguard/claude_rules_test.go` returns `nil`
when the file does not start with `---\n` — "no frontmatter, an always-loaded rule" — so for these ten files
`TestClaudeRulesAreScopedToExistingPaths` checked only the 200-line limit and not a single glob.
The test's file header comment names exactly what it is meant to prevent: a glob that matches no file makes that rule
"never read, silently". On dec64ca, changing `internal/score/**` in `score.md` to `internal/scorex/**` and running this
test: **PASS**.

The consequence has two layers: first, context cost — every session reads about 84 KB more, and guard points for
unrelated packages are mixed in with the task at hand; second, this repository's only check for "will a rule never be
read" is in effect switched off, so if a later rename or directory move leaves a glob matching nothing, nothing will
report it (and only once the comment is moved and the frontmatter takes effect does a glob that matches nothing really
make that rule unreadable).

## Initial direction

First the scope test learns to report "frontmatter not on line 1" (written red); then the licence comment of the ten files
moves to the line after the `---` that closes the frontmatter (kept, not deleted, line count unchanged). Only these ten
files and `cmd/aguard/claude_rules_test.go` change. If, once the glob check takes effect again, some glob matches nothing,
it is fixed in this proposal only when the replacement is unambiguous; otherwise stop and ask.

## Done criteria

All fixtures are built on the spot in `t.TempDir()`, the same way as the existing tests.

- [x] `TestClaudeRulesProblemsAreCaught` (`cmd/aguard/claude_rules_test.go`, extended) pins: the four shapes from the
  table above that were measured to make Claude Code ignore `paths:` — one HTML comment line, one blank line, one
  `# Title` line or a UTF-8 BOM before the `---` — each report one `frontmatter must start on line 1`; their globs all
  match, so this is the only problem for each; exact count 3 → 7. Red when only the fixtures are added and the check is
  not changed
- [x] Reverse assertion (same test): none of the following is reported —
  an always-loaded rule with an HTML comment at the top and no frontmatter (the shape of `invariants.md`);
  two `---` divider lines in the body with a sentence like `Note: …` between them, prose that YAML can read as a mapping;
  a `paths:` example in a fenced code block (its glob matches nothing, and it must not be reported as matching nothing
  either);
  frontmatter on line 1 with the licence comment on the line after the closing `---` (the shape of the ten files after W2);
  frontmatter with CRLF line endings that starts on line 1 (Claude Code accepts it, so it must not be reported as "not on
  line 1", open question 4).
  The three existing problems (a glob that matches nothing, an empty `paths:`, too long) are still reported
- [x] `TestClaudeRulesAreScopedToExistingPaths`: after W1 red in this repository, reporting **exactly these ten**, one
  each; green after W2
- [x] The glob check really takes effect again: after W2, temporarily changing `internal/score/**` in `score.md` to
  `internal/scorex/**` → red, `matches no file`; the same change is green on dec64ca (the "Problem" section). The mutation
  is reverted after the run and not committed
- [x] Each of the ten files has the same line count before and after, and the change to each is just one line moving
  (after `sort`, byte-identical to `origin/main`)
- [x] The "When loaded" column in `CLAUDE.md` agrees entry by entry with each file's `paths:`
- [x] The same `InstructionsLoaded` measurement with Claude Code 2.1.107: after W2 the only rules loaded with
  `session_start` are `conventions.md` and `invariants.md`
- [x] `make verify` green; `go version` does not switch toolchains, line 2 of `go.mod` is still `go 1.23.5`, no
  dependency added

## Out of scope

- **The licence comment of no other `.md` changes**: the first line of `CLAUDE.md`, `CONTRIBUTING.md`, `invariants.md`,
  `docs/**` and so on stays as it is — they have no frontmatter, so a comment on the first line does no harm.
  `conventions.md` never had a licence comment, and none is added
- **Rule content does not change**: apart from that one line moving, not a byte of the ten files changes
- **No Go code outside `claude_rules_test.go` changes**. In particular, the product's `internal/parse/frontmatter.go`
  does not change (see open question 5)
- The scope test is not taught to accept frontmatter with CRLF line endings (open question 4)
- No note saying "frontmatter must be on line 1" is added to `CLAUDE.md` or the rule files: the test's error message is
  the note (open question 6)
- No dependency added; `go.mod` does not change

## Must not claim

- Do not say "these ten rules now load when a matching file is touched": what was measured is only that they **no
  longer** load with `session_start`; a path-triggered load (`path_glob_match`) needs a model session that really reads
  files, and this proposal did not measure it
- Do not say "every version of Claude Code behaves like this": only 2.1.107 on this machine was measured; the
  documentation agrees with the measurement
- Do not say past sessions missed guard points because of this: the direction is the opposite — in the past they read
  **more**, with the ten files present every time; after the fix they **no longer** appear when the files touched do not
  match, which is what the table in `CLAUDE.md` was designed for
- Do not say the scope test used to be entirely broken: the 200-line limit was checked all along; only the globs went
  unchecked

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The scope test reports "frontmatter not on line 1"; four misplaced shapes, five reverse assertions; run red | `cmd: test — the rules scope check reports a paths frontmatter that does not start on line 1, which Claude Code ignores (P-015)` |
| 2 | The licence comment of the ten rule files moves to the line after the closing `---` | `rules: the licence comment moves below the frontmatter in the ten path-scoped files, so their paths take effect and get checked (P-015)` |
| 3 | This file, the index | `proposals: P-015 (P-015)` |

## Open questions

1. **Which shapes does the check recognise: only "a comment above", or "anything above"?**
   **Recommendation**: anything. Report when a block enclosed by a pair of `---` lines is found somewhere other than line
   1 and can be read as a YAML mapping with a `paths` key; also report when the file starts with a BOM; a block inside a
   fenced code block does not count (that is an example). Measurement shows that a comment, a blank line, a BOM and a
   heading all make `paths:` ineffective; recognising only the comment would miss the other three, and the BOM is
   invisible to the eye. Requiring "a `paths` key" keeps divider lines in the body from being reported by mistake: even
   when YAML reads the prose between two `---` lines as a mapping, it will not happen to have a `paths` key.
   **Decided (2026-10-09)**: as recommended.
2. **Should the licence comment move below the frontmatter, or be deleted?**
   **Recommendation**: move it to the line after the closing `---`. Deleting it would make these ten the only `.md` files
   in the repository without a licence marker; moving it does not change the line count, so `detect.md` is still 198
   lines and does not hit the 200-line limit.
   **Decided (2026-10-09)**: as recommended.
3. **What if some glob matches nothing once the glob check takes effect again?**
   **Recommendation**: fix it in this proposal only when the replacement is unambiguous (for example a path that one of
   this repository's own commits renamed), recording before and after in "Done"; when it is ambiguous, stop and ask; do
   not guess.
   **Decided (2026-10-09)**: as recommended.
4. **The scope test does not accept CRLF frontmatter (`frontmatterPaths` only accepts `---\n`), while Claude Code 2.1.107
   does (the last row of the table above); fix that at the same time?**
   **Recommendation**: no. This is a different kind of blindness, and there are no CRLF files in this repository's
   `.claude/rules/` (`git ls-files .claude/rules | xargs grep -l $'\r'` is empty); the new check does not report "not on
   line 1" for CRLF frontmatter that starts on line 1 either, so it gives no wrong hint (the reverse assertion `crlf.md`
   pins this).
   **Decided (2026-10-09)**: as recommended.
5. **The product's `parse.PathScoped` has a similar discrepancy; change it at the same time?**
   `splitFrontmatter` in `internal/parse/frontmatter.go` strips a BOM and leading whitespace before it looks for `---`,
   so `collectRules` names a rule whose first line is blank or a BOM `(path-scoped)`, while Claude Code 2.1.107 loads it
   every session (the table above). For this repository's shape (a comment above) it judges correctly. Measured:
   `aguard scan --json --inbox off` built from dec64ca, scanning the directory from the table above, names the blank-line
   and BOM files `(path-scoped)` and not the comment and heading files.
   **Recommendation**: not in this proposal. This is product behaviour, and `splitFrontmatter` also serves `SKILL.md`,
   where it has not been measured whether Claude Code tolerates leading whitespace; a change gets a proposal of its own,
   which measures `SKILL.md` first.
   **Decided (2026-10-09)**: as recommended.
6. **Should `CLAUDE.md` or one of the rules say "frontmatter must be on line 1"?**
   **Recommendation**: no. The test's error message explains the reason itself; per "Out of scope" the rule content does
   not change, and the table in `CLAUDE.md` needs no change either.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #32 (2026-10-09; find the sha with git log --grep P-015)
Released: pending release
Evidence: TestClaudeRulesProblemsAreCaught (cmd/aguard/claude_rules_test.go); red with only the fixtures added and the check unchanged: one missing problem "<name>: frontmatter must start on line 1" for each of the four misplaced shapes, want exactly 7 problems, got 3 → green after adding misplacedFrontmatter
Evidence: TestClaudeRulesAreScopedToExistingPaths (same file); after W1 red in this repository (dec64ca + W1): 10 problem(s), exactly one each for detect gate hash judge npm pipeline plugin report reputation score: "frontmatter must start on line 1 (Claude Code ignores it anywhere else, so this rule loads every session)" → green after W2
Evidence: the five reverse assertions (licensed.md, ruled.md, example.md, moved.md, crlf.md all not reported) each confirmed by mutation to bite: removing the fence skip → example.md reported (got 8); counting any pair of --- → ruled.md reported (got 8); removing the BOM branch → late-bom.md missed (got 6); also reporting frontmatter that starts on line 1 → crlf.md reported (got 8). Mutations reverted after the run, not committed
Evidence: the glob check takes effect again: internal/score/** in score.md temporarily changed to internal/scorex/** — PASS on dec64ca (blind) → FAIL after W2, paths entry "internal/scorex/**" matches no file; reverted, not committed. After W2 all 18 globs in the ten files match, none matches nothing, open question 3 was not triggered
Evidence: line counts of the ten files 198 91 25 41 78 151 77 51 69 16 → unchanged; each diff is 1+/1−, byte-identical to origin/main after sort; detect.md is still 198 lines
Evidence: the "When loaded" column of CLAUDE.md compared entry by entry with the paths: of the ten files: 10/10 agree, CLAUDE.md not changed
Evidence: Claude Code 2.1.107, an InstructionsLoaded hook recording load_reason, claude -p run in a copy extracted with git archive: on dec64ca 13 project-level files load with session_start (CLAUDE.md + all 12 rules) → on this branch 3 (CLAUDE.md, conventions.md, invariants.md). Path-triggered loads were not measured ("Must not claim")
Evidence: Out of scope — git diff --stat origin/main -- '*.md' is empty once the ten rules and docs/proposals are left out; -- '*.go' has only cmd/aguard/claude_rules_test.go; -- go.mod go.sum CLAUDE.md .claude/rules/conventions.md .claude/rules/invariants.md is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 is go 1.23.5, no new dependencies
```
