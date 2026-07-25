// Package cubeclient is a typed, tenant-scoped Go client for the Mesha Cube
// Core governed-metric service. It is the ONLY sanctioned way for the Mesha
// backend to read official leadership KPIs from Cube.
//
// Design constraints (from the CEO-AI plan):
//   - Cube owns the metric formulas; this client only asks for members.
//   - Tenant scope always comes from the server session and is signed into a
//     short-lived JWT; it is never taken from user text and never a query field.
//   - Hard per-call deadline (default 8s), bounded retries, typed errors.
//   - Self-contained: no import of the parent ceoai package (avoids cycles).
package cubeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout   = 8 * time.Second
	defaultMaxRetry  = 2
	defaultRowLimit  = 100
	defaultTokenTTL  = 2 * time.Minute
	loadPath         = "/cubejs-api/v1/load"
	cubeContinueWait = "Continue wait"
)

// Config configures a Client. BaseURL and APISecret are required.
type Config struct {
	// BaseURL is the Cube service origin, e.g. from MESHA_CUBE_URL.
	BaseURL string
	// APISecret is the shared Cube JWT signing secret (MESHA_CUBE_API_SECRET).
	APISecret string
	// Timeout is the per-call deadline (default 8s).
	Timeout time.Duration
	// MaxRetries is the number of retries on transient failure (default 2).
	MaxRetries int
	// RowLimit is the hard ceiling applied to every query (default 100).
	RowLimit int
	// HTTPClient is optional; if nil a default client is created.
	HTTPClient *http.Client
}

// Client is a safe, reusable Cube client.
type Client struct {
	baseURL    string
	secret     string
	timeout    time.Duration
	maxRetries int
	rowLimit   int
	httpClient *http.Client
}

// New builds a Client. It returns ErrNotConfigured if base URL or secret is
// missing so callers fail fast rather than at first query.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APISecret) == "" {
		return nil, ErrNotConfigured
	}
	c := &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		secret:     cfg.APISecret,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		rowLimit:   cfg.RowLimit,
		httpClient: cfg.HTTPClient,
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	if c.maxRetries < 0 {
		c.maxRetries = defaultMaxRetry
	}
	if c.rowLimit <= 0 || c.rowLimit > defaultRowLimit {
		c.rowLimit = defaultRowLimit
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout}
	}
	return c, nil
}

// Load runs a governed metric query scoped to tenantID and returns typed rows.
//
// tenantID MUST come from the server-side session. An empty tenantID is a
// programming error and returns ErrMissingTenant with no network call.
func (c *Client) Load(ctx context.Context, tenantID string, q Query) (*Result, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrMissingTenant
	}
	// Enforce the hard row ceiling regardless of caller input.
	if q.Limit <= 0 || q.Limit > c.rowLimit {
		q.Limit = c.rowLimit
	}

	body, err := json.Marshal(struct {
		Query Query `json:"query"`
	}{Query: q})
	if err != nil {
		return nil, fmt.Errorf("cubeclient: marshal query: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Small linear backoff; bounded by ctx / per-call timeout.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
		res, retriable, err := c.doLoad(ctx, tenantID, body)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !retriable {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = ErrTimeout
	}
	return nil, lastErr
}

// doLoad performs a single request. It returns (result, retriable, error).
func (c *Client) doLoad(ctx context.Context, tenantID string, body []byte) (*Result, bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	token, err := signSecurityContext(c.secret, tenantID, defaultTokenTTL)
	if err != nil {
		return nil, false, fmt.Errorf("cubeclient: sign token: %w", err)
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL+loadPath, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Timeout / transient network → retriable.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, true, ErrTimeout
		}
		return nil, true, fmt.Errorf("cubeclient: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, fmt.Errorf("cubeclient: read body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, false, ErrUnauthorized
	case resp.StatusCode >= 500:
		return nil, true, &APIError{StatusCode: resp.StatusCode, Body: truncate(string(raw))}
	case resp.StatusCode >= 400:
		return nil, false, &APIError{StatusCode: resp.StatusCode, Body: truncate(string(raw))}
	}

	// Cube can return HTTP 200 with {"error":"Continue wait"} while a query
	// warms up. Treat that as retriable.
	var probe struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &probe)
	if probe.Error != "" {
		if strings.Contains(probe.Error, cubeContinueWait) {
			return nil, true, ErrTimeout
		}
		return nil, false, &APIError{StatusCode: resp.StatusCode, Body: truncate(probe.Error)}
	}

	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("cubeclient: decode response: %w", err)
	}
	return &out, false, nil
}

func truncate(s string) string {
	const max = 512
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
