<!-- SPDX-License-Identifier: MIT -->
# Proposals

Each proposal answers "what is to be done, how far it has to go to count as done, and what is explicitly not done". Start
a new one from [TEMPLATE.md](TEMPLATE.md). Numbers are three digits, increasing and globally unique; when you create the
file, take the largest number in the index on `origin/main` and add one. Numbering in this repository restarts at
**001**; P-001 to P-041 of the former repository `agent-guard` are that repository's history and are not continued here.

Which changes need a proposal first: a change to a rule, an invariant, the data model or the collection surface, or
anything where "how far it has to go to count as done" is not obvious. A typo, a CI line or a one-line comment does not.
Proposals are written in English, like their commits and pull requests. [../../CONTRIBUTING.md](../../CONTRIBUTING.md) carries a
short summary of these rules.

## The directory is the state

The subdirectory a file sits in is its state; the file does **not** carry a "Status:" line, because two sources of truth
always drift. A state change is one `git mv`; the file name stays the same, and `git log --follow` and
`git log --grep P-NNN` work as before. **draft and design exist only on the `p/NNN-slug` branch**; `main` has only
`complete/` and `rejected/`. To see what is in progress, look at `git branch -r --list 'origin/p/*'` and the open PRs.

| Directory | Meaning | What the file contains |
|---|---|---|
| `draft/` | Proposed; the maintainer has not yet said whether it is worth designing (on the branch) | Only the problem, source, consequences and initial direction |
| `design/` | The maintainer said it is worth designing. Add the Done criteria, Out of scope, Must not claim and Work items, and ask the Open questions **all at once**; once the maintainer has answered, it is accepted and implementation follows; it stays here until the PR is opened (on the branch) | All six sections |
| `complete/` | Finished, and the maintainer said ship it; the "Done" section is filled in, with evidence lines, and reaches `main` with the PR. **Release records also go here** (`NNN-release-x.y.z.md`, not through draft/design) | Six sections + "Done"; a release record holds the included Ps, the changelog, what changed where, and evidence lines |
| `rejected/` | Finished or half done and the maintainer said no, or the same thing as another proposal; also reaches `main` with the PR | The body gives the reason for rejection, or which proposal it was merged into |

"In progress" is not a directory: an existing `p/NNN-*` branch means in progress. Each of the four directories carries a
`.gitkeep`, so it exists even when empty.

## Commits and tracing back

- Branch name `p/NNN-slug`; every commit message ends with `(P-NNN)`. This repository **installs no hook to enforce**
  either of these; review does.
- From code to proposal: `git blame` → the `(P-NNN)` at the end of the commit message → this directory (look for the
  number in the four subdirectories).
- From proposal to code and release: `git log --grep 'P-NNN'`; `git tag --contains <merge-commit>`;
  the "Done" section at the end of the file records the merged PR and the release directly.

## Index

