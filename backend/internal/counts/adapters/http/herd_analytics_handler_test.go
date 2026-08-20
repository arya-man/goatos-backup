package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type herdAnalyticsHandlerService struct {
	milkPreparationHandlerService
	seen domain.HerdAnalyticsQuery
	out  domain.HerdAnalytics
}

func (f *herdAnalyticsHandlerService) GetHerdAnalytics(_ context.Context, req domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error) {
	f.seen = req
	return f.out, nil
}

// The window length a leader picked must reach the reader unchanged, and the park
// scope must ride along -- a scope silently dropped here would show one farm's
// leadership the whole estate under that farm's own label.
func TestGetHerdAnalyticsPassesParkAndMonthsThrough(t *testing.T) {
	service := &herdAnalyticsHandlerService{out: domain.HerdAnalytics{
		WindowFrom: "2025-09-01", WindowTo: "2026-08-20",
		Totals: domain.HerdAnalyticsTotals{LiveAnimals: 324, Kids: 100, Adults: 224},
	}}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/herd-analytics?park_id=20000000-0000-4000-8000-000000000001&from=2025-03-04&to=2026-02-17", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetHerdAnalytics(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.seen.TenantID != "10000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant=%q", service.seen.TenantID)
	}
	if service.seen.ParkID == nil || *service.seen.ParkID != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("park=%+v", service.seen.ParkID)
	}
	if service.seen.FromDate != "2025-03-04" || service.seen.ToDate != "2026-02-17" {
		t.Fatalf("window=%s..%s, want 2025-03-04..2026-02-17", service.seen.FromDate, service.seen.ToDate)
	}
	var body domain.HerdAnalytics
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Totals.Kids+body.Totals.Adults != body.Totals.LiveAnimals {
		t.Fatalf("kids+adults must partition live: %+v", body.Totals)
	}
}

// A present-but-invalid window is REJECTED, never quietly rewritten to the default:
// a leader who named a window must be shown that window or an error, never a
// different window under the label they chose. Half a window counts as invalid —
// filling in the missing bound would answer a question nobody asked.
func TestGetHerdAnalyticsRejectsAnOutOfRangeWindow(t *testing.T) {
	service := &herdAnalyticsHandlerService{}
	handler := NewHandler(service, slog.Default())
	for _, raw := range []string{
		"from=2026-03-01",
		"to=2026-08-20",
		"from=March&to=2026-08-20",
		"from=2026-13-01&to=2026-08-20",
		"from=2026-03&to=2026-08-20",
		"from=2026-08-20&to=2026-03-01",
		"from=2019-01-01&to=2026-08-20",
	} {
		req := httptest.NewRequest(http.MethodGet, "/counts/herd-analytics?"+raw, nil)
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
		recorder := httptest.NewRecorder()

		handler.GetHerdAnalytics(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400 body=%s", raw, recorder.Code, recorder.Body.String())
		}
	}
}

// Absent means "use the declared default", which is the one case that IS defaulted.
func TestGetHerdAnalyticsDefaultsTheWindowWhenAbsent(t *testing.T) {
	service := &herdAnalyticsHandlerService{}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/herd-analytics", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetHerdAnalytics(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	wantFrom, wantTo := domain.HerdAnalyticsDefaultWindow(time.Now())
	if service.seen.FromDate != wantFrom || service.seen.ToDate != wantTo {
		t.Fatalf("window=%s..%s, want the default %s..%s", service.seen.FromDate, service.seen.ToDate, wantFrom, wantTo)
	}
	if service.seen.ParkID != nil {
		t.Fatalf("park should be nil when unscoped, got %+v", service.seen.ParkID)
	}
}
