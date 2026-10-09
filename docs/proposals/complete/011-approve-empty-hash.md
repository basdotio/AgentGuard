<!-- SPDX-License-Identifier: MIT -->
# 011 — aguard approve prints approved even for things that have no content hash, and actually stores nothing

- **Source**: when the worst artifact has no hash (a config that failed to parse, a file that cannot be opened),
  `aguard approve` prints `approved` and exits 0 anyway, while the approvals store has in fact refused the empty key; the
  gate's own PreToolUse clean branch also says trusted for an empty hash. Ported from P-053 in the former private
  repository agent-guard
- **Depends on**: none (can be merged independently of P-009; P-009 gives hooks / MCP / permissions a hash, but the empty
  hashes this proposal fixes still exist after it)
- **Branch**: `p/011-approve-empty-hash`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`aguard approve <path>` (`cmd/aguard/gate.go:343` `approvePath`) scans the target, takes the worst artifact
`gate.Summarize` gives, writes its hash into the approvals store, and then prints `approved …`.

When the worst artifact's hash is empty, the approvals store refuses it (`internal/gate/approvals.go:167`
`Store.Approve` returns immediately on `""`, `:129` `Approved("")` is always false, pinned by
`TestEmptyHashIsNeverApproved`), so **nothing is stored**; `approvePath` still calls `Save` (creating an empty store when
there is none), prints `approved`, and exits 0. The user believes this content is now trusted; next time the gate asks
again and `SessionStart` lists it again — while the answer the user just got was "approved".

With a binary built from `main` (`dec64ca`, v0.18.0) and `--root` pointing at an empty directory, three targets measured:

```
.claude dir, broken settings.json    → approved hook "settings.json" (100/100, clean)
                                       hash
                                     exit 0, created a 38-byte approvals store: {"version": 1, "approvals": {}}
single file notes.md under chmod 000 → approved instruction "notes.md" (100/100, clean)
                                       hash
                                     exit 0, likewise created an empty approvals store
.claude dir with only one hook       → approved hook "PreToolUse[Bash]#1" (100/100, clean)
                                       hash
                                     exit 0, likewise created an empty approvals store
```

The third one is because hooks / MCP / permissions have no hash at all on `main` (P-009 is fixing that). Once P-009 is
merged the third one goes away and the first two remain — artifacts that failed to parse (`PARSE-000`) are left as `""`,
and no hash can be computed for files that cannot be read either.

The gate itself has the same defect: the clean branch of `handlePre` (from `internal/gate/hook.go:263` on) still calls
`Approve` for an empty hash (which is dropped), says trusted, and reports a store change. Given a skill directory that
contains `plugins/installed_plugins.json` (so it is routed to the root collector) and a broken `settings.json`, and fed a
`PreToolUse[Skill]` event, `main`'s binary answers:

```
{"systemMessage":"AgentGuard: hook \"settings.json\" 100/100 (Low) · no finding at or above the threshold · trusted from now on for content (none)"}
```

and writes an empty approvals store under `--root`.

## Initial direction

When the worst artifact has no hash, `approvePath` **refuses explicitly**: it does not print `approved`; it reports one
sentence saying which artifact, why it has no hash, and that nothing was approved; it goes through the CLI's runtime error
(exit code 2); it does not touch the approvals store. Targets that have a hash (normal skills / plugins / directories /
files) behave unchanged. The gate's clean branch likewise: with an empty hash it does not say trusted and does not report
a store change. This touches `cmd/aguard/gate.go` and `internal/gate`, and perhaps adds a "why there is no hash" field to
`gate.Verdict`; one sentence each in the doc pair and in spec §17.

## Done criteria

- [x] `TestApproveRefusesWhatHasNoContentHash` (`cmd/aguard/gate_approve_test.go`, new), one row each for the two targets
  reachable today: a `.claude` directory with a broken `settings.json` (`PARSE-000`), and a single file under
  `chmod 000`. Each row asserts: `approvePath` returns an error; the error contains `<kind> "<name>" has no content hash`,
  `nothing was approved` and a hint pointing at `--json` (the `PARSE-000` row must also say `did not parse`, the other row
  `the scanner could not compute one`); the error is **not** a `*failExit` (so `main` takes the runtime error path, exit
  code 2); the output contains no `approved`; the approvals store file is **not created**. Red today: returns nil, prints
  `approved … hash `, and creates an empty approvals store
