package domain

import (
	"math"
	"testing"
	"time"
)

func buyerAsOf() time.Time { return time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC) }

// TestBuildBuyerAnalyticsFoldsDealsIntoOneBuyerAndReadsRepeatCadence pins the page's arithmetic:
// every deal with the same resolved key is ONE buyer, the register's name/phone/category win
// over the typed spelling, purchases count deals, the cadence is the span over the gaps, and the
// summary ranges over the same buyers the rows do.
func TestBuildBuyerAnalyticsFoldsDealsIntoOneBuyerAndReadsRepeatCadence(t *testing.T) {
	facts := []BuyerDealFact{
		// Mahendran: two register-backed deals plus a sheet deal folded onto the same vendor by
		// name. Three purchases, 2025-12-03 -> 2026-09-09 = 280 days over 2 gaps = 140.
		{DealID: "m1", SaleDate: "2026-09-09", BuyerKey: "vendor:v1", VendorID: "v1", BuyerName: "Mahendran", VendorName: "Mahendran", VendorPhone: "9750078019", VendorCategory: "Agent", VendorPlace: "Pollachi", Animals: 40, Revenue: 341964, Outstanding: 0, ProductTypes: []string{"Sheep"}},
		{DealID: "m2", SaleDate: "2026-09-02", BuyerKey: "vendor:v1", VendorID: "v1", BuyerName: "Mahendran", VendorName: "Mahendran", VendorPhone: "9750078019", VendorCategory: "Agent", VendorPlace: "Pollachi", Animals: 25, Revenue: 197415, Outstanding: 50000, ProductTypes: []string{"Goat"}},
		{DealID: "m3", SaleDate: "2025-12-03", BuyerKey: "vendor:v1", VendorID: "v1", BuyerName: "mahendran", VendorName: "Mahendran", VendorPhone: "9750078019", VendorCategory: "Agent", VendorPlace: "Pollachi", Animals: 10, Revenue: 60000, ProductTypes: []string{"Sheep"}},
		// Firoz: name-only, two deals on two spellings; the newest spelling and place show.
		{DealID: "f1", SaleDate: "2026-03-14", BuyerKey: "name:firoz", BuyerName: "firoz", BuyerPlace: "Palladam", Animals: 5, Revenue: 40000, ProductTypes: []string{"Goat"}},
		{DealID: "f2", SaleDate: "2026-08-11", BuyerKey: "name:firoz", BuyerName: "Firoz", BuyerPlace: "Palladam, TN", Animals: 6, Revenue: 60000, Outstanding: 10000, ProductTypes: []string{"Goat", "Manure"}},
		// Riyaz: one purchase, name-only.
		{DealID: "r1", SaleDate: "2026-05-17", BuyerKey: "name:riyaz", BuyerName: "Riyaz", BuyerPlace: "Coimbatore", Animals: 12, Revenue: 100000, ProductTypes: []string{"Sheep"}},
		// Keethiraj: three purchases all on ONE day -- a repeat buyer with no cadence.
		{DealID: "k1", SaleDate: "2026-08-17", BuyerKey: "name:keethiraj", BuyerName: "Keethiraj", Animals: 1, Revenue: 1000, ProductTypes: []string{"Goat"}},
		{DealID: "k2", SaleDate: "2026-08-17", BuyerKey: "name:keethiraj", BuyerName: "Keethiraj", Animals: 1, Revenue: 1000, ProductTypes: []string{"Goat"}},
		{DealID: "k3", SaleDate: "2026-08-17", BuyerKey: "name:keethiraj", BuyerName: "Keethiraj", Animals: 1, Revenue: 1000, ProductTypes: []string{"Goat"}},
	}

	out := BuildBuyerAnalytics(facts, buyerAsOf(), 25, 0)

	if out.TotalBuyers != 4 || len(out.Buyers) != 4 || out.Summary.Buyers != 4 {
		t.Fatalf("buyers = total %d, rows %d, summary %d; want 4 each", out.TotalBuyers, len(out.Buyers), out.Summary.Buyers)
	}
	// Highest revenue first.
	m := out.Buyers[0]
	if m.BuyerKey != "vendor:v1" || !m.InRegister || m.BuyerName != "Mahendran" || m.Phone != "9750078019" || m.Category != "Agent" || m.Place != "Pollachi" {
		t.Fatalf("mahendran row = %+v", m)
	}
	if m.Purchases != 3 || m.RepeatPurchases != 2 || !m.Repeat || m.Animals != 75 || m.Revenue != 599379 || m.Outstanding != 50000 {
		t.Fatalf("mahendran figures = %+v", m)
	}
	if m.FirstSaleDate != "2025-12-03" || m.LastSaleDate != "2026-09-09" {
		t.Fatalf("mahendran dates = %s..%s", m.FirstSaleDate, m.LastSaleDate)
	}
	if m.AvgDaysBetween == nil || *m.AvgDaysBetween != 140 {
		t.Fatalf("mahendran cadence = %v, want 140", m.AvgDaysBetween)
	}
	if m.DaysSinceLast == nil || *m.DaysSinceLast != 6 {
		t.Fatalf("mahendran days since last = %v, want 6", m.DaysSinceLast)
	}
	if len(m.ProductTypes) != 2 || m.ProductTypes[0] != "Goat" || m.ProductTypes[1] != "Sheep" {
		t.Fatalf("mahendran product types = %v", m.ProductTypes)
	}

	byKey := map[string]BuyerRow{}
	for _, r := range out.Buyers {
		byKey[r.BuyerKey] = r
	}
	f := byKey["name:firoz"]
	if f.InRegister || f.Phone != "" || f.BuyerName != "Firoz" || f.Place != "Palladam, TN" {
		t.Fatalf("firoz should be a name-only buyer with the newest spelling: %+v", f)
	}
	if f.Purchases != 2 || f.AvgDaysBetween == nil || *f.AvgDaysBetween != 150 {
		t.Fatalf("firoz cadence = %+v", f)
	}
	r := byKey["name:riyaz"]
	if r.Repeat || r.RepeatPurchases != 0 || r.AvgDaysBetween != nil {
		t.Fatalf("riyaz must be a one-time buyer with no cadence: %+v", r)
	}
	k := byKey["name:keethiraj"]
	if !k.Repeat || k.AvgDaysBetween != nil {
		t.Fatalf("same-day repeat buyer must report no cadence rather than 0: %+v", k)
	}

	// Shares sum to 100 over the same rows the summary counts.
	shares := 0.0
	for _, r := range out.Buyers {
		shares += r.SharePct
	}
	if math.Abs(shares-100) > 1e-9 {
		t.Fatalf("shares sum to %v, want 100", shares)
	}
	s := out.Summary
	if s.RepeatBuyers != 3 || s.OneTimeBuyers != 1 || s.NotInRegister != 3 || s.Purchases != 9 {
		t.Fatalf("summary = %+v", s)
	}
	wantRepeatRevenue := 599379.0 + 100000 + 3000
	if s.RepeatRevenue != wantRepeatRevenue || math.Abs(s.RepeatRevenuePct-wantRepeatRevenue/s.Revenue*100) > 1e-9 {
		t.Fatalf("repeat revenue = %v (%v%%), want %v", s.RepeatRevenue, s.RepeatRevenuePct, wantRepeatRevenue)
	}
	if s.Outstanding != 60000 || s.Animals != 101 || s.PeriodFrom != "2025-12-03" || s.PeriodTo != "2026-09-09" {
		t.Fatalf("summary totals = %+v", s)
	}
}

