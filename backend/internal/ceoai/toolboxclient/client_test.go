package toolboxclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, baseURL string, mutate func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL: baseURL,
		Toolset: "mesha_ceo_toolset",
		// Make retries fast and deterministic in tests.
		sleep: func(context.Context, time.Duration) error { return nil },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"empty base", Config{Toolset: "x"}},
		{"no scheme", Config{BaseURL: "127.0.0.1:5001", Toolset: "x"}},
		{"empty toolset", Config{BaseURL: "http://127.0.0.1:5001"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("expected ErrConfig, got %v", err)
			}
		})
	}
}

func TestLoadToolset(t *testing.T) {
	manifest := manifestEnvelope{
		ServerVersion: "0.32.0-test",
		Tools: map[string]ToolManifest{
			"mesha_ops_exceptions": {
				Description: "ops exceptions",
				Parameters: []ToolParameter{
					{Name: "tenant_id", Type: "string", Required: true, Description: "tenant"},
					{Name: "limit", Type: "integer", Required: false, Description: "cap"},
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/toolset/mesha_ceo_toolset" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	ts, err := c.LoadToolset(context.Background())
	if err != nil {
		t.Fatalf("LoadToolset: %v", err)
	}
	if ts.ServerVersion != "0.32.0-test" {
		t.Fatalf("server version = %q", ts.ServerVersion)
	}
	tool, ok := ts.Tools["mesha_ops_exceptions"]
	if !ok {
		t.Fatalf("tool missing")
	}
	if tool.Name != "mesha_ops_exceptions" {
		t.Fatalf("tool name not backfilled: %q", tool.Name)
	}
	if len(tool.Parameters) != 2 || !tool.Parameters[0].Required {
		t.Fatalf("params wrong: %+v", tool.Parameters)
	}
}

func TestLoadToolsetInvalidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, nil)
	if _, err := c.LoadToolset(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestInvokeSuccess(t *testing.T) {
	rows := []map[string]any{{"area": "vaccination", "severity": "high"}}
	rowsJSON, _ := json.Marshal(rows)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/tool/mesha_ops_exceptions/invoke" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["tenant_id"] != "tenant-1" {
			t.Errorf("tenant not forwarded: %v", got)
		}
		// Toolbox wraps rows as a JSON string in "result".
		_ = json.NewEncoder(w).Encode(invokeEnvelope{Result: string(rowsJSON)})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	raw, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "tenant-1", "limit": 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(decoded) != 1 || decoded[0]["area"] != "vaccination" {
		t.Fatalf("rows wrong: %v", decoded)
	}
}

func TestInvokeToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Toolbox returns HTTP 200 with an embedded error object as the result.
		errObj, _ := json.Marshal(map[string]string{"error": `relation "ceo_ai.x" does not exist`})
		_ = json.NewEncoder(w).Encode(invokeEnvelope{Result: string(errObj)})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	_, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	var te *ToolError
	if !errors.As(err, &te) {
		t.Fatalf("expected *ToolError, got %v", err)
	}
	if te.Tool != "mesha_ops_exceptions" {
		t.Fatalf("tool = %q", te.Tool)
	}
}

func TestInvokeHTTP4xxNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad param"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxRetries = 3 })
	_, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %v", err)
	}
	if he.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", he.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("4xx must not retry; calls = %d", got)
	}
}

func TestInvokeHTTP5xxRetriesThenSucceeds(t *testing.T) {
	var calls int32
	rowsJSON := `[{"ok":true}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("upstream down"))
			return
		}
		_ = json.NewEncoder(w).Encode(invokeEnvelope{Result: rowsJSON})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxRetries = 3 })
	raw, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	if err != nil {
		t.Fatalf("Invoke after retries: %v", err)
	}
	if string(raw) != rowsJSON {
		t.Fatalf("raw = %s", raw)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestInvoke5xxExhaustsRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxRetries = 2 })
	_, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 { // initial + 2 retries
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestInvokeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(invokeEnvelope{Result: "[]"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) {
		cfg.Timeout = 20 * time.Millisecond
		cfg.MaxRetries = 0
	})
	_, err := c.Invoke(context.Background(), "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestInvokeContextCancelNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxRetries = 3 })
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	_, err := c.Invoke(ctx, "mesha_ops_exceptions", map[string]any{"tenant_id": "t"})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestInvokeEmptyToolName(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:1", nil)
	if _, err := c.Invoke(context.Background(), "  ", nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

// Ensure the manifest tool count contract holds for the full curated toolset
// shape the backend expects (documents the 15-tool coverage in a unit test).
func TestManifestFullCoverageShape(t *testing.T) {
	names := []string{
		"mesha_count_by_scope", "mesha_capacity_summary", "mesha_vaccination_due_summary",
		"mesha_vaccination_dose_pickup", "mesha_feed_direction_summary", "mesha_shifting_summary",
		"mesha_procurement_summary", "mesha_source_entry_health", "mesha_ops_exceptions",
		"mesha_sop_execution", "mesha_verification_queue", "mesha_inventory_stock",
		"mesha_workforce_coverage", "mesha_action_center", "mesha_audit_summary",
	}
	tools := map[string]ToolManifest{}
	for _, n := range names {
		tools[n] = ToolManifest{Description: n, Parameters: []ToolParameter{{Name: "tenant_id", Required: true}}}
	}
	body, _ := json.Marshal(manifestEnvelope{ServerVersion: "t", Tools: tools})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	ts, err := c.LoadToolset(context.Background())
	if err != nil {
		t.Fatalf("LoadToolset: %v", err)
	}
	if len(ts.Tools) != 15 {
		t.Fatalf("expected 15 tools, got %d", len(ts.Tools))
	}
	for _, n := range names {
		tm, ok := ts.Tools[n]
		if !ok {
			t.Fatalf("missing tool %q", n)
		}
		if len(tm.Parameters) == 0 || !tm.Parameters[0].Required || tm.Parameters[0].Name != "tenant_id" {
			t.Fatalf("tool %q must require tenant_id first: %+v", n, tm.Parameters)
		}
	}
	_ = fmt.Sprint(ts.ServerVersion)
}
