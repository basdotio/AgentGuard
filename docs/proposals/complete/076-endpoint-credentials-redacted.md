<!-- SPDX-License-Identifier: MIT -->
# 076 — A judge endpoint written with credentials in it is printed whole: `user:pass@` or a key in the query reaches the JSON report, the `LLM-002` note and the config errors

- **Source**: a review on 2026-10-10, found again on 2026-10-19
- **Depends on**: P-023 (the redirect refusal names origins only)
- **Branch**: `p/076-endpoint-credentials-redacted`

## Problem

`config.CheckEndpoint` accepts an `llm.base_url` with userinfo (`https://user:pass@host/v1`, which Go's client sends as
Basic auth when no key is configured — a live credential) and one with a secret in the query string (`?api-key=…`, the
form some gateways document). Nothing then treats that string as sensitive: every place that shows the endpoint prints it
as written. The Markdown report is made to be pasted into a pull request comment.

Reproduced on `origin/main` (go1.23.5) with `base_url: https://alice:s3cret@llm.invalid/v1?api-key=k123` and
`check --llm skill/demo`:

| Where | What it printed |
|---|---|
| JSON `judge.endpoint` | `"https://alice:s3cret@llm.invalid/v1?api-key=k123"` |
| `LLM-002` note, JSON / Markdown / terminal | `base_url "https://alice:s3cret@llm.invalid/v1?api-key=k123" is not loopback: …` |
| `LLM-000` note from the failed calls (JSON / Markdown) | `… first error: call judge endpoint: Post "https://alice:***@llm.invalid/v1?api-key=k123/chat/completions": …` — Go strips the password from its own `*url.Error`, not the user name and not the query |
| `CheckEndpoint`, plain http to a remote host (setup and `llm test` error, `LLM-000` note, JSON `judge.reason`) | `llm.base_url http://alice:s3cret@llm.invalid/v1?api-key=k123 is plain http to a remote host: …` |
| `CheckEndpoint`, unparseable (`:99999x` as the port) | `llm.base_url "https://alice:s3cret@llm.invalid:99999x/v1?api-key=k123" is not a valid URL` |
| `llm status` | `endpoint: https://alice:s3cret@llm.invalid/v1?api-key=k123` |
| `llm test` | `error: https://alice:s3cret@…?api-key=k123 did not answer for model fake: call judge endpoint: Post "https://alice:***@…?api-key=k123/chat/completions": …`; on success the `OK · … via <base_url>` line |
| `llm setup` | `endpoint <base_url> · model …` and `… are sent to <base_url> for analysis` |
| `llm preview`, text and `--json` | `Endpoint <base_url>, model …` / `"endpoint": "<base_url>"` |

The redirect refusal (P-023) is already safe: it names `scheme://host[:port]` only, and `url.URL.Host` carries no
userinfo. The terminal and Markdown disclosure line (`Sent to <host>: …`) prints the host only as well.

## Initial direction

One display function for an endpoint, in the redaction package: userinfo becomes `<REDACTED>@`, every query value and a
fragment become `<REDACTED>`, scheme, host, port and path are kept, and the result goes through `redact.Secrets` like
every other quoted string. It works on the raw string, so a `base_url` that does not parse is redacted the same way.
Every print site uses it; the request keeps the real URL.

## Done criteria

- [x] `TestEndpoint` (`internal/redact/endpoint_test.go`, new): userinfo (user and password, user only, a token as the
  user), every query value, a fragment, a value-less query part, a secret-shaped path segment, a scheme-less and an
  unparseable string are redacted; scheme, host, port and path survive; idempotent. `TestEndpoint_PlainURLsUnchanged`:
  the default endpoint, the three presets and a set of credential-free self-hosted forms come back byte-identical.
  `TestEndpointHost`: the host is found in a raw and in a displayed endpoint
- [x] A test per print site, each with `base_url` carrying userinfo and a query secret (`alice:s3cret@`, `?api-key=k123`):
  the output contains neither `s3cret`, `k123` nor `alice`, and does contain the host — JSON `judge.endpoint`, the
  `LLM-002` note in JSON, Markdown and terminal, the `LLM-000` note and `judge.reason` from a refused endpoint, each
  `CheckEndpoint` error (invalid, plain http, wrong scheme), the `LLM-000` note from a failed call, `llm status`,
  `llm setup`, `llm test` (success and failure), `llm preview` text and JSON
