package http

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// "Sort all rows" (maintainer request 2026-09-25): Buyer analytics and Farm born sorted only the
// 25 rows the browser held, so "revenue, highest first" showed the biggest buyer ON THAT PAGE.
// These drive the real handler -> service -> domain path with a fake repository holding 30 rows
// whose biggest value sits on PAGE 2 of the default (newest-first) order, and assert the header
// sort reaches the whole set: page 1's top row is the biggest of ALL rows, and page 2 continues.

type sortBuyerRepo struct{ facts []domain.BuyerDealFact }

func (r sortBuyerRepo) ClosedBuyerDeals(context.Context, string, string) ([]domain.BuyerDealFact, error) {
	return r.facts, nil
}

func thirtyBuyers() []domain.BuyerDealFact {
	facts := []domain.BuyerDealFact{}
	for i := 0; i < 30; i++ {
		facts = append(facts, domain.BuyerDealFact{
			DealID: fmt.Sprintf("d%02d", i),
			// Buyer 0 has the OLDEST sale, so the default newest-first order puts it last.
			SaleDate:  fmt.Sprintf("2026-08-%02d", i+1),
			BuyerKey:  fmt.Sprintf("name:buyer %02d", i),
			BuyerName: fmt.Sprintf("Buyer %02d", i),
			Animals:   1,
			// ...and the BIGGEST revenue.
			Revenue: float64(1000 - i),
		})
	}
	return facts
}

func TestBuyerSortReachesEveryRowNotJustThePage(t *testing.T) {
	svc := app.NewBuyerAnalyticsService(sortBuyerRepo{facts: thirtyBuyers()})
	h := NewBuyerAnalyticsHandler(svc)

	code, body := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?sort=revenue&dir=desc&limit=25", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	rows := body["buyers"].([]any)
	if top := rows[0].(map[string]any); top["buyer_name"] != "Buyer 00" || top["revenue"] != 1000.0 {
		t.Fatalf("top row after revenue desc = %v, want Buyer 00 at 1000 (the biggest of all 30)", top)
	}
	_, next := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?sort=revenue&dir=desc&limit=25&offset=25", nil)
	if first := next["buyers"].([]any)[0].(map[string]any); first["revenue"] != 975.0 {
		t.Fatalf("page 2 must continue the same order at 975, got %v", first)
	}
	// The default order is unchanged: newest last sale first.
	_, def := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?limit=25", nil)
	if first := def["buyers"].([]any)[0].(map[string]any); first["buyer_name"] != "Buyer 29" {
		t.Fatalf("default order changed: %v", first)
	}
	if code, body := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?sort=phone_number", nil); code != http.StatusBadRequest || body["error"] != "invalid_sort" {
		t.Fatalf("unknown sort column = %d %v, want 400 invalid_sort", code, body)
	}
}

type sortFarmBornRepo struct{ facts []domain.FarmBornAnimalFact }

func (r sortFarmBornRepo) FarmBornAnimals(context.Context, string, domain.FarmBornFilter) ([]domain.FarmBornAnimalFact, error) {
	return r.facts, nil
}

func (r sortFarmBornRepo) FarmBornOptions(context.Context, string) (domain.FarmBornOptions, error) {
	return domain.FarmBornOptions{}, nil
}

func TestFarmBornSortReachesEverySoldRow(t *testing.T) {
	facts := []domain.FarmBornAnimalFact{}
	for i := 0; i < 30; i++ {
		value := float64(5000 + i*10)
		facts = append(facts, domain.FarmBornAnimalFact{
			GoatID: fmt.Sprintf("g%02d", i), Tag: fmt.Sprintf("T%02d", i), Bucket: domain.FarmBornSold,
			// Newest sale is i=29; the biggest value is i=0 on the OLDEST date -> page 2 by default.
			SaleDate:  fmt.Sprintf("2026-09-%02d", i%28+1),
			SaleValue: &value,
		})
	}
	facts[0].SaleDate = "2026-08-30"
	big := 99999.0
	facts[0].SaleValue = &big
	// A sale with no recorded value sorts LAST whichever way.
	facts[1].SaleValue = nil
	h := NewFarmBornSalesHandler(app.NewFarmBornSalesService(sortFarmBornRepo{facts: facts}))

	code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?from=2026-08-26&to=2026-09-25&sort=sale_value&dir=desc&limit=25")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if top := body["sold"].([]any)[0].(map[string]any); top["tag"] != "T00" {
		t.Fatalf("top row after value desc = %v, want T00 (the biggest of all 30)", top)
	}
	_, asc := serveFarmBorn(t, h, "/procurement/farm-born-sales?from=2026-08-26&to=2026-09-25&sort=sale_value&dir=asc&limit=25&offset=25")
	page := asc["sold"].([]any)
	if last := page[len(page)-1].(map[string]any); last["tag"] != "T01" {
		t.Fatalf("an unpriced sale must sit last even ascending, got %v", last)
	}
	if code, body := serveFarmBorn(t, h, "/procurement/farm-born-sales?sort=goat_id"); code != http.StatusBadRequest || body["error"] != "invalid_sort" {
		t.Fatalf("unknown sort column = %d %v, want 400 invalid_sort", code, body)
	}
}
