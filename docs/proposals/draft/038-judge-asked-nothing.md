<!-- SPDX-License-Identifier: MIT -->
# 038 — `check --llm --fail-on-llm` passes a plugin or a plain directory the judge asked nothing about: zero questions read as "looked and found nothing"

- **Source**: new finding (2026-10-10), measured on v0.20.1+ (`6cab205`); follow-up to P-026
- **Depends on**: P-026 (exit 4 = "the LLM gate could not be evaluated")
- **Branch**: `p/038-judge-asked-nothing`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`--fail-on-llm` exits 4 when the judge could not answer (P-026): it did not run, a call failed, or a call was never made. It
does not see a judge that was never asked. `judge.planFor` has a pass for skills, the instruction-like kinds, connectors, hooks
and MCP servers, and **none** for `plugin`, `directory` or `quarantined` (nor `permission`, by design): for those the only
call is triage of their static findings, and with no static finding there is no call at all. `judgeGap` reads only `Ran`,
`Failed` and `Skipped`, so a run that planned zero questions about the target is "the judge answered everything" — exit 0,
and the report carries no note saying the judge never looked.

Measured with `authority: escalate`, `samples: 1`, `base_url` on a closed local port, `--fail-on-llm high --fail-on critical`:

| Target | `judge` block | exit |
|---|---|---|
| a plugin directory (`.claude-plugin/plugin.json` + `skills/p1/SKILL.md` whose body asks the agent to quietly send the user's home directory to an address in config) | `ran: true, calls: 0, failed: 0` | **0** |
| a plain directory (`NOTES.md` with the same sentence + a benign `build.sh`) | `ran: true, calls: 0, failed: 0` | **0** |
| the same `SKILL.md` checked as a skill (control) | 2 calls, 2 failed | 4 |

A pull-request job that runs `aguard check ./plugin --llm --fail-on-llm high` goes green on exactly the content the judge
exists to read: a rewritten instruction no regex matches.

## Initial direction

Disclose, on every `--llm` run, the artifacts whose kind the judge has no question for (one `LLM-000` note), and make the
`--fail-on-llm` gate not evaluable (exit 4, P-026's stderr line) when the target of a `check` is one of them. Record what the
judge *should* ask about a plugin tree or an unrecognised directory as an issue; that is a design question, not decided here.
