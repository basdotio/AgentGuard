<!-- SPDX-License-Identifier: MIT -->
# 059 — The judge's server pass has no false-positive denominator, and every judge number so far was read on the set it was tuned against

- **Source**: the maintainer, 2026-10-15 ("良性服务器源码的误报分母还没有……这是一个空白" and "要不要再找一批验证集,以免优化是针对之前的测试集来的");
  P-053's benign panel (76 servers, not in the corpus); `judge-comparison-2026-09-25.zh-CN.md` from P-041 on
- **Depends on**: P-053 (the server pass, `LLM-010`), P-054 (its two readings), P-046 (`samples: 3` for anything that gates),
  P-033 (judgefold), P-021 (the driver's three judge-run exceptions)
- **Branch**: `p/059-benign-servers-heldout`
- **Corpus side**: agent-artifact-corpus PR "mcp: benign server source, by derivation and by harvest" (the four manifest entries,
  `scripts/harvest-mcp-servers.py`, the two layer-2 prep scripts, and a harness fix)

## Problem

Two gaps, one measurement discipline.

**No denominator for `LLM-010`.** P-053 measured the server pass's false positives on 76 real servers cloned into a scratch
directory: four pinned commits, trees not in the corpus, `judge.jsonl` written by hand in judgefold's row shape. Four draws at
`samples: 3` gave 4, 7, 5, 6 flags — the panel is too small to read a one-sample effect, and nobody else can rerun it, because the
driver (`baselines/cmd/baseline`) never hands the judge flags to `check`, which is the only invocation that reads a server tree.
Every other judge figure in this repository is reproducible from a corpus commit and a release asset; this one is not, and the
proposal that shipped the pass said so ("下一步应请语料维护者把良性服务器源码收进语料").

**Every judge figure was read on the set the prompt was tuned against.** The category table (P-041), what counts as a shown
category (P-049), the declared-only filter (P-054), the injection clause (P-050): each was written after reading this corpus's
residue, and each was then measured on this corpus. The 224 subset, the 30 residual benign flags, the 541 hooks-and-catalogues
panel are all draws from it. cc-audit was not tuned against it. How much of the 1.65% → 0.93% and 138 → 247 is capability and how
much is familiarity with the test cannot be told from any number so far, and a report that says "on this corpus" where it should
say "in general" is the exact overclaim the baselines README exists to prevent.

## Initial direction

**A denominator, in the corpus, in two populations.** Server source becomes a layer-1 benign population on the `mcp` surface
(`kind: server`, beside the tool catalogues), from two kinds of upstream and reported apart: four manifest entries derived from
pinned commits (the reference servers and the archived ones, wild; the fastmcp and python-sdk examples, fixture), and a GitHub
harvest of third-party Python servers by code search, one per repository, permissive licences only, forks excluded, trees bounded,
every exclusion printed, pinned in `manifest/pins/mcp-servers.json` so `--from-pins` rebuilds the set. Benign keeps the corpus's
meaning — somebody publishes this — and a security benchmark's own attack servers, a botnet tool and a tree holding live-looking
keys leave under a named `DROPPED` list rather than a quiet glob.

**The driver measures it.** The adapter gains one route: a tree that places nowhere but is MCP server source by aguard's own
recogniser goes to `check`, flagged `server-source` beside `secondary-surface`, and `-aguard-check-llm` hands the judge flags to
`check` on that route only. Default off, recorded in `run.yaml` (`check_llm`), and `judgefold -check-llm` reads it so a server
row with no judge summary is an incomplete answer rather than a static row by design. Every `check`-routed sample that is not
server source stays static, as in every judge run before this one. Two staging rules that the first run over the 89 derived
trees exposed are fixed on the way: server source is decided before any lone instruction file is looked for (a server's README was
being staged as its CLAUDE.md), and a lone `.md` places as an instruction file only when it is the whole tree.

**A held-out set, used once, aggregate only.** Three populations no proposal was written against: the malicious skills of
`agent-skill-malware` (ClawHub campaigns, wild, layer 2, deduplicated byte-for-byte against layer 1), a seeded, risk-stratified
subset of SkillHarm (fixture), and the harvest's repositories created on or after 2026-09-20, which did not exist when the corpus
was built. v0.21.0 — the judge before P-041 — and v0.24.0 run on each at `samples: 3`; the comparison is the two totals per
population, never a per-sample list, because the set stops being held out the first time a residue is read to write a prompt.

## Done criteria

- [x] Corpus: 89 derived server trees under `corpus/benign/mcp/{ref,arch,fm,sdk}-*` from four manifest entries, `make validate`
  green; `scripts/harvest-mcp-servers.py` vendors the code-search population and the two recent populations, prints every
  exclusion, and `--from-pins` rebuilds exactly the pinned set; a harness fix (`refute --write` no longer strips a `reviewed:`
  block) with a test
- [x] `TestStageSaysWhenTheUnplaceableTreeIsServerSource`, `TestAReadmeBesideSourceIsNotAnInstructionFile`,
  `TestServerSourceRouteCarriesTheJudgeFlagsOnlyWhenAsked`, `TestIncompletenessOnTheServerSourceRoute`: red on the base
- [x] `results/aguard/2026-10-15-llm-gpt-4.1-mini-p059-benign-servers-s3/`: the full server denominator at `samples: 3`, per source,
  raw archived on a release before the directory is committed
- [x] `results/aguard/2026-10-15-llm-gpt-4.1-mini-p059-heldout-*/`: v0.21.0 and v0.24.0 on the three held-out populations, aggregate
  tables only in the write-up
- [x] `baselines/README.md`, `docs/decisions/judge-comparison-2026-09-25.zh-CN.md`, the LLM-judge docs: the denominator, the route,
  the held-out reading and its limits

## Measured

All at `samples: 3`, gpt-4.1-mini, 2026-10-15; raw in `aguard-judge-raw-2026-10-p059.tar.gz` on v0.24.0. The corpus side is
basdotio/agent-artifact-corpus PR #2 (4,486 layer-1 samples after it: 300 malicious, 4,167 benign, 19 hard negatives).

**The denominator** (`…-p059-benign-servers-s3/`, 547 trees, 506 asked):

| population | asked | `LLM-010` ≥ high | any vote ≥ high | static ≥ high |
|---|---:|---|---:|---:|
| official + examples (4 upstreams, 89) | 69 | 5 (7.2% [3.1, 15.9]) | 9 | 10/89 |
| third-party, code search (268) | 261 | **65 (24.9% [20.0, 30.5])** | 127 | 59/268 |
| third-party, repositories created ≥ 2026-09-20 (190) | 189 | 26 (13.8% [9.6, 19.4]) | 74 | 58/190 |
| all | 506 | **95 (18.8% [15.6, 22.4])** | 140 | 127/547 |

P-053's 76-server panel (5–10%) was official and example code; on real third-party servers the pass flags one in five, and
142 of the code-search population's 176 escalating votes name `credential-read` for a server reading its own key for its
own API — the connector pass got that control in P-049, the server pass has none. `LLM-010` is advisory on third-party
servers until it does (docs updated). The static column is new too: `EXFIL-001` fires on 21–30% of real server trees.

**The held-out reading**, v0.21.0 (before P-041) against v0.24.0, totals only:

| population | n | v0.21.0: static / static∪judge / judge-added | v0.24.0: same |
|---|---:|---|---|
| ClawHub malicious skills (`agent-skill-malware`, wild) | 123 | 68 / 68 / 0 | 68 / 68 / 0 |
| ClawHub benign skills (same dataset) | 216 | 7 / 7 / 0 | 7 / 7 / 0 |
| SkillHarm, 10 per risk (fixture, LLM-authored) | 120 | 8 / 15 (12.5%) / 7 | 8 / **43 (35.8% [27.8, 44.7])** / 35 |
| skills from repositories created ≥ 2026-09-20 (full trees) | 400 | 72 / 76 (19.0%) / 4 | 72 / 81 (20.2%) / 9 |

Read together: where the judge has work to do that static rules do not (SkillHarm's intent-shaped attacks, ten of twelve
risks up), the gain holds on a set nobody tuned against; where static rules already carry the population (ClawHub's
`curl | sh` campaigns, `EXEC-011` 54 of 68) the judge adds nothing before or after; on real benign skill trees the judge's
added flags are 4 → 9 of 400, inside a `samples: 3` noise band and in the wrong direction, and the static rules flag
**72 of 400 (18%)** — the corpus's 2,480 benign skills are mostly a bare `SKILL.md`, and real installed skills carry
scripts that read an environment variable and make a request. That last number is the largest finding of the set and
belongs to the static rules, not the judge.

