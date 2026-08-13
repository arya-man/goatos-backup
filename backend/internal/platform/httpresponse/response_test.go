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

// TestWriteErrorLogs4xxWithCode is the regression test for the night a CEO tapped
// Publish, got a bare "HTTP 409 Conflict", and the ONLY server evidence was an
// access-log line reading `409 POST /weighing/campaigns |` — no code, no actor,
// no reason. A 4xx a real user hit must be traceable to WHY it was refused.
func TestWriteErrorLogs4xxWithCode(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	handler := httpmiddleware.RequestContext(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, log, http.StatusConflict, struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "weighing_shed_already_scheduled", Message: "Some of these sheds are already scheduled on this date."}, nil)
	}))

	req := httptest.NewRequest(http.MethodPost, "/weighing/campaigns", nil)
	req.Header.Set("X-Request-ID", "req-409")
	req.Header.Set("traceparent", "trace-409")
	req.Header.Set("X-GoatOS-Tenant-ID", "tenant-1")
	req.Header.Set("X-GoatOS-Actor-ID", "actor-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d", rec.Code)
	}
	logOut := logBuf.String()
	for _, want := range []string{
		`"msg":"http_4xx"`,
		`"level":"WARN"`,
		`"code":"weighing_shed_already_scheduled"`,
		`"status":409`,
		"POST /weighing/campaigns",
		"trace-409", "req-409", "tenant-1", "actor-1",
	} {
		if !strings.Contains(logOut, want) {
			t.Fatalf("4xx log missing %q: %s", want, logOut)
		}
	}
	if strings.Contains(logOut, "http_5xx") {
		t.Fatalf("4xx must not be logged as http_5xx: %s", logOut)
	}
}

// A map envelope (the other shape in use across handlers) must yield the same code.
func TestWriteErrorLogs4xxCodeFromMapEnvelope(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	rec := httptest.NewRecorder()

	WriteError(rec, req, log, http.StatusBadRequest, map[string]any{
		"code":    "invalid_request",
		"message": "bad request",
	}, errors.New("validation failed"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	logOut := logBuf.String()
	for _, want := range []string{`"msg":"http_4xx"`, `"code":"invalid_request"`, "validation failed"} {
		if !strings.Contains(logOut, want) {
			t.Fatalf("4xx log missing %q: %s", want, logOut)
		}
	}
}

// 401 on an unauthenticated probe is noise, not a user-facing refusal: the auth
// middleware already logs those with full context (auth_failed). Logging them a
// second time here would turn every scanner hit into a WARN pair.
func TestWriteErrorDoesNotDoubleLog401(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	rec := httptest.NewRecorder()

	WriteError(rec, req, log, http.StatusUnauthorized, map[string]any{"code": "missing_bearer_token"}, nil)

	if logBuf.Len() != 0 {
		t.Fatalf("401 should not emit an http_4xx log: %s", logBuf.String())
	}
}
