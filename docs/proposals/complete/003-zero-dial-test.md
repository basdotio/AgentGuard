<!-- SPDX-License-Identifier: MIT -->
# 003 — No test pins "never connects out", yet the baselines template says one does

- **Source**: new finding (2026-10-09) — invariant #1 (zero outbound connections by default) is the basis for trusting
  the tool, but no test pins it; the baselines description text claims tests guarantee it. Ported from P-044 in the
  former private repository agent-guard
- **Depends on**: none
- **Branch**: `p/003-zero-dial-test`

<!-- No "Status" line: the directory the file is in is the status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Invariant #1 (`.claude/rules/invariants.md:7`) says "never execute scanned content, never connect out (except the
explicitly enabled LLM judge)". The README, the summary table in `docs/architecture.md` and several comments in
`cmd/aguard` all restate it. **But no test in the whole repository has ever asserted it**:

| Place | What it says now | What actually exists |
|---|---|---|
| `baselines/tools.yaml:33-37` (aguard's `uploads_samples_basis`) | "invariant #1, **enforced by tests in this repository**: aguard never connects out except for the explicitly opted-in LLM judge" | `grep -rn 'RoundTrip\|DefaultTransport' --include='*_test.go'` only hits the name of the judge's own unit test (`TestHTTPClient_RoundTripAndRedaction`) and a YAML round-trip test in baselines; the judge's `httptest` unit tests test "what the judge sent", not "what the other commands did not send" |
| `baselines/cmd/baseline/main.go:397-413` `uploadsFor` | Without `--llm`, writes the sentence above verbatim into every run.yaml | The committed `baselines/results/aguard/2026-09-24/run.yaml:10` and `2026-09-29e/run.yaml:10` both carry this sentence |
| `cmd/aguard/llm.go:112`, the comment on `runLLMSetup` | "It never prints the key and never sends anything" | No test |
| `cmd/aguard/main.go:807`, the comment on the `version` command | "Offline by construction … never a release feed" | No test |

Today the sentence **happens to be true**: the only `http.Client` in the product is in `NewHTTP` at
`internal/judge/openai.go:52-65` (pass `nil` and it creates a new `&http.Client{}`), and both call sites,
`cmd/aguard/main.go:273` (`runJudge`) and `cmd/aguard/llm.go:220` (`runLLMTest`), pass `nil`; the reputation allowlist
is `go:embed`, `gate` and `collect` have no network code, and `hack/reputation-refresh` is a different binary.
But "happens to be true" and "pinned by a test" are two different things:

- **Nothing turns red for the next person who adds a network call.** A casual "verify the key during setup", "check
  for a new version in `version`", "`check` runs the judge too" (P-004 does exactly the last one) would each add a new
  outbound path and stay all green.
- **The file others cite says something on our behalf without evidence.** run.yaml is the file others read when they
  cite the measurements (the wording of the `uploadsFor` comment); it says "enforced by tests", and those tests do not
  exist. This repository's disclosure discipline is "a green light is not evidence"; here there is not even a green
  light.
- **The invariant itself does not state its boundary.** "Except the explicitly enabled LLM judge" does not say which
  commands. `scan --llm` connects, `llm test` connects too (one Ping), `check`, `hook`, `approve` do not — this list
  exists only in the head of whoever has read the code.

## Initial direction

`internal/judge` adds a package-level test seam `var Transport http.RoundTripper` (nil = `http.DefaultTransport`, as
today), and `NewHTTP` uses it when the caller gives no client. `cmd/aguard` adds an in-process test: with the judge
**enabled** in the config, replace the seam with a RoundTripper that only counts and returns an error, run the command
entry points one by one, and assert zero round trips; the same counter must see ≥ 1 on `scan --llm`, proving it is not
blind.

Invariant #1 is rewritten as an enumerated list of "the only paths that may connect out today", and states which test
pins it; the baselines sentence is changed to name this test. Judge behaviour does not change, no network path is
added, and the committed `baselines/results/` are not touched.

## Done criteria

- [x] `TestZeroDial_OnlyTheJudgeConnects` (`cmd/aguard/zero_dial_test.go`, new): with the judge **enabled** in the config
  (`writeJudgeConfig` pointing at `http://127.0.0.1:9`, `max_retries: 0`), `judge.Transport` and `http.DefaultTransport`
  are each replaced with a RoundTripper that only counts and returns an error; after each entry point below has run
  **both counters are 0**, and the entry point itself did not fail (zero from an entry point that exits early on an error
  means nothing):
  `scanEnv` (without llm; `clean` uses it too) and the `gateLivenessNote` that `scan` appends after scoring (the
  fixture uses the real `gate.PlanInstall` to register a gate pointing at a deleted binary, and it must return
  `GATE-001`), `scanInbox` (without llm), `scanEnv` + `scanInbox` with llm but `llm.enabled: false`, `checkTarget` (with
  the same opts as the `check` command), `runHook` fed `PreToolUse` (the reply must be `ask`, proving it really scanned)
  and `SessionStart` (no "could not audit" in the reply), the `PostToolUse` rescan (through `gateOptions` +
  `gate.Handle` sharing one in-memory store, the reply must be "risk accepted"; for why it does not go through
  `runHook` see open question 6), `approvePath`, `listApprovals` (this row first writes an approval into the store
  itself and the output must list it — it does not rely on the `approve` row running first, and holds when `-run`
  selects this row alone), `runLLMSetup`, `runLLMStatus`, `runVersion` (see the next item), `collect.CollectTarget`
  (`hash`). Today `judge.Transport` does not exist, so it is red at compile time
- [x] **`version` runs the whole command body, one row per branch**: the function body of the `version` command is
  moved verbatim into `runVersion(w, root)`, and the zero table runs one row for **each exit** of `pluginVersionLine`
  and of the `versionLine` it delegates to — for a root with `aguard` 9.9.9 installed, release builds `v1.0.0` /
  `v9.9.9` / `v10.0.0` produce the newer / matches / older comparison lines, and `dev` produces "dev build; not
  compared"; a root with no plugin installed and one whose `plugin.json` carries no version take the two exits that
  return empty; a root with only the old name `agentguard` installed takes the rename hint, and a root with both the new
  and the old name installed takes the "old plugin still present" suffix (these two exits were added with the v0.17
  rename). One more row **does not go through `-ldflags`** and stamps the version from build info (`applyBuildInfo`,
  the path a binary installed with `go install` takes, since v0.18): the build line must print the version and commit
  from the build info, and the plugin line still compares. Every row asserts that the build line is present and that
  the plugin line is the sentence this branch should print (or is absent)
- [x] **Reverse assertion (positive control, so the test is not blind)**: in the same test, **before** the zero table,
  the same pair of counters must see judge counter ≥ 1 and default counter = 0 on the three allowed paths:
  `scanEnv(llm: true)`, `scanInbox(llm: true)`, `runLLMTest`. The former proves the seam is really attached to the
  judge's client, the latter proves the judge's requests do not bypass the seam
- [x] **Both counters must already be empty at the start of every row**, and after the zero table it waits 50 ms
  (`lateRequestSettle`) and collects once more: a request that lands after its entry point returned is reported as
  "landed after some row returned", rather than swallowed by the next row's reset at its start or left unseen after the
  counters are restored. It only guarantees "it is reported", **not that it is attributed to the row that sent it**
  (see Must not claim)
- [x] `TestNewHTTP_TransportSeam` (`internal/judge/run_test.go`, new): with no seam set, the client built by
  `NewHTTP(…, nil)` has `Transport == nil` (that is, `http.DefaultTransport`, the same as today's `&http.Client{}`);
  with the seam set, requests go through it; **reverse assertion**: a client the caller supplies (the existing tests all
  supply `srv.Client()`) is used as is, the seam does not override it
- [x] `TestZeroDial_ClaimsNameTheTest` (`cmd/aguard/zero_dial_test.go`, new): uses `runtime.FuncForPC` to get the
  **real name** of the test above, and asserts that aguard's `uploads_samples_basis` in `baselines/tools.yaml` and
  `.claude/rules/invariants.md` both contain it. Renaming the test without changing these two places → red; today
  tools.yaml only says "enforced by tests in this repository" → red.
  Plus **set equality**: every path in the positive control (`zeroDialControl`) must be one `` - `path`: `` entry in
  invariant #1's list, and every entry in the list must have the positive control see it connect out (both directions);
  every path must also appear in that sentence in `baselines/tools.yaml` and in the outbound-list line of spec §16.4
- [x] `TestZeroDial_NoClientOutsideTheJudge` (`cmd/aguard/zero_dial_source_test.go`, new): `go/parser` reads every
  non-test `.go` under `cmd/` and `internal/`; outside `internal/judge` no `net/http` `Client`/`Transport` type may
  appear (a literal, `new()`, a variable declaration, and a type assertion on the default transport + `Clone` all go
  through this name), and `judge.Transport` may not be assigned, have its address taken or carry an initial value in a
  non-test file; importing a package of this module outside `cmd/` and `internal/` is red too (the walk would no longer
  cover enough). `internal/judge` is **not exempted as a whole package**: no `http.Transport` may appear inside the
  package; `http.Client` may only be the type of the `http.Client{Transport: Transport}` literal in `NewHTTP` (all keys
  named, the value of `Transport` is exactly the seam identifier), or the type of a `*http.Client` field / parameter /
  return value (a declaration, which builds no client).
  **Reverse assertion**: the number of files read is > 0, the declaration of `var Transport` must be found in
  `internal/judge`, and there is **exactly one** literal of the `NewHTTP` kind — otherwise this check guards nothing
- [x] Reverse assertion: `TestHTTPClient_RoundTripAndRedaction`, `TestHTTPClient_CountsTokens`,
  `TestRun_RetriesOnlyRetryableErrors`, `TestLLMCommands_SetupTestStatus`, `TestE2E_*` (seven; the ones that enable the
  judge hit `httptest` through `NewHTTP(…, nil)`), `TestAJudgeRunDisclosesTheUpload`, `TestPluginVersionLine`,
  `TestPluginVersionLine_LegacyName`, `TestApplyBuildInfo` stay green **without a single character changed** — the
  judge's behaviour, `uploadsFor`'s behaviour, and `version`'s comparison and version stamping have not changed
- [x] `make verify` green; the second line of `go.mod` is still `go 1.23.5`, and `go version` shows no toolchain switch

## Out of scope

- **No network path is added**: the only extra use of `net/http` in product code is reading one package-level
  variable; the entry points that connect out are still the same two.
  The other product-code change is moving the `version` command's function body verbatim into `runVersion(w, root)`
  (in `main.go` a closure becomes a one-line call, `version.go` gains one function); the output is byte-for-byte
  unchanged (evidence in "Done")
- **Judge behaviour does not change**: request content, retries, timeouts, `CheckEndpoint`, `Usage()` are untouched;
  the signature of `NewHTTP` is untouched, and the client a caller gets by passing `nil` is field-for-field identical
  to today's when the seam is nil
- **`version`'s behaviour does not change**: `pluginVersionLine` / `versionLine` / the hint functions and
  `applyBuildInfo` are not changed by a single character
- **No redirect handling** (`CheckRedirect` / a cross-host redirect carrying the Bearer header away): this proposal
  touches no field of `http.Client` except `Transport`
- **No CI network-isolation job**: that is the layer that can see raw sockets and child processes; this proposal only
  pins the layer that goes through `judge.Transport` / `http.DefaultTransport`, plus a source check that closes off
  clients with their own transport
- **No static import allowlist** (see open question 2). `TestZeroDial_NoClientOutsideTheJudge` is not one: it does not
  care who imports `net/http`, `net`, `os/exec`, only whether anyone builds a `Client`/`Transport` outside the seam and
  whether anyone touches the seam in production code — that is the counters' own blind spot; it widens the view of this
  proposal's test, not the "never execute" half
- **No `check --llm`**: P-004 is doing it in parallel; once both are merged, whichever merges later changes five places
  (see open question 3)
- **No fix for the gate's pending being lost across processes**: P-007 is doing it in parallel (see open question 6)
- **The committed `baselines/results/` do not change**: the sentence in the two run.yaml files of 2026-09-24 and
  2026-09-29e is a record written at the time
- **The Go code of `baselines/cmd/baseline` does not change**: `uploadsFor` forwards the registry's sentence verbatim;
  what changes is that sentence itself in `tools.yaml`
- The invariant summary tables in the README / `docs/architecture*.md` do not change (see open question 5)
- No dependencies added, `go.mod` / `go.sum` not touched

## Must not claim

- **Do not say "zero egress proven" / "verified zero egress"**. **Do not say "every request in the process that goes
  through `net/http` is visible" either**: the counters only see requests that go through `judge.Transport` or
  `http.DefaultTransport`. Not visible: a client with its own `http.Transport` (the source check covers this for this
  module's product code, `internal/judge` included; `hack/` and `baselines/` are other binaries, outside what it
  reads), **a client of this kind that a dependency builds in its own code** (the source check reads only this module;
  today no other module in the binary imports `net/http` or `os/exec`, and `pflag` imports `net` only for IP-typed
  flags — a conclusion from reading the code; read a fourth direct dependency before adding it), a raw `net.Dial`,
  `exec` of a `curl` (today product code has neither — zero imports of `os/exec`, `net` only used for `ParseIP` — but
  that is a conclusion from reading the code, not from this test), an asynchronous request sent more than 50 ms after
  the last row returns, code written only in a cobra `RunE` closure, **package-level `init()`** (it has finished before
  the test installs the counters; `applyBuildInfo` runs once more in `version`'s build-info row, other things in
  `init()` are out of view)
- **Do not say "the client in the judge package is watched by the positive control"**: the positive control only
  watches the client its own three paths use. What can be said is only: inside the judge package **only the one client
  in `NewHTTP`** is allowed, and its transport is the seam. For the `Transport` value of that literal in `NewHTTP`, the
  source check only recognises the identifier `Transport` and does not follow what it resolves to: shadowing the
  package-level seam inside `NewHTTP` with `var Transport = …` or a parameter of the same name is invisible to the
  source check (`Transport := …` is treated as an assignment to the seam and is red). The shadowing value either spells
  out `http.Transport` (source check red), or is `http.DefaultTransport` (positive control red), or comes from a
  dependency or a RoundTripper that dials itself — that is, the two blind spots above
- **Do not say that a single row selected with `-run` "proves zero"**: each of the 26 rows (3 positive control + 23 zero
  table) passes when selected alone, but when selected alone the positive control does not run, and that row's zero does
  not prove the counters are attached to the judge's client. The evidence is the whole table run together
- **Do not say the zero table runs "the commands"**: each row calls the function the command calls (`scanEnv`,
  `checkTarget`, `runHook`, `runVersion`…), not cobra's `RunE` closure; a request added in the closure outside that
  function is invisible to this test. `version` is the only one whose entire closure body was moved into a function;
  the closures of the other commands still hold rendering, `failGate` and similar code this proposal does not run
- **Do not say every line of `version` has run**: one row per exit, but inside the few hint functions that only build
  strings (`updateHint`, `renameHint`, `leftoverHint`, `switchCommand`) there are further branches by install channel
  (desktop / CLI) and marketplace, and the zero table only reaches the one the fixture uses (CLI install, this project's
  marketplace)
- **Do not say an asynchronous request is attributed to the row that sent it**: a late request is reported as "landed
  after X returned"; one that lands while a later row runs is attributed to that row — it is red, but the row name may
  be wrong
- **Do not say "every command is tested"**: `hook install/uninstall/status`, `approvals forget`,
  `clean --apply/--undo/--ask` are not in the zero table; they only touch local files, but this test has not run them.
  The list in the invariant states **the ones that were run**
- **Do not say new commands are caught automatically**: an entry point already in the zero table turns red as soon as
  it connects out; a **newly added** command has to be added to the table by hand, the test will not discover it for you
- Do not say the two committed run.yaml files "were right at the time": when that sentence was written the test did not
  exist; only now does it become true

## Work items

| W | One line | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | Three new tests, run red (compile red: `judge.Transport` does not exist; tools.yaml names no test) | `judge, cmd: tests — nothing counts what the commands send, and the baselines claim names no test (P-003)` |
| 2 | The `judge.Transport` seam; `NewHTTP` uses it when the client is nil | `judge: a nil client takes its transport from a test seam, so a test can count every request (P-003)` |
| 3 | Invariant #1 rewritten as an enumerated list + names the test; spec §16.4 and §13 synced | `rules, spec: invariant #1 lists the only two paths that may connect out and names the test that pins it (P-003)` |
| 4 | The `baselines/tools.yaml` sentence names the test (and, in passing, drops the trailing `(P-021)` that pointed at a former-repo number) | `baselines: the registry's upload claim names the test that enforces it (P-003)` |
| 5 | The zero table also covers the gate-liveness probe `scan` appends after scoring; the invariant list synced | `cmd, rules: the zero table also covers the gate-liveness probe scan appends after scoring (P-003)` |
| 6 | `version`'s function body extracted into `runVersion`; the zero-table row runs the whole command body, against an installed plugin, as a release build | `cmd: the version row runs the command's whole body against an installed plugin, so a request from either half of it is seen (P-003)` |
| 7 | Invariant #1 and spec §16.4 changed to "only two transports are visible", listing the blind spots one by one; a source check closes off clients with their own transport in product code | `rules, spec, cmd: invariant #1 says the counters see two transports and lists what they miss; a source check closes the own-transport gap for product code (P-003)` |
| 8 | Each row asserts at its start that the counters are empty; at the end of the table it waits 50 ms and collects once more | `cmd, rules, spec: a request that lands after its row returned is reported, not discarded by the next row's reset or lost when the counters are put back (P-003)` |
| 9 | The positive control and invariant #1's outbound list are one set; tools.yaml and spec §16.4 must also spell out every path | `cmd, rules: the positive control and invariant #1's list of outbound paths are one set, so check --llm cannot join the control without joining the list (P-003)` |
| 10 | The gate-liveness probe row must find the fixture's dead registration; the `approvals` row must list the approved skill | `cmd: the gate-liveness row must find the fixture's dead registration and the approvals row must list the approved skill, so neither zero is about nothing (P-003)` |
| 11 | The source check no longer exempts `internal/judge` as a whole package: inside it only `NewHTTP`'s one seam client is allowed; file header, invariant #1, spec §16.4/§13 synced, and "a client a dependency builds itself" listed as a blind spot | `cmd, rules, spec: inside internal/judge only NewHTTP's seam client may be built, so a second judge client with a transport of its own is red instead of dialling unseen (P-003)` |
| 12 | The `approvals` row writes the approval it lists itself, instead of relying on the `approve` row running first | `cmd: the approvals row seeds the approval it lists, so it passes when -run selects it alone instead of depending on the approve row having run (P-003)` |
| 13 | `version` runs one row per exit of `pluginVersionLine` / `versionLine` (including the two rename exits added in v0.17), plus one row stamped from build info; `init()` added to the blind spots of invariant #1 and the spec | `cmd, rules, spec: the version command runs once per return of pluginVersionLine and versionLine and once stamped from build info, so a request added to any branch is seen, not only the newer one (P-003)` |
| 14 | This file's "Done", the index | `proposals: P-003 (P-003)` |

## Open questions

1. **Does the seam replace only `judge.Transport`, or `http.DefaultTransport` as well?**
   **Recommendation**: replace both, each with its own counter. `judge.Transport` is the chosen seam, and the positive
   control uses it to prove "the judge's requests go through the seam"; `http.DefaultTransport` is the backstop: any
   newly added `http.Get` / `http.DefaultClient` anywhere outside the judge goes through it and is counted on the
   zero-table entry points. Replacing only the latter would in fact also test today's judge (a nil Transport is
   DefaultTransport), but if the judge's client one day gets a Transport of its own, the test would quietly go from
   "can count" to "cannot count" — the positive control would catch it, but the seam means it does not have to rely on
   this coincidence.
   **Decided (2026-10-08)**: as recommended.
2. **Should a static import allowlist test be added in passing (`go/parser` scanning non-test `.go`: `net/http` only in `internal/judge`, `net` only for `ParseIP` and the like, `os/exec` zero)?**
   **Recommendation**: not in this proposal. It should be designed together with the other half of invariant #1
   ("never execute", `os/exec`), and CI network isolation is the layer that can see raw sockets and child processes;
   this proposal only pins requests that go through the two transports, and states this boundary in "Must not claim".
   **Decided (2026-10-08)**: as recommended. `TestZeroDial_NoClientOutsideTheJudge` is not the import allowlist asked
   about here, see "Out of scope".
3. **Does the enumerated list include P-004's `check --llm`?**
   **Recommendation**: no. P-004 is on a parallel branch and does not exist in this branch's code; the list states
   today's facts. Once both are merged, whichever merges later adds a line and moves `check --llm` from the zero table
   to the positive control (explained in the PR). The five places the later one must change are written down here so
   they do not live only on the PR —
   ① add a `` - `check --llm`: `` entry to invariant #1's outbound list (together with the count in "only two paths");
   ② the outbound-list line of spec §16.4; ③ the "(scan --llm, llm test)" in the `baselines/tools.yaml` sentence;
   ④ the comment on the `check` row of the zero table ("the opts `check` passes" — after P-004 `check` passes
   `llm: useLLM`, so that zero-table row is either renamed to "`check` without `--llm`" or its opts change);
   ⑤ add a `check --llm` positive-control row to `zeroDialControl` (`checkTarget` with `llm: true`). ①②③⑤ are forced
   to change together by the set equality in `TestZeroDial_ClaimsNameTheTest`; ④ can only be caught by a human reader.
   **Decided (2026-10-08)**: as recommended.
4. **Should "enforced by tests in this repository" in the two committed run.yaml files be changed?**
   **Recommendation**: no. Results are measurement records, frozen once written; change the registry sentence, and
   later runs carry the version that names the test as a matter of course.
   **Decided (2026-10-08)**: as recommended.
5. **Should the enumerated list also go into the invariant summary tables in the README / `docs/architecture*.md`?**
   **Recommendation**: no. The summary tables' "except the explicitly enabled LLM judge" is still true, and they
   themselves say "full text in `.claude/rules/invariants.md`"; copying the list into two bilingual pairs of docs adds
   four more copies that each drift on their own — `TestZeroDial_ClaimsNameTheTest` only looks at `invariants.md`,
   `tools.yaml` and the outbound-list line of spec §16.4.
   **Decided (2026-10-08)**: as recommended.
6. **(Found during implementation, outside this proposal's scope) The consent given at the gate's prompt is never recorded.**
   `gate.LoadStore` (`internal/gate/approvals.go:96-125`) only reads `approvals` back, **not `pending`**; and every
   hook event is a new process (`runHook` calls `LoadStore` every time). So the pending that `PreToolUse` writes to
   disk when it decides `ask` is always empty in the `PostToolUse` process, `handlePost` returns right at
   `pendingFor`, and the rescan and "risk accepted" path is never reached in real runs.
   **Recommendation**: do not fix it in this proposal — it changes the gate's behaviour and has nothing to do with
   connecting out. P-007 is the one that fixes it. This proposal's zero table therefore drives the `PostToolUse` rescan
   through `gate.Handle`, the only half of it that could connect out; once P-007 is merged it can go back to `runHook`.
   **Decided (2026-10-08)**: as recommended.

Added during the port:

7. **How do the `version` rows map onto this repository's v0.17 / v0.18 `version`?** In the former repo
   `pluginVersionLine` was one function with six exits; this repository's v0.17 renamed the plugin from `agentguard` to
   `aguard`, `pluginVersionLine` gained two exits, "only the old name installed" and "both new and old installed", and
   the comparison moved into `versionLine`; since v0.18 a binary not stamped by `-ldflags` takes its version from build
   info (`applyBuildInfo`, run in `init()`).
   **Recommendation**: the zero table runs one row per **exit** of the two functions (eight rows); the plugin uses this
   repository's name and marketplace (`aguard@AgentGuard`, old name `agentguard@AgentGuard`), each root built with the
   existing `writePluginInstalls`; plus one row stamped from build info, so `applyBuildInfo` runs once under the
   counters; `init()` itself is recorded as a blind spot. `runVersion` is only a move and does not touch the stamping
   logic — it still reads the same three package-level variables.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #22 (2026-10-09; find the sha with git log --grep P-003)
Released: pending release
Evidence: TestZeroDial_OnlyTheJudgeConnects (cmd/aguard/zero_dial_test.go); compile red at W1 (cmd/aguard/zero_dial_test.go:85: undefined: judge.Transport), green after W2: the three positive-control rows scan --llm / Downloads item / llm test are all seen by the judge counter, default counter 0; the 23 zero-table rows (14 entry points + 9 version rows) have both counters at 0, and every entry point ran to success (Pre replies ask, the Post rescan replies risk accepted, SessionStart has an audit result, the gate-liveness probe reports GATE-001, approvals lists the entry it wrote itself)
Evidence: TestNewHTTP_TransportSeam (internal/judge/run_test.go); compile red at W1 (run_test.go:306: undefined: Transport), green after W2; reverse assertion: the srv.Client() the caller supplies still hits its own server, the seam count is unchanged
Evidence: TestZeroDial_ClaimsNameTheTest; after W2 both places red ("baselines/tools.yaml says aguard's no-upload claim is enforced by tests, but does not name TestZeroDial_OnlyTheJudgeConnects" + "invariant #1 does not name the test that pins it") → green after W3, W4; set equality (W9): add a check --llm row to the positive control, docs unchanged → 3 red (invariant #1 does not list it, tools.yaml does not state it, spec §16.4 does not state it); add a check --llm entry to invariant #1 with no positive control → 1 red; delete llm test from invariant #1 → 1 red
Evidence: mutation (not committed, reverted right after the run, working tree git status empty) — force o.llm on in checkTarget → five rows red: Downloads item (without --llm) / check / hook Pre / Post rescan / approve (judge counter 3 / 4 / 3 / 6 / 4); revert NewHTTP to &http.Client{} → the three positive-control rows red (judge counter 0, default counter 13 / 3 / 1) and the source check red with 2 (that literal is not the seam shape; 0 seam literals), TestNewHTTP_TransportSeam red as well
Evidence: (W6) the version row runs the whole command body; at W5, adding http.Get(127.0.0.1:9) in version's cobra closure → all green (it only called pluginVersionLine, could not see it); after W6 the same request placed in runVersion → red: "version sent 1 request(s) through http.DefaultTransport to [127.0.0.1:9]"; placed outside runVersion, in the closure → still green (recorded in Must not claim)
Evidence: (W6) output byte-for-byte unchanged — W5 and W6 each built once with the same set of -ldflags (-X main.version=v1.0.0 -X main.commit=abc1234 -X main.date=2026-10-09) and once without -ldflags (-buildvcs=false, takes the build info, falls back to dev); version --root on four roots, cmp identical for all: a temporary root with aguard 9.9.9 installed (237 B / 2 lines, containing "plugin aguard 9.9.9 is newer than this binary (1.0.0)"), a temporary root with only the old name agentguard 0.16.0 installed (312 B / 2 lines, rename hint), this machine's ~/.claude (312 B / 2 lines), a nonexistent root (74 B / 1 line); the unstamped builds 226 / 303 / 303 / 65 B, likewise byte-for-byte identical
Evidence: (W7, W11) TestZeroDial_NoClientOutsideTheJudge (cmd/aguard/zero_dial_source_test.go); (&http.Client{Transport: &http.Transport{}}).Get(127.0.0.1:9) in runLLMStatus → TestZeroDial_OnlyTheJudgeConnects still green (blind spot measured), source check red with 2 (the http.Client and http.Transport at llm.go:233); another client with its own transport built inside internal/judge and called from checkTarget: at W10 (judge package exempted as a whole) all three TestZeroDial_* green → after W11 the source check red with 2 (the http.Client and http.Transport at zz_isolated.go:8); var Transport = http.DefaultTransport in NewHTTP shadowing the seam → source check green, the three positive-control rows red (judge counter 0, default counter 13 / 3 / 1), recorded in Must not claim
Evidence: (W8) runLLMStatus starts a goroutine that calls http.Get after 20 ms: at W7 a single run is 3/3 green (missed), with -count=5 it is attributed to the next round's "scan --llm" (wrong row); after W8 red on every run with -count=5: "a request landed after "hash" returned, before the counters are put back"
Evidence: (W10, W12) the gate-liveness probe row: the fixture registers no gate → red ("want the fixture's dead registration reported as GATE-001, got []"); gateLivenessNote returns nil directly → red; the approvals row: listApprovals prints nothing when there are records → red; at W11 the approvals row selected alone with -run is red ("approvals did not list the skill approved in the row above, so it read nothing: \"\"") → green after W12
Evidence: (W13) nine version rows; an http.Get(127.0.0.1:9) inserted before each of the eight exits of pluginVersionLine / versionLine, plus one in applyBuildInfo, one place at a time: at W12 (one version row) only the newer one is red, the other eight — matches / older / dev / no plugin installed / no version / only the old name installed / both installed / applyBuildInfo — all green; after W13 all nine red, each turning its own row red (the newer one also turns the "both installed" and "build info" rows red — they take the newer comparison anyway), the other eight turn only their own row red
Evidence: each of the 26 rows run alone with -run: 26/26 green, exactly 1 row run each time; go test -race -count=5 -run TestZeroDial green
Evidence: reverse assertions not changed by a single character — git diff origin/main -- cmd/aguard/e2e_test.go cmd/aguard/main_test.go cmd/aguard/buildinfo_test.go internal/judge/judge_test.go baselines/cmd/baseline/passthrough_test.go is empty; internal/judge/run_test.go has only added lines (+47 −0); TestHTTPClient_RoundTripAndRedaction, TestHTTPClient_CountsTokens, TestRun_RetriesOnlyRetryableErrors, TestLLMCommands_SetupTestStatus, the seven TestE2E_*, TestPluginVersionLine, TestPluginVersionLine_LegacyName, TestApplyBuildInfo, TestAJudgeRunDisclosesTheUpload still green
Evidence: Out of scope — git diff --stat origin/main -- baselines/results baselines/cmd internal/gate internal/collect internal/detect cmd/aguard/buildinfo.go README.md README.zh-CN.md docs/architecture.md docs/architecture.zh-CN.md go.mod go.sum is empty; the product-code changes are internal/judge/openai.go (+11 −1: one package-level variable and its comment, one field in NewHTTP), cmd/aguard/main.go (+1 −9) and version.go (+18 −0): the version command body moved verbatim into runVersion
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod second line go 1.23.5; collect / detect not changed, no scan on a real machine needed
```
