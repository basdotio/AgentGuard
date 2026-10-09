<!-- SPDX-License-Identifier: MIT -->
# 027 — Before turning on `--llm`, nobody can see what would leave the machine: the excerpts are visible only to a capture server

- **Source**: new finding (2026-10-09) — follow-up to P-003 (zero-dial test), P-005 (judge egress paths), P-006
  (judge rendering) and P-020 (excerpt padding): each of them changed what the judge sends, and each proved it with an
  `httptest` server reading request bodies, because there is no other way to look
- **Depends on**: none
- **Branch**: `p/027-judge-units-preview`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

A user deciding whether to point `--llm` at their own endpoint (a hosted model is the common case: `aguard llm setup`
offers the presets) has to accept, up front, that "best-effort-redacted excerpts" of their skills, hooks, MCP
configuration and instruction files go to that vendor. `docs/llm-judge.md` describes the rules those excerpts follow —
redaction, the two homes replaced by `~` (P-005), MCP configuration by key, padding folded (P-020), comment lines
dropped, 6,000/2,000-byte caps kept head + tail — but nothing lets the user **see** the result for their own files:

| What they can do today | What it shows |
|---|---|
| `aguard llm status` | the endpoint, model and key source — not one byte of content |
| `aguard scan --llm` / `check --llm` | the verdicts; the excerpts have already left |
| `aguard check <target> --json` | the static report; its snippets are not the judge's excerpts (different caps, no home scrub, no condensing) |
| point `base_url` at a local capture server | the request bodies — the only way, and the one P-003/P-005/P-006/P-020 all used in their tests |

The same gap hits anyone reproducing a reported judge run: a finding's `file:line` says where the judge looked, not what
it was given, and the excerpt depends on the artifact's static findings (triage evidence, the collusion pass gated on
`EXFIL-002`), on the baseline and the reputation list (they run before the judge), on `samples` and `max_calls`.
Rebuilding it by hand means re-implementing the excerpt builders, which is how a second, drifting implementation starts.

## Initial direction

