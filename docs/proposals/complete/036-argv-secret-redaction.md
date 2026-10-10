<!-- SPDX-License-Identifier: MIT -->
# 036 — A secret given as the separate argument after `--api-key` in an MCP server's args reaches the judge in the clear: each element is redacted alone

- **Source**: new finding (2026-10-10), measured on v0.20.1: every judge payload of the 3,539 corpus samples was replayed
  through `redact.Secrets`, and 6 payloads changed on a second pass — all MCP-config excerpts of one benign sample whose
  servers pass `"--api-key", "<value>"` and `"--access-token", "<value>"` (placeholders in the corpus)
- **Depends on**: none
- **Branch**: `p/036-argv-secret-redaction`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

An MCP server is usually started with its credential on the command line:
`"args": ["-y", "@upstash/context7-mcp", "--api-key", "<key>"]`. Redaction recognises a credential that a flag
announces (`flagSecretRE`: `--api-key`, `--access-token`, `--token`, `--password`, `--secret`, … followed by `=` or
whitespace, and `flagUserPassRE`: `-u` / `--user user:pass`), but only when the flag and its value are in the same
string. In an argv array they are two elements.

The judge's MCP-config pass (`LLM-009`) sends the entry as `key=value` lines (`detect.MCPConfigLines`), one line per
array element, and `mcpExcerpt` (`internal/judge/excerpt.go`) redacts each line on its own: `args=--api-key` and
`args=<key>`. Neither line holds both halves, so the value is sent as written unless it happens to match a known token
prefix (`sk-…`, `ghp_…`, …) or the entropy floor (24 characters or more and Shannon entropy of at least 3.6). A real
short key — 16 to 20 characters, the size many vendors issue — matches neither.

The joined excerpt still has the shape the regex wants (`--api-key\nargs=<key>`: `\s` matches the newline), which is
why a second `Redact` pass over the sent text changes it: the payload is not a fixed point of the redactor, and the part
that changes is the secret.

### Measured on `origin/main` (94c8a41, v0.20.1)

A root with one `.mcp.json` holding eight servers, each value made up here (16–20 characters, no known prefix), run
through `aguard llm preview --root <fixture> --inbox off --json` (no key, nothing sent):

| Server args | Sent in the `mcp-config` payload |
|---|---|
| `"--api-key", "k7Qp2xLm9Rt4Vw8Z"` | `args=--api-key` / `args=k7Qp2xLm9Rt4Vw8Z` |
| `"--access-token", "sbp9Xk2Lq7Wm4Rt8Zv"` | `args=--access-token` / `args=sbp9Xk2Lq7Wm4Rt8Zv` |
| `"--token", "T0k3nAbCdEf9876"` | `args=--token` / `args=T0k3nAbCdEf9876` |
| `"--password", "Pw4Rd9xQz2Lm7Kt"` | `args=--password` / `args=Pw4Rd9xQz2Lm7Kt` |
| `"--secret", "S3cR3tVa1ue7Qx"` | `args=--secret` / `args=S3cR3tVa1ue7Qx` |
| `"-u", "admin:Hunt3r2Pa55wd"` | `args=-u` / `args=admin:Hunt3r2Pa55wd` |
| `"--user", "admin:Hunt3r2Pa55wd"` | `args=--user` / `args=admin:Hunt3r2Pa55wd` |
| `"--verbose", "plainword", "--version", "1.2.3"` (control) | as written — correctly |

