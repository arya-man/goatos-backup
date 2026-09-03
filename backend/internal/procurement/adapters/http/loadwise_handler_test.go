package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type stubLoadwiseService struct{ out domain.LoadwiseSales }

func (s stubLoadwiseService) LoadwiseSales(context.Context, string, string) (domain.LoadwiseSales, error) {
	return s.out, nil
}

func (stubLoadwiseService) SetLoadCost(context.Context, string, string, domain.LoadCostEdit, string) error {
	return nil
}

// The narrow loadwise-weights read exists so the Growth Director's WeighingMonitor grant can
// serve the ADG Analytics Comparison tab WITHOUT opening the purchase and sales money (review
// finding on PR 174). This pins the projection: every priced field the full read carries must be
// absent from the narrow one, and present on the full one so the check cannot pass vacuously.
func TestLoadwiseWeightsCarriesNoMoneyField(t *testing.T) {
	cost, price := 1234.5, 431.0
	value := 5000.0
	svc := stubLoadwiseService{out: domain.LoadwiseSales{
		TotalLoads: 1,
		Loads: []domain.LoadwiseLoad{{
			LoadID: "load-1", LoadRef: "131", VendorName: "Krishnamorrthy", Status: "closed", Farm: "CBE",
			Purchased: 63, Remaining: 63, RemainingSheep: 63,
			AnimalCost: &cost, TransportCost: &cost, OtherCost: &cost, PurchaseValue: &value,
			LandedPricePerKg: &price, SalePricePerKg: &price, RemainingValue: &value, ProfitLoss: &value,
			AvgPurchaseWeightKg: &price,
		}},
	}}
	h := NewLoadwiseHandler(svc)

	serve := func(handler http.HandlerFunc, path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d body %s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Loads []map[string]any `json:"loads"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Loads) != 1 {
			t.Fatalf("%s: decode %v, loads %d", path, err, len(body.Loads))
		}
		return body.Loads[0]
	}
	narrow := serve(h.LoadwiseWeights, "/procurement/loadwise-weights")
	full := serve(h.LoadwiseSales, "/procurement/loadwise-sales")

	money := func(key string) bool {
		k := strings.ToLower(key)
		return strings.Contains(k, "cost") || strings.Contains(k, "value") || strings.Contains(k, "price") || strings.Contains(k, "profit")
	}
	fullMoney := 0
	for key := range full {
		if money(key) {
			fullMoney++
		}
	}
	if fullMoney == 0 {
		t.Fatalf("the full read carries no priced field; the narrow check below would be vacuous: %v", full)
	}
	for key := range narrow {
		if money(key) {
			t.Fatalf("loadwise-weights leaks a priced field %q: %v", key, narrow)
		}
	}
	for _, key := range []string{"load_id", "load_ref", "vendor_name", "purchased", "remaining", "remaining_sheep", "remaining_goats", "avg_purchase_weight_kg"} {
		if _, ok := narrow[key]; !ok {
			t.Fatalf("loadwise-weights must carry %q for the weight comparison: %v", key, narrow)
		}
	}
}