A command that runs the same pipeline `scan --llm` / `check --llm` runs, up to the point where the judge would send,
and prints the planned calls instead of making them: per artifact its kind, name and canonical hash, and per call the
pass and the exact text that would sit inside the nonce fence, built by the same functions (`planFor` / `userPrompt`),
plus what the plan already knows was left out (an MCP configuration cut to fit, triage items past the cap, calls past
the budget). It never builds an HTTP client, so it joins the zero-dial test's zero table. Candidates: `aguard llm
preview [path]` next to `setup`/`test`/`status`, or `check --llm --dry-run`.

### Measured (2026-10-09, on `origin/main` 155865b)

- `aguard llm --help` lists `setup`, `status`, `test`; neither `scan --help` nor `check --help` has a dry-run or preview
  flag (`grep -i 'dry\|preview'`: no match).
- A scratch environment (one skill whose script reads `~/.aws/credentials` and posts it, a `CLAUDE.md` with a
  3,000-space line, a `PreToolUse[Bash]` hook, one MCP server with `DB_PASS` in its env) scanned with
  `scan --llm --inbox off` against a local capture server: **7 calls** — intent, triage, injection (skill), MCP config,
  injection (hook), injection (`CLAUDE.md`), capability (hook). What they carried was visible only in the captured
  bodies: `env.DB_PASS=<REDACTED>`, the hook command as `<REDACTED>.claude/hooks/log.sh`, the padded line as one blank
  line, the comment line of the script gone. Nothing in the report shows any of it.
- The same skill as a `.zip`, `check --llm` twice: the 3 payloads are identical across the two runs once the nonce is
  masked (the extraction directory does not reach them for this fixture).
- These bodies, nonce masked and sorted, are kept in scratch as the "before" of what is sent
  (`before-scan.norm` sha256 `f54379e4…`, `before-zip.norm` `f8d585a6…`); the branch must reproduce them byte for byte.

## Design

**`aguard llm preview [path]`**, a fourth subcommand next to `setup` / `test` / `status` (see open question 1):

- With a path: what `check <path> --llm` would send. Without: what `scan --llm` would send — the `--root` environment
  and, as on `scan`, the Downloads candidates (`--inbox`, same default, `off` disables).
- It runs the same `scanEnv` / `scanInbox` / `checkTarget` → `analyze()`. At the exact point where `analyze()` calls
  `runJudge`, a preview sink in `scanOpts` receives `judge.Plan(arts, opts)` instead; `runJudge` is never reached, so
  no `judge.NewHTTP`, no key lookup, no endpoint check. The options that shape the plan (`samples`, `max_calls`, the
  scan's home) come from one helper that `runJudge` uses too.
- `judge.Plan` is `Run`'s own planning: the call table (`buildTasks` → `planFor`) and the budget cut move into one
  function both call. Each planned call's payload is rendered by the function the HTTP client uses for the text inside
  the fence: `userPrompt` and `Triage` are split into "fence around payload" and "payload" (`judgePayload`,
  `triagePayload`, the latter including the 40-item cap), so the preview cannot print a payload the client would not
  send. The pass name and rule ID come from one mode table that `finding()` also reads.
- Per artifact: kind, name, canonical hash (`ArtifactReport.Hash`, as computed by `analyze()`), and per question: pass,
  rule, the instruction (the system message's task text; the barrier rule after it names the per-call nonce and is not
  shown), temperature, how many times it is asked (`samples`), how many of those the budget refuses, the payload, where
  its lines come from (file and source lines, from the units' line maps), and what the run would disclose as shortened
  (an MCP configuration cut to fit; triage items past 40).
- `--json` carries the payload bytes exactly. The terminal view prefixes every payload line (so a payload cannot forge
  the end of its block) and passes it through `report.Sanitize` (invariant #7); it says that `--json` has the exact bytes.
- Deterministic: no timestamp, the plan's own order, artifacts in `analyze()` order. Same input on the same machine →
  byte-identical JSON (the homes replaced come from the machine; the config is input).

## Done criteria

- [x] `TestPlan_PayloadsAreWhatTheClientSends` (`internal/judge/preview_test.go`, new): for artifacts covering every
  pass (skill with a script and static findings → intent, injection, triage; a base64 blob → deobfuscation; `EXFIL-002`
  → collusion; `CLAUDE.md` with a padded line; a hook → injection + capability; an MCP server with an oversized value →
  MCP config, shortened), the multiset of `Plan` payloads equals the multiset of fenced contents an `httptest` server
  receives from `Run` with `NewHTTP`, fence lines removed; temperatures and the model match per call; `shortened` is
  what `Run`'s `LLM-000` names. Red at compile today (`Plan` undefined)
- [x] `TestPlan_BudgetAndSamplesMatchRun`: with `samples: 3` and `max_calls` cutting inside a question, the calls
  `Plan` marks as not sent equal `Stats.Skipped` of `Run` on the same artifacts, and every payload `Run` sent is in the
  plan exactly `calls − not_sent` times
- [x] `TestPlan_TriageCapIsShown`: 45 static findings → the triage payload carries 40 lines, `shortened` says 5 were
  not sent, and the endpoint receives that payload; `TestPlan_SourcesNameTheLinesSent`: the intent call names
  `run.sh:1,3-5` (line 2 is a comment and is not sent); `TestRequestModel`: the preview's default model is the
  client's
- [x] `TestLLMPreview_MatchesWhatScanAndCheckSend` (`cmd/aguard/preview_test.go`, new, drives the built binary): a
  fixture set — skill, hook, MCP config, `CLAUDE.md`, a padded line and an oversized MCP value, one Downloads item —
  run through `scan --llm --root R --inbox D --json` and `check T --llm --json` against an `httptest` capture judge; the
  payloads `llm preview --root R --inbox D --json` and `llm preview T --json` print are byte-identical, as multisets, to
  the captured fenced contents, and so are the call counts, models and temperatures
- [x] `TestLLMPreview_Deterministic`: the same `llm preview --json` run twice → byte-identical stdout
- [x] `TestLLMPreview_TerminalViewIsSanitized`: a payload holding ESC and U+202E prints U+FFFD in the terminal view and
  the raw bytes (JSON-escaped) under `--json`; every payload line in the terminal view carries the prefix
- [x] Zero-dial: `llm preview <target>` and `llm preview` (environment + Downloads) are rows of the zero table in
  `TestZeroDial_OnlyTheJudgeConnects`, with the judge **enabled** in the config; both counters stay 0 and each row
  asserts it planned at least one call (a preview that planned nothing would pass for the wrong reason)
- [x] **Reverse assertion**: the judge still sends — the positive control of `TestZeroDial_OnlyTheJudgeConnects`
  (`scan --llm`, its Downloads items, `check --llm`, `llm test`) stays green with the judge counter ≥ 1, and the
  existing judge tests (`TestHTTPClient_RoundTripAndRedaction`, `TestRun_*`, `TestE2E_*`, `TestCheckCmd_LLMRunsTheJudge`)
  pass unchanged; the scratch capture of the measured fixture reproduces `before-scan.norm` / `before-zip.norm`
  byte for byte on the branch binary (what is sent did not change)
- [x] `make verify` green; `go.mod` line 2 still `go 1.23.5`; no toolchain switch; no new dependency

## Out of scope

- **What is sent does not change**: no excerpt builder, cap, redaction, home scrub, prompt text or request field is
  changed; the refactor only splits existing functions so the preview can call them (evidence: the scratch capture
  before/after)
- **No public Go package** (`pkg/…`): `judge.Plan` is in `internal/judge`
- **No `check --llm --dry-run` / `scan --llm --dry-run`** (open question 1)
- **No validation of a real run's preconditions**: the preview does not resolve the key, does not run
  `CheckEndpoint`, does not need `llm.enabled`; `llm status` and `llm test` do that
- **The plugin's `/aguard-llm` flow is not changed** (left as a follow-up)
- **No change to `scan`, `check`, the gate or their reports**; the gate never runs the judge, so it has nothing to preview

## Must not claim

- **Not "everything that leaves the machine"**: the request also carries the `Authorization` header with the key,
  the system message's barrier rule naming the per-call nonce, and the connection itself (address, TLS). The preview
  shows the body's model, temperature, the instruction text and the payload — the part that comes from the user's files
- **Not "the run will send exactly these calls"**: a real run can send fewer (the run deadline `total_timeout`, an
  endpoint refused, a missing key, `llm.enabled: false`) and a retry re-sends the same call; the plan is for the files
  and config as they are when the preview runs
- **Not "redaction is verified"**: the preview shows what best-effort redaction produced, it does not make it a guarantee
- **Not "every omitted line is listed with its reason"**: the sources give the file lines a payload came from;
  comment lines, blank runs and capped middles are the lines missing from them, without a reason each; a file that
  did not fit or is not a behavior file does not appear at all
- **Not "identical across machines"**: the homes replaced by `~` are this machine's
- **Not "zero egress proven"**: the zero-table row shows only that the preview sends nothing through the two
  transports the counters watch (the limits P-003 recorded)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | Judge tests: `Plan` payloads vs what `Run` sends, budget and samples (red: `Plan` undefined) | `judge: tests — nothing shows what a run would send without sending it (P-027)` |
| 2 | `judge.Plan`, sharing `Run`'s plan and the client's payload builders; one mode table | `judge: Plan returns the calls Run would make, with the payloads the client would send (P-027)` |
| 3 | Command tests: preview vs captured `scan --llm` / `check --llm`, determinism, sanitizing, zero-table rows (red) | `cmd: tests — llm preview must print what scan --llm and check --llm send, and send nothing (P-027)` |
| 4 | `aguard llm preview [path]`: the preview sink in `analyze()`, the renderers, the shared plan options | `cmd: llm preview prints what --llm would send for a target or the environment, without sending it (P-027)` |
| 5 | Docs: llm-judge pair, README pair, architecture pair, spec command list, invariant #1's zero table, judge.md guard | `docs, rules: llm preview in the judge docs and in invariant #1's zero table (P-027)` |
| 6 | Done block, `git mv` to `complete/` | `proposals: P-027 (P-027)` |

