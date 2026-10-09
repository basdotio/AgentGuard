<!-- SPDX-License-Identifier: MIT -->
# 031 — A `--llm` report cannot say which judge produced its LLM findings: only `tool_version` names it, and it moves when the judge did not and stays put when the judge changed

- **Source**: maintainer decision (2026-10-10) to overturn the second of P-002's two judge decisions — "today a report
  identifies the judge's code only through `tool_version`" — while keeping the first (the judge stays outside
  `rules_version`)
- **Depends on**: P-002 (`rules_version`, merged); coordinates with P-027 (PR #47, not merged), which refactors the same
  `internal/judge` files
- **Branch**: `p/031-judge-versions-in-report`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Two `--llm` reports, or two committed judge benchmark runs under `baselines/results/aguard/*-llm-*`, differ in their LLM
findings. The first question — "was it the same judge?" — has only one field to answer it: `tool_version`. P-002 measured
that field for the rule table and found it wrong in both directions; it is wrong in the same ways for the judge:

- the version moves while the judge's code does not (a release that touches nothing in `internal/judge/`);
- the judge changes while the version looks the same (two `git describe` strings one suffix apart, with a rewrite of
  what is sent to the model between them);
- a plain `go build` says `dev`, which names nothing.

And `tool_version` names a whole commit, so even when it does change it cannot say *what* changed: the prompts, or the
way an artifact is cut into the excerpt the model reads, or only something that cannot change an answer at all.

P-002 wrote this limit down as a fact in six passages, the `docs/rules.md` header and two tests. The maintainer has now
decided that the report should name the judge instead.

## Initial direction

Two versions, owned by `internal/judge`: a **computed** `judge.PromptVersion()` over everything the client wraps around
the scanned content (system prompts, fence and section layout, response format, temperatures, request envelope), and a
**hand-bumped** `judge.ExcerptVersion` for how an artifact becomes the excerpt and how a quote is grounded against it,
guarded by a golden test. Both, plus the model and `samples`, go into the report's `judge` block; `aguard version` prints
the two versions after `rules=`. `rules_version` does not change.

## Measured (2026-10-10, this repository at origin/main 0cc9391 = `v0.19.0`)

Measured the way P-002 measured the rules: `git rev-list --count` and `git describe` over `internal/judge/` between tags.

| Case | Measured | Consequence |
|---|---|---|
| The version moves, the judge does not | `v0.16.0..v0.17.0`: 4 commits, **0** touch `internal/judge/`; `v0.17.0..v0.18.0`: 10 commits, **0** | Two `--llm` reports from v0.16.0 and v0.18.0 differ in `tool_version` while every byte of judge code is the same |
| The judge changes, the version looks the same | `v0.18.0-55-ga146e8a` (tests only) and `v0.18.0-56-g0146a3c` ("every excerpt replaces the home directory with ~", P-005); `v0.18.0-213-gcbd819f` (tests only) and `v0.18.0-214-ge7c18f7` (padding inside a line is folded before any cap, P-020) | One suffix apart, and the request bodies sent for the same input differ between them |
| What changed cannot be read off the version | `v0.18.0..v0.19.0`: 256 commits, 38 touch `internal/judge/`; of the non-test ones, the excerpt and grounding changed many times (P-005, P-006, P-020, P-021) and the prompts **never**: `git log -G 'Task = \|barrierRule\|===AGUARD\|maxTriageItems\|samplingTemperature\|Temperature\|sectionLabels\|no content\|not declared'` over the non-test judge files since the first public commit `2b4c2a7` finds nothing | A reader who sees two different `tool_version` values cannot tell "the model was asked something else" from "the model was shown something else" from "nothing it sees changed" |
| A build that does not go through `make` | `go build ./cmd/aguard` in this worktree → `aguard dev (commit none, built unknown) · reputation entries=18 · rules=43f245966105` | No information at all about the judge |

The four committed judge runs (`baselines/results/aguard/*-llm-*/run.yaml`) today:

| Run | `tool_version` | Model, samples |
|---|---|---|
| `2026-09-25-llm-glm-5.3-flash` | `aguard v0.11.0-35-g7fbbb68 (…) · reputation entries=18` | `glm-5.3-flash`, 1 — in a `judge:` block written by hand ("contents reproduced under `judge:` below" from a config that is not committed) |
| `2026-09-28-llm-gpt-4.1-mini` | `aguard v0.12.0-2-gb3f3e2a (…)` | `gpt-4.1-mini`, 1 — same, by hand |
| `2026-09-29-llm-gpt-4.1-mini-s3` | `aguard v0.14.0-6-g4d878bc (…)` | `gpt-4.1-mini`, 3 — same, by hand |
| `2026-09-29-llm-gpt-4.1-mini-s3-votes` | `aguard v0.14.0-12-gfc6b179 (…)` | `gpt-4.1-mini`, 3 — same, by hand |

- None of the four commits is an object in this repository (`git cat-file -t 7fbbb68` → "Not a valid object name", and
  the same for the other three): they were built in the former private repository, so a reader of this one cannot
  resolve the only field that names the judge's code.
- Read-only in the former repository: `7fbbb68..b3f3e2a` 47 commits, 2 touch `internal/judge/` (P-019: LLM-009 never
  escalates, `run.go` and its test); `b3f3e2a..4d878bc` 99 commits, **0**; `4d878bc..fc6b179` 6 commits, 2 (P-035: a
  sampled finding's reason names every agreeing vote's severity, `run.go` and its test). `git diff --stat 7fbbb68 fc6b179
  -- internal/judge/` is `advisory_only_test.go`, `consensus_test.go`, `run.go` only. So the four runs showed the model
  the same prompts and the same excerpts — across `tool_version` values from `v0.11.0-35` to `v0.14.0-12`, and nothing in
  the committed files says so. The run.yaml of the first had to explain its own fold in prose ("the behaviour shipped
  in v0.12.0 (P-019 made LLM-009 advisory-only)").
- The model and `samples` are in those files only because someone copied them from an uncommitted config. A `--llm`
  report carries neither: `JudgeSummary` has ran / reason / artifacts / calls / failed / skipped / findings / endpoint
  and the cost fields.

What is sent today (the "before" for the byte-identity criterion): a capture server on `127.0.0.1` answering every
call, `scan --root <fixture>/.claude --inbox off --llm --json` and `check <skill> --llm --json` of a scratch fixture
(a skill with a credential read, an exfiltrating `curl` and a base64 blob, `CLAUDE.md`, a slash command, a hook, an MCP
server), at `samples: 1` and `samples: 3`: 11 / 4 / 27 / 10 request bodies (11 / 4 / 11 / 4 distinct). The bodies, nonce
masked and sorted, are kept in scratch.

## Design

Code read (origin/main 0cc9391): `internal/judge/prompt.go` (`systemPrompt`, `userPrompt`, `sectionLabels`, the six task
texts, `barrierRule`), `triage.go` (`triageTask`, the triage user layout built inline, `maxTriageItems`), `openai.go`
(`chatRequest`, `chat()` encodes with `SetEscapeHTML(false)`, `NewHTTP` defaults the model to `llama3.1`), `run.go`
(`samplingTemperature`, `buildTasks`, `planFor`, `Options.defaults`), `excerpt.go` / `decode.go` / `egress.go` /
`ground.go`, `cmd/aguard/main.go` (`runJudge` and the not-enabled branch of `analyze`), `cmd/aguard/inbox.go` (the
Downloads summary), `cmd/aguard/version.go` (`binaryVersionLine`, `runVersion`), `internal/report/plain.go`
(`judgeLine`, `judgeSummaryLine`), and the diff of PR #47 (P-027) over `internal/judge`.

The judge splits into what can be computed and what cannot, and the two get different mechanisms:

- **`judge.PromptVersion()` — computed.** Everything the client wraps around the scanned content, i.e. what would shape
  the answer to the same excerpt: each pass's system message (task text + barrier rule), the user-message layout (fence,
  section labels, the `(no content)` / `(not declared)` / `(no readable behavior found)` placeholders, the triage
  `[RULE] evidence` lines and the triage item cap), the response format (it is in the barrier rule and `triageTask`), the
  temperatures (0, and `samplingTemperature` when `samples > 1`), and the request envelope (`chatRequest`'s fields as
  encoded). It is a sha256 over a table of **call shapes** — every pass × {both sides, declared empty, behavior empty,
  both empty} × {0, `samplingTemperature`}, plus triage at its cap — each rendered as the exact JSON body the client
  would send, with three stand-ins: the scanned content (`<declared>`, `<behavior>`, `<rule>`, `<evidence>`), the model
  (`<model>`, recorded separately) and the nonce (`<nonce>`, random per call). First 12 hex digits, `sync.OnceValue`,
  pure `promptVersion(shapes)` for tests — the `detect.RulesVersion()` pattern.
  Built by the same `systemPrompt` / `userPrompt` the client calls; the triage messages are built inline in `Triage`
  today (P-027 extracts them), so `prompt_version.go` restates them — and a capture test is what makes that safe: it drives
  the **real** `HTTPClient` for every shape through `httptest`, masks the nonce, and requires each body to equal the one
  that was hashed, byte for byte. A new prompt, a new request field, a changed join or a changed encoder setting either
  moves the version or turns that test red; there is no third outcome.
  Not in it: the endpoint URL and headers (`Authorization` is the key), `Ping` (`llm test` sends no scanned content and
  produces no finding), and the model name.
- **`judge.ExcerptVersion` — hand-bumped**, an integer constant in `internal/judge/ground.go`. It covers what cannot be
  hashed without hashing code: which passes an artifact gets (`planFor`), the text each pass carries (condense, comment
  stripping, head/tail caps, the declared-purpose and hook/connector/MCP templates, the decode limits, home stripping and
  redaction order) and how a quote is checked against it (`groundSpan`, the line map, the snippet window). The discipline
  is `rulesEpoch`'s, but with a tripwire `rulesEpoch` never had: a golden test builds a fixed `t.TempDir()` fixture (a
  skill with comments, blank runs, a padded line, a base64 payload, a long file, a home path and a token; `CLAUDE.md`; a
  slash command; a hook; an MCP server; a connector; static findings that trigger collusion and triage), takes every task
  `buildTasks` plans and every unit, runs a fixed set of quotes through `groundSpan`, and pins the sha256 of all of it
  **together with** the `ExcerptVersion` literal. Changing the excerpt builder without revisiting the version goes red
  with "bump judge.ExcerptVersion to N+1 and set the pinned digest to …"; changing the version without the digest goes
  red too, so the pair is always reviewed together.
