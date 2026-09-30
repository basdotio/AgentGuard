<!-- SPDX-License-Identifier: MIT -->
# AgentGuard Roadmap

Status of what's shipped and what's planned. Ordered by the agreed priority.

What shipped, release by release, is in [CHANGELOG.md](CHANGELOG.md). This file keeps what is in
progress, what is recorded but not scheduled, and the limitations we state out loud.

## Now (in progress)

> Current scheduling — phases, milestones and gates — lives in
> the maintainers' internal plan. This file records what shipped, what is recorded but
> not scheduled, and the limitations we are willing to state out loud.

- [ ] **A.1 AST detection** (pure-Go, no CGO) — the LEXICAL half shipped (see below); what is
      still open is anything needing a syntax tree: a chained expression like
      `Buffer.from(secret).toString('base64')`, or following a value across statements. Both
      need a real parser per language, and in pure Go that is a large dependency (a JS/Python
      parser) or CGO (tree-sitter) — a call to make against "single static binary, two
      dependencies", not one to make silently while implementing something else.

> **C.8 and C.9 shipped** — see below. **B.7 (install-time gate) is closed as superseded, not
> done**: Claude Code has no install event to hook, and one would not be complete anyway — a
> skill also arrives by `git clone`, by `cp`, and by hand. The load-time gate (`aguard hook`,
> shipped) holds the boundary that actually exists; see "Load-time gate" above for the design
> record. What remains in this section is A.1.

> Rule expansion (was A.2) is not a milestone and has stopped being tracked as one — it is
> continuous work with no done state. The specific gaps that entry named are closed; the
> adversarial corpus under "Known limitations" is where open detection gaps live now, because
> a gap asserted by a failing-when-fixed test cannot be forgotten the way a checkbox can.

## Later (recorded, not scheduled)

- **C.10 Homebrew tap** — a `brew install` path on top of the raw-binary release, which now
  exists (see "Going public" above).
- **C.11 Windows support, tested rather than compiled** — `make dist` deliberately stops at
  darwin/linux. The Windows binary built cleanly from day one, which is the trap: CI runs
  ubuntu only, the codebase has no `GOOS` branch, and the symlink containment behind invariant
  #2 rests on primitives that differ there (creating a symlink needs a privilege the tests
  cannot assume, so the boundary cases would not even be exercised). Shipping a binary no test
  has run, for a tool whose verdicts people act on, trades a missing platform for a wrong
  verdict. Doing this properly = a CI matrix running the suite on windows-latest, then the
  target goes back into DIST_TARGETS. Source builds still work today.
- **D.11 cloud reputation DB (P1)** — the LOCAL half shipped (see above); what remains is the
  hosted side: a lookup by content hash against a shared known-malicious list. The placeholder
  `internal/sink` interface that once stood for it was removed (P-038): it had no callers, and an
  interface is cheapest to design next to its first implementation.
- **D.12 ERC-8004 tie-in (P2)** — publish the security score on-chain as a verifiable agent/skill
  attestation. This is what `overall` is kept byte-identical with and without `--llm` for.
  The determinism requirement is not a preference: repeated runs of a hosted model under nominally
  deterministic settings still vary in output text and task accuracy (arXiv 2408.04667), so a value a
  third party recomputes offline cannot contain a model's opinion.
- **D.13 Agent BOM (P2, recorded after surveying the auditable-AI literature)** — the security-auditing
  literature is converging on a "bill of materials" framing: a hierarchical graph separating STATIC
  capability bases (models, tools, long-term memory) from DYNAMIC runtime state, joined by semantic
  edges (see arXiv 2605.06812). `scan --json` already contains the whole static layer — skills, MCP
  servers, hooks, permissions, subagents, rules, memory ARE the capability bindings — it simply is not
  named or shaped that way. The reason to care is D.12: **a score is a judgement, a BOM is a fact**, and
  attesting to a fact on-chain is far more defensible than attesting to a number whose formula must
  then be frozen forever. Not scheduled, because the value depends on a consumer that does not exist
  yet; graph-shaping the output before anyone reads it would be building an interface for nobody.
