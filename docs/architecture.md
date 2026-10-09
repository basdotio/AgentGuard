# AgentGuard architecture (as built)

**English** | [中文](architecture.zh-CN.md)

Orientation for someone joining this codebase. It describes **what the code does today**, not
what it should do — that distinction decides which document wins when they disagree:

| Document | Authority | Lives in |
|---|---|---|
| [`docs/spec/spec.zh-CN.md`](spec/spec.zh-CN.md) | **Normative.** What must be true. Code comments cite it as `spec §N`. | here |
| [`README.md`](../README.md) | User-facing: install, commands, what gets scanned, capability boundary. | here |
| [`ROADMAP.md`](../ROADMAP.md) | What shipped, what is deferred, and the **honest known limitations** (including confirmed evasions). | here |
| [`rules.md`](rules.md) | **Generated** from the engine's rule set (`make docs`, CI-verified): every rule ID, its dimension, severity and why it fires. | here |
| **this file** | Descriptive: the as-built map. Update it in the same PR that changes the structure it describes. | here |

Scale, for calibration: about 20k lines of non-test Go across 16 `internal` packages plus the
CLI, about 20k lines of tests (596 test functions plus 2 fuzz targets), three direct dependencies
(`spf13/cobra`, `gopkg.in/yaml.v3`, `golang.org/x/term`), one static binary, no CGO.

## The one-paragraph mental model

An AI coding agent auto-loads skills, MCP servers, hooks, subagents, slash commands and
instruction files. Each of those is *instructions plus granted tool permissions* — not
documentation — and any of them can carry a directive the agent will follow. AgentGuard reads
those artifacts on disk, matches them against rules, and produces a reproducible risk score
plus a report. It never executes what it scans and never makes a network call (unless you
explicitly turn on the optional LLM judge). It is a body-check, not a guarantee of removal:
the score is a relative risk signal, not a safety certificate.

## Pipeline

[`cmd/aguard/main.go`](../cmd/aguard/main.go)'s `analyze()` is the single orchestration point
for `scan`, `check` and `clean`. **The stage order is load-bearing**, and the function's
comments say why:

```
collect  → detect → permcheck → reputation → ignore/baseline → judge(opt) → hygiene → score → report
(§4)       (§5.1)   (§7)         (D11)        (.aguardignore)   (§5.2)       (§6)      (§5.3)  (§9)
```

Four of those orderings are decisions, not sequence:

- **reputation before baseline** — squelch known-trusted tools' own noise first, so the
  baseline file only has to cover what is genuinely left over.
- **judge after suppression** — a known-good artifact can still earn an advisory annotation.
- **suppression before scoring** — a baseline changes the number, which is why every
  suppression must leave a note carrying the *highest* severity it silenced.
- **`check` enters through layout routing, not through `scan`** —
  `collect.CollectTarget` decides most-specific-first: single file → `SKILL.md` → plugin
  manifest → looks-like-a-root → any other directory (read as one whole tree).

Each package is an approximately pure stage over the immutable types in
[`internal/model`](../internal/model/model.go).

## Package map

