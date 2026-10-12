<!-- SPDX-License-Identifier: MIT -->
# 048 — Two category texts put 33 of 3,220 benign samples at high: a hook that talks to localhost or whose program's name sounds like networking, and a tool parameter that takes the service's own key

- **Source**: P-041's full-benign measurement (`baselines/results/aguard/2026-10-12-llm-gpt-4.1-mini-p041-benign-s1/compare.txt`,
  2026-10-12); the two follow-ups its Done names
- **Depends on**: P-041
- **Branch**: `p/048-judge-network-and-own-key`

## Problem

P-041 made a judge finding's severity follow a closed category the model names. Its full-benign run (3,220 samples,
`samples: 1`, gpt-4.1-mini) put 53 samples at high; the gate was 0.5% and the run gave 1.65%. Two categories account for
33 of the 53, and reading each one shows the table's text, not the model, is what put them there:

- **`network` on hooks (`LLM-008`), 25 findings on 13 samples.** Nine are a URL on `localhost:<port>` (one sample, nine
  hook events): the text said "a URL" and a local port is one. About eight are a program whose name suggests
  networking — `oak-dev ci hook`, `entroly ravs capture`, `h5i hook claude sync`, `ox agent hook`, `gt mail check`,
  `heartbeat.py`, `bob-check-update.js` — and the model wrote "likely contacts a network service" each time, from the
  name. Six are `npx <package>@alpha` on every event and one is `git pull`: those do fetch code from the network and are
  not this proposal's business.
- **`credential-read` on MCP tool descriptions (`LLM-003`), 8 samples.** Every one is a tool parameter through which
  the user hands the tool its own service's credential — `secretKey: Stripe secret key`, `accessKeyId: AWS Access Key
  ID`, `password: ServiceNow password`, `applePrivateKey: P8 private key contents`. The text said "reads keys, tokens,
  passwords … with no stated reason", and a parameter named for a key fits it; it is not the tool reading the user's
  secrets from disk or environment, which is what the category is for.

No malicious sample on the 819 subset has its flag resting on either category alone (checked on the P-041 run: every
static-missed malicious sample flagged through a hook `network` or a connector `credential-read` also carries another
high category), so tightening the two texts risks little recall there; the measurement says how little.

## Initial direction

Two sentences in `internal/judge/severity.go`, nothing else in the code: `network` on a hook means a host outside this
machine (a URL, hostname or IP other than localhost, or a network tool, or a package runner that fetches code), and
neither a localhost URL nor a program's name is it; `credential-read` excludes a tool parameter that takes the service's
own key or password. `judge-prompt` moves. Measured on the 819 subset and the full benign set at `samples: 1`, the two
runs P-041 took, against P-041's folds.

## Done criteria

- [x] `TestPrompt_EveryTableCategoryIsOffered` and `TestSeverityTable_EveryPassCategoryIsDecided` green without a
  character changed (the names and severities are the same; only two descriptions changed); `judge-prompt` moves exactly
  once (`ec4382a9226a` → `83ca81a0755b`), `judge-excerpt` stays 6
- [x] Measured (every gate below MISSED; see Rejected), recorded under `baselines/results/aguard/<date>-llm-gpt-4.1-mini-p048-*/` with `run.yaml`, `judge.jsonl`,
  `scorecard.txt` and a `compare.txt` against the matching P-041 directory, and appended to
  `docs/decisions/judge-comparison-2026-09-25.zh-CN.md`:
  - full benign 3,220, `samples: 1`: hook samples with an escalated `LLM-008` `network` at high ≤ 8 (P-041: 13, of
    which the six `npx` and the one `git pull` may stay); MCP samples with an escalated `LLM-003` `credential-read` at
    high ≤ 1 (P-041: 8); samples escalated at high from any non-009 rule ≤ 1.0% (P-041: 1.65%), with its interval
  - 819 subset, `samples: 1`: reachable malicious flagged ≥ 134 (P-041: 137, and two `samples: 1` runs of one prompt
    differ by about 3 on this count); every malicious sample that was flagged in P-041's run and is not flagged here is
    named, with the category it lost; hard negatives escalated 0 (P-041: 0); benign 500 escalated non-009 ≤ 9 (P-041: 11)
- [x] `make verify` green; `internal/judge` outside `severity.go`, `internal/score`, `internal/gate`, `internal/detect`
  untouched

## Out of scope

- `npx` / `pipx` / `git pull` in hooks: they fetch and run code from the network and stay `network` (or `install`)
- Hook scripts named only by path (`install` / `permission-override` read from `ensure-gh-cli.sh`, `permission-retry.cjs`:
  10 of the 53): that needs the script's text in the excerpt, a different proposal
- `credential-read` on skills and in the intent and explain passes: the sentence is shared, the exclusion names a tool
  parameter, which only a connector has; the 819 run measures that nothing else moved
- Any change to the table's names, severities, the vote, `judgefold`, or `samples`

## Must not claim

- Do not say the benign rate is "fixed" or "under 0.5%" unless the full-benign number says so with its interval; the
  target of this proposal is 1.0%
- Do not say the model stopped inferring from names: the text forbids it twice now and a single draw still may; the
  number is what is reported
- Do not pool: hooks, MCP and skills are reported apart, each with n

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The two descriptions | `judge: network on a hook means a host outside this machine; credential-read is not a tool parameter that takes the service's own key (P-048)` |
| 2 | The two measurements, folded, recorded, the decisions doc appended | `baselines: P-048 measured — 819 and full benign at samples 1 against P-041's runs (P-048)` |
| 3 | This file's Done, the index | `proposals: P-048 (P-048)` |

