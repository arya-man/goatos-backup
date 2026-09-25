package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/sales/app"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// oneDealService answers GetDeal from a fixed tenant-keyed set, the way the repository's
// tenant_id predicate does: a deal is only found inside the tenant that owns it.
type oneDealService struct {
	SalesService
	deals map[string]map[string]domain.Deal
}

func (s oneDealService) GetDeal(_ context.Context, tenantID, dealID string) (domain.Deal, error) {
	if d, ok := s.deals[tenantID][dealID]; ok {
		return d, nil
	}
	return domain.Deal{}, ports.ErrDealNotFound
}

func TestGetDealServesOneLedgerRowOrNotFound(t *testing.T) {
	const tenantA = "00000000-0000-4000-8000-000000000001"
	const tenantB = "00000000-0000-4000-8000-000000000002"
	received := 20000.0
	deal := domain.Deal{
		DealID: "5d2c7a8e-0000-4000-8000-000000000001", SaleDate: "2026-09-02", Farm: "CBE",
		BuyerName: "Mahendran", ProductType: "Goat", Breed: "Malai", SalesValue: 197415,
		PaymentReceived: &received, Status: domain.StatusDealClosed,
		Payments: []domain.DealPayment{{PaymentID: "p-1", DealID: "5d2c7a8e-0000-4000-8000-000000000001", ReceivedOn: "2026-09-02", AmountRupees: 20000, Note: "Advance at sale"}},
		Lines:    []domain.DealLine{{LineNo: 1, ProductType: "Goat", Breed: "Malai"}},
	}
	svc := oneDealService{deals: map[string]map[string]domain.Deal{tenantA: {deal.DealID: deal}}}
	mux := http.NewServeMux()
	Register(mux, NewSalesHandler(svc))

	get := func(tenant, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/sales/deals/"+id, nil)
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), tenant))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("found: exactly the ledger row shape", func(t *testing.T) {
		rec := get(tenantA, deal.DealID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
		want, _ := json.Marshal(toDealPayload(deal))
		var gotJSON, wantJSON any
		_ = json.Unmarshal(rec.Body.Bytes(), &gotJSON)
		_ = json.Unmarshal(want, &wantJSON)
		g, _ := json.Marshal(gotJSON)
		w, _ := json.Marshal(wantJSON)
		if string(g) != string(w) {
			t.Fatalf("body is not one ledger row:\n got %s\nwant %s", g, w)
		}
	})

	notFound := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["error"] != "not_found_or_not_allowed" || body["message"] != app.SalesHTTPError(ports.ErrDealNotFound).Message {
			t.Fatalf("not-found body = %v", body)
		}
	}
	t.Run("missing", func(t *testing.T) {
		notFound(t, get(tenantA, "5d2c7a8e-0000-4000-8000-0000000000ff"))
	})
	t.Run("another tenant's deal is not found, never served", func(t *testing.T) {
		notFound(t, get(tenantB, deal.DealID))
	})
}