- [x] `TestApproveRefusalLeavesTheStoreAlone` (same file, new): when the approvals store already holds one approval, after
  the refusal it is **still the same file** (`os.SameFile`), its bytes unchanged, that approval still there. Red today:
  `Save` rewrote it with a temp file + rename
- [x] `TestApproveNeverFallsBackToAnotherArtifact` (same file, new): a root with a broken `settings.json` + a subagent + a
  command → refused, and the approvals store is not created. The refusal must not fall back to "the first artifact that
  has a hash"
- [x] `TestCleanLoadWithNoHashIsNotClaimedTrusted` (`internal/gate/unhashed_test.go`, new): the `PreToolUse` clean branch
  meets an empty hash (one row each: a `PARSE-000` hook, an instruction with no note at all) → returns no decision (allows
  as usual), the message contains no `trusted` and says `not remembered, it has no content hash: <reason>`, no store
  change is reported, the approvals store is empty. Red today: says `trusted from now on for content (none)` and reports
  a store change
- [x] Reverse assertion `TestApproveStillRecordsWhatHasAHash` (`cmd/aguard/gate_approve_test.go`, new, green today): a
  clean skill → returns nil, prints `approved skill "<name>" (…, clean)`, exactly one entry in the approvals store, with a
  key equal to the hash `collect.TreeHash` computes; a skill with a credential exfiltration chain → recorded as
  `accepted-risk`, and the "you accepted a risk" reminder is printed. Stays green after the fix without a single character
  changed
- [x] Reverse assertion `TestCleanLoadWithAHashIsStillRemembered` (`internal/gate/unhashed_test.go`, new, green today): the
  same artifact with a hash → recorded as `clean`, says `trusted from now on for content <short hash>`, reports a store
  change
- [x] Reverse assertion: the existing test files of `internal/gate` stay green without a single character changed
  (including `TestEmptyHashIsNeverApproved`); gate invariant #2 does not change — the call sites that write approvals are
  still the original three (`approvePath`, `handlePre`, `handlePost`), and none of them accepts an externally supplied hash
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **The approvals store does not change** (`internal/gate/approvals.go`): `Approve` still silently drops an empty key, and
  `Approved("")` is still always false
- **On the gate's hook path only the clean branch of `handlePre` changes**: with an empty hash it does not say trusted and
  does not report a store change; `handlePost` / `SessionStart` do not change
- **`approve` is not made to approve several artifacts at once**, nor does it approve a different artifact when the worst
  one has no hash (open question 2)
- **No hash is made up for things that have none**: `collect` / `detect` do not change by a line; `PARSE-000` artifacts
  and unreadable files are still `""`. Computing hashes for hooks / MCP / permissions is P-009's job
- **The output of a successful `approve` does not change** (the three `approved <kind> "<name>" (…)` lines and the
  accepted-risk reminder)
- `go.mod` / `go.sum` do not change, no new dependencies

## Must not claim

- Do not say "things without a hash can be approved now": what is fixed is **refusing out loud**, not making them
  approvable
- Do not say "the earlier approveds are now invalid": they **were never stored** (the approvals store refuses empty keys),
  and the gate kept asking all along
- Do not say "once P-009 is merged this problem is gone": `PARSE-000` artifacts and unreadable files still have no hash
  after P-009
- Do not say `aguard check` (terminal / markdown) shows that a config did not parse: it does not show an artifact's own
  dim-0 notes, and a config that failed to parse reads there as `looks safe` (measured on `main`: for a `.claude`
  directory with a broken `settings.json` the terminal report says `Your Claude Code setup looks safe. No findings.`,
  while `--json` has `PARSE-000` and `COV-000`) — that is another gap, which the maintainer has approved splitting out
  (P-013). So the refusal message itself spells out the file and `PARSE-000`, and the hint points at `check --json`

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Two refusal tests + one reverse assertion, run red | `cmd: tests — approve prints "approved" for a target with no content hash, stores nothing and exits 0 (P-011)` |
| 2 | `gate.Verdict` carries "why there is no hash"; `approvePath` refuses on an empty hash, says why, and does not touch the approvals store; one sentence in `approve`'s help | `gate, cmd: approve refuses a target with no content hash, says why, and leaves the store alone (P-011)` |
| 3 | One sentence each in the install-gate pair, spec §17 and `.claude/rules/gate.md` | `docs: install-gate, spec §17 and gate.md say approve refuses what has no content hash (P-011)` |
| 4 | The gate's clean branch also claims trusted on an empty hash — red test + reverse assertion | `gate: tests — a clean load with no content hash is announced as trusted and rewrites the store (P-011)` |
| 5 | Fix: does not say trusted, does not report a store change; the reason for a parse failure spells out the file and `PARSE-000` | `gate: a clean load with no content hash says it was not remembered and why, and leaves the store alone (P-011)` |
| 6 | The `check` hint in the refusal now points at `check --json`; add the "never falls back to another artifact" test | `cmd: approve's refusal points at check --json, which does list what was not read, and never falls back to another artifact (P-011)` |
| 7 | gate.md gains the sentence about the clean branch | `rules: gate.md says the PreToolUse clean branch, like approve, never claims trust over an empty hash (P-011)` |
| 8 | The `aguard check <path>` hint in the install-gate pair is just as untrue → now points at `check --json` (found during the port; the former repo has no such item) | `docs: install-gate points at check --json, the report that lists what approve could not read (P-011)` |
| 9 | This file, the index | `proposals: P-011 (P-011)` |