## Open questions

1. **Command or flag?** `aguard llm preview [path]`, or `check --llm --dry-run` / `scan --llm --dry-run`.
   **Recommendation**: `llm preview`. The user deciding has usually not enabled the judge yet, and `--llm` without
   `llm.enabled` already means "static only + `LLM-000`"; `--json` on `check` / `scan` must keep meaning the report,
   and the flag would interact with `--fail-on`, `--sarif`, `--md`, `--html`; the `llm` group is where the judge's other
   primitives live (`setup` writes, `test` makes one call, `status` reads the config, `preview` shows the payloads).
   **Decided (2026-10-09)**: as recommended
2. **Does the preview need `llm.enabled: true`?** **Recommendation**: no; it plans with the config as it is (samples,
   max_calls, endpoint, model) and says whether the judge is enabled. **Decided (2026-10-09)**: as recommended
3. **Without a path, include the Downloads candidates?** **Recommendation**: yes, with `scan`'s `--inbox` default and
   `off`, because `scan --llm` sends them. **Decided (2026-10-09)**: as recommended
4. **Show the system message?** **Recommendation**: show each pass's instruction text once (JSON), not the barrier rule:
   it names the per-call nonce, and printing it with a stand-in nonce would be a fake barrier.
   **Decided (2026-10-09)**: as recommended
