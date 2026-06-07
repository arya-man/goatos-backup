package httpmiddleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestContextPreservesIncomingIDs(t *testing.T) {
	var logs strings.Builder
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := RequestContext(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != "req-123" {
			t.Fatalf("request id = %q", got)
		}
		if got := TraceIDFromContext(r.Context()); got != "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01" {
			t.Fatalf("trace id = %q", got)
		}
		if got := TenantIDFromContext(r.Context()); got != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("tenant id = %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "req-123" {
		t.Fatalf("response request id = %q", rec.Header().Get("X-Request-ID"))
	}
	if rec.Header().Get("traceparent") != "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01" {
		t.Fatalf("response traceparent = %q", rec.Header().Get("traceparent"))
	}
	if !strings.Contains(logs.String(), `"status":202`) {
		t.Fatalf("request log missing status: %s", logs.String())
	}
}

func TestRequestContextGeneratesMissingRequestID(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := RequestContext(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Fatal("missing generated request id")
		}
		if TraceIDFromContext(r.Context()) == "" {
			t.Fatal("missing generated trace id")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("response missing generated request id")
	}
}
