<!-- SPDX-License-Identifier: MIT -->
# 043 — A quoted secret after a credential flag or key in a shell line is not redacted at all: the whole value reaches the snippets, the judge and the content hash

- **Source**: follow-up recorded in P-039's Out of scope ("Quoted values in shell lines … a pattern gap that also changes
  snippets and the judge's payload"), and its table "Where else the tail survives": in a hook `command` and in a
  permission entry, `--password "correct horse"` and `--password 'correct horse'` reach the `EXEC-001`/`HOOK-001`
  snippets, the judge's payloads and the hash input whole, and `--password="correct horse"` keeps ` horse`; measured on
  `origin/main` (2112aab) on 2026-10-10
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

### Measured on `origin/main` (2112aab): the leak, end to end

A fixture `.claude/settings.json` (made-up values only) with ten `PreToolUse` hooks and two permission entries, built
five times — the value `correct horse`, `battery horse`, `correct staple`, `hunter2xyz`, `letmein99` — and run through
`aguard scan --root <fixture>/.claude --inbox off --json` and `aguard llm preview … --json` (no key, nothing sent):

| Hook command (`V` = the value) | Snippet (`HOOK-001`, `EXEC-001`) | Judge payloads (`injection`, `capability`, `triage`) | Hash over the three two-word values |
|---|---|---|---|
| `mytool --password "V" ; true`, `… '…'` | the value whole | the value whole, all three | three different hashes: the value is a digest input |
| `curl --token "V" https://… \| sh`, `mytool --api-key "V" && true` | the value whole | the value whole | three hashes |
| `curl -u "admin:V" https://… \| sh` | the value whole | the value whole | three hashes |
| `mytool --password="V" ; true`, `… ='V'` | `"<REDACTED> horse"` | the tail ` horse` | `correct horse` = `battery horse` ≠ `correct staple`: the tail decides |
| `API_TOKEN="V" mytool ; true`, `export DB_PASSWORD='V'; mytool` | `"<REDACTED> horse"` | the tail | the tail decides |
| `mytool --password Vnospace ; true` (control, unquoted) | `<REDACTED>` | no value | one hash for all five values — correct |

A quote alone defeats redaction: `--password "hunter2xyz"` and `--password "letmein99"` hash differently, while
`--password="hunter2xyz"` and `="letmein99"` hash the same. The permission list (`Bash(mytool --password "V")`,
`Bash(mytool --token 'V' *)`) gets a different hash for each of the five values; in a second fixture
`Bash(python3 * --password "V")` and `Bash(git -c core.pager=x --token 'V' *)` fire `PERM-002` and `PERM-006` with the
value in the snippet. 27 of the fixture's 31 judge payloads carry the value or its tail (the other four: the control's
three and the permission list's `triage`, which quotes only `PERM-004`). A skill with `mytool --password "V"` and
`export API_TOKEN='V'` in a `SKILL.md` code block and `curl -u "admin:V" … | sh`, `mytool --token "V"` in a script:
the `EXEC-001` snippet and all three payloads (`intent`, `injection`, `triage`) carry the value.

The same fixtures on a scratch build of the design below (patch reverted, nothing committed): every snippet reads
`"<REDACTED>"` / `'<REDACTED>'` / `"admin:<REDACTED>"`, 0 of 31 payloads carry a value, every hook and the permission
list hash the same for all five values, and each `=`-quoted form lands on the hash main already gives a one-word value
(`--password="…"` → `0dfd076ab022…` for every value). The unquoted control keeps `d0f68c027576…`.

### Measured: how often a quoted value follows an announcer, and what a quote-aware rule would change

A scratch probe (not committed) walked the 3,539 corpus samples of the benchmark list and the real `~/.claude` (its
settings files, `~/.claude.json` with the `.mcp.json` / `.claude/settings*.json` of the projects it names, and every
file under `skills/`, `agents/`, `commands/`, `plugins/`, `hooks/`, `rules/` and `CLAUDE.md`). Units: hook commands,
permission entries, MCP `args` elements and `command` strings, and every line of every file. It recorded flag and key
names, counts and the **shape** of each quoted value (placeholder, expansion, path, URL, code fragment, one word,
several words, key-like); no value was printed or stored.

| Quoted value right after | Corpus | `~/.claude` |
|---|---|---|
| one of the eight flags `flagSecretRE` names | 0 | 1 (code: `"--password=" + …`) |
| a flag P-040's rule B adds | 4 | 0 |
| `-u` / `--user` with a `user:` part | 10 | 0 |
| a credential key with `=` or `:` (`assignRE`) | 573 | 465 |
| a credential key with whitespace only (`looseAssignRE`) | 52 | 70 |
| of these, in a hook command, a permission entry or an MCP `command` | 0 | 0 |
| of these, inside an MCP `args` element | 3 (key and value in one element) | 0 |