// TestBuildBuyerAnalyticsPaginationSlicesRowsButNotTheSummary pins the read-model contract: the page slices
// rows only; TotalBuyers and every summary figure stay whole-filter.
func TestBuildBuyerAnalyticsPaginationSlicesRowsButNotTheSummary(t *testing.T) {
	facts := []BuyerDealFact{
		{DealID: "1", SaleDate: "2026-01-01", BuyerKey: "name:a", BuyerName: "A", Revenue: 300},
		{DealID: "2", SaleDate: "2026-01-02", BuyerKey: "name:b", BuyerName: "B", Revenue: 200},
		{DealID: "3", SaleDate: "2026-01-03", BuyerKey: "name:c", BuyerName: "C", Revenue: 100},
	}
	out := BuildBuyerAnalytics(facts, buyerAsOf(), 2, 2)
	if out.TotalBuyers != 3 || out.Summary.Buyers != 3 || out.Summary.Revenue != 600 {
		t.Fatalf("whole-filter figures moved with the page: %+v", out)
	}
	if len(out.Buyers) != 1 || out.Buyers[0].BuyerName != "C" || out.Limit != 2 || out.Offset != 2 {
		t.Fatalf("page = %+v", out)
	}
	beyond := BuildBuyerAnalytics(facts, buyerAsOf(), 2, 10)
	if len(beyond.Buyers) != 0 || beyond.TotalBuyers != 3 {
		t.Fatalf("page beyond the end = %+v", beyond)
	}
	empty := BuildBuyerAnalytics(nil, buyerAsOf(), 0, 0)
	if empty.TotalBuyers != 0 || empty.Summary.RepeatRevenuePct != 0 || empty.Limit != DefaultBuyerPageSize {
		t.Fatalf("empty read = %+v", empty)
	}
}

func TestNormalizeBuyerNameAndFarmFilter(t *testing.T) {
	if got := NormalizeBuyerName("  Ramesh   Traders "); got != "ramesh traders" {
		t.Fatalf("normalized = %q", got)
	}
	for raw, want := range map[string]struct {
		farm string
		ok   bool
	}{"": {"", true}, "all": {"", true}, "ALL": {"", true}, "CBE": {"CBE", true}, "CPT": {"CPT", true}, "cbe": {"", false}, "Pollachi": {"", false}} {
		farm, ok := NormalizeBuyerFarmFilter(raw)
		if farm != want.farm || ok != want.ok {
			t.Fatalf("farm filter %q = %q/%v, want %q/%v", raw, farm, ok, want.farm, want.ok)
		}
	}
}
