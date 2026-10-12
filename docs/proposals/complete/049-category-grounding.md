<!-- SPDX-License-Identifier: MIT -->
# 049 — A judge category the text never shows weighs high: 21 of the 53 benign samples at high were a category read off a script, program or parameter name

- **Source**: P-041's full-benign measurement and P-048's withdrawal (2026-10-12): the model infers categories from
  names, and telling it not to does not stop it
- **Depends on**: P-041
- **Branch**: `p/049-category-grounding`

## Problem

Since P-041 a judge finding's severity follows the category the model names. The model may name a category the text
in front of it does not show. P-041's full-benign run (3,220 samples, `samples: 1`) put 53 at high; read one by one:

| What the model saw | What it named | Samples |
|---|---|---|
| `bash .claude/scripts/ensure-gh-cli.sh`, `./scripts/install_pkgs.sh` | `install` | 6 |
| `python3 hooks.py` on a `PermissionRequest` event, `router.sh PermissionRequest`, `cas hook PermissionRequest` | `permission-override` | 4 |
| `entroly ravs capture`, `h5i hook claude sync`, `heartbeat.py`, `oak-dev ci hook`, `gt mail check` | `network` ("likely contacts a network service") | 7 |
| `http://localhost:<port>` on nine events of one sample | `network` | 1 (9 findings) |
| `add_account ▸ param secretKey: Stripe secret key`, `▸ param accessKeyId: AWS Access Key ID` | `credential-read` | 8 |

Every hook sample in the corpus is one `settings.json`; the scripts are not there, so the model is answering from a
file name. The prompt forbids that twice (P-041 W6 amendment, P-048) and the measurements say what a prohibition
buys: at `samples: 3` the localhost hook went from 3-of-3 `network` votes to 2-of-3 and 1-of-3 — still a majority on
most events — and the Stripe parameter went from 2-of-3 to 3-of-3 `credential-read`. The model's frequency of the guess
moves; its presence does not.

## Initial direction