| Package | Responsibility | Lines |
|---|---|---|
| [`collect`](../internal/collect/) | Enumerate and read artifacts; canonical hashing; symlink containment; claim the root's "unowned half" | 3014 |
| [`detect`](../internal/detect/) | The rule engine: the line-oriented regex rules (count and list in [`rules.md`](rules.md)), structural and shape checks, file roles, logical-line normalisation, comment awareness, redaction | 3237 |
| [`judge`](../internal/judge/) | Optional LLM passes, nonce barrier, evidence grounding, k-of-n consensus | 1980 |
| [`report`](../internal/report/) | Terminal / JSON / SARIF / self-contained HTML renderers; control-character and bidi sanitising (one classifier shared with `detect`) | 2032 |
| [`hygiene`](../internal/hygiene/) | Junk analysis: duplicates, context bloat, stale refs, zombie skills | 1054 |
| [`permcheck`](../internal/permcheck/) | Permission allowlist audit, including the escapable-binary table | 262 |
| [`model`](../internal/model/) | Immutable result types shared by every stage | 613 |
| [`config`](../internal/config/) | Config file loading, presets, key-file and endpoint rules (LLM judge) | 452 |
| [`score`](../internal/score/) | Deterministic scoring + the one-way LLM escalation | 142 |
| [`ignore`](../internal/ignore/) | `.aguardignore` baseline suppression | 184 |
| [`clean`](../internal/clean/) | Reversible quarantine of zombie skills (`--apply` / `--undo`) — audit manual: [`clean-internals.md`](internals/clean-internals.md) | 2005 |
| [`parse`](../internal/parse/) | SKILL.md front-matter / settings parsing | 111 |
| [`reputation`](../internal/reputation/) | Embedded, versioned allowlist/blocklist keyed by canonical hash | 120 |
| [`gate`](../internal/gate/) | The load-time gate: hook event dispatch, verdicts, hash-keyed approvals, `settings.json` merge-install with two backup slots | 1646 |
| [`inbox`](../internal/inbox/) | The Downloads scan: candidate discovery and bounded, traversal-refusing zip extraction | 376 |
| [`safeio`](../internal/safeio/) | The one way a file the tool did not write is opened: non-regular files refused before Open, size capped by the read | 101 |
| [`cmd/aguard`](../cmd/aguard/) | CLI wiring, `analyze()`, the Downloads pass, the gate runner, `llm setup/test/status` | — |

Two responsibilities are split in a way worth knowing up front, because both halves say
"permission":

- **`permcheck` looks only at the *textual shape* of an allow entry** — it never opens a file.
- **`detect.permissionUnits` follows the local script an allow entry names.** How dangerous
  `Bash(./scripts/deploy.sh *)` is depends on what `deploy.sh` does; that is "read the file and
  run rules", not a semantic judgement, so it is static, not AI.

## The invariants

These are why anyone should trust the output. Several are enforced in **more than one place**,
so changing one means changing all of them together. Full text and rationale live in the
container's `.claude/rules/invariants.md` (moved out of `CLAUDE.md` 2026-09-16) and in each package's doc comment; the short list:

1. **Never execute scanned content, never make a network call** (except the explicitly enabled
   LLM judge). Read-only throughout.
2. **Symlink boundaries converge and fail closed.** `collect.withinDir` and `detect.inBoundary`
   both resolve links *before* deciding. A skill's internal files may not point outside the
   skill root; a skill directory that is itself a symlink is a legitimate install mechanism but
   must resolve inside `$HOME`. Resolution failure is always a refusal.
3. **Redaction has exactly one exit.** `detect.Redact` is the only path that produces a
   snippet, and everywhere it runs **before** truncation — otherwise a secret straddling the
   byte limit leaks as a fragment. Report and LLM judge consume the same redacted view.
4. **Two scores: `Overall` is purely deterministic; `OverallEffective` carries one-way LLM
   escalation.** The one-way property is structural, not conventional:
   `OverallEffective = min(Overall, escalated)`. A successful prompt injection can therefore
   only make the attacker's own artifact look worse.
5. **No omission is silent.** Every coverage gap or suppression emits a dimension-0 note that
   never scores: `IO-000` `COV-000` `PARSE-000` `SCOPE-001` `IGN-000` `REP-GOOD` `LLM-000`
   `LLM-002`. Suppression notes must carry the **highest severity they silenced**.
6. **Concurrency must not change output.** `detect.Engine.Run` runs per artifact in parallel
   but writes results back by index, so finding order matches serial execution exactly.
### Two terminal modes

The default terminal report prints every finding and folds the dimension-0 coverage notes
into a single line carrying their count, their highest severity and their rule IDs;
`--verbose` prints those notes in full. The split exists because the notes' prose is long by
necessity (a note has to explain what was *not* read and why) and on a real machine it
outweighs the findings it sits under — which is how a reader learns to skip the end of the
report.

