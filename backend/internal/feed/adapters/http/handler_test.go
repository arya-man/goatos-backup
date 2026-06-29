package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeReadinessReader struct {
	gotTenantID string
	readiness   domain.Readiness
	err         error
}

func (f *fakeReadinessReader) Readiness(_ context.Context, tenantID string) (domain.Readiness, error) {
	f.gotTenantID = tenantID
	return f.readiness, f.err
}

func TestGetReadinessReturnsFailClosedContract(t *testing.T) {
	checkedAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	reader := &fakeReadinessReader{readiness: domain.Readiness{
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
	Register(mux, NewHandler(reader))
	req := httptest.NewRequest(http.MethodGet, "/feed-direction/readiness", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reader.gotTenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant forwarded=%q", reader.gotTenantID)
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
	Register(mux, NewHandler(&fakeReadinessReader{err: errors.New("boom")}))
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
