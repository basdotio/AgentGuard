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
| of these, inside an MCP `args` element | 3 (key and value in one element, each a `"$(…)"`: kept, question 3) | 0 |

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
| Corpus | 0 of 4,704 changed | 1 of 1,618 (an `EXFIL-001` snippet: `"password": "[PLACEHOLDER]"` inside a `curl -d` JSON body) | 12 of 8,770, in 12 skill samples: 11 `injection` excerpts and the `triage` payload quoting that `EXFIL-001` snippet |
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

## Done criteria

The quoted value form of questions 1–6 and the per-replacement guard of question 7 are the design; implementation
starts on top of P-040 (merged, 78678a8) and re-measures every number above there before recording it. "An announcer" below means a flag `flagSecretRE` names
(the eight, plus whatever P-040 adds), `-u` / `--user` with a `user:` part, or a credential key followed by `=` or `:`.

- [ ] `TestQuotedValues` (`internal/redact`, table-driven): after each announcer — `--password`, `--token`,
  `--api-key`, one flag P-040 adds (`--key`), `-u admin:`, `--user=admin:`, `API_TOKEN=`, `"password": `,
  `password: `, `export DB_PASSWORD=` — the made-up values `"correct horse"`, `'correct horse'`, `"hunter2xyz"`,
  `"P@ss!w0rd"`, `"ab\"cd ef"` (an escaped quote) and an unterminated `"correct horse` at the end of a line are gone
  from `Secrets` and `Credentials`; the announcer, the quotes and the user survive (`"<REDACTED>"`,
  `"admin:<REDACTED>"`, unterminated `"<REDACTED>`), a carrier word stays (`"Authorization": "Bearer correct horse"` →
  `"Bearer <REDACTED>"`), and every output is a fixed point of `Secrets`. **Red on the base** for every row: the value
  whole after a flag or `-u`, its tail after a key
- [ ] **Reverse assertion, unit level** (same test): `Secrets` returns the base's output byte for byte for expansions
  (`--password "$PW"`, `"${PW}"`, `"$(op read x)"`, `` "`cat f`" ``, `'Bearer $token'`, `-u "$U:$P"`,
  `"Authorization": "Bearer $TOKEN"`), for quotes that close a string (`"--password=" + pw`, `print("token:", t)`,
  `'Bearer ' + token`, `date -u '+%Y-%m-%dT%H:%M:%SZ'`), for bodies whose first or last character is a space or
  invisible (`" lead"`, `"trail "`, a body starting with U+200B), for short and literal key values (`password: ""`,
  `token: "yes"`, `password: "pw1"`), for a whitespace-only key (`token "abcdefghijklmnop"`), for a flag no pattern names
  (`--author "A B"`) and for every string without a quote; a quoted value stops at a newline and the next line comes
  out unchanged. `TestArgv` and the bare rows of `TestAnnounced` pass unchanged
- [ ] **`redact.Announced` agrees** (`TestAnnounced`): the P-039 reverse row `{"--password", "\"quoted value\""}`
  moves to the positive half as `{…, 1, true}` — the element's opening quote is the kept head, everything after it is
  forgotten — and `Argv` still equals `Announced` plus `Secrets` of the head for every row
- [ ] **Content hash golden** (`TestContentHashGolden`, `internal/detect`): a new case, a settings file with the hook
  `mytool --password "correct horse" ; true` and the permission entry `Bash(mytool --token 'correct horse' *)`: both
  canonical inputs pinned with `\"<REDACTED>\"` / `'<REDACTED>'` and both digests as literals computed by hand
  (`printf 'aguard:hook:v1\0%s' … | shasum -a 256`). **Red on the base**: the inputs hold `correct horse`. The six
  existing constants (inputs and digests) do not change by a byte
- [ ] **Content hash rotation** (same package): two hooks, and two permission lists, whose quoted secret differs
  anywhere hash the same as the one written `<REDACTED>` — red on the base; the unquoted control hashes as on the base
- [ ] **The guard is per replacement** (`TestContentHash_GuardRefusesOneReplacement`, `internal/detect`): in
  `mytool --password 'P@ss#1' --token hunter2` and `Bash(curl -u admin:* --token hunter2)` the span holding a structure
  character stays as written and `hunter2` is not in the digest input; two such entries differing only in the
  `--token` value hash the same. **Red on the base** for the grant (the base refuses the whole string, `hunter2`
  included). **Reverse**: the span itself is still never forgotten — `--password 'P@ss#1'` against `'P@ss#2'`, `-u
  admin:*` against `-u admin:hunter2`, and every pair of `TestContentHash_ReplacementNeverTakesStructure` still re-key;
  green on the base and the branch
