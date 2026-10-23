<!-- SPDX-License-Identifier: MIT -->
# 099 — `urllib.parse` is not a network client, and a URL literal needs a host

- **Source**: the maintainer, 2026-10-23 ("接着你的方案做": after P-098, the static half of the benign-server false positives)
- **Depends on**: P-012 / P-095 (the network leg and its URL-literal fallback)
- **Branch**: `p/099-a-parser-is-not-a-client`

## Problem

After P-098, 93 of the 543 benign server trees are flagged high statically; 67 by `EXFIL-001` alone. Read one by one,
two groups of them have no network leg at all:

- `networkActionRE` named the bare word `urllib`, so `from urllib.parse import quote, urlsplit` — string handling —
  was an outbound action (13 trees).
- `urlLiteralRE` was the scheme alone, so `startswith("https://")` and `"URL must start with https://"` were
  destinations (4 trees).

The rest of the 67 are P-061/P-074 ground (a server's own key to a destination the chain cannot read or match), or a
child process given the environment in a spelling `envChildCopyRE` does not list; neither is touched here.

## Initial direction

`urllib` counts only in its sending forms: `urllib.request`, `urllib.urlopen`/`urlretrieve`, `urllib2`, `urllib3`,
`from urllib import … request|urlopen|urlretrieve`, a bare `import urllib`. A URL literal needs a character after the
scheme that can start a host — a templated host (`f"https://{instance}/…"`) still counts, as an unreadable destination.
`networkCapableRE` (P-095) is built on `networkActionRE` and follows it. `rulesEpoch` 8 → 9.

## Done criteria

- [x] `TestDetect_AParserIsNotAClient`: `urllib.request`, `from urllib import request`, `urllib3`, a templated host and a
  spelled-out host still chain; `urllib.parse` and a scheme with no host do not
- [x] Static over every skill tree, the held-out sets, the fresh 200 and every non-skill sample, main against the branch;
  per-sample check that no malicious or hard-negative sample's `EXFIL-*` findings change
- [x] `make verify`; `.claude/rules/detect.md`

## Must not claim

Not "server false positives fixed": 78 of 543 benign server trees stay high statically, most of them API clients whose
destination the chain cannot read (P-074's ground).

## Measured

Static, corpus `6d7aa48e`, main (`results/aguard/2026-10-23-p099-*-main`, the static rules of P-095) against the branch
(`results/aguard/2026-10-23-p099-*-changed`).

| | n | main | branch |
|---|---:|---:|---:|
| malicious skill trees (development) | 393 | 122 | 122 |
| held-out malicious | 243 | 80 | 80 |
| every non-skill sample: malicious | 156 | 39 | 39 |
| benign skill trees (development) | 2,879 | 146 | 145 |
| held-out ClawHub benign | 216 | 6 | 6 |
| freshly sampled benign skills | 200 | 25 | 25 |
| every non-skill sample: benign | 1,627 | 195 | **180** |

No malicious or hard-negative sample's `EXFIL-*` findings changed. On the 543 benign server trees: static 93 → 78; with
the judge, folding P-098's live run (`2026-10-23-llm-gpt-4.1-mini-p098-benign-servers-branch-s3`) with each static
result, 124 → 110 (22.8% → 20.3%).

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The two narrowings, the test, the measurement | P-099: urllib.parse is not a network client, and a URL literal needs a host; measured |
