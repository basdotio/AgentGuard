<!-- SPDX-License-Identifier: MIT -->
# 004 — The pre-install check cannot use the judge, and CI users can only take the scan --root detour

- **Source**: by the spec `check` is always static, so the judge is unavailable when checking a downloaded skill before
  install; `--llm` is an explicit switch anyway, and `scan` already offers it for equally untrusted Downloads content.
  Ported from P-047 in the former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/004-check-llm`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`aguard check <path>` is the command that reviews a skill / plugin / zip on a PR (default `--fail-on high`). It has no
`--llm`: on `main` (`dec64ca`, v0.18.0) `aguard check ./x --llm` is cobra's `error: unknown flag: --llm`, exit code 2;
`--fail-on-llm` likewise does not exist (`error: unknown flag: --fail-on-llm`, exit code 2). `cmd/aguard/main.go:683`
says "`check` is static-only by contract (spec §3)", the help string at `main.go:656` is "(static only)"; spec §3 says
verbatim "`check` (untrusted pre-install content) is **forced static** and does not accept `--llm`".

Someone who wants the judge to look at a single target has only one detour today:

| Detour | Consequence |
|---|---|
| **`scan --root <target>`** | `scan`'s root is "the operator's own environment", so it **reads `<root>/.aguardignore` automatically** (`resolveIgnorePath`, `autoBaseline: true`). A target that ships a baseline listing its own rule IDs can suppress its own findings and keep `--fail-on` quiet — exactly the shape `check` deliberately refuses (`TestCheckTarget_TargetSuppliedBaselineIgnored`). To use the judge, a CI user has to reopen a hole that was already closed |
| **Build a fake `.claude` tree and put the target in `skills/<name>/`** | This is what the baselines adapter does (`Stage` in `baselines/adapter/aguard/stage.go`). The score becomes the aggregate environment score instead of this artifact's score; you also have to remember `--inbox off`, or the CI machine's `~/Downloads` is scanned too; targets with the wrong layout (bare MCP source, a single file) cannot be put in at all |

And the "untrusted pre-install content" reason no longer holds on the `scan` side: `scan --llm` runs each **not yet
installed** candidate in `~/Downloads` through `checkTarget`, and the judge runs as usual (`checkCandidate` in
`cmd/aguard/inbox.go`). The same kind of content, the same code path: coming in through `scan` it can be judged,
coming in through `check` it cannot.

The spec also contradicts itself in one place: the command-line line of §3 says "forced static, **ignores** `--llm`",
the item below it says "**does not accept** `--llm`". The actual behaviour is the latter.

In one sentence: **the command that most needs the judge's second opinion — reviewing something someone else wrote that
is not yet installed — is exactly the only one that cannot get it; and the only way to get it is more dangerous than
not getting it.**

## Initial direction

`check` registers `--llm` and `--fail-on-llm` with exactly the same semantics as `scan`: `--llm` is the same explicit
switch and sends the same redacted excerpt; `--fail-on-llm` likewise requires `llm.authority: escalate`, and without
the authority it is refused with an error; `--fail-on` still only looks at deterministic findings.
The `scanOpts` built by the load-time gate (`aguard hook`) and by `aguard approve` never set `llm`, and a test pins
this. Spec §3 / §5.2 / §16 and the docs that say "`check` is always static" change accordingly. The baselines adapter
keeps passing `--llm` only to `scan`; only the reason changes.

## Done criteria

- [x] `TestCheckCmd_LLMRunsTheJudge` (`cmd/aguard/check_llm_test.go`, new, runs the binary): fake endpoint + a config
  with `llm.enabled: true`, `check <skill dir> --llm --config … --json` → exit 0, `judge.ran == true`, one `LLM-003`,
  `overall` equal to the `overall` of `check` on the same directory without `--llm`, `overall_effective < overall`;
  **the same skill packed as a `.zip`** run again, likewise `judge.ran == true`. Without `--quiet` stderr has the
  `LLM judge:` line, with `--quiet` it does not. Today: exit 2, `unknown flag: --llm`
- [x] `TestCheckCmd_FailOnLLMNeedsAuthority` (same file, new, runs the binary): under `authority: escalate`,
  `--llm --fail-on-llm high` → exit 1; under `authority: advisory` the same command → exit 2, stderr names
  `llm.authority: escalate` (refused, not ignored). Today both exit 2, reporting `unknown flag: --llm` (cobra reports
  the first unknown flag; `check <x> --fail-on-llm high` on its own reports `unknown flag: --fail-on-llm`).
  This only holds when the fixture has **no** deterministic high — with a high, the default `--fail-on high` returns 1
  first and the refusal never appears; and when the refusal does appear it is already after the judge has sent its
  requests. The next item (W7) covers this
- [x] `TestFailGateFlags_RefusedBeforeTheJudge` (same file, new, runs the binary; W7): the fixture **has one
  deterministic high**, the endpoint counts. On `check` and on `scan`, `--fail-on-llm high` without the authority (with
  and without `--llm`), `--fail-on-llm hgih`, `--fail-on hgih` → all exit 2, stderr names the reason, the endpoint gets
  **0** requests, stdout is empty (nothing scanned, no report printed). Reverse: `authority: escalate` +
  `--llm --fail-on-llm high` → exit 1, the judge runs as usual. `TestFailGate_DeterministicHitDoesNotMaskARefusal` (same
  file, in-process): when `--fail-on` has already hit, `failGate` still refuses a `--fail-on-llm` it cannot honour, and
  does not return exit 1
- [x] Reverse assertion: `--fail-on` under `check --llm` still only looks at deterministic findings — the same fixture,
  `authority: escalate`, `--llm` plus the default `--fail-on high` → exit 0 (the judge's high reaches `--fail-on-llm`,
  not `--fail-on`). `TestFailGate`, `TestFailGate_*` still green without a single character changed
- [x] Reverse assertion: `TestGateScannerNeverEnablesLLM` (`cmd/aguard/check_llm_test.go`, new, in-process): a
  **judge-ready** config with `authority: escalate` points at a counting endpoint; `gateOptions(…).Scan`, `.ScanRoot`,
  `approvePath`, `runHook` (`PreToolUse[Skill]` and `SessionStart`) each run once → the endpoint receives **0**
  requests, and the results of `Scan` / `ScanRoot` have `Judge == nil`. Green before and after the change: what it pins
  is "the gate does not turn on the judge along with `check`".
  Tightened in W8: every hook reply must also **read as an audit** — `PreToolUse` contains no `GATE-000`, names
  `test-runner` and carries `NN/100`; `SessionStart` begins with `AgentGuard audited … at session start`. Asserting only
  that the reply is non-empty is not enough: a `GATE-000` that scanned nothing is also non-empty and also 0 requests
- [x] Reverse assertion: `TestCheckCmd_ConfigAloneNeverCallsTheJudge` (same file, new, runs the binary): a judge-ready
  config, **without** `--llm` → 0 requests to the endpoint, no `judge` key in the JSON, stderr empty. Green before and
  after the change
- [x] Reverse assertion: the output of `check` without `--llm` is byte-for-byte unchanged — `origin/main` and this
  branch each built into a binary with the same set of `-ldflags`, run on four targets (malicious skill / benign skill /
  single file / zip) for text, `--json` (with `scanned_at` removed), `--md -`, `--sarif` and so on: `diff` empty, same
  exit codes.
  `TestGateAgreesWithCheck`, `TestCheckTarget_*`, `TestMarkdownFlag_*` still green without a single character changed.
  **The scope is valid thresholds**: after W7, with a misspelt threshold `check` (and `scan`) exits 2 before collecting
  and prints no report, whereas `origin/main` prints the full report first and then exits 2 — same exit code, different
  stdout (see "Must not claim")
- [x] Reverse assertion: `baselines/adapter/aguard/passthrough_test.go` still green without a single character changed —
  the adapter still passes `--llm` only to `scan`
- [x] `make verify` green; `go version` does not switch toolchains; the second line of `go.mod` is still `go 1.23.5`, no
  new dependencies

## Out of scope

- **The semantics of `--fail-on` do not change**: it only looks at deterministic findings, and `check` still defaults to
  `high`; no model output can change its answer (invariant #4)
- **The load-time gate, `aguard approve` and `clean` never set `llm`**, and no config option is added that would let
  them turn on the judge. The gate has a 30s deadline, fails open and fires on every load; a hint whose answer depends
  on whether the endpoint replied is not a gate
- **No defaults change**: `--llm` on `check` is off by default; `llm.enabled` defaults to `false`; `--fail-on-llm`
  defaults to empty
- **The baselines behaviour does not change**: the adapter still passes `--llm` only to `scan` (`passthrough_test.go`
  not changed by a single character), and committed results are not rerun.
  Only its reasoning changes (one line in the README, two comments in the adapter, the help string of the driver flag)
- **`plugin/` is not touched**: whether `/aguard-vet` offers a "deep check" step is a decision for the plugin's
  conversation flow, taken separately
- **`README.md` / `README.zh-CN.md` are not touched** (see open question 3)
- **`.claude/rules/invariants.md` is not touched**: invariant #1 is being rewritten as an enumeration by the parallel
  P-003; once both are merged, that list must contain `check --llm` (written in the PR).
  **Whichever merges later is responsible**, four places in total: the outbound-path list of invariant #1, spec
  §16.4's "there are only two outbound paths", `uploads_samples_basis` in `baselines/tools.yaml`, and a `check --llm`
  row added to the positive control of `TestZeroDial_OnlyTheJudgeConnects` (its `check` row in the zero table is
  without `--llm` and should still be 0)
- **The logic that scrubs home when the judge sends content out does not change** (the parallel P-005): `checkTarget`
  does not set `scanOpts.home`; on its branch P-005 makes the judge **always** scrub the OS user's home
  (`os.UserHomeDir()`), plus an absolute scan home, so once both are merged what `check --llm` sends out is scrubbed
  too. Whichever merges later adds an outbound assertion for `check --llm` (once for a directory and once for a
  `.zip`), so that the assertion itself says whether it is covered (written in the PR)
- The code of `internal/judge`, `internal/report` and `internal/model` is not touched, nor `go.mod` / `go.sum`
- No per-item summary like the Downloads one for `check`: one target is one `ScanResult.Judge`

## Must not claim

- Do not say "`check` in CI now uses the judge": it is an explicit opt-in, and without `--llm` the output with valid
  thresholds does not change by a single byte
- Do not say "`check` / `scan` without `--llm` is byte-for-byte unchanged under any arguments": after W7, with a
  misspelt threshold or a `--fail-on-llm` without the authority they exit 2 before collecting, with empty stdout;
  before, they scanned, printed the report and then exited 2 (same exit code). The config is also read earlier: when
  the config and `--root` / the target are both wrong, the config error is reported first
- Do not say "a `--fail-on-llm` without the authority has always been refused": before W7 (the released `scan` too),
  whenever deterministic findings reached `--fail-on` first (`check` defaults to `high`), the run exited 1 and the
  refusal never appeared; when it did appear, the judge's requests had already been sent and the report already printed
- Do not say "the judge can now make `check` fail": only through `--fail-on-llm`, and only with
  `llm.authority: escalate`; `--fail-on` never sees it
- Do not say "the gate now has the judge too" or "the gate and `check --llm` give the same answer": the gate
  corresponds to `check` without `--llm` (the deterministic half of the two is the same anyway; the judge only moves
  `overall_effective`)
- Do not say "benchmark samples that go through `check` are now judged too": the adapter has not changed, and the bare
  trees that do not fit into the fake home and are handed to `check` are still static
- Do not say "sending untrusted content to the judge is safe": redaction is best effort; a non-local endpoint still
  produces `LLM-002`; when reviewing someone else's PR in CI, `--llm` means the redacted excerpt of that content leaves
  the machine

## Work items

| W | One line | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | Four new tests, run red (the two binary tests red on `unknown flag`; the two reverse assertions already green before the change) | `cmd: tests — check has no --llm or --fail-on-llm, so a single target cannot reach the judge (P-004)` |
| 2 | `check` registers `--llm` / `--fail-on-llm`, passes `quiet`, goes through `failGate` according to `cfg.LLM.MayEscalate()`; the `gate.go` comment changes its reason | `cmd: check takes --llm and --fail-on-llm with scan's semantics, and the load-time gate still never asks (P-004)` |
| 3 | Spec §3 / §5.1 / §5.2 / §11 / §16.4 / §17 | `spec: check runs the judge only when asked with --llm, and the load-time gate never does (P-004)` |
| 4 | The three pairs `docs/llm-judge` / `docs/architecture` / `docs/install-gate`, `config.example.yaml` (`ROADMAP.md` need not change: this repository's ROADMAP has no "`check` never calls the judge" sentence) | `docs: the llm-judge, architecture and install-gate pairs stop saying check is always static (P-004)` |
| 5 | The package comments of `internal/judge/judge.go` and `internal/config/config.go`, `.claude/rules/judge.md`, `.claude/rules/gate.md` | `judge, config, rules: package docs and the judge and gate rules name check --llm and keep the gate static (P-004)` |
| 6 | `baselines/README.md`, two adapter comments, the help string of `-aguard-extra-args` | `baselines: the adapter still passes --llm to scan only, now for the reason that is still true (P-004)` |
| 7 | The RunE of `check` / `scan` validates the two thresholds and the authority with `validateFailGates` **before** collecting; `failGate` validates first, then decides; one sentence each in spec §3 and the `docs/llm-judge` pair. Test and fix in the same commit, the red is in "Done" | `cmd: a misspelt --fail-on-llm or a missing llm.authority is refused before anything is scanned or sent, and a deterministic hit no longer hides it behind exit 1 (P-004)` |
| 8 | `TestGateScannerNeverEnablesLLM` reads the content of each hook reply (`assertGateAudited`), not only its length | `cmd: tests — the gate-never-asks test reads each hook reply as an audit, so a GATE-000 that scanned nothing can no longer pass it with zero requests (P-004)` |
| 9 | This file, the index | `proposals: P-004 (P-004)` |

## Open questions

1. **What does `check --fail-on-llm` do without `--llm`?**
   **Recommendation**: exactly what `scan` does, the same `failGate` call: without the authority it is refused; with the
   authority and no LLM findings it degrades into a deterministic gate at the same level. No separate
   "`--fail-on-llm` needs `--llm`" error — that would make the same-named flag mean different things on `check` and
   `scan`, while the direction says "same semantics as scan".
   **Decided (2026-10-08)**: as recommended.
2. **What does `check`'s Short help string "(static only)" become?**
   **Recommendation**: `Pre-install gate: scan a single skill/dir/file (static; --llm adds the judge)`. A help string is
   not report output and is not within the scope of "byte-for-byte unchanged".
   **Decided (2026-10-08)**: as recommended.
3. **Should the README pair get a `check --llm` line?**
   **Recommendation**: no. The README's judge section never said `check` is static, and "off by default (needs both
   config and `--llm`)" still holds after this proposal; the "statically scan one skill/dir/file" at `README.md:174`
   describes the default without `--llm`; the full reference is `docs/llm-judge.md`, so write it there. Changing both
   READMEs together buys one duplicated line.
   **Decided (2026-10-08)**: as recommended.
