// Package toolboxclient is a small, self-contained HTTP client for the Google
// genai MCP Toolbox server (github.com/googleapis/genai-toolbox) as configured
// for the Mesha leadership assistant by docs/ceo-ai/mcp-toolbox-tools.yaml.
//
// It speaks the Toolbox native /api REST contract (enabled with --enable-api,
// which tools/dev/run-mcp-toolbox-local.sh and the Cloud Run deployment set):
//
//	GET  /api/toolset/{toolset}          -> manifest of tools + parameters
//	POST /api/tool/{tool}/invoke {json}  -> {"result": "<json string>"}
//
// The Mesha backend calls Toolbox server-side ONLY, after it has verified the
// leadership role and bound tenant_id from the session. This client never
// decides permissions and never sees database credentials; Toolbox holds the
// read-only role. The read-only SQL fallback is intentionally NOT a Toolbox
// tool (the Go sqlguard executes it), so this client only loads/invokes the
// curated ceo_ai.* tools.
//
// This package deliberately imports only the Go standard library and must not
// import its parent ceoai packages, so it stays a reusable adapter with no
// cycle back into the orchestrator.
package toolboxclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default tuning. The per-call deadline is 8s per the assistant tool contract.
const (
	DefaultTimeout    = 8 * time.Second
	DefaultMaxRetries = 2
	defaultUserAgent  = "mesha-ceoai-toolboxclient/1"
)

// Sentinel errors callers can match with errors.Is.
var (
	// ErrConfig is returned when the client is constructed with invalid config.
	ErrConfig = errors.New("toolboxclient: invalid config")
	// ErrToolNotFound is returned when the toolset manifest has no such tool.
	ErrToolNotFound = errors.New("toolboxclient: tool not found in toolset")
	// ErrInvalidResponse is returned when the server response cannot be decoded
	// into the expected Toolbox shape.
	ErrInvalidResponse = errors.New("toolboxclient: invalid response")
)

// HTTPError is a transport/status-level failure from the Toolbox server.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
	Tool       string // tool or toolset the request targeted, for logs
}

func (e *HTTPError) Error() string {
	if e.Tool != "" {
		return fmt.Sprintf("toolboxclient: http %d (%s) for %q: %s", e.StatusCode, e.Status, e.Tool, truncate(e.Body, 256))
	}
	return fmt.Sprintf("toolboxclient: http %d (%s): %s", e.StatusCode, e.Status, truncate(e.Body, 256))
}

// retryable reports whether the status warrants a retry.
func (e *HTTPError) retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// ToolError is a tool-level (business/SQL) failure. Toolbox returns HTTP 200
// with a result body of {"error": "..."} when the underlying query fails; this
// client surfaces that as a typed, NON-retryable error (the SQL is
// deterministic, so retrying is pointless).
type ToolError struct {
	Tool    string
	Message string
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("toolboxclient: tool %q failed: %s", e.Tool, e.Message)
}

// Config configures a Client.
type Config struct {
	// BaseURL is the Toolbox server origin, e.g. http://127.0.0.1:5001 locally
	// or the internal Cloud Run URL in staging. Required.
	BaseURL string
	// Toolset is the toolset name the assistant loads, e.g. mesha_ceo_toolset.
	// Required.
	Toolset string
	// AuthToken is an optional shared secret sent as `Authorization: Bearer …`
	// on every request. It is REQUIRED when BaseURL is not a loopback host: the
	// Toolbox trusts the tenant_id argument the backend injects, so an exposed
	// (non-loopback) toolbox with no auth would let any network peer invoke the
	// curated tools with an arbitrary tenant_id and read cross-tenant data. New
	// fails closed on a non-loopback BaseURL with no AuthToken.
	AuthToken string
	// HTTPClient is optional; a sane default with DefaultTimeout is used when nil.
	HTTPClient *http.Client
	// Timeout is the per-call deadline. Zero uses DefaultTimeout.
	Timeout time.Duration
	// MaxRetries bounds transient retries (network + 5xx + 429). Negative uses 0.
	// Zero-value means DefaultMaxRetries; set explicitly to a negative-safe 0 via
	// WithNoRetries if you want none.
	MaxRetries int
	// UserAgent overrides the default User-Agent header.
	UserAgent string
	// now/sleep are injectable for tests; nil uses real time.
	sleep func(context.Context, time.Duration) error
}

