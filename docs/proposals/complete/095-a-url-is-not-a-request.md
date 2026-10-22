<!-- SPDX-License-Identifier: MIT -->
# 095 — In a script with no way to send, a URL literal is not the network leg

- **Source**: the maintainer, 2026-10-22 ("好" to: look at the static false positives, after the judge was ruled out — iron rule #2 forbids a judge from lowering a static finding)
- **Depends on**: P-012 (the URL literal counts in scripts), P-061 / P-074 (the destination check and its rejected extension)
- **Branch**: `p/095-a-url-is-not-a-request`

## Problem

Statically, 38 of 200 freshly sampled real GitHub skills are flagged high; 30 of those 38 carry `EXFIL-001`/`-003`. Read
one by one, most are not API clients at all: the network leg is a URL that is only text — a usage string
(`"支持: https://www.douyin.com/video/xxx"`, ten findings in one author's five skills), a secret scanner's Discord-webhook
regex, `startswith(("http://", "https://"))`, an SVG `xmlns`, an install hint in an error message. P-012 made a URL
literal in a script count as egress because it may be "the visible half of a call whose client the verb list does not
know" — but these files have no client of any kind.

Over every corpus run, 217 of 522 benign high `EXFIL-001`/`-003` findings sat in a file with no outbound action, against
9 of 51 malicious (7 of those 9 are evasion spellings the engine's normalisation reveals; the other 2 use Node's
`https.request`, which the action list does not know).

## Initial direction

The URL literal stays a leg in a real script only when the file can send: some non-comment line matches
`networkCapableRE` — `networkActionRE` plus the clients it does not name (`require`/`import` of `https`, `http2`, `net`,
`tls`, `axios`, `node-fetch`, `got`, `undici`, `ws` …; Python `import`/`from` of `requests`, `httpx`, `aiohttp`, `urllib3`,
`http.client`, `socket`, `websockets`, `smtplib`, `paramiko` …; `https.request(`, `XMLHttpRequest`, `new WebSocket(`,
`sendBeacon(`, `socket.socket(`, `Net::HTTP`, `Invoke-WebRequest`/`-RestMethod`, `Net.WebClient`). Read over both views
like every leg. An outbound action still counts everywhere, as before. The judge's `CredentialGoesHome` is not touched.
`rulesEpoch` 7 → 8.

## Done criteria

- [x] `TestDetect_InstructionURLLiteralIsNotEgress`: the script case split in two — a token and a URL in a file that
  requires `https` still chain (the literal fallback's reason to exist); the same in a shell file with no client is quiet
- [x] Static over every skill tree, the held-out sets, the fresh 200 and every non-skill sample, main against the branch;
  per-sample check that no malicious sample loses an `EXFIL-001`/`-003` finding
- [x] `make verify`; `.claude/rules/detect.md`

## Out of scope

The judge. Named-key API clients that do send (`GITHUB_TOKEN` to `api.github.com` where P-061 cannot read the
destination) — that is P-074's ground, rejected. Secret-scanner tools whose regexes read as credential reads.

## Must not claim

Not "EXFIL-001 false positives fixed": about a third of the benign load goes; API clients that read a key and call a
destination built at run time remain, by P-074's decision. The capability list is a list: a client hidden behind a
dynamic import (`__import__("url"+"lib")`) is not seen — which hides the same file's action from `networkActionRE` too.

## Measured

Static, corpus `6d7aa48e`, main (P-094, `results/aguard/2026-10-22-p095-*-main`) against the branch
(`results/aguard/2026-10-22-p095-*-changed`), built the same day.

| | n | main | branch |
|---|---:|---:|---:|
| malicious skill trees (development) | 393 | 122 | 122 |
| held-out malicious (ClawHub 123 + SkillHarm fresh 120) | 243 | 80 | 80 |
| every non-skill sample: malicious | 156 | 39 | 39 |
| benign skill trees (development) | 2,879 | 162 | **146** |
| held-out ClawHub benign | 216 | 6 | 6 |
| freshly sampled benign skills | 200 | 38 | **25** |
| every non-skill sample: benign (MCP servers, hooks, configs, …) | 1,627 | 245 | **195** |

No malicious or hard-negative sample loses an `EXFIL-001`/`-003` finding (checked per sample on the raw output). Three
development malicious trees lose the cross-file `EXFIL-002` (low, advisory) and stay flagged by other rules. Benign
flagged high: 79 fewer samples across the corpus; on real GitHub skills sampled fresh, 19% → 12.5%.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The gate, the split test, the measurement | P-095: a URL literal is the network leg only in a script that can send; measured |
