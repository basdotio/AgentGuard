<!-- SPDX-License-Identifier: MIT -->
# 061 — EXFIL-001 cannot tell a key sent home from a key sent away: the destination control misses most real scripts, and matches the wrong label when it does

- **Source**: the maintainer, 2026-10-16 ("你先测试一下这 20 个，如果加规则测试下是有效的就提案然后 pr"), after P-060's label review found 20 of 20 flagged real skill trees to be the key going to its own service
- **Depends on**: P-059 (the 400 recent skill trees and the held-out sets), P-060 (the review), the destination-consistency downgrade that already existed in `internal/detect/destination.go`
- **Branch**: `p/061-exfil-destination-precision`

## Problem

`EXFIL-001` fires when one file reads a credential and makes an outbound request. On the corpus's benign skills — mostly a
bare `SKILL.md` — it fires on 4.6%; on 400 real skill trees from repositories newer than the corpus it fires on 18%, and a
seeded review of 20 of the 72 found every one to be the service's own key going to the service's own API, a test file, or
prose. A destination-consistency downgrade already existed (key named `DEEPL_API_KEY`, every destination on `deepl`), but
on those 20 it fired once. Three reasons, read off the chain's own state:

1. **A destination written as a variable made the whole file unverifiable.** `curl "$API/credits"`, fetch(`${BASE}/v1`),
   `requests.get(url)`, `API_BASE + "/images"` — the ordinary shape of a script that sets its base URL once at the top —
   set `destUnreadable`, and one unreadable line vetoes the downgrade.
2. **Any label of the host could vouch.** `api.openai.com.evil.net` carried the token `openai`, so a key sent there read as
   a key sent home. The opposite defect in the same code: `reelql.tail6c0e2d.ts.net` is reelql's machine, and
   `api.orcarouter.ai` is `ORCA_KEY`'s service, and neither matched.
3. **Two credential-leg shapes read no credential.** `${key}` and `${totalTokens(project)}` are JavaScript template
   locals, matched as "a shell secret named outright" because the `$VAR` branch was case-insensitive; `{ ...process.env, X }`
   and `dict(os.environ, X="1")` seed a child process, the idiom EXFIL-004 already exempts for `.copy()`.

## Initial direction

Fix the control, not the samples. (1) Resolve a destination variable **one level inside the same file**, the way SUP-006
resolves a shell variable: a line that assigns the name an `http(s)` literal supplies the host; a name the file never assigns
stays unreadable; a reference followed by a host-extending character (`${API}.evil.net`) is not resolved; a base that
resolves to loopback keeps the chain on-box. (2) A label vouches only **in a position that can be the service's own**: any label may (`purdue.brightspace.com` is
Purdue's tenant, `otx.alienvault.com` is OTX's product — the first draft of this change allowed only the registrable label
and turned three benign servers high), but not a label followed by a TLD-like label short of the host's end (a whole
domain embedded in a longer host), not the platform's own label under a hosted suffix (`evil.github.io`), and only by
exact equality — a label that merely begins with the token (`openaievil.net`) is a lookalike. (3) The `$VAR` branch of the
credential leg becomes case-sensitive upper-case (`credShellVarRE`), and the child-environment copy is not a leg unless the
same line serialises or sends it (`envChildCopyRE` / `envLeavesRE`). Every change is a downgrade or a narrower leg except the
registrable-label rule, which also **closes** the subdomain hole; `rulesEpoch` 3 → 4.

## Done criteria

- [x] `TestChain_DestinationResolvesSameFileBaseURL` (four resolutions, four reverse assertions: unassigned parameter,
  dot-extended reference, service-named subdomain, the used assignment decides), `TestHostMatchesService` (twelve hosts, both directions),
  `TestChain_CredentialLegPrecision` (four non-reads, three reads kept), `TestChain_ResolvedLoopbackBaseStaysOnBox`;
  the existing `TestChain_DestinationConsistency` reverse cases still hold
- [x] No malicious sample loses an `EXFIL-001`/`EXFIL-003` at high: corpus 300, ClawHub 123, SkillHarm 120
- [x] Measured on the 72 flagged recent trees (the development set — read in P-060, so not held out for this change) and
  on a **fresh seeded draw** of repositories the harvest found and never vendored
- [x] `docs/rules.md` regenerated; `.claude/rules/detect.md` carries the four guards and the two things not to loosen

## Measured

Static, `check --json`, aguard at main (v0.25.0 + P-059/P-060) against this branch:

| set | n | high before → after | `EXFIL-001/003` at high before → after |
|---|---:|---|---|
| recent real skill trees (the development set) | 400 | 72 → 64 | 61 → 53 |
| ClawHub benign + 547 benign servers | 763 | 135 → 128 | 121 → 113 |
| corpus malicious + ClawHub malicious + SkillHarm | 543 | **182 → 182** | **49 → 49** |
| **fresh draw**: 200 repositories the harvest found and never vendored, seed 20261017 (`cache/fresh-skills/`, by `fresh-draw.py` beside the P-059 prep scripts' convention) | 200 | **44 → 38** | 36 → 30 |

Of the 20 reviewed trees, 5 clear (the CDN-texture demo, the two test files, the prose-and-visualisation one, and two
own-base-URL scripts); 15 stay high for reasons the control is right to keep: a base URL set by the user outside the file
(`$ORCA_BASE`), a polling URL taken from an API response, help text naming a third party's domain beside the vendor's key,
a credential whose name carries no service (`SP_TOKEN`), a dynamic key name (`os.environ.get(provider["key_env"])`),
`example.com` in documentation strings beside real calls, and findings that are not `EXFIL` at all (`EXEC-001` on the
project's own installer text, `FS-001` on defensive prose, `PERM-007`). No benign sample in any set gained a high. The first draft of the label rule, which let only the registrable label vouch, turned three benign servers high — `purdue.brightspace.com` for `PURDUE_PASSWORD`, `otx.alienvault.com` for `OTX_API_KEY`, `data-platform.mlit.go.jp` for `MLIT_API_KEY` — a tenant, a product and a ccSLD; that is why the shipped rule is positional rather than positional-and-registrable.

The gain is one in nine of the real-tree false positives on the development set and one in seven on the fresh draw, with recall intact and a spoofing hole closed. What remains is
not a precision bug in this rule but the limits of a one-file, one-level view: the next step on this surface is not another
regex but the judge's own-service control (P-049) applied to the server pass, and a per-author cap in the harvest.

## Done

```
Merged: PR #83 (2026-10-16; PR #82 was closed with its stacked base; find the sha with git log --grep P-061)
Released: v0.26.0
Evidence: TestChain_DestinationResolvesSameFileBaseURL, TestHostMatchesService, TestChain_CredentialLegPrecision, TestChain_ResolvedLoopbackBaseStaysOnBox (internal/detect) — red on 18a9770
Evidence: static: recent 400 72 → 64; fresh draw 200 (seed 20261017) 44 → 38; other benign 763 135 → 128; malicious 543 182 → 182 with 49 EXFIL highs intact; no sample gained a high
Evidence: rulesEpoch 3 → 4; rules 629837fddcba → 8fc8772b2951; make verify: all gates passed
```