- [x] The Markdown and terminal disclosure still name the host (`Sent to llm.invalid: …`) from the redacted
  `judge.endpoint`
- [x] **Reverse assertion**: the request still goes to the configured URL with its credentials — a stub observes the
  Basic auth header `alice:s3cret` (no key configured) and `k123` in the query; with a key the Bearer header as before
- [x] **Reverse assertion**: an endpoint without credentials prints exactly as before — the JSON `judge` block of a run
  against a plain loopback endpoint is byte-identical to what `origin/main` writes, and `TestLLMCommands_SetupTestStatus`,
  `TestCheckEndpoint`, `TestE2E_JudgeRedirectIsRefusedAndReported`, `TestLLMTest_RedirectIsRefused` and the zero-dial
  tests are green without a character changed
- [x] `make verify` green with go1.23.5, no toolchain switch, `go.mod` second line `go 1.23.5`

## Out of scope

- **No change to what is sent**: the request URL, its userinfo, its query and the Authorization header are exactly as
  before. `CheckEndpoint` accepts and refuses the same set of URLs.
- **The query is appended to wrongly, and that stays**: `NewHTTP` builds the request URL as `base_url + "/chat/completions"`,
  so `…/v1?api-key=k123` is requested as path `/v1` with the query `api-key=k123/chat/completions`. A query-carrying
  `base_url` has therefore never worked as the gateway meant it. Fixing it changes what is sent, which this item does not;
  it is recorded for its own proposal.
- **A non-200 body the endpoint echoes is not touched**: `judge endpoint returned 401: <body>` quotes the endpoint's own
  answer; what a server chooses to echo is another question.
- No new rule ID, no change to the report layout, no new dependency; `go.mod`/`go.sum` untouched.
- **What the judge reads does not move**: `redact.Secrets` and `redact.Credentials` are not changed, only a function the
  excerpt path never calls is added, so `ExcerptVersion` stays 9 and `PromptVersion` stays `b851dfd5412b`
  (`excerpt_version_test.go` and `prompt_version_test.go` green without a character changed).

## Must not claim

- Not "credentials in `base_url` are safe" or "never printed anywhere": this item covers what aguard prints. The config
  file still holds them in the clear, and proxies, logs on the endpoint's side and the endpoint itself see them.
- Not "the endpoint is redacted like a secret scanner would": the display is structural (userinfo, query values, fragment)
  plus the same best-effort `redact.Secrets` every snippet goes through; a credential in the host name or in a path
  segment that does not look like one is printed.
- Not "a URL with credentials is refused": it is accepted and used as before (open question 1).

## Work items

| W | In one sentence | Commit message (no sha; a rebase changes it) |
|---|---|---|
| 1 | This proposal | `P-076: proposal — a judge endpoint written with credentials in it is printed whole` |
| 2 | Tests, red on `origin/main` | `P-076: tests — every place that shows the judge endpoint prints its userinfo and query secrets` |
| 3 | `redact.Endpoint` / `redact.EndpointHost` and every print site through them; the judge's own request errors too | `P-076: an endpoint is shown with its userinfo, query values and fragment redacted; the request keeps them` |
| 4 | `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`, invariant #3 | `P-076: docs — a shown endpoint is redacted` |
| 5 | Done, `git mv` to `complete/`, the index | `P-076: proposal complete` |

## Open questions

1. **Refuse a `base_url` with userinfo or a query instead?**
   **Recommendation**: no. Some gateways document exactly these forms; refusing them breaks a working setup (userinfo
   without a key does reach the endpoint as Basic auth) to fix a display problem. Whether aguard should refuse or warn on
   such a URL is a separate decision.
   **Decided (2026-10-19)**: as recommended.
2. **Where does the function live, and is it a second redactor?**
   **Recommendation**: in `internal/redact` (the leaf every package can import, config included), as a structural URL
   operation that then calls `Secrets` on the result — so a key-shaped path segment is caught by the same patterns as
   everywhere else and there is no second pattern list. Invariant #3 gets one line naming it.
   **Decided (2026-10-19)**: as recommended.
3. **Drop the userinfo, or mark it?**
   **Recommendation**: mark it — `<REDACTED>@`. The reader learns that the URL carries credentials (which explains a
   Basic auth they may not have meant to send) without learning them. The user name goes too: a token is often written as
   the user (`https://<token>@host`), and `redact.Secrets`' URL pattern keeps the user name, which is right for a
   connection string in a skill and wrong here. Because `<REDACTED>@` does not parse as a URL, the report's disclosure
   line finds the host with `redact.EndpointHost`, the same split the display uses, instead of `url.Parse`.
   **Decided (2026-10-19)**: as recommended.
