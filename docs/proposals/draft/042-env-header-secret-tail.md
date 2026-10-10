<!-- SPDX-License-Identifier: MIT -->
# 042 — An env or header secret that holds a space keeps its tail in the content hash: part of the secret is a digest input

- **Source**: follow-up recorded by P-039 ("Follow-ups left out on purpose" and its open question 6): an env or header
  value under a credential key is read as `KEY=VALUE` by the shell-line patterns, which stop at whitespace, so
  `"DB_PASSWORD": "correct horse"` hashes `<REDACTED> horse`; to be measured on `origin/main` (fc2b83f)
- **Depends on**: P-039 (`redact.Announced`); lands after P-040 (the flag set) and P-041, which change `internal/redact`
- **Branch**: `p/042-env-header-secret-tail`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The content hash of an MCP server and of the settings `env` block is printed in the JSON report and stored as the key
of every gate approval and reputation entry, so it replaces secrets before digesting (`.claude/rules/hash.md`: no hash
may be a digest of credential material). A string under an object key is viewed as `KEY=VALUE` (`redactTree`,
`internal/detect/contenthash.go`) through `redact.Credentials`, whose `assignRE` takes a value up to the first byte
outside `[A-Za-z0-9/+_.\-]`.

In a JSON object member the whole string is the value. For `"env": {"DB_PASSWORD": "correct horse"}` or
`"headers": {"Authorization": "Bearer abc def"}` the digest input keeps ` horse` / ` def`: a fragment of the secret is
committed to by a published hash, recoverable with a word list (P-039 showed this for argv values), and the identity
follows that fragment — rotating the head keeps an approval, rotating the tail re-asks.

## Initial direction

When a string is held under a credential key in a JSON object (env, headers), forget it from the first byte the patterns
replace to its end — the decision P-039 exported for argv (`redact.Announced`), asked for a key/value pair, under the
structure guard the hash already has; and the same wherever `KEY=VALUE` is rendered for snippets and excerpts, if the
measurement shows a tail there too. Measure the leak, the corpus and real-config frequency, false positives, and the
re-key first.
