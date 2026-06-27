package identityhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// errRepo returns a configurable error from every repo method to trigger 5xx.
type errRepo struct {
	err error
}

func (r errRepo) GetGoatByID(context.Context, string, string) (*domain.GoatPassport, error) {
	return nil, r.err
}
func (r errRepo) GetGoatByDisplayID(context.Context, string, string) (*domain.GoatPassport, error) {
	return nil, r.err
}
func (r errRepo) SearchGoats(context.Context, ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	return nil, nil, r.err
}
func (r errRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return nil, r.err
}
func (r errRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	return nil, r.err
}
func (r errRepo) GetGoatTimeline(context.Context, ports.GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error) {
	return nil, nil, r.err
}
func (r errRepo) AddGoatIdentifier(context.Context, ports.AddGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	return nil, r.err
}
func (r errRepo) RetireGoatIdentifier(context.Context, ports.RetireGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	return nil, r.err
}
func (r errRepo) MoveGoat(context.Context, ports.MoveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	return nil, r.err
}
func (r errRepo) ExitGoat(context.Context, ports.ExitGoatCommand) (*ports.AdminGoatMutationResult, error) {
	return nil, r.err
}
func (r errRepo) StageGoat(context.Context, ports.StageGoatCommand) (*ports.AdminGoatMutationResult, error) {
	return nil, r.err
}
func (r errRepo) Ping(context.Context) error { return r.err }

// newFakeService constructs an app.Service backed by a fake repo.
// Since we're in the same package we rely on app.NewService being exported.
func newFakeService(repo ports.Repository) *app.Service {
	return app.NewService(repo)
}

// TestIdentity5xxLogsServerSideWithTraceID verifies that the handler logs an
// http_5xx line server-side when an internal error occurs, and that the log
// entry includes the request ID for correlation.
func TestIdentity5xxLogsServerSideWithTraceID(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	mux := http.NewServeMux()
	Register(mux, NewHandler(newFakeService(errRepo{err: errors.New("simulated internal error")}), log))
	// PanicRecovery outermost, then RequestContext
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(mux))

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-5xx-test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}

	logOut := logBuf.String()
	if !strings.Contains(logOut, "http_5xx") {
		t.Errorf("expected http_5xx log line, got: %s", logOut)
	}
	// The log entry must carry the request ID for correlation.
	if !strings.Contains(logOut, "req-5xx-test") {
		t.Errorf("expected req-5xx-test in log output, got: %s", logOut)
	}
	// The app layer wraps the repo error; check for the wrapped prefix.
	if !strings.Contains(logOut, "identity repository error") {
		t.Errorf("expected wrapped error text in log output, got: %s", logOut)
	}
}

// TestIdentity4xxDoesNotLogServerSide verifies that 4xx client errors do not
// produce an http_5xx server log entry.
func TestIdentity4xxDoesNotLogServerSide(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	mux := http.NewServeMux()
	Register(mux, NewHandler(newFakeService(&handlerRepo{}), log))
	handler := httpmiddleware.RequestContext(log)(mux)

	// Missing limit on SearchGoats produces a 400.
	req := httptest.NewRequest(http.MethodGet, "/goats/search", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-4xx-test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logBuf.String(), "http_5xx") {
		t.Errorf("unexpected http_5xx log on 4xx response: %s", logBuf.String())
	}
}

// TestIdentityHandlerDoesNotEchoRawRequestBody verifies that the HTTP boundary
// log lines do not reproduce the raw JSON request body verbatim.  The boundary
// logger records err, trace_id, route, and status — structured fields only, not
// a body echo.
//
// NOTE on data-classification: goat identifiers (RFID, old tag, breed, farm)
// are business data, NOT PII.  They SHOULD appear in log output when they come
// from a service error message or from structured context fields attached to the
// logger.  This test only asserts that the handler does not echo the raw body
// bytes; it does not assert that identifiers are hidden from logs.
func TestIdentityHandlerDoesNotEchoRawRequestBody(t *testing.T) {
	rawBodySentinel := "RAW-BODY-SENTINEL-SHOULD-NOT-BE-ECHOED-XYZ"

	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	mux := http.NewServeMux()
	Register(mux, NewHandler(newFakeService(errRepo{err: errors.New("db error")}), log))
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(mux))

	// Body contains a sentinel value that would only appear in logs if the
	// handler echoes the raw request body — which it must not do.
	body := `{"identifier_type":"rfid","identifier_value":"` + rawBodySentinel + `","scope_key":"global:rfid","evidence_refs":[],"row_version":1}`
	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "00000000-0000-4000-8000-000000000099")
	req.Header.Set("X-Request-ID", "req-body-echo-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOut := logBuf.String()
	if strings.Contains(logOut, rawBodySentinel) {
		t.Errorf("HTTP boundary log must not echo raw request body; found sentinel %q in: %s", rawBodySentinel, logOut)
	}
}

// TestIdentity5xxLogsServerSideEnvelope verifies that a 5xx response carries
// a valid JSON envelope with a non-empty trace_id field.
func TestIdentity5xxLogsServerSideEnvelope(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	mux := http.NewServeMux()
	h := &Handler{
		service: newFakeService(errRepo{err: errors.New("internal db error")}),
		log:     log,
	}
	Register(mux, h)
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(mux))

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-5xx-envelope")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if envelope["trace_id"] == "" || envelope["trace_id"] == nil {
		t.Errorf("expected trace_id in 500 envelope, got: %v", envelope)
	}
	if !strings.Contains(logBuf.String(), "http_5xx") {
		t.Errorf("expected http_5xx in server log, got: %s", logBuf.String())
	}
}

// TestHandlerAcceptsNilLogger verifies that NewHandler does not panic when no
// logger is passed and uses slog.Default() as the fallback.
func TestHandlerAcceptsNilLogger(t *testing.T) {
	h := NewHandler(newFakeService(&handlerRepo{}))
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.log == nil {
		t.Fatal("expected non-nil log on handler when logger arg omitted")
	}
}

// TestHandlerAcceptsExplicitLogger verifies the variadic injection path.
func TestHandlerAcceptsExplicitLogger(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(newFakeService(&handlerRepo{}), log)
	if h.log != log {
		t.Fatal("expected handler to use the supplied logger")
	}
}
