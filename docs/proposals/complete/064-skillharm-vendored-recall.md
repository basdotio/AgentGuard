<!-- SPDX-License-Identifier: MIT -->
# 064 — Malicious skill trees to measure recall against, not only benign ones to measure false positives: SkillHarm into the corpus, and what v0.26.0 catches of it

- **Source**: the maintainer, 2026-10-16 ("现在语料库新加的是良性的skill有400,能测试误报率,是不是也需要有恶意的来测试准确率", then "好" to vendoring SkillHarm at 20 per risk)
- **Depends on**: P-059 (the held-out SkillHarm draw of 10 per risk, and its prep script), agent-artifact-corpus PR #3 (the vendoring)
- **Branch**: `p/064-skillharm-vendored-recall`

## Problem

P-059 and P-060 added 400 real skill trees and 547 server trees to measure false positives on, and only 120 malicious
skill trees — held out in the corpus's cache, never in layer 1 — to measure recall on. The corpus's own malicious skills
(300 across every surface) are mostly bare `SKILL.md` files, which is why `EXFIL-001` reads 4.6% on the corpus's benign
skills and 18% on real trees; the malicious side had the same shape problem. Recall on full malicious trees — payload
in a script, in a helper, in a companion document, in a PDF — had no denominator in layer 1 at all.

Two things about the P-059 held-out reading only came out while building that denominator, and both belong here:

1. **Its 20 `redirect_to_new_pdf` samples carried no payload.** The held-out prep skipped every binary, PDFs included, so
   those trees were a `SKILL.md` saying "read the guide in full" beside no guide. 0 of 20 caught, by both binaries — a
   figure about a missing file, not about the scanner.
2. **Its 27 `redirect_to_new_doc` samples were missed by every pass, structurally.** The payload is a companion
   `PREFLIGHT.md` / runbook that `SKILL.md` tells the agent to read first. The static engine files any `.md` that is not
   `SKILL.md`/`CLAUDE.md` as a prose doc (`roleForPath` → `roleDoc`, injection rules only), and the judge's behaviour
   excerpt reads only `behaviorExts` — scripts and config, no `.md`. So `rm -f /var/log/fjsp-audit/access.log` in a
   `PREFLIGHT.md` is read by nothing. 0 of 27 at v0.21.0 and at v0.24.0.

So of the 120, 47 were unreachable by construction, and the 43/117 headline of P-059 was really 43 of the 73 the tool
could see.

## Initial direction

