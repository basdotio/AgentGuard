<!-- SPDX-License-Identifier: MIT -->
# 008 — The undo command the gate prints after an approval fails when pasted as is: the hash is followed by an ellipsis

- **Source**: the hint after accepting a risk, `undo with: aguard approvals forget <12 hex digits>…`, fails when pasted and
  run; in addition, `forget ""` deletes the approval when there is only one.
  Ported from P-050 in the former private repository agent-guard
- **Depends on**: none (can be merged independently of P-007; without P-007 this hint is never reached in the real
  two-process hook, but the code and the tests do not depend on it)
- **Branch**: `p/008-undo-hint-pastes`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

After PostToolUse records an "accept the risk" approval, the gate prints (`internal/gate/hook.go:345-347`):

```
AgentGuard: risk accepted for skill "pdf-export" (51/100) · content a1e9cd5dbc1a… · undo with: aguard approvals forget a1e9cd5dbc1a…
```

The hash in the undo command is the output of `shortHash`: the first 12 characters **plus a `…`**. Pasted and run as is,
`aguard approvals forget a1e9cd5dbc1a…` takes the `…` as part of the hash prefix; `resolveHashPrefix`
(`cmd/aguard/gate.go:429`) finds no match, reports `no approval matches` and exits 2.
The comment on `forgetApproval` (`cmd/aguard/gate.go:415`) itself says "it accepts a prefix, because a prefix is what the
messages print" — but what the messages print is not a prefix; it is a prefix plus an ellipsis.
The `content hash` in SessionStart and in the prompt has the same `shortHash` form (`internal/gate/gate.go:179`, `:190`),
and copying from there fails the same way.

A second defect along the way: the empty string is a prefix of every hash, and `resolveHashPrefix` does not reject it.
When the approval store holds exactly one approval, `aguard approvals forget ""` deletes it without asking.

Run by hand with a binary built from `main` (`dec64ca`, v0.18.0) against a temporary root (`aguard approve` records one
approval, then it is undone in the form the message shows):

```
aguard approvals forget bede3b30172e…    → error: no approval matches "bede3b30172e…"   exit 2
aguard approvals forget bede3b30172e...  → error: no approval matches "bede3b30172e..."  exit 2
aguard approvals forget ""               → forgot bede3b30172edde57545241d7d19b1214a041489ce7f2ec328a82d62a77e7774;
                                           the gate will ask about that content again   exit 0
aguard approvals                         → no approvals recorded
```

Consequence: when a user who clicked "accept" in the prompt wants to take it back, the only undo command the tool gives
is broken; and a slipped empty argument silently withdraws an approval.
On this repository's `main`, the undo hint is not yet reached in the real two-process hook flow (PostToolUse cannot read
back the pending that PreToolUse held, see P-007); only after P-007 is merged will anyone see it for the first time, so
nobody has pasted it yet.

## Initial direction

The undo command prints a prefix that **can be pasted directly** (without `…`); the `content …` shown for display does not
change. Along the way, `forget` tolerates a trailing `…` / `...` (short hashes copied from other messages carry the
ellipsis too), still requires a unique match, and rejects an empty prefix.

## Done criteria

- [ ] `TestUndoHintPastesAsIs` (`cmd/aguard/gate_undo_test.go`, new): go through Pre → Post to get the "accept the risk"
  SystemMessage, split the command after `undo with:` **as is** into arguments and hand them to `forgetApproval` →
  success, the approval is deleted. Red today: `no approval matches "…"`
- [ ] `TestForgetAcceptsTheShortHashAsPrinted` (same file, new): `a1e9cd5dbc1a…` and `a1e9cd5dbc1a...` both resolve to the
  single approval
- [ ] Reverse assertion: a prefix that matches several approvals still reports "matches N approvals", one that matches
  none still reports "no approval matches"; `forget all` behaves as before
- [ ] Reverse assertion: an **empty prefix** (`""`, or `…`, which is empty once the ellipsis is removed) is an error and
  deletes nothing — today `forget ""` deletes the approval when there is only one
- [ ] Reverse assertion: the `content <12 hex digits>…` shown in the hint does not change; `TestGateAsksThenRemembers` and
  `TestApprovalOnlyCoversWhatWasShown` stay green unchanged
