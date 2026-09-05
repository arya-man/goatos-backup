package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const analyticsTestTenant = "10000000-0000-4000-8000-000000000001"

type stubAnalyticsService struct {
	seen   domain.HealthAnalyticsQuery
	called bool
	out    domain.HealthAnalytics
	err    error
}

func (s *stubAnalyticsService) GetHealthAnalytics(_ context.Context, req domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error) {
	s.seen = req
	s.called = true
	return s.out, s.err
}

func analyticsRequest(t *testing.T, query string, withTenant bool) (*httptest.ResponseRecorder, *stubAnalyticsService) {
	t.Helper()
	svc := &stubAnalyticsService{out: domain.HealthAnalytics{WindowFrom: "2026-08-01", WindowTo: "2026-09-05"}}
	mux := http.NewServeMux()
	RegisterAnalytics(mux, NewAnalyticsHandler(svc, nil))

	req := httptest.NewRequest(http.MethodGet, "/health/analytics"+query, nil)
	if withTenant {
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), analyticsTestTenant))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec, svc
}

func TestAnalyticsHandlerServesTheWholePageInOneResponse(t *testing.T) {
	rec, svc := analyticsRequest(t, "?from=2026-08-01&to=2026-09-05", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.seen.TenantID != analyticsTestTenant {
		t.Fatalf("tenant = %q, want the context tenant", svc.seen.TenantID)
	}
	if svc.seen.FromDate != "2026-08-01" || svc.seen.ToDate != "2026-09-05" {
		t.Fatalf("window = %q..%q, want it carried through verbatim", svc.seen.FromDate, svc.seen.ToDate)
	}

	var body domain.HealthAnalytics
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.WindowFrom != "2026-08-01" {
		t.Fatalf("window_from = %q, want the served window echoed back", body.WindowFrom)
	}
}

// A present-but-invalid window is a 400, never a silent fallback to the default.
// The read must not answer a question the caller did not ask.
func TestAnalyticsHandlerRejectsAnInvalidWindowWithoutReading(t *testing.T) {
	for _, query := range []string{
		"?from=2026-08-01",
		"?to=2026-08-01",
		"?from=01-08-2026&to=2026-08-31",
		"?from=2026-08-31&to=2026-08-01",
	} {
		t.Run(query, func(t *testing.T) {
			rec, svc := analyticsRequest(t, query, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q", rec.Code, query)
			}
			if svc.called {
				t.Fatal("the service was read with a window the handler should have refused")
			}
		})
	}
}

func TestAnalyticsHandlerRequiresTenantContext(t *testing.T) {
	rec, svc := analyticsRequest(t, "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if svc.called {
		t.Fatal("the service was read without a tenant")
	}
}

// An absent window is the ONE case that is legal to substitute, because the
// caller named nothing to contradict.
func TestAnalyticsHandlerDefaultsAnAbsentWindow(t *testing.T) {
	rec, svc := analyticsRequest(t, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.seen.FromDate == "" || svc.seen.ToDate == "" {
		t.Fatalf("window = %q..%q, want the default resolved before the read", svc.seen.FromDate, svc.seen.ToDate)
	}
}

// The park is an OPTIONAL scope carried from the top bar. An absent one must
// reach the service as nil (every park), not as an empty-string park id that
// matches no row.
func TestAnalyticsHandlerLeavesAnAbsentParkNil(t *testing.T) {
	_, svc := analyticsRequest(t, "?park_id=", true)
	if svc.seen.ParkID != nil {
		t.Fatalf("park = %q, want nil", *svc.seen.ParkID)
	}

	_, scoped := analyticsRequest(t, "?park_id=22222222-2222-4222-8222-222222222222", true)
	if scoped.seen.ParkID == nil || *scoped.seen.ParkID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("park = %v, want the requested park", scoped.seen.ParkID)
	}
}
