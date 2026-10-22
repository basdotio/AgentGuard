<!-- SPDX-License-Identifier: MIT -->
# 091 — A document the SKILL.md names runs the instruction rules, with one answer to which documents those are

- **Source**: the maintainer, 2026-10-22 ("好" to: improve the static rules where the judge cannot reach, starting with the documents a SKILL.md names); the static half P-065 deferred
- **Depends on**: P-065 (the judge's companion pass and its resolver), P-061 (the credential-to-network chain)
- **Branch**: `p/091-named-documents-are-instructions`

## Problem

"Read `PREFLIGHT.md` in full before starting" makes the file an instruction the agent carries out. Since P-065 the judge
reads it; the engine still filed it as a prose document (`roleDoc`) and ran only the injection rules, so a piped
download, a changed package source or an SSH key touched there met no rule — and a scan without `--llm` saw nothing in
the 54 of 240 SkillHarm trees that put the payload in a companion `.md`. The question "which documents does this SKILL.md
name" had one implementation, the judge's; the engine had none.

## Initial direction

The resolver moves from the judge into the engine as `detect.NamedDocs` (same rules: named by path or bare name, through
`FileReach`, `SKILL.md` and licences excluded, one level, the first eight) and the judge's companion pass calls it, so
the rules and the judge give one answer about one tree. A named document is read as two units: the whole
text as a `roleDoc` (the injection rules, as before), and its code only — fenced blocks and indented blocks, every prose
line replaced by a placeholder so line numbers hold (`codeOnly`) — as `roleNamedDoc`, which runs every other rule except
the credential-to-network chain (`EXFIL-001`/`-003`). A document nothing names is unchanged. `rulesEpoch` 4 → 5;
`docs/rules.md` regenerated. No judge behaviour changes (`ExcerptVersion` and `PromptVersion` do not move). One line of
this repository's own audit skill changes: `references/triage.md` wrote its scoring formula with a Greek `Σ` against a
Latin identifier, the mixed-script shape `OBF-005` exists for, and the self-scan now reads that file's code block.

Three versions were measured before this was proposed. The first gave named documents `roleInstruction` outright,
chain included; the second dropped the chain; the third, this one, reads only their code. Each step is in Measured.

## Done criteria

- [x] `TestNamedDocument_RunsTheInstructionRules` (named: full rules; the same line unnamed: injection only),
  `TestNamedDocument_NoSkillMDNamesNothing`, `TestNamedDocument_NoCredentialChain`, `TestNamedDocument_ProseIsNotACommand`
  (a sentence about `curl|bash` is not a finding, the same command in a fence is; line numbers hold); the judge's
  companion tests unchanged and passing; the plugin self-scan clean
- [x] Static, over every skill tree of the corpus and the three held-out sets, main against both versions
- [x] `.claude/rules/detect.md`, `docs/rules.md` (generated), the decisions document

## Measured

Static only (no judge; deterministic, so no raw archive), corpus `6d7aa48e`, main (`80a670c`; P-090 since touched only the judge, so the static rules are the same as `f2b5e82`) against the branch;
`results/aguard/2026-10-22-p091-{skills,heldout}-{main,changed}/` hold main and the final version.

| | n | main | full rules | no chain | **code only, no chain** |
|---|---:|---:|---:|---:|---:|
| malicious skill trees (development) | 393 | 92 | 98 | 98 | **98** |
| held-out malicious (ClawHub 123 + SkillHarm fresh 120) | 243 | 71 | 73 | 73 | **73** |
| benign skill trees (development) | 2,879 | 160 | 174 | 164 | **162** |
| held-out ClawHub benign | 216 | 6 | 6 | 6 | **6** |
| this repository's plugin self-scan | — | clean | 8 findings | 8 findings | **clean** (after the `Σ` line) |

The eight malicious trees gained (six development, two held-out) rest on `SUP-006` (a changed package source), `FS-001`
(an SSH key touched) and `EXEC-001` (a download piped to a shell) in files named `PREFLIGHT.md`, `ENVIRONMENT.md`,
`ENVIRONMENT_SETUP.md`, `forms.md`, `guide.md`, `nodejs.md` — none through the chain. With the chain, 11 of the 14 benign
trees gained were `EXFIL-001`/`-003` on reference guides (`DEPLOYMENT.md`, `OAUTH.md`, `node_mcp_server.md`) whose examples
call an API with its own key. Without the chain four benign trees were left,
all `SUP-006`: two named a registry in a sentence, two set one in a code block. Every malicious line gained sat in a code
block; the two prose ones went with the third step, and so did this repository's own audit skill, whose remediation
guide explains `curl|bash` and `~/.ssh` deny rules in sentences — six high findings in the second version. The two benign
trees left run `pip config set global.index-url …` and `--index-url https://download.pytorch.org/whl/nightly/cpu` in code,
which BAS Guard flags disclosed or not by design.

**Reading.** A small recall gain for every user who runs without the judge, at two benign trees in 2,879. The ceiling is
the rule set: most SkillHarm companion payloads (`rm -f` a log, `>> ~/.bashrc`, a `cp` over a config) are shapes no
static rule has, in any file — that is a question about the rules, not about which files they read.

## Done

```
Merged: PR #117 (2026-10-22; find the sha with git log --grep P-091)
Evidence: results/aguard/2026-10-22-p091-skills-{main,changed}: malicious 92 → 98 of 393, benign 160 → 162 of 2,879
Evidence: results/aguard/2026-10-22-p091-heldout-{main,changed}: held-out malicious 71 → 73 of 243, ClawHub benign 6 → 6 of 216
Evidence: rulesEpoch 4 → 5; make verify: all gates passed
```
