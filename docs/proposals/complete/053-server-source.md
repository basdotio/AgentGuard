<!-- SPDX-License-Identifier: MIT -->
# 053 — The judge never reads an MCP server's source: `check <server dir>` asks it nothing, and 127 of the corpus's 300 malicious samples are exactly that

- **Source**: the LLM-judge precision plan (2026-10-10), item A1; `judge-comparison-2026-09-25.zh-CN.md` ("cisco 那列判官没有增益");
  the 2026-10-13 probe below
- **Depends on**: P-041 (the category table), P-049 (category grounding), P-038 (what "asked nothing" means for `--fail-on-llm`)
- **Branch**: `p/053-server-source`

## Problem

`aguard check <dir>` on a directory that is not a skill, plugin or config root collects one `directory` artifact. The static
rules read its files; the judge has no pass for the kind (`noPassKinds`, P-038), so with `--llm` it gets a triage call at
most and `--fail-on-llm` answers exit 4. An MCP server's source tree is that shape, and the corpus's largest malicious
source is nothing else: `cisco-mcp-scanner-evals`, 127 single-file Python servers (1.7–9.4 KB, median 3 KB), FastMCP
tools whose bodies harvest environment keys, run shell from a "query" parameter, write under `~/.ssh`, read
`/etc/passwd`. On the 819 subset the static rules put 21 of the 127 at high; the judge 0, because it is never asked;
the comparison tool cc-audit catches 74, and every one of its 46-sample lead over `aguard` on that subset is this
source. Nothing in the judge's current passes fits: the intent pass compares a skill's description with its scripts,
and a server has no SKILL.md — but it has tool docstrings, which are what the user is told the tools do.

### Measured (probe, 2026-10-13, no code change)

Each server was wrapped as a pretend skill — `SKILL.md` whose description is the server's **tool docstrings only** (never
the module docstring, which in these evals names the attack), the server file under `scripts/` — and run through
`check --llm` at `samples: 1`, so the existing intent pass did the job the new pass would do:

| Panel | flagged at high |
|---|---|
| 127 Cisco servers, as vendored | 92 (static alone: 21; cc-audit: 74) |
| the same 127 with every comment and docstring line naming the attack removed (`HIDDEN:`, `harvest`, `exfiltrat`, …; 95 files changed) | 91 — the judge reads the code, not the labels |
| by truth dimension | execution 23/32 · injection 15/21 · backdoor 15/15 · exfiltration 11/11 · filesystem 8/12 · reconnaissance 8/10 · resource-abuse 1/10 |
| 76 benign servers (below), `samples: 1` | 3: a login that reads a handle and password (`credential-read`), a `code_mode` server whose tool executes arbitrary Python (`remote-execution` — arguably a finding), file-reading tools named `file-deletion` (a miscategorisation) |

