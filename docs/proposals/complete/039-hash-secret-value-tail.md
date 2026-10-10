<!-- SPDX-License-Identifier: MIT -->
# 039 — The content hash of an MCP server keeps the tail of a secret that holds a space or a quote: part of the secret is a digest input

- **Source**: follow-up recorded by P-036 ("Follow-ups left out on purpose" and its open question 3): the content hash
  reads an argument after a flag the way the shell-line patterns do, so `"--password", "correct horse"` hashes the tail
  ` horse`; measured on `origin/main` (6cab205) on 2026-10-10
- **Depends on**: P-036 (`redact.Argv`)
- **Branch**: `p/039-hash-secret-value-tail`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The content hash of a hook, MCP server or permission list is printed in the JSON report and stored as the key of every
gate approval and reputation entry. A digest over a low-entropy secret is a commitment anyone can brute-force, so the
hash replaces secrets before digesting (`.claude/rules/hash.md`, the W-006 principle: no hash may be a digest of
credential material). In an argument array, `redactTree` (`internal/detect/contenthash.go`) reads an element that
follows an element starting with `-` as the one string `flag value`, through `redact.Credentials`, and keeps the part
after the flag.

Those patterns were written for a shell line, where a value ends at whitespace or a quote. In an argv the element is one
argument, all of it the value. P-036 made the judge's excerpt replace an announced element from its first announced
byte to its end (`redact.Argv`); the hash still takes the regex reading, so for
`"args": ["--password", "correct horse"]` the digest input is `["--password","<REDACTED> horse"]`.

### Measured on `origin/main` (6cab205)

One `~/.claude.json` with twelve servers, each `{"command":"mytool","args":[…]}`, values made up here, run through
`aguard scan --root <fixture>/.claude --inbox off --json` (first 16 hex digits of each `hash`):

| Server `args` | Hash on main |
|---|---|
| `"--password", "correct horse"` | `6769c53bf10840ba` |
| `"--password", "battery horse"` | `6769c53bf10840ba` (same tail, same hash) |
| `"--password", "correct staple"` | `602c881111dbbf1f` (same head, another hash) |
| `"--api-key", "ab'cd"` / `"zz'cd"` / `"ab'zz"` | `46cc27fa4f60a2ae` / `46cc27fa4f60a2ae` / `b5d29e44b6547cb1` |
| `"-u", "admin:pass word"` / `"admin:fish word"` / `"admin:pass bird"` | `2a2a4a13d16d6ac3` / `2a2a4a13d16d6ac3` / `79de20e608d15749` |
| `"--password", "hunter2xyz"` / `"letmein99"` (control: no space, no quote) | `ad651d55e48bf867` for both — correct |

Consequences:

1. **Part of the secret is a digest input, and it can be recovered from the published hash.** Knowing the rest of the
   entry (a server's README gives the command and the flag), five candidate words tried with
   `printf 'aguard:mcp:v1\0{"args":["--password","<REDACTED> %s"],"command":"mytool"}' "$w" | shasum -a 256` give the
   full 64-digit hash of the `correct horse` server for `w=horse` and for no other — the tail of the password, read back
   from the report.
2. **The identity follows a fragment of the secret.** "Changing only a replaced secret does not re-key" is the hash's
   stated contract (`contenthash.go`, `TestContentHash_SecretsAreNotDigestInputs`). Here it holds for half the value:
   rotating `correct horse` to `battery horse` keeps the approval, rotating it to `correct staple` re-asks; two machines
   with different passwords that share a last word share one identity, while two machines whose passwords differ only
   in the last word get two — the identity is decided by a fragment of the secret, not by the configuration.

The same is true wherever the patterns stop before the element ends: for a key word inside a flag
(`--github-token`, `--client-secret`, read by `looseAssignRE`) that is any byte outside `[A-Za-z0-9/+_.\-]`, so
`"--client-secret", "abcdefghijklmnop=="` hashes `<REDACTED>==` and `"--github-token", "abcdefghijklmn@xy"` hashes
`<REDACTED>@xy` (measured: two such servers with different heads share a hash on main).

The judge's `mcp-config` excerpt of the same twelve servers (`aguard llm preview --json`) already carries
`args=--password <REDACTED>`, `args=--api-key <REDACTED>` and `args=-u admin:<REDACTED>`: after P-036, the hash is
the one place an argv tail survives.

## Initial direction

`redactTree` asks the same element-level question `redact.Argv` asks — through one exported helper in
`internal/redact` that `Argv` itself calls, not a copy — and replaces an announced element from its first announced
byte to its end, under the structure guard the hash already has. This re-keys every MCP entry that carries such a
value (and nothing else), so `TestContentHashGolden` gains a fixture with one and keeps its five constants.

### Where else the tail survives (measured on main, same fixtures plus hooks and a permission entry)

| Place | `--password` value with a space | Result |
|---|---|---|
| Judge `mcp-config` excerpt (P-036) | `["--password", "correct horse"]` | `args=--password <REDACTED>` — no tail |
| Static findings of an MCP server | same | the value line fires no rule; nothing quoted |
| Env value under a credential key (hash: MCP `env`, settings `env` block) | `"DB_PASSWORD": "correct horse"`, `"API_TOKEN": "correct horse"` | hashes the value as `<REDACTED> horse` (read by `assignRE` as `KEY=VALUE`): two entries whose passwords differ only in the last word hash differently. Not argv: a key/value pair (see Out of scope) |
| Hook command, unquoted (snippet, judge, hash) | `… --password correct horse` | `--password <REDACTED> horse` — correct: to a shell, `horse` is another argument |
| Hook command, quoted (snippet, judge, hash) | `… --password "correct horse"` / `'correct horse'` | **the whole value in the clear** in `EXEC-001`/`HOOK-001` snippets, the judge's `injection`/`capability`/`triage` payloads and the hash input: `flagSecretRE` cannot start a value at a quote |
| Hook command, `=` and quoted | `… --password="correct horse"` | `--password="<REDACTED> horse"` (read by `assignRE`) |
| Permission entry, quoted | `Bash(… --password "correct horse")` | the whole value in the hash input (same pattern gap) |

The quoted shell-line forms are a pattern gap in `redact.Credentials`, not the argv view: fixing them changes static
snippets and the judge payload as well as re-keying hooks and permission lists, so they are recorded as a follow-up,
not done here.

## Done criteria

- [x] `TestContentHashGolden` (`internal/detect/contenthash_test.go`) gains a sixth case, an MCP server in the same
  fixture file whose args carry `"--password", "correct horse battery"` and `"-u", "admin:pass word"`: its canonical
  input is pinned as `…"--password","<REDACTED>","-u","admin:<REDACTED>"…` and its digest as a literal computed by hand
  (`printf 'aguard:mcp:v1\0%s' … | shasum -a 256`). **Red on the base**: the input holds `<REDACTED> horse battery` and
  `admin:<REDACTED> word`. The five existing constants (inputs and digests) do not change by a byte
- [x] `TestContentHash_ArgvValueIsForgottenWhole` (same file): for an announced element with a space, a tab, a quote,
  base64 padding after a key-word flag, or a `user:pass` with a space, two configurations whose secrets differ anywhere
  in the element hash the same as the one written with `<REDACTED>`, and no fragment of the secret is in the digest
  input. **Red on the base** for every row
- [x] **Reverse assertion, structure** (same test): an announced element whose replaced span would hold a shell
  structure character (`-u admin:pw $(curl …|sh)`) still hashes differently from `admin:hunter2` — the guard is not
  weakened. Green on the base and the branch
- [x] **Reverse assertion, no tail** (`TestContentHash_ArgvWithoutTailIsUnchanged`): canonical inputs measured on the
  base for elements with nothing after the patterns' match (`--api-key k7Qp…`, `-u admin:hunter2`, `--github-token
  <16 chars>`), elements no flag announces (`--verbose "plain word"`, `-y @scope/pkg`, `--port 8080`, `-u root`), the
  one-string `--api-key=abc def`, and an announced element whose span holds structure (kept as the base reads it) are
  pinned as literals; green on the base and the branch
- [x] `redact.Argv` keeps its behaviour: `TestArgv` passes unchanged, the new helper has its own table rows, and the
  judge's excerpts do not move by a byte — `TestExcerptVersion_IsPinnedWithItsGolden` unchanged, `ExcerptVersion` stays
  where `origin/main` has it at rebase time
- [x] **Reverse assertion, corpus** (scratch replay, not committed): every artifact hash and every planned judge payload
  of the 3,539 corpus samples (placed by `baselines/adapter/aguard.Stage`, `aguard llm preview --json`) compared between
  the base binary and the branch's; every changed hash is listed and explained, and no payload changes
- [x] Re-key accounting: entries of `internal/reputation/data/reputation.json` whose hash changes are counted (expected
  0: all 18 are plugin and Claude Desktop skill tree hashes, which `redactTree` never computes); the real
  `scan --root ~/.claude --json` is compared between the two binaries and the number of changed hashes reported
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency; no toolchain switch

## Out of scope

- **Patterns do not change**: `flagSecretRE`, `flagUserPassRE`, `assignRE`, `looseAssignRE`, the prefix list and the
  entropy floor stay as they are, so `redact.Secrets` and `redact.Credentials` return the same output for every string.
  Widening the set of flags (`--key`, `-p`, `--db-pass`) is P-040's
- **Quoted values in shell lines** (`--password "correct horse"` in a hook command or a permission entry) are not
  touched: that is a pattern gap that also changes snippets and the judge's payload (measured above); follow-up
- **Env and header values under a credential key** (`"DB_PASSWORD": "correct horse"`, the settings `env` block) keep
  the `KEY=VALUE` reading: a key/value pair, not a flag/value pair, and a separate re-key; follow-up
- **The domain suffixes do not change** (`aguard:mcp:v1` …): only entries holding such a value re-key (question 2)
- `internal/collect/hash.go` (`TreeHash`/`FileHash`), `internal/reputation`, `internal/gate`, `internal/judge`, the
  rule table and `docs/rules.md`: zero diff

## Must not claim

- Not "a content hash never commits to a secret": only an element a flag announces in a shape the existing patterns
  know is forgotten whole. A flag they do not know, a quoted value in a shell line, an env value with a space, and an
  announced element whose tail holds a shell structure character (`"correct horse$x"`: the guard refuses the whole-element
  replacement and the base's reading stays, tail included) still put secret material into the digest input
- Not that the change affects only "values with a space or a quote": it affects every announced element whose patterns'
  match ends before the element does — for a key word inside a flag that includes `=` padding, `@`, `:` and other bytes
  outside `[A-Za-z0-9/+_.\-]`
- Not that the corpus result says anything about configurations outside the corpus

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The hash-level tests: the golden case with a tail and the whole-element rows (red on the base), the structure and no-tail reverse assertions (green on both) | `detect: tests — an argv secret with a space or a quote leaves its tail in the content hash (P-039)` |
| 2 | `redact.Announced`, the element-level question `Argv` asks, exported with its own rows; `Argv` calls it and does not change | `redact: Announced says where in an argument the value a flag announces starts (P-039)` |
| 3 | `redactTree` replaces an announced element from that byte to its end, under the structure guard, else keeps its reading | `detect: an argument a flag announces is forgotten whole by the content hash (P-039)` |
| 4 | `.claude/rules/hash.md`, the invariant #3 note, spec §8 and §16.3, and the architecture pair name the rule and the re-key | `docs: the content hash forgets an announced argument whole (P-039)` |

## Open questions

1. **Call `redact.Argv`, or share the question it asks?** `Argv` passes every element through `Secrets`, entropy
   catch-all included, and has no structure guard; the hash deliberately takes only `Credentials` (a base64 payload
   replaced before hashing could be swapped without re-keying, `.claude/rules/hash.md`) and refuses a replacement that
   takes structure. Calling `Argv` would import both differences into the hash. **Recommendation**: export the one
   decision `Argv` makes — is this element announced by the flag before it, and at which byte does the value start —
   as `redact.Announced(flag, arg) (int, bool)`, and have `Argv` call it. Each caller then redacts the kept head with
   its own half (`Secrets` for the judge, guarded `Credentials` for the hash). One flag list, one decision, no copy
   (invariant #3). **Decided (2026-10-10)**: as recommended
2. **Bump the domain suffix (`aguard:mcp:v1`)?** The comment above the constants says to bump it "with any change to
   what goes in". A bump re-keys every hook, MCP and permission entry, including the ones whose input does not change,
   whose golden constants must stay. **Recommendation**: keep `v1`; reword the comment so the suffix moves with the
   definition's shape (fields, encoding, markers), while a change to what the redaction forgets re-keys only the entries
   holding such a value and is named in its proposal. **Decided (2026-10-10)**: as recommended
3. **What if the whole-element replacement would take structure?** **Recommendation**: refuse it and keep the base's
   reading of that element — never more of the secret than the base commits to — rather than the value as written (more)
   or a cut at the first structure character (a third reading nothing else uses). The residual goes into Must not claim.
   **Decided (2026-10-10)**: as recommended
4. **Which arrays?** **Recommendation**: every array `redactTree` walks — the same pairs the base reads as
   `flag value` — with the structure set of the mode it already uses (shell for `command`/`args`, literal elsewhere; a
   grant string is read alone, as before). **Decided (2026-10-10)**: as recommended
5. **`ExcerptVersion`?** W2 does not change `Argv`'s output. **Recommendation**: no bump; prove it with `TestArgv`,
   the excerpt pin and the corpus payload comparison; keep whatever `origin/main` has at rebase time (P-037 may set 3).
   **Decided (2026-10-10)**: as recommended
6. **Env and header values with a space (`KEY=VALUE`)?** The same kind of tail, measured above, but a key/value pair:
   another reading, another re-key (the settings `env` domain too), and the judge already sends such values by key name
   (P-005). **Recommendation**: not here; a follow-up that measures it first. **Decided (2026-10-10)**: as recommended
7. **Stored approvals.** **Recommendation**: no migration and no code in `internal/gate`. A re-keyed MCP server is
   listed again by the gate's `SessionStart` notice (which informs and never blocks) when it has findings at the
   threshold, until `aguard approve` stores the new key, once; the old record stays in the approvals file and matches
   nothing. A migration would have to recompute the old key from the secret's tail — the commitment being removed.
   **Decided (2026-10-10)**: as recommended

## Done

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-039 after the merge)
Released: pending release
Evidence: TestContentHashGolden case "mcp server, announced arguments holding a space" and
  TestContentHash_ArgvValueIsForgottenWhole (internal/detect/contenthash_test.go, contenthash_argv_test.go): red on the
  base with the tests alone (commit "detect: tests — …"): the golden input held `<REDACTED> horse battery` and
  `admin:<REDACTED> word` (digest 47b731339abf…), and all 7 rows re-keyed with the secret and put its fragment in the
  input; green after W3 with the hand-computed digest 630d1bd53d01…
Evidence: fixture of twelve servers, `scan --json` on both binaries: the nine with a tail re-key, each onto the hash of
  the same entry without one (`correct horse` / `battery horse` / `correct staple` → ad651d55e48b, the `hunter2xyz`
  server's); the five-word brute force that recovered ` horse` from the main hash matches nothing on the branch; the
  control servers, three hook commands and the permission list hash as on main. Second fixture: `--client-secret
  <16>==`, `--github-token <14>@xy` and a tab re-key; `correct horse$x`, `-u admin:$(curl …|sh)` and the env values
  hash as on main (the residuals named above)
Evidence: reverse — the five existing golden constants, TestContentHash_ArgvWithoutTailIsUnchanged (10 inputs measured
  on the base) and the structure rows are green on the base and the branch; TestArgv unchanged and green, TestAnnounced
  checks Argv against Announced row by row; `llm preview --json` payloads of the three fixtures byte-identical between
  the binaries; ExcerptVersion stays 2 (TestExcerptVersion_IsPinnedWithItsGolden unchanged)
Evidence: corpus replay (3,539 samples placed by baselines/adapter/aguard.Stage, `llm preview --json`, scratch, not
  committed): 0 of 4,704 artifact hashes and 0 of 8,770 planned payloads differ between the base binary and the
  branch's — no corpus entry carries an announced argument the patterns stop reading early
Evidence: re-key accounting — reputation.json: 0 of 18 entries (all plugin and Claude Desktop skill tree hashes, which
  redactTree never computes; internal/collect untouched). `scan --root ~/.claude --json --inbox off`: 184 artifacts
  (28 MCP servers), overall 69, 0 hashes changed, JSON identical apart from scanned_at and tool_version. Stored
  approvals: on a fixture approved with the base binary, the branch's SessionStart lists the server again (it informs,
  never blocks); one `aguard approve` stores the new key and the old record stays, matching nothing
Evidence: not done — `git diff --stat origin/main -- internal/collect internal/reputation internal/gate internal/judge
  internal/redact/redact.go internal/redact/redact_test.go docs/rules.md go.mod go.sum baselines docs/llm-judge.md
  docs/llm-judge.zh-CN.md` is empty
Verify: make verify → "verify: all gates passed" (go1.23.5, no toolchain switch; go.mod line 2 `go 1.23.5`)
```

Follow-ups left out on purpose:

- A quoted value in a shell line — `--password "correct horse"` or `'correct horse'` in a hook command or a permission
  entry — is not redacted at all: it reaches static snippets (`EXEC-001`, `HOOK-001`), the judge's payloads and the
  hash input; `--password="correct horse"` keeps ` horse`. A pattern change in `redact.Credentials`, which re-keys hooks
  and permission lists and changes snippets.
- An env or header value under a credential key that holds a space (`"DB_PASSWORD": "correct horse"`, the settings
  `env` block) keeps its tail in the MCP and settings-env hashes. Another reading (`KEY=VALUE`) and another re-key.
- An announced argument whose tail holds a shell structure character keeps the base's reading, tail included.