- **D.14 compliance anchors (P2)** — EU AI Act Article 12 (record-keeping; the high-risk obligations
  took effect 2 Aug 2026), ISO/IEC 42001, NIST AI 600-1. The useful form is narrow and honest: a
  report can say which of its outputs SERVE a record-keeping duty. It must not say "compliant" —
  that is a claim about an organisation, not about a scan, and overreaching here would poison the
  disclosure discipline the rest of the tool is built on.

## Known limitations (honest)

- Static only: cannot prove malice, observe runtime behavior, decrypt obfuscated payload intent, inspect MCP endpoints or dependency internals, or catch zero-days.
- A plugin is scanned as one tree. Its bundled **hooks now ARE audited per (event, matcher, command)** through the same builder `settings.json` hooks use, so `HOOK-001`, the followed script and the judge's hook pass all apply and a finding names `hook:PreToolUse[Bash]#1 (plugin acme@mk)`. Its bundled **MCP servers are now MCP artifacts too** (`collectPluginMCP`, one per server, counted in the inventory — the Figma plugin's server used to leave `mcp=0` and a summary saying nothing was exposed). Its bundled **skills and commands are still only read as text** — spec §4's finer attribution for those remains open.
- **Claude Desktop's remote MCP connectors ARE now collected** (`collect/connectors.go`, 2026-09-08).
  The cached tool list from each connector attached to the account — every tool's name, description and
  parameter descriptions, read from `claude-code-sessions/*/*/local_*.json` (`remoteMcpServersConfig`),
  never by connecting — is one `KindConnector` artifact per connector, hashed over the tool list. Only
  the connector list is decoded out of a file that also holds session state; coverage is what this
  machine has SEEN in desktop sessions (a browser-only connector is not cached, and the report says so).
  Detection runs the dimension-1 rules (a description is prose about a tool, like a doc) plus four
  connector-only rules `MCP-001..004` for the tool-poisoning shapes, and the judge's injection pass.
  Remaining gaps: connectors used only on claude.ai are out of reach without connecting (which the tool
  will not do); the desktop layout is observed, not documented, so an app update can move it (handled as
  "nothing collected", never silence).
- **The Downloads scan is one-shot, not a watcher.** `scan` checks agent-shaped items under ~/Downloads
  (internal/inbox) at the moment it runs; nothing watches the folder, nothing quarantines, nothing
  notifies. Making it behave like antivirus needs a resident process (launchd on macOS) that runs the
  inbox check on new files and posts a notification, plus a quarantine step reusing `clean`'s reversible
  move. Deliberately deferred: a resident process changes the trust story of a tool that today runs only
  when asked and never phones home, macOS will prompt for Downloads access, and a false positive that
  pops a notification costs far more than one in a report the user asked for. Decide those three first.
  Only .zip archives are opened; .tar.gz and friends are disclosed as not read.
- Hook scripts are followed one level deep, and only within HOME — anything else is not read
  (`COV-000`) and scores as `HOOK-002`. HTTP-type hooks are first-class artifacts (`HOOK-003`).
- **A bare URL literal is a network leg, whatever it is for.** `EXFIL-001/003` count
  `const LOGO = 'https://…/logo.png'` the same as `fetch('https://…')`, because a URL string
  is how an unknown HTTP client names its destination and dropping it reopens exactly that
  false negative. Cost, measured on obra/superpowers 6.3.0: two files stay high on a brand-image
  constant that no request ever uses. The loopback screen cannot help (the host is real and
  external), so this is what the reputation allowlist is for — see `internal/reputation` and
  `hack/reputation-refresh`.
- **A URL held in a variable is an unknown destination.** `const url = \`http://localhost:${p}\`;
  http.get(url, …)` stays high: the screen reads host literals on the network line and nothing
  else. Resolving the identifier means following a value across statements, which the lexical
  layer deliberately does not do (see CLAUDE.md on `logical.go`) — every cheap approximation
  fails open somewhere (`url + '.evil.example'`, a reassignment in another scope). If this is
  ever done, it is done with a real syntax tree, not a regex over assignments.
