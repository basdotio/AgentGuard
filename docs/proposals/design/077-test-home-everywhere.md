<!-- SPDX-License-Identifier: MIT -->
# 077 — P-075 isolated the home for `cmd/` and `internal/` only: tests elsewhere, the cache/state/data directories and the usage log still reach the developer's machine, and no test lists the locations a test must not touch

- **Source**: new finding (2026-10-19), follow-up to P-075 (PR #95): a review of what `internal/testhome` replaces against
  every location the product resolves on its own
- **Depends on**: P-075
- **Branch**: `p/077-test-home-everywhere`

## Problem

P-075 gives every test package under `cmd/` and `internal/` a temporary `HOME` and `XDG_CONFIG_HOME` and unsets
`CLAUDE_CONFIG_DIR`. Four gaps remain.

1. **The usage log is written by the test suite.** `AGUARD_USAGE_LOG=<path>` (P-073) makes every judge run append one
   line to that file. It is the developer's own ledger, set in their shell, and `testhome.Main` leaves it alone. The
   `cmd/aguard` tests that run the judge in process against an `httptest` stub (and do not set the variable themselves)
   append to it. Measured on `origin/main` (eba242f) with `AGUARD_USAGE_LOG` pointing at a one-line file:
   `go test -count=1 ./...` passes and the file has **43 lines afterwards: 42 appended by `cmd/aguard`** — 40
   `check`/`scan` judge runs of model `fake` and 2 `llm test` lines of model `test-model`, all against
   `http://127.0.0.1:<port>`. A ledger kept to add up real judge runs over days now holds test traffic, told apart from
   real runs only by the model name and the loopback endpoint.
2. **`hack/` is not covered.** `hack/gen-rules` and `hack/reputation-refresh` have tests, in the same module, and no
   `TestMain`; `TestEveryTestPackageIsolatesTheHome` walks `cmd/` and `internal/` only. Neither package's tests read the
   home today (`reputation-refresh` reads `os.UserHomeDir()` in `main`, not in a tested function), so nothing fails —
   but a test added there tomorrow that reaches `config.LoadUser("")` or the default root reads the developer's files
   and the guard stays green. `baselines/` is excluded by P-075 with a stated reason, and the guard does not name the
   exclusion: any other new top-level directory is not walked either.
3. **`XDG_CACHE_HOME`, `XDG_STATE_HOME` and `XDG_DATA_HOME` still come from the developer's shell.** No product code
   reads them today (`grep` for `XDG_`, `UserCacheDir`, `UserConfigDir` over `cmd/` and `internal/` finds only
   `XDG_CONFIG_HOME`), so this is not a failure now. It is the next place a default would naturally go — a judge
   response cache, a gate state file — and the first such read would reach the developer's real directory from every
   test that does not think of it, exactly as the config did before P-075.
4. **No test lists the locations the suite must not touch.** P-075's guard checks that each package *calls*
   `testhome.Main`, and `TestMain_MovesTheHomeAway` checks the three variables it replaces. Neither asks the product
   where it would read or write by default. The usage log above is the proof that "these variables are replaced" and
   "every default location is inside the temporary home" are different statements: the gap was not in a variable the
   helper knew about. The defaults today are the config file and key file (`config.DefaultPath`, `DefaultKeyPath`), the
   `--report` directory, the default root (`$CLAUDE_CONFIG_DIR`, else `~/.claude`) and what lives under it (gate
   approvals and pending verdicts, the `settings.json` that `hook install` edits and its backups, `clean`'s quarantine
   and manifest), the user-level MCP config and the Claude Desktop store and session cache beside the root,
   `~/Downloads`, `~` in a configured path, the home the judge scrubs from what it sends, and the usage log.

The same run also confirms what P-075 fixed: a config naming a provider this build rejects, a marker
`~/.claude/settings.json` and marker files under the three XDG directories are byte-identical afterwards. Only the
usage log changed.

## Initial direction

Extend `internal/testhome` rather than add a second mechanism: point the three remaining XDG variables into the
temporary home, unset `AGUARD_USAGE_LOG` with `CLAUDE_CONFIG_DIR`, and expose the temporary home so a test can compare
against it. Give the two `hack/` packages their `TestMain` and make the guard walk the whole module, with `baselines/`
as a named exclusion that carries its reason. Add one guard in `cmd/aguard` that calls the product's own resolution
functions and requires every answer inside the temporary home, and a reverse test that a test's own invalid config is
still refused. No product code changes.

## Done criteria

- [ ] With a poisoned real home — `~/.config/aguard/config.yaml` = `llm: {enabled: true, provider: bogus}`, a
  `~/.claude/settings.json` marker, marker files under `$XDG_CACHE_HOME/aguard`, `$XDG_STATE_HOME/aguard`,
  `$XDG_DATA_HOME/aguard` (all three set into that home), and `AGUARD_USAGE_LOG` at a one-line marker file —
  `HOME=<that> XDG_CONFIG_HOME= go test -count=1 ./...` passes and **every** marker file is byte-identical afterwards,
  the usage log included (it gains 42 lines on `origin/main`). Go's own cache paths are resolved before `HOME` moves
- [ ] `TestEveryTestPackageIsolatesTheHome` walks the whole module, not only `cmd/` and `internal/`; `hack/gen-rules` and
  `hack/reputation-refresh` call `testhome.Main`; `baselines/` is skipped by a named entry that carries its reason, and
  the positive control requires the walk to have seen `cmd/aguard`, `internal/testhome` and both `hack/` packages
- [ ] `TestMain_MovesTheHomeAway` also pins: `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME` lie under the temporary
  home and none is the value the binary started with; `AGUARD_USAGE_LOG` is unset; `testhome.Home()` is the isolated home
- [ ] `TestDefaultLocations_AreInsideTheTestHome` (`cmd/aguard`, new) pins: every default location listed under Problem
  item 4, resolved through the product's own functions (`config.DefaultPath`, `config.DefaultKeyPath`,
  `defaultReportPath`, `defaultRoot`, `gate.ApprovalsPath`, `gate.SettingsPath`, `collect.TrashDir` under the root,
  every entry of `scanLocations`, `defaultInbox`, `config.ExpandHome`, `os.UserHomeDir`, `os.UserCacheDir`,
  `os.UserConfigDir`, the three XDG variables) lies under `testhome.Home()`, and `AGUARD_USAGE_LOG` is unset. It fails
  with "testhome.Main did not run" when the package's `TestMain` is removed, rather than passing against the real home
