package identityhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestGetGoatPassportContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-contract")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["goat"] == nil || body["warnings"] == nil || body["trace_id"] != "req-contract" {
		t.Fatalf("unexpected contract shape: %#v", body)
	}
}

func TestMissingTenantReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-Request-ID", "req-missing-tenant")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_tenant_scope" || envelope.TraceID != "req-missing-tenant" || envelope.FieldErrors == nil {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestNotImplementedUsesErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001/timeline?limit=10", nil)
	req.Header.Set("X-Request-ID", "req-deferred")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "goat_timeline_deferred" || envelope.TraceID != "req-deferred" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAnalyticsTenantScopeMismatchReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000002", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-tenant-mismatch")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "tenant_scope_mismatch" || envelope.TraceID != "req-tenant-mismatch" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

type handlerRepo struct{}

func (handlerRepo) GetGoatByID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) GetGoatByDisplayID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) SearchGoats(context.Context, ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	return []domain.GoatSummary{handlerPassport().Summary}, nil, nil
}

func (handlerRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return nil, nil
}

func (handlerRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	return nil, nil
}

func (handlerRepo) ListConflicts(context.Context, ports.ListConflictsParams) ([]domain.ConflictSummary, *string, error) {
	return nil, nil, nil
}

func (handlerRepo) GetConflict(context.Context, string, string) (*domain.ConflictDetailResult, error) {
	return nil, ports.ErrNotFound
}

func (handlerRepo) ListIdentityCounts(context.Context, ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	return nil, domain.Freshness{}, nil
}

func (handlerRepo) Ping(context.Context) error { return nil }

func handlerPassport() *domain.GoatPassport {
	return &domain.GoatPassport{
		GoatID:        "10000000-0000-4000-8000-000000000001",
		DisplayID:     "G-000001",
		Species:       "goat",
		IdentityState: "clean",
		Summary: domain.GoatSummary{
			GoatID:          "10000000-0000-4000-8000-000000000001",
			DisplayID:       "G-000001",
			LifecycleStatus: "alive",
			IdentityState:   "clean",
			LocationPath:    domain.LocationPath{Display: "Synthetic CBE"},
			Warnings:        []domain.Warning{},
		},
		Identifiers:  []domain.GoatIdentifier{},
		EvidenceRefs: []domain.EvidenceRef{},
		FamilyRefs:   []domain.FamilyRef{},
		RowVersion:   1,
	}
}