- **A pinned build-tool subcommand is not a narrowing, and `PERM-006` does not say so yet.**
  `Bash(make test *)` is now reported, because `make` accepts `-f <any makefile>` after the
  target (measured; see `escapeMech.positionFree`). But the option lever is not the whole
  problem: `cd <any repo> && make test` reaches arbitrary execution with no lever at all,
  because the target's meaning is defined by a Makefile in whatever directory the agent
  happens to be in. Same for `npm test`, `npm start`, `yarn <script>` — both verified
  executing a payload from a project file. Under that reading, every open-ended grant over a
  build tool is `Bash(*)`, and `Bash(npm test *)` is an extremely common way to write one.
  **Deliberately NOT implemented**: it would put a medium on a large share of real
  allowlists, and an alert that gets dismissed is worth what no alert is worth — the same
  judgement that keeps the generated-directory disclosure to one merged note. The honest fix
  is probably guidance (write `Bash(make test)`, fully specified) plus a rule narrow enough
  to name the case, not a blanket widening of `PERM-006`.
- **Evasions confirmed by the adversarial corpus** (`cmd/aguard/adversarial_test.go`, each asserted inverted so closing one fails the test):
  - ~~The *contents* of a `GeneratedDir` are not scanned.~~ **CLOSED for the artifact's own build
    output.** One predicate was answering two questions, and they have different answers: "is this part
    of the artifact's IDENTITY" (hashing) and "should this be READ" (scanning). `collect/skip.go` now
    keeps two lists. The hash list is UNCHANGED and effectively frozen — it is the reputation
    database's key, so adding a directory breaks every stored hash; verified that editing `dist/` and
    `node_modules/` leaves an artifact's hash byte-identical while editing `SKILL.md` changes it. The
    scan list is smaller: `dist/`, `build/`, `out/` and `.next` ARE read now, because they are generated
    from the code in this same tree and a name-based skip there was the hiding place. Third-party trees
    (`node_modules/`, `vendor/`) and `.git/` stay unread on purpose — findings in a dependency describe
    somebody else's code, and git's own `hooks/*.sample` files were measured producing a high-severity
    finding about a sample. That residual exclusion keeps its inverted assertion, reworded from "gap"
    to what it is: a design decision, with `SUP-004` still catching an artifact that steers the agent
    INTO an unread tree.

  - ~~A file whose extension is not in `textExts` — an extensionless `bootstrap`, a dotfile like `.bashrc` — is not scanned.~~ **CLOSED.** The allowlist is a fast path now, not the decision: a file it does not cover is read when a content sniff says it is text. This was the cheapest evasion in the corpus — it required no technique, only the omission of a suffix. Binaries are still unread and still silent (a PNG is not an instruction).
  - ~~Rules match one line at a time, so a shell line continuation splits the pattern in half.~~ **CLOSED** by the lexical pass (`internal/detect/logical.go`), which folds continuations — and interior quoting, so `cu""rl` is `curl` — before any rule runs, while keeping a raw view so evidence still quotes what the file says at the line the command starts on.
  - ~~Rules are literal regexes over raw bytes: a zero-width space or a confusable inside a command name defeats them.~~ **CLOSED** by the same pass: invisible code points (including the bidi controls behind "Trojan Source") are stripped and homoglyphs mapped to ASCII through a small hand-written confusables table. The concealment is reported in its own right — `INJ-004` for invisible characters, `OBF-005` for a token mixing ASCII with Cyrillic/Greek — so an evasion attempt surfaces even when what it was hiding has no rule.
- **Project scope walks UP to an ancestor `.claude`** — measured: with cwd `<x>/trashlab/proj`,
  Claude Code resolved project skills to `<x>/.claude/skills`. So `--root <project>/.claude`
  can report clean while the session loads skills from an ancestor directory the scan never saw.
  Not handled; closing it means resolving the same ancestor chain Claude Code does.
