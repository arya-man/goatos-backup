package reportinghttp

import (
	"context"
	"encoding/base64"
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

func TestAnalyticsInvalidFilterReturnsBadRequestEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(fakeRepo{listErr: ports.ErrInvalidFilter})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001&limit=50&park_id=not-a-uuid", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-invalid-filter")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_filter" || envelope.TraceID != "req-invalid-filter" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAnalyticsUsesRequestTenantAsRepositoryAuthority(t *testing.T) {
	var got ports.CountParams
	handler := analyticsHandler(fakeRepo{capture: &got})
	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&limit=50", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-authority")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("repo tenant = %q", got.TenantID)
	}
}

func TestAnalyticsCountsLimitValidation(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantCode string
	}{
		{
			name:     "missing limit",
			query:    "grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001",
			wantCode: "missing_limit",
		},
		{
			name:     "invalid limit",
			query:    "grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001&limit=abc",
			wantCode: "invalid_limit",
		},
		{
			name:     "over max limit",
			query:    "grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001&limit=501",
			wantCode: "invalid_limit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := analyticsHandler(fakeRepo{})
			req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?"+tc.query, nil)
			req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
			req.Header.Set("X-Request-ID", "req-"+tc.wantCode)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var envelope domain.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if envelope.Code != tc.wantCode {
				t.Fatalf("code=%s want %s", envelope.Code, tc.wantCode)
			}
		})
	}
}

func TestAnalyticsCountsInvalidCursorReturnsBadRequestEnvelope(t *testing.T) {
	handler := analyticsHandler(fakeRepo{listErr: ports.ErrInvalidCursor})
	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001&limit=50&cursor=bad", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-invalid-cursor")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_cursor" || envelope.TraceID != "req-invalid-cursor" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAnalyticsCountsStructurallyValidCursorReturnsCleanly(t *testing.T) {
	cursor := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"count_value":999,"counter_id":"90000000-0000-4000-8000-000000000999"}`))
	handler := analyticsHandler(fakeRepo{})
	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000001&limit=50&cursor="+cursor, nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-valid-cursor")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var result domain.IdentityCountsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if result.HasMore || result.NextCursor != nil {
		t.Fatalf("empty fake page should not have next page: %#v", result)
	}
}

func analyticsHandler(repo fakeRepo) http.Handler {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	return httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)
}

type fakeRepo struct {
	listErr error
	capture *ports.CountParams
}

func (f fakeRepo) ListIdentityCounts(_ context.Context, params ports.CountParams) (*ports.CountPage, error) {
	if f.capture != nil {
		*f.capture = params
	}
	return &ports.CountPage{Items: []domain.IdentityCount{}}, f.listErr
}

func (fakeRepo) RebuildIdentityCounters(context.Context, ports.RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error) {
	return nil, nil
}

func (fakeRepo) UpdateIdentityCounters(context.Context, ports.UpdateIdentityCountersParams) (*domain.IncrementalCounterUpdateResult, error) {
	return nil, nil
}

func (fakeRepo) Ping(context.Context) error {
	return nil
}
