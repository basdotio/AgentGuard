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

## Initial direction

One argv-aware helper in `internal/redact`, the single redaction implementation: `redact.Argv([]string) []string`
redacts every element, and views an element that follows a flag in the context of that flag, through the existing
`flagSecretRE` / `flagUserPassRE` (no second flag list). `mcpExcerpt` applies it to each run of lines that come from
one array before its per-line redaction. Bumps `ExcerptVersion` (what the MCP pass sends changes for such configs).
The content hash, static snippets and `internal/collect` are not touched.
