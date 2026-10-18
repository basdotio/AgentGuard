<!-- SPDX-License-Identifier: MIT -->
# 073 — A judge run without --json leaves no record of what it called or spent: the usage is printed once to stderr and is gone

- **Source**: the maintainer, 2026-10-19: judge runs made by hand or from scripts over days cannot be added up afterwards
- **Depends on**: P-001 (the judge's usage in the JSON `judge` block), P-031 (which judge: model, samples, the two
  versions), P-069 (`reasked`)
- **Branch**: `p/073-judge-usage-log`

## Problem

What a judge run did — calls, failed, skipped, retries, triage calls, repaired, re-asked, prompt and completion tokens,
and which model and judge build did it — reaches the JSON `judge` block only with `--json`. Otherwise it is one stderr
line (`LLM judge: N call(s) in … · M tokens in / K out`) that `--quiet` suppresses, and that the Downloads pass never
prints because every item there is judged quietly. `aguard llm test` prints its tokens to stdout. Someone who runs
`check --llm` or `scan --llm` by hand, or from a script that keeps only the exit code or the terminal report, cannot add
up afterwards what their judge runs did over a day or a week; the line scrolled past or was never printed. The
baselines driver records usage, but only for its own benchmark runs.

## Initial direction

An opt-in, append-only usage log. `AGUARD_USAGE_LOG=<path>` set: every judge run that issued at least one call appends
exactly one JSON line per judge block to that file — the block's usage counts and the judge's identity, copied from the
values the block already holds, nothing recounted. Unset or empty: nothing is written anywhere and every output is what
it was. The line never carries anything from the scanned content and never the endpoint's path, query or userinfo.

## Done criteria

- [x] `TestUsageLog_CheckLLMAppendsTheJudgeBlock` (`cmd/aguard/usagelog_test.go`, new; drives the built binary): with
  `AGUARD_USAGE_LOG` set, `check --llm` against an `httptest` stub appends exactly one line whose every shared field
  equals the JSON `judge` block's (plus `tool_version` and `rules_version` equal to the report's); a second run makes
  two lines; the endpoint is the origin alone (`http://127.0.0.1:<port>`) though `base_url` carries `user:pass@`, a
  path and a query; a created file is mode 0600
- [x] The line never carries the scanned target: the fixture's directory and skill name are a unique marker, and no line
  contains it, the target's path, the userinfo, the path or the query
- [x] `TestUsageLog_ScanLLMOneLinePerJudgeBlock`: `scan --llm` with a Downloads folder of two items writes two lines —
  `section: environment` equal to `judge`, `section: downloads` equal to `inbox.judge` (already summed over the items)
- [x] Reverse assertions (`TestUsageLog_UnsetWritesNothingAndChangesNoOutput`): without the variable, or with it empty,
  no file appears in the working directory; with it set, stdout (the `scanned_at` clock reading masked), stderr and the
  exit code equal the run without it; a run that made no call (`llm.enabled: false`, or no `--llm`) creates no file