- **Model and samples** are what the run used: the model every request named (`judge.ModelFor`, which reads `NewHTTP`'s
  own default rather than restating it) and the effective sample count (`judge.SamplesFor`, through `Options.defaults`, so
  `samples: 0` reads 1, as the run did).

Where they appear:

- **`model.JudgeSummary`** (the JSON `judge` block, also `inbox.judge`) gains `prompt_version`, `excerpt_version`,
  `model`, `samples`, all `omitempty`. The two versions are set on **every** `judge` block — the block exists exactly
  when `--llm` was requested, and they name this binary's judge, so "key absent" means only "older report". `model` and
  `samples` are set when the judge was configured to run (`runJudge`, and the Downloads summary when the config is
  ready); the "requested but not enabled" block has no configuration to name. Nothing else from the `llm` config goes in:
  no key, no headers, no `authority`. One constructor in `cmd/aguard` builds the block for all three call sites.
- **Human renderers**: one line after the judge summary line, only when the judge ran —
  `LLM judge: model <m> · samples <n> · prompt_version <v> · excerpt_version <n>` — in the terminal (through `Sanitize`),
  markdown (through `text`, like the judge line next to it; the model comes from the operator's config, not the scanned
  tree) and HTML (template-escaped, the model sanitized in `sanitizeResult` like the reason). `--json` / SARIF are not
  touched by it; JSON carries the fields above.
