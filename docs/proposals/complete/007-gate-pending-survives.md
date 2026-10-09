<!-- SPDX-License-Identifier: MIT -->
# 007 — A skill approved at the gate's prompt is asked about again on the next load: the approval was never recorded

- **Source**: every hook event is a new process and `LoadStore` does not read pending back, so the approval given at
  the prompt is already lost by `PostToolUse`; the existing tests share an in-memory store within one process and have
  always been green. Ported from P-049 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/007-gate-pending-survives`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The gate returns `ask` for a skill with a high finding, the operator clicks allow in the prompt, and the skill loads.
The user docs (the event table in `docs/install-gate.md`) say that `PostToolUse` then "records it", and the same bytes
are not asked about again. **In fact it has never been recorded**: the next load of the same bytes shows the prompt
again, and `aguard approvals` is empty.

The cause is at the process boundary. Every Claude Code hook event is **a new process**:

| Process | What it does | Actual result |
|---|---|---|
| `PreToolUse` | Decides `ask`; `Store.pend` parks the verdict in `pending` keyed by `tool_use_id` (`internal/gate/approvals.go:139`), `Save` writes it to disk | The file does contain this pending |
| `PostToolUse` | `LoadStore` reads the file (`approvals.go:96`), `handlePost` looks up the pending by `tool_use_id` (`internal/gate/hook.go:314`), rescans, and promotes it to an approval if the hash matches | `LoadStore` only copies `on.Approvals` into the new Store (`approvals.go:115-124`); **`Pending` is never read back**. The pending is not found, `handlePost` returns empty right away, records nothing, **and says nothing** |

There is a knock-on effect too: any later disk write (the next `PreToolUse`, `aguard approve`,
`aguard approvals forget`) overwrites the file with this Store that has no pending, so the pending of a prompt
**currently open** in another session is wiped along with it.

Running the built binary by hand against a temporary root, one `aguard hook` process per step (2026-10-09, `main` at
`dec64ca`):

```
1. PreToolUse  toolu_repro1  → decision: ask; the store has pending.toolu_repro1 (hash a1e9cd5d…, 51/100)
2. PostToolUse toolu_repro1  → stdout 0 bytes; store unchanged, approvals still {}
   aguard approvals          → no approvals recorded
