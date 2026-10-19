<!-- SPDX-License-Identifier: MIT -->
# 075 — Tests read the developer's own config: an invalid `llm.provider` in `~/.config/aguard/config.yaml` turns `make verify` red

- **Source**: new finding (2026-10-19): on a maintainer's machine whose real config sets `llm.provider` to a value this
  build does not accept, `TestAdversarialCorpus` and 70 other top-level `cmd/aguard` tests fail with a config error
- **Depends on**: none
- **Branch**: `p/075-test-home-isolation`

## Problem

Every entry point the tests drive with an empty `--config` (`scanEnv`, `checkTarget`, `runHook`, the built binary in a
few tests) loads the user-level config through `config.LoadUser("")`, which reads `$XDG_CONFIG_HOME/aguard/config.yaml`,
else `$HOME/.config/aguard/config.yaml`. No test package sets either variable unless a single test needs a particular
value, so the suite reads whatever the developer has there.

Measured on `origin/main` (1c7ff83) with a config holding `llm: {provider: not-a-provider}`, placed once under
`XDG_CONFIG_HOME` and once under `HOME` (with `XDG_CONFIG_HOME` unset): `go test ./...` fails `cmd/aguard` in both
cases — `TestAdversarialCorpus`, the `TestAdversarial_*` invariants, `TestCheckZip_*`, `TestE2E_*`, `TestGate*` and more,
each with `config llm.provider "not-a-provider": want openai_compatible, or one of …`. The code under test is fine; the
red comes from the developer's home. A *valid* config is worse, because it is silent: `gate.fail_on`, `gate.action` or an
enabled judge change what those tests exercise, and the result depends on whose machine ran them.

`make verify` has the same leak one level up: after the tests it builds the binary and runs
`bin/aguard check plugin --fail-on low` in the developer's shell, which loads the same config and exits 2 on it — so
even with every test isolated, `make verify` stays red on that machine.

The same three variables reach further than the config: `HOME` is the default scan root and the Downloads folder,
`CLAUDE_CONFIG_DIR` overrides the default root, and `XDG_CONFIG_HOME` is where reports are written by default. The
other packages under `internal/` pass today only because none of their tests happens to load the user config; `collect`,
`judge` and `config` read the home directly, and almost every package imports `collect`.

## Initial direction

One test-only helper, `internal/testhome`, whose `Main` points `HOME` at a fresh temporary directory, `XDG_CONFIG_HOME`
at its `.config` and unsets `CLAUDE_CONFIG_DIR` before any test runs; every test package under `cmd/` and `internal/`
calls it from `TestMain`. A source test requires that `TestMain` in each package with tests, and a regression test plants
an invalid config in a "real" home and reruns the test binary against it. `make verify`'s plugin self-scan runs with
`HOME` and `XDG_CONFIG_HOME` at a path nothing creates.

## Done criteria

- [x] `TestIsolation_RealHomeConfigIsNotSeen` (`cmd/aguard/testhome_test.go`, new): plants `llm.provider:
  not-a-provider-this-build-accepts` under both `$HOME/.config/aguard` and `$XDG_CONFIG_HOME/aguard` of a temporary
  directory, checks that `config.Load` refuses that file (positive control), reruns this package's test binary with
  `HOME` and `XDG_CONFIG_HOME` pointing there and requires `TestAdversarialCorpus`, `TestE2E_StaticBaseline` and
  `TestGateEndToEnd` each to print `--- PASS`. Red without `TestMain` in `cmd/aguard` (every probe fails on the config
  error), green with it
- [x] `TestEveryTestPackageIsolatesTheHome` (`internal/testhome/testhome_test.go`, new): every directory under `cmd/` and
  `internal/` that holds a `_test.go` file has `func TestMain(m *testing.M) { os.Exit(testhome.Main(m)) }`; red before
  the `TestMain` files were added (all 18 packages listed), green after; the walk must have seen `cmd/aguard` and `internal/testhome` (positive control)
- [x] `TestMain_MovesTheHomeAway`: inside a test, `HOME` and `XDG_CONFIG_HOME` differ from the values the binary was
  started with, `CLAUDE_CONFIG_DIR` is unset, `os.UserHomeDir()` is the isolated home, which is a resolved path and holds
  only an empty `.config`
- [x] Reverse assertion: tests that set their own home with `t.Setenv` (`TestLoadUser_DefaultLocation`, the judge egress
  tests, `runAguard`) still pass unchanged; the toolchain processes tests start (`go build` in `builtAguard`, `git` in the
  doc-link tests) still run with the developer's environment (`testhome.OriginalEnviron`), so the build cache is not cold
  and `builtAguard` does not skip
- [x] With the invalid config in the shell's `XDG_CONFIG_HOME`, and separately in `HOME` with `XDG_CONFIG_HOME` unset,
  `go test -count=1 ./...` passes
- [x] `make verify`'s self-scan runs `bin/aguard check plugin` with `HOME` and `XDG_CONFIG_HOME` under
  `bin/verify-home-never-created` and `CLAUDE_CONFIG_DIR` empty. Reverse assertion: the same environment on a skill with
  `curl … | bash` still exits 1; the same command in the developer's shell with the invalid config exits 2 (the bug)
- [x] `make verify` green under `go version go1.23.5` with no toolchain switch; `head -3 go.mod` line 2 `go 1.23.5`

## Out of scope

