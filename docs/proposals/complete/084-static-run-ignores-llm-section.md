<!-- SPDX-License-Identifier: MIT -->
# 084 — A judge setting this build does not accept stops every static run, and turns the load-time gate off

- **Source**: open point of P-075 (PR #95), 2026-10-21: the config package says "Missing/invalid config degrades to
  static-only", yet a static `check` exits 2 on an unknown `llm.provider`
- **Depends on**: none
- **Branch**: `p/084-static-run-ignores-llm-section`

## Problem

`config.Load` validates the whole file in one pass and returns no config at all when any of it fails. Three of those
checks are about the judge only: `llm.timeout` / `llm.total_timeout` must parse, `llm.provider` must be a preset or
`openai_compatible`, `llm.authority` must be `advisory` or `escalate`. A static run never reads any of these keys, yet it
dies on them. Measured on `origin/main` (e18e255) with a config holding only `llm: {provider: not-a-provider}`:

| Entry point | Exit | What the user gets |
|---|---|---|
| `scan --root … --inbox off` | 2 | `error: config llm.provider "not-a-provider": want openai_compatible, or one of …` |
| `check <skill>` | 2 | the same |
| `approve <skill>` | 2 | the same |
| `hook` `PreToolUse` (a skill load) | 0 | `AgentGuard [GATE-000] config error, this call was NOT audited: …` |
| `hook` `SessionStart` | 0 | the same `GATE-000` |
| `version`, `hash` | 0 | unaffected |

The last two rows are the serious ones. The load-time gate never consults the judge (`gateOptions`, "None of these
scanOpts … may set llm"), but it loads the same config, and a load error makes it fail open: every skill load passes
unaudited, and the only trace is a system message per load. A config that one build accepts and another rejects is
ordinary — a provider name added in a newer build and then a downgrade, a hand-edited typo in a timeout, or two builds on
one machine sharing `~/.config/bas-guard` — and in each case the gate quietly stops auditing.

## Initial direction

`config.Load` keeps refusing what every run reads (unreadable or unparsable YAML, an invalid `gate` section). An `llm`
section that fails its own validation is set aside instead: `cfg.LLM` reverts to the defaults (judge off) and keeps the
reason, `LLMConfig.Problem()`. Every path that needs the judge asks for it first and refuses with exactly today's
message; static runs and the gate go ahead, and a static CLI run that is not `--quiet` says once on stderr that the
section was ignored.

## Done criteria

- [x] `TestLoad_InvalidLLMSectionIsSetAside` (`internal/config/config_test.go`): for an unknown provider, an unknown
  authority, an unparsable `timeout` and an unparsable `total_timeout`, `Load` returns no error, `Problem()` carries
  the message `Load` used to return, `cfg.LLM` is the default section with the judge off, and the file's `gate` section
  (`action: deny`, `fail_on: medium`) is kept; `GateReady()` returns the same problem
- [x] Reverse (`TestLoad_StillRefusesWhatEveryRunReads`): an invalid `gate.action`, an invalid `gate.fail_on` and
  unparsable YAML are still `Load` errors; a valid `llm` section has `Problem() == nil`
- [x] `TestInvalidLLMSection_StaticRunsGoAhead` (`cmd/aguard/llm_section_test.go`, built binary): with `--config`
  naming the invalid file, `check <skill> --json` and `scan --root … --inbox off --json` exit as they do without it and
  print the same report (the `scanned_at` reading masked); stderr carries exactly one line naming the provider and saying
  the run is static; with `--quiet` it carries none
- [x] `TestInvalidLLMSection_JudgePathsStillRefuse` (same file): `check --llm`, `scan --llm`, `check --fail-on-llm high`,
  `llm test`, `llm status` and `llm preview <skill>` each exit 2 with the provider message, as on `origin/main`
- [x] `TestInvalidLLMSection_GateStillAudits` (same file, in-process `runHook`): with the invalid config, a credential
  exfiltration skill's `PreToolUse` is answered `ask` naming `EXFIL-`, not a `GATE-000` config error; an invalid
  `gate.action` still gives the `GATE-000` config error (the gate's own section is not set aside)
- [x] P-075's and P-077's regression tests poison the home with an invalid `gate.action` instead of an invalid provider,
  which still fails every run; their positive controls (`config.Load` refuses the planted file) stay
- [x] `make verify` green under `go version go1.23.5` with no toolchain switch; `head -3 go.mod` line 2 `go 1.23.5`

## Out of scope

- The `gate` section: an unknown `gate.action` or `gate.fail_on` decides whether a load is interrupted or refused, so it
  stays a load error and the gate's `GATE-000` for it stays as it is
- Unparsable YAML, including a wrong type inside the `llm` section (`samples: many`): yaml.v3 fails the whole document,
  and a file that cannot be read cannot be trusted for its `gate` section either
- What the gate says about a set-aside `llm` section: nothing — it never reads the section (open question 3)
- A report field or note for the ignored section: the JSON and HTML reports are unchanged (open question 2)
- The `llm setup` command: it rewrites the section and never validated the old one

## Must not claim

- Not "an invalid config is ignored": only the `llm` section's own validation is deferred, and only for runs that do not
  use the judge. Every judge path refuses exactly as before
- Not "the judge falls back to defaults": a set-aside section is never used, not even its valid keys. The defaults
  stand in only so a static run reads no half-validated value

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-084: proposal — a judge setting this build rejects stops static runs and turns the gate off` |
| 2 | `config.Load` sets an invalid `llm` section aside with its reason; the judge paths refuse with it; static runs warn once; tests | `config: an invalid llm section is set aside, static runs and the gate go ahead (P-084)` |
| 3 | P-075 and P-077 regression tests poison the home through the `gate` section | `test: the home-isolation tests poison the gate section, which every run reads (P-084)` |
| 4 | spec §11, the llm-judge pair, the gate rule | `docs: an invalid llm section only stops the judge (P-084)` |
| 5 | This file to `complete/`, Done filled, the index row | `P-084: done — evidence and the index row` |

## Open questions

1. **Set the section aside, or ignore only the failing key?** **Recommendation**: the whole section, reverted to the
   defaults. A key-by-key fallback would run the judge on a configuration the user never wrote (the localhost default
   where they meant a hosted provider) — the silent fallback the spec already rules out for provider names. Static runs
   do not care which; judge runs refuse either way.
   **Decided (2026-10-21)**: as recommended.
2. **How does a static run say it?** **Recommendation**: one stderr line per command, not under `--quiet`, and no
   report note. The section is not part of what was scanned, so a note would change reports — and SARIF fingerprints
   — on one machine and not another for something that is not about the scanned content; the stderr line reaches the
   person who wrote the config. `--quiet` is also what the gate and each Downloads item use internally, so the line
   cannot repeat per item or per load.
   **Decided (2026-10-21)**: as recommended.
3. **Does the gate mention the set-aside section?** **Recommendation**: no. The gate never reads it (by contract); a
   message per load would be noise about something that does not change the gate's verdict, and `scan`, `check` and
   `llm status` already say it.
   **Decided (2026-10-21)**: as recommended.
4. **`llm status` with an invalid section: print the effective values or refuse?** **Recommendation**: refuse with the
   message, as today. It describes the judge configuration, and the honest description of an unusable one is the reason
   it is unusable; printing the stand-in defaults would describe a judge nobody configured.
   **Decided (2026-10-21)**: as recommended.

## Done

```
Merged: PR #107 (find the sha with git log --grep P-084 after the merge)
Evidence: TestLoad_InvalidLLMSectionIsSetAside, TestLoad_StillRefusesWhatEveryRunReads (internal/config/llm_section_test.go); TestLoad_PresetFillsEndpoint and TestLoad_BadDurationIsAnError now read the same message through Problem()
Evidence: TestInvalidLLMSection_StaticRunsGoAhead and TestInvalidLLMSection_GateStillAudits (cmd/aguard/llm_section_test.go) red on e18e255 (static runs exit 2; the gate answers GATE-000 "config error, this call was NOT audited"), green after
Evidence: reverse — TestInvalidLLMSection_JudgePathsStillRefuse green before and after (check/scan --llm, check/scan --fail-on-llm high, llm test, llm status, llm preview: exit 2 with the provider message); an invalid gate.action still gives the GATE-000 config error
Evidence: built binary, config `llm: {provider: not-a-provider}`: scan, check, approve exit 2 → 0 (scan and check with one stderr warning); hook PreToolUse and SessionStart GATE-000 → audited; version and hash unchanged
Evidence: P-075's positive control caught the blind spot (config.Load accepted the planted llm.provider); both isolation tests now plant an invalid gate.action
Evidence: make verify: all gates passed (go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 2 go 1.23.5; fresh test cache)
```
