package reportinghttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/reporting/app"
	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

func TestAnalyticsTenantScopeMismatchReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(fakeRepo{})))
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

type fakeRepo struct{}

func (fakeRepo) ListIdentityCounts(context.Context, ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	return nil, domain.Freshness{}, nil
}

func (fakeRepo) RebuildIdentityCounters(context.Context, ports.RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error) {
	return nil, nil
}

func (fakeRepo) Ping(context.Context) error {
	return nil
}
