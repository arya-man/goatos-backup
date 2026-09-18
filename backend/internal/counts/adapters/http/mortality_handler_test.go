package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type mortalityHandlerService struct {
	milkPreparationHandlerService
	seen domain.MortalityQuery
	out  domain.Mortality
}

func (f *mortalityHandlerService) GetMortality(_ context.Context, req domain.MortalityQuery) (domain.Mortality, error) {
	f.seen = req
	return f.out, nil
}

// Park scope and the named window must reach the reader unchanged, and the payload must
// come back as the domain shape the client decodes.
func TestGetMortalityPassesParkAndWindowThrough(t *testing.T) {
	rate := 7.5
	service := &mortalityHandlerService{out: domain.Mortality{
		WindowFrom: "2026-03-04", WindowTo: "2026-09-17",
		Totals: domain.MortalityTotals{Deaths: 3, Animals: 40, RatePct: &rate, KidDeaths: 2, AdultDeaths: 1},
	}}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/mortality?park_id=20000000-0000-4000-8000-000000000001&from=2026-03-04&to=2026-09-17", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetMortality(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.seen.TenantID != "10000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant=%q", service.seen.TenantID)
	}
	if service.seen.ParkID == nil || *service.seen.ParkID != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("park=%+v", service.seen.ParkID)
	}
	if service.seen.FromDate != "2026-03-04" || service.seen.ToDate != "2026-09-17" {
		t.Fatalf("window=%s..%s", service.seen.FromDate, service.seen.ToDate)
	}
	var body domain.Mortality
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Totals.KidDeaths+body.Totals.AdultDeaths != body.Totals.Deaths {
		t.Fatalf("kids+adults must partition deaths: %+v", body.Totals)
	}
	if body.Totals.RatePct == nil || *body.Totals.RatePct != 7.5 {
		t.Fatalf("rate must ride the wire: %+v", body.Totals.RatePct)
	}
}

// The window rules are Herd Analytics' rules: one bound alone, a reversed pair and an
// over-wide span are 400s, never rewritten.
func TestGetMortalityRejectsInvalidWindows(t *testing.T) {
	service := &mortalityHandlerService{}
	handler := NewHandler(service, slog.Default())
	for _, query := range []string{"from=2026-01-01", "from=2026-09-01&to=2026-08-01", "from=2020-01-01&to=2026-09-01", "from=nope&to=2026-09-01"} {
		req := httptest.NewRequest(http.MethodGet, "/counts/mortality?"+query, nil)
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
		recorder := httptest.NewRecorder()
		handler.GetMortality(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d want 400", query, recorder.Code)
		}
	}
}
