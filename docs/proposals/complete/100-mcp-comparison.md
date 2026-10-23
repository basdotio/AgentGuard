<!-- SPDX-License-Identifier: MIT -->
# 100 — MCP server source: BAS Guard and cc-audit on Cisco's malicious servers, the benign denominator, and a held-out benign set

- **Source**: the maintainer, 2026-10-22/23 ("MCP 服务器和 hook 两类还没有和竞品做过对比 … 好", then "先做完"). A measurement
  record, not a change.
- **Depends on**: P-060 (the September/October comparison this completes on the server side), P-098 and P-099 (the
  versions measured)
- **Branch**: `p/100-mcp-comparison`

## What was run

cc-audit 3.23.9 (the release asset `cc-audit-v3.23.9-aarch64-apple-darwin.tar.gz`, sha256 `8ad5e0b0…cbfbe4` as GitHub
lists it; the default `cc-audit init` policy, `sha256:03fa4186…c67c`, the same as P-060's runs) through the
`ccaudit` adapter, static. BAS Guard with the judge, gpt-4.1-mini at `samples: 3`. Corpus `6d7aa48e`.

Three sets:

- **Cisco's 127 malicious servers** (`mal-mcp-ci-*`): used to tune the judge since P-053 — a development set.
- **543 benign server trees**: P-059's 547 less four no longer in the corpus — a development set (P-059, P-062, P-098, P-099).
- **697 benign server trees no measurement had touched**: the corpus's re-harvest of 2026-10-21 (470 Python, 227
  TypeScript/JavaScript) less P-059's 547. Read in totals only. The judge's server pass asks Python servers only, so on
  the 227 others BAS Guard is static.

Cisco's own `mcp-scanner` is not a column: P-060 found its behavioral analyzer flagging 2 of these 127 because its
response validator discards part of the model's answers.

## Readings

| set | n | BAS Guard, with the judge | BAS Guard, static | cc-audit 3.23.9 |
|---|---:|---:|---:|---:|
| Cisco malicious servers: flagged (more is better) | 127 | **103–107** | 22 | 74 (58%) |
| benign server trees, development: flagged (fewer is better) | 543 | **~110** (20%) | 78 (14%) | 268 (49%) |
| benign server trees, held out: flagged (fewer is better) | 697 | **104 (14.9%)** | 94 (13.5%) | 522 (74.9%) |

- Cisco: 107 at v0.31.0, 103 at P-098 (two lowered by P-098, the rest a rerun's movement);
  `results/aguard/2026-10-23-llm-gpt-4.1-mini-p098-cisco-servers-{main,branch}-s3/`.
- Development benign: P-098's branch run (124) folded with P-099's static rules (78), as P-099 records.
- Held out: `results/aguard/2026-10-23-llm-gpt-4.1-mini-p100-mcp-heldout-s3/` (P-098 + P-099; raw archive
  `aguard-judge-raw-2026-10-p100.tar.gz` on v0.31.0, sha256 `7ab3b9d7…a457`); static `results/aguard/2026-10-23-p099-mcp-heldout-changed/`.
  Two samples had a judge call that did not finish.
- cc-audit: `results/ccaudit/2026-10-23-p100-{cisco-servers,benign-servers,mcp-heldout}/`. The benign-servers run
  scored 545 of the 547 listed (two errors, both no longer in the corpus); the 543 column counts the 543 both tools read.

## Reading

On the held-out servers BAS Guard flags one in seven; cc-audit flags three in four. The judge adds 10 of the 104 there,
against 31 on the development trees — P-098 was fitted on the latter, and this is the first reading of it on servers it
never saw. On Cisco's malicious servers the judge is what catches them (22 static, 103–107 with it).

## Must not claim

The malicious column is one vendor's synthetic evaluation set, used for tuning; there is no held-out malicious server
set. The judge's numbers hold for this model, this day and this sampling.

## Done

```
Merged: PR #126 (2026-10-23; find the sha with git log --grep P-100)
Evidence: results/ccaudit/2026-10-23-p100-*; results/aguard/2026-10-23-llm-gpt-4.1-mini-p100-mcp-heldout-s3 (raw archive on v0.31.0)
Evidence: schema 89 files / 37,122 lines
```