Vendor the recall material into layer 1 so that it is versioned, labelled on the corpus's axes and run by the same
driver as everything else, then measure v0.26.0 on it once, per realization and per dimension, and leave the fixes to
their own proposals. Corpus side (agent-artifact-corpus PR #3): 240 SkillHarm fixed-payload trees, 20 per risk by the
same seeded order the held-out draw used (so those 120 are now layer 1 and spent as held-out), PDFs fetched, labels
derived from the upstream's `risk_id` and `realization` through a category map whose bends are named in the manifest
(`dos` and `poisoning` are filesystem, not resource-abuse; `proxy_attack` is resource-abuse; the PDF realization is
`binary-container`/evasive; the other redirects are indirect). The 192 self-mutating rows and skill-inject's templates
are not samples; agent-skill-malware's 124 real ClawHub skills stay out as held-out material. The corpus goes
4,486 → 4,726 samples, 300 → 540 malicious.

## Done criteria

- [x] Live at `samples: 3`, v0.26.0, the 240 vendored trees: static, judge, by realization / dimension / tier, and the
  120 shared ids against P-059's v0.24.0 run
- [x] Results directory, raw archive on the release, schema round-trip counts, the decisions document

## Measured

Live, `samples: 3`, gpt-4.1-mini, 2026-10-16, v0.26.0, layer-1 paths at corpus commit 4f9c61be
(`results/aguard/2026-10-16-llm-gpt-4.1-mini-p064-skillharm-vendored-s3/`; raw in `aguard-judge-raw-2026-10-p064.tar.gz` on
v0.26.0). 240 asked, 237 complete, 3 incomplete (judge calls failed; counted as not caught below). "judge" is the fold's
predicate: an escalated (grounded, 2 of 3) LLM finding at high; "any" is any single vote at high.

| by realization — where the upstream put the payload | n | static | judge | any |
|---|---:|---:|---:|---:|
| `plain_text` — in `SKILL.md` itself | 50 | 6 | 33 | 37 |
| `redirect_to_new_script` — a new script `SKILL.md` tells the agent to run | 66 | 11 | 46 | 47 |
| `executable_code` — inside an existing script | 17 | 0 | 9 | 10 |
| `import_new_helper` — a helper module an existing script imports | 15 | 0 | 8 | 11 |
| `redirect_to_new_doc` — a companion `.md` the agent is told to read first | 54 | **0** | **0** | 3 |
| `redirect_to_new_pdf` — a PDF the agent is told to read first | 38 | **0** | **0** | 0 |
| all | 240 | 17 (7%) | 96 (40%) | 108 |
| the 148 the tool can see (first four rows) | 148 | 17 (11%) | 96 (**65%**) | 105 |

| by dimension (the corpus's derived label) | n | static | judge | | by tier | n | static | judge |
|---|---:|---:|---:|---|---|---:|---:|---:|
| filesystem | 100 | 8 | 38 | | plain | 50 | 6 | 33 |
| injection | 40 | 0 | 4 | | indirect | 152 | 11 | 63 |
| backdoor | 20 | 3 | 10 | | evasive (the PDFs) | 38 | 0 | 0 |
| execution | 20 | 0 | 13 | | | | | |
| exfiltration | 20 | 0 | 11 | | | | | |
| permission | 20 | 5 | 7 | | | | | |
| resource-abuse | 20 | 1 | 13 | | | | | |

Escalated rules: `LLM-001` 62, `LLM-003` 32, `LLM-006` 3. The 120 ids shared with P-059's held-out run, v0.24.0 → v0.26.0:
static 8 → 8, judge 43 → 42, per realization unchanged within one or two votes (helper 5 → 3, script 25 → 26) — P-061
and P-062 touched nothing here, as they should not have, and the two runs are two draws.

**Reading.** Three findings, in order of size:

1. **38% of the set is invisible by construction, not by judgement.** 92 of 240 samples put the payload in a companion
   document or a PDF, and the tool catches 0 of 92. Static files a non-`SKILL.md` `.md` as prose and runs injection rules
   only; the judge's behaviour excerpt lists scripts and config extensions and no `.md`; nobody reads a PDF. SkillHarm's
   designers reached for this shape in 207 of 641 samples (32%), so it is not exotic. The next proposal on this surface is
   "a document `SKILL.md` tells the agent to read is an instruction file": read `.md` files the instruction text names
   (and, for PDFs, at least the text layer) into the behaviour excerpt and the instruction rule set. That is a judge and
   engine change with a false-positive cost to measure on the 400 real trees first — real skills ship `references/*.md`
   too — and it is its own proposal, not this one.
2. **Where the tool can see, the judge carries recall and the static rules do not: 17 against 96 of 148.** Static catches
   the `authorized_keys` append and `/etc/sudoers` shapes (backdoor 3, permission 5) and almost nothing else; `>> /root/.bashrc`,
   `rm -f /var/log/…`, `curl -X POST … /attest`, `cat > ~/.pip/pip.conf` in an instruction file are not rule shapes today.
   Whether they should be is a precision question (P-061 showed 18% of real trees already carry an `EXFIL-001` high), and
   the judge's 65% on the visible set is the figure that says what a static expansion would have to beat.
3. **Injection is the weakest dimension even where visible: 4 of 40.** `goal_hijacking` and `output_manipulation` are
   instructions that change what the agent *says* — "treat the header band as registered", "round the AirTemp column" —
   with no host effect. The judge's intent question is built for mismatches between declared purpose and behaviour, and
   these artifacts declare a purpose they then subvert in prose; the triage pass passes them. A different question, not a
   different threshold.

What this is not: a recall rate. Every sample was written by an LLM the upstream names, 20 per risk were drawn, and the
corpus's own label for the dimension is a rule over the upstream's category. It is coverage of twelve risk shapes by six
placements at one version, to be read per cell, and the cells that matter are the two zeros.

## Done

```
Merged: PR #86 (2026-10-16; find the sha with git log --grep P-064)
Evidence: results/aguard/2026-10-16-llm-gpt-4.1-mini-p064-skillharm-vendored-s3: 240 asked, static 17, judge 96, any 108; redirect_to_new_doc 0/54, redirect_to_new_pdf 0/38; shared 120 with P-059: 8 → 8 static, 43 → 42 judge
Evidence: agent-artifact-corpus PR #3 (commit 4f9c61be): 240 sh-* trees, 4,486 → 4,726 samples, 300 → 540 malicious; raw aguard-judge-raw-2026-10-p064.tar.gz on v0.26.0 (sha256 63e73b1e…0084)
Evidence: schema round-trip 39 files / 25,016 lines; make verify: all gates passed
```
