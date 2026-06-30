package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeService struct {
	gotTenantID string
	readiness   domain.Readiness
	err         error

	gotCommand domain.CountsProjectionExceptionResolutionCommand
	resolution domain.CountsProjectionExceptionResolution
	resolveErr error
}

func (f *fakeService) Readiness(_ context.Context, tenantID string) (domain.Readiness, error) {
	f.gotTenantID = tenantID
	return f.readiness, f.err
}

func (f *fakeService) ResolveCountsProjectionException(_ context.Context, in domain.CountsProjectionExceptionResolutionCommand) (domain.CountsProjectionExceptionResolution, error) {
	f.gotCommand = in
	return f.resolution, f.resolveErr
}

func TestGetReadinessReturnsFailClosedContract(t *testing.T) {
	checkedAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	service := &fakeService{readiness: domain.Readiness{
		TenantID:          "00000000-0000-4000-8000-000000000001",
		Status:            domain.ReadinessBlocked,
		CurrentGate:       "G2",
		GenerationAllowed: false,
		CountsShiftingSubgates: []domain.CountsShiftingSubgate{{
			ID: "CSG1", Status: domain.ReadinessBlocked, LastCheckedAt: checkedAt,
		}},
		SafetyInvariants: []domain.SafetyInvariant{{
			Key: "shifted_pregnant_destination_recompute", Status: domain.ReadinessBlocked,
			BlockerReason: "destination shed recompute required", LastCheckedAt: checkedAt,
		}},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service))
	req := httptest.NewRequest(http.MethodGet, "/feed-direction/readiness", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if service.gotTenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant forwarded=%q", service.gotTenantID)
	}
	var got domain.Readiness
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.GenerationAllowed {
		t.Fatal("readiness response unexpectedly allows generation")
	}
	if got.CurrentGate != "G2" {
		t.Fatalf("current_gate=%q, want G2", got.CurrentGate)
	}
	if len(got.SafetyInvariants) != 1 || got.SafetyInvariants[0].Key != "shifted_pregnant_destination_recompute" {
		t.Fatalf("safety invariants = %+v", got.SafetyInvariants)
	}
}

func TestGetReadinessSurfacesInternalError(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeService{err: errors.New("boom")}))
	req := httptest.NewRequest(http.MethodGet, "/feed-direction/readiness", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != "internal_error" {
		t.Fatalf("error code=%q", env.Code)
	}
}

func TestResolveCountsProjectionExceptionForwardsActionEnvelope(t *testing.T) {
	service := &fakeService{resolution: domain.CountsProjectionExceptionResolution{
		ResolutionID:          "resolution-1",
		ProjectionExceptionID: "77000000-0000-4000-8000-000000000001",
		Action:                "resolve",
		Status:                "resolved",
		WorkState:             "resolved",
		ResolvedByRef:         "90000000-0000-4000-8000-000000000001",
		ResolutionReason:      "reviewed pregnant destination ration context",
		ResolvedAt:            time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service))
	body := []byte(`{"resolution_reason":"reviewed pregnant destination ration context","resolution_ref":"shift-report:123"}`)
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/counts-projection/exceptions/77000000-0000-4000-8000-000000000001/resolve", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "resolve-counts-exception-1")
	ctx := httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithActorID(ctx, "90000000-0000-4000-8000-000000000001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if service.gotCommand.TenantID != "00000000-0000-4000-8000-000000000001" ||
		service.gotCommand.ProjectionExceptionID != "77000000-0000-4000-8000-000000000001" ||
		service.gotCommand.Action != "resolve" ||
		service.gotCommand.ActorID != "90000000-0000-4000-8000-000000000001" ||
		service.gotCommand.IdempotencyKey != "resolve-counts-exception-1" ||
		service.gotCommand.ResolutionRef == nil ||
		*service.gotCommand.ResolutionRef != "shift-report:123" {
		t.Fatalf("forwarded command=%+v", service.gotCommand)
	}
	var got countsProjectionExceptionActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Resolution.Status != "resolved" || got.TraceID == "" {
		t.Fatalf("response=%+v", got)
	}
}

func TestDismissCountsProjectionExceptionMapsIdempotencyConflict(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeService{resolveErr: feedapp.ErrIdempotencyConflict}))
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/counts-projection/exceptions/77000000-0000-4000-8000-000000000001/dismiss", bytes.NewReader([]byte(`{"resolution_reason":"duplicate stale exception"}`)))
	req.Header.Set("Idempotency-Key", "dismiss-counts-exception-1")
	ctx := httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithActorID(ctx, "90000000-0000-4000-8000-000000000001")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != "idempotency_conflict" {
		t.Fatalf("error code=%q", env.Code)
	}
}

func TestResolveCountsProjectionExceptionRejectsInvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeService{}))
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/counts-projection/exceptions/77000000-0000-4000-8000-000000000001/resolve", bytes.NewReader([]byte(`{`)))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
