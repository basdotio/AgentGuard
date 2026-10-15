<!-- SPDX-License-Identifier: MIT -->
# 058 — The default terminal report is written for the auditor: a reader who does not write code has to find the one thing to do under scan internals, judge versions and places the scan never looked

- **Source**: the maintainer's review of real reports (2026-10-15), decided as suggested ("start with your suggestions")
- **Depends on**: none
- **Branch**: `p/058-report-for-readers`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

The default terminal report (`report.Text`) is the one most people read, and most of them do not write code. Measured on
`v0.25.0` (`1d9ffcb`) against the maintainer's real `~/.claude` (counts only):

- `check` of a skill with one high finding prints 28 lines; the finding is said three times (the verdict line, the
  "what to look at" list, the findings list), and 11 of the 28 lines are scan details. A clean skill prints 19 lines to say
  "nothing to fix".
- `check <skill>` lists four "Where the scan looked" rows that do not apply to a single target: the target is treated as a
  config root, so the report says "Claude Desktop store absent …/skills/<x>/Library/Application Support/…" and "User MCP config
  absent …/skills/.claude.json". `check <file>` adds two "MCP config in the root" rows under the file's own path. None of these
  places is read for a single target.
- With `--llm`, the judge's opinion on a static finding is printed as `⚖ triage (LLM, advisory): likely-benign — …`: jargon,
  and nothing says that it leaves the finding and the score alone. Two lines of judge internals (`prompt_version`,
  `excerpt_version`, model, samples) sit in the summary.
- `LLM-002` — the disclosure that excerpts left the machine for a non-local judge endpoint — is folded into "Not checked … coverage
  is incomplete" as a medium coverage note. It is not a coverage gap, and the line that says where the content went is hidden.
- Nothing says why the score is the number it is (a single high caps the score at 69, the environment number is an average),
  and no finding says what the reader can do next with this tool (which file and line to read, the `aguard approve` command).

## Initial direction

Reorder and shorten the default terminal view only: one block per static finding with the judge's view and a concrete next
step, a derived "Why N" line, a short "Judge:" line, the `LLM-002` disclosure on its own line, scan details behind `--verbose`, a
three-line clean report, and no root-derived locations for a single target. `--verbose`, JSON, SARIF, HTML and markdown keep
every datum; markdown follows the same reading order. JSON and SARIF stay byte-identical.
