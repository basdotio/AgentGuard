<!-- SPDX-License-Identifier: MIT -->
# 027 — Before turning on `--llm`, nobody can see what would leave the machine: the excerpts are visible only to a capture server

- **Source**: new finding (2026-10-09) — follow-up to P-003 (zero-dial test), P-005 (judge egress paths), P-006
  (judge rendering) and P-020 (excerpt padding): each of them changed what the judge sends, and each proved it with an
  `httptest` server reading request bodies, because there is no other way to look
- **Depends on**: none
- **Branch**: `p/027-judge-units-preview`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

A user deciding whether to point `--llm` at their own endpoint (a hosted model is the common case: `aguard llm setup`
offers the presets) has to accept, up front, that "best-effort-redacted excerpts" of their skills, hooks, MCP
configuration and instruction files go to that vendor. `docs/llm-judge.md` describes the rules those excerpts follow —
redaction, the two homes replaced by `~` (P-005), MCP configuration by key, padding folded (P-020), comment lines
dropped, 6,000/2,000-byte caps kept head + tail — but nothing lets the user **see** the result for their own files:

| What they can do today | What it shows |
|---|---|
| `aguard llm status` | the endpoint, model and key source — not one byte of content |
| `aguard scan --llm` / `check --llm` | the verdicts; the excerpts have already left |
| `aguard check <target> --json` | the static report; its snippets are not the judge's excerpts (different caps, no home scrub, no condensing) |
| point `base_url` at a local capture server | the request bodies — the only way, and the one P-003/P-005/P-006/P-020 all used in their tests |

The same gap hits anyone reproducing a reported judge run: a finding's `file:line` says where the judge looked, not what
it was given, and the excerpt depends on the artifact's static findings (triage evidence, the collusion pass gated on
`EXFIL-002`), on the baseline and the reputation list (they run before the judge), on `samples` and `max_calls`.
Rebuilding it by hand means re-implementing the excerpt builders, which is how a second, drifting implementation starts.

## Initial direction

A command that runs the same pipeline `scan --llm` / `check --llm` runs, up to the point where the judge would send,
and prints the planned calls instead of making them: per artifact its kind, name and canonical hash, and per call the
pass and the exact text that would sit inside the nonce fence, built by the same functions (`planFor` / `userPrompt`),
plus what the plan already knows was left out (an MCP configuration cut to fit, triage items past the cap, calls past
the budget). It never builds an HTTP client, so it joins the zero-dial test's zero table. Candidates: `aguard llm
preview [path]` next to `setup`/`test`/`status`, or `check --llm --dry-run`.
