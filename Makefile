# SPDX-License-Identifier: MIT
# AgentGuard build. Pure Go, single static binary (CGO disabled).
BIN     := bin/aguard
PKG     := ./cmd/aguard
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE)

export CGO_ENABLED=0

.PHONY: build test lint docs dist npm-dist clean reputation-refresh verify hooks bench

# Portable sha256: coreutils on Linux, shasum (perl) on macOS.
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

# The whole gate in one command: what a change must pass before it is
# handed over for review. The self-scan covers the shipped plugin — a repository that gates other
# people's skills runs its own through the same gate. Each line is its own shell; the first failure stops make. The order is
# cheapest-first so a red vet does not wait for the race detector. Deliberately absent: the
# real-machine scan (`aguard scan --root ~/.claude`) — that one is run by hand after collect/detect
# changes, because its input is this machine, not the repository.
verify:
	go vet ./...
	go test -race -cover ./...
	@command -v golangci-lint >/dev/null || { echo "verify: golangci-lint is not installed — see CONTRIBUTING.md for the pinned version" >&2; exit 1; }
	golangci-lint run ./...
	go run ./hack/gen-rules && git diff --quiet -- docs/rules.md || { echo "verify: docs/rules.md drifted from the code — commit the regenerated file" >&2; exit 1; }
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG) && $(BIN) check plugin --fail-on low --quiet
	@grep -qx 'go 1.23.5' go.mod || { echo "verify: go.mod's go directive moved off 1.23.5 (see .claude/rules/conventions.md)" >&2; exit 1; }
	@echo "verify: all gates passed"

# Install the pre-commit hook. Per machine: a clone that skipped this is not gated.
hooks:
	cp hack/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "hooks: pre-commit installed into .git/hooks/"

# Regenerate docs/rules.md from the engine's own rule set. CI runs this and fails if the
# result differs from what is committed, so the rule reference cannot drift from the code —
# a wrong severity in a security tool's documentation is acted upon.
docs:
	go run ./hack/gen-rules

# Run aguard over the agent-artifact-corpus and grade the verdicts (P-010, P-015). Three steps,
# two of them the corpus's own: `corpus samples` (work list) → baselines/cmd/baseline (ours:
# stage each sample where aguard looks, exec the freshly built binary, fold to one word, and
# account for EVERY test point in a ledger) → `corpus score`.
#
# The driver replaced hack/corpus-runner in P-015 W5. That runner returned before the binary was
# executed for any sample it could not place, so 127 test points were never handed to the scanner
# at all — and the reason recorded against them was the runner's belief about aguard rather than
# aguard's behaviour. Measured after the change: 3539 of 3539 scored, 0 uncovered; recall over
# the full malicious set is 85/300 (28%) where the partial denominator read 71/173 (41%).
# The corpus is a sibling checkout, never vendored (licence isolation) and never a CI dependency;
# this target is for a maintainer's machine. Verdicts and per-sample JSON land under bin/bench/
# (ignored) so a before/after diff of a rule change is `diff bin/bench/*/verdicts.jsonl`.
#
#   make bench                                  # threshold high — the gate's own behaviour
#   make bench BENCH_THRESHOLD=medium           # the second group; publish both, never one
#   make bench CORPUS=/elsewhere/agent-artifact-corpus
#
# Read the scorecard with docs/corpus-benchmark.zh-CN.md §2 open: per source, with n,
# reputation off, and the uncovered count stated — a pooled percentage from this output is the
# 11.0%-that-was-12.9% mistake again.
CORPUS          ?= ../agent-artifact-corpus
BENCH_THRESHOLD ?= high
BENCH_OUT       := bin/bench/$(BENCH_THRESHOLD)
bench: build
	@test -d "$(CORPUS)/harness" || { echo "bench: no corpus at $(CORPUS) — clone github.com/basdotio/agent-artifact-corpus beside this repo or pass CORPUS=" >&2; exit 2; }
	@mkdir -p $(BENCH_OUT)
	cd $(CORPUS)/harness && go run ./cmd/corpus samples > $(abspath $(BENCH_OUT))/samples.jsonl
	go run ./baselines/cmd/baseline -tool aguard -aguard $(BIN) -corpus $(CORPUS) \
	  -threshold $(BENCH_THRESHOLD) -samples $(BENCH_OUT)/samples.jsonl \
	  -out $(BENCH_OUT) -raw $(BENCH_OUT)/raw -score-tool aguard
	cd $(CORPUS)/harness && go run ./cmd/corpus score -tool aguard $(abspath $(BENCH_OUT))/verdicts.jsonl

# Renew allowlist entries whose marketplace pin moved WITHOUT changing the finding set the
# human review covered (see hack/reputation-refresh). Exits 1 and prints the diff when a set
# changed — that is the signal to read the new version, not a failure to route around.
# `-write` is opt-in; run bare to see what would move.
reputation-refresh: build
	go run ./hack/reputation-refresh -aguard $(BIN) $(REFRESH_FLAGS)