## Open questions

1. **Which exit code does the refusal use?**
   **Recommendation**: 2 (a runtime error, returned as a plain `error`). In this tool 1 means "a finding reached the
   threshold"; this is not a problem with a finding but "cannot be done"; it takes the same path as
   `nothing could be collected from … — refusing to approve` in the same function.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
2. **What if one call involves several artifacts?**
   **Recommendation**: this case does not exist in `approvePath` — it only approves **the one** worst artifact
   `gate.Summarize` picks, even when the target is a directory routed to the root collector. So "approve the ones with a
   hash, name the ones skipped" does not apply; if the worst artifact has no hash **the whole call is refused**, with no
   fallback to the next artifact that has a hash — that would amount to approving content the printed verdict does not
   describe. Making `approve` approve several at once is a different contract, not in this proposal.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
3. **What does the reason say?**
   **Recommendation**: only what the artifact itself can tell: it carries `PARSE-000` (`SrcParseError`) → say it did not
   parse and was not fully read; otherwise → `the scanner could not compute one`, plus a command that shows "what was not
   read". **Do not guess by kind**: after P-009 the kind no longer decides whether there is a hash, and a reason written
   by kind would become false the day P-009 merges.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port). **Review correction (2026-10-09,
   former repo)**: the hint originally pointed at `aguard check <target>`, but the terminal report prints `looks safe` for
   a config that failed to parse (see "Must not claim"; retested with the same result on this repo's `main`); so the
   reason for `PARSE-000` spells out **the file and the note itself**
   (`"<file>" did not parse, so it was not fully read [PARSE-000: <title>]`), and the hint now points at `check --json`
   (W5, W6).
4. **Where is the reason computed?**
   **Recommendation**: a field on `gate.Verdict` (`Unhashed`), filled in `Summarize` — only it knows which artifact was
   picked; finding "the worst" again in `cmd` would copy the definition, and this repository has an explicit rule that the
   same decision has only one definition.
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).
5. **Should the gate's hook path change with it?** The clean branch of `handlePre` likewise calls `Approve` on an empty
   hash (which is dropped) and says `trusted from now on for content (none)`.
   **Recommendation**: not in this proposal. That path's target comes from `ResolveSkill` and is always a directory;
   normally it goes through `TreeHash`, and `TreeHash` never returns `""`; only when "the skill directory itself looks like
   a root" (for example it contains `plugins/installed_plugins.json`) is it routed to the root collector, where it may pick
   an artifact with an empty hash. The consequence is one untrue message + asking again next time (failing towards "asking
   more"), not a pass.
   **Decided (2026-10-09)**: as recommended (former repo). **Review correction (2026-10-09, former repo)**: the review
   reproduced it, and it is the same contract as this proposal ("never claim trust over an empty hash"), so it is folded
   into this proposal (W4–W7) rather than split out. Retested with the same result on this repo's `main` (see the last
   paragraph of "Problem").
6. **Which merges first, this or P-009 (not merged)?**
   **Recommendation**: either order; the only conflict would be the index line. **Whichever merges second** has to change,
   on rebase, the half sentence P-009 writes into `.claude/rules/gate.md` — "`PARSE-000` artifacts are still `""`, and
   `approve` still prints an empty `approved` for them (to be fixed, not in P-009)" — into "`approve` refuses them
   (P-011)" — while either one is merged on its own that sentence is true; once both are merged it is false. Measured,
   see "Done".
   **Decided (2026-10-09)**: as recommended (decided in the former repo, kept on port).

## Done

