// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

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
// The default client carries NO timeout of its own on purpose: the deadline belongs to the
// caller's context, one per call, so the configured llm.timeout is the real ceiling instead
// of silently losing to a hard-coded one. judge.Run always sets that deadline — any other
// caller MUST do the same, or a hung endpoint hangs the process.
func NewHTTP(baseURL, apiKey, model string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Transport: Transport}
	}
	if model == "" {
		model = "llama3.1"
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    httpClient,
	}
}

// Usage returns the tokens this endpoint has reported so far. Endpoints that omit the usage
// field report zeros — this is a cost baseline, not an accounting guarantee.
func (c *HTTPClient) Usage() (prompt, completion int) {
	return int(c.promptTokens.Load()), int(c.completionTokens.Load())
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
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
	content, err := c.chat(ctx, systemPrompt(r.Mode, nonce), userPrompt(r, nonce), r.Temperature)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(content)
}

// chat POSTs one system+user exchange to the /chat/completions endpoint and returns the
// assistant message content. Shared by Judge and Triage so the transport/redaction-escaping
// path is identical.
func (c *HTTPClient) chat(ctx context.Context, system, user string, temperature float64) (string, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false) // keep <REDACTED> / shell metachars literal for the model
	if err := enc.Encode(chatRequest{
		Model: c.model,
		// 0 for a normal call — as deterministic as the endpoint allows. Consensus sampling
		// passes a non-zero value; either way the DETERMINISTIC score never depends on this.
		Temperature: temperature,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}); err != nil {
		return "", fmt.Errorf("marshal judge request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", fmt.Errorf("build judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A transport error is usually transient (connection reset, DNS blip, timeout on the
		// endpoint's side). The context deadline is NOT: retrying a call we already ran out of
		// time for just burns the rest of the run.
		if ctx.Err() != nil {
			return "", fmt.Errorf("call judge endpoint: %w", err)
		}
		return "", Retryable(fmt.Errorf("call judge endpoint: %w", err), 0)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read judge response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		herr := fmt.Errorf("judge endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		// 429 and 5xx are the endpoint saying "not now", not "never" — the difference matters
		// on a metered API, where one rate-limit burst would otherwise drop a batch of checks.
		// 4xx (bad key, bad model, malformed request) is a real answer; retrying it is noise.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return "", Retryable(herr, retryAfter(resp.Header.Get("Retry-After")))
		}
		return "", herr
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("parse judge envelope: %w", err)
	}
	c.promptTokens.Add(cr.Usage.PromptTokens)
	c.completionTokens.Add(cr.Usage.CompletionTokens)
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("judge returned no choices")
	}
	return cr.Choices[0].Message.Content, nil
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

// parseVerdict extracts the JSON verdict from a model reply, tolerating prose or code
// fences around it by slicing the outermost { … }.
func parseVerdict(content string) (Verdict, error) {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return Verdict{}, fmt.Errorf("no JSON object in judge reply")
	}
	var v Verdict
	if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
		return Verdict{}, fmt.Errorf("parse judge verdict: %w", err)
	}
	return v, nil
}
