// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// newNonce returns an unpredictable barrier token (spec §5.2). crypto/rand failure is
// astronomically unlikely; if it ever happens we FAIL the call (→ coverage note) rather than
// proceed with a guessable barrier a hostile skill could forge to break out.
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("nonce barrier unavailable (crypto/rand failed): %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// HTTPClient talks to an OpenAI-compatible /chat/completions endpoint (default: a LOCAL
// model, e.g. Ollama at http://localhost:11434/v1). The API key, when present, comes from
// an env var (config.APIKey) — never from disk.
type HTTPClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client

	// Token counters, reported by the endpoint. Atomic because a run is concurrent. They are
	// telemetry only: nothing here can reach a finding or the score.
	promptTokens     atomic.Int64
	completionTokens atomic.Int64
	// cachedPromptTokens is the part of promptTokens the endpoint says it served from its prompt
	// cache (usage.prompt_tokens_details.cached_tokens, P-071): a subset, never added to the
	// prompt count. The vendor bills it at its own rate; nothing here prices anything.
	cachedPromptTokens atomic.Int64
	// noTemperature is set the first time the endpoint refuses the temperature member (a 400
	// naming the parameter, as some models do: only their default is allowed), after which every
	// call omits it (P-052). A consensus run then samples at the model's default, which is what
	// such a model offers; the run's summary says so (temperature_omitted).
	noTemperature atomic.Bool
}

// TemperatureOmitted reports whether the endpoint refused the temperature member and the client
// now sends calls without it.
func (c *HTTPClient) TemperatureOmitted() bool { return c.noTemperature.Load() }

// Transport is a TEST SEAM, and nothing outside tests assigns it. It is the RoundTripper a client
// built by NewHTTP(…, nil) uses; nil — the only value production ever leaves here — means
// http.DefaultTransport, exactly what the bare &http.Client{} this replaced used.
//
// It exists for invariant #1 (no network except the explicitly enabled judge): the commands pass
// a nil client, so this is the one point every judge request crosses, and a test in cmd/aguard
// swaps it for a counter to prove which entry points send anything and which send nothing. A
// caller that supplies its own *http.Client is not affected by it.
var Transport http.RoundTripper

// NewHTTP builds a client. httpClient may be nil; tests inject their own. baseURL is the
// OpenAI-compatible root (…/v1); "/chat/completions" is appended.
//
// The default client follows a redirect only within the configured origin (sameOriginOnly): the
// endpoint CheckEndpoint vetted is the only place the key and the excerpts may go, and a 30x is the
// endpoint naming another one. Every hop it does follow crosses the same Transport.
//
// The default client carries NO timeout of its own on purpose: the deadline belongs to the
// caller's context, one per call, so the configured llm.timeout is the real ceiling instead
// of silently losing to a hard-coded one. judge.Run always sets that deadline — any other
// caller MUST do the same, or a hung endpoint hangs the process.
func NewHTTP(baseURL, apiKey, model string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Transport: Transport, CheckRedirect: sameOriginOnly}
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   RequestModel(model),
		http:    httpClient,
	}
}

// TokenUsage is what an endpoint reported about the tokens of the calls made so far. A struct, not
// three positional ints of one type, so two of them cannot be swapped at a call site (P-071).
type TokenUsage struct {
	Prompt     int
	Completion int
	// CachedPrompt is the part of Prompt the endpoint served from its prompt cache — a subset of
	// Prompt, never in addition to it. 0 when the endpoint does not report the field.
	CachedPrompt int
}

