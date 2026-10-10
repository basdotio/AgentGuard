<!-- SPDX-License-Identifier: MIT -->
# 040 — A value after a credential flag the patterns do not know (`--key`, `--private-key`, `--db-pass`, `-p`) reaches the judge in the clear and enters the content hash

- **Source**: follow-up recorded by P-036 ("Flags the patterns do not know … still send their value and enter the
  hash"); `.claude/rules/hash.md` also names `--db-password …` and `-p<pw>` among the secrets that enter the digest
  input as written
- **Depends on**: P-039 (`p/039-hash-secret-value-tail`), which changes how the content hash views an argv value; this
  proposal widens which flags announce one, and lands after it
- **Branch**: `p/040-secret-flag-coverage`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see
README.md. -->

## Problem

Redaction knows a credential on a command line only by the flag in front of it. `flagSecretRE` names eight flags
(`--password`, `--passwd`, `--passphrase`, `--pass`, `--token`, `--secret`, `--api-key` with its `_`/no-separator
spellings, `--access-token`/`--access_token`); `flagUserPassRE` adds `-u`/`--user user:pass`. Since P-036 the same
patterns decide which element of an MCP server's `args` is a secret (`redact.Argv`), and the content hash has read an
array that way since P-009. A flag outside those patterns announces nothing, so its value is redacted only if it
happens to carry a known token prefix or clear the entropy floor (24 characters or more, Shannon entropy at least 3.6).

### Measured on `origin/main` (6cab205)

A scratch probe in `internal/redact` (not committed) ran `redact.Argv([flag, v])`, `redact.Secrets(flag+" "+v)` and
`redact.Secrets(flag+"="+v)` for made-up values of 8, 11, 12, 16 and 20 characters with no known prefix (all under the
entropy floor). The two-element and one-string readings agree for every flag, so one column stands for both:

| Flag | `flag v` / `["flag","v"]` | `flag=v` |
|---|---|---|
| `--password`, `--pass`, `--token`, `--secret`, `--api-key`, `--apikey`, `--access-token` | redacted at every length | redacted |
| `--db-password`, `--auth-token`, `--api-token`, `--client-secret`, `--refresh-token`, `--bearer`, `--auth`, `--access-key` | redacted from 12 characters only (`looseAssignRE`: the flag ends in a key word) | redacted from 4 |
| `--key`, `--private-key`, `--secret-key`, `--auth-key`, `--app-key`, `--db-pass`, `--pat`, `--pwd`, `--credential(s)`, `-p`, `-k`, `-t` | **not redacted at any length** | **not redacted** |

`--secret-key` is the sharpest case: the key word is in the flag, but not at its end, so neither `flagSecretRE`
(anchored to `--secret` followed by `=` or whitespace) nor `looseAssignRE` (key word then whitespace) sees it.

`aguard llm preview --root <fixture> --inbox off --json` (no key, nothing sent) on a `.mcp.json` with one server per
flag confirms it end to end: the `mcp-config` payload carries `args=--key` / `args=k7Qp2xLm9Rt4Vw8Z`,
`args=-p` / `args=Pw4Rd9xQz2Lm7Kt`, and likewise for `--db-pass`, `--private-key` and `--pat`; `--auth-token`,
`--client-secret` and `--bearer` with 12+ character values come out as `args=--auth-token <REDACTED>`.

The consequences, where each reading is used:

- **The judge** (`--llm`, `mcp-config` pass and every pass that quotes a hook command or a script line): the value
  leaves the machine to the configured model vendor. That is what invariant #3 and spec §16.3 exist to prevent.
- **Static snippets** (`scan` text, `--json`, `--html`, `--md`, SARIF): a finding that quotes a line holding
  `--key <v>` in one string (a hook command, a script line, a permission entry) prints the value.
- **Content hashes** of MCP servers, hooks and permission entries: the value goes into the digest input, so a stored
  approval or a hash printed in a report is the digest of a short credential — the thing the hash rules say no hash
  may be (W-006).

### Measured: what actually follows a flag (stage 2, 2026-10-10)

A scratch probe (not committed) walked the 3,539 corpus samples of the benchmark list and the real `~/.claude`
(`settings*.json`, `~/.claude.json` with its `projects` entries, every JSON file under `plugins/`, and the
`.mcp.json` / `.claude/settings*.json` of the 54 project directories `~/.claude.json` names) and paired every flag with
the value that follows it: `--x v`, `--x=v`, `-x v` and an attached `-p<v>`. MCP `args` were paired element by element,
hook commands and the pattern inside a permission entry were split as a shell line. Only flag names, counts and the
**shape** of each value were recorded (placeholder such as `$VAR`, `<…>` or `YOUR_KEY`; path; URL; number; file name;
word; key-like, meaning eight characters or more with letters and digits; other). No value was printed or stored.

| Population | Flag/value pairs | Distinct flags | Key-like values | Pairs whose flag names a credential |
|---|---|---|---|---|
| Corpus, MCP `args` + hooks + permissions | 711 (172 + 269 + 270) | 129 | 0 | 4: `--api-key` ×2, `--access-token` ×1 (placeholders, already covered), `--secret_arn` ×1 (an ARN, not a credential) |
| `~/.claude`, the same three surfaces | 488 (90 + 32 + 366) | 53 | 2 (both after `-f` in permission entries) | 0 |

So in configuration — the only surfaces that enter a content hash — neither population carries a single value after a
flag outside the eight, secret or not. The corpus is curated and holds placeholders where a credential would be, and
this `~/.claude` names no credential flag in any MCP `args`, hook or permission entry. **Neither can show a protected
secret**; what they can show is the cost of each candidate, so the measurement was widened to the text the scanner
also quotes and the judge also sends: every line of every non-JSON file in the corpus samples (23,311 pairs) and of
`~/.claude`'s
`skills/`, `agents/`, `commands/`, `plugins/`, `hooks/`, `rules/` and `CLAUDE.md` (51,314 pairs).

**Long flags.** Two candidate rules were measured over all four populations (configuration and text, corpus and
`~/.claude`; 75,824 pairs), counting occurrences of flags each rule would add to the eight:

| Rule | Adds | Credential by name | A path to a credential file | Unclear | Not a credential |
|---|---|---|---|---|---|
| **A** — the flag's last `-`/`_` word is any of `password passwd passphrase pass pwd secret token key apikey pat credential(s)` | 66 occurrences, 26 flags | 38 (`--key` 17, `--client-secret` 6, `--user_pass` 3, `--admin_password`, `--certificate-key`, `--certificate-passphrase` 2 each, six more 1 each) | 3 (`--private-key` 2, `--from-private-key` 1: all paths or file names) | 9 (`--data-key` 8, `--resource-key` 1) | 16 (`--from-token`/`--to-token`/`--sell-token`/`--buy-token` 8: token symbols and contract addresses; `--meta_key` 2, `--space-key`, `--assignment-key`, `--page-token`, `--no-password` 1 each; `--generate-password` 2) |
| **B** — the last word is in the password family or `secret` (`password passwd passphrase pass pwd secret`); `key` only bare or after one qualifier from `api app private secret access auth client license master encryption signing`; `--pat`, `--bearer`, `--credential(s)` only bare; `token` compounds unchanged | 37 occurrences, 10 flags | 32 (`--key` 17, `--client-secret` 6, `--user_pass` 3, `--admin_password` 2, `--certificate-passphrase` 2, `--keychain-password`, `--token-secret` 1 each) | 2 (`--private-key`: both paths) | 0 | 3 (`--generate-password` 2, `--no-password` 1) |

The two words that cost rule A its false positives are `token` (1 credential flag, `--remote-token`, against 9
occurrences of flags that are not) and an open-ended qualifier before `key` (`--meta_key`, `--space-key`,
`--assignment-key`). Both are why B keeps them closed. A flag whose credential word is **not** last is excluded by
both rules, by construction; measured, those are 49 occurrences of 19 flags, none of them a credential value:
`--key-id` 15, `--secret-id` 9 (8 of them paths), `--discovery-token-ca-cert-hash` 4 (a public CA hash),
`--password-stdin` 2, `--key-stdin` 2, `--token-in`/`--token-out` 4, `--key-file`, `--secret-file`,
`--vault-password-file`, `--token-type`, `--token-name`, `--secret_arn` 1 each, `--to-secret-id` 2 and five more 1
each. `--token-endpoint` and `--password-file` occur nowhere; `--keyring` once, with a path.

**Short flags.** `-p` precedes a value 2,136 times (421 in the corpus, 1,715 in `~/.claude`; 21 of them in
configuration). 1,731 of those are `mkdir -p <dir>`, where `-p` takes no value at all and a regex would still read the
directory as one; then `claude -p <prompt>` 100, `ps -p <pid>` 22, `ssh -p <port>` 21. The attached form `-p<v>`
occurs 256 times, all `find -path` / `-print`, `dotnet -p:<prop>` and the like; 0 key-like. `-P` (23) is a password
to `sqlcmd` and `bcp` (9, placeholders)
and a regex flavour or a parent pid to `grep` and `pgrep`; `-k` (53) is a `pytest` filter or a `launchctl` switch;
`-t` (1,361) is `xargs`, `ls`, `tmux` and `docker`. Seven values after these four flags are key-like — four
`sourmash -p` parameter strings, a parent issue after `jira issue create -P`, an issue key after `zaira history -k`,
one after `-t` in a comment — and none of them is a credential.

**What each rule changes end to end.** Both rules were built as scratch patches to `Credentials` (reverted; nothing
committed) with the same exemption — a value that is a URL or starts with `/`, `./`, `../`, `~/` or ends in a key-file
extension, and a flag starting with `no-`, announce nothing — and compared with the base binary: every corpus sample
placed as the benchmark adapter places it (`baselines/adapter/aguard.Stage`) through `scan --json` and
`llm preview --json` (no key, nothing sent), and `~/.claude` through both with `--inbox off`.

| | Content hashes | Static findings (whole finding JSON, snippet included) | Judge payloads |
|---|---|---|---|
| Corpus, base totals | 4,704 artifacts | 1,618 findings, 164 scan notes | 8,770 |
| Corpus, rule A | 0 changed | 0 changed | 13 changed: 6 by credential-named flags (`--key` ×4 samples, `--etherscan-key`, `--token-secret`), 6 by flags that are not (`--meta_key`, `--space-key`, `--assignment-key`, `--page-token`, `--from-token`/`--to-token`, gpg's `--full-generate-key`), 1 unclear (`--resource-key`) |
| Corpus, rule B | 0 changed | 0 changed | **5 changed, all by credential-named flags** (`--key` ×4 samples, `--token-secret` ×1); every value is a placeholder |
| `~/.claude` (177 artifacts: 28 MCP, 29 hook, 2 permission), either rule | 0 of 177 changed | 0 of 782 changed | 0 of 280 changed |

Every changed payload is a skill's `injection` excerpt, i.e. a line of markdown or script text; no MCP `args`, hook
command or permission entry in either population changes, which is why no hash moves. **Re-keying, measured: 0 of 18
reputation entries** (all are plugin and Claude Desktop skill tree hashes, which redaction never touches), **0 of
4,704 corpus artifact hashes, 0 of 177 `~/.claude` hashes, and 0 stored approvals** (this machine has no
`~/.claude/.aguard-approvals.json`). Outside these populations, an MCP server, hook or permission entry that holds a
newly covered flag with a value re-keys exactly once, and the gate asks about it once more: per
`.claude/rules/hash.md`, a change to `redactCredentials` re-keys those three kinds, which costs one more question and
never a silent pass.

## Initial direction

Measure first: over the corpus and a real `~/.claude`, which flags actually precede a value in MCP `args`, hook
commands and permission entries, which of those values are secret-shaped, and how often each candidate flag precedes a
port, a path or a URL instead. Then widen the one pattern set in `internal/redact` that `flagSecretRE`, `redact.Argv`
and the content hash all read — no second flag list — by the flags the measurement supports, and count what re-keys.

## Done criteria

Rule B with the exemption (questions 1–4) is the design; implementation starts on top of P-039 and re-measures every
number below there before recording it.

- [x] `TestFlagSecrets` (`internal/redact/flags_test.go`, table-driven): for each of the 28 spellings rule B adds
  (`widenedFlags`: `--key`, `--private-key`, `--secret-key`, `--access-key`, `--auth-key`, `--app-key`, `--license-key`,
  `--master-key`, `--encryption-key`, `--signing-key`, `--client-key`, `--db-pass`, `--db-password`, `--db-passwd`,
  `--db-pwd`, `--pwd`, `--admin_password`, `--user_pass`, `--keychain-password`, `--certificate-passphrase`,
  `--client-secret`, `--client_secret`, `--token-secret`, `--webhook-secret`, `--pat`, `--bearer`, `--credential`,
  `--credentials`), in lower and upper case, a made-up value of 8, 11, 16 and 20 characters with no known prefix is gone
  from `Secrets("tool "+flag+" "+v)`, `Secrets("tool "+flag+"="+v)` and `Argv([srv, flag, v, --verbose])`, the flag
  survives, and each output is a fixed point of `Secrets`. **Red on the base** for every flag: in all three readings for the flags the base never redacts
  (`--key`, `--private-key`, `--secret-key`, `--db-pass`, `--pat`, `--pwd`, …), and in the space and argv readings
  under 12 characters for those `looseAssignRE` already takes from 12 (`--db-password`, `--client-secret`, `--bearer`)
- [x] **Reverse assertion, unit level** (`TestFlagSecrets_Reverse`): the string comes out unchanged from `Secrets`
  (the `=` reading skipped only where `assignRE` takes it alone, before and after alike: `--page-token=…`), the pair
  unchanged from `Argv`, and a widened flag reads no value across a line break (question 7), after every flag the
  measurement found to carry a non-credential — `--key-id`,
  `--secret-id`, `--secret_arn`,
  `--key-file`, `--secret-file`, `--password-file`, `--vault-password-file`, `--password-stdin`, `--token-endpoint`,
  `--token-type`, `--keyring`, `--keychain`, `--keyword`, `--meta_key`, `--space-key`, `--assignment-key`,
  `--page-token`, `--from-token`, `--no-password`, `--author`, `--bypass`, `--multipass` — and after a newly covered
  flag when the value is a path or URL (`--private-key ./id.pem`, `--private-key ~/.ssh/id_rsa`, `--key /obj/path`,
  `--credentials https://…`); and the short flags `-p 8080`, `mkdir -p dir`, `-p<attached>`, `-P x`, `-k x`, `-t x`
  keep their values
- [x] **The eight named flags are byte-identical** (`TestFlagSecrets_NamedFlagsUnchanged`): `TestArgv`'s P-036 rows pass unchanged except its two reverse rows
  that pinned this gap (`--db-password hunter2` and `run --key <v>`), which move to the positive half;
  `--password ./x`, `--token https://…` are still redacted (the exemption applies only to what rule B adds)
- [x] **Content hash** (`internal/detect`): two MCP entries that differ only in the value after `--key` (or `--db-pass`)
  hash the same — red on the base, where they differ — and two that differ only in the path after `--private-key`
  hash differently (the exemption keeps a swapped key file visible to an approval). `TestContentHashGolden` unchanged
- [x] **Judge payload** (`internal/judge`): a `.mcp.json` with one server per newly covered flag sends none of the
  values in its `mcp-config` payload and writes each pair on the flag's line (`args=--key <REDACTED>`), a fixed point
  of `detect.Redact`; red on the base
- [x] **Reverse assertion, corpus level** (scratch replay, not committed): against the binary P-039 leaves on `main`,
  exactly the payloads this proposal measured change (5 of 8,770 here, every one by `--key` or `--token-secret`);
  every other payload, every finding and every artifact hash is byte-identical
- [x] **Re-key count recorded**: reputation entries and `~/.claude` hashes affected (0 of 18 and 0 of 177 here); the
  `ExcerptVersion` bump pinned with a golden fixture that now carries a `--key` pair
  (`TestExcerptVersion_IsPinnedWithItsGolden`); `rulesEpoch` unchanged, since no finding changes
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency

## Out of scope

- **Short flags** (`-p`, `-P`, `-k`, `-t`, the attached `-p<v>`): not added (question 2). A rule that knows the command
  (`mysql -p<pw>`, `sqlcmd -P`, `bcp -P`, `sshpass -p`) needs a context `Secrets` does not have; it is a follow-up
- **`token` compounds** (`--auth-token`, `--api-token`, `--refresh-token`, `--session-token`): unchanged, still
  redacted from 12 characters by `looseAssignRE` (question 1)
- **`key` after an open-ended or two-word qualifier** (`--etherscan-key`, `--data-key`, `--resource-key`,
  `--api-private-key`, `--from-private-key`), and **compounds without a separator** (`--dbpass`, `--adminpassword`):
  not covered. The separator is what keeps `--bypass` and `--multipass` out
- **The eight named flags, `flagUserPassRE`, `assignRE`, `looseAssignRE`, `credKeys`, the prefix list and the entropy
  floor**: output unchanged for every string in which only they match; the exemption does not apply to the eight
- **Environment keys** (`MYSQL_PASS=…`: `credKeys` has no `pass`): not touched
- **The content hash code**: `internal/detect/contenthash.go`, `internal/collect/hash.go`, `internal/reputation` and
  `internal/gate` get zero diff. The hash reads `Credentials` and re-keys through it, as `redact.Argv` does; how it
  views the tail of an announced element is P-039's
- **Detection**: no rule, severity, score weight or `rulesEpoch` changes; `docs/rules.md` does not change
- `baselines/results/` does not change by a byte; no run is re-folded

## Must not claim

- Not "a credential after a flag is now redacted": only after the flags rule B names. A short flag, a `token`
  compound under 12 characters, a vendor-qualified key under 24 characters and a separator-less compound still go
  out as written — redaction stays best-effort (spec §16.3)
- Not that the measurement found or protected a secret: neither population holds one after any of these flags. The
  numbers show what each rule costs, not what it saves
- Not that a path is never a secret: after a flag rule B adds, a short value in the standard base64 alphabet that
  happens to start with `/`, or a password that ends in `.txt` or another key-file extension, is read as a path and
  kept (URL-safe base64, hex and alphanumeric keys never start with `/` or hold a `.`)
- Not that the change is free for hashes: an MCP server, hook or permission entry outside the measured populations
  that carries a newly covered pair re-keys once, and the gate asks about it again

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The tests: the newly covered spellings and their reverse rows in `internal/redact`, the content-hash pair, the judge payload (red on the base) | `redact: tests — a value after a credential flag the patterns do not know is not redacted (P-040)` |
| 2 | Widen `flagSecretRE` by rule B with the exemption (`internal/redact/flags.go`); the two P-036 reverse rows that pinned the gap move to the positive half; `ExcerptVersion` 3 → 4 with the golden fixture carrying a `--key` pair, in the same commit (`.claude/rules/judge.md` since P-037: any change under `internal/redact` changes the excerpt bytes) | `redact: a flag whose last word names a credential announces its value (P-040)` |
| 3 | The `docs/llm-judge` pair, spec §16.3, the invariant #3 note and the hash rules' list of what `Redact` does not recognise | `docs: which flags announce a credential (P-040)` |

## Open questions

1. **Which long flags are added?** Every word added redacts every value after it — in every report, every judge
   payload and every content hash — so a flag that only sometimes carries a credential costs evidence in the cases
   where it does not. Rule A (any flag whose last word is a credential noun) adds 66 occurrences on the measured
   text, 38 of them credential-named and 16 not, and changes 13 corpus payloads of which 6 are over-redactions. Rule B
   adds 37: 32 credential-named, 2 paths after `--private-key` (exempt, question 4) and 3 not credentials (two
   already redacted today, the third exempt); it changes 5 payloads, all credential-named. What B gives up, measured:
   6 credential-named occurrences (`--certificate-key` 2, `--api-private-key`, `--deepl-auth-key`, `--etherscan-key`,
   `--remote-token` 1 each) and the 9 unclear ones (`--data-key` 8, `--resource-key` 1), none with a key-like value.
   A vendor key is usually 24 characters or more and high-entropy, so the entropy floor takes it in reports and
   payloads, and a high-entropy digest cannot be reversed. **Recommendation**: rule B — the password family and
   `secret` as the last word after
   any qualifier; `key` bare or after one credential qualifier (`api app private secret access auth client license
   master encryption signing`); `--pat`, `--bearer`, `--credential`, `--credentials` bare; `token` compounds unchanged
   (1 credential flag against 9 occurrences that are not).
   **Decided (2026-10-10)**: as recommended
2. **Are `-p`-style short flags in?** The single-letter flags mean different things to different commands, and
   `Secrets` sees a string, not a command. Measured: `-p` precedes a value 2,136 times, 1,731 of them `mkdir -p <dir>`
   (no value at all), then `claude -p <prompt>`, `ps -p <pid>`, `ssh -p <port>`; 0 credentials. The attached `-p<v>`:
   256, all `find -path`/`-print`, `dotnet -p:` and the like; 0 key-like. `-P`: 9 of 23 are passwords (`sqlcmd`,
   `bcp`), the rest `grep -P`, `pgrep -P`, `jira -P`. `-k` 53 and `-t` 1,361, no credential. Adding `-p` would redact
   about 2,100 values here to protect none. **Recommendation**: no short flags. Name a command-aware rule (`mysql`,
   `sqlcmd`, `bcp`, `sshpass`) as a follow-up; it would need the command in view, which an MCP `args` array has in its
   sibling `command` and a hook line has in its first word. **Decided (2026-10-10)**: as recommended
3. **Are `*-file`, `*-path`, `*-endpoint`, `*-url` (and `*-id`, `*-stdin`, `*-type`, `*-name`, `*-arn`, `*-hash`)
   excluded?** **Recommendation**: yes, and by construction rather than by a list: a flag announces a value only when
   the credential word is its **last** word, so `--key-file`, `--password-file`, `--token-endpoint`, `--secret-id`
   never match and no suffix list has to be kept. Measured: 49 occurrences of 19 such flags, none with a credential
   value; `--secret-id` alone is 9 (8 of them paths). **Decided (2026-10-10)**: as recommended
4. **Should redaction after a newly covered flag depend on the value's shape?** This is the policy choice. Two
   directions are possible, and they fail differently:
   - *Redact only values that look like a key* (length, letters and digits, entropy): a password is whatever its
     owner typed — `hunter2` after `--db-pass` is the case this proposal exists for — so a positive shape test
     re-opens exactly the short-credential gap.
   - *Redact everything except what is plainly not a secret*: a URL, or a path (`/…`, `./…`, `../…`, `~/…`, or a
     key-file extension). The cost is the narrow false negative named in "Must not claim". The gain is twofold. The
     reader and the judge still see `--private-key ~/.ssh/id_rsa` (the two `--private-key` occurrences measured are
     both paths); a skill pointing a tool at a key file is evidence, not a secret. And the content hash keeps the
     path, so an approval re-asks when the key file is swapped; a forgotten path would let it survive the swap. In the
     hash an over-redaction is not cosmetic: what is replaced can change without re-keying (`.claude/rules/hash.md`).

   **Recommendation**: the second — exempt URLs, paths and `--no-*` flags (`--no-password <positional>`), redact
   every other value at any length; no length or entropy test. Apply it only to what rule B adds: the eight named
   flags keep redacting whatever follows, byte for byte as today, because their name is the credential word and
   P-036 pinned their behaviour. **Decided (2026-10-10)**: as recommended
5. **Where does the flag list live?** **Recommendation**: in `flagSecretRE` alone (one regex, its name group telling
   the eight named flags from the added ones); `redact.Argv` asks `Credentials` whether a flag announces the next
   element and the content hash reads `Credentials`, so both inherit the change with no list of their own. Nothing in
   `detect`, `judge` or `collect` names a flag (invariant #3). **Decided (2026-10-10)**: as recommended
6. **Versions and landing order?** What the judge's passes send changes for text with such a pair, so
   `ExcerptVersion` goes up by one (from whatever P-037, P-038 and P-039 leave on `main`); no finding changes, so
   `rulesEpoch` stays. P-039 changes how the hash views an announced element's tail, and this proposal changes which
   elements are announced: both re-key the same three kinds. **Recommendation**: implement after P-039 merges, on top
   of it, re-measure the corpus replay and the re-key counts there, and ship both in one release so users re-approve
   once. **Decided (2026-10-10)**: as recommended

Asked during implementation (stage 3):

7. **Does a widened flag read its value across a line break?** The patterns' separator is `[=\s]+`, so a newline counts.
   The judge's MCP excerpt writes an element no flag announces on its own line, and since P-037 every field ends with a
   whole-field `Redact`: `args=--private-key` / `args=~/.ssh/id_ed25519` was read as `--private-key` followed by the
   value `args=~/.ssh/id_ed25519`, which is not a path (it starts with `args=`), and the kept line became `<REDACTED>`
   — `TestPlan_MCPWidenedFlagSecretsAreRedacted` caught it. In a script or a markdown file the next line is
   likewise the next command, not the flag's argument. **Recommendation**: a second-tier flag announces nothing when
   the separator holds a line break; the eight named flags keep reading across it, as P-036 pinned
   (`tool --password\nhunter2` is still redacted). **Decided (2026-10-10)**: as recommended
8. **Is the third work item (`ExcerptVersion`) its own commit?** Since P-037, `.claude/rules/judge.md` requires the bump
   in the same commit as any change under `internal/redact`. **Recommendation**: fold it into W2; the docs become W3.
   **Decided (2026-10-10)**: as recommended

## Done

```
Merged: PR #59 (2026-10-10; find the sha with git log --grep P-040)
Released: pending release
Evidence: red on the base with W1 alone (commit "redact: tests — …", on 2112aab): TestFlagSecrets fails for every one
  of the 28 widened spellings; TestContentHash_WidenedFlagValuesAreNotDigestInputs fails its 5 "same" cases (`--key`,
  `--db-pass`, a 7-character `--client-secret`, `--private-key=<v>`, a hook's `--db-password`: the value reached the
  digest input and moved the hash); TestPlan_MCPWidenedFlagSecretsAreRedacted sends the value for 9 of 9 flags. All
  three green after W2
Evidence: reverse — TestFlagSecrets_Reverse, TestFlagSecrets_NamedFlagsUnchanged, the "differ" half of the
  content-hash test (paths after `--private-key`, a URL after `--credentials`, `--key-file`, `--token-endpoint`, `-p`)
  and the key-file half of the judge test green on the base and on the branch; TestContentHashGolden unchanged and
  green. The two P-036 TestArgv reverse rows that pinned this gap (`--db-password hunter2`, `run --key <v>`) failed
  on W2 as predicted in the design and moved to the positive half
Evidence: ExcerptVersion — with the fix alone the golden digest stayed 870e91b426266cd3 (the fixture held no widened
  pair); with a `--key` pair added it is d689185d4bdf37de without the fix and b94da7ba8cb9c053 with it, pinned as
  {4, b94da7ba8cb9c053}
Evidence: corpus replay on 2112aab (3,539 samples placed by baselines/adapter/aguard.Stage, `scan --json` and
  `llm preview --json`, scratch, not committed): 5 of 8,770 judge payloads change, all skill `injection` excerpts
  (`--key` in 4 samples, `--token-secret` in 1, every value a placeholder); 0 of 4,704 artifact hashes, 0 of 1,618
  findings and 0 of 164 scan notes change. Same 5 as the design's measurement on 6cab205
Evidence: ~/.claude on both binaries (`scan --json` and `llm preview --json`, `--inbox off`): 177 artifacts (28 MCP,
  29 hook, 2 permission), 0 hashes, 0 of 782 findings and 0 of 280 payloads change, overall 69 on both;
  `scan --root ~/.claude --quiet` prints nothing on either. Re-key: 0 of 18 reputation entries (plugin and Claude
  Desktop tree hashes), no stored approvals on this machine
Evidence: not done — `git diff --stat origin/main -- internal/detect/contenthash.go internal/collect
  internal/reputation internal/gate internal/detect/rules_version.go docs/rules.md baselines go.mod go.sum` is empty
Verify: make verify → "verify: all gates passed" (go1.23.5, no toolchain switch; go.mod unchanged, `go 1.23.5`)
```

Follow-ups left out on purpose:

- A command-aware rule for short flags (`mysql -p<pw>`, `sqlcmd -P`, `bcp -P`, `sshpass -p`): `Secrets` sees a string,
  and the command is in view only to its callers (an MCP entry's `command`, a hook line's first word)
- `key` after an open or two-word qualifier (`--etherscan-key`, `--data-key`, `--api-private-key`) and compounds without
  a separator (`--dbpass`): not covered; a vendor key of 24 characters or more is taken by the entropy floor
- `MYSQL_PASS=…`: `credKeys` has no `pass`; a pattern change that re-keys env blocks, its own proposal
