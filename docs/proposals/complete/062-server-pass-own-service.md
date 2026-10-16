<!-- SPDX-License-Identifier: MIT -->
# 062 — The server pass calls a server reading its own key a credential read: the own-service control the connector pass has, applied where the static chain can answer it

- **Source**: the maintainer, 2026-10-16 ("好", after P-059's denominator put `LLM-010` at 65 of 261 third-party servers and 142 of 176 escalating votes on `credential-read`)
- **Depends on**: P-053 (the server pass), P-049 (`categoryShown`, the connector pass's own-service control), P-061 (`EXFIL-001`'s destination resolution — the chain this proposal asks), P-059 (the 547-tree denominator)
- **Branch**: `p/062-server-pass-own-service`

## Problem

On the P-059 denominator the server pass flags 95 of 506 benign servers it is asked about, and 142 of the code-search
population's 176 escalating votes name `credential-read`. The shape behind that number is a server reading
`WEATHER_API_KEY` to call `api.weather.com`: what the key exists for, and from a 2,000-byte excerpt the model cannot tell it
from a key sent anywhere. The connector pass got a tool-side control for exactly this in P-049 (a `credential-read` resting
on parameter lines alone weighs medium); the server pass has none.

## Initial direction

Ask the static chain. `detect.CredentialGoesHome(path, text)` runs `EXFIL-001`'s chain over the **whole file** — the named
credentials it reads, the destinations it names after P-061's one-level resolution — and answers true only when every
non-loopback destination belongs to a service a named credential is for, or every destination is loopback. The excerpt
builder sets that answer on each behaviour unit (`sourceUnit.goesHome`); when a `credential-read` vote on the server pass
grounds in such a unit, the finding weighs medium and the note says why (`[category not shown by the text: credential-read
(the credential's own service)]`). One category on one pass; nothing else moves. An artifact-wide variant
(`CredentialGoesHomeAcross`) was built and measured too: aggregating every file's destinations pulls in every file's
unreadable ones, and it clears fewer (9 against 12), so the per-file answer ships and the aggregate stays as a tested
helper for the next reader.

Measured offline first, on the recorded P-059 raws, before any live call: of the 96 flagged benign servers, 74 are
`credential-read` and **12** pass the control. The other 62 split as 21 whose credential read is a shape the chain does
not count (a key from a config object, a settings module), 20 whose network call is in another file, 14 whose destination
matches no credential's name (`calcom` / `api.cal.com`, a Tavily key beside news-site URLs), 5 with an unreadable
destination and 2 with a nameless key. On Cisco's 127 servers: 18 of 108 flagged are `credential-read`, **0** pass. So
the expected effect is about one in eight of the server false positives, with no recall cost — modest, mechanical, and
the measurement is what this proposal is for.

## Done criteria

- [x] `TestCredentialGoesHome` (nine shapes, both directions), `TestCredentialGoesHomeAcross`,
  `TestRun_ServerOwnKeyCredentialReadWeighsMedium` (own key home → medium with the note; the same key away → high; a
  different category on the same file → high)
- [x] Live at `samples: 3`: the 547 benign servers and the 127 Cisco servers with the control, against P-059's runs
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, the decisions document

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-16, same work lists and config as P-059 (`…-p062-benign-servers-s3/`,
`…-p062-cisco-servers-s3/`; raw in `aguard-judge-raw-2026-10-p062.tar.gz` on v0.25.0):

| population | asked | `LLM-010` ≥ high, P-059 → P-062 | any vote ≥ high |
|---|---:|---|---|
| official + examples | 69 | 5 → 5 | 9 → 10 |
| third-party, code search | 261 | 65 → **60** | 127 → 125 |
| third-party, repos created ≥ 2026-09-20 | 189 | 26 → 26 | 74 → 74 |
| all | 519 | **96 → 91** | 210 → 209 |
| Cisco's 127 malicious servers (recall) | 127 | 108 → 105 (2 incomplete) | — |

18 `LLM-010` findings carry the own-service note. The sample-level change, 96 → 91, is **inside the `samples: 3` noise band**
(two same-prompt draws of 541 samples differed by 8 in P-049; the P-059 and P-062 runs are two draws), and so is Cisco's
108 → 105 (the same three votes could go either way; the control cannot touch them, since 0 of 18 Cisco `credential-read`
files pass it offline). Read together with the offline count — 12 of 96 on the same raws — the control does what it says
and removes about one in eight of the server false positives, and the live run cannot show that against the draw noise. It
ships because it is a correctness rule, the connector pass's twin, with a zero recall cost by construction, not because it
moved a headline number. What would move it is in the breakdown above: 41 of the 74 `credential-read` flags rest on a read
the chain does not count or a call in another file, which is a different control (a credential's path through a config
object, an artifact-wide answer that does not inherit every file's unreadable destinations) and a separate proposal, if ever.

## Done

```
Merged: PR #84 (2026-10-16; find the sha with git log --grep P-062)
Released: v0.26.0
Evidence: TestCredentialGoesHome, TestCredentialGoesHomeAcross (internal/detect); TestRun_ServerOwnKeyCredentialReadWeighsMedium (internal/judge) — red on bd01158
Evidence: offline on P-059 raws: 12 of 96 flagged benign servers pass, 0 of Cisco's 18; live samples 3: 96 → 91 of 519, Cisco 108 → 105, both inside the draw noise; raw aguard-judge-raw-2026-10-p062.tar.gz on v0.25.0
Evidence: judge-prompt cc2cab18585d and judge-excerpt 7 unchanged; make verify: all gates passed
```