// Usage returns the tokens this endpoint has reported so far. Endpoints that omit the usage
// field report zeros — this is a cost baseline, not an accounting guarantee.
func (c *HTTPClient) Usage() TokenUsage {
	return TokenUsage{
		Prompt:       int(c.promptTokens.Load()),
		Completion:   int(c.completionTokens.Load()),
		CachedPrompt: int(c.cachedPromptTokens.Load()),
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"` // nil once the endpoint refused it (P-052)
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		// Optional (P-071): absent, null, or a null cached_tokens all decode as 0.
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Judge sends one redacted comparison and parses the model's JSON verdict.
// Ping makes one minimal chat call and reports how long it took. It is the health check behind
// `aguard llm test`: the same endpoint, model, key and request shape a real judge run uses, so a
// wrong URL, a bad key, a renamed model or a blocked network fails HERE, with the server's own
// error in hand, rather than as N failed calls at the end of a scan. Nothing scanned is sent.
func (c *HTTPClient) Ping(ctx context.Context) (time.Duration, error) {
	started := time.Now()
	_, err := c.chat(ctx, "You are a connectivity check.", "Reply with the single word OK.", 0)
	return time.Since(started), err
}

func (c *HTTPClient) Judge(ctx context.Context, r Request) (Verdict, error) {
	nonce, err := newNonce()
	if err != nil {
		return Verdict{}, err
	}
	content, err := c.chat(ctx, systemPromptFor(r, nonce), userPrompt(r, nonce), r.Temperature)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(content)
}

// chat POSTs one system+user exchange to the /chat/completions endpoint and returns the
// assistant message content. Shared by Judge and Triage so the transport/redaction-escaping
// path is identical.
func (c *HTTPClient) chat(ctx context.Context, system, user string, temperature float64) (string, error) {
	content, sent, err := c.chatOnce(ctx, system, user, temperature)
	if err != nil && sent && refusesTemperature(err) {
		// The endpoint answered the shape, not the content: this model takes no temperature
		// member. Remember it and send the same call once more without one (P-052); the retry
		// counters are for "not now" answers, and this is not one. The decision rests on what
		// THIS call sent, not on the flag: concurrent first calls are all refused together, and
		// the one that sets the flag must not rob the others of their re-send.
		c.noTemperature.Store(true)
		content, _, err = c.chatOnce(ctx, system, user, temperature)
	}
	return content, err
}

// refusesTemperature recognises the 400 an OpenAI-shaped endpoint returns for a model that allows
// only its default temperature: the body names the parameter and the unsupported-value code.
func refusesTemperature(err error) bool {
	s := err.Error()
	return strings.Contains(s, "returned 400") && temperatureParam.MatchString(s) && strings.Contains(s, "unsupported_value")
}

var temperatureParam = regexp.MustCompile(`"param"\s*:\s*"temperature"`)

// chatOnce is one POST; chat wraps it with the temperature fallback. sentTemperature says whether
// this request carried the member, which is what decides whether a refusal earns a re-send.
func (c *HTTPClient) chatOnce(ctx context.Context, system, user string, temperature float64) (content string, sentTemperature bool, err error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false) // keep <REDACTED> / shell metachars literal for the model
	// 0 for a normal call — as deterministic as the endpoint allows. Consensus sampling
	// passes a non-zero value; either way the DETERMINISTIC score never depends on this.
	// Nil once the endpoint said it takes no such member.
	temp := &temperature
	if c.noTemperature.Load() {
		temp = nil
	}
	sentTemperature = temp != nil
	if err := enc.Encode(chatRequest{
		Model:       c.model,
		Temperature: temp,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}); err != nil {
		return "", sentTemperature, fmt.Errorf("marshal judge request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", sentTemperature, fmt.Errorf("build judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A redirect out of the configured origin is the endpoint's answer, and a final one:
		// retrying only sends the same excerpts to it again, to be refused again (P-023).
		var refused *redirectRefused
		if errors.As(err, &refused) {
			return "", sentTemperature, fmt.Errorf("call judge endpoint: %w", refused)
		}
		// A transport error is usually transient (connection reset, DNS blip, timeout on the
		// endpoint's side). The context deadline is NOT: retrying a call we already ran out of
		// time for just burns the rest of the run.
		if ctx.Err() != nil {
			return "", sentTemperature, fmt.Errorf("call judge endpoint: %w", err)
		}
		return "", sentTemperature, Retryable(fmt.Errorf("call judge endpoint: %w", err), 0)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", sentTemperature, fmt.Errorf("read judge response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		herr := fmt.Errorf("judge endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		// 429 and 5xx are the endpoint saying "not now", not "never" — the difference matters
		// on a metered API, where one rate-limit burst would otherwise drop a batch of checks.
		// 4xx (bad key, bad model, malformed request) is a real answer; retrying it is noise.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return "", sentTemperature, Retryable(herr, retryAfter(resp.Header.Get("Retry-After")))
		}
		return "", sentTemperature, herr
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", sentTemperature, fmt.Errorf("parse judge envelope: %w", err)
	}
	c.promptTokens.Add(cr.Usage.PromptTokens)
	c.completionTokens.Add(cr.Usage.CompletionTokens)
	c.cachedPromptTokens.Add(cr.Usage.PromptTokensDetails.CachedTokens)
	if len(cr.Choices) == 0 {
		return "", sentTemperature, fmt.Errorf("judge returned no choices")
	}
	return cr.Choices[0].Message.Content, sentTemperature, nil
}

// retryAfter parses a Retry-After header (delta-seconds or HTTP-date). An unparsable or
// absurd value yields 0, i.e. "use our own backoff" — a hostile or broken endpoint must not
// be able to park the scan for an hour.
func retryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, maxBackoff)
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return min(d, maxBackoff)
		}
	}
	return 0
}
