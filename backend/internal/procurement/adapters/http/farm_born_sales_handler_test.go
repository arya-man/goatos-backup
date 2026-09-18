package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type stubFarmBornService struct {
	req app.FarmBornRequest
	out domain.FarmBornSales
	err error
}

func (s *stubFarmBornService) FarmBornSales(_ context.Context, _ string, req app.FarmBornRequest) (domain.FarmBornSales, error) {
	s.req = req
	if s.err != nil {
		return domain.FarmBornSales{}, s.err
	}
	return s.out, nil
}

func serveFarmBorn(t *testing.T, h *FarmBornSalesHandler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	h.FarmBornSales(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: decode %v: %s", path, err, rec.Body.String())
	}
	return rec.Code, body
}

// TestFarmBornHandlerForwardsEveryFilterAndSerialisesThePage pins the wire contract: every query
// parameter reaches the service by name, the payload carries the summary, the four breakdowns,
// the ledger page and the option vocabularies, an unpriced sale omits its value, and the pen is
// the composed display string (never a raw partition label).
func TestFarmBornHandlerForwardsEveryFilterAndSerialisesThePage(t *testing.T) {
	value := 9000.0
	stub := &stubFarmBornService{out: domain.FarmBornSales{
		Summary:   domain.FarmBornSummary{OnFarm: 3, Sold: 2, SoldPriced: 1, Revenue: 9000, AvgPrice: 9000, From: "2026-08-18", To: "2026-09-18"},
		ByBreed:   []domain.FarmBornBucket{{Key: "goat:sirohi", Label: "Sirohi", Detail: "Goat", OnFarm: 3, Sold: 2, SoldPriced: 1, Revenue: 9000}},
		BySex:     []domain.FarmBornBucket{{Key: "male", Label: "Male", OnFarm: 3, Sold: 2}},
		ByStage:   []domain.FarmBornBucket{{Key: "f2-male", Label: "F2-Male", OnFarm: 3, Sold: 2}},
		ByPen:     []domain.FarmBornBucket{{Key: "s1|2", Label: "Castro 2", Detail: "Coimbatore", ParkID: "p1", OnFarm: 3, Sold: 2}},
		Sold:      []domain.FarmBornSoldRow{{GoatID: "g1", Tag: "TAG1", Breed: "Sirohi", Sex: "male", Stage: "F2-Male", ParkName: "Coimbatore", PenDisplay: "Castro 2", SaleDate: "2026-09-10", SaleValue: &value, BuyerName: "Mahendran", DealID: "d1"}, {GoatID: "g2", Tag: "TAG2", PenDisplay: "Castro 2", SaleDate: "2026-09-01"}},
		TotalSold: 2, Limit: 25, Offset: 0,
		Options: domain.FarmBornOptions{Parks: []domain.FarmBornOption{{Key: "p1", Label: "Coimbatore"}}, Pens: []domain.FarmBornOption{{Key: "s1|2", Label: "Castro 2", ParkID: "p1"}}, Breeds: []domain.FarmBornOption{{Key: "sirohi", Label: "Sirohi"}}},
	}}
	h := NewFarmBornSalesHandler(stub)
	code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?from=2026-08-18&to=2026-09-18&park_id=p1&pen=s1%7C2&species=goat&breed=Sirohi&sex=male&stage=F2-Male&limit=50&offset=25")
	if code != http.StatusOK {
		t.Fatalf("status = %d body %v", code, body)
	}
	want := app.FarmBornRequest{From: "2026-08-18", To: "2026-09-18", ParkID: "p1", Pen: "s1|2", Species: "goat", Breed: "Sirohi", Sex: "male", Stage: "F2-Male", Limit: 50, Offset: 25}
	if stub.req != want {
		t.Fatalf("service request = %+v, want %+v", stub.req, want)
	}
	summary := body["summary"].(map[string]any)
	if summary["on_farm"].(float64) != 3 || summary["sold"].(float64) != 2 || summary["revenue"].(float64) != 9000 || summary["from"] != "2026-08-18" {
		t.Fatalf("summary = %v", summary)
	}
	for _, key := range []string{"by_breed", "by_sex", "by_stage", "by_pen"} {
		if rows, ok := body[key].([]any); !ok || len(rows) != 1 {
			t.Fatalf("%s = %v", key, body[key])
		}
	}
	pen := body["by_pen"].([]any)[0].(map[string]any)
	if pen["label"] != "Castro 2" || pen["detail"] != "Coimbatore" || pen["park_id"] != "p1" || pen["key"] != "s1|2" {
		t.Fatalf("by_pen row = %v", pen)
	}
	sold := body["sold"].([]any)
	first := sold[0].(map[string]any)
	if first["tag"] != "TAG1" || first["pen"] != "Castro 2" || first["sale_value"].(float64) != 9000 || first["buyer_name"] != "Mahendran" {
		t.Fatalf("sold[0] = %v", first)
	}
	if _, present := sold[1].(map[string]any)["sale_value"]; present {
		t.Fatalf("an unpriced sale must omit sale_value: %v", sold[1])
	}
	if body["total_sold"].(float64) != 2 {
		t.Fatalf("total_sold = %v", body["total_sold"])
	}
	options := body["options"].(map[string]any)
	if options["pens"].([]any)[0].(map[string]any)["park_id"] != "p1" {
		t.Fatalf("pen option must carry its park: %v", options["pens"])
	}
	for _, key := range []string{"parks", "pens", "species", "breeds", "sexes", "stages"} {
		if _, ok := options[key].([]any); !ok {
			t.Fatalf("options.%s must be an array, never null: %v", key, options[key])
		}
	}
}

// TestFarmBornHandlerRefusesBadInput pins that a bad page size or page is refused at the
// transport and that a service refusal maps to its own code.
func TestFarmBornHandlerRefusesBadInput(t *testing.T) {
	h := NewFarmBornSalesHandler(&stubFarmBornService{})
	if code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?limit=x"); code != http.StatusBadRequest || body["error"] != "invalid_limit" {
		t.Fatalf("limit=x -> %d %v", code, body)
	}
	if code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?offset=x"); code != http.StatusBadRequest || body["error"] != "invalid_offset" {
		t.Fatalf("offset=x -> %d %v", code, body)
	}
	h = NewFarmBornSalesHandler(&stubFarmBornService{err: app.ErrFarmBornWindowInvalid})
	if code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?from=2026-09-18&to=2026-08-01"); code != http.StatusBadRequest || body["error"] != "invalid_window" {
		t.Fatalf("inverted window -> %d %v", code, body)
	}
}
