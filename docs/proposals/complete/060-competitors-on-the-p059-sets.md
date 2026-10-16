<!-- SPDX-License-Identifier: MIT -->
# 060 — The two comparison tools on the P-059 populations, and Cisco's own server analyzer on the server denominator

- **Source**: the maintainer, 2026-10-15 ("然后是不是可以再竞品对比一下新的数据"), after P-059 merged and agent-artifact-corpus PR #2 landed
- **Depends on**: P-059 (the populations and the held-out discipline), P-017 / P-022 (the cc-audit and skill-scanner adapters and their pins), P-034 (per-source rates)
- **Branch**: `p/060-competitors-p059`

## Problem

P-059 put four numbers on the table that no comparison tool had been measured against: 547 benign server trees, 400 real skill
trees from repositories newer than the corpus, and two held-out malicious sets. Two of those numbers are about the static
rules, not the judge — `EXFIL-001` at high on 18% of real skill trees and 21–30% of real server trees — and whether that is
this scanner's defect or the shape of the problem cannot be told from one scanner. The third, cc-audit's 46-sample lead on
the 300-malicious subset, was traced to one source (cisco) in September; the held-out sets are the first chance to see
whether the lead exists anywhere else. And the server denominator has a natural second column: the vendor whose eval set is
the corpus's malicious server side ships a scanner with an LLM analyzer for server source.

## Initial direction

Measure, change nothing. cc-audit v3.23.9 and Cisco skill-scanner (2.3.0, the current wheel; 2.1.0 is no longer the one `pip`
resolves) through their existing adapters, static, on the four skill populations. Cisco mcp-scanner's `behavioral` analyzer
on the 547 server trees, pointed at the same model the judge uses (gpt-4.1-mini) so the column differs in the tool and not in
the model; a script beside the run, because its unit is a tool rather than a tree and no adapter exists yet. Report per
population, aguard's static column and its static∪judge column apart, every competitor figure `provisional`, and a tool that
reads nothing in a tree counted as no-verdict rather than clean.

## Done criteria

- [x] `results/ccaudit/2026-10-16-p059-*/`, `results/skill-scanner/2026-10-16-p059-*/`: the driver's own output on the four
  skill populations
- [x] `results/mcp-scanner/2026-10-16-behavioral-benign-servers/`: the behavioral analyzer on the 547 server trees, with
  the runner and the fold beside the run, and `mcp-scanner` registered in `baselines/tools.yaml`
- [x] The decisions document carries one table per population with every tool's column and the denominators
- [x] Raw output archived on a release before the directories are committed

## Measured

Static, 2026-10-15/16, corpus at 810f3d46; every competitor figure provisional. aguard's two columns are P-059's runs.

**Four skill populations** (a sample counts as flagged at each tool's own threshold):

| population | n | aguard static | aguard static∪judge | cc-audit 3.23.9 | skill-scanner 2.3.0 |
|---|---:|---:|---:|---:|---:|
| recent real skill trees (benign; fewer is better) | 400 | 72 (18%) | 81 (20%) | **236 (59%)** | 84 (21%) |
| ClawHub benign skills (fewer is better) | 216 | 7 (3.2%) | 7 | 51 (24%) | 10 (4.6%) |
| ClawHub malicious skills (more is better) | 123 | 68 (55%) | 68 | 53 (43%) | 58 (47%) |
| SkillHarm, 10 per risk (more is better) | 120 | 8 | **43 (36%)** | 40 (33%) | 8 (6.7%) |

The 18% static rate on real trees is not this scanner's alone — skill-scanner is at 21%, cc-audit at 59% — and every
tool is far above its rate on the corpus's bare-`SKILL.md` populations. cc-audit's September lead on the 300-malicious
subset does not reappear on either held-out malicious set, and it pays 24–59% on the benign side. On SkillHarm the two
static competitors sit at 8 and 40; the only column at 43 is the one with the judge.

**547 benign server trees, Cisco mcp-scanner `behavioral` (gpt-4.1-mini, directory mode)**: tools found in 458 trees, none
in 89; HIGH 0, ≥ MEDIUM 0, unsafe 0. Not a usable column: in file mode on Cisco's own 127 malicious eval servers the same
analyzer found a tool in 33 of 127 and flagged 2 (the judge: 108; static: 21; cc-audit: 74), and the log shows its response validator discarding part of the model's answers
(`Invalid response … skipping`) while the rest come back SAFE, `api_key_harvesting_environment` included, under gpt-4.1 as
well. Directory mode found no tool in any of those 127 (8 of 8 probed returned an empty list). Recorded in
`results/mcp-scanner/2026-10-16-behavioral-*`, with the runner and the fold beside the run; the tool is registered in
`baselines/tools.yaml` with no driver adapter yet.

**Label review, after the fact (2026-10-16; labels only, no code)**: a seeded 20 (seed 20261016) of the 72 recent skill trees
aguard flags at high, read at the flagged lines against the SKILL.md's declared purpose: 20 of 20 are false positives — 14
read the service's own key from the environment to call that service's own API (four of them the same author's four
repositories), two are test files, one an example page fetching a CDN texture, one the project's own `curl | bash` installer
quoted in generated page text, one defensive prose naming the commands to block, one a settings hint in a string. `EXFIL-001`
cannot tell "own key to own API" from "key to somewhere else"; the connector pass of the judge got that control in P-049, the
static rule has none. The same discount applies to the competitors' 84 and 236. Changing `EXFIL-001` on this evidence
would make the 400 no longer held out for the static rules; the next draw from newer repositories is the set to measure it on.