- **`aguard version`**: `runVersion` prints `binaryVersionLine(...) + " · judge-prompt=<v> · judge-excerpt=<n>"`.
  `binaryVersionLine` itself does not change, so P-002's test of it stays as written, `$2` is still the version
  (`release.yml` `awk '{print $2}'`), and the baselines adapter, which takes the whole first line as `tool_version`,
  carries both from now on with no change to `baselines/`.

Coordination with PR #47 (P-027, not merged): all new code goes in new files (`internal/judge/prompt_version.go`,
`internal/judge/identity.go`, their tests, `cmd/aguard/judge_summary.go`, `internal/report/judge_identity.go`), and
the five files #47 refactors (`prompt.go`, `judge.go`, `run.go`, `triage.go`, `openai.go`) are not edited at all —
`ExcerptVersion` lives in `ground.go`, which #47 does not touch. #47's
refactor is byte-identical in what is sent, so the capture test holds across it; once it merges, `prompt_version.go`'s
restated triage messages can call #47's `triagePayload` / `fenced`, `ModelFor` can become its `RequestModel`, and
`llm preview` can print the two versions — follow-ups, named below, not done here.

Sentence (b) of P-002 is replaced, in the same six passages, the `docs/rules.md` header and the two tests that pinned it,
by what is now true: a `--llm` report names the judge in its `judge` block (`prompt_version`, `excerpt_version`, `model`,
`samples`), and the judge stays outside `rules_version`. `rulesEpoch` and everything `rules_version` hashes are
byte-identical; only the comment sentence changes, and the value stays `43f245966105`.