**Findings are never folded, in either mode.** Only dimension-0 meta is. A default view that
under-reports is the failure this split could easily become, and a reader cannot ask for
detail on a finding they were never shown. Invariant 5 survives the collapse because the
severity travels with the count: a baseline that suppressed a critical does not read as a
low-severity footnote. The notes live in two places — the scan's own, and those a collector attaches
to an artifact (a config file that did not parse becomes an artifact carrying `PARSE-000`) — and
the three human renderers read both through one function, `report.notesOf`; reading only the
scan's own once rendered a broken `settings.json` as "looks safe". When something Claude Code
loads was not fully read (a note attached to an artifact, an `IO-000` / `PARSE-000` anywhere, a
scan-level `COV-000` — where detect and collect file what they did not read — or an artifact carrying
`SUP-004`) the summary says coverage is incomplete instead of "looks safe". Four scan-level notes
disclose a deliberate skip and leave it alone — the top-level entries no collector owns, an empty root,
third-party / VCS trees, a hook script read as part of its plugin — matched through the producers'
exported title constants because nothing else in the data tells them from a gap; the judge's notes
(the privacy notice among them) stay in "Not checked" only too. The Checked line counts the
inventory, and the scanned artifacts when the inventory counts nothing (a single file or a plain
directory under `check`, a root holding only `CLAUDE.md`). `--json`, `--html` and `--md` are unaffected by the flag, so a CI job's output
never depends on which mode a human chose. `--md` (P-008) is the third human-read renderer, for PR
comments and issues: same derived data and order, and every string off the scanned tree in a code span.

7. **The terminal renderer strips control characters** (`report.sanitize`) — findings contain
   attacker-influenced filenames and, with `--llm`, model-generated text. JSON and HTML get
   escaping from their encoders.

The recurring failure mode these exist to prevent has one shape: **a scan that read nothing
rendering as a clean bill of health.** `check <typo>`, a bare directory routed to `CollectAll`,
a root's unowned half, a mistyped `--root` — each printed `100/100` and exit `0` at some point
in this project's history. When you touch collection, ask what an empty result renders as.

## Scoring

```
per artifact = clamp(100 − Σ_dimensions max(severity penalty), 0, 100)   # critical 40 · high 25 · medium 12 · low 5
overall      = mean of artifact scores, then leaky-bucket caps: any critical → ≤49 · any high → ≤69
band         = ≥85 Low · ≥70 Watch · ≥50 Elevated · <50 High
```

Within one dimension only the highest hit counts; penalties add across dimensions. That
property is used deliberately when placing a rule — see `OBF-004`, which sits in dimension 6
rather than 3 so its penalty adds instead of being absorbed.

**Reproducibility is a hard requirement** (the score is intended to feed an on-chain
attestation later), so nothing non-deterministic may enter it. Ten dimensions: 1 injection ·
2 excessive permissions · 3 exfiltration · 4 code execution · 5 supply chain · 6 obfuscation ·
7 backdoor (advisory) · 8 resource abuse (advisory) · 9 filesystem · 10 intent mismatch (LLM
only). **Dimension 0 = scan/coverage notes, never scored.**

## Detection engine essentials

Every rule's ID, dimension, severity and trigger is catalogued in [`rules.md`](rules.md),
generated from `builtinRules()` — add a rule, run `make docs`, or CI fails. Read
[`internal/detect/`](../internal/detect/) with these four ideas first:

- **`fileRole` decides which rules run at all**, and it is the primary false-positive control.
  A bundled `.md` (`roleDoc`) runs dimension-1 rules only; `SKILL.md`/`CLAUDE.md` and slash commands (`commands/*.md`, by kind)
  (`roleInstruction`) and scripts (`roleScript`) run everything; a hook command
  (`roleHookCmd`) additionally runs `.hookOnly()` rules — shell chaining is unremarkable in a
  script and telling in a hook. **Check this before adding a rule.**