All seven credentials are in the clear. With `--llm` against a remote endpoint, that is the user's key leaving the
machine to a model vendor — exactly what redaction exists to prevent (invariant #3, spec §16.3).

The other places an argv array is rendered or hashed, checked on the same fixture:

- **The MCP content hash** (`internal/detect/contenthash.go`) does **not** commit to the value: since P-009,
  `redactTree` views an array element that follows an element starting with `-` as `flag value`, through the same
  regexes, and `TestContentHashGolden` pins `["--api-key","<REDACTED>"]`. Measured: a second fixture with every one of
  the seven values changed gives the same eight hashes (`ctx=2b701abfd4a7…`, `supa=c5e387114e34…`, …). Nothing to fix
  there, and so nothing to re-key.
- **Static findings** (`scan --json`, text, `--html`): none of the seven values appears. A finding quotes one line of
  the MCP unit, and a line that holds only the value fires no rule.
- **Hook commands and permission entries** are single strings, so the flag and its value are redacted together.
- **The corpus, replayed here**: every one of the 3,539 corpus samples placed as the benchmark adapter places it
  (`baselines/adapter/aguard.Stage`) and run through `aguard llm preview --json`: 8,770 planned payloads, of which 2
  change under a second `redact.Secrets` pass — the `mcp-config` payloads of servers `Context7_authenticated` and
  `Supabase` in `ben-conn-spacehendrix-clauder-mcp`, i.e. the `--api-key` and `--access-token` pairs (at
  `samples: 3` each is sent three times: the 6 calls of the source measurement).

## Initial direction

One argv-aware helper in `internal/redact`, the single redaction implementation: `redact.Argv([]string) []string`
redacts every element, and views an element that follows a flag in the context of that flag, through the existing
`flagSecretRE` / `flagUserPassRE` (no second flag list). `mcpExcerpt` applies it to each run of lines that come from
one array before its per-line redaction. Bumps `ExcerptVersion` (what the MCP pass sends changes for such configs).
The content hash, static snippets and `internal/collect` are not touched.

## Done criteria

- [x] `TestPlan_MCPArgvSecretsAreRedacted` (`internal/judge/plan_test.go`): an MCP entry with the seven pairs measured
  above; the planned `mcp-config` payload contains none of the seven values, carries `args=--api-key <REDACTED>`
  (and `args=-u admin:<REDACTED>` for `-u`, likewise `--user`; question 7), and is a fixed point of `detect.Redact` —
  the property the corpus replay checks. **Red on the base**: every value is in the payload, and a second `Redact` changes it
- [x] `TestArgv` (`internal/redact/redact_test.go`, table-driven): an element after each flag spelling `flagSecretRE`
  knows (`--password`, `--passwd`, `--passphrase`, `--pass`, `--token`, `--secret`, `--api-key`, `--api_key`,
  `--apikey`, `--access-token`, `--access_token`) becomes `<REDACTED>`; after `-u` / `--user` a `user:pass` element
  becomes `user:<REDACTED>`; a value holding a space or a quote is replaced to the end of the element, never only up to
  it; `Argv(Argv(x)) == Argv(x)`, the output has as many elements as the input, and joining the output with a space or
  a newline gives a fixed point of `Secrets`
- [x] **Reverse assertion, unit level** (`TestArgv`): an element that follows no flag, or a flag that announces no
  credential (`--verbose plainword`, `--version 1.2.3`, `-y @scope/pkg`, `--port 8080`, `-u root` without a colon,
  `--api-key=<v>` in one element followed by a positional), comes out exactly as `Secrets` gives it alone
- [x] **Reverse assertion, excerpt level** (`TestPlan_MCPExcerptWithoutSecretFlagsIsUnchanged`): for MCP entries
  without such a pair, the payload is byte-identical to the per-line rendering the base sends (each `MCPConfigLines`
  line masked and redacted alone), so a configuration with no secret flag sends what it sent before
- [x] **Reverse assertion, corpus level** (scratch replay, not committed): over the 3,539 samples, the second
  `Secrets` pass changes 2 payloads before and 0 after; every other planned payload and every artifact hash is
  byte-identical between the base binary and this branch's
- [x] **No re-key**: `TestContentHashGolden` and every content-hash test pass unchanged, and
  `git diff --stat origin/main -- internal/detect/contenthash.go internal/collect internal/reputation internal/gate`
  is empty: no stored approval and no reputation entry is invalidated
- [x] `ExcerptVersion` is 2, pinned with the digest of the golden fixture whose MCP entry now carries an `--api-key`
  pair (`TestExcerptVersion_IsPinnedWithItsGolden`)
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency

## Out of scope

- **The content hash does not change**, for any artifact: it already pairs a flag with the next element (measured
  above). `internal/detect/contenthash.go`, `internal/collect/hash.go`, `internal/reputation`, `internal/gate`: zero
  diff. Its one difference from the new excerpt view — for `"--password", "correct horse"` it keeps the tail ` horse`
  because it takes the regex's reading — is named as a follow-up, since changing it re-keys
- **No pattern changes**: `flagSecretRE`, `flagUserPassRE`, `assignRE`, the prefix list and the entropy floor stay as
  they are, so `Secrets` and `Credentials` return the same output for every string as on the base. A flag the patterns
  do not know (`--key`, `-p`, `--db-pass` with a value under 12 characters) is not added here
- **Static findings and their snippets do not change**: the MCP rule unit, `jsonStrings`, `redactClip` and the line
  rules are untouched (no value reached a report in the measurement)
- **Hook commands, permission entries and connector tool lists are not touched**: each is a single string or carries
  no argv
- **The judge's other passes, prompts, caps and `PromptVersion` do not change**; only the `mcp-config` payload of an
  entry with such a pair does
- `baselines/results/` does not change by a byte; no run is re-folded

## Must not claim

- Not "a credential on a command line is always redacted": only one a flag announces in a shape the existing patterns
  know, plus the known prefixes and the entropy catch-all. `--key <v>`, `-p <pw>` and `--db-pass <short>` still go out
  as written — redaction stays best-effort (spec §16.3)
- Not that the content hash was wrong or is changed: it was measured and already did not commit to these values
- Not that the corpus result says anything about configurations outside the corpus: it says this branch removes the two
  payloads it found and changes no other byte there

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The excerpt-level tests: the seven pairs (red on the base) and the no-pair reverse assertion | `judge: tests — a secret after a secret-carrying flag in MCP args reaches the judge's payload (P-036)` |
| 2 | `redact.Argv` with its table test, and the `detect.RedactArgv` delegation the judge calls | `redact: Argv views an argument that follows a flag together with that flag (P-036)` |
| 3 | `mcpExcerpt` redacts each run of lines from one array as an argv; `ExcerptVersion` 2 with the golden fixture carrying a pair | `judge: redact an MCP entry's array as one argv, and bump ExcerptVersion to 2 (P-036)` |
| 4 | The `docs/llm-judge` pair, spec §16.3 and the invariant #3 note name the argv view | `docs: an argument after a flag is redacted with its flag (P-036)` |

## Open questions

1. **How much of an announced element is replaced?** The regex reading of `flag value` stops at whitespace or a
   quote, so `"--password", "correct horse"` would send ` horse` and `"--api-key", "ab'cd"` would send `'cd`. In argv
   the element is one argument, all of it the value. **Recommendation**: from the first byte the flag announced to the
   end of the element, keeping what comes before it (the `admin:` of `-u admin:pw`, as `flagUserPassRE` does today).
   **Decided (2026-10-10)**: as recommended
2. **Which element counts as a flag, and where does the list of secret flags live?** **Recommendation**: any element
   starting with `-` (the predicate the content hash already uses); whether it announces a credential is decided by
   running `Credentials` over `flag + " " + value` and checking that the flag survives and the value changed — so the
   only flag list is `flagSecretRE` / `flagUserPassRE` (and the key words of `looseAssignRE`), with no second copy.
   **Decided (2026-10-10)**: as recommended
3. **Should the content hash move to the same helper?** It already pairs, through its guarded `Credentials` view, and
   moving it would re-key every MCP entry with a whitespace- or quote-bearing flag value. **Recommendation**: leave it
   byte-for-byte; record the tail difference (question 1) as a follow-up that re-keys and needs its own proposal.
   **Decided (2026-10-10)**: as recommended
4. **Where is an array recognised in the excerpt?** `MCPConfigLines` flattens an array into consecutive lines with the
   same key, and object keys are unique and sorted, so a run of lines sharing a key is one array in order.
   **Recommendation**: group runs there, in `mcpExcerpt`, rather than changing `MCPConfigLines` (its values are raw by
   contract and the content hash and rules read the same entry). The one way two arrays' worth of lines can share a key
   — a literal dotted key `"a.b"` beside an object `a` with key `b` — costs at most one over-redacted value.
   **Decided (2026-10-10)**: as recommended
5. **Static snippets?** A finding quotes one line of the MCP unit and a line rule cannot see the line before it; no
   value reached a report in the measurement. **Recommendation**: do not change them here. **Decided (2026-10-10)**: as
   recommended
6. **`ExcerptVersion`?** What the `mcp-config` pass sends changes for an entry with a pair. **Recommendation**: bump to
   2, as `.claude/rules/judge.md` requires, and add an `--api-key` pair to the golden fixture so the pin exercises the
   change. **Decided (2026-10-10)**: as recommended

Asked during implementation (stage 2):

7. **One line or two for a redacted pair?** Written as `args=--api-key` then `args=<REDACTED>`, the payload holds no
   secret but is still not a fixed point of `Redact`: read across the newline, `flagSecretRE` takes `args=<REDACTED>`
   for the flag's value, and a second pass rewrites it to `<REDACTED>` — the corpus replay would keep counting the two
   payloads. **Recommendation**: write an announced element on its flag's line, `args=--api-key <REDACTED>`, the pair
   as one command line reads it; an element no flag announces keeps its own line, so an entry without a pair is
   unchanged. `redact.Argv` itself keeps one output element per input element; the merge is the excerpt's rendering.
   **Decided (2026-10-10)**: as recommended

## Done

```
Merged: PR #55 (2026-10-10; find the sha with git log --grep P-036)
Released: pending release
Evidence: TestPlan_MCPArgvSecretsAreRedacted (internal/judge/argv_test.go): red on the base with the test alone (commit
  "judge: tests — …"): all 9 rows send the value, 8 of them are not a fixed point of Redact (`-u` is, and still leaks);
  green after W3
Evidence: `aguard llm preview --root <fixture> --inbox off --json`, eight servers: 7 of 7 made-up values in the
  mcp-config payloads on the base → 0 on the branch (`args=--api-key <REDACTED>`, `args=-u admin:<REDACTED>`); the
  control server `--verbose plainword --version 1.2.3` byte-identical; the eight content hashes identical on both
  binaries, and identical again for a second fixture with all seven values changed (the hash already pairs, P-009)
Evidence: corpus replay (3,539 samples placed by baselines/adapter/aguard.Stage, `llm preview --json`, scratch, not
  committed): payloads changed by a second redact.Secrets pass 2 → 0 of 8,770 (the Context7_authenticated and Supabase
  mcp-config payloads of ben-conn-spacehendrix-clauder-mcp); 8,768 payloads and all 4,704 artifact hashes byte-identical
  between the base binary and the branch's
Evidence: reverse — TestPlan_MCPExcerptWithoutSecretFlagsIsUnchanged green on the base and the branch; TestArgv's
  reverse rows (`--verbose plainword`, `--version 1.2.3`, `-y @scope/pkg`, `--port 8080`, `-u root`, `--api-key=<v>`
  then a positional, `--key <v>`, `--db-password hunter2`) equal Secrets of each element; with argvView wired and the
  golden fixture not yet carrying a pair, TestExcerptVersion_IsPinnedWithItsGolden stayed green on digest
  47f51be199b21e6a; with the `--api-key` pair added the fixture digest is 62df741d4f245be1 without the fix and
  270922b0e0fc4223 with it, pinned as {2, 270922b0e0fc4223}
Evidence: no re-key — TestContentHashGolden unchanged and green; `scan --root ~/.claude --json --inbox off` gives the
  same JSON on both binaries (184 artifacts, 28 of them MCP servers, overall 69; scanned_at aside); reputation.json's
  18 entries are plugins and Claude Desktop skills (by name and source), and no hash of any kind moved, so 0 reputation entries and 0 stored gate
  approvals are invalidated
Evidence: not done — `git diff --stat origin/main -- internal/detect/contenthash.go internal/detect/contenthash_test.go
  internal/collect internal/reputation internal/gate internal/redact/redact.go internal/redact/redact_test.go baselines
  go.mod go.sum docs/rules.md` is empty
Verify: make verify → "verify: all gates passed" (go1.23.5, no toolchain switch; go.mod line 2 `go 1.23.5`)
```

Follow-ups left out on purpose:

- The content hash reads a flag's value as the regex does, so `"--password", "correct horse"` hashes the tail
  ` horse`. Replacing the whole element there re-keys those entries, which needs its own proposal.
- Flags the patterns do not know (`--key`, `-p`, `--db-pass` with a value under 12 characters) still send their value
  and enter the hash. Widening `flagSecretRE` is a pattern change that re-keys, so it needs its own proposal.