## Done criteria

- [ ] `TestPromptVersion_HashesWhatTheClientSends` (`internal/judge/prompt_version_test.go`, new): for every call shape
  (6 passes × 4 content variants × 2 temperatures, and triage with one item more than its cap), the real `HTTPClient`'s
  request body, captured through `httptest` with the nonce masked, equals the body `PromptVersion` hashed for that shape,
  byte for byte. Red on origin/main at compile time (no `PromptVersion`, no call-shape table)
- [ ] `TestPromptVersion_EveryInputIsDecided` (same file, new): by reflection, every field of `Request` and of
  `chatRequest` is declared as stand-in, enumerated or not sent, and every `Mode` constant declared in `judge.go` (read
  with `go/ast`) has shapes. **Mutation**: add a field to `Request`, or a seventh `Mode` constant, without declaring it → red
- [ ] `TestPromptVersion_MovesWithWhatShapesTheAnswer` (same file, new): on a copy of the shape table, changing one word
  of one system prompt, one byte of a user layout, one temperature, the triage cap, dropping a shape or swapping two each
  moves `promptVersion`; `TestPromptVersion_IsStable`: 12 lowercase hex digits, two calls equal, equal to
  `promptVersion(callShapes())`. **Manual mutation, recorded**: one word of `injectionTask` changed in the source moves
  the value `aguard version` prints, and every test stays green (the version follows the prompt; nothing to update)
- [ ] `TestExcerptVersion_IsPinnedWithItsGolden` (`internal/judge/excerpt_version_test.go`, new): the fixture's digest and
  `ExcerptVersion` equal the pinned pair. Red on origin/main at compile time (no `ExcerptVersion`). **Mutation**: change
  `maxFileBytes` or the omitted-lines marker → red with the message telling the editor to bump `ExcerptVersion`; reverted
- [ ] `TestE2E_ReportNamesItsJudge` (`cmd/aguard/judge_summary_test.go`, new): `scan --llm` and `check --llm` against an
  `httptest` judge → the `judge` block has `prompt_version == judge.PromptVersion()`, `excerpt_version ==
  judge.ExcerptVersion`, `model` = the configured one (`llama3.1` when the config leaves it empty), `samples` = the
  configured one (1 when the config says 0); the API key, set through the environment to a sentinel, appears nowhere in
  the JSON; "requested but not enabled" has the two versions and no `model` / `samples`. Red on origin/main at compile time
- [ ] **Reverse assertion**: without `--llm` there is no `judge` block at all, and with `--llm` the deterministic numbers
  (`overall`, findings, `rules_version`) are unchanged by this proposal — `TestE2E_*` and `TestCheckCmd_*` pass with no
  line of theirs changed
- [ ] `TestRunVersion_JudgeVersionsComeAfterRules` (`cmd/aguard/judge_summary_test.go`, new): the first line of
  `runVersion` is `binaryVersionLine(...)` exactly, followed by ` · judge-prompt=<PromptVersion()> · judge-excerpt=<n>`,
  and `strings.Fields(line)[1]` is the version. **Reverse**: `TestBinaryVersionLine_RulesComeAfterTheExistingFields`,
  `TestPluginVersionLine`, `TestPluginVersionLine_LegacyName`, `TestApplyBuildInfo` unchanged and green