- **Rules match the line an interpreter would run; evidence quotes the line the file
  contains.** [`logical.go, shape.go`](../internal/detect/logical.go) folds line continuations,
  zero-width characters and word-splitting quotes (`cu""rl`) into a normalised string for
  matching, while evidence keeps the raw bytes — printing `curl …` when the file says `cu""rl`
  would tell the user something false about their own machine. Rules run on raw first, then
  normalised, so normalisation can only add findings, never silence one (notably `INJ-004`,
  whose whole purpose is reporting zero-width characters).
- **Some checks are structural, not regex.** `EXFIL-001/002/003` and `OBF-004` accumulate
  three legs — credential read, encode, network egress — at two granularities. Two legs
  (credential + egress) complete a chain; encoding is an amplifier. Cross-file only reaches
  low + advisory, because unrelated files legitimately do each half. A same-file chain whose
  every network target is loopback drops to that same band, so a local sidecar cannot cap the
  environment at 69. `HOOK-002` (script outside HOME) and `HOOK-003` (HTTP hook) are also
  structural: they score a surface the line-oriented rules cannot see.
- **Advisory means "we cannot confirm this."** Dimensions 7 and 8 are advisory by
  construction, and the report must say *not confirmed*. A tool that distinguishes suspicion
  from certainty is one you can put in CI.

Skipped on purpose: non-text extensions, files over 1 MiB, `collect.ExcludeFromScan` names
(`.git`, `node_modules`, `dist`, …), and byte-identical duplicates within one skill tree. Every
one of those skips is disclosed — see invariant 5. `ExcludeFromHash` is excluded from **both** the
hash walk and the scan walk because the canonical hash is the reputation key and must survive a
rebuild; the compensating check is `SUP-004`, which fires when an artifact *points the agent
into* an excluded directory.

**The rule table has a version.** [`rules_version.go`](../internal/detect/rules_version.go) hashes
what decides an engine rule's finding — each `builtinRules()` entry's ID, dimension, severity,
flags and pattern source, in engine order, but not its title or explanation — plus an integer
`rulesEpoch`. The structural and permission checks and the notes that are not `LLM-` IDs are
built inline, so the epoch is all that covers them. Scan reports carry it as `rules_version`,
`aguard version` prints it, and the `rules.md` header shows it, so a pattern change cannot ship
without `make docs`. It covers **deterministic detection only** — what `overall` is
computed from. **When you change deterministic detection code outside `builtinRules()`** —
structural or shape checks, the role gate, the lexical layer, which files are read, `collect`'s
credential-import check (`EXFIL-005`), `permcheck` — in a way that changes which findings an input
produces or their ID/dimension/severity/advisory flag, **bump `rulesEpoch` in the same commit.**
Nothing enforces it; a forgotten bump lets two reports claim the same rules while different code
produced them. The LLM judge is outside it altogether — prompts, grounding, consensus and severity
clamping included: the judge moves only `overall_effective`, so a judge change never bumps the
epoch, and today a report identifies the judge's code only through `tool_version`.

## Canonical hashing

Skills and plugins get a tree hash (relative paths sorted, plus each file's sha256); a single
file gets its sha256. It is the reputation database key, so it must be stable across machines
and checkouts. **Changing the hashing logic invalidates every entry in
[`reputation.json`](../internal/reputation/data/reputation.json)**; regenerate with
`aguard hash`. `test/` is deliberately *not* excluded — payloads hide there.

Hooks, MCP servers and permission lists get a **content hash** instead
([`detect/contenthash.go`](../internal/detect/contenthash.go)), filled by `analyze()` right after
the rule engine runs and before reputation, the gate's `SessionStart`, `aguard approve` or the
Downloads pass read it. It is `sha256(<kind domain> 0x00 <canonical JSON>)` over the configuration
alone — the whole hook or server entry, no path, no artifact name — so one configuration on two
machines is one identity. A hook's input includes the content of the script it runs (or a marker
saying why that could not be read); secrets are replaced by `Redact`'s credential half before
hashing, but never a span that carries structure (shell syntax, a grant wildcard, a URL
delimiter). Changing only a secret does not re-key; changing that half of `Redact` re-keys all
three kinds. Artifacts whose config did
not parse keep the empty hash, which no approval or reputation entry can match.

