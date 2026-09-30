<!-- SPDX-License-Identifier: MIT -->
# Contributing

## Build and check

```bash
make build      # -> bin/aguard (CGO_ENABLED=0; version/commit/date via -ldflags)
make test       # go test -race -cover ./...
make lint       # golangci-lint (version pinned in .github/workflows/ci.yml)
make docs       # regenerate docs/rules.md from the rule set — required after adding or changing a rule
make verify     # everything CI runs, in one command; green before you ask for review
make hooks      # optional: a pre-commit hook that scans staged skills with aguard itself
```

Go 1.23.x. Do not let `go mod tidy` raise the `go` directive in `go.mod` (CI pins 1.23); the
conventions in `.claude/rules/conventions.md` explain the one dependency that tries to.

## Making a change

- One branch per change, a pull request against `main`, squash or rebase merge.
- Every `.go` file starts with `// SPDX-License-Identifier: MIT`. Code and user-visible strings
  are English. Documents that exist as a bilingual pair (`README`, `docs/architecture`,
  `docs/llm-judge`, `docs/install-gate`) are changed together.
- A change to a detection rule runs against the corpus benchmark (`make bench`, needs a sibling
  checkout of `agent-artifact-corpus`) and states its before/after per source, with n. Numbers are
  never pooled across sources.
- A change that touches an invariant (`.claude/rules/invariants.md`) or the data model updates
  `docs/spec/spec.zh-CN.md` in the same pull request and adds or extends an invariant test.
- Confirmed bypasses, coverage gaps and rejected approaches are recorded under `issues/` with the
  evidence; a fix that leaves nothing worth knowing afterwards needs no record beyond its commit.

## Proposals for larger changes

A change to a rule, an invariant, the data model or the collection surface — anything whose
"done" is not obvious — starts as a proposal under `docs/proposals/` (template and rules in
[docs/proposals/README.md](docs/proposals/README.md), in Chinese). The directory a proposal sits
in is its state: `draft/` and `design/` live on the `p/NNN-slug` branch, `complete/` and
`rejected/` land on `main` with the PR. Commits on such a branch end with `(P-NNN)`. Typos, CI
lines and comment fixes need none of this.

## Reporting a security issue

Open a private security advisory on GitHub rather than a public issue for anything that lets a
scanned artifact evade the gate or influence the scanner.