// Client is a concurrency-safe Toolbox HTTP client.
type Client struct {
	baseURL    string
	toolset    string
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
	userAgent  string
	authToken  string
	sleep      func(context.Context, time.Duration) error
}

// New builds a Client, validating required config.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: BaseURL is required", ErrConfig)
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("%w: BaseURL must include http(s) scheme, got %q", ErrConfig, cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.Toolset) == "" {
		return nil, fmt.Errorf("%w: Toolset is required", ErrConfig)
	}
	// Fail closed: a non-loopback Toolbox with no shared secret is a cross-tenant
	// read hole (the tool statements trust the injected tenant_id argument).
	if !isLoopbackBaseURL(base) && strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, fmt.Errorf("%w: non-loopback BaseURL %q requires an AuthToken (loopback-only otherwise)", ErrConfig, cfg.BaseURL)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	retries := cfg.MaxRetries
	if retries == 0 {
		retries = DefaultMaxRetries
	}
	if retries < 0 {
		return nil, fmt.Errorf("%w: MaxRetries must be >= 0, got %d", ErrConfig, retries)
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	sleep := cfg.sleep
	if sleep == nil {
		sleep = ctxSleep
	}
	return &Client{
		baseURL:    base,
		toolset:    strings.TrimSpace(cfg.Toolset),
		httpClient: hc,
		timeout:    timeout,
		maxRetries: retries,
		userAgent:  ua,
		authToken:  authToken,
		sleep:      sleep,
	}, nil
}

// isLoopbackBaseURL reports whether base points at the local host. A parse
// failure is treated as NON-loopback (fail closed).
func isLoopbackBaseURL(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ToolParameter describes one tool parameter as advertised by the manifest.
type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// ToolManifest describes one tool as advertised by the manifest.
type ToolManifest struct {
	Name        string          `json:"-"`
	Description string          `json:"description"`
	Parameters  []ToolParameter `json:"parameters"`
}

// Toolset is the decoded manifest for a toolset.
type Toolset struct {
	ServerVersion string
	Tools         map[string]ToolManifest
}

// manifestEnvelope matches the Toolbox /api/toolset response body.
type manifestEnvelope struct {
	ServerVersion string                  `json:"serverVersion"`
	Tools         map[string]ToolManifest `json:"tools"`
}

// LoadToolset fetches and decodes the configured toolset manifest. It is the
// discovery call the planner uses to know which tools and parameters exist.
func (c *Client) LoadToolset(ctx context.Context) (*Toolset, error) {
	path := "/api/toolset/" + urlPathEscape(c.toolset)
	body, err := c.do(ctx, http.MethodGet, path, nil, c.toolset)
	if err != nil {
		return nil, err
	}
	var env manifestEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%w: decode manifest: %v", ErrInvalidResponse, err)
	}
	if env.Tools == nil {
		return nil, fmt.Errorf("%w: manifest has no tools field", ErrInvalidResponse)
	}
	for name, t := range env.Tools {
		t.Name = name
		env.Tools[name] = t
	}
	return &Toolset{ServerVersion: env.ServerVersion, Tools: env.Tools}, nil
}

// invokeEnvelope matches the Toolbox /api/tool/{name}/invoke response body.
// result is a JSON-encoded STRING: either a rows array or an {"error":...} obj.
type invokeEnvelope struct {
	Result string `json:"result"`
}

type toolErrorPayload struct {
	Error string `json:"error"`
}