- [x] `TestUsageLog_RefusesWhatIsNotARegularFile`: a symlink to a file, a dangling symlink, a directory, and a path
  under a regular file each give exactly one stderr warning, write nothing through (the symlink's target stays empty,
  the dangling one's target is not created), and leave the exit code as it is without the variable
- [x] `TestUsageLog_LLMTestRecordsItsOneCall`: `llm test` appends one line for its one call, answered (tokens as
  reported) or failed (`failed: 1`), with no `samples`
- [x] `TestZeroDial_OnlyTheJudgeConnects` runs with `AGUARD_USAGE_LOG` set; each positive-control row appends what its
  command appends and the table checks one line per row; every zero row is still zero
- [x] Docs: `docs/llm-judge.md` and `docs/llm-judge.zh-CN.md`, spec §3, invariant #1's note in `.claude/rules/invariants.md`
- [x] `make verify` green under `go version go1.23.5` with no toolchain switch; `head -3 go.mod` line 2 `go 1.23.5`

## Out of scope

- A config key for the path (open question 1); a log rotation or size cap: the file is the operator's, like a shell
  history
- Per-item lines for the Downloads pass (open question 2)
- Latency (the stderr line's p50/p95): a duration is not in the `judge` block and would be a new count
- The full `base_url` with its userinfo that the JSON `judge.endpoint` and the `LLM-002` note already print verbatim:
  an existing behaviour of the report, not of this log, and its own proposal if it is worth one
- The baselines driver's own ledger and usage sums: unchanged

## Must not claim

- Not a cost or price record: the tool computes no money. A line records the counts the judge kept and the tokens the
  endpoint reported, nothing else; an endpoint that reports no usage leaves the token keys out, and nothing in the log
  makes up the difference
- Not a guarantee against interleaving on every file system: one `write` per line on an `O_APPEND` descriptor keeps
  lines whole on local file systems; POSIX states that guarantee only for pipes up to `PIPE_BUF` (512 bytes at the
  minimum, macOS's value; 4096 on Linux), a line with every field at a realistic maximum measures about 450 bytes,
  and a long model name or host can push one past 512. A network file system may not honour `O_APPEND` at all
- Not tamper-evident: an append-only log the user owns, not an audit trail

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-073: proposal — a judge run leaves no record of what it called or spent without --json` |
| 2 | `cmd/aguard/usagelog.go`, the three call sites, the tests, the zero-dial table run with the variable set | `P-073: AGUARD_USAGE_LOG appends one JSON line per judge block that made a call` |
| 3 | The docs pair, spec §3, invariant #1's note | `P-073: docs — the judge usage log` |
| 4 | This file to `complete/`, Done filled, the index row | `P-073: done — evidence and the index row` |

## Open questions

1. **An environment variable or a config key?** **Recommendation**: the environment variable `AGUARD_USAGE_LOG`. CI
   jobs and benchmark drivers set it per run without editing a config file, a script that wraps `aguard` sets it for
   exactly the runs it wants recorded, and its absence is the default behaviour with no file to read. A config key
   (`llm.usage_log`) can come later and would only name the same path.
   **Decided (2026-10-19)**: as recommended.
2. **`scan` with a Downloads folder: one line per item, one line per scan, or one line per judge block?**
   **Recommendation**: one line per judge block the report carries — `section: environment` for `judge` and
   `section: downloads` for `inbox.judge` — each equal to its block. The Downloads block is already the sum over the
   items (`checkCandidate`), so nothing is counted again and a line can be checked against the report it came from;
   per-item lines would record how many items a folder held and multiply the lines with no block to check them
   against; one summed line per scan would be a number no report shows. `check` and `llm test` have one block and
   no `section`.
   **Decided (2026-10-19)**: as recommended.
3. **Which fields?** **Recommendation**: `time` (RFC 3339, UTC), `command`, `section`, `tool_version`,
   `rules_version`, `prompt_version`, `excerpt_version`, `model`, `endpoint` (origin only), `samples`, and the block's
   counts: `calls`, `failed`, `skipped`, `retries`, `triage_calls`, `repaired`, `reasked`, `temperature_omitted` (when
   true), `prompt_tokens` / `completion_tokens` (absent when the endpoint reported none, as in the block). Not
   `artifacts` or `findings` (counts of the scanned content), not `ran` (a line exists only when a call was made),
   not `reason` (free text that can carry a path or the `base_url`).
   **Decided (2026-10-19)**: as recommended.
4. **Does `--quiet` silence the warning when the log cannot be written?** **Recommendation**: no. Setting the variable
   is asking for the record; a missing record that says nothing is the problem this proposal exists to solve. The
   warning is one line and never changes the exit code or a report.
   **Decided (2026-10-19)**: as recommended.
5. **When in the run is the line written?** **Recommendation**: as soon as the judge's work is done — after the
   Downloads pass, before any report is written — so a report that fails to write (`--html` to a missing directory)
   does not take the record of calls already made with it.
   **Decided (2026-10-19)**: as recommended.
6. **`llm test`: which line?** **Recommendation**: one line whenever the call was issued (the config, endpoint and key
   checks passed): `calls: 1`, `failed: 0` or `1`, the tokens the endpoint reported, no `samples` (a connectivity
   check is not a sampled question).
   **Decided (2026-10-19)**: as recommended.
7. **The file's mode and what the path may be.** **Recommendation**: created 0600; an existing regular file is
   appended to and keeps its mode; a path that exists as a symlink (dangling or not), a directory or a special file,
   or whose parent is not a directory, is refused before the open (`Lstat`) and checked again against the opened file
   after it, so a symlink swapped in between is refused before a byte is written. A relative path is relative to the
   working directory; `~` is the shell's to expand.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR #93 (find the sha with git log --grep P-073 after the merge)
Evidence: TestUsageLog_CheckLLMAppendsTheJudgeBlock, TestUsageLog_ScanLLMOneLinePerJudgeBlock, TestUsageLog_LLMTestRecordsItsOneCall, TestEndpointOrigin, TestUsageLog_ExistingFileKeepsItsModeAndLines (cmd/aguard/usagelog_test.go); the five binary-driven ones red with the three call sites removed, green with them
Evidence: reverse — TestUsageLog_UnsetWritesNothingAndChangesNoOutput (unset/empty: no file; set: stdout with scanned_at masked, stderr and exit equal; llm.enabled false and no --llm: no file), TestUsageLog_RefusesWhatIsNotARegularFile (symlink, dangling symlink, directory, parent a file: one warning each, nothing written through, exit unchanged)
Evidence: TestZeroDial_OnlyTheJudgeConnects with AGUARD_USAGE_LOG set: 4 control rows, 4 lines, every zero row still zero
Evidence: a line with every field at a realistic maximum measures 446 bytes
Evidence: make verify: all gates passed (go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 2 go 1.23.5)
```