- [ ] **Judge payload** (`internal/judge`): a `.claude` fixture with one hook per quoted form sends no byte of any value
  in its `injection`, `capability` and `triage` payloads, each a fixed point of `detect.Redact`; red on the base (27 of
  31 payloads here)
- [ ] **Static snippet** (`internal/detect`): the `HOOK-001` / `EXEC-001` snippets of those hooks and the `PERM-002` /
  `PERM-006` snippets of `Bash(python3 * --password "V")` / `Bash(git -c core.pager=x --token 'V' *)` carry
  `"<REDACTED>"`; red on the base
- [ ] `ExcerptVersion` 4 → 5 (P-040 left 4), in the same commit as the redact change, pinned with the golden fixture now carrying a quoted pair
  (`TestExcerptVersion_IsPinnedWithItsGolden`); `rulesEpoch` unchanged
- [ ] **Reverse assertion, corpus level** (scratch replay, not committed): against the binary P-040 leaves on `main`,
  exactly the changes measured here re-measured there (0 of 4,704 hashes, 1 of 1,618 findings, 12 of 8,770 payloads on
  2112aab), each one listed and explained; every other hash, finding and payload byte-identical; `~/.claude` 0 of 184
  hashes, 0 of 813 findings, 0 of 302 payloads
- [ ] Re-key count recorded, for the guard and for the quoted form separately: reputation entries (0 of 18 here),
  corpus and `~/.claude` hashes, stored approvals
- [ ] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no new dependency; no toolchain switch

## Out of scope

- **Which flags announce a value** is P-040's: this proposal names no flag and adds none; the quoted form applies to
  whatever the flag-name group of `flagSecretRE` holds when it lands
- **`looseAssignRE`** (a key followed by whitespace only) gets no quoted form (question 1); token compounds P-040
  leaves to it (`--auth-token "…"`) stay as today when quoted
