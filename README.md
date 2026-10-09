<!-- SPDX-License-Identifier: MIT -->
# AgentGuard (`aguard`)

**English** | [中文](README.zh-CN.md)

![go](https://img.shields.io/badge/go-1.23%2B-00ADD8)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

> Local, offline security scan + junk cleanup for AI agent environments. Gives Claude Code users an "antivirus for agents": **scan for malice, audit permissions, clean junk** — one command, nothing leaves the machine.
>
> `scan` and `check` are strictly read-only. `clean` is the only command that writes, and it only ever MOVES things inside your config root — it never deletes.

## What it is

AI coding agents **auto-load** a lot of external things — skills, MCP servers, hooks, subagents, instruction files (CLAUDE.md) — each handed your real tools and permissions, and any of them can hide a malicious directive. AgentGuard statically scans those artifacts on your machine and produces a risk report + cleanup suggestions.

**Positioning: a body-check + alerting + cleanup tool, not "guaranteed removal."** The score is a relative risk signal, not a safety certification. See "Capability boundary" below.

## Highlights

- **Local-first and offline**: never executes scanned content, no network, uploads nothing. Scanning is strictly read-only; the one command that writes (`clean`) moves and never deletes.
- **Single binary**: pure Go, `CGO_ENABLED=0` — `curl` it down and run.
- **10 detection dimensions**: injection / excessive permissions / data exfiltration / code execution / supply chain / obfuscation / backdoor / resource abuse / filesystem / intent mismatch (intent judging is an optional LLM enhancement, off by default).
- **Junk cleanup that never deletes**: duplicates, context bloat (quantifies reclaimable tokens), stale references, zombie skills. `clean` only ever MOVES things into a trash directory inside your config root, records every step, and `--undo` replays it. Deleting stays your call.
- **Permission audit**: inline secrets, wildcard arbitrary-command execution, over-broad paths, missing deny fallback.
- **Load-time gate**: registered as a Claude Code hook, it audits a skill **before an agent loads it** and asks you about anything that carries a finding — see [`docs/install-gate.md`](docs/install-gate.md).
- **Runs inside Claude Code**: a bundled plugin (three skills + `/aguard-setup`, `/aguard-scan`, `/aguard-vet`, `/aguard-gate`, `/aguard-help`, `/aguard-llm`) so you can ask for an audit in plain language and get the findings triaged, not just printed.
- **Privacy**: secret values are `<REDACTED>` before the report is built — raw credentials never reach the output.

## Install

**From npm** (macOS / Linux, no platform to pick). The binary ships inside the package, so
there is no install script and nothing is fetched at install time:

```bash
npx --yes @bas.io/guard@latest check ./some-skill   # vet one thing before installing it
npx --yes @bas.io/guard@latest scan --report        # audit this machine's whole setup
npm i -g @bas.io/guard                              # or install it: `aguard` on your PATH
```

`check` is the one to start with: it judges the artifact you point at, so its answer does not
depend on which machine is running it. `scan` is a statement about the machine it runs on.

> **Do not install the load-time gate from an `npx` run.** `aguard hook install` registers the
> absolute path of the binary it ran from, and npx's path is a cache npm later reclaims — after
> which every skill loads unaudited while the setup still looks protected (`GATE-001`). The
> command warns you when it notices. Use `npm i -g`, or a release binary, for the gate.

**Prebuilt binary** (macOS / Linux, no toolchain needed). Pick your platform from
`darwin-arm64` · `darwin-amd64` · `linux-amd64` · `linux-arm64`:

```bash
REPO=basdotio/AgentGuard
PLAT=darwin-arm64                                   # <- yours
curl -fsSLO "https://github.com/$REPO/releases/latest/download/aguard-$PLAT"
curl -fsSLO "https://github.com/$REPO/releases/latest/download/SHA256SUMS.txt"

# Verify before running it — this tool has no business asking for trust it won't demonstrate.
shasum -a 256 --ignore-missing -c SHA256SUMS.txt     # Linux: sha256sum
chmod +x "aguard-$PLAT" && sudo mv "aguard-$PLAT" /usr/local/bin/aguard
```

> **There is deliberately no `curl … | sh` installer.** This tool reports `curl` piped to a shell
> as `EXEC-001`, high severity, in anybody else's repository. Publishing one here would be asking you
> to do the exact thing it will warn you about, on the machine it is meant to protect. Download,
> verify, then run — three lines instead of one, and the only install path a security scanner can
> honestly recommend.

**From source** (Go 1.23+):

```bash
git clone https://github.com/basdotio/AgentGuard && cd AgentGuard
make build          # produces bin/aguard
# or directly:
CGO_ENABLED=0 go build -o bin/aguard ./cmd/aguard
```

Every release ships the four static binaries plus `SHA256SUMS.txt`, all built by
[`.github/workflows/release.yml`](.github/workflows/release.yml) from a version tag — the same
`make dist` a maintainer runs locally.

**Windows is build-from-source only, on purpose.** It cross-compiles fine; nothing has ever
run the test suite on it, there is no `GOOS` branch in the code, and the symlink boundary the
scanner relies on behaves differently there. A published binary no test has executed, for a
tool whose verdicts people act on, is worse than a missing platform.

## Use it from inside Claude Code (plugin)

The fastest way to get started. A bundled plugin ships three skills and six slash commands, so
you can ask in plain language ("is my `~/.claude` safe?", "check this skill before I install
it") instead of memorising flags:

In the **terminal** Claude Code, as slash commands:

```
/plugin marketplace add basdotio/AgentGuard
/plugin install aguard@AgentGuard
```

In the **VS Code / JetBrains extension**, `/plugin` is not available — run the same two
steps from a shell with the `claude` CLI instead:

```bash
claude plugin marketplace add basdotio/AgentGuard
claude plugin install aguard@AgentGuard
```
Third-party marketplaces do not auto-update by default in the terminal. Turn it on once so skill
and command updates arrive on their own — `/plugin` → **Marketplaces** → `AgentGuard` → **Enable
auto-update** (the desktop app's "Sync automatically" toggle is the same thing). The binary is
separate and never updates itself; `aguard version` says when it has fallen behind the plugin.

**Installed the plugin before v0.17.0?** It was called `agentguard` then, and that name receives no
further updates. It was renamed because it collides with the marketplace name `AgentGuard` in
Claude Code's install cache on a case-insensitive filesystem — macOS by default — where installing
it fails with `EINVAL` ([issues/022](issues/022-plugin-install-case-collision-macos.md)). Switch
once; `aguard version` prints the exact command for your install:

```bash
claude plugin install aguard@AgentGuard
claude plugin uninstall agentguard@AgentGuard
```


The repository is public and the plugin ships from `main`, so the plain form above is the one to
use — no `#…` suffix, no repo access, no git credentials.

Then restart Claude Code (the extension: reload the window) and run `/aguard-setup`. It
brings up the `aguard` binary if it is missing, runs the first scan, triages the findings
with you, and offers the load-time gate. The `curl` download needs no token: the releases it
reads are public. Building from source (`make build`, Go 1.23+) stays available. See
[`docs/rules.md`](docs/rules.md) for what a finding means once a scan runs.

| | |
|---|---|
| `/aguard-setup` | zero to protected: binary, first scan, fix plan, gate |
| `/aguard-scan [root]` | audit a config root and triage the findings |
| `/aguard-vet <path\|url>` | vet one skill/plugin/file before installing or committing it |
| `/aguard-gate [action]` | install / inspect / repair the load-time gate, manage approvals |
| `/aguard-help` | how to use it, in plain language: what you can ask, nothing to memorise |
| `/aguard-llm [status\|test\|setup]` | set up or check the optional AI deep check (a hosted model you provide) |

The three skills (`agentguard-audit`, `agentguard-vet`, `agentguard-gate`) also fire on their
own when a request matches, and they carry the part a `--help` cannot: how to read the two
scores, which findings are shapes rather than verdicts, why a dimension-0 note is not a risk,
and what a clean report still does not prove.

The plugin is the [`plugin/`](plugin/) subtree — skills and commands only, deliberately
excluding this repo's own docs. Those docs quote every attack pattern the engine detects, so
shipping them would make `aguard scan` report its own plugin as high-risk in every user's
environment. The subtree scans `100/100` at `--fail-on low`, and a change that breaks that is
a bug in the change.

Needs the `aguard` binary on `PATH` (the skills install it for you). The plugin adds no MCP
server and registers no hooks of its own — `aguard hook install` stays an explicit,
`--dry-run`-previewed step.

## Usage

```bash
aguard scan                      # full scan of the current user's ~/.claude
aguard scan --root ~/.claude     # specify the root
aguard scan --verbose            # also print the full text of every coverage note
aguard scan --html report.html   # also write a self-contained HTML report
aguard scan --md report.md       # also write the report as markdown, for a PR comment or an issue ("-" = stdout)
aguard scan --json               # machine-readable output
aguard scan --fail-on high       # CI/gate: exit 1 when a finding is ≥ high

aguard clean                     # cleanup: list junk + reclaimable tokens (read-only)
aguard clean --zombie            # also look for never-used skills (weak signal, opt-in)
aguard clean --zombie --apply    # MOVE them to <root>/.aguard-trash (never deletes)
aguard clean --undo last         # put a batch back, re-deriving every safety check
aguard clean --ask               # answer duplicate pairs one at a time (↑/↓ or 1/2)

aguard check ./some-skill        # pre-install gate: statically scan one skill/dir/file (defaults to --fail-on high)

aguard hook install              # load-time gate: audit each skill before an agent loads it
aguard approve ./some-skill      # trust those exact bytes, so the gate stops asking
aguard approvals                 # list what has been trusted

aguard version
```

The terminal report shows every finding by default and folds the coverage notes into one
line that still carries their count, their highest severity and their rule IDs. `--verbose`
prints those notes in full — reach for it when the question is what the scan did *not* read.
That includes a note attached to one item, such as a `settings.json` that does not parse: the
summary then names the item as not fully checked, and does the same for a file that could not be
read. When something Claude Code loads was not fully read — a file that could not be read or parsed,
an item carrying its own coverage note, a skill folder the scan could not open, a file over the size
cap, a hook script it could not follow — the summary says coverage is incomplete instead of calling
the setup safe. Notes about things the scan skips by design (your own session history at the top of
the config directory, a plugin's `node_modules/`) are listed but do not change that sentence. When
the inventory has nothing to count — `aguard check` on a single file or a plain folder — the summary
says what it checked ("Checked 1 file.") rather than that nothing was found. Coverage notes never change the score, and change the exit code in one case only: with `--fail-on-llm`, a judge that could not answer for every artifact exits `4` (below). Findings are never folded in either mode, and `--json` / `--html` / `--md` always carry everything, so
what a CI job sees never depends on which flag a human passed.

**Exit codes**: `0` below threshold · `1` a finding at/above `--fail-on` · `2` runtime error
(and, for `clean` only, `3` = acted partially, with every refusal named; with `--fail-on-llm` only, `4` = the gate could not be
evaluated — the judge did not run, or a call failed or was never made — with the reason on stderr; `4` is not a pass, and a
finding at either threshold still exits `1`). A run stopped by a
signal ends as `128 + signal` — `130` for Ctrl-C, `141` for a closed output pipe — through the
npm launcher exactly as for the bare binary.
`scan` does not exit non-zero on findings by default (informational mode); `check` defaults to `--fail-on high` (gate mode).

**Got a finding and want to know what it means?** [`docs/rules.md`](docs/rules.md) lists every
rule ID the tool can print — dimension, severity, and why it fires. It is generated from the
engine's own rule set and CI fails if it drifts, so the severity you read there is the severity
that will gate your build. Its header names the **rules version**, the same value a report carries
as `rules_version` (`--json`) and `aguard version` prints: when two reports differ and their rules
versions differ too, the rules changed between them.

## Cleanup (`clean`)

`clean` is the only command that writes, and the boundary is narrow enough to state in full:

- **It never deletes.** Everything it acts on is *moved* into `<root>/.aguard-trash/`, with the
  move recorded before it happens. `--undo` puts it back, re-deriving every safety decision from
  the filesystem rather than trusting the record. `rm -rf` is yours to run when you are satisfied.
- **Only `skills/`.** `settings.json`, `settings.local.json`, `.claude.json`, `.mcp.json` and
  anything under `hooks/` are never moved and never restored over — checked after resolving
  symlinks, because a decision made on a path as written is a decision a symlink can lie to.
- **Quarantined content still scores.** Moving something into the trash does not improve your
  score; only deleting it does, and that is a deliberate act you perform. Otherwise `clean --apply`
  would be the shortest path to turning a red `--fail-on high` green.
- **Nothing is chosen for you.** A duplicate pair asks which side survives and has no default:
  answer it with `--resolve <id> --keep <name>`, `--keep-both`, or walk them with `--ask`. A
  reversible action on a guess is still a guess.

`--zombie` ("installed but never used") is opt-in and never rated high confidence: it can only see
this machine's history, and it says how many sessions that was.

Full guide, including the safety boundary and what it deliberately does not do:
[`docs/clean-guide.zh-CN.md`](docs/clean-guide.zh-CN.md). Implementation and the invariant/test table:
[`docs/internals/clean-internals.md`](docs/internals/clean-internals.md).

## Load-time gate (`aguard hook`)

`check` answers when you remember to ask. The gate answers at the moment a skill is about to
be loaded, whether or not anyone remembered:

```bash
aguard hook install    # merges into ~/.claude/settings.json (backs it up), then restart Claude Code
```

Claude Code has no hook for INSTALLATION, and one would not be complete anyway — a skill also
arrives by `git clone`, by `cp`, and by hand. Loading is the boundary that can be held, and it
is the one that matters: a skill on disk is inert until an agent reads it into context.
Approvals are keyed by the artifact's **canonical hash**, so editing an approved skill re-opens
the question by itself. Full contract, including what it deliberately does *not* cover:
[`docs/install-gate.md`](docs/install-gate.md).

## What it scans

skills (SKILL.md + scripts + resources) · MCP config (statically: injection text and interpreter-preload `env` such as `NODE_OPTIONS`; an unpinned package or a credential written into `env` is reported only by the optional LLM judge, `LLM-009`) · **hooks** (settings.json — can silently run shell, a focus area) · **permission allowlists** · subagents · slash commands · installed **plugins** · CLAUDE.md · **Claude Desktop's own store** (plugins added in Customize → Plugins and the skills in Customize → Skills live under `~/Library/Application Support/Claude/`, not `~/.claude`, and are handed to the CLI as `--plugin-dir`; they are collected and marked "via Claude Desktop"). The default root honours `$CLAUDE_CONFIG_DIR`. **Downloads** are checked too: agent-shaped items under `~/Downloads` (a skill folder, a plugin, an MCP config, an instructions file, or a `.zip` holding one) are each checked on their own and listed in a separate Downloads section — they are not installed, so they never enter the score; everything else in the folder is counted, never read, never named. `--inbox <dir>` points elsewhere, `--inbox off` disables. `aguard check foo.zip` checks a downloaded archive directly. Install-symlink skills (e.g. `~/.agents/skills/*`) are resolved to their real target and audited; a symlink escaping to a system path is skipped with a warning.

Two surfaces get extra treatment, because both hand over execution while looking narrow:

- **Hooks** are audited per *command*, not per event: a finding names the exact
  `PreToolUse[Bash]#1` that carries it. A hook that just points at a script
  (`sh "$CLAUDE_PROJECT_DIR/.claude/hooks/pre.sh"`) has that script read and scanned too —
  otherwise a filename hides the payload. Referenced scripts are only read inside your home
  directory; anything outside is not read (`COV-000`) and scores as `HOOK-002` — a coverage
  gap and an unaudited silent-execution point are different facts. `PermissionRequest` raises
  that to high, because the hook takes the authorization decision. An HTTP hook (`type: http`)
  is a first-class artifact: posting events to loopback is `HOOK-003` low, posting
  `PermissionRequest` to loopback or posting anything off-box is high. A hook command that
  chains shell (`;` `&&` `||` `|` backticks `$(`) is `HOOK-001` — unremarkable in a script,
  telling in a hook, so that rule runs on hook commands only.
- **Permission allowlists** are checked against an escapable-binary list (`PERM-006`):
  `Bash(git *)` is arbitrary execution (`git -c core.pager=…`) wearing a narrow-looking
  disguise, and so are open-ended grants over `find` `awk` `tar` `docker` `ssh` `npm` `make`
  `xargs` `rsync` and friends. Fully specified grants (`Bash(git status)`) stay clean. A grant
  naming one of *your* scripts (`Bash(./scripts/deploy.sh *)`) has that script read and scanned
  too — the grant is only as narrow as what the script does. Same containment as hooks: inside
  your home directory only, anything else is a coverage note.

## LLM intent judge (optional, off by default)

Static rules can't catch "the description says A but the code does B," nor prompt injection
phrased to dodge keyword rules. With `--llm` an isolated model runs a set of passes chosen by
artifact kind:

- **hidden injection** — on skills, `CLAUDE.md`, subagents, slash commands *and* hooks, always
- **intent mismatch** — declared purpose vs. what the scripts actually do (skills)
- **deobfuscation** — decode embedded base64/hex blobs (decoded, never executed) and ask what
  the payload does
- **cross-file collusion** — do different files of one skill collect and send?
- **hook capability** — does the command do more than intercepting *its* event requires?
- **MCP configuration** — unpinned package, unknown publisher, remote endpoint, env credentials
- **triage** — label existing static findings likely-real vs. likely-benign noise

It is **source-agnostic** — every artifact is judged on its content, official or not. A flagged
verdict must **quote** what triggered it, and the quote is checked against the text that was
actually sent; anything unquotable is discarded and counted, so a confident hallucination
can't reach your report.

The judge is treated as **adversarial**: the scanned content is untrusted and reaches the
prompt, so a **nonce barrier** fences it as inert data and the model is told to ignore (and
report) any instruction inside it. Per the iron law, the LLM can only push risk **up** — it can
never remove or downgrade a static finding, move `overall`, or trip `--fail-on`. Triage
annotates; it never deletes.

**Setting it up** takes one command and no YAML — a hosted, OpenAI-compatible model you have an
account with (`deepseek`, `openai`, `qwen`, or any endpoint with `--base-url`/`--model`):

```bash
aguard llm setup --provider deepseek    # prompts for the key with hidden input; --key-stdin for pipes
aguard llm test          # one call: does the endpoint answer for that model?
aguard scan --llm        # the config is found at ~/.config/aguard/config.yaml from now on
```

The key goes to `~/.config/aguard/llm.key` (mode 0600, refused if anyone else can read it); an
`api_key_env` variable still works and takes precedence, for CI. Inside Claude Code, `/aguard-llm`
walks through the same three steps in conversation.

Hard constraints: **off by default** (needs both config and `--llm`), sends **only redacted** excerpts (raw secrets never leave the box), and the verdict is
**advisory** — `Source=llm` findings **never move `overall` and never trip `--fail-on`**, so
scoring stays reproducible.

A judge run does report a second number, `overall_effective`, next to the real one: the same
formula with its findings folded in. It is **always lower or equal**, not reproducible, and
gates nothing — it exists so you can see what the judge saw without that opinion leaking into
the number CI depends on. Since the environment score is averaged and bucket-capped, the
report also names the individual artifacts the judge moved, which is where the signal actually
shows.

```bash
# config.yaml
llm:
  enabled: true
  provider: openai_compatible
  base_url: http://localhost:11434/v1   # default: a local model (e.g. Ollama)
  api_key_env: AGUARD_LLM_KEY           # key read from this env var only, never disk
  model: llama3.1

aguard scan --llm --config config.yaml
```

A judge outage never fails silently: unreachable/unconfigured runs surface an `LLM-000`
coverage note, so "judge off" can't masquerade as "no intent issues."

**Full reference:** [`docs/llm-judge.md`](docs/llm-judge.md) (models, endpoints, privacy, rule
IDs) · copy-ready [`config.example.yaml`](config.example.yaml).

## Capability boundary (honest disclosure)

This release is a **static** scanner; its ceiling is "surface discovery + cleanup." It cannot:
- prove malice (only a suspicion level) · observe runtime behavior (remote second-stage payloads, conditional backdoors) · recover the true intent of encrypted/heavily-obfuscated payloads · observe what an MCP endpoint actually does · inspect dependency internals · catch zero-day / unknown techniques.

Coverage caveats worth knowing:
- A **plugin** is scanned as one tree, so its bundled skills/commands/hooks *are* read, but a
  plugin-bundled hook is not audited per (event, command) the way a `settings.json` hook is.
- A **hook script** is followed one level and only inside your home directory. An out-of-home
  reference is not read, and scores as `HOOK-002`.
- Under a config root, **top-level directories no collector owns are not read** — on a real
  machine those are your transcripts, config backups and shell snapshots (`sessions/`,
  `history.jsonl`, `file-history/`), and pulling them into a report would trade a blind spot for
  a leak. They are named in a `COV-000` rather than skipped in silence. Loose files that look
  like code or instructions *are* read, and so is anything under `skills/`, `agents/` or
  `commands/` even with its manifest missing — those are the paths an agent actually loads from.
- **Generated/vendored directories** (`dist/`, `build/`, `out/`, `node_modules/`, `vendor/`,
  `coverage/`) are not read, and are excluded from the canonical hash so an artifact's identity
  survives a rebuild. The scan says so once, as a `COV-000` note. And because the skip is decided
  by a *name the artifact's author chose*, an artifact that points the agent INTO one — a
  `SKILL.md` saying "run `dist/setup.sh`" — is reported as `SUP-004` and scores.

Dimension 7/8 hits (backdoor / resource abuse) are always labeled **"advisory: not confirmed."**
So is `EXFIL-002`, the cross-file credentials→network pair: unrelated files legitimately do
each half, so it is a pair worth reading, not a verdict.

The exfiltration check is structural rather than a pattern: one file that reads credentials AND
makes an outbound call is `EXFIL-001`; one that also **encodes in between** is `EXFIL-003` plus
an `OBF-004` in the obfuscation dimension, because what leaves the box is then unreadable to
whoever is watching the wire. If every network target in that file is loopback (`127.0.0.1`,
`localhost`, `::1`), the chain stays visible but drops to the same low + advisory band as
`EXFIL-002` — the data has not left the machine. Outbound means more than an HTTP client — a
DNS lookup, `nc`, `/dev/tcp`, `scp` and `ssh` carry data just as well. None of it is proof:
encoding a payload has honest uses, so the finding says "this is the shape the attack has",
never "this is the attack".

## Baseline (`.aguardignore`)

Once you've reviewed the findings for an environment, silence the acceptable ones so repeat
scans stay signal-only. `scan` and `clean` read `<root>/.aguardignore` (or `--ignore <file>`):

```
# one rule per line — blank lines and # comments ignored
INJ-001                 # suppress this rule everywhere
EXEC-004  vendor/*       # suppress this rule only under matching evidence paths
*         dist/*         # suppress ANY rule under a path glob
```

Suppressed findings are dropped **before scoring**, and the count is reported as an
`IGN-000` note — a baseline can't silently hide a genuinely new risk.

`check` does **not** auto-discover a baseline. Its target is the thing you are vetting, so a
`.aguardignore` found inside it was written by whoever wrote the artifact, not by you — a
skill shipping one that names its own rule IDs would otherwise score itself 100/100. For
`check`, only an explicit `--ignore <file>` applies.

## Reputation (embedded allowlist / blocklist)

AgentGuard ships a small, **versioned, offline** reputation list keyed by an artifact's
canonical hash — no network, on by default:

- **known-good** (e.g. an official-marketplace plugin such as superpowers, reviewed and recorded with its reason) → its findings are suppressed, so
  a trusted tool's own `rm -rf`/`curl|bash` examples don't drown the report. A `REP-GOOD`
  note records the count + highest suppressed severity (never silent).
- **known-malicious** → a `REP-BAD` finding is raised even if static rules found nothing.

**What is actually in it today: the allowlist half.** The blocklist is *mechanically* wired
(hash-exact, `REP-BAD` is critical, it gates) and *empty* — curating known-bad hashes is
ongoing work, and the file ships no example entry standing in for one, because this list feeds
the score and a demonstration record would be a false record. `aguard version` prints the real
entry count. Treat the value of reputation today as noise suppression on trusted toolkits, not
as malware coverage; detection comes from the static rules.

Because the list is embedded and versioned, it is deterministic and **feeds the score**.
Disable with `--no-reputation`. A maintainer adds an entry by computing its hash:

```bash
aguard hash ./some-skill     # prints the canonical hash to add to the reputation list
```

A cloud reputation service (fresh allowlist + community blocklist, hash-only, opt-in) is
planned — see the design doc; the embedded list is its always-available offline subset.

**Claude Desktop's built-in skills** (docx, xlsx, pptx, skill-creator, import-memory, …) are the one
allowlisted source that is not a repository: several are published nowhere a commit could be pinned.
Their entries pin the desktop's own per-skill `updatedAt` stamp instead (`source: claude-desktop`),
carry the same review and fingerprint, and renew only on a machine that has the desktop store.

## Use as a gate (pre-commit / CI)

`check` returns a non-zero exit code when a finding meets `--fail-on`, so it drops into any gate:

```bash
# git pre-commit hook (scans staged skills)
cp hack/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit

# CI (GitHub Actions)
cp hack/github-action.yml .github/workflows/agentguard.yml
```

See [`hack/`](hack/) for both. A single skill: `aguard check ./some-skill` (defaults to `--fail-on high`).

## Reports

`aguard scan --html report.html` writes a self-contained (offline, no-script) HTML report:
score gauge, environment tiles, findings grouped by artifact+rule with line-level evidence,
and the junk/hygiene section. `aguard scan --report` writes the same report without a path to
invent — to `~/.config/aguard/reports/scan-<timestamp>.html` (or under `$XDG_CONFIG_HOME`),
outside the scan root, and prints where. The plugin does this on every scan and opens the
report at the end of `/aguard-setup`. Reports stay on the machine; they hold your own paths
and redacted snippets.

`--md <path>` writes the same report as GitHub-flavoured markdown, for the place a scan result
usually needs to go next: a pull request comment or an issue. `-` writes it to stdout instead of
the terminal report, so a reviewer can post the result of vetting a new skill without retyping it:

```bash
aguard check ./new-skill --md - | gh pr comment 42 --body-file -
aguard check ./new-skill --md review.md      # or write a file and paste it
```

Every name, path and snippet from the scanned tree is emitted inside a code span, so a file
named `@someone` or `<img src=…>` cannot ping a person or load an image from your comment; the
findings table, the plain-language verdict, the advisory labels and the "not checked" notes are
all there, in the same order as the terminal. Paste the result of `check` or `scan --root .`;
a whole-machine `scan` also lists what it saw under `~/Downloads`, so pass `--inbox off` before
posting one publicly.

## Development

```bash
make test           # go test -race -cover ./...
make lint           # golangci-lint
make dist           # cross-compile release binaries + SHA256SUMS.txt into dist/
```

New to the codebase? [`docs/architecture.md`](docs/architecture.md) is the as-built map: the
pipeline and why its stage order is load-bearing, the package layout, the invariants and where
each is enforced, and an honest account of what is not implemented yet.

## License

[MIT](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): one branch per change, a pull request against `main`,
and `make verify` green before you ask for review. `make hooks` installs the pre-commit gate that
scans any skill you stage with aguard itself.
