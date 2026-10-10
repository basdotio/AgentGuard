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
   with different passwords that share a last word share one identity, and two with the same password written in
   another config differ only through the tail.

The judge's `mcp-config` excerpt of the same twelve servers (`aguard llm preview --json`) already carries
`args=--password <REDACTED>`, `args=--api-key <REDACTED>` and `args=-u admin:<REDACTED>`: after P-036, the hash is
the one place an argv tail survives.

## Initial direction

`redactTree` asks the same element-level question `redact.Argv` asks — through one exported helper in
`internal/redact` that `Argv` itself calls, not a copy — and replaces an announced element from its first announced
byte to its end, under the structure guard the hash already has. This re-keys every MCP entry that carries such a
value (and nothing else), so `TestContentHashGolden` gains a fixture with one and keeps its five constants.