## LLM judge (optional, off by default)

Requires **both** `llm.enabled: true` in config **and** `--llm`, which `scan` and `check` both
take; `clean` and the load-time gate never call it (the gate's scanners never set `llm`, pinned by
`TestGateScannerNeverEnablesLLM`). Per artifact kind it runs passes for hidden injection, intent mismatch, deobfuscation
(decode only, never execute), cross-file collusion, hook capability, MCP configuration, and
triage labelling. Scanned content is treated as **hostile**: every call fences it inside a
`crypto/rand` nonce barrier as inert data, and a nonce generation failure fails the call rather
than degrading to a guessable fence. One step before egress is outside `detect.Redact`'s reach
(`judge/egress.go`): as each excerpt is built, two home directories become `~` — the OS user's,
on every run, and the scanned environment's (`judge.Options.Home`, the absolute `--root`'s parent)
— each made absolute, with symlinks resolved, and in Claude Code's encoded project-directory form
(that last one not for a one-segment home such as `/root`: `-root` reads like a command-line option,
so it is sent as written while `/root/…` is still replaced). It runs
*after* redaction, so the entropy rule weighs each run as the report does, and so does the same
replacement on the static `File` fields in triage evidence. MCP configuration goes as `key=value`
lines (`detect.ConfigLines`, the same leaves the static pass reads): command, args, env, url and
headers first, each line capped at 500 bytes, a cut disclosed as `LLM-000`, and a value whose key
names a credential withheld. It stays out of `Redact` because that is the exit for every static
snippet.

Two properties matter more than the passes:

- **The iron law**: the judge may only *add* `Source=llm` findings and display-only advisory
  labels. It can never delete, downgrade or reorder a static finding, and `--fail-on` reads
  deterministic findings only — no model output can change that answer.
- **Escalation eligibility is per finding**: a finding must pass evidence grounding
  (`judge.Ground` back-fills a real `file:line`; ungroundable verdicts are discarded and
  counted as `LLM-005`) **and** k-of-n consensus across samples. `llm.authority` gates
  `--fail-on-llm`, not the number itself.