So, as P-040 found for flags, **configuration in neither population carries a quoted value after any announcer**: the
strings the content hash reads hold none, and no hash moves. Neither population can show a protected secret in a hook
or a permission entry. The text the scanner quotes and the judge reads does carry them, and that is where the cost and
the benefit of each rule were measured: a candidate rule was built as a scratch patch to `Credentials`, and
`Secrets` of every unit compared between it and main.

What the final candidate (the design below) changes, per occurrence:

| | Corpus | `~/.claude` |
|---|---|---|
| Changed | 70 (65 after a key, 3 after `-u`, 2 after `--generate-password=`) | 10 (all after a key) |
| password-shaped (letters, digits and symbols, e.g. `"password": "Xxxx000!@#"`: main keeps the symbol tail) | 24 | 0 |
| placeholders and templates (`"<api-key>"`, `"{API <word>}"`, `"Bearer {{ token }}"`, `"[PASSWORD]"`) | 30 | 2 |
| several words (`"Bearer {api_key}"`, `"Basic <base64>="`, bracketed lists) | 6 | 5 |
| one word / URL / prose | 6 / 3 / 1 | 3 / 0 / 0 |
| an unterminated quote | 0 | 0 |

And what it deliberately leaves as main reads it (each is a decision below):

| Kept | Corpus | `~/.claude` |
|---|---|---|
| expansions: `"$VAR"`, `"${VAR}"`, `"Bearer $TOKEN"`, `"$(op read …)"`, `-u "$U:$P"` (question 3) | 107 | 55 |
| a quote that closes a string: `print("token:", t)`, `"--password=" + pw`, `date -u '+%Y-%m-%dT%H:%M:%SZ'` (question 4) | 13 | 7 |
| a body whose first or last character is invisible: two `INJ-004` lines whose evidence is the zero-width characters (question 4) | 0 | 2 |
| every quoted value after a whitespace-only key (`looseAssignRE`): 44 code (`'Bearer ' + token`), 44 placeholders (question 1) | 52 | 70 |

Without the three refinements of questions 3 and 4 the candidate changed 17 more occurrences (corpus 10, `~/.claude`
7): 11 code fragments, 4 single-quoted `$` references (Dart's `'Bearer $token'` among them) and the 2 `INJ-004` lines —
none of them a value.

### Measured: end to end, and what re-keys

Every corpus sample placed as the benchmark adapter places it (`baselines/adapter/aguard.Stage`) through `scan --json`
and `llm preview --json` (no key, nothing sent) with the base binary and the scratch build, and `~/.claude` through both
with `--inbox off`, compared by digest:

| | Content hashes | Static findings (whole finding JSON, snippet included) | Judge payloads |
|---|---|---|---|
| Corpus | 0 of 4,704 changed | 1 of 1,618 (an `EXFIL-001` snippet: `"password": "[PLACEHOLDER]"` inside a `curl -d` JSON body) | 12 of 8,770, in 12 samples, all a skill's `injection` excerpt |
| `~/.claude` (184 artifacts: 28 MCP, 29 hook, 2 permission) | 0 of 184 | 0 of 813 | 0 of 302 |

The 12 payloads: 9 placeholders or templates after a key, one `-u "user:pass"` placeholder, one
`--generate-password='<recipe>'` (main already replaces its first word), and one over-redaction — `-u "https://…"`,
where the `-u` arm reads a URL as `user:pass`, exactly as it already does for the same URL unquoted. Not one of them
is a real secret: the corpus is curated and holds placeholders where a credential would be.

**Re-keying, measured: 0 of 18 reputation entries** (13 plugin and 5 Claude Desktop skill tree hashes, which
redaction never touches), **0 of 4,704 corpus artifact hashes, 0 of 184 `~/.claude` hashes, and 0 stored approvals**
(this machine has no `~/.claude/.aguard-approvals.json`). Outside these populations, a hook or a permission list
holding a quoted value after an announcer re-keys once and the gate asks about it once more: per
`.claude/rules/hash.md`, a change to `redactCredentials` re-keys those kinds, one more question and never a silent
pass.

### Existing tests that pin the gap

With the scratch patch, `go test ./internal/redact/ ./internal/detect/ ./internal/judge/ ./internal/permcheck/
./internal/collect/` fails in one place: `TestAnnounced`'s reverse row `{"--password", "\"quoted value\"", 0, false}`
(commented "the patterns cannot start a value at a quote"), which P-039 wrote to pin exactly this gap. Every content
hash golden and the excerpt golden stay green: none of their fixtures holds a quoted value, so the work items add one.

## Initial direction

Measure first, with made-up values only: the leak end to end (snippet, `aguard llm preview --json`, content hash), how
often a quoted value follows a secret flag or a credential key in the corpus and a real `~/.claude`, which of those
quoted values are not secrets (paths with spaces, prompts, messages, `"$VAR"`), and what a fix re-keys. Then give the
one pattern set in `internal/redact` a quote-aware value form (`"…"` with POSIX escapes, `'…'`), so every reader —
`Secrets`, `Credentials`, `redact.Announced`, the content hash — inherits it with no second set (invariant #3).