```
Merged: PR #20 (2026-10-09; find the sha with git log --grep P-011)
Released: v0.19.0
Evidence: TestApproveRefusesWhatHasNoContentHash (cmd/aguard/gate_approve_test.go); W1 red on this repo's main, both rows "approve reported success for a target with no content hash" followed by approved hook "settings.json" (100/100, clean) / approved instruction "notes.md" (100/100, clean) and an empty hash line → green after W2; after W6 the error carries the --json hint, still green
Evidence: TestApproveRefusalLeavesTheStoreAlone (same file); W1 red "approve reported success …" + "a refused approve rewrote the approvals store" → green after W2: os.SameFile true, bytes unchanged, the existing 1 approval still there
Evidence: reverse assertion TestApproveStillRecordsWhatHasAHash (same file); already green at W1 (the clean skill / accepted-risk rows), still green afterwards without a single character changed
Evidence: TestCleanLoadWithNoHashIsNotClaimedTrusted (internal/gate/unhashed_test.go); W4 red, both rows "claimed trust over an empty hash" (… trusted from now on for content (none)) + "reported the store as changed" → green after W5; the reverse TestCleanLoadWithAHashIsStillRemembered green before and after the fix
Evidence: TestApproveNeverFallsBackToAnotherArtifact (cmd/aguard/gate_approve_test.go); mutation (on an empty hash, fall back to the first artifact in res.Artifacts that has a hash) → red "approve reported success for a root whose worst artifact has no content hash", green after restoring
Evidence: binary on a real machine, main (dec64ca) → this branch, --root pointing at an empty directory: the three targets .claude with a broken settings.json, notes.md under chmod 000, and .claude with only one hook all go from exit 0 + approved + a newly created 38-byte empty approvals store → exit 2, one stderr line error: … has no content hash: …; nothing was approved (aguard check "…" --json …), the --root directory stays empty
Evidence: binary on a real machine, approving the .claude with a broken settings.json again when the approvals store already holds 1 entry: main exit 0, the file replaced (inode 206776258 → 206776259) → this branch exit 2, inode unchanged, sha unchanged, the 1 approval still there
Evidence: binary on a real machine, gate PreToolUse[Skill] (skills/x/ contains plugins/installed_plugins.json and a broken settings.json): main says trusted from now on for content (none) and writes an empty approvals store → this branch says not remembered, it has no content hash: "…/skills/x/settings.json" did not parse, so it was not fully read [PARSE-000: Parse failed, artifact not fully covered] · it is audited again on every load, and writes no file under --root
Evidence: open question 6 — the former repo's P-009 patches stacked on this branch: all code patches apply, the doc patches conflict on .claude/rules/gate.md and hash.md (exactly where the half sentence from question 6 is); after stacking, go test -race ./internal/gate/ ./cmd/aguard/ is all green, including P-009's TestGate_ApprovedHookLeavesSessionStart; the .claude with only one hook then approves successfully and stores the hash, the broken settings.json is still refused. The trial branch was local only and has been deleted
Evidence: reverse assertion — the existing test files of internal/gate unchanged and still green (including TestEmptyHashIsNeverApproved); call sites that write approvals: main 3 → HEAD 3 (cmd/aguard/gate.go approvePath, internal/gate/hook.go handlePre, handlePost), none added, none accepts an externally supplied hash
Evidence: Out of scope — git diff --stat origin/main -- internal/gate/approvals.go internal/gate/approvals_test.go internal/gate/gate_test.go internal/gate/hook_test.go internal/gate/memory_test.go internal/gate/deadline_test.go internal/gate/install_test.go internal/gate/resolve_test.go internal/gate/status_test.go internal/collect internal/detect go.mod go.sum is empty; internal/gate/hook.go only gains 5 lines in the clean branch of handlePre; the Fprintf of the three approved lines in cmd/aguard/gate.go is unchanged
Evidence: known limitation — when the .claude with only one hook is refused on main, the reason is the scanner could not compute one, while check --json has no note at all for this artifact (hash empty, findings empty): the hint's "lists the notes that say what was not read" does not hold for this case; after P-009 is merged this kind of target no longer reaches the refusal. No reason by kind is added, see open question 3
Evidence: Must not claim — on main, aguard check on the .claude with a broken settings.json gives the terminal report Your Claude Code setup looks safe. No findings., while --json has PARSE-000 and COV-000; so the refusal hint and the install-gate pair both point at --json (W6, W8)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), line 2 of go.mod is go 1.23.5
```