## Open questions

1. **A `notify` category for hooks that post to a webhook?** **Recommendation**: no — the 25 findings held no webhook
   post at all; the plan's guess was wrong and the data says localhost and names. Adding a category nobody triggered
   would be a text nobody measured.
   **Proceeded under the recommendation (2026-10-12)** on the maintainer's go-ahead for this item.
2. **Keep `npx <pkg>` and `git pull` in hooks at high?** **Recommendation**: yes. A hook that fetches a package or a
   repository on every event and runs it is the supply-chain shape the capability pass exists to name; the corpus's
   "benign" is an assumed label, and these seven are reported in the Done as the ones that stay.
   **Proceeded under the recommendation (2026-10-12).**
3. **Measure at `samples: 3` too?** **Recommendation**: no. The vote does not change what a category means, and the
   benign cost this proposal addresses was measured at `samples: 1`; the 819 and full-benign runs are the matching pair.
   **Proceeded under the recommendation (2026-10-12).**

## Rejected

Withdrawn by the measurement (2026-10-12); the texts are reverted (b7e1c63), `judge-prompt` is back at `ec4382a9226a`, and
the four runs stay under `baselines/results/aguard/2026-10-12-llm-gpt-4.1-mini-p048-*/` as the record of what a wording
change to the category table can and cannot do.

```
Merged: PR #67 (2026-10-12; find the sha with git log --grep P-048)
Released: nothing to release — the code on main is v0.22.0's
Evidence: full benign 3,220, samples 1 (…-p048-benign-s1/compare.txt): escalated non-009 53 → 59 = 1.83% [1.42, 2.36] (gate ≤ 1.0%); hook `network` findings 25 → 26 (localhost 9 → 6, name-guess 7 → 8, npx 6 → 11, url/tool 3 → 1); MCP `credential-read` samples 8 → 8; between the two draws 15 escalated samples were lost and 20 gained out of about 55 — the churn of samples: 1, larger than any effect the texts could have
Evidence: 819 subset, samples 1 (…-p048-819-s1/compare.txt): reachable malicious flagged 137 → 135 (gate ≥ 134; lost mal-hook-pretooluse-autoapprove and mal-skill-sg-definition-of-done-push, neither through the two categories); hard negatives 0 → 1 (socket-client-plain again); benign 500 escalated 11 → 15 (gate ≤ 9), hook `network` 9 → 3 inside the same churn
Evidence: the decisive pair, 541 benign hooks + MCP at samples 3, old texts (…-p048-hm-s3-old/) against new (…-p048-hm-s3-new/compare.txt in old): escalated 28 → 34 (hooks 20 → 28, MCP 8 → 6); the `npx` clause added hook flags (alibaba-tron, commercetools-merchant-center, sentinel-api-testing, playwright-ts-lib); the localhost clause turned miniverse's 3-of-3 `network` votes into 2-of-3 and 1-of-3, which still escalate; the parameter clause was ignored outright (the Stripe tool went 2 of 3 → 3 of 3 `credential-read`)
Evidence: what the model does with a "NOT this" clause: it lowers the share of draws that name the category, it does not remove it; under a majority bar that is not enough. The 25-finding `network` class is not a wording problem — the reading of localhost and of program names is the model's at every draw. The hook problem needs the script's text in the excerpt (P-041's Done, follow-up two) or a different pass, not another sentence
Evidence: make verify: all gates passed on the branch after the revert; git diff --stat v0.22.0 -- internal/judge: empty
```
