package httpmiddleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanicRecoveryReturns500Envelope(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	// A handler that panics.
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic value")
	})

	// Wire: PanicRecovery -> RequestContext -> panicHandler
	handler := PanicRecovery(log)(RequestContext(log)(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	req.Header.Set("X-Request-ID", "req-panic-test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if envelope["code"] != "internal_error" {
		t.Errorf("expected code=internal_error, got %v", envelope["code"])
	}
	if envelope["message"] != "internal server error" {
		t.Errorf("expected generic message, got %v", envelope["message"])
	}
	if envelope["trace_id"] == nil || envelope["trace_id"] == "" {
		t.Errorf("expected trace_id in envelope, got: %v", envelope["trace_id"])
	}
	if strings.Contains(rec.Body.String(), "test panic value") {
		t.Fatalf("panic response leaked panic value: %s", rec.Body.String())
	}
}

func TestPanicRecoveryLogsStackAndPanicValue(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := PanicRecovery(log)(RequestContext(log)(http.HandlerFunc(panicSentinelHandler)))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOut := logBuf.String()
	if !strings.Contains(logOut, "http_panic") {
		t.Errorf("expected http_panic log entry, got: %s", logOut)
	}
	if !strings.Contains(logOut, "sentinel panic payload") {
		t.Errorf("expected panic value in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, "panicSentinelHandler") {
		t.Errorf("expected original handler frame in stack, got: %s", logOut)
	}
	if !strings.Contains(logOut, "goroutine") {
		t.Errorf("expected stack trace (goroutine...) in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"msg":"http_request"`) || !strings.Contains(logOut, `"status":500`) {
		t.Errorf("expected panic request-line status log, got: %s", logOut)
	}
}

func panicSentinelHandler(http.ResponseWriter, *http.Request) {
	panic("sentinel panic payload")
}

func TestPanicRecoveryDoesNotAppendEnvelopeAfterResponseStarted(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial response"))
		panic("panic after write")
	})
	handler := PanicRecovery(log)(RequestContext(log)(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-panic-after-write")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want original started response status 200", rec.Code)
	}
	if got := rec.Body.String(); got != "partial response" {
		t.Fatalf("body=%q, want only partial response without appended envelope", got)
	}
	logOut := logBuf.String()
	if !strings.Contains(logOut, "http_panic") || !strings.Contains(logOut, "panic after write") {
		t.Fatalf("panic was not logged with value: %s", logOut)
	}
}

func TestPanicRecoveryDoesNotAppendEnvelopeAfterFlush(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("flush failed: %v", err)
		}
		panic("panic after flush")
	})
	handler := PanicRecovery(log)(RequestContext(log)(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-panic-after-flush")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want original flushed response status 200", rec.Code)
	}
	if rec.Body.String() != "" {
		t.Fatalf("body=%q, want no appended envelope after flush", rec.Body.String())
	}
	if !rec.Flushed {
		t.Fatal("expected response to be flushed")
	}
	logOut := logBuf.String()
	if !strings.Contains(logOut, "http_panic") || !strings.Contains(logOut, "panic after flush") {
		t.Fatalf("panic was not logged with value: %s", logOut)
	}
}

func TestPanicRecoveryDoesNotFireOnNormalRequests(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := PanicRecovery(log)(RequestContext(log)(normalHandler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(logBuf.String(), "http_panic") {
		t.Errorf("unexpected http_panic log on normal request: %s", logBuf.String())
	}
}