- **What a judge finding shows is what was sent**: its snippet is the excerpt line(s) the quote
  landed on (re-redacted, at most 512 bytes; longer lines are cut to a window around the quoted
  text, each cut end marked `…`, never from the line's start), never the model's quote; a quote of the excerpt's
  "N line(s) omitted" marker does not ground; the model's reason is capped at 512 bytes; triage
  labels go through the same sanitising as every other attacker-influenced string
  (`report.sanitizeResult`) and are kept only for a rule id that was sent for triage, byte for
  byte, so every renderer attaches them to the same finding.

Full reference: [`docs/llm-judge.md`](llm-judge.md).

## Current status

**Shipped and load-bearing:** static detection across 10 dimensions (rule counts live in the
header of [`rules.md`](rules.md), generated from the code; any number restated here would be a
stale copy), including the shape checks in `detect/shape.go` (padding, disguised archives,
shipped bytecode, registry redirects), permission audit with the escapable-binary table,
per-command hook artifacts that follow referenced scripts, plugin collection (both
`installed_plugins.json`, Claude Desktop's own plugin/skill store in `collect/desktop.go`, and
the sandbox's `synced/<uuid>/` layout), remote MCP connectors read from the desktop's cached
tool lists (`collect/connectors.go`, never by connecting) with the `MCP-001..004` poisoning
rules, sandbox detection that stamps a "this is not your computer" banner on a report produced
inside Claude Cloud / Cowork (`collect/environment.go`), the Downloads scan (`internal/inbox`:
agent-shaped items and zips under ~/Downloads, each checked alone, never in the score),
hygiene/clean (report-only; `--apply` quarantines zombie skills reversibly, `--undo` restores),
deterministic scoring, `.aguardignore` baselines, the embedded reputation allowlist, the LLM
judge with grounding + consensus over condensed head+tail excerpts, terminal/JSON/SARIF/HTML
reports, both gates, and distribution as release binaries, a Claude Code plugin, and the
`@bas.io/guard` npm package (five packages, no install scripts). Since v0.9.0: every read of a
user file goes through `safeio`; entries that exist but cannot be read are disclosed and change
the tree hash; a credential `@import` is refused and scored (`EXFIL-005`); `check` routes on
structure only; and the report names the worst artifact beside the mean.

**Deliberately not shipped yet** — each of these is a decision with a reason recorded in
[`ROADMAP.md`](../ROADMAP.md), not an oversight:

- **AST-based detection.** The lexical half shipped; a real syntax tree needs either CGO
  (tree-sitter) or a large pure-Go parser per language, against a "single static binary, two
  dependencies" constraint. So chained expressions like
  `Buffer.from(secret).toString('base64')` and cross-statement value tracking are still out of
  reach.
- **The reputation blocklist.** Mechanically wired and *empty* — curating known-bad hashes is
  ongoing. Today reputation's value is noise suppression on trusted toolkits; detection comes
  from the rules.
- **The cloud reputation lookup.** Nothing in the tree talks to a hosted list; the interface for
  it will be added together with the first implementation (ROADMAP D.11).
- **Windows binaries.** Cross-compiles cleanly, which was the trap: CI runs ubuntu only, there
  is no `GOOS` branch in the code, and the symlink containment behind invariant 2 rests on
  primitives that differ there. Source builds work; published artifacts are darwin/linux until
  a CI matrix runs the suite on Windows.

**Confirmed evasions** are asserted *inverted* in
[`cmd/aguard/adversarial_test.go`](../cmd/aguard/adversarial_test.go) — closing one makes a
test fail, so a gap cannot be forgotten the way a checklist item can. Currently one: the contents
of an `ExcludeFromScan` directory (a vendored or generated tree) are not scanned — its findings
would be about somebody else's dependency — while the artifact that directs the agent into it is
(`SUP-004`). Unknown extensions, unowned top-level files and homoglyph command names were once on
this list and are now caught; the ROADMAP's "Known limitations" keeps the longer story.

## Working on it

```bash
make build            # -> bin/aguard (CGO_ENABLED=0, version/commit/date via -ldflags)
make test             # go test -race -cover ./...
make lint             # golangci-lint
make dist             # release binaries + SHA256SUMS.txt (darwin/linux)
go test -run TestFailGate ./cmd/aguard/     # one test
./bin/aguard scan --root ~/.claude          # the real-machine smoke test
```

Conventions that reviewers will hold you to:

- `// SPDX-License-Identifier: MIT` heads every `.go` file; each package declares the
  invariants it owns in one file's package doc comment.
- **Code and all user-visible strings are English only.** Docs come in bilingual pairs
  (`README.md`/`README.zh-CN.md`, this file and its `.zh-CN.md`) — change one, change the other.
- Comments cite the spec (`spec §16.3`). Continue that: a non-obvious guard should say which
  requirement it implements.
- Tests are table-driven with `t.TempDir()` fixtures (there is no `testdata/`); the judge uses
  `httptest`. **The valuable tests are the invariant tests** — nothing executed, boundaries
  hold, secrets redacted, LLM findings don't move the score, `--fail-on` contract. Any change
  to a numbered invariant above needs a matching test.
- Releases: push a `v*` tag; [`release.yml`](../.github/workflows/release.yml) re-runs
  vet + tests, builds via `make dist`, verifies the stamped version matches the tag, and
  publishes the binaries with their checksums.

After a change to [`logical.go`](../internal/detect/logical.go) or anything in the reader path,
run `aguard scan` against a real `~/.claude` before opening the PR. That file is a hand-written
lexer eating attacker-controlled bytes; its first version panicked on a real machine (a line
ending in a single quote) while the table tests stayed green. Two fuzz targets guard it now.
