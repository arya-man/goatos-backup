package httpresponse

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestWriteErrorLogs5xxCause(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	handler := httpmiddleware.RequestContext(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, log, http.StatusInternalServerError, map[string]any{
			"code":         "internal_error",
			"message":      "internal server error",
			"field_errors": []any{},
			"trace_id":     "trace-1",
			"retryable":    true,
		}, errors.New("database unavailable"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("traceparent", "trace-1")
	req.Header.Set("X-GoatOS-Tenant-ID", "tenant-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	logOut := logBuf.String()
	for _, want := range []string{"http_5xx", "database unavailable", "trace-1", "req-1", "tenant-1", "GET /goats/search"} {
		if !strings.Contains(logOut, want) {
			t.Fatalf("5xx log missing %q: %s", want, logOut)
		}
	}
}

func TestWriteErrorDoesNotLog4xx(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	rec := httptest.NewRecorder()

	WriteError(rec, req, log, http.StatusBadRequest, map[string]any{
		"code":         "invalid_request",
		"message":      "bad request",
		"field_errors": []any{},
		"trace_id":     "trace-1",
		"retryable":    false,
	}, errors.New("validation failed"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("4xx should not emit http_5xx log: %s", logBuf.String())
	}
}