- **A running session sees cleanup happen** — measured: Claude Code watches `skills/`, `commands/`
  and `agents/` for changes. The O_EXCL lock protects two `aguard` runs from each other, not
  `aguard` from a live agent mid-conversation.
- **`skills/synced/` is sync-owned** — measured: it is repopulated automatically from archives.
  Quarantining anything under it would be undone by the next sync, so cleanup there should be
  refused rather than attempted. Currently neither refused nor attempted (it is not a zombie
  candidate today).
- A **root** scan reads only the known sub-layouts, so files a root collector does not own — a stray script at the top of `~/.claude`, or a directory under `skills/` with no `SKILL.md` — are still **not scanned**. The **silence** is closed: unowned top-level entries now produce one aggregated `COV-000` naming them (`internal/collect/unowned.go`), so "0 findings" no longer means both "audited, fine" and "never opened". Runtime state (caches, transcripts, `history.jsonl`) is excluded from the note by name, because a disclosure that fires on every environment is wallpaper. Actually SCANNING unowned entries remains open — they need an artifact identity and a place in the score, which is a larger change than making the gap visible.
- Large monorepo-as-one-skill (e.g. a large toolkit repo) is scanned as ONE artifact, so its findings all
  land on a single name and its score is one number for the whole tree. Concurrency (A.3)
  and the baseline (A.4) removed the speed and noise problems. Splitting it into sub-artifacts
  is now **deliberately not planned**: the environment score is an AVERAGE over artifacts, so
  splitting one dirty tree into N units lets the clean ones average the dirty one away —
  measured 86→97 on the same tree. That is inflation dressed as precision. What a real
  `~/.claude` showed the report actually lost was not *which* name but *how wide*: 303 findings
  folded to 40 readable (artifact, rule) groups, yet one group of 56 findings spanning 28 files
  displayed three evidence lines, and `×56` reads identically whether the cause is one bad file
  or half a plugin. So groups now carry `Files` (distinct affected files, counting every evidence
  leg) and print `×56 in 28 files` plus a `… and 25 more file(s)` line — without that line the
  three shown paths read as the complete set. Both renderers share `Group.MoreFiles()`. See
  `issues/008`.
- **A plain FILE sitting directly under `skills/` is still dropped in silence.** The unresolvable
  cases are now disclosed (`unresolvedNote`), but `!fi.IsDir()` still `continue`s without a word.
  Whether it is a real gap needs the same "does the agent have a load path in" analysis the rest of
  `unowned.go` rests on — and `.DS_Store` shows the answer is not simply "report it": a disclosure
  that fires on every macOS machine is wallpaper. Deliberately left open rather than guessed.
- Claude Code only (multi-platform not yet abstracted).
- **The load-time gate covers skills only.** `PreToolUse[Skill]` is the sole interception point
  Claude Code offers for something an agent pulls into context on purpose. A plugin's hooks and
  MCP servers, and `CLAUDE.md`, are live from the first turn of a session and are never
  "loaded" through a tool call — the `SessionStart` audit reports them but cannot stop them.
- **Only the GATE's own dead registration is reported, not any hook's.** `GATE-001` fires when
  settings.json names an aguard binary that is gone. A hook of someone else's that names a
  missing local executable is still silent, because `detect.scriptRefs` only follows tokens
  with a script extension (`.sh`, `.py`, …) — an extensionless binary is neither read nor
  announced. Generalising it means deciding which first tokens are local paths and which are
  PATH lookups (`jq`, `npx`, `echo`), and a wrong guess turns every ordinary hook into a
  warning. Worth doing; not worth guessing at.
- **The gate re-resolves a skill NAME to a path**, because the hook payload carries no path.
  Project root, then user root, then installed plugins. A name it cannot place — or one two
  plugins both provide — produces `GATE-000` (loaded without an audit), never a clean verdict.
  Closing this properly needs a resolved path in the hook payload, which is not ours to add.
