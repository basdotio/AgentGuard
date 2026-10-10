<!-- SPDX-License-Identifier: MIT -->
# 043 — A quoted secret after a credential flag or key in a shell line is not redacted at all: the whole value reaches the snippets, the judge and the content hash

- **Source**: follow-up recorded in P-039's Out of scope ("Quoted values in shell lines … a pattern gap that also changes
  snippets and the judge's payload"), and its table "Where else the tail survives": in a hook `command` and in a
  permission entry, `--password "correct horse"` and `--password 'correct horse'` reach the `EXEC-001`/`HOOK-001`
  snippets, the judge's payloads and the hash input whole, and `--password="correct horse"` keeps ` horse`; to be
  measured on `origin/main` (2112aab)
- **Depends on**: P-040 (`p/040-secret-flag-coverage`), which widens the set of flags `flagSecretRE` names; this
  proposal changes the value that follows them, and lands after it
- **Branch**: `p/043-quoted-shell-secret-values`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Redaction knows a credential on a command line by what announces it: a flag (`flagSecretRE`, `flagUserPassRE`) or a
key (`assignRE`, `looseAssignRE`). Every value form those patterns know is a bare shell word — `[^\s'"]+` after a
flag, `[A-Za-z0-9/+_.\-]+` after a key. A shell user who quotes the value, which is what anyone does for a password
with a space or a shell character in it, writes a form none of them knows:

- `--password "correct horse"` and `--password 'correct horse'`: the opening quote is outside the value class, so
  `flagSecretRE` matches nothing and **the whole value stays**.
- `--password="correct horse"`, `API_TOKEN="correct horse"`: `assignRE` takes the separator's optional quote and then
  the first bare word, so the value becomes `<REDACTED> horse` — **the tail stays**.

Those strings are shell lines the scanner quotes and the judge reads: a hook's `command`, the pattern inside a
permission entry (`Bash(…)`), a script line, a line of skill text. Where the value surfaces:

- **Static snippets** (`scan` text, `--json`, `--html`, `--md`, SARIF): a finding that quotes the line (`EXEC-001`,
  `HOOK-001`, …) prints the value.
- **The judge** (`--llm`): the passes that quote a hook command or a script line send it to the configured model
  vendor — what invariant #3 and spec §16.3 exist to prevent.
- **Content hashes** of hooks and permission lists: the value is a digest input, so the hash printed in the JSON report
  and stored with every gate approval commits to the password (the W-006 rule in `.claude/rules/hash.md`: no hash may
  be a digest of credential material), and the identity follows the secret: rotating it re-asks, two machines with
  different passwords never share an identity.

## Initial direction

Measure first, with made-up values only: the leak end to end (snippet, `aguard llm preview --json`, content hash), how
often a quoted value follows a secret flag or a credential key in the corpus and a real `~/.claude`, which of those
quoted values are not secrets (paths with spaces, prompts, messages, `"$VAR"`), and what a fix re-keys. Then give the
one pattern set in `internal/redact` a quote-aware value form (`"…"` with POSIX escapes, `'…'`), so every reader —
`Secrets`, `Credentials`, `redact.Announced`, the content hash — inherits it with no second set (invariant #3).