- `baselines/`: its adapter already runs the built binary with `HOME` and `XDG_CONFIG_HOME` in its work directory, and its
  Go tests do not load the user config; `baselines/cmd/judgefold` builds and runs aguard with its own environment
- Other environment variables that change behaviour: `AGUARD_LLM_KEY` (the default `api_key_env`; read only when a test
  enables the judge, and those tests point it at an `httptest` stub), and any future `AGUARD_*` variable. Each is its
  own case when it can change a test's result without the test setting it
- The CI workflow's own plugin self-scan step: a CI runner has no user config, and the step is not `make verify`
- Product code: no non-test file changes; the config lookup order, `LoadUser` and every default stay as they are
- Windows `USERPROFILE`: not a release target (conventions)

## Must not claim

- Not "tests are hermetic": only `HOME`, `XDG_CONFIG_HOME` and `CLAUDE_CONFIG_DIR` are replaced. `TMPDIR`, `PATH`, the
  locale and other variables still come from the developer's shell, and toolchain processes deliberately get the original
  environment
- Not "a test can no longer read the real home": a test that hard-codes a path, or reads `testhome.OriginalEnviron()` and
  passes it to aguard, still can. The guard checks that every package calls `Main`, not what the tests do afterwards

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-075: proposal — tests read the developer's own config, an invalid provider turns make verify red` |
| 2 | `internal/testhome`, a `TestMain` in every test package, the regression and guard tests, toolchain calls on the original environment | `test: run every test package with a temporary HOME and XDG_CONFIG_HOME (P-075)` |
| 3 | Architecture pair, CONTRIBUTING, the conventions rule | `docs: the testhome helper and the TestMain rule (P-075)` |
| 3b | `make verify`'s plugin self-scan runs without the developer's config; the conventions rule names it | `build: make verify's plugin self-scan runs without the developer's config (P-075)`, `docs: the conventions rule names the self-scan's home too (P-075)` |
| 4 | This file to `complete/`, Done filled, the index row | `P-075: done — evidence and the index row` |

## Open questions

1. **Per test with `t.Setenv`, or once per package in `TestMain`?** **Recommendation**: `TestMain`. The tests that load
   the config are spread over dozens of helpers (`scanEnv`, `checkTarget`, `runHook`, the built binary), and the next
   one written will not remember a per-test call. `TestMain` decides what a test that sets nothing sees; a test that needs
   a particular home still uses `t.Setenv`, which overrides it for that test only.
   **Decided (2026-10-19)**: as recommended.
2. **Only the packages that read the home today, or every package with tests?** **Recommendation**: every package
   under `cmd/` and `internal/`. "Reads the home" is transitive — almost every package imports `collect`, which reads it —
   so a list of readers drifts the first time a helper moves; a rule with no exceptions is checkable by a short source
   test and costs one three-line file per package.
   **Decided (2026-10-19)**: as recommended.
3. **How to prove the fix without writing into the developer's real home?** **Recommendation**: rerun the package's own
   test binary (`os.Args[0]`) with `HOME` and `XDG_CONFIG_HOME` pointing at a temporary directory that holds the invalid
   config — from the child's point of view that is its real home, exactly as a developer's shell hands it over. A
   positive control checks that `config.Load` refuses the planted file, and the probes must each print `--- PASS`, so a
   child that ran nothing cannot pass.
   **Decided (2026-10-19)**: as recommended.
4. **What do `go build` and `git` started from a test see?** **Recommendation**: the environment the binary started with
   (`testhome.OriginalEnviron()`). `go build` derives its build cache, module cache and `GOENV` from the home; under the
   isolated one the cache is cold (slow) and, offline, the build fails into `builtAguard`'s `t.Skip`, so a dozen binary
   tests would silently stop running. `git ls-files --exclude-standard` reads the global excludes file under the home.
   Anything that runs aguard itself keeps the isolated environment.
   **Decided (2026-10-19)**: as recommended.

Found during stage 2:

5. **`make verify`'s self-scan: a temporary directory or a path that never exists?** **Recommendation**: a fixed path
   under the ignored `bin/` that nothing creates (`bin/verify-home-never-created`). The config lookup treats a missing
   file as defaults, `check plugin` needs nothing under the home, and the recipe stays one command whose exit code is
   the gate's — no `mktemp`, no clean-up removal, no saved exit status.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR to be opened (find the sha with git log --grep P-075 after the merge)
Evidence: TestIsolation_RealHomeConfigIsNotSeen (cmd/aguard/testhome_test.go): red before cmd/aguard had its TestMain (every probe failed on `config llm.provider "not-a-provider-this-build-accepts"`), green after
Evidence: TestEveryTestPackageIsolatesTheHome, TestMain_MovesTheHomeAway, TestOriginalEnviron_IsTheStartingEnvironment (internal/testhome/testhome_test.go); the guard listed all 18 test packages before their main_test.go files, none after
Evidence: go test -count=1 ./... with `llm.provider: not-a-provider` in the shell's XDG_CONFIG_HOME: cmd/aguard FAIL (71 top-level tests) on 1c7ff83 → ok; the same with the file under HOME and XDG_CONFIG_HOME unset: FAIL → ok
Evidence: reverse — go test -v ./cmd/aguard prints no --- SKIP (builtAguard still builds with the original environment); the self-scan's environment on a `curl … | bash` skill exits 1; `bin/aguard check plugin` in a shell whose config names an unaccepted provider exits 2
Evidence: make verify: all gates passed on a machine whose real config names a provider this build rejects (go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 2 go 1.23.5; fresh test cache)
```