- [ ] Reverse assertion `TestIsolatedHome_OwnConfigIsStillRefused` (`cmd/aguard`, new): a config the test writes itself
  with `provider: bogus` is refused by `scanEnv` and `checkTarget` through `--config`, and by `scanEnv` at the default
  location inside the temporary home, with `XDG_CONFIG_HOME` set and with it empty (the `HOME` fallback); without it the
  scan runs on defaults
- [ ] The guards bite: removing `hack/gen-rules`' `TestMain` turns `TestEveryTestPackageIsolatesTheHome` red naming
  `hack/gen-rules`; removing `cmd/aguard`'s turns `TestDefaultLocations_AreInsideTheTestHome` red
- [ ] `make verify` green under `go version go1.23.5` with no toolchain switch; `go.mod` line 2 `go 1.23.5`

## Out of scope

- Product code: no non-test file outside `internal/testhome` changes (`git diff --stat origin/main...HEAD -- cmd internal
  hack` lists only `_test.go` files and `internal/testhome`). Defaults, lookup order and the usage log's behaviour stay
- `baselines/`: kept out, with the reason now written into the guard. Its tests and its non-test code start the Go
  toolchain (`go build`, `go list`, `go run ./cmd/corpus`, the scorer) with the inherited environment; under a temporary
  home those children get an empty build and module cache and lose the developer's `GOENV` (a `GOPROXY`, say), which is
  slow online and fails offline, and steering the non-test spawns to the original environment would change non-test
  code. In process it reads no aguard config, and the aguard it runs gets an environment the adapter builds from
  nothing (`HOME` and `XDG_CONFIG_HOME` in its work directory; no `AGUARD_USAGE_LOG`) — the poisoned run above includes
  `baselines/` and leaves the markers untouched
