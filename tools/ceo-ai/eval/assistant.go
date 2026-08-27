package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AssistantClient calls the Mesha leadership assistant endpoint server-side.
// Tenant + role scope come from the bearer session on the server, never from
// the request body — the harness only sends the question text (and an optional
// conversation id), exactly like the browser would.
type AssistantClient struct {
	URL     string
	Bearer  string
	Tenant  string
	Timeout time.Duration
	HTTP    *http.Client
}

type askRequest struct {
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id,omitempty"`
	// Stream MUST be false: the /ceo-ai/ask route defaults to an SSE token
	// stream (backend/internal/ceoai/adapters/http/routes.go), whose
	// text/event-stream frames are not JSON. The harness needs a single JSON
	// envelope, so it opts out of streaming explicitly.
	Stream bool `json:"stream"`
}

// errRateLimited signals the assistant refused this call because the per-user
// limiter tripped (a 200 refusal envelope, not an HTTP error). The caller paces
// and retries rather than scoring it as a wrong answer.
var errRateLimited = fmt.Errorf("assistant rate-limited")

// ask sends one question and returns the parsed answer plus the measured
// wall-clock latency in milliseconds.
func (c AssistantClient) ask(ctx context.Context, question string) (*AssistantResponse, int64, error) {
	body, err := json.Marshal(askRequest{Question: question, Stream: false})
	if err != nil {
		return nil, 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Mesha-Eval", "1")
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	if c.Tenant != "" {
		req.Header.Set("X-GoatOS-Tenant-ID", c.Tenant)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, latency, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, latency, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, latency, fmt.Errorf("assistant auth rejected (status %d) — check MESHA_EVAL_BEARER leadership grant", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, latency, fmt.Errorf("assistant status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out AssistantResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, latency, fmt.Errorf("assistant response not JSON: %s", truncate(string(raw), 300))
	}
	// The per-user limiter returns a 200 refusal envelope (not an HTTP 429).
	// Surface it as a distinct sentinel so the runner paces and retries instead
	// of recording a rate-limit refusal as a graded answer.
	if out.Mode == "refused" && strings.Contains(out.Answer, "sending requests too quickly") {
		return nil, latency, errRateLimited
	}
	return &out, latency, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