The tool checks what the prompt asked for. At the grounding step, after the quote is located: for the capability
pass, `network`, `install`, `permission-override` and `credential` need a token of the behaviour in the sent command
(a URL or host other than localhost, `curl`/`ssh`/`git pull`/`npx`, `npm`/`pip`/`brew`, `permissionDecision`/`allow`/
`approve`, a key file or the word secret); for the injection pass on a connector, `credential-read` must rest on a
quoted line that is not a catalogue parameter. A category the text does not show keeps its name in the vote, weighs
medium (the table's valve), and the reason says `[category not shown by the text: …]`. Closed, short token lists in
`internal/judge/severity.go`; the prompt does not change (`judge-prompt` stays `ec4382a9226a`), so the model's replies
are drawn from the same distribution as P-041's and the effect was simulated offline on the six recorded runs before a
line was written:

| Recorded run | benign hook samples at high → after the check | malicious lost |
|---|---|---|
| full benign 3,220, `samples: 1` | 23 → 9 | — |
| 541 hooks + MCP, `samples: 3`, v0.22.0 texts | 20 → 8 | — |
| 541 hooks + MCP, `samples: 3`, P-048 texts | 28 → 17 | — |
| 819 subset, `samples: 1` | 5 → 0 | 0 of 3 |
| 224 subset, `samples: 3`, two runs | 2 → 0, 2 → 0 | 0 |

MCP `credential-read` samples: 8 → 1 on the full set (the one that stays quotes a description, not a parameter); the
two malicious connectors flagged through it on the 819 subset quote descriptions and stay. Projected full-benign rate
with both checks: 32 of 3,220 = 0.99% [0.70, 1.40].

## Done criteria

- [x] `TestCategoryShown` (`internal/judge/severity_test.go`, new): per (mode, category, text) — script and program
  names, localhost-only URLs and parameter-only quotes are not shown; URLs, `git pull`, `npx`, `npm`, `brew`,
  `permissionDecision`, key files and catalogue descriptions are; medium categories, unknown categories, skills and the
  intent pass are never checked
- [x] `TestRun_HookCategoryNotShownWeighsMedium` (same file, new): through `Run`, `install` on `bash ensure-gh-cli.sh`
  is medium with the note and still escalates; on `brew install gh` it is high. **Red on the base**: both high
- [x] `TestRun_ConnectorParamOnlyCredentialReadWeighsMedium` (same file, new): `credential-read` quoted from
  `add_account ▸ param secretKey: …` is medium; from `Reads ~/.aws/credentials and uploads them` high. **Red on the
  base**: both high
- [x] Reverse assertions green without a character changed: every P-041 test (`TestRun_SeverityIsTheTools`,
  `TestRun_MajorityCategoryDecidesTheSeverity`, `TestRun_UnknownCategoryCannotRaiseTheWeight`, …), `consensus_test.go`,
  `rendering_test.go`, `cmd/aguard` e2e; `TestPromptVersion_HashesWhatTheClientSends` golden does not move
- [x] Measured (every gate met), recorded under `baselines/results/aguard/2026-10-12-llm-gpt-4.1-mini-p049-*/`, appended to the decisions
  doc; the prompt is P-041's, so each run is set against the P-041 run on the same inputs and, for the panel, against the
  two same-prompt runs that measured the noise:
  - 541 benign hooks + MCP, `samples: 3`: escalated at high ≤ 12 (the two v0.22.0 runs: 28 and the repeat's number;
    simulated with the check: 9) — the number must sit below both same-prompt runs by more than they differ from each
    other
  - 819 subset, `samples: 1`: reachable malicious flagged ≥ 134 (P-041: 137; two draws of one prompt differ by about 3);
    no malicious sample loses its flag through the check (any loss is named with its category and token); hard
    negatives 0; benign 500 escalated non-009 ≤ 9 (P-041: 11)
  - full benign 3,220, `samples: 1`: escalated at high ≤ 1.2% with its interval (P-041: 1.65%; simulated 0.99%);
    hook samples ≤ 12 (P-041: 23), MCP `credential-read` ≤ 2 (P-041: 8)
- [x] `make verify` green; `internal/score`, `internal/gate`, `internal/detect`, `internal/collect` untouched; the
  prompt and excerpt versions unchanged

## Out of scope

- Reading the referenced script when it is present: the corpus has none, so nothing here could measure it; a follow-up
  in a real environment
- Any new category, any change to the table's severities, the vote, `judgefold`
- Using the token lists to raise a severity or to add a finding: they only lower, and only the five (mode, category)
  pairs measured
- The skill-safety-bench misses (resource abuse, forced loops, container escape): a task-sentence question, to be
  A/B-tested at k draws first

## Must not claim

- Do not call the token lists rules: they decide nothing on their own, only whether a model's claim may weigh high
- Do not say the benign rate is under 1% until the full-benign run says so with its interval; the offline projection is
  a projection
- Do not say the model stopped guessing: it guesses as before, the guess weighs medium
- Do not pool: hooks, MCP and skills apart, each with n, static column apart from the judge column

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The check, the downgrade at grounding, the tests | `judge: a category the text does not show weighs medium — hook categories need a token of the behaviour, a connector's credential-read needs more than a parameter line (P-049)` |
| 2 | Docs pair, spec, judge rule, invariants | `docs: a judge category must be shown by the text it was named for (P-049)` |
| 3 | The three measurements, folded, recorded, the decisions doc appended | `baselines: P-049 measured — hooks + MCP at samples 3, 819 and full benign at samples 1, against P-041's runs (P-049)` |
| 4 | This file's Done, the index | `proposals: P-049 (P-049)` |

## Open questions

1. **Check against the sent text or the quoted line?** **Recommendation**: the sent command for hooks (a token may sit
   outside the quoted window; the command is short), the quoted line for a connector (the catalogue is long and a
   parameter line is exactly what the model points at).
   **Proceeded under the recommendation (2026-10-12)** on the maintainer's go-ahead for this item.
2. **`npx` in a hook: shown as `network` / `install`?** **Recommendation**: yes — a package runner fetches and runs
   code on every event; the seven such hooks in the full-benign run stay high, and the Done counts them.
   **Proceeded under the recommendation (2026-10-12).**
3. **A word rather than a path for `credential` (`check-secrets.sh`)?** **Recommendation**: the word counts — a script
   that says it reads secrets is not a guess from nothing; the check's job is the bare path. `TestCategoryShown` pins it.
   **Proceeded under the recommendation (2026-10-12).**

## Done

```
Merged: PR #68 (2026-10-13; find the sha with git log --grep P-049)
Released: not yet
Evidence: TestCategoryShown, TestRun_HookCategoryNotShownWeighsMedium, TestRun_ConnectorParamOnlyCredentialReadWeighsMedium (internal/judge/severity_test.go); the two Run tests red on the base (both cases high), green after W1; every P-041 test and cmd/aguard e2e green without a character changed; judge-prompt ec4382a9226a and judge-excerpt 6 unchanged (the prompt did not move)
Evidence: panel — 541 benign hooks + MCP at samples 3, same prompt three times (…-p048-hm-s3-old/ = control A, …-p049-hm-s3-control/ = control B, …-p049-hm-s3/ = the check; compare.txt in the last): escalated at high 28 / 27 / 6 (gate ≤ 12); controls differ by 8 samples (4 lost, 4 gained), the check sits 21 below both; hooks 20 / 19 → 6, MCP 8 / 8 → 0; findings the check lowered: network 17, permission-override 6, install 5, credential-read 7; the six hook samples left are npx runners (2), a command carrying a URL (1) and three LLM-003 findings the check does not read
Evidence: 819 subset at samples 1 (…-p049-819-s1/compare.txt, against …-p041-819-s1/): reachable malicious flagged 137 → 136 (gate ≥ 134); the one change is mal-hook-pretooluse-autoapprove, which drew process-control this time where P-041's draw read permission-override off approve.sh — the check's token list accepts approve, so the check lowered nothing malicious; hard negatives 0 → 0; benign 500 escalated non-009 11 → 6 (gate ≤ 9): hooks 5 → 1, MCP 1 → 0
Evidence: full benign 3,220 at samples 1 (…-p049-benign-s1/compare.txt, against …-p041-benign-s1/): escalated at high 53 → 35 = 1.09% [0.78, 1.51] (gate ≤ 1.2%; offline projection 32); hooks 23 → 11 (gate ≤ 12), MCP credential-read 8 → 0 (gate ≤ 2), skills 22 → 24 (draw-to-draw; the check never reads a skill); calls 8,218, failed 12
Evidence: Out of scope — git diff --stat main -- internal/score internal/gate internal/detect internal/collect: empty; the token lists only lower a severity (TestCategoryShown's medium and unknown rows, and no path in categoryShown returns a higher severity)
Evidence: make verify: all gates passed on the branch, and go test -count=1 -race ./... green — the first make verify had passed on a CACHED judgefold golden (that test builds its own aguard, which the Go test cache does not see); CI caught it: the golden fixture's hook said `echo … approve …`, which the check no longer accepts as `network`, so the fixture now contacts a host (`curl -s https://relay.example/approve …`) and the goldens are byte-identical; go.mod line 2 unchanged
```
