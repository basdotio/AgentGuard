<!-- SPDX-License-Identifier: MIT -->
# 089 — An injection-pass credential-read weighs medium when the lines around the quote send the key only to its own service

- **Source**: the maintainer, 2026-10-21 ("你先修1类": the judge false positives the benign-label review sheet named)
- **Depends on**: P-062 (the server pass asks the static chain whether a key goes home), P-061 (`detect.CredentialGoesHome`)
- **Branch**: `p/089-a-key-sent-home-in-an-example`

## Problem

Among the 21 review samples still high on main, a `SKILL.md` that documents an OpenAI call — `` Authorization: `Bearer
${process.env.OPENAI_API_KEY}` `` in a `fetch` to `api.openai.com` — was judged a credential read by the injection pass,
three votes. That is how the API is used. The server pass already asks the static chain whether a file's named key goes
only to its own service (P-062), over the whole file; the injection pass did not ask at all.

## Initial direction

On the injection pass, a `credential-read` vote weighs medium, with the reason, when the twelve lines either side of
its quote — in the unit the quote landed in — read a named credential and send it only to that credential's own service
(`detect.CredentialGoesHome` on the window). A window, not the whole unit: a long `SKILL.md` documents several services,
and one foreign destination anywhere would hide every example that goes home. A key sent to a host that is not its
service keeps its weight. No prompt change; `PromptVersion` and `ExcerptVersion` do not move.

## Done criteria

- [x] `TestQuoteGoesHome_AKeyInItsOwnExample`: the same quote is medium sent to `api.openai.com`, high sent to `collector.example.net`
- [x] Live at `samples: 3`, main against the branch, the same day: the benign samples that ever escalated an injection
  credential-read, the 30 review samples, and the 240 SkillHarm trees as the recall guard
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-21, main (`540b641`, v0.30.0) against the branch (`997fa36`), corpus `aceef06e`,
282 samples: 17 benign that ever escalated an `LLM-003` credential-read in a committed run, the 30 review samples, the
240 SkillHarm trees; `results/aguard/2026-10-21-llm-gpt-4.1-mini-p089-{main,branch}-s3/`, raw in
`aguard-judge-raw-2026-10-p089.tar.gz` on v0.30.0.

| | n | main | branch |
|---|---:|---:|---:|
| benign, judge-on high | 42 | 22 | 23 |
| SkillHarm, judge-on high | 240 | 174 | 172 |
| escalated `LLM-003` credential-read lowered by the change | 282 | — | 1 (benign) |

The one finding lowered is the case: `ben-skill-md-choufeng-impersonate-agents-convex`, its OpenAI example, now medium
with "the credential's own service, nearby". No malicious finding was lowered; the SkillHarm 174 → 172 and the benign
22 → 23 are changes on other rules and votes (the run band).

**Reading.** Correct and narrow, like P-087: the shape it removes is real and common in documentation, but at three
votes the injection pass rarely calls it a credential read in the first place — one tree in this set. It costs nothing
on the malicious side because an exfiltration sends the key somewhere that is not its service.

## Done

```
Merged: PR #115 (2026-10-22; find the sha with git log --grep P-089)
Evidence: results/aguard/2026-10-21-llm-gpt-4.1-mini-p089-{main,branch}-s3: one benign credential-read lowered (the OpenAI example), none malicious; SkillHarm 174 → 172 of 240 (run band)
Evidence: raw aguard-judge-raw-2026-10-p089.tar.gz on v0.30.0 (sha256 ebbf730d…5717, 296,545 bytes); schema round-trip 72 files / 33,416 lines; make verify: all gates passed
```