# Cross-compile single binaries for the release targets, plus the checksum file. Release
# artifacts are the RAW binaries (not archives) so the documented install is a curl + chmod:
# an unarchived static binary is what "single binary" is supposed to buy. SHA256SUMS.txt is
# produced here rather than in the release workflow so that what CI publishes is byte-for-byte
# what `make dist` gives a maintainer locally — a security tool whose own download cannot be
# verified is asking for trust it has not earned.
#
# windows/amd64 is deliberately NOT shipped. It cross-compiles cleanly, and that is the whole
# problem: CI runs ubuntu only, there is not one GOOS branch in the codebase, and the symlink
# boundary (invariant #2) is enforced with primitives that behave differently on Windows —
# where creating a symlink needs a privilege the tests cannot assume. Publishing a binary no
# test has ever executed, for a tool whose output people act on, trades a missing platform for
# a wrong verdict. Add windows here together with a CI matrix that runs the suite on it.
DIST_TARGETS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

dist:
	@mkdir -p dist
	@for t in $(DIST_TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
	  echo "  $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/aguard-$$os-$$arch$$ext $(PKG) || exit 1; \
	done
	@cd dist && $(SHA256) aguard-* > SHA256SUMS.txt && echo "  SHA256SUMS.txt"

# npm distribution. THE package name lives here and nowhere else: the launcher derives the
# per-platform package names from its own package.json at run time, and the templates take it
# by substitution. Renaming the package is this one line plus the READMEs.
NPM_NAME    := @bas.io/guard
# npm needs bare semver; the Go stamp is a git describe, so `v0.8.1` -> `0.8.1`.
#
# An untagged tree stamps `0.8.1-2-gabc` or `0.8.1-dirty`, and npm ACCEPTS both — they are legal
# semver prereleases (measured, not assumed: `npm pack` happily produces
# example-0.8.1-3-gabc1234-dirty.tgz). So nothing downstream would stop a hand-run publish from
# permanently occupying a junk version, and npm forbids ever reusing one. The refusal has to be
# here, and it is the check below, not a property of npm.
#
# Overriding the version is still allowed — `make npm-dist NPM_VERSION=0.0.0-dev` lays the
# packages out from any tree — because then it is a stated choice rather than a git artefact.
NPM_VERSION ?= $(patsubst v%,%,$(VERSION))
# Empty unless NPM_VERSION was derived above rather than supplied by the caller.
NPM_VERSION_DERIVED := $(if $(filter file,$(origin NPM_VERSION)),1,)
NPM_OUT     := dist/npm

# Lay out the five packages under dist/npm: one launcher package plus one per platform, each
# carrying its own static binary. No package has an install script — the binary is IN the
# tarball and npm picks the right one from the os/cpu fields. The widespread alternative (a
# postinstall that downloads a binary) is the exact shape this scanner reports on other
# people's repositories, so shipping it here would make the tool contradict its own README.
#
# It deliberately does NOT depend on `dist`: it packages the binaries that are already there.
# LDFLAGS stamps a build date, so re-running `dist` would produce different bytes, and the npm
# tarballs would then carry binaries that no published SHA256SUMS.txt describes. Reusing the
# artifacts is what keeps "what npm installs" and "what the release page lists" the same file.
npm-dist:
	@if [ -n "$(NPM_VERSION_DERIVED)" ] && ! echo '$(NPM_VERSION)' | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$'; then \
	  echo "refusing to build npm packages at version '$(NPM_VERSION)': that is a git describe of an" >&2; \
	  echo "untagged or dirty tree, and npm would accept publishing it. Check out a version tag, or" >&2; \
	  echo "state the version deliberately: make npm-dist NPM_VERSION=0.0.0-dev" >&2; \
	  exit 1; \
	fi
	@for t in $(DIST_TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; \
	  [ -f dist/aguard-$$os-$$arch ] || { echo "dist/aguard-$$os-$$arch missing — run 'make dist' first" >&2; exit 1; }; \
	done
	@rm -rf $(NPM_OUT)
	@mkdir -p $(NPM_OUT)/launcher/bin
	@sed -e 's|__NAME__|$(NPM_NAME)|g' -e 's|__VERSION__|$(NPM_VERSION)|g' \
	  npm/package.json.tmpl > $(NPM_OUT)/launcher/package.json
	@sed -e 's|__NAME__|$(NPM_NAME)|g' npm/README.md.tmpl > $(NPM_OUT)/launcher/README.md
	@cp npm/bin/aguard.js $(NPM_OUT)/launcher/bin/aguard.js
	@echo "  $(NPM_NAME)@$(NPM_VERSION)"
	@for t in $(DIST_TARGETS); do \
	  os=$${t%/*}; goarch=$${t#*/}; \
	  cpu=$$goarch; [ $$goarch = amd64 ] && cpu=x64; \
	  dir=$(NPM_OUT)/$$os-$$cpu; \
	  mkdir -p $$dir || exit 1; \
	  sed -e 's|__PKG__|$(NPM_NAME)-'"$$os-$$cpu"'|g' \
	      -e 's|__NAME__|$(NPM_NAME)|g' \
	      -e 's|__VERSION__|$(NPM_VERSION)|g' \
	      -e 's|__OS__|'"$$os"'|g' \
	      -e 's|__CPU__|'"$$cpu"'|g' \
	      npm/platform.package.json.tmpl > $$dir/package.json || exit 1; \
	  cp dist/aguard-$$os-$$goarch $$dir/aguard || exit 1; \
	  chmod 0755 $$dir/aguard || exit 1; \
	  echo "  $(NPM_NAME)-$$os-$$cpu"; \
	done

clean:
	rm -rf bin dist