// Invoke calls a single tool with the given parameters and returns the decoded
// result rows as raw JSON (a JSON array). The caller (Mesha backend) MUST have
// already placed the session-bound tenant_id into params; this client passes
// params through verbatim and does not inject or trust any scope of its own.
//
// A tool-level failure (bad SQL / missing view) is returned as *ToolError; a
// transport/status failure is returned as *HTTPError. Both are distinguishable
// with errors.As.
func (c *Client) Invoke(ctx context.Context, tool string, params map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(tool) == "" {
		return nil, fmt.Errorf("%w: tool name is required", ErrConfig)
	}
	if params == nil {
		params = map[string]any{}
	}
	reqBody, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%w: encode params: %v", ErrConfig, err)
	}
	path := "/api/tool/" + urlPathEscape(tool) + "/invoke"
	respBody, err := c.do(ctx, http.MethodPost, path, reqBody, tool)
	if err != nil {
		return nil, err
	}

	var env invokeEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("%w: decode invoke envelope for %q: %v", ErrInvalidResponse, tool, err)
	}

	// result is a JSON string. Detect an embedded tool error object first.
	trimmed := strings.TrimSpace(env.Result)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: empty result for %q", ErrInvalidResponse, tool)
	}
	if strings.HasPrefix(trimmed, "{") {
		var te toolErrorPayload
		if err := json.Unmarshal([]byte(trimmed), &te); err == nil && te.Error != "" {
			return nil, &ToolError{Tool: tool, Message: te.Error}
		}
	}
	// Otherwise result is the rows payload (typically a JSON array). Return it
	// verbatim as RawMessage so the backend can decode into business shapes.
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("%w: result for %q is not valid JSON", ErrInvalidResponse, tool)
	}
	return json.RawMessage(trimmed), nil
}

// do performs an HTTP request with bounded retries on transient failures.
func (c *Client) do(ctx context.Context, method, path string, body []byte, tool string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential-ish backoff: 100ms, 200ms, ... capped at 1s, ctx-aware.
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			if backoff > time.Second {
				backoff = time.Second
			}
			if err := c.sleep(ctx, backoff); err != nil {
				return nil, err
			}
		}

		respBody, err := c.attempt(ctx, method, path, body, tool)
		if err == nil {
			return respBody, nil
		}
		lastErr = err

		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// attempt performs a single HTTP request.
func (c *Client) attempt(ctx context.Context, method, path string, body []byte, tool string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrConfig, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network/timeout error is transient.
		return nil, &transientError{err: err, tool: tool}
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MiB cap
	if readErr != nil {
		return nil, &transientError{err: readErr, tool: tool}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(respBody),
			Tool:       tool,
		}
	}
	return respBody, nil
}

// transientError wraps a network/read failure so isRetryable can spot it.
type transientError struct {
	err  error
	tool string
}

func (e *transientError) Error() string {
	if e.tool != "" {
		return fmt.Sprintf("toolboxclient: transport error for %q: %v", e.tool, e.err)
	}
	return fmt.Sprintf("toolboxclient: transport error: %v", e.err)
}

func (e *transientError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.retryable()
	}
	var te *transientError
	if errors.As(err, &te) {
		// Do not retry context cancellation/deadline from the CALLER; only
		// genuine transport failures. A per-attempt deadline surfaces as a
		// deadline error but the caller's ctx may still be alive.
		return !errors.Is(te.err, context.Canceled)
	}
	return false
}

// ctxSleep sleeps for d unless ctx is done first.
func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// urlPathEscape escapes a single path segment without escaping slashes into
// %2F incorrectly; toolset/tool names are simple identifiers, so a minimal
// escape of spaces and reserved chars is enough while staying dependency-free.
func urlPathEscape(seg string) string {
	// Tool/toolset names are [a-zA-Z0-9_]; return as-is when they match, else
	// fall back to a conservative replacement of spaces.
	for _, r := range seg {
		if !(r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')) {
			return strings.ReplaceAll(seg, " ", "%20")
		}
	}
	return seg
}