3. PreToolUse  toolu_repro2  → decision: ask (the same bytes, asked again)
```

Why the tests are all green: the tests in `internal/gate` that cover this path (`TestGateAsksThenRemembers`,
`TestApprovalOnlyCoversWhatWasShown`) have Pre and Post **share the same in-memory Store**, so the pending never went
through the disk; the end-to-end test in `cmd/aguard` (`TestGateEndToEnd`) goes through the real `runHook`, but only
sends `PreToolUse`, never `PostToolUse`.

Consequences:

- **The promise "allowing once at the prompt is enough" does not hold.** Every load of a skill with a high finding that
  the operator has already decided to accept prompts again; and the operator reads this as "the gate is broken", which
  is exactly the kind of cost that `.claude/rules/gate.md` keeps saying makes people uninstall the gate.
- **And it is silent**: PostToolUse outputs nothing, and the operator has no way to learn that their answer was
  dropped. The only way around it is `aguard approve <path>` in a terminal, and the docs do not tell them to do that.
- The pending in the file does not grow on its own (the next `pend` overwrites the whole thing), but every entry is
  only ever written, never read.

## Initial direction

`LoadStore` reads `pending` back, with hygiene rules in the same spirit as for approvals: rows with an empty key or an
empty hash are dropped; expired ones (by the **same** expiry rule that `pend` prunes with) are dropped; an unreadable /
unknown-version file still degrades as a whole to empty. Only `internal/gate/approvals.go` and its tests change;
`handlePost`'s "reread, recompute the hash, promote only on a match" is untouched — that is exactly why pending exists.
Add a test across two processes (two `runHook` calls), a scenario the existing tests cannot construct by their
structure.

## Done criteria

- [x] `TestGateRemembersAnApprovalAcrossHookProcesses` (`cmd/aguard/gate_pending_test.go`, new): the real `runHook`
  runs twice — `PreToolUse` decides `ask`, the file has this pending (precondition, holds today) → `PostToolUse` with
  the same `tool_use_id` → the approvals in the file contain the hash `checkTarget` computes for the same directory,
  verdict `accepted-risk`, Post's output says "risk accepted"; a third `PreToolUse` (new id) outputs 0 bytes. **Red
  today**: approvals are empty after Post
- [x] `TestPendingSurvivesSaveAndLoad` (`internal/gate/pending_test.go`, new): `pend` → `Save` → `LoadStore` →
  `pendingFor` finds it, with all six fields intact. Red today
- [x] `TestPostPromotesAcrossProcesses` (`internal/gate/pending_test.go`, new): Pre and Post each call `Handle` with a
  Store **freshly read from disk**, with a `Save` in between → after Post it is approved and the pending is consumed.
  Red today
- [x] `TestPendingHygieneOnLoad` (same file, new): one file holds good and bad rows — empty id, empty hash, no date,
  future date and expired (`pendingTTL + 60` seconds ago) are dropped; `pendingTTL − 60` seconds ago and fresh are kept;
  the approvals hygiene rules in the same file stay as before (a key that does not match its hash is dropped).
  **Why ±60 seconds and not exactly `pendingTTL`**: the cross-process tests run on the wall clock (`LoadStore` uses the
  process's own clock, see open question 1), and one second passing between writing and reading the file would turn
  "exactly TTL" into TTL + 1; the exact boundary is guaranteed by the shared `expired()` and by `TestPendingExpires`
  (TTL + 1 is pruned)
- [x] Reverse assertion (a)/(f) — changed bytes are not promoted, and Post **recomputes** the hash to compare rather
  than trusting the hash in the file:
  `TestChangedBytesAreNotPromotedAcrossHookProcesses` (`cmd/aguard`, new): after Pre, change one file in the skill →
  Post → approvals empty, output contains "changed between the prompt and the load". **This output line is half the
  criterion**: today Post returns right away when it cannot find the pending, so "not promoted" comes for free; only
  this line proves the pending was found and the comparison really happened.
  `TestPendingHashIsComparedNotTrusted` (`internal/gate`, new): hand-write a file whose pending hash differs from the
  scan result → Post → neither hash is in approvals
- [x] Reverse assertion (b): `TestExpiredPendingIsNotPromoted` (`internal/gate`, new): the scanner gives the **same**
  hash as the pending, the pending parked `pendingTTL + 60` seconds ago → not approved after a cross-process Post;
  parked `pendingTTL − 60` seconds ago → approved (a control in the same test, so that "not approved" is not for some
  other reason)
- [x] Reverse assertion (c): `TestPostForAnotherCallPromotesNothing` (`internal/gate`, new): Pre `c1` → fresh read →
  Post `c2` → not approved; after another fresh read, `c1`'s pending is still there (someone else's answer does not
  consume it)
- [x] Reverse assertion (d): `TestMediumPassIsNotRememberedAcrossProcesses` (`internal/gate`, new): a pass with only
  medium findings, after a cross-process Pre → Post approvals are still empty; `TestPassWithMediumFindingIsNotRemembered`
  and the three next to it still green without a single character changed
- [x] Reverse assertion (e): `TestCorruptStoreAsksRatherThanAllows`, `TestKeyMustMatchItsOwnHash`,
  `TestLoadStore_FIFOReadsAsCorruptNotHang` still green without a single character changed; `TestPendingHygieneOnLoad`
  adds one more case: the `pending` section has the wrong type → the whole file `Corrupt`, approvals empty
- [x] Still green without a single character changed: `TestGateAsksThenRemembers`, `TestApprovalOnlyCoversWhatWasShown`,
  `TestPendingExpires`, `TestDenyParksNothing`, `TestFailedToolCallRecordsNothing`, `TestAskEscalatesWhenNobodyWillSeeIt`,
  `TestGateEndToEnd`, `TestHookRunnerNeverFails`
- [x] Manual run: after `make build`, two `aguard hook` processes fed the same `tool_use_id`, recorded once before and
  once after the fix (the "Problem" section of this file is the before)
- [x] `make verify` green; `go version` does not switch toolchains

## Out of scope

- **What gets approved does not change**: not one line of `handlePost`'s "re-resolve, rescan, promote only on a hash
  match" changes; what gets promoted is still the `v.Hash` computed by this process (gate invariant: no API accepts a
  hash string from outside). `Verdict.Remembered()`, the thresholds and the `accepted-risk` / `clean` distinction do not
  change
- **The prompt text does not change**: `Verdict.Reason()`, `UnrememberedLine()`, Post's two sentences "risk accepted" /
  "changed between" and the `GATE-000` sentences are not changed — Post's two sentences have **never appeared** in
  production today (Post never finds the pending); after the fix they appear for the first time, but the text is the
  existing text (for one flaw in it, see open question 5)
- **`ask → deny` in auto-accept modes does not change**; `deny` still does not `pend`
- **The fail-open `GATE-000` does not change**; neither do the 30-second `scanDeadline` and the 10-second
  `resolveDeadline` / `preScanDeadline`
- **The approval key is still the canonical hash**; the pending key is still `tool_use_id`
- **A corrupt file still reads as entirely empty**, and `Save` still refuses to overwrite it; the version number
  `storeVersion` is not bumped (the file format did not change: `pending` was always in it, only nobody read it)
- **The exported signature of `LoadStore` does not change** (five call sites: four in `cmd/aguard/gate.go`, one in
  `internal/gate/status.go`)
- **No file lock**: two sessions doing load → pend → save at the same time, the later write overwrites the earlier one.
  This is a race approvals has always had, and what it loses is in the "ask once more" direction; a lock would have to
  deal with stale lock files, network file systems and timeouts, and this package's top constraint is that it must
  never hold up a load
- No dependencies added; `collect` / `detect` not touched (so no scan on a real machine needed)

## Must not claim

- **Do not say "approvals clicked earlier now take effect"**: before the fix not one was written into approvals, so
  after the fix every skill will still be asked about **once** more, and the approval clicked that time is the one
  remembered. Skills approved with `aguard approve` in a terminal are unaffected (that path has always worked)
- **Do not say "the gate remembers every choice you make"**: only those decided `ask`, that you allowed, and whose
  bytes had not changed at load are recorded. Rejections, `deny`, a `deny` escalated from an auto mode, medium passes
  and answers given more than an hour later are not recorded, as before
- **Do not say "tested in a real Claude Code session"**: the manual run was two `aguard hook` processes fed
  **constructed** event JSON; what Skill's `PostToolUse` payload looks like in a real session (especially whether
  `tool_response` is an object) was not observed in this proposal (see open question 4)
- **Do not say "concurrency-safe"**: see the last item of "Out of scope"
- **Do not say "you can undo by following the hint in Post's line"**: the `aguard approvals forget a1e9cd5dbc1a…` in
  that line carries an ellipsis, and pasting it as is reports `no approval matches` (see open question 5); what works
  is the prefix without the ellipsis, or the column listed by `aguard approvals`

## Work items

| W | One line | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | Cross-process tests + reverse assertions, run red | `gate, cmd: tests — an approval given at the prompt is lost between the PreToolUse and PostToolUse processes (P-007)` |
| 2 | `LoadStore` reads pending back: empty id / empty hash / no date / future date / expired are dropped, and the expiry rule shares one function with `pend` | `gate: the approvals store reads its pending verdicts back, so PostToolUse in a new process finds the prompt it answers (P-007)` |
| 3 | Spec §17, `.claude/rules/gate.md`, `docs/install-gate.md` and its zh pair | `docs: spec §17, the gate rules and the install-gate pair say a parked verdict crosses the process boundary through the file (P-007)` |
| 4 | This file, the index | `proposals: P-007 (P-007)` |

## Open questions

1. **Which clock does `LoadStore` judge expiry by?**
   **Recommendation**: the process's own `time.Now()`. `LoadStore` runs in `runHook` before `Options` exists and cannot
   get `o.Now`; and in production `o.Now` is `nowUnix` = `time.Now().Unix()`, the same clock. The exported
   `LoadStore(path)` signature does not change; internally it forwards to `loadStore(path, now)`, and in-package tests
   inject the time.
   **Decided (2026-10-08)**: as recommended.
   Implementation note: in the end the tests did not use an injected clock — the W1 tests had to compile against
   **today's** API (otherwise the whole package fails to compile and the reason for red would be the wrong one), so they
   all run on the wall clock (`wallNow`, with a 60-second margin at the boundaries). With no caller, `loadStore(path, now)`
   was not split out; `LoadStore` takes `time.Now().Unix()` directly and hands it to `readPending`. The clock is
   still the same one, and the conclusion is unchanged.
2. **What about rows whose `asked_at` is ≤ 0 (no date) or later than now (future date)?**
   **Recommendation**: drop both. Neither can be written by the gate's own clock (`pend` always takes `nowUnix()` in
   production), and the expiry rule would always judge them "not expired", so they would stay in the file forever.
   Wrongly dropping a legitimate one (the clock set back between two hooks) costs one more prompt — the same direction
   as a corrupt file reading as empty.
   **Decided (2026-10-08)**: as recommended.
3. **`pendingFor`'s comment says "if not expired", but the code does not check. Change the code or the comment?**
   **Recommendation**: change the comment, stating the two places expiry is judged (when `pend` writes, when
   `LoadStore` reads). Every hook event is a new process, and a Store does not live long enough for a record in it to
   expire; adding a `now` parameter to `pendingFor` stops nothing more in production, but requires changing the call
   in the existing test `TestPendingExpires`.
   **Decided (2026-10-08)**: as recommended.
4. **Can Skill's `PostToolUse` payload in real Claude Code pass `Event.succeeded()`?** `succeeded()` returns false when
   `tool_response` is missing or is not a JSON object, in which case after the fix the pending merely survives and is
   still not promoted. This repository has no sample of a real payload (only the `{"success":true}` constructed in
   tests).
   **Recommendation**: this proposal does not change `succeeded()` (that is the judgement of "what counts as a
   successful load", and changing it is a different decision), and lists it in the PR as a point the AI is unsure of; if
   a real machine shows the shape is wrong, open a separate proposal.
   **Decided (2026-10-08)**: as recommended.
   Verification (2026-10-08, the official Claude Code hooks docs `code.claude.com/docs/en/hooks.md`): `tool_use_id` is
   present in both Pre and Post and identical for the same call; each event is a separate process invocation; a call
   rejected in the permission prompt does not trigger `PostToolUse`. **The docs do not describe the shape of the Skill
   tool's `tool_response`**; still unconfirmed.
5. **The undo command in Post's hint line does not work when pasted as is.** In "risk accepted … · undo with: aguard
   approvals forget a1e9cd5dbc1a…" the hash is the output of `shortHash`, with the `…`; `resolveHashPrefix` takes
   `a1e9cd5dbc1a…` as the prefix and reports `no approval matches` (measured by hand with the fixed binary, exit 2).
   This line never appeared in production before (Post never found the pending); once this proposal fixes that, it will
   reach users for the first time.
   **Recommendation**: not changed in this proposal ("Out of scope": prompt and hint text unchanged), handled by another
   small proposal (P-008). This proposal discloses it in "Must not claim" in the meantime.
   **Decided (2026-10-08)**: as recommended (carrying over the maintainer's advance answer "as recommended" to this
   proposal's open questions; it does not widen this proposal's scope, and the maintainer can overturn it on the PR).

## Done

Manual comparison (`make build`, a temporary root under `/tmp`, one `aguard hook` process per step, events as in the
"Problem" section):

```
                      before (main dec64ca)                 after (this branch)
