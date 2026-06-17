package authaudit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	testTenantID = "00000000-0000-4000-8000-000000000001"
	testActorID  = "10000000-0000-4000-8000-000000000001"
)

func TestHandlerRecordsSignInWithoutGrantLookup(t *testing.T) {
	emailVerified := true
	recorder := &captureRecorder{}
	handler := RequestWrapped(NewHandler(staticVerifier{claims: platformauth.Claims{
		Subject:         testActorID,
		ExternalSubject: "firebase-uid-1",
		Issuer:          "https://securetoken.google.com/goatos-dev",
		Audience:        "goatos-dev",
		Email:           "ravi@mesha.sg",
		EmailVerified:   &emailVerified,
		Expires:         time.Unix(1_800_000_000, 0).UTC(),
	}}, recorder, nil))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in","source":"admin-web"}`))
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set(httpmiddleware.TenantContextHeader, testTenantID)
	req.Header.Set("X-Mesha-Session-User-Agent", "Chrome")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events=%d want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != ActionSignIn || event.TenantID != testTenantID || event.ActorID != testActorID {
		t.Fatalf("event=%#v", event)
	}
	if event.Metadata["external_subject"] != "firebase-uid-1" || event.Metadata["email"] != "ravi@mesha.sg" || event.Metadata["token_tenant_source"] != "header" {
		t.Fatalf("metadata=%#v", event.Metadata)
	}
	if got, ok := event.Metadata["email_verified"].(bool); !ok || !got {
		t.Fatalf("email_verified metadata=%#v", event.Metadata["email_verified"])
	}
}

func TestHandlerRecordsFailedSignInWhenVerifiedTokenHasNoTenantContext(t *testing.T) {
	recorder := &captureRecorder{}
	handler := RequestWrapped(NewHandler(staticVerifier{claims: platformauth.Claims{
		Subject:         testActorID,
		ExternalSubject: "firebase-uid-1",
		Issuer:          "https://securetoken.google.com/goatos-dev",
		Audience:        "goatos-dev",
		Expires:         time.Unix(1_800_000_000, 0).UTC(),
	}}, recorder, nil))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in","source":"admin-web"}`))
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorTraceID(t, rec)
	if len(recorder.events) != 1 {
		t.Fatalf("events=%d want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != ActionFailedSignIn || event.TenantID != "" || event.ActorID != testActorID {
		t.Fatalf("event=%#v", event)
	}
	if event.Metadata["reason"] != "missing_tenant_context" {
		t.Fatalf("metadata=%#v", event.Metadata)
	}
}

func TestHandlerRejectsMissingBearerWithoutAudit(t *testing.T) {
	recorder := &captureRecorder{}
	handler := RequestWrapped(NewHandler(staticVerifier{claims: platformauth.Claims{Subject: testActorID}}, recorder, nil))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorTraceID(t, rec)
	if len(recorder.events) != 0 {
		t.Fatalf("events=%d want 0", len(recorder.events))
	}
}

func TestHandlerRejectsInvalidActionWithoutAudit(t *testing.T) {
	recorder := &captureRecorder{}
	handler := RequestWrapped(NewHandler(staticVerifier{claims: platformauth.Claims{Subject: testActorID}}, recorder, nil))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.token_dump"}`))
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorTraceID(t, rec)
	if len(recorder.events) != 0 {
		t.Fatalf("events=%d want 0", len(recorder.events))
	}
}

func TestHandlerReturnsWriteFailure(t *testing.T) {
	handler := RequestWrapped(NewHandler(staticVerifier{claims: platformauth.Claims{
		Subject: testActorID,
		Issuer:  "issuer",
		Expires: time.Unix(1_800_000_000, 0).UTC(),
	}}, failingRecorder{}, nil))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.session_refresh"}`))
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set(httpmiddleware.TenantContextHeader, testTenantID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != "auth_audit_write_failed" {
		t.Fatalf("code=%s", body.Code)
	}
}

func RequestWrapped(h *Handler) http.Handler {
	mux := http.NewServeMux()
	Register(mux, h)
	return httpmiddleware.RequestContext(nil)(mux)
}

func assertErrorTraceID(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.TraceID == "" || body.TraceID == "missing-trace" {
		t.Fatalf("trace_id=%q body=%s", body.TraceID, rec.Body.String())
	}
}

type staticVerifier struct {
	claims platformauth.Claims
	err    error
}

func (s staticVerifier) Verify(string) (platformauth.Claims, error) {
	if s.err != nil {
		return platformauth.Claims{}, s.err
	}
	return s.claims, nil
}

type captureRecorder struct {
	events []Event
}

func (r *captureRecorder) Record(_ context.Context, event Event) error {
	r.events = append(r.events, event)
	return nil
}

type failingRecorder struct{}

func (failingRecorder) Record(context.Context, Event) error {
	return errors.New("forced audit failure")
}