5. **Samples**: one entry per question with `calls: N`, not N identical entries. **Recommendation**: as stated.
   **Decided (2026-10-09)**: as recommended
6. **Mention `llm preview` in the plugin's `/aguard-llm` flow?** **Recommendation**: not here; the plugin's wording has
   its own self-scan constraints. Recorded as a follow-up. **Decided (2026-10-09)**: as recommended

## Done

```
Merged: PR to be opened (2026-10-10; find the sha with git log --grep P-027 after the merge)
Released: pending release
Evidence: TestPlan_PayloadsAreWhatTheClientSends, TestPlan_BudgetAndSamplesMatchRun, TestPlan_TriageCapIsShown, TestPlan_SourcesNameTheLinesSent, TestRequestModel (internal/judge/preview_test.go); red at compile at W1 (undefined: PlannedCall, Plan, RequestModel) → green at W2; a Plan that shows the bare Behavior instead of judgePayload → red (12 previewed, 12 sent, 3 "planned but not sent"); a Plan that drops the triage cap note → red
Evidence: TestLLMPreview_MatchesWhatScanAndCheckSend, TestLLMPreview_Deterministic, TestLLMPreview_TerminalViewIsSanitized (cmd/aguard/preview_test.go, built binary); red at W3 (undefined: previewOpts, runLLMPreview, previewGutter; with stubs for them the binary has no llm preview: "unknown flag: --inbox") → green at W4; planning with an empty home instead of the scan's → red (scan --llm: the hook's injection and capability calls differ on both sides)
Evidence: zero table of TestZeroDial_OnlyTheJudgeConnects (cmd/aguard/zero_dial_test.go): rows "llm preview <target>" and "llm preview (environment, Downloads items)" green with the judge enabled in the config, each having planned calls; the same rows with llm: true added to the preview's opts → red, 4 and 16 judge requests to 127.0.0.1:9
Evidence (reverse assertion): the positive control of TestZeroDial_OnlyTheJudgeConnects (scan --llm, its Downloads items, check --llm, llm test) and every existing test pass with no existing test line changed (git diff --stat origin/main...HEAD -- '*_test.go': two new preview_test.go files, zero_dial_test.go +27/-0); what is sent did not change: the request bodies of the measured scratch fixture (scan --llm, 7 calls; check --llm on the .zip twice, 6 calls), nonce masked and sorted, are byte-identical on 155865b and on the branch binary (sha256 f54379e4… and f8d585a6… on both)
Evidence (real environment, ~/.claude, --inbox off): llm preview exit 0, 297 calls over 181 artifacts (173 with calls), all seven passes; 97 s wall against 94 s for the static scan of the same root; two runs minutes apart had identical requests and differed only in the content hash of two memory files rewritten between them
Evidence (not done): git diff --stat origin/main...HEAD -- internal/judge/excerpt.go internal/judge/egress.go internal/judge/ground.go internal/judge/decode.go internal/collect internal/detect internal/gate internal/report plugin go.mod go.sum → empty; no pkg/ directory
Verify: make verify → "verify: all gates passed"; go.mod line 2 go 1.23.5; go version go1.23.5 darwin/arm64, no toolchain switch
```
