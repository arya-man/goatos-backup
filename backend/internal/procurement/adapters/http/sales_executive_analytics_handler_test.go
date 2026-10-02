package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type stubSalesExecutiveService struct {
	days int
	page domain.SalesExecutivePage
	err  error
}

func (s *stubSalesExecutiveService) SalesExecutiveAnalytics(_ context.Context, _ string, days int, page domain.SalesExecutivePage) (domain.SalesExecutiveAnalytics, error) {
	s.days, s.page = days, page
	if s.err != nil {
		return domain.SalesExecutiveAnalytics{}, s.err
	}
	at := time.Date(2026, 10, 2, 5, 30, 0, 0, time.UTC)
	return domain.SalesExecutiveAnalytics{
		Days: 7, PeriodFrom: "2026-09-26", PeriodTo: "2026-10-02",
		Current: domain.SalesExecutiveCounts{VendorsAdded: 2, MarketCalls: 3, LeadCalls: 1, SalesRecorded: 1, SalesValue: 1000},
		Daily:   []domain.SalesExecutiveDay{{Date: "2026-10-02", VendorsAdded: 2, Calls: 4, Sales: 1}},
		People: []domain.SalesExecutivePerson{{ActorID: "a1", Name: "Hemant", ActiveDays: 1, LastActiveAt: at, LastActivityKind: domain.ActivitySaleRecorded,
			Counts: domain.SalesExecutiveCounts{VendorsAdded: 2, MarketCalls: 3, LeadCalls: 1, SalesRecorded: 1}}},
		Recent: []domain.SalesActivityFact{{Kind: domain.ActivitySaleRecorded, ActorName: "Hemant", At: at, BusinessDate: "2026-10-02", Subject: "Mahendran", Amount: 1000}},
	}, nil
}

func serveSalesExecutive(t *testing.T, svc *stubSalesExecutiveService, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	NewSalesExecutiveAnalyticsHandler(svc).SalesExecutiveAnalytics(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return rec.Code, body
}

func TestSalesExecutiveAnalyticsHandlerServesTheWholePage(t *testing.T) {
	svc := &stubSalesExecutiveService{}
	code, body := serveSalesExecutive(t, svc, "/procurement/sales-executive-analytics?days=7&activity_offset=20&vendor_offset=40")
	if code != http.StatusOK || svc.days != 7 || svc.page.ActivityOffset != 20 || svc.page.VendorOffset != 40 {
		t.Fatalf("code=%d days=%d page=%+v", code, svc.days, svc.page)
	}
	current := body["current"].(map[string]any)
	if current["calls"].(float64) != 4 || current["total"].(float64) != 7 {
		t.Fatalf("calls/total must be composed from the buckets: %v", current)
	}
	for _, key := range []string{"daily", "people", "recent", "latest_vendors", "days_options"} {
		if _, ok := body[key].([]any); !ok {
			t.Fatalf("%s must be an array (never null): %v", key, body[key])
		}
	}
	recent := body["recent"].([]any)[0].(map[string]any)
	if recent["at"] != "2026-10-02T05:30:00Z" || recent["kind"] != "sale_recorded" {
		t.Fatalf("recent row = %v", recent)
	}
}

func TestSalesExecutiveAnalyticsHandlerRefusesABadPeriod(t *testing.T) {
	code, body := serveSalesExecutive(t, &stubSalesExecutiveService{}, "/procurement/sales-executive-analytics?days=abc")
	if code != http.StatusBadRequest || body["error"] != "invalid_days" {
		t.Fatalf("code=%d body=%v", code, body)
	}
	code, body = serveSalesExecutive(t, &stubSalesExecutiveService{}, "/procurement/sales-executive-analytics?activity_offset=x")
	if code != http.StatusBadRequest || body["error"] != "invalid_offset" {
		t.Fatalf("code=%d body=%v", code, body)
	}
	code, body = serveSalesExecutive(t, &stubSalesExecutiveService{err: app.ErrSalesExecutiveOffsetInvalid}, "/procurement/sales-executive-analytics?vendor_offset=-1")
	if code != http.StatusBadRequest || body["error"] != "invalid_offset" {
		t.Fatalf("code=%d body=%v", code, body)
	}
	code, body = serveSalesExecutive(t, &stubSalesExecutiveService{err: app.ErrSalesExecutiveDaysInvalid}, "/procurement/sales-executive-analytics?days=31")
	if code != http.StatusBadRequest || body["error"] != "invalid_days" {
		t.Fatalf("code=%d body=%v", code, body)
	}
}