4. **Keep the query keys?**
   **Recommendation**: yes, only the values go (`?api-key=<REDACTED>&deployment=<REDACTED>`). Which parameter is set is
   what the operator needs to recognise the configuration; a part with no `=` is all value and is redacted whole.
   **Decided (2026-10-19)**: as recommended.
5. **A malformed `base_url` in an error message?**
   **Recommendation**: the same function. It splits the raw string (scheme, authority up to the first `/?#`, the last
   `@` in it, `?`, `#`) without parsing it, so a string `url.Parse` rejects is redacted the same way; `Secrets` then runs
   over it as over any quoted text.
   **Decided (2026-10-19)**: as recommended.
6. **The judge's own transport errors carry the URL (`Post "<url>": …`, Go's `*url.Error`).**
   **Recommendation**: the judge client replaces that URL with its display form before the error leaves `chatOnce`, keeping
   the wrapped cause, so `errors.Is`/`errors.As` and the retry classification see the same error as before.
   **Decided (2026-10-19)**: as recommended.

## Done

```
Merged: PR <filled in by the commit that records the pull request> (2026-10-19; find the sha with git log --grep P-076 after the merge)
Evidence: reproduced on origin/main with a binary built from it and base_url https://alice:s3cret@llm.invalid/v1?api-key=k123 — the ten rows of the problem table, each printing the user, the password or the query value; after W3 the same commands print https://<REDACTED>@llm.invalid/v1?api-key=<REDACTED> in every row, and "Sent to llm.invalid" still names the host
Evidence: red at W2 (tests on origin/main code): TestCheckEndpoint_ErrorsDoNotEchoCredentials 4/4 subtests, TestHTTP_RequestErrorDoesNotEchoCredentials (`Post "https://alice:***@llm.example/v1?api-key=k123/chat/completions"`), TestHTTP_BuildErrorDoesNotEchoCredentials (`parse "https://alice:s3cret@…"`), TestDisclosure_HostFromARedactedEndpoint (url.Parse refuses `<REDACTED>@`), TestE2E_EndpointCredentialsAreNotPrinted 2/2 (judge answers / unreachable; JSON, text, text --verbose, Markdown, HTML, SARIF, the Downloads block), TestE2E_RefusedEndpointWithCredentialsIsNotPrinted 2/2 (plain http, unparseable; judge.reason and the LLM-000), TestLLMCommands_EndpointCredentialsAreNotPrinted (setup, status, test, test error, preview, preview --json, setup refusal), internal/redact did not build (Endpoint undefined); all green after W3
Evidence: reverse — TestE2E_EndpointCredentialsAreNotPrinted asserts every request still reached llm.invalid with Basic auth uzer9:s3cret and api-key=k123 in its query; TestHTTP_RequestErrorDoesNotEchoCredentials the same at the client, with the *url.Error still in the chain and the failure still retryable; TestLLMCommands_EndpointCredentialsAreNotPrinted the Bearer key with the query; live against a loopback stub with no key: "basic=alice:s3cret", query "api-key=k123/chat/completions"
Evidence: reverse — TestE2E_PlainEndpointPrintsAsBefore green on origin/main and after: judge.endpoint, the LLM-002 text and `llm status` for https://llm.invalid:8443/openai/v1 as written, judge.endpoint unchanged for the default and the three presets; TestEndpoint_PlainURLsUnchanged (ten credential-free forms byte-identical); Endpoint is the only field of the judge block that changed, so a credential-free block is byte-identical
Evidence: mutation (reverted, git status empty) — shownURL returning its argument: the two judge tests, the e2e "judge unreachable" and the llm commands test red
Evidence: TestZeroDial_OnlyTheJudgeConnects, TestZeroDial_NoClientOutsideTheJudge, TestZeroDial_ClaimsNameTheTest, TestZeroDial_SeamClientLiteralShapes, TestE2E_JudgeRedirectIsRefusedAndReported, TestLLMTest_RedirectIsRefused, TestLLMCommands_SetupTestStatus, TestCheckEndpoint, TestLLMPreview_* green, their files unchanged
Evidence: make verify: all gates passed (go1.23.5, no toolchain switch; go.mod second line go 1.23.5)
```