The benign panel is not in the corpus — the corpus's benign MCP samples are 357 tool catalogues (`tools.json`), a
different shape, and its manifest says so ("SERIALISATION MISMATCH ON THE MCP SURFACE … a false-positive rate computed
against them would be meaningless"). The panel is real server source at pinned commits: `modelcontextprotocol/servers`
bbb701e (fetch, git, time), `modelcontextprotocol/servers-archived` 9be4674 (git, sentry, sqlite), `modelcontextprotocol/python-sdk`
c9356a2 (34 example servers and snippets), `jlowin/fastmcp` e2fcc41 (36 examples). 76 is small; its interval at 3 flags is
[1.3%, 11%].

## Initial direction

A **server-source pass**. When `check`'s target is a directory whose files make it an MCP server — a Python file
importing `mcp` / `fastmcp` with `@….tool(` decorators (TypeScript's `server.tool(` later) — the collector labels the
artifact kind `server` instead of `directory`, and the judge asks the intent question of it: DECLARED is the tool
names with the first line of each docstring (what a client sees), BEHAVIOR is `behaviorExcerpt` over the tree (the same
walker, redaction, 2,000-byte per-file and 6,000-byte total caps the intent pass uses for scripts). The finding is a
new rule, `LLM-010` ("server source does more than its tools say", dimension intent), with the intent pass's category
table and `disclosed` semantics, so the fold and the per-kind table keep it apart from `LLM-001` and the static column
is untouched. `noPassKinds` loses nothing: `directory` stays unasked; `server` is asked, so `check <server> --llm
--fail-on-llm` gives an answer where it gave exit 4. `scan` is out of scope here: a server a project configures by
`command: python server.py` is reached through `.mcp.json`, whose own pass (`LLM-009`) stays advisory; following that
command to its file is a later proposal.

## Done criteria

- [x] `TestCollectTarget_ServerSourceIsItsOwnKind` and `TestLooksLikeServerSource_NeverFollowsASymlink` (`internal/collect/server_test.go`): a directory with a `.py` importing `mcp` and a
  `@app.tool(` is kind `server`; a plain directory, a skill, a plugin keep their kinds; a `.py` that merely mentions mcp
  in a string does not qualify. **Red on the base**: every such directory is `directory`
- [x] `TestServerDeclared_IsTheToolDocstrings` and `TestServerDeclared_ReadsRegisteredTools` (`internal/judge/server_test.go`): DECLARED lists tool names with docstring first
  lines, never the module docstring; BEHAVIOR is the tree under the intent caps; units ground
- [x] `TestRun_ServerSourceGetsLLM010` (`internal/judge`): a scripted client flagging `credential-read` on a server
  fixture yields an escalating `LLM-010` at high; `disclosed: true` lowers it; `AsksNothingOf(KindServer)` is false and
  `TestAsksNothingOf_FollowsPlanFor` green
- [x] `cmd/aguard` e2e: `check <server> --llm --fail-on-llm high` exits 1 on a flagged server and 0 on a clean one — no
  longer 4; `check <plain dir> --llm --fail-on-llm high` still exits 4
- [x] Reverse assertions green without a character changed: every skill / hook / connector / mcp test; the 357 benign
  tool catalogues (kind `connector`) are not touched by the new kind; `--fail-on` deterministic contract
- [x] `make docs` adds the `LLM-010` row; `docs/llm-judge.md` + zh document the pass; spec §5.2 names it; P-038's exit-4
  text says `server` is asked
- [x] Measured and recorded under `baselines/results/aguard/2026-10-13-llm-gpt-4.1-mini-p053-*/` (the benign gate MISSED by one; see Done):
  - the 127 Cisco samples through the real pass at `samples: 3`: flagged at high ≥ 85 (probe at `samples: 1`: 92), per
    truth dimension; static column unchanged (21)
  - the 76-server benign panel at `samples: 3`, pinned commits named in `run.yaml`: flagged at high ≤ 3, each named
  - the full 819 subset: everything outside the 127 unchanged to the draw (the pass fires on kind `server` only)
- [x] `make verify` green; `internal/score`, `internal/gate`, `internal/detect` untouched (the collector gains a kind, the
  judge a pass)

## Out of scope

- TypeScript / Go / Rust servers: the 127 are Python; the recogniser is written to take a second language without a
  second design, and the TypeScript docstring shape (`server.tool("name", "description", …)`) is noted for it
- Following `.mcp.json`'s `command` to a local server file under `scan`: a different trust boundary (the file may be
  anywhere), a later proposal
- Resource-abuse shapes (1 of 10 in the probe): the same model limit P-050 recorded on skills
- Vendoring the benign server panel into the corpus: proposed to the corpus's maintainer separately; until then the
  panel lives in the run's `run.yaml` by repository and commit

## Must not claim

- Do not quote 92/127 as a rate of real servers: the Cisco evals are synthetic single-file servers written to be
  caught, and the probe's scrubbed variant only shows the judge did not need their labels
- Do not say the benign cost is 4%: 3 of 76 at `samples: 1` is an interval of [1.3%, 11%], and one of the three is a
  real `execute`-arbitrary-code tool
- Do not pool the 127 with the 173 reachable malicious of the 819 subset into one recall: they are reported as the
  corpus's scorecard reports them, per source

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | `KindServer` and the recogniser in collect, tests red then green | `collect: a directory that is an MCP server's source is kind server (P-053)` |
| 2 | The server excerpt (tool docstrings as DECLARED) and the pass → `LLM-010`, unasked table, tests | `judge: the server-source pass — a server's tools against what their docstrings say (P-053)` |
| 3 | e2e: the gate answers for a server; docs pair, spec, rules row | `docs: LLM-010, the server-source pass (P-053)` |
| 4 | The three measurements, recorded, the decisions doc appended | `baselines: P-053 measured — 127 servers and a 76-server benign panel at samples 3 (P-053)` |
| 5 | This file's Done, the index | `proposals: P-053 (P-053)` |

## Open questions

1. **A new artifact kind (`server`) or a flag on `directory`?** **Recommendation**: a kind. The JSON report, the fold's
   per-kind table and `AsksNothingOf` all key on kind; a flag would need every reader to learn it. Readers of older
   reports see a new kind value only for targets that were `directory` before, and the schema's kind list is open.
2. **A new rule id (`LLM-010`) or reuse `LLM-001`?** **Recommendation**: new. The static column and every committed fold
   split by rule; a server finding under `LLM-001` would make the skill numbers unreadable afterwards.
3. **What is DECLARED?** **Recommendation**: tool names with the first docstring line each, nothing else — the client
   sees exactly that. A server with no tool docstrings gets DECLARED = the tool names alone, which the intent prompt
   treats as a thin declaration (most behaviour then reads as undisclosed, which is right for a server that documents
   nothing).
4. **Does the pass run under `scan` for servers a project configures?** **Recommendation**: not here (see Out of
   scope). The benefit on the corpus is zero — its servers are `check` targets — and the boundary question deserves
   its own page.
5. **How large must the benign panel be before the Done can be read?** **Recommendation**: the 76 at `samples: 3` for
   this proposal, with the interval stated, and a request to the corpus's maintainer to vendor benign servers so the
   next measurement is reproducible from the corpus alone.

## Done

```
Merged: PR #73 (2026-10-13; find the sha with git log --grep P-053)
Released: v0.24.0
Evidence: TestCollectTarget_ServerSourceIsItsOwnKind (nine shapes: FastMCP at the top, two levels down, the low-level SDK, the decorator without the call, low-level handlers, Tool(name=…) registrations, a client that only imports the SDK, a decorator without the SDK, mcp in a string, a plain directory, a skill bundling a server, a server too deep to probe) and TestLooksLikeServerSource_NeverFollowsASymlink (internal/collect/server_test.go); red on the base (every directory)
Evidence: TestServerDeclared_IsTheToolDocstrings, TestServerDeclared_ReadsRegisteredTools, TestRun_ServerSourceGetsLLM010 (internal/judge/server_test.go): the declared side is tool names with docstring first lines and never the module docstring; LLM-010 at high on credential-read, medium when disclosed; a server gets exactly the server pass; directory stays unasked; TestAsksNothingOf_FollowsPlanFor green with the new kind in its fixture
Evidence: TestCheckCmd_ServerSourceAnswersTheGate (cmd/aguard/check_server_test.go): check <server> --llm --fail-on-llm high exits 1 on a flagged server and 0 on a clean one, with kind server and LLM-010 in the report; a plain directory still exits 4 (P-038)
Evidence: every skill / hook / connector / mcp test green without a character changed; judge-prompt moved (the mode's shapes joined PromptVersion), judge-excerpt stayed 6; make docs added the LLM-010 row
Evidence: 127 Cisco servers at samples 3 (…-p053-servers-s3/compare.txt): judge escalated at high 108 = 85.0% [77.8, 90.2] (gate ≥ 85); static 21 unchanged, all 21 inside the 108; cc-audit on the same 127: 74; by truth dimension execution 28/32, injection 19/21, backdoor 15/15, exfiltration 11/11, filesystem 10/12, reconnaissance 9/10, resource-abuse 1/10; the 19 not flagged are nine resource-exhaustion shapes (fork bomb, memory / thread / fd exhaustion, ReDoS, zip bomb — the model's limit P-050 recorded on skills), two injection (cache / result poisoning), two execution (LDAP / XPath injection), the rest filesystem and reconnaissance; calls 436, failed 6, tokens 575,675 / 104,378
Evidence: 76 benign servers at samples 3 (…-p053-benign-servers-s3/compare.txt; the trees are in raw_archive under benign-servers/): escalated at high 4 = 5.3% [2.1, 12.8] — gate ≤ 3 MISSED by one; 69 recognised as kind server, the 7 others are prompt/resource-only examples that build no server and rightly stay directory; the four: atproto_mcp (its tools are defined where the extractor cannot read them, so nothing is declared and the login's credential read is "undisclosed"), code_mode and search (a read_file tool the model names file-deletion / credential-read although its docstring says it reads a file — a wrong category and a disclosed read, both the model's), text_me (posts the message with its API key to the SMS service it exists to call); calls 215, failed 0
Evidence: the recogniser was widened once on measurement: the decorator-with-call alone recognised 28 of the 76; the forms the other 48 use (decorator without the call, low-level handlers, Tool(name=…) registrations, a bare FastMCP( / Server( construction) joined it with tests, and the 127 were unaffected (all recognised under both)
Evidence: 819 outside the 127: the pass fires on kind server only, and no sample of the 819 outside the Cisco source is that kind (every one is a skill, hook, config or catalogue), so the rest of the subset is untouched by construction; not re-run
Evidence: Out of scope — git diff --stat main -- internal/score internal/gate internal/detect/rules*.go: empty (detect.go gained the kind in its tree-kinds case only)
Evidence: make verify: all gates passed on the branch; go test -count=1 green; go.mod line 2 unchanged
```