- [ ] `TestJudgeIdentityLine_*` (`internal/report/judge_identity_test.go`, new): the terminal, markdown and HTML reports
  print the one identity line when the judge ran, and nothing when it did not run or `--llm` was not passed; a model name
  carrying U+202E, a backtick run or `[x](u)` stays inert in each. **Reverse**: `TestCleanSettingsReportIsUnchanged`
  (the golden) unchanged
- [ ] `TestRulesVersionDocs_NameTheJudgeInItsOwnBlock` (`internal/detect/rules_version_docs_test.go`, replaces
  `TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion`) and `TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion`
  (`hack/gen-rules/main_test.go`, rewritten): the six passages and the `docs/rules.md` header name `prompt_version` and
  `excerpt_version` and no longer say "only through `tool_version`" or its Chinese equivalent; the header still puts every
  `LLM-` ID outside `rules_version`. Red on origin/main for the six passages and the header
- [ ] **Reverse assertion**: `rules_version` is `43f245966105` before and after; `TestRulesVersion_*`,
  `TestRulesDocHeaderSaysWhatTheHashCovers`, `TestHashGolden` and `TestZeroDial_*` pass with no line of theirs changed, and
  `TestRulesEpoch_ScopeIsDeterministicOnly` with only its doc comment changed (it restated sentence (b))
- [ ] **What is sent does not change**: the capture of "Measured", rerun with this branch's binary, gives the same nonce-
  masked request bodies for all four runs (`diff` empty)
- [ ] `make verify` green; `go version` does not switch toolchains; `go.mod` line 2 is `go 1.23.5`; no new dependency

## Out of scope

- **No protocol version header on judge requests.** No endpoint reads one, and an unknown header sent to an arbitrary
  OpenAI-compatible endpoint the user brings has no value; the request headers do not change
- **What is sent does not change**: request bytes to the endpoint are byte-identical (the capture criterion); no prompt,
  excerpt, cap or temperature changes, and `ExcerptVersion` starts at 1 for what ships today
