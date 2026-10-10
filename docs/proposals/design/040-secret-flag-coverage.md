<!-- SPDX-License-Identifier: MIT -->
# 040 — A value after a credential flag the patterns do not know (`--key`, `--private-key`, `--db-pass`, `-p`) reaches the judge in the clear and enters the content hash

- **Source**: follow-up recorded by P-036 ("Flags the patterns do not know … still send their value and enter the
  hash"), and the hash rules' own admission (`.claude/rules/hash.md`: "`--db-password …`, `-p<pw>` … go into the digest
  input as written")
- **Depends on**: P-039 (`p/039-hash-secret-value-tail`), which changes how the content hash views an argv value; this
  proposal widens which flags announce one, and lands after it
- **Branch**: `p/040-secret-flag-coverage`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

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
secret**;
what they can show is the cost of each candidate, so the measurement was widened to the text the scanner also quotes
and the judge also sends: every line of every non-JSON file in the corpus samples (23,311 pairs) and of `~/.claude`'s
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
directory as one; then `claude -p <prompt>` 100, `ps -p <pid>` 22, `ssh -p <port>` 21. The attached form `-p<v>` occurs 256 times, all `find -path` /
`-print`, `dotnet -p:<prop>` and the like; 0 key-like. `-P` (23) is a password to `sqlcmd` and `bcp` (9, placeholders)
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
