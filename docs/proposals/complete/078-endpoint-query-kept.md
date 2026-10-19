<!-- SPDX-License-Identifier: MIT -->
# 078 — A judge endpoint with a query string has never worked: `/chat/completions` is appended after the query, so the path stays the base and the query value swallows it

- **Source**: new finding (2026-10-19) while doing P-076 (PR #96), recorded in its Out of scope: `NewHTTP` appends
  `/chat/completions` after the whole `llm.base_url`, so a query-carrying endpoint is requested at the base path
- **Depends on**: none (P-076 changes how the endpoint is *shown*; this changes the URL a request is *sent* to)
- **Branch**: `p/078-endpoint-query-kept`

## Problem

`judge.NewHTTP` stores `strings.TrimRight(base_url, "/")` and every request goes to that string with
`"/chat/completions"` appended. The suffix is meant for the path, but it is appended to the end of the string, which is
the path only when the URL ends there. Some gateways take a parameter in the query (`?api-version=…`, `?api-key=…`), and
`CheckEndpoint` accepts such a `base_url`. Measured on `origin/main` (eba242f) with an httptest stub that records
`r.URL.Path` and `r.URL.RawQuery`, through `NewHTTP(…, nil)` for both `Judge` and `Ping`:

| `base_url` (after the stub's origin) | Path received | Query received |
|---|---|---|
| `/v1?api-version=2024-01-01&api-key=k123` | `/v1` | `api-version=2024-01-01&api-key=k123/chat/completions` |
| `/v1/?api-version=2024-01-01&api-key=k123` | `/v1/` | `api-version=2024-01-01&api-key=k123/chat/completions` |
| `?api-key=k123` | `/` | `api-key=k123/chat/completions` |
| `/v1#section` | `/v1` | (none; the fragment `section/chat/completions` is never sent) |
| `/v1?api-key=k123#section` | `/v1` | `api-key=k123` |

So the call reaches the base path, never `/chat/completions`, and the last query value is corrupted. Such a
configuration has never worked: `aguard llm test` fails with whatever the gateway answers for its base path, and so does
every call of `scan --llm` / `check --llm`. `aguard llm test` (`runLLMTest` → `Ping`) was reproduced the same way:
path `/v1`, query `api-version=2024-01-01&api-key=k123/chat/completions`.

Every place a URL is derived from `base_url`, checked:

- **The request URL** (chat completions for `Judge`, triage and `Ping` alike): one expression in `chatOnce`, the bug.
- **`llm test`**: `Ping` → the same `chatOnce`, the same URL.
- **The same-origin redirect check** (P-023): compares scheme, host and port of `via[0].URL`, the request URL. The
  query and the path do not take part, so it is unaffected either way.
- **`CheckEndpoint`**: parses `base_url` and looks at scheme and host only. A query or fragment is accepted; a
  `base_url` that does not parse is refused before any request is built, on all three outbound paths.
- **`llm preview`**: reports `base_url` as configured (`Endpoint: cfg.LLM.BaseURL`), not a request URL; how it is shown
  is P-076's concern. Its payloads come from the same planner as the client's and contain no URL.
- **The usage log's `endpoint`** and the report's `LLM-002` / loopback checks: origin or host only.
- **A trailing slash**: `TrimRight` already makes `/v1/` and `/v1` (and `/v1//`) one URL — but only when nothing
  follows the path; with a query the slash stays in the path.

## Initial direction

Build the request URL once in `NewHTTP` from the parsed `base_url`: cut it where `net/url` does (the fragment at the
first `#`, then the query at the first `?`), trim trailing slashes from what remains, append `/chat/completions`, and put
the query back as written. The fragment is dropped (an HTTP client never sends it). Every byte of scheme, userinfo, host,
path and query stays as written; a `base_url` with neither `?` nor `#`, or one that does not parse, goes through the old
expression unchanged.

## Done criteria

- [x] `TestHTTP_QueryBaseURLKeepsPathAndQuery` (`internal/judge/chaturl_test.go`): for `base_url`
  `…/v1?api-version=2024-01-01&api-key=k123` the stub receives path `/v1/chat/completions` and query
  `api-version=2024-01-01&api-key=k123`, for both `Judge` and `Ping` (on `main`: path `/v1`); the same for a trailing
  slash before the query, a query on the root, a fragment, query plus fragment, and an escaped query value
- [x] `TestLLMTest_QueryBaseURLReachesChatCompletions` (`cmd/aguard`): `llm test` with that `base_url` reaches
  `/v1/chat/completions` with the query as written (on `main`: path `/v1`)
- [x] Reverse assertion: `TestHTTP_PlainBaseURLRequestLineUnchanged` — for base_urls without query or fragment (root,
  `/`, `/v1`, `/v1/`, `/v1//`, a two-segment path, an escaped path) the request target received is byte-identical to
  the old expression's; `TestHTTP_PresetURLsUnchanged` — every preset and the default local endpoint are sent to exactly
  the old URL string. Both green before and after the fix
- [x] `FuzzChatURL`: for any input without `?` and `#`, and any input that does not parse, the URL is the old one byte for
  byte; for an http(s) `base_url` with either, scheme, userinfo, host and query are kept, the path is the trimmed base
  path plus `/chat/completions`, and there is no fragment
- [x] The redirect tests (`internal/judge/redirect_test.go`, `cmd/aguard/judge_redirect_test.go`) and the zero-dial tests
  pass with their files unchanged
- [x] `docs/llm-judge.md` and `docs/llm-judge.zh-CN.md` say how a query and a fragment in `base_url` are used
- [x] `make verify` green under `go version go1.23.5` with no toolchain switch; `head -3 go.mod` line 2 `go 1.23.5`

## Out of scope

- How the endpoint is shown (reports, errors, `llm` commands): P-076
- `llm preview` reporting the request URL instead of `base_url`: it reports what is configured, and the request URL is a
  function of it
- Vendor-specific URL schemes (a deployment name in the path, an `api-key` header instead of a bearer token): the judge
  speaks one OpenAI-compatible shape; a gateway that needs a different header is not made to work by this change
- `CheckEndpoint`: unchanged; it accepts a query or fragment and refuses what does not parse, as before

## Must not claim

- Not "Azure OpenAI is supported", or any named gateway: only that the query reaches the server with the request at
  `<path>/chat/completions`. Whether a given gateway then answers depends on what else it expects
- Not "every request is unchanged": requests change for exactly the base_urls that contain `?` or `#` and parse

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-078: proposal — a judge endpoint with a query string has never worked` |
| 2 | The request URL built in one function, unchanged; the tests, red on the query and fragment rows | `P-078: tests — a base_url with a query or fragment is requested at the base path` |
| 3 | `chatURL` keeps the query, drops the fragment, joins the path | `P-078: the judge appends /chat/completions to the path of base_url and keeps its query` |
| 4 | The docs pair | `P-078: docs — how a query and a fragment in base_url are used` |
| 5 | This file to `complete/`, Done filled, the index row | `P-078: done — evidence and the index row` |

## Open questions

1. **Re-serialize a parsed `url.URL`, or split the string?** **Recommendation**: parse with `net/url` to decide whether
   the string is a URL at all, then split the string where `net/url` splits it (`#` first, then `?`) and reassemble.
   `url.URL.String()` re-encodes (an escaped path or query can come back spelled differently), and the only bytes this
   change has a reason to touch are the end of the path.
   **Decided (2026-10-19)**: as recommended.
2. **A fragment: drop it, or refuse it in `CheckEndpoint`?** **Recommendation**: drop it. No HTTP client sends a
   fragment, so dropping it changes nothing the server sees except that the suffix now lands on the path; refusing it
   would turn a harmless spelling into a configuration error.
   **Decided (2026-10-19)**: as recommended.
3. **A `base_url` that does not parse?** **Recommendation**: keep the old expression for it. `CheckEndpoint` refuses it
   before any of the three outbound paths builds a request, and a caller that skips the check still gets the same
   `build judge request` error it got before.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR #97 (find the sha with git log --grep P-078 after the merge)
Evidence: TestHTTP_QueryBaseURLKeepsPathAndQuery (internal/judge/chaturl_test.go): base_url …/v1?api-version=2024-01-01&api-key=k123 → Judge and Ping received path /v1, query "api-version=2024-01-01&api-key=k123/chat/completions" at the tests commit; path /v1/chat/completions, query "api-version=2024-01-01&api-key=k123" after the fix; all six rows red → green
Evidence: TestLLMTest_QueryBaseURLReachesChatCompletions (cmd/aguard/endpoint_query_test.go): llm test reached path "/v1" at the tests commit, /v1/chat/completions with the query as written after
Evidence: reverse — TestHTTP_PlainBaseURLRequestLineUnchanged (7 base_urls without query or fragment) and TestHTTP_PresetURLsUnchanged (default local endpoint + deepseek, openai, qwen) green at the tests commit and after; dropping the trailing-slash trim turns them and FuzzChatURL red (11 FAIL lines)
Evidence: FuzzChatURL: 60 s, ~3.8M executions, no failure after one oracle fix (a path net/url re-encodes is compared as written; that input is a seed)
Evidence: redirect_test.go, judge_redirect_test.go, zero_dial_test.go, zero_dial_source_test.go unchanged; TestNewHTTP_RefusesCrossOriginRedirects and the TestZeroDial_* tests pass
Evidence: make verify: all gates passed (go version go1.23.5 darwin/arm64, no toolchain switch; go.mod line 2 go 1.23.5)
```