- **Unquoted values** keep the base's reading byte for byte, including the gaps it has: a key value with symbols
  (`PASSWORD=P@ss!w0rd`, where `assignRE`'s value class stops at `@`), a value starting with a symbol, a backslash
  escape (`a\ b`). Follow-up if measured
- **Expansions** (a quoted body holding `$` or a backtick), **quotes that close a string**, **bodies with a space or
  an invisible character at either end**, **multi-line quoted strings** past their first line, **shell words that
  start unquoted** (`abc"def ghi"`) and **JSON-escaped quotes in raw JSON text** (`\"…\"`; 10 occurrences
  measured, 9 of them an expansion, a placeholder, an empty string or after a flag no pattern names) keep the base's
  reading
- **The content hash definition**: the domains, the canonical JSON, `viewElement`'s whole-element rule and the structure
  sets stay; in `internal/detect/contenthash.go` only `guardedView` changes, from one decision per string to one per
  replacement (question 7). `internal/collect/hash.go`, `internal/reputation` and `internal/gate` get zero diff. The
  hash reads `Credentials` and inherits the quoted form, as `redact.Argv` and `redact.Announced` do
- **Env and header values held as a JSON object member** (`"env": {"DB_PASSWORD": "correct horse"}`): P-042's. The
  joined `KEY=VALUE` view the hash builds for them holds no quote; only a value that itself starts with a quote is
  read by the quoted form
- **Detection**: no rule, severity, score weight or `rulesEpoch` changes; `docs/rules.md` does not change. `PERM-001`
  does not fire on a quoted value (`inlineSecret` wants `[:=]` and twelve non-space bytes) — a detection gap, follow-up
- `internal/judge` changes only `ExcerptVersion` and its golden fixture; `baselines/results/` does not change by a byte

## Must not claim

- Not "a quoted secret is now redacted": only after an announcer, in `"…"` or `'…'` on one line, with no `$` or
  backtick in it, starting and ending with a visible character and not starting with `, ; ) ] } + .`; after a key at
  least four characters long and not a literal (`true`, `yes`, …). A whitespace-only key, a flag no pattern names, a
  multi-line or JSON-escaped string and a value with a `$` in it still go out as written — redaction stays best-effort
  (spec §16.3)
- Not that the content hash forgets every quoted secret: the structure guard refuses a replacement that takes a shell
  structure character, so a quoted password holding `#`, `*`, `?`, `&`, `;`, `|`, `<`, `>`, `(`, `)`, `[`, `]`, `{`,
  `}` or `\` enters the digest input as written, as on the base — but on its own now: the string's other secrets are
  replaced (question 7)
- Not that the measurement found or protected a secret: configuration in neither population holds a quoted value
  after an announcer, and the text changes are placeholders, templates and example values. The numbers show what the
  rule costs, not what it saves
- Not that `-u "…"` always holds a password: the quoted form reads `-u "https://…"` as `user:pass`, the over-redaction
  the bare form already makes

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The tests: the quoted rows and their reverse rows in `internal/redact`, the moved `TestAnnounced` row, the content-hash golden case and rotation pairs, the per-replacement guard rows, the snippet and judge-payload fixtures (red on the base), the structure-guard reverse rows (green on both) | `redact: tests — a quoted value after a credential flag or key is not redacted (P-043)` |
| 2 | The structure guard per replacement: `redact.CredentialsKeeping` offers each replacement to a predicate, `Credentials` is it with none, and `guardedView` refuses only the replacements that take structure | `detect: the content hash refuses a replacement that takes structure, not the whole string (P-043)` |
| 3 | The quoted value form in `internal/redact/redact.go`: one double-quoted and one single-quoted fragment and one predicate, used by the value groups of `flagSecretRE`, `flagUserPassRE` and `assignRE`, with a replacement that keeps the quotes and the carrier; `ExcerptVersion` 4 → 5 and the golden fixture carrying a quoted pair, in the same commit | `redact: a quoted value after a credential flag or key is forgotten whole (P-043)` |
| 4 | The `docs/llm-judge` pair, spec §16.3, the invariant #3 note and `.claude/rules/hash.md` (the quoted gap it lists as open is closed; the guard is per replacement) | `docs: a quoted value after a credential flag or key is redacted (P-043)` |

## Open questions

1. **Which patterns get a quoted form?** A quote after an announcer is an opening quote only when the announcer ends
   in something that takes a value: a flag with `=` or whitespace, `-u` before `user:`, a key before `=` or `:`. After
   a key followed by whitespace alone (`looseAssignRE`) the next quote is mostly not an opening one but the far
   side of a string being built — `'Bearer ' + token`, `"token " + t`: measured, the 122 quoted occurrences after a
   whitespace-only key are 44 code fragments, 44 placeholders, 5 expansions, 28 words, phrases, URLs and empty strings,
   and 1 key-shaped value.
   `urlCredRE` and the prefix list never meet a quote inside what they match. **Recommendation**: `flagSecretRE`,
   `flagUserPassRE` and `assignRE`; not `looseAssignRE`. **Decided (2026-10-10)**: as recommended
2. **Where does a quoted value end?** **Recommendation**: as POSIX `sh` reads it — a double-quoted value at the next
   `"` not escaped by a backslash, a single-quoted value at the next `'` (no escapes inside single quotes) — and never
   past a newline. An unterminated quote runs to the end of the line, not to the next space: a shell word does not end
   inside an open quote, and the line bound keeps one stray quote from swallowing the rest of a file the judge reads.
   Measured: no change in either population comes from an unterminated quote; the rule is there for the cut or
   multi-line value, on the side of forgetting. **Decided (2026-10-10)**: as recommended
3. **What about expansions inside the quotes?** `"$TOKEN"`, `"${API_KEY}"`, `"Bearer $TOKEN"`, `"$(op read …)"` are
   references, not secrets: the name is what a reader and the judge need to see (that a skill reads `$GITHUB_TOKEN`
   is the evidence), and the hash guard would refuse the replacement anyway — `$` and the backtick are shell structure —
   putting every other secret on that line back into the digest input (question 7). Measured: 107 such occurrences
   in the corpus and 55 in `~/.claude` (a bare `"$VAR"` or `"${VAR}"` 57 and 21, a command substitution 8 and 3, the
   rest a `$` or a backtick inside a longer value). The unquoted form already replaces
   `--password $PW`; that stays (no byte of a string without a quote changes). **Recommendation**: a quoted body
   holding `$` or a backtick, in either kind of quote (Dart and others interpolate inside single quotes too), is not a
   value and keeps the base's reading. The residual — a literal secret with a `$` in it — goes into Must not claim.
   **Decided (2026-10-10)**: as recommended
4. **What about a quote that closes a string, or a body with an invisible edge?** In code the quote after
   `token:` or `--password=` is often the end of a literal: `print("token:", t)`, `"--password=" + pw`,
   `date -u '+%Y-%m-%dT%H:%M:%SZ'` (the `-u` arm meets `date`'s UTC flag). Its body starts with a space or one of
   `, ; ) ] } + .`. Two `INJ-004` test lines in `~/.claude` quote a value made of zero-width characters, and the
   finding's evidence is exactly those characters. **Recommendation**: a body is a value only when its first and last
   characters are visible (graphic, not a space) and its first is not one of `, ; ) ] } + .`; otherwise the base's
   reading stays. Measured: these rules take out 11 code fragments and the 2 `INJ-004` lines, and no change that
   remains is code; a password that starts or ends with a space stays as today (Must not claim). **Decided (2026-10-10)**: as recommended
5. **What does the replacement look like?** **Recommendation**: keep the quotes and what announces the value, as the
   bare form keeps the flag: `--password "<REDACTED>"`, `API_TOKEN='<REDACTED>'`, `-u "admin:<REDACTED>"`, an
   unterminated `"<REDACTED>`, and a carrier word inside the quotes stays (`"Bearer <REDACTED>"`), as `assignRE`
   already keeps it outside them. The floors stay where they are, measured on the body: none after a flag, four
   characters and the literal list (`true`, `yes`, carrier words) after a key. The replacement leaves no quote
   unbalanced that was balanced, so the hash's guard sees the same skeleton and the output stays a fixed point of
   `Secrets`. **Decided (2026-10-10)**: as recommended
6. **How does it compose with P-040's flag list and with `redact.Announced`?** P-040 widens the flag-name group of
   `flagSecretRE` (rule B) and exempts, for the flags it adds, a value that is a URL or a path and a `--no-*` flag.
   This proposal changes the value group only. **Recommendation**: the quoted form is a property of the value, so it
   holds after every flag the name group names, the eight and rule B alike, with no flag list of its own; P-040's
   exemption is asked of the quoted body, the text between the quotes, so `--private-key "~/my keys/id.pem"` stays as
   visible as `--private-key ~/.ssh/id_rsa` does. `Announced` and `Argv` do not change: they ask `Credentials` about
   `flag + " " + element` and inherit the form, so an argv element that itself opens with a quote
   (`["--password", "\"quoted value\""]`) becomes announced from byte 1 — its opening quote is the kept head and the
   rest is forgotten whole, P-039's rule — and the excerpt and the hash agree on it as on every other element. Nothing
   in `detect`, `judge` or `collect` names a quote form (invariant #3). **Decided (2026-10-10)**: as recommended
7. **The structure guard: a quoted secret with `#` in it, on a line with another secret.** The hash refuses a
   replacement that takes a shell structure character and then keeps the whole string as written (`guardedView`). A
   quoted password with `#`, `*` or `&` in it is exactly what people quote, so for `mytool --password 'P@ss#1' --token
   hunter2` the base replaces `hunter2` and keeps `'P@ss#1'`, while this rule's replacement of `'P@ss#1'` is refused
   and takes `hunter2`'s replacement down with it: the one case where the digest input commits to more than on the
   base. A guard that refuses one replacement and keeps the others would close it, but it changes how the hash views
   every string the guard refuses today, re-keying those entries, and it lives in `contenthash.go`.
   Measured on a fixture: on the base two such hooks differing only in the `--token` value hash the same, on the
   scratch build they do not; two whose quoted password has no structure character hash the same on both.
   **Recommendation**: accept it here; measured, no hashed string in either population holds a quoted value at all, so
   0 strings are affected. Name it in Must not claim, pin with a reverse row that the guard still refuses a replacement
   taking `#`, and record the per-replacement guard as a follow-up with its own re-key count.
   **Decided (2026-10-10)**: as recommended. **Decided again (2026-10-11), by the maintainer: not as recommended —
   option A.** The regression is not accepted: in this proposal the guard becomes per replacement — a replacement whose
   span holds a structure character is refused on its own, and the other replacements in the string still apply — so
   `mytool --password 'P@ss#1' --token hunter2` keeps `'P@ss#1'` and replaces `hunter2`. Goldens for strings without
   such a span stay byte-identical; the re-key (every hashed string in which the base refused one replacement and
   another would have applied) is measured and stated (`.claude/rules/hash.md`)
8. **Does it hide text from the judge?** A flag or key now forgets everything up to the closing quote, so an author
   can put a sentence the judge will not see inside `--password "…"`. That is already true of a bare word, and of a
   whole argv element after `--password` since P-036; the static rules read the raw text, not the redacted one; the
   span ends at the line. Measured: one changed occurrence in the corpus is prose. **Recommendation**: accept, and
   state it in the `docs/llm-judge` pair next to the argv rule. **Decided (2026-10-10)**: as recommended; confirmed by
   the maintainer (2026-10-11): accept and document
9. **Versions and landing order?** What the judge's passes send changes for text with a quoted pair, so
   `ExcerptVersion` goes up by one from what P-040 leaves; a snippet is evidence formatting, the finding set does not
   change (0 of 1,618 and 0 of 813 findings added or removed), so `rulesEpoch` stays (`rules_version.go`: not for
   evidence formatting). **Recommendation**: implement after P-040 merges, on top of it, re-measure the corpus replay
   and the re-key counts there, and ship in the same release as P-039 and P-040 so a user whose hook or permission
   list holds such a value re-approves once. **Decided (2026-10-10)**: as recommended
