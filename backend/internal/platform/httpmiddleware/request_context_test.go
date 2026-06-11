package httpmiddleware

import (
	"crypto/rand"
	"errors"
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
		if got := ActorIDFromContext(r.Context()); got != "90000000-0000-4000-8000-000000000001" {
			t.Fatalf("actor id = %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "90000000-0000-4000-8000-000000000001")
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

func TestRequestContextAllowsResponseControllerFlush(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := RequestContext(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("flush through statusRecorder failed: %v", err)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !rec.Flushed {
		t.Fatal("expected underlying recorder to be flushed")
	}
}

func TestStatusRecorderForwardsWriteHeaderOnce(t *testing.T) {
	underlying := newCountingResponseWriter()
	rec := &statusRecorder{ResponseWriter: underlying, status: http.StatusOK}

	rec.WriteHeader(http.StatusServiceUnavailable)
	rec.WriteHeader(http.StatusTeapot)

	if rec.status != http.StatusServiceUnavailable {
		t.Fatalf("recorded status=%d, want %d", rec.status, http.StatusServiceUnavailable)
	}
	if underlying.writeHeaderCount != 1 {
		t.Fatalf("forwarded WriteHeader count=%d, want 1", underlying.writeHeaderCount)
	}
	if len(underlying.statuses) != 1 || underlying.statuses[0] != http.StatusServiceUnavailable {
		t.Fatalf("forwarded statuses=%v, want [%d]", underlying.statuses, http.StatusServiceUnavailable)
	}
}

func TestGenerateIDPanicsWhenCryptoRandFails(t *testing.T) {
	original := rand.Reader
	rand.Reader = failingReader{}
	defer func() { rand.Reader = original }()

	defer func() {
		if p := recover(); p == nil {
			t.Fatal("expected generateID to fail hard when crypto/rand fails")
		}
	}()
	_ = generateID()
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

type countingResponseWriter struct {
	header           http.Header
	writeHeaderCount int
	statuses         []int
}

func newCountingResponseWriter() *countingResponseWriter {
	return &countingResponseWriter{header: make(http.Header)}
}

func (w *countingResponseWriter) Header() http.Header {
	return w.header
}

func (w *countingResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *countingResponseWriter) WriteHeader(status int) {
	w.writeHeaderCount++
	w.statuses = append(w.statuses, status)
}
