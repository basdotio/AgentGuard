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

## Initial direction

Measure first: over the corpus and a real `~/.claude`, which flags actually precede a value in MCP `args`, hook
commands and permission entries, which of those values are secret-shaped, and how often each candidate flag precedes a
port, a path or a URL instead. Then widen the one pattern set in `internal/redact` that `flagSecretRE`, `redact.Argv`
and the content hash all read — no second flag list — by the flags the measurement supports, and count what re-keys.