- [ ] `make verify` green

## Out of scope

- The displayed hash length (12 characters) and the 16 characters in the `aguard approvals` list do not change
- The semantics of approvals, the storage format and the rest of the hint text do not change
- `LoadStore` (`internal/gate/approvals.go`), which P-007 changes, is not touched

## Must not claim

- Do not say "the undo command used to work": before P-007 it never appears in the real hook flow at all, and after
  P-007 it fails when pasted
- Do not say the prefix is always unique: when 12-character prefixes collide, `forget` reports an error asking for more
  characters; this is existing behaviour

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Two new tests + the empty-prefix reverse assertion, run red | `cmd: tests — the undo command the gate prints fails when pasted, and an empty prefix forgets an approval (P-008)` |
| 2 | The undo command in the hint prints the prefix without an ellipsis | `gate: the undo hint prints a prefix that can be pasted as it is (P-008)` |
| 3 | `forget` strips a trailing `…` / `...` and rejects an empty prefix | `cmd: approvals forget takes a short hash as printed and refuses an empty prefix (P-008)` |
| 4 | The install-gate pair explains that `forget` accepts a prefix | `docs: install-gate says forget takes the prefix the messages print (P-008)` |
| 5 | This file, the index | `proposals: P-008 (P-008)` |

## Open questions

1. **Should the hint print the full hash or a 12-character prefix?**
   **Recommendation**: a 12-character prefix, without an ellipsis. A full 64-character hash is too long for a one-line
   system message; the probability of a 12-character collision in one machine's approval store is negligible, and on a
   collision `forget` reports an error asking for more characters.
   **Decided (2026-10-08)**: as recommended (decided in the former repo, kept on port).
2. **Should `forget` tolerate the ellipsis as well?**
   **Recommendation**: yes. The `content hash` in SessionStart and in the prompt is also the `shortHash` form with an
   ellipsis, and a user copying from there fails the same way; strip a trailing `…` or `...` and then match as a prefix,
   with the uniqueness requirement unchanged.
   **Decided (2026-10-08)**: as recommended (decided in the former repo, kept on port).
3. **Should the prefix have a minimum length (say 4 characters)?**
   **Recommendation**: no; reject only the empty prefix. When a non-empty prefix is not unique, the existing logic already
   reports an error.
   **Decided (2026-10-08)**: as recommended (decided in the former repo, kept on port).

## Done

```
Merged: PR #17 (2026-10-09; find the sha with git log --grep P-008)
Released: pending release
Evidence: TestUndoHintPastesAsIs (cmd/aguard/gate_undo_test.go); W1 red: the undo command as printed failed: no approval matches "a1e9cd5dbc1a…" → green after W2: the pasted command deleted the approval, and the displayed content a1e9cd5dbc1a… is unchanged
Evidence: TestForgetAcceptsTheShortHashAsPrinted (same file); W1 red (both … and ... give no approval matches) → still red after W2 (W2 changes only the hint) → green after W3
Evidence: reverse assertion TestForgetRefusesAnEmptyPrefix (same file); W1 red: forget "" succeeded and withdrew the approval (the existing hazard) → after W3, "", … and ... are all refused and the approval remains
Evidence: reverse assertion TestForgetStillRequiresAUniqueMatch (same file, added with W3): two approvals sharing a 12-character prefix → matches 2 approvals; no match → no approval matches; both remain
Evidence: reverse assertions green unchanged — go test -race ./internal/gate/ ok (TestGateAsksThenRemembers, TestApprovalOnlyCoversWhatWasShown, etc.; no changes to gate_test.go or hook_test.go)
Evidence: binary run by hand against a temporary root — main (dec64ca): forget <12 hex digits>… and <12 hex digits>... both give no approval matches and exit 2, forget "" exits 0 and deletes the only approval; this branch: forget "" and forget … report no hash given and exit 2 with the approval still there, <12 hex digits>... and <12 hex digits>… both give forgot and exit 0, forget all with two approvals gives forgot 2 approval(s)
Evidence: Out of scope — git diff --stat origin/main -- internal/gate/approvals.go internal/gate/gate_test.go internal/gate/hook_test.go go.mod go.sum is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod line 2 is go 1.23.5
```