| P | Title | Source | PR / Release |
|---|---|---|---|
| [001](complete/001-judge-usage-in-json.md) | The judge's tokens, triage calls and retries never reach the report; the cost can only be copied from stderr | The judge's usage is printed only to stderr, and with `--quiet` (every Downloads item) it is nowhere; ported from P-042 in the former repository | PR #19; v0.19.0 |
| [002](complete/002-rules-version.md) | A report does not say which version of the rules produced it, so two reports cannot tell whether the rules changed or the input did | New finding (2026-10-09): when two scans score differently, the report cannot say whether the rule table changed; ported from P-043 in the former repository | PR #21; v0.19.0 |
| [003](complete/003-zero-dial-test.md) | No test pins "never connects out", yet the baselines template says one does | New finding (2026-10-09); ported from P-044 in the former repository agent-guard | PR #22; v0.19.0 |
| [004](complete/004-check-llm.md) | A pre-install check cannot use the judge; CI users have to take the detour through scan --root | `check` is always static, so a pre-install check cannot use the judge; `scan` already offers `--llm` for Downloads content that is just as untrusted; ported from P-047 in the former repository | PR #26; v0.19.0 |
| [005](complete/005-judge-egress-paths.md) | A BYO judge sends the user name, absolute paths and env values without their key names to the model vendor | `--llm` excerpts carry absolute home-directory paths and the user name, and MCP env values are sent without their key names; ported from P-045 in the former repository | PR #24; v0.19.0 |
| [006](complete/006-judge-rendering.md) | The model's whole evidence passage is rendered as evidence, and a triage reason can escape the markdown | The judge snippet is the model's whole evidence passage, the triage reason is not redacted and can escape the code span, and model text has no length cap; ported from P-046 in the former repository | PR #23; v0.19.0 |
| [007](complete/007-gate-pending-survives.md) | A skill approved in the gate's prompt is asked about again on the next load: the approval was never recorded | Each hook event is a new process, and what is held in pending is never read back; ported from P-049 in the former repository | PR #18; v0.19.0 |
| [008](complete/008-undo-hint-pastes.md) | The undo command the gate prints after an approval fails when pasted as is: the hash is followed by an ellipsis | The undo command printed after accepting a risk fails when pasted, and `forget ""` deletes the only approval; ported from P-050 in agent-guard | PR #17; v0.19.0 |
| [009](complete/009-content-hash-three-kinds.md) | Hook, MCP and permission artifacts have no hash, so the gate and the reputation allowlist always see them as "unknown" | Three artifact kinds have an empty hash, so the gate's SessionStart and the reputation allowlist always judge them unknown; ported from P-051 in the former repository | PR #29; v0.19.0 |
| [010](complete/010-relative-root-hook-scripts.md) | With a trailing slash or a relative path in --root, scripts referenced by hooks and permission grants are not followed, and the same configuration scores higher | When `--root` is written as `<abs>/`, `.`, `./` or `home/.claude`, the `~/…` scripts referenced by hooks and permission grants are not read, and the score goes 69 → 100; ported from P-052 in agent-guard | PR #27; v0.19.0 |
| [011](complete/011-approve-empty-hash.md) | aguard approve prints approved even for something with no content hash, and actually stores nothing | `approve` still reports approved and exits 0 for a worst artifact that has no hash, and the gate's clean branch also says trusted for an empty hash; ported from P-053 in agent-guard | PR #20; v0.19.0 |
| [012](complete/012-collect-anchors-root.md) | With a relative --root, a skill installed as a symlink is not collected at all, and `--root .` also misses the configuration under home; the CI template's `.mcp.json` is read only by chance | collect calls `Clean` but not `Abs`: a relative form drops skills installed as symlinks, and `--root .` takes the wrong home; the CI template reads the repository's top-level `.mcp.json` only because home happens to equal the root; ported from P-055 in agent-guard | PR #30; v0.19.0 |
| [013](complete/013-artifact-notes-rendered.md) | When settings.json fails to parse, the terminal and markdown reports say "looks safe": an artifact's own dim-0 notes are never rendered | collect attaches `PARSE-000` to the artifact, the three human-readable renderers render only scan-level notes, and an unparseable `settings.json` is reported as "looks safe … Nothing was found to check" (invariant #5); ported from P-054 in agent-guard | PR #28; v0.19.0 |
| [014](complete/014-hook-outside-snippet-redacted.md) | In the evidence of the hook-outside-the-boundary finding, the resolved path after the arrow is not redacted | The resolved path after the arrow in `HOOK-002` and the registry address in the `Why` of `SUP-006` are not redacted; ported from P-056 in agent-guard | PR #25; v0.19.0 |
| [015](complete/015-rules-frontmatter-first.md) | A licence comment pushes the frontmatter of ten rule files off line 1: path-scoped loading stops working, and the scope test no longer checks the globs | New finding: the first public commit added a licence comment above the frontmatter of ten path-scoped rule files; since then the scope test checks no glob, and Claude Code no longer loads them by path | PR #32; v0.19.0 |
| [016](complete/016-zip-check-reproducible.md) | Checking the same zip twice gives different SARIF: the random extraction directory name gets into the uri, the artifact and the fingerprint, and Code Scanning opens a batch of new alerts on every run | Checking the same zip twice gives different reports: the random extraction directory name leaks into the artifact name, the uri and the fingerprint, and Code Scanning opens new alerts on every CI run; `approve x.zip` records a temporary path that has already been deleted; a root-shaped zip treats the shared `$TMPDIR` as home; ported from P-048 in agent-guard | PR #31; v0.19.0 |
| [017](complete/017-checked-line-and-scan-notes.md) | The summary contradicts itself: checking a file lists findings while saying "Nothing was found to check"; the parts of loaded content that were not read leave "looks safe" untouched | Follow-up recorded in P-013: the Checked sentence is derived only from the inventory counts, so `check <file>` / `check <dir>` / a root with only `CLAUDE.md` / an unreadable `settings.json` all say "Nothing was found to check"; detect's scan-level `COV-000` about loaded content (unreadable subdirectories, oversized files, hook scripts not followed) does not hedge the headline | PR #39; v0.19.0 |
| [018](complete/018-collect-notes-redacted.md) | A token in an import line, a plugin name or the name of an entry inside a tree reaches the report verbatim through several notes: those evidence snippets are not redacted | Left by P-014's "Out of scope" and its open question 3: collect's notes, `unreadableNote` and the `GATE-001` snippet splice file text or configuration values into the evidence verbatim, without going through `Redact` | PR #41; v0.19.0 |
| [019](complete/019-raw-root-entry-points.md) | Writing the same directory another way gives another answer: `clean --root .` refuses to undo, `check .` does not read it as a root layout, and with a relative form the gate lets plugin skills through unaudited | Entry points with the same root cause, left by P-012: `clean` judges the trash to be out of bounds, `check`/`hash` route by string, and the gate and `version` take home from the raw root; decided by the maintainer (2026-10-09) to combine them into one | PR #40; v0.19.0 |
| [020](complete/020-excerpt-padding-evasion.md) | Pad an instruction line with whitespace and the judge's excerpt holds only an omission marker: the judge cannot see exactly the sentence it exists to read | Follow-up recorded in P-005 and P-006: the excerpt is capped in bytes, so an instruction line padded with several thousand bytes of whitespace (including Unicode whitespace and zero-width characters) is replaced by an omission marker, or cut to a whitespace-only prefix, before it is sent | PR #38; v0.19.0 |
| [021](complete/021-plugin-mcp-unscanned.md) | A plugin's bundled MCP server never goes through the rules: the same configuration scores 75 when written by hand into ~/.claude.json and 100 when installed with a plugin | An existing gap recorded in P-009's open question 6: detect and the judge look up the entry in `mcpServers` by the artifact name with its ` (plugin …)` suffix, find nothing, get zero units, and record a clean 100 | PR #37; v0.19.0 |
| [022](complete/022-bench-fold-reads-judge-usage.md) | When the benchmark folds a judge run, the triage call count and per-question usage are inferred: aguard's JSON already reports the real numbers, and the rig reads none of them | Follow-up recorded in P-001: `triage_calls` / `questions` in `judge.jsonl` are derived from the documentation, the rig does not read the real numbers in the `--json` judge summary, and raw/ also writes unreported fields as 0 | PR #35; v0.19.0 |
| [023](complete/023-judge-redirect.md) | If the judge endpoint answers with a redirect, the API key goes out in plain text, or an excerpt of the scanned content goes to a host the user never configured | Follow-up recorded by P-003 in its "Out of scope": the judge's client uses Go's default redirect policy, and following a 30x no longer goes through `CheckEndpoint` | PR #34; v0.19.0 |
| [024](complete/024-frontmatter-leading-bytes.md) | A rule file with a blank line or a BOM before its frontmatter is labelled (path-scoped) in the report, yet Claude Code loads it every session | P-015 open question 5: `splitFrontmatter` strips the BOM and leading whitespace before looking for `---`, while Claude Code 2.1.107 only accepts frontmatter that starts at the first byte; `SKILL.md` uses the same parser, so measure first | PR #36; v0.19.0 |
| [025](complete/025-release-0.19.0.md) | Release record v0.19.0: P-001 to P-024 | The maintainer said "release v0.19.0" (2026-10-09) | PR #43; v0.19.0 |