- **`rules_version` does not change**: `rulesEpoch`, `rulesVersion` and everything they hash are byte-identical; only the
  comment sentence (b) in `rules_version.go` changes. The judge stays outside `rules_version` (P-002's decision (a))
- **The canonical hash, the gate, collect and detect do not change**: zero diff in `internal/collect`, `internal/gate`,
  `internal/reputation`, `internal/permcheck`, `internal/clean`; in `internal/detect` only that comment, the doc comment of
  `TestRulesEpoch_ScopeIsDeterministicOnly` and `rules_version_docs_test.go`
- **The five files PR #47 refactors are not edited** (`internal/judge/{prompt,judge,run,triage,openai}.go`)
- **`aguard llm preview` does not show the versions**: it exists only on #47's branch; a follow-up after it merges
- **`baselines/` does not change**, and the committed `results/` do not change by a byte. Recording the four fields in
  the ledger, and the bench fold reading `judge.samples` instead of `run.yaml`, are follow-ups
- **`authority` and the rest of the `llm` config do not go into the report**: `authority` decides whether a gate may act,
  not what the judge produced
- **SARIF does not change**; `plugin/` does not change; the README pair does not change (the judge's reference is the
  `docs/llm-judge` pair)
- No dependency added; `go.mod` / `go.sum` do not change

## Must not claim

- **Do not say "same `prompt_version`, `excerpt_version` and `model` ⇒ same answers"**: the model is sampled (and at
  `samples > 1` deliberately at temperature 0.8), a vendor can change what serves a model name without renaming it, and an
  endpoint can add its own system prompt or limits that no report can see
- **Do not say the two versions cover the whole judge**: consensus and its tally, severity clamping, `LLM-007`'s fixed
  severity, the advisory-only table, the mode → rule ID / dimension / title mapping and the triage label parsing are in
  neither; for those `tool_version` is still the only stamp
- **Do not say `excerpt_version` is mechanical**: it is bumped by hand; the golden catches a change only where its
  fixture exercises the code, so a change that fixture does not reach can ship under the same number (as a forgotten
  `rulesEpoch` bump can)
- **Do not say `prompt_version` covers the model, the endpoint or the content**: those are abstracted out by design;
  `model` and `endpoint` are their own fields
- **Do not say a report without the keys had no judge, or a different one**: it predates this proposal
- **Do not say the four committed judge runs used the same judge build**: their versions cannot be computed afterwards;
  what is measured is that no commit between their builds touched the prompt or excerpt files
- **Do not say `rules_version` now covers the judge**: it does not; nothing about it changed

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | New tests and the rewritten docs tests, run red | `judge, cmd, report, detect, gen-rules: tests — a --llm report and aguard version cannot say which judge produced the LLM findings (P-031)` |
| 2 | `judge.PromptVersion` over the call-shape table, `ModelFor`, `SamplesFor` | `judge: PromptVersion hashes every request body the client sends, with the content, model and nonce stood in (P-031)` |
| 3 | `judge.ExcerptVersion` in `ground.go`, pinned with the golden digest | `judge: ExcerptVersion names how an artifact becomes the excerpt and how a quote is grounded, pinned to a golden (P-031)` |
| 4 | `JudgeSummary` gains the four fields; one constructor in `cmd/aguard` for the three call sites; the version line | `model, cmd: the judge block names prompt_version, excerpt_version, model and samples, and aguard version prints the two versions (P-031)` |
| 5 | The identity line in the terminal, markdown and HTML reports | `report: one line under the judge summary names the model, samples and the two judge versions when the judge ran (P-031)` |
| 6 | Sentence (b) replaced: `rulesEpoch` and `ScanResult.RulesVersion` comments, the architecture pair, spec §5.1 / §8, the gen-rules header, `make docs` | `detect, model, gen-rules, docs: the judge is named by its own block, not only through tool_version, and stays outside rules_version (P-031)` |
| 7 | The `docs/llm-judge` pair, spec §5.2 and §8 `JudgeSummary`, `.claude/rules/judge.md` | `docs, rules: llm-judge, spec and the judge rule say what the two versions cover and when ExcerptVersion is bumped (P-031)` |
| 8 | This file, the index | `proposals: P-031 (P-031)` |

## Open questions

1. **Pin `PromptVersion` to a literal, as `ExcerptVersion`'s digest is pinned?**
   **Recommendation**: no — P-002's answer for `rules_version`, for the same reason: the value should move with every
   prompt change, and a test whose only correct response is "update the number" protects nothing. The digest pinned for
   `ExcerptVersion` is different: it guards a number a person has to remember to bump. What protects `PromptVersion` is the
   capture test — it can only be green if the hash covers what is sent.
   **Decided (2026-10-10)**: as recommended.
2. **Append the two versions to the `aguard version` line?**
   **Recommendation**: yes, after ` · rules=<v>`, with `$2` still the version. The baselines adapter takes that whole line
   as `tool_version`, so every future benchmark run records the judge build without a change to `baselines/`.
   **Decided (2026-10-10)**: as recommended.
3. **Record `model` and `samples` too?**
   **Recommendation**: yes. They are the other half of attribution — the same judge build at `samples: 1` and `samples: 3`
   asks at different temperatures and keeps different findings — and `samples` is what reads `calls` back as questions.
   The values recorded are the effective ones (`llama3.1` when the config names no model, 1 when it says 0), because that
   is what the requests carried.
   **Decided (2026-10-10)**: as recommended.
4. **The versions on a `judge` block whose judge did not run?**
   **Recommendation**: yes in JSON, so a missing key means only "older report"; `model` / `samples` only when the judge
   was configured to run. The human identity line only when it ran: with no LLM finding there is nothing to attribute.
   **Decided (2026-10-10)**: as recommended.
5. **Print the current judge versions in the `docs/rules.md` header, as `rules_version` is?**
   **Recommendation**: no. `rules_version` is there so the drift check catches a pattern change; a prompt change is
   already caught by the capture test, and the page is the rule reference. The header's sentence about the judge names
   the fields that identify it.
   **Decided (2026-10-10)**: as recommended.
6. **Which passes an artifact gets — `PromptVersion` or `ExcerptVersion`?**
   **Recommendation**: `ExcerptVersion`. The plan lives in `planFor`'s code, which cannot be hashed without hashing source
   (P-002 open question 4), and it decides, like the excerpt, what text a given artifact puts in front of the model.
   `PromptVersion` stays the computed half: what is wrapped around any excerpt.
   **Decided (2026-10-10)**: as recommended.