- `AGUARD_LLM_KEY` (the default `api_key_env`), as in P-075: read only by tests that enable the judge, which point it at an
  `httptest` stub on loopback; it is a read of a value, not a location
- `TMPDIR`: zip extraction and `builtAguard` use it, and it is where the temporary home itself lives
- Windows `USERPROFILE`, `LOCALAPPDATA`, `APPDATA`: not a release target (conventions)

## Must not claim

- Not "tests are hermetic": `TMPDIR`, `PATH`, the locale and every other variable still come from the developer's
  shell, and toolchain processes deliberately get the original environment
- Not "the suite wrote into the developer's config": measured, the config, `settings.json` and the XDG markers were
  untouched before this change too. The write that happened was to the usage log, and only when the developer had set it
- Not "every location the product could touch is listed": the guard lists the defaults the product resolves today; a
  new default is covered only once someone adds it to the table (the cache/state/data variables are the defence for the
  case where nobody does)

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-077: proposal — P-075 left hack/, the cache/state/data directories and the usage log outside the test home` |
| 2 | `testhome`: the three XDG variables, `AGUARD_USAGE_LOG` unset, `Home()`; its own test pins them | `P-077: the test home also covers the XDG cache, state and data directories and the usage log` |
| 3 | The guard walks the whole module with `baselines/` named; `TestMain` in both `hack/` packages | `P-077: every test package in the module runs under the test home, hack/ included` |
| 4 | The default-location guard and the reverse test in `cmd/aguard` | `P-077: a guard lists every default location and requires each inside the test home` |
| 5 | Architecture pair, CONTRIBUTING, the conventions rule | `P-077: docs — the test home covers the whole module and the guard lists the defaults` |
| 6 | This file to `complete/`, Done filled, the index row | `P-077: done — evidence and the index row` |

## Open questions

1. **`AGUARD_USAGE_LOG`: unset it in `testhome.Main`, or make each judge test set its own?**
   **Recommendation**: unset it in `Main`. The tests that append are spread over every helper that enables the judge
   (`scanEnv`, `checkTarget` with `--llm` options, the inbox), the same shape P-075 answered with `TestMain`; the tests
   that exercise the log (`usagelog_test.go`, the zero-dial positive control) already set it with `t.Setenv` or pass it
   to the built binary explicitly, so they are unaffected.
   **Decided (2026-10-19)**: as recommended.
2. **The three XDG variables: create the directories, or only point at them?**
   **Recommendation**: point only. A fresh machine does not necessarily have them; the code that would write there has to
   `MkdirAll` anyway, and the isolated home then still holds only `.config`, which `TestMain_MovesTheHomeAway` keeps
   asserting.
   **Decided (2026-10-19)**: as recommended.
3. **`baselines/`: cover it or keep it out?**
   **Recommendation**: keep it out, as a named entry in the guard with the reason (Out of scope). Covering it would need
   non-test code changes to keep the Go toolchain on the developer's caches, for a tree whose in-process code reads no
   aguard config and whose aguard children already run in an environment built from nothing. Naming it changes the
   default: the walk now covers every other directory, so the next top-level directory with tests is in by default.
   **Decided (2026-10-19)**: as recommended.
4. **Where does the default-location guard live?**
   **Recommendation**: `cmd/aguard`, because most of the defaults are unexported functions of `package main`
   (`defaultRoot`, `defaultInbox`, `defaultReportPath`, `scanLocations`), and calling them is the point — a list of
   paths built in the test would only agree with itself. It compares against `testhome.Home()`, recorded by `Main`, not
   against `$HOME`: without `TestMain`, `$HOME` is the developer's and every location would be "under" it.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR <number> (find the sha with git log --grep P-077 after the merge)
Evidence: <filled in at stage 4>
```