4. **Should `TestGateScannerNeverEnablesLLM` first be refactored so the gate's `scanOpts` can be inspected directly?**
   **Recommendation**: no refactor. A counting endpoint plus `Judge == nil` is a **behavioural** observation: even if
   someone later turns on the judge in the gate a different way (say by reading the config's `JudgeReady()`), it turns
   red; inspecting a struct field only catches the one case of "writing `llm: true` into the literal".
   **Decided (2026-10-08)**: as recommended.
5. **Should baselines, in passing, also give `--llm` to the samples that go through `check`?**
   **Recommendation**: no. Every committed judge run was measured on the `scan` path, and the samples that go through
   `check` are static; changing it now means the next rerun measures something different, and it would quietly change
   a column of numbers already published. Measuring that is a measurement decision for another proposal.
   **Decided (2026-10-08)**: as recommended.

## Done

All the red evidence was measured in this repository: the W1 tests were applied on `main` at `dec64ca`; W7 is a single
"test + fix" commit, and its red was obtained by swapping that commit's `cmd/aguard/main.go` back to the W6 version,
leaving the tests as they are, and running the same set of tests (restored right after the swap, working tree clean).

```
Merged: PR #26 (2026-10-09; find the sha with git log --grep P-004)
Released: v0.19.0
Evidence: TestCheckCmd_LLMRunsTheJudge (cmd/aguard/check_llm_test.go); red at W1 (exit 2, error: unknown flag: --llm) → green after W2: directory and zip each judge.ran = true, 4 calls, an LLM-003, overall 83 = the static check's 83, overall_effective 58; with --quiet no "LLM judge" on stderr
Evidence: TestCheckCmd_FailOnLLMNeedsAuthority (same file); red at W1 (all three exit 2, unknown flag: --llm) → green after W2: escalate + --fail-on-llm high → exit 1; advisory + --fail-on-llm high → exit 2, stderr names llm.authority: escalate; reverse assertion --llm + default --fail-on high → exit 0 (the judge's high does not reach --fail-on)
Evidence: reverse assertion TestGateScannerNeverEnablesLLM (same file); green at W1 (before the change) and after the change, counting endpoint 0 requests, Judge == nil for Scan / ScanRoot. Mutation check: adding llm: true to the Scan of gateOptions in gate.go → red (Judge.Ran = true, Calls 3, endpoint 6 requests); adding llm: true to ScanRoot → red (endpoint 6); adding llm: true to approvePath → red (endpoint 3); all three reverted
Evidence: reverse assertion TestCheckCmd_ConfigAloneNeverCallsTheJudge (same file); green before and after the change: judge-ready config, without --llm → 0 requests, no judge key in the JSON, stderr empty
Evidence: W7 TestFailGateFlags_RefusedBeforeTheJudge (same file); with main.go swapped back to W6, red on 6/8 rows: check without authority (--llm) exit 1 and stderr has "LLM judge: 4 call(s)", check without authority (no --llm) exit 1, check --fail-on-llm hgih exit 1 and 4 calls, check --fail-on hgih exit 2 but endpoint 4 requests and stdout has the full JSON, scan without authority exit 1 and 4 calls, scan --fail-on-llm hgih exit 2 but endpoint 4 requests and stdout has JSON → after W7 8/8 green: the 6 refusal rows all exit 2, 0 requests, stdout empty; the two reverse rows (escalate + --llm --fail-on-llm high, check and scan) exit 1 before and after, with judge requests
Evidence: W7 TestFailGate_DeterministicHitDoesNotMaskARefusal (same file); with main.go swapped back to W6, red 2/2 (typo and no authority both return failExit, i.e. exit 1) → after W7 both return an ordinary error (exit 2). TestFailGate, TestFailGate_* (6) still green without a single character changed
Evidence: W8 TestGateScannerNeverEnablesLLM green on correct code after tightening. Weakness demonstration (temporary test, not committed, product code not changed): PreToolUse for a skill that is not installed, SessionStart with a config that cannot be parsed, both replies are GATE-000, both non-empty, endpoint 0 requests — the old assertion reply.Len() > 0 passes both; assertGateAudited is red on both ("the gate did not audit, so a zero request count proves nothing")
Evidence: reverse assertion check without --llm byte-for-byte unchanged — origin/main (dec64ca) and this branch each built into a binary with the same set of -ldflags, 4 targets (malicious skill / benign skill / single file / zip) × 8 outputs (text, --json, --md -, --sarif, --quiet, --fail-on low, --fail-on critical, judge-ready --config) = 32 cases, 0 differences in stdout / stderr / SARIF / exit code. Only normalised what already differs between two runs of the same binary: scanned_at; the zip target's temporary extraction directory name aguard-inbox-N and the SARIF fingerprint aguard/v1 derived from it (main's binary run twice on the same zip gives 6 fingerprints, all different; a pre-existing problem, not part of this proposal)
Evidence: the error path differs by design (see Must not claim): check <malicious skill> --fail-on hgih --json, main exit 2 + stdout 4610 bytes, this branch exit 2 + stdout 0 bytes, stderr identical
Evidence: reverse assertions still green without a single character changed — TestGateAgreesWithCheck, TestFailGate, TestFailGate_* (6), TestCheckTarget_* (8), TestMarkdownFlag_* (3), passthrough_test.go of baselines/adapter/aguard (not changed by a single character)
Evidence: port — the former repo's 8 code commits (module path already changed to AgentGuard, P number already changed to P-004) applied in order with git am -3 on main's dec64ca; the only conflict was the ROADMAP.md hunk of W4 (this repository's ROADMAP was rewritten and has no "check never calls the judge" sentence), that hunk dropped, the other 7 files applied as is; the other 7 commits had no conflicts and no manual edits
Evidence: Out of scope — git diff --stat origin/main -- internal/report internal/model internal/gate internal/score internal/collect internal/detect plugin README.md README.zh-CN.md ROADMAP.md .claude/rules/invariants.md go.mod go.sum baselines/adapter/aguard/passthrough_test.go baselines/results baselines/tools.yaml hack is empty; internal/judge/judge.go, internal/config/config.go, cmd/aguard/gate.go, baselines/adapter/aguard/aguard.go only have comment changes, baselines/cmd/baseline/main.go only the help string of -aguard-extra-args
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch); go.mod second line go 1.23.5, no new dependencies; cmd/aguard coverage 46.7% → 48.0%; scan on a real machine not applicable (collect / detect not touched)
```

To be added once both are merged (whichever merges later is responsible, see "Out of scope"): the outbound-path list of
invariant #1, spec §16.4's "there are only two outbound paths", `uploads_samples_basis` in `baselines/tools.yaml`, a
`check --llm` row in the positive control of `TestZeroDial_OnlyTheJudgeConnects` (P-003); an outbound assertion that
what `check --llm` (directory and `.zip`) sends out has home scrubbed (P-005, which makes the judge always scrub
`os.UserHomeDir()`, so `check --llm` is covered by it).
**The first four are done** (2026-10-09; P-003 merged first and this proposal later, so this proposal is responsible):
invariant #1's list and the zero table, spec §16.4 and §13 ④, and `uploads_samples_basis` in `baselines/tools.yaml`
all changed to three paths; `check --llm` added to the positive control of `TestZeroDial_OnlyTheJudgeConnects`, and
`check --llm` with `llm.enabled: false` added to the zero table. The two test rows were added first;
`TestZeroDial_ClaimsNameTheTest` immediately named the three docs that did not say it (red), and turned green once they
were filled in. The last one, the outbound assertion for the scrubbed home, is left to P-005: it merges after this
proposal.
