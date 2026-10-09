<!-- SPDX-License-Identifier: MIT -->
# 023 — When the judge's endpoint answers with a redirect, the API key goes out in cleartext, or an excerpt of the scanned content goes to a host the user never configured

- **Source**: a follow-up P-003 recorded in "Out of scope" — "no redirect handling (`CheckRedirect` / a cross-host redirect
  carrying the Bearer header away)"
  ([complete/003-zero-dial-test.md](../complete/003-zero-dial-test.md))
- **Depends on**: none (P-003 is merged)
- **Branch**: `p/023-judge-redirect`

<!-- No "Status" line: the directory the file is in is its status (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

`CheckEndpoint` (`internal/config/config.go`) refuses a remote `llm.base_url` that is not https, and the reason is in its
own error message: the API key is a Bearer header and the excerpt is the request body, so cleartext http puts both on the
wire. Setup, `llm test` and the judge all go through it. **But it only looks at the address written in the config.** The
judge's client (`NewHTTP` in `internal/judge/openai.go`, `&http.Client{Transport: Transport}`) uses Go's default redirect
policy: the endpoint answers with a 30x, the client sends the request again to `Location`, **without going through
`CheckEndpoint` again**, and the report says not a word about it.

Measured in this repository (go1.23.5) with httptest: `NewHTTP(…, nil)` wired through the `judge.Transport` seam to several
local servers (dialing routed by host name, no network egress), `Judge` sends a marked excerpt, the endpoint answers with a
redirect, and we look at what the **target** receives:

| Configured endpoint | Redirected to | 301 / 302 / 303 (switches to GET, drops the body) | 307 / 308 (resends the POST as is) |
|---|---|---|---|
| `https://example.com/v1` | `http://example.com/…` (same host, downgraded to cleartext) | **key sent in cleartext** | **key and excerpt both sent in cleartext** |
| same as above | `https://collector.test/…` (another host) | key stripped | key stripped, **excerpt sent to it** |
| same as above | `http://collector.test/…` (another host, cleartext) | key stripped | key stripped, **excerpt sent to it in cleartext** |
| same as above | `https://eu.example.com/…` (subdomain) | **key sent** | **key and excerpt both sent** |
| same as above | `https://example.com/…/` (same origin, only the path differs) | key sent back to the same origin | key and excerpt sent back to the same origin |
| `http://localhost:11434/v1` (local) | `http://collector.test/…` (remote) | key stripped | key stripped, **excerpt crosses the network in cleartext** |
| same as above | `http://localhost:8080/…` (same host, another port) | key sent | key and excerpt both sent |

- **A same-host downgrade to http always sends the key in cleartext**: when Go decides "whether to carry `Authorization`"
  it compares only the host name, not the scheme or the port (a subdomain counts as the same too), so `https://h` →
  `http://h` carries it all the same. This is exactly what `CheckEndpoint` refuses, only now it is the endpoint that asks
  for it.
- **On a host change the key is stripped, the excerpt is not**: 307/308 resend the request body as is, per the spec, and the
  request body is the redacted excerpt (the "best effort" one of invariant #3). What receives it is a host the user never
  wrote into the config and `CheckEndpoint` never saw. In the row that jumps out from a local endpoint, the excerpt still
  crosses the network in cleartext — the only reason `CheckEndpoint` allows http, "it does not cross the wire", does not
  hold here.
- **No error anywhere**: in 35 calls (5 status codes × 7 targets) `Judge` returned `err == nil` every time, and the target's
  200 was accepted as a verdict; the `scan --llm` report has nothing saying "the judge's request was forwarded elsewhere".
  Invariant #5 ("no omission may be silent") is about what was not seen; here something was **sent to a place never
  mentioned**, and that should not be silent either.
- The known Go defect CVE-2024-45336 (`Authorization` carried again on `a.com` → `b.com/1` → `b.com/2`) is measured as fixed
  on go1.23.5: neither hop carries the key. So the problem is not in Go's filtering layer, but that **the judge should not
  follow a cross-origin redirect at all**.

## Initial direction

Add a `CheckRedirect` to the client `NewHTTP` builds: follow only **same-origin** redirects (scheme and host:port both equal
to those of the configured endpoint), refuse all others without sending that hop; a refusal is not a crash but a failure of
this call, which reaches the report through the existing `LLM-000` path ("LLM judge failed on N call(s) …"), with the error
naming the scheme://host it was redirected to; the static results do not change by a character, and there is no retry (it
is a definite answer, not jitter). P-003's source check `TestZeroDial_NoClientOutsideTheJudge` describes the literal
`http.Client{Transport: Transport}`; one more field means its wording, invariant #1 and spec §16.4/§13 have to follow
together, so that "there is only one client, built in `NewHTTP`, and its transport is the seam" stays true to its word.

## Done criteria

- [x] `TestNewHTTP_RefusesCrossOriginRedirects` (`internal/judge/redirect_test.go`, new): through `NewHTTP(…, nil)` and the
  `Transport` seam (routed by host name to local httptest servers, the https ones with httptest's own certificates, no
  network egress), the six cross-origin targets in the problem table × 301/302/303/307/308, 30 rows in total: **the target
  receives 0 requests**; the endpoint receives exactly 1; the call returns an error, `errors.As` retrieves the redirect
  refusal, `isRetryable` is false; the error states the status code, the target's scheme://host and the configured origin,
  and does **not** carry the target's path. Today all 30 rows are red (the target receives requests, `err == nil`)
- [x] **Reverse assertion** (same file): an https endpoint that does not redirect behaves as today — key and excerpt sent,
  verdict parsed, seam count 1; same-origin redirects (307/308 to another path on the same origin, 301 switching to GET) are
  still followed, and the second hop goes through the same seam, carrying the key and the excerpt to the same server (seam
  count 2); a same-origin redirect loop stops at the 10th hop (Go's default limit is kept), instead of being followed until
  the timeout
- [x] `TestE2E_JudgeRedirectIsRefusedAndReported` (`cmd/aguard/judge_redirect_test.go`, new): `scan --llm` against an
  endpoint that answers 307 with `Location` pointing to another origin (another local port — the same host name, so today Go
  carries the key along too): the target receives 0 requests; the report has one `LLM-000` whose `Why` names the redirect
  and the target; `JudgeSummary` ran, `Failed == Calls`, `Retries == 0` (`max_retries` is 2); `Overall` and each artifact's
  static findings are identical, finding for finding, to a scan without `--llm`. Red today: the target receives the key and
  the excerpt, and the report has nothing
- [x] `TestLLMTest_RedirectIsRefused` (same as above): `llm test` fails against the same endpoint, the error names the
  redirect and the target, and the target receives 0 requests. **Reverse assertion**: `TestLLMCommands_SetupTestStatus`
  still green without a character changed — against an endpoint that does not redirect, `llm test` still reports `OK ·`
- [x] P-003's `TestZeroDial_OnlyTheJudgeConnects`, `TestZeroDial_ClaimsNameTheTest`, `TestZeroDial_NoClientOutsideTheJudge`
  still green; the source check's comment and error, invariant #1, and spec §16.4/§13 describe the shape of the new literal,
  and the sentence "the judge package has only the one client in `NewHTTP`, and its transport is the seam" still holds word
  for word
- [x] `TestZeroDial_SeamClientLiteralShapes` (`cmd/aguard/zero_dial_source_test.go`, new, added during implementation):
  today's `NewHTTP` literal (seam + `CheckRedirect`) counts as a seam client; adding this key did not loosen the part that
  actually matters — a literal whose `Transport` is not the seam, that has no `Transport` (and so falls back to
  `http.DefaultTransport`), or that does not name all its keys, does not count
- [x] Reverse assertions still green without a character changed: `TestHTTPClient_RoundTripAndRedaction`,
  `TestHTTPClient_ClassifiesRetryable`, `TestRun_RetriesOnlyRetryableErrors`, `TestCheckEndpoint`,
  `TestLLMSetup_KeyRoutesAndCleartextRefusal`, the existing `TestE2E_*`
- [x] `make verify` green; the second line of `go.mod` is still `go 1.23.5`, and `go version` shows no toolchain switch

## Out of scope

- **No change to `CheckEndpoint`**: the config-side rule (remote must be https, local may be http) does not change by a
  character; this item only deals with the 30x the endpoint answers
- **No change to the `http.Client`'s `Transport`, `Timeout`, `Jar`**: only a `CheckRedirect` is added. The seam and "the
  default client has no timeout" are untouched
- **No change to a caller-supplied client**: when `NewHTTP` receives a non-nil client it uses it as is (pinned by P-003's
  `TestNewHTTP_TransportSeam`); product code cannot build its own client (the source check), so this only affects tests
- **No change to the retry policy**: 429/5xx/transport errors are retried as before; only "redirect refused" is kept out of
  the retry. The 10-hop limit on redirect loops is still handled as a transport error (as today)
- **The judge does not stop early after the first refusal**: as with a 401, each call fails on its own, and they add up to
  one `LLM-000`
- **No new rule ID, no change to the report format**: `LLM-000` and its existing sentence "LLM judge failed on N call(s) …"
  are reused
- **No change to `internal/collect`, `internal/detect`, `internal/gate`, `internal/score`, `internal/report`**; no dependency
  added, `go.mod`/`go.sum` untouched
- **No change to proxy behaviour**: `http.DefaultTransport` still reads `HTTPS_PROXY` and similar environment variables —
  that is the user's own setting

## Must not claim

- **Do not say "the judge connects only to the configured host"**. What can be said is: **the judge does not follow a
  cross-origin redirect** (refused if any of scheme, host name, port differs). Same-origin redirects are still followed;
  where DNS resolves that name to, and where a proxy set in the environment variables forwards the request, are not covered
  by this item
- **Do not say "the key is never sent in cleartext"**: this item closes the redirect path; a local endpoint
  (`http://localhost…`) is cleartext to begin with, and `CheckEndpoint` allows it because it does not cross the wire
- **Do not say Go's header filtering "is already enough"**, nor the reverse, that Go has a vulnerability: Go strips
  cross-host `Authorization` as documented; it just compares host names (not the scheme or the port, and a subdomain counts
  as the same party), and it never strips the request body — those two points are the problem table
- **Do not describe a refused redirect as "the endpoint is malicious"**: the error is neutral — not followed, nothing sent
  there, and if that address is the real endpoint, set `llm.base_url` to it
- Do not say "all redirects are refused": same-origin ones are followed

## Work items

| W | In one sentence | Commit message (no sha, rebase changes it) |
|---|---|---|
| 1 | New tests in the judge package and in cmd, run red (the target receives requests, `err == nil`, no `LLM-000` in the report) | `judge, cmd: tests — a redirect from the judge's endpoint is followed to a plaintext or unconfigured origin, and nothing says so (P-023)` |
| 2 | Same-origin redirect policy on `NewHTTP`'s client; a refusal is a call failure that is not retried | `judge: the client follows a redirect only within the configured origin; any other is refused before the hop is sent, and not retried (P-023)` |
| 3 | The source check's comment and error, invariant #1, spec §16.4/§13 follow the shape of the new literal, and state that the judge does not follow a cross-origin redirect | `cmd, rules, spec: the zero-dial source check names the seam client with its redirect policy, and invariant #1 says the judge does not follow a redirect out of its origin (P-023)` |
| 4 | `judge.md`, `docs/llm-judge*.md`, spec §11 get the redirect sentence | `docs, rules: the judge follows a redirect only within the configured origin (P-023)` |
| 5 | (added during implementation) remove the false sentence "comparing with `via[0]` guards against drifting away step by step" (see the correction in open question 2) | `judge, rules: same origin is an equivalence, so the comparison with via[0] guards no chain a previous-hop comparison would miss — drop that claim (P-023)` |
| 6 | (added during implementation) the tests' TLS fixtures clone the transport of httptest's own client instead of assembling a TLS config of their own | `judge: tests — the redirect fixtures clone httptest's TLS client transport instead of building a TLS config of their own (P-023)` |
| 7 | "Done" in this file, the index | `proposals: P-023 (P-023)` |

## Open questions

1. **Refuse all, or only cross-origin?**
   **Recommendation**: only cross-origin. A same-origin redirect (only the path differs) sends nothing anywhere new: the key
   and the excerpt still go to the origin the user configured and `CheckEndpoint` allowed. Refusing all would turn harmless
   behaviour such as "the endpoint normalises its path" into a judge failure, which goes beyond the two things this item
   addresses (cleartext, an unconfigured host).
   **Decided (2026-10-09)**: as recommended.
2. **How is "same origin" compared? Against what?**
   **Recommendation**: lowercase scheme, lowercase host name, and port (filled in as 443/80 by scheme when not written) must
   all three be equal; the comparison is against the hop of the **configured endpoint** (`via[0]`). (Correction during
   implementation: the design said here "do not compare with the previous hop, or it can drift away step by step", which was
   wrong — same origin is an equivalence relation, and comparing with the previous hop gives exactly the same result;
   choosing `via[0]` only makes "the configured origin" directly visible in the code.) The port has to be compared: another
   port on the same host can be another service, and Go ignores the port when deciding whether to carry the key (the last
   row of the problem table).
   **Decided (2026-10-09)**: as recommended.
3. **Should a refused redirect be retried?**
   **Recommendation**: no. It is a definite answer from the endpoint, not jitter; a retry would only send the same endpoint
   a few more copies of the same excerpt, to be refused a few more times.
   **Decided (2026-10-09)**: as recommended.
4. **Should the error name the redirect target? How much of it?**
   **Recommendation**: the status code, the target's scheme://host[:port] (quoted with `%q`: `Location` is written by the
   endpoint, and the quotes make control and direction characters visible), and the configured origin; **not the path or
   the query** (signed URLs and the like put one-time credentials in the query). What the operator needs is "where it was
   redirected to", to decide whether to change `llm.base_url`.
   **Decided (2026-10-09)**: as recommended.
5. **A custom `CheckRedirect` replaces Go's default "stop after 10 hops"; keep that?**
   **Recommendation**: keep it, the same 10 hops and the same error text, handled as today (as a transport error);
   otherwise a same-origin redirect loop would be followed until the single call's timeout.
   **Decided (2026-10-09)**: as recommended.
6. **Should P-003's source check also pin "`NewHTTP`'s literal must carry `CheckRedirect`"?**
   **Recommendation**: no. Measured: `isSeamClientLiteral` already allows other keyed fields (its comment says, in its own
   words, "a redirect policy or a timeout may be added; they carry no transport"), and with `CheckRedirect` added the check
   is still green; what is wrong is only its comment, its error, and the shape of the literal written in invariant #1 / the
   spec. The source check is about "is there dialing that bypasses the two counters"; the redirect policy is pinned by the
   behavioural tests through `NewHTTP(…, nil)`; binding the two into one check would mean that changing either side requires
   understanding the other first. So only the wording changes, and it states: `CheckRedirect` only decides whether the next
   hop is sent, every hop it allows goes through the same `Transport`, and the counters still count it.
   **Decided (2026-10-09)**: as recommended.

## Done

```
Merged: PR #34 (2026-10-09; find the sha with git log --grep P-023)
Released: v0.19.0
Evidence: TestNewHTTP_RefusesCrossOriginRedirects (internal/judge/redirect_test.go); at W1, on origin/main, 30/30 subtests red, two lines each: "the redirect target received 1 request(s) (first: POST, API key true, excerpt true)…" (307/308 for the same-host downgrade to http, the subdomain, another local port; 301–303 are GET, key true) / for a host change "API key false, excerpt true" (307/308), plus "the call succeeded: a verdict was taken from <target>"; 30/30 green after W2
Evidence: TestE2E_JudgeRedirectIsRefusedAndReported (cmd/aguard/judge_redirect_test.go); red at W1: "the redirect target received 4 request(s)" + "no LLM-000 note … notes = []"; green after W2 — target 0 requests, LLM-000's Why = "LLM judge failed on 4 call(s); those checks did not run (first error: call judge endpoint: the endpoint answered 307 with a redirect to "http://127.0.0.1:<port>", outside the configured origin http://127.0.0.1:<port>: not followed, and nothing was sent there (if that address is the real endpoint, set llm.base_url to it))", JudgeSummary Calls 4 / Failed 4 / Retries 0 (max_retries 2), Overall the same as the scan without --llm, static findings identical one for one, no LLM findings
Evidence: TestLLMTest_RedirectIsRefused (same as above); red at W1: "llm test passed against an endpoint that redirects to http://127.0.0.1:<port>: output "OK · test-model answered in 1ms …""; green after W2, error "<endpoint> did not answer for model test-model: call judge endpoint: the endpoint answered 307 with a redirect to …", target 0 requests
Evidence: reverse assertion TestNewHTTP_SameOriginRedirectsAndPlainCallsUnchanged already green on origin/main (5/5 subtests: no redirect, same-origin 301/307/308, same-origin loop stops at 10 hops), still green after W2 — https endpoint without a redirect seam count 1, same-origin redirect seam count 2, second hop carries the key (307/308 also the excerpt)
Evidence: mutation (not committed, reverted with git checkout right after each run, git status empty) — a. remove CheckRedirect: cross-origin 30/30 red + two cmd tests red, the three TestZeroDial_* still green (the source check does not pin the redirect policy, see open question 6); b. compare the host name only (not scheme, port): 10/30 red (the same-host downgrade to http and the other-local-port rows × 5) + two cmd tests red; c. remove the errors.As branch in chat: 30/30 red (isRetryable true, and the full Location carried by url.Error shows up in the error: "Post \"http://example.com/landing/chat?sig=one-time-token\": …") + e2e red (retries); d. error writes the full Location: 30/30 red (carries the query); e. remove the 10-hop limit: the same-origin loop row red; f. isSeamClientLiteral allows a literal without Transport: TestZeroDial_SeamClientLiteralShapes red
Evidence: the source check was already green on the new literal — after W2 added CheckRedirect and before W3 changed the wording, TestZeroDial_NoClientOutsideTheJudge PASS (isSeamClientLiteral allows other keyed fields); what W3 changed is its comment and error, and the shape of the literal written in invariant #1 and spec §16.4/§13; TestZeroDial_OnlyTheJudgeConnects, TestZeroDial_ClaimsNameTheTest still green
Evidence: reverse assertions not changed by a character — git diff origin/main -- cmd/aguard/main_test.go cmd/aguard/e2e_test.go cmd/aguard/zero_dial_test.go internal/judge/judge_test.go internal/config/config_test.go is empty; internal/judge/run_test.go only changes one comment sentence in TestNewHTTP_TransportSeam (+2 −1, "a nil client is &http.Client{}" no longer holds once the redirect policy is added); TestHTTPClient_RoundTripAndRedaction, TestHTTPClient_ClassifiesRetryable, TestHTTPClient_CountsTokens, TestRun_RetriesOnlyRetryableErrors, TestNewHTTP_TransportSeam, TestCheckEndpoint, TestLLMCommands_SetupTestStatus, TestLLMSetup_KeyRoutesAndCleartextRefusal, the fourteen existing TestE2E_* still green
Evidence: Out of scope — git diff --stat origin/main -- internal/config internal/collect internal/detect internal/gate internal/score internal/report internal/model go.mod go.sum README.md README.zh-CN.md docs/architecture.md docs/architecture.zh-CN.md baselines is empty; the product-code changes are internal/judge/openai.go (+12 −1: one more CheckRedirect in the literal, a non-retrying branch in chat, comments) and the new file internal/judge/redirect.go (74 lines)
Evidence: make verify: all gates passed; go version go1.23.5 (no toolchain switch), go.mod second line go 1.23.5; collect / detect unchanged, no scan on a real machine needed
```