1. PreToolUse  r1     ask; pending.toolu_repro1 written     ask; pending.toolu_repro1 written
2. PostToolUse r1     stdout 0 bytes; approvals {}          "risk accepted for skill "pdf-export" (51/100) · content a1e9cd5dbc1a…"
                      pending left as is                    approvals: 1 accepted-risk entry (hash a1e9cd5d…); pending cleared
   aguard approvals   no approvals recorded                 1 approval(s) · a1e9cd5dbc1a401b accepted-risk 51/100
3. PreToolUse  r2     ask (same bytes asked again)          0 bytes (approved content loads silently)
```

```
Merged: PR #18 (2026-10-09; find the sha with git log --grep P-007)
Released: pending release
Evidence: TestGateRemembersAnApprovalAcrossHookProcesses (cmd/aguard/gate_pending_test.go); red at W1 (after two runHook calls approvals empty, PostToolUse output "") → green after W2: approvals 1 accepted-risk entry, hash equal to the one checkTarget computes, pending consumed, third PreToolUse 0 bytes
Evidence: TestPendingSurvivesSaveAndLoad, TestPostPromotesAcrossProcesses, TestPendingHygieneOnLoad (fresh / near-ttl rows read back as 0), TestPostForAnotherCallPromotesNothing, TestExpiredPendingIsNotPromoted/answered_within_the_hour (internal/gate/pending_test.go); red at W1 (pending read back from the file is always 0 entries) → green after W2
Evidence: reverse assertion (a)/(f) TestChangedBytesAreNotPromotedAcrossHookProcesses (cmd/aguard), TestChangedBytesAreNotPromotedAcrossProcesses, TestPendingHashIsComparedNotTrusted (internal/gate): at W1 "not approved" already held but "changed between the prompt and the load" was absent (red, proving the before half came for free) → after W2 both halves hold; mutation: short-circuit v.Hash != pending.Hash in handlePost → these three plus TestApprovalOnlyCoversWhatWasShown, 4 in total, turn red
Evidence: reverse assertion (b) TestExpiredPendingIsNotPromoted/answered_after_the_hour and the expired row of TestPendingHygieneOnLoad; mutation: pendingRowOK does not check expiry → both turn red (a prompt from 3660 seconds ago is approved; the expired row is read back). Mutation: remove the "future date" and "empty hash" hygiene rules → TestPendingHygieneOnLoad turns red on the from-2099 / no-hash rows respectively
Evidence: reverse assertion (c) TestPostForAnotherCallPromotesNothing (red at W1: toolu_1's pending no longer exists in the new process; after W2 another tool_use_id is not approved and does not consume it); (d) TestMediumPassIsNotRememberedAcrossProcesses (green before and after the fix: a medium pass does not pend), TestPassWithMediumFindingIsNotRemembered / TestCleanPassIsStillRemembered / TestLowOnlyPassIsRemembered / TestLLMFindingDoesNotDecideMemory still green without a single character changed; (e) TestCorruptStoreAsksRatherThanAllows, TestKeyMustMatchItsOwnHash, TestLoadStore_FIFOReadsAsCorruptNotHang still green without a single character changed, the last case of TestPendingHygieneOnLoad "pending section has the wrong type → whole file Corrupt, approvals 0, pending 0"
Evidence: still green without a single character changed — TestGateAsksThenRemembers, TestApprovalOnlyCoversWhatWasShown, TestPendingExpires, TestDenyParksNothing, TestFailedToolCallRecordsNothing, TestAskEscalatesWhenNobodyWillSeeIt, TestGateEndToEnd, TestHookRunnerNeverFails
Evidence: manual run, see the table above: before the fix Post 0 bytes, approvals 0 entries, third Pre still ask → after the fix Post one line "risk accepted", approvals 1 entry, third Pre 0 bytes; pasting the undo command in Post's line as is → no approval matches, exit 2 (open question 5, left to P-008)
Evidence: Out of scope — git diff --stat origin/main -- internal/gate/hook.go internal/gate/gate.go internal/gate/status.go cmd/aguard/gate.go internal/gate/gate_test.go internal/gate/approvals_test.go internal/gate/memory_test.go cmd/aguard/gate_e2e_test.go go.mod go.sum internal/collect internal/detect internal/report is empty; the code change is only in internal/gate/approvals.go (+62 −2)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); internal/gate coverage 83.1%
```
