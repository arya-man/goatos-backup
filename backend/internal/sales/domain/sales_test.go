package domain

import (
	"errors"
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }
func sp(v string) *string   { return &v }

func validWrite() DealWrite {
	return DealWrite{
		SaleDate: "2026-08-17", Farm: FarmCBE, ProductType: ProductSheep, Breed: "Anantapur",
		BuyerName: "Tanveer", BuyerPlace: "Madur",
		AnimalCount: fp(23), TotalWeightKg: fp(600), SalesValue: 201500,
	}
}

func TestDealWriteValidateAcceptsARealSale(t *testing.T) {
	if err := validWrite().Normalize().Validate(); err != nil {
		t.Fatalf("valid write rejected: %v", err)
	}
}

func TestDealWriteValidateRejectsEachBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DealWrite)
		field  string
	}{
		{"missing sale_date", func(w *DealWrite) { w.SaleDate = "" }, "sale_date"},
		{"malformed sale_date", func(w *DealWrite) { w.SaleDate = "17-08-2026" }, "sale_date"},
		{"unknown farm rejected not defaulted", func(w *DealWrite) { w.Farm = "MYSORE" }, "farm"},
		{"lowercase farm is not silently accepted", func(w *DealWrite) { w.Farm = "cbe" }, "farm"},
		{"unknown product type", func(w *DealWrite) { w.ProductType = "Cattle" }, "product_type"},
		{"blank breed", func(w *DealWrite) { w.Breed = "   " }, "breed"},
		{"blank buyer", func(w *DealWrite) { w.BuyerName = "  " }, "buyer_name"},
		{"zero sale value", func(w *DealWrite) { w.SalesValue = 0 }, "sales_value"},
		{"negative sale value", func(w *DealWrite) { w.SalesValue = -5 }, "sales_value"},
		{"negative weight", func(w *DealWrite) { w.TotalWeightKg = fp(-1) }, "total_weight_kg"},
		{"negative animals", func(w *DealWrite) { w.AnimalCount = fp(-2) }, "animal_count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := validWrite()
			tc.mutate(&w)
			err := w.Normalize().Validate()
			var v ErrDealValidation
			if !errors.As(err, &v) {
				t.Fatalf("want ErrDealValidation, got %v", err)
			}
			if v.Field != tc.field {
				t.Fatalf("failed field = %q want %q", v.Field, tc.field)
			}
		})
	}
}

func TestNormalizeFarmFilter(t *testing.T) {
	for raw, want := range map[string]string{"": "", "all": "", "ALL": "", "CBE": "CBE", "CPT": "CPT"} {
		got, ok := NormalizeFarmFilter(raw)
		if !ok || got != want {
			t.Fatalf("NormalizeFarmFilter(%q) = %q,%v want %q,true", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"cbe", "Bangalore", "CBE,CPT"} {
		if _, ok := NormalizeFarmFilter(raw); ok {
			t.Fatalf("NormalizeFarmFilter(%q) accepted; must reject rather than widen to all", raw)
		}
	}
}

func TestAnimalsPrefersAnimalCountAndNeverCountsManure(t *testing.T) {
	d := Deal{ProductType: ProductSheep, AnimalCount: fp(23), MaleCount: fp(99), FemaleCount: fp(99)}
	if got := d.Animals(); got != 23 {
		t.Fatalf("animal_count must win over the split: got %v", got)
	}
	d = Deal{ProductType: ProductGoat, MaleCount: fp(12), FemaleCount: fp(11)}
	if got := d.Animals(); got != 23 {
		t.Fatalf("male+female fallback: got %v", got)
	}
	d = Deal{ProductType: ProductManure, AnimalCount: fp(5)}
	if got := d.Animals(); got != 0 {
		t.Fatalf("manure must never contribute animals: got %v", got)
	}
}

// TestBuildDealAggregates pins the whole rollup contract on a small mixed fixture: two sheep
// deals in different months, one goat deal, one manure deal.
func TestBuildDealAggregates(t *testing.T) {
	closed := []Deal{
		{SaleDate: "2025-04-15", Farm: FarmCPT, ProductType: ProductSheep, Breed: "Anantapur",
			BuyerName: "Tanveer", BuyerPlace: sp("Madur"), AnimalCount: fp(20), TotalWeightKg: fp(500), SalesValue: 150000},
		{SaleDate: "2025-05-02", Farm: FarmCPT, ProductType: ProductSheep, Breed: "Anantapur",
			BuyerName: "Tanveer", AnimalCount: fp(10), TotalWeightKg: fp(250), SalesValue: 100000},
		{SaleDate: "2025-05-20", Farm: FarmCBE, ProductType: ProductGoat, Breed: "Sojat",
			BuyerName: "Irshad", MaleCount: fp(3), FemaleCount: fp(2), TotalWeightKg: fp(200), SalesValue: 90000},
		{SaleDate: "2025-05-25", Farm: FarmCBE, ProductType: ProductManure, Breed: "Manure",
			BuyerName: "Raitha FPO", TotalWeightKg: fp(3000), SalesValue: 30000},
	}

	summary, monthly, bands, buyers := BuildDealAggregates(closed)

	if summary.Deals != 4 || summary.Revenue != 370000 {
		t.Fatalf("summary deals/revenue = %d/%v", summary.Deals, summary.Revenue)
	}
	if summary.LiveRevenue != 340000 || summary.LiveWeightKg != 950 {
		t.Fatalf("live revenue/weight = %v/%v", summary.LiveRevenue, summary.LiveWeightKg)
	}
	if summary.Animals != 35 || summary.Sheep != 30 || summary.Goats != 5 {
		t.Fatalf("animals/sheep/goats = %v/%v/%v", summary.Animals, summary.Sheep, summary.Goats)
	}
	if summary.ManureKg != 3000 || summary.ManureRevenue != 30000 {
		t.Fatalf("manure kg/revenue = %v/%v", summary.ManureKg, summary.ManureRevenue)
	}
	// Numerator and denominator range over the same closed live weighed deals.
	if want := 340000.0 / 950.0; math.Abs(summary.RealizedPricePerKg-want) > 1e-9 {
		t.Fatalf("realized price = %v want %v", summary.RealizedPricePerKg, want)
	}
	if summary.PeriodFrom != "2025-04-15" || summary.PeriodTo != "2025-05-25" {
		t.Fatalf("period %s..%s", summary.PeriodFrom, summary.PeriodTo)
	}

	if len(monthly) != 2 || monthly[0].Month != "2025-04" || monthly[1].Month != "2025-05" {
		t.Fatalf("monthly months wrong: %+v", monthly)
	}
	may := monthly[1]
	if may.SheepRevenue != 100000 || may.GoatRevenue != 90000 || may.ManureRevenue != 30000 {
		t.Fatalf("may revenues: %+v", may)
	}
	if may.SheepCount != 10 || may.GoatCount != 5 || may.ManureKg != 3000 {
		t.Fatalf("may counts: %+v", may)
	}
	// The monthly series must sum to the summary revenue -- same producer rows, same predicate.
	total := 0.0
	for _, m := range monthly {
		total += m.SheepRevenue + m.GoatRevenue + m.ManureRevenue
	}
	if total != summary.Revenue {
		t.Fatalf("monthly sum %v != summary revenue %v", total, summary.Revenue)
	}

	if len(bands) != 2 {
		t.Fatalf("bands = %+v", bands)
	}
	// Goat/Sojat: 90000/200 = 450 per kg beats Sheep/Anantapur (250000/750 ≈ 333), so it leads.
	if bands[0].ProductType != ProductGoat || bands[0].Breed != "Sojat" {
		t.Fatalf("band order wrong: %+v", bands)
	}
	sheep := bands[1]
	if sheep.Deals != 2 || sheep.WeightKg != 750 || sheep.Revenue != 250000 {
		t.Fatalf("sheep band: %+v", sheep)
	}
	// Weighted average, not the mean of per-deal prices.
	if want := 250000.0 / 750.0; math.Abs(sheep.AvgPricePerKg-want) > 1e-9 {
		t.Fatalf("sheep avg = %v want %v", sheep.AvgPricePerKg, want)
	}
	if math.Abs(sheep.MinPricePerKg-300) > 1e-9 || math.Abs(sheep.MaxPricePerKg-400) > 1e-9 {
		t.Fatalf("sheep min/max = %v/%v", sheep.MinPricePerKg, sheep.MaxPricePerKg)
	}
	// Manure never enters price bands.
	for _, b := range bands {
		if b.ProductType == ProductManure {
			t.Fatalf("manure leaked into price bands: %+v", b)
		}
	}

	if len(buyers) != 3 || buyers[0].BuyerName != "Tanveer" {
		t.Fatalf("buyer order: %+v", buyers)
	}
	tanveer := buyers[0]
	if tanveer.Deals != 2 || tanveer.Revenue != 250000 || tanveer.Animals != 30 {
		t.Fatalf("tanveer row: %+v", tanveer)
	}
	if tanveer.BuyerPlace != "Madur" {
		t.Fatalf("place must keep the first recorded value: %+v", tanveer)
	}
	if want := 250000.0 / 370000.0 * 100; math.Abs(tanveer.SharePct-want) > 1e-9 {
		t.Fatalf("share = %v want %v", tanveer.SharePct, want)
	}
	if len(tanveer.ProductTypes) != 1 || tanveer.ProductTypes[0] != ProductSheep {
		t.Fatalf("tanveer products: %+v", tanveer.ProductTypes)
	}
}

func TestBuildDealAggregatesGuardsZeroWeight(t *testing.T) {
	summary, _, bands, _ := BuildDealAggregates([]Deal{
		{SaleDate: "2025-06-01", ProductType: ProductSheep, Breed: "Anantapur", BuyerName: "A", SalesValue: 5000},
	})
	if summary.RealizedPricePerKg != 0 {
		t.Fatalf("no weighed live deals must yield 0, got %v", summary.RealizedPricePerKg)
	}
	if len(bands) != 0 {
		t.Fatalf("weightless deal must not create a price band: %+v", bands)
	}
}

// TestBuildDealAggregatesOneToManyBuyerDeals is the cardinality adversarial case: one buyer with
// MANY deals across product types must contribute one buyer row whose figures are sums, and the
// deals must never fan the buyer's animals or revenue out by the number of product types.
func TestBuildDealAggregatesOneToManyBuyerDeals(t *testing.T) {
	deals := []Deal{
		{SaleDate: "2025-04-01", ProductType: ProductSheep, Breed: "Anantapur", BuyerName: "Tanveer", AnimalCount: fp(10), TotalWeightKg: fp(250), SalesValue: 100000},
		{SaleDate: "2025-04-08", ProductType: ProductGoat, Breed: "Sojat", BuyerName: "Tanveer", AnimalCount: fp(5), TotalWeightKg: fp(200), SalesValue: 90000},
		{SaleDate: "2025-04-20", ProductType: ProductManure, Breed: "Manure", BuyerName: "Tanveer", TotalWeightKg: fp(1000), SalesValue: 10000},
	}
	summary, _, _, buyers := BuildDealAggregates(deals)
	if len(buyers) != 1 {
		t.Fatalf("one buyer with many deals must stay one row: %+v", buyers)
	}
	b := buyers[0]
	if b.Deals != 3 || b.Revenue != 200000 || b.Animals != 15 {
		t.Fatalf("buyer sums fanned out: %+v", b)
	}
	if len(b.ProductTypes) != 3 {
		t.Fatalf("product types must be the distinct set: %+v", b.ProductTypes)
	}
	if b.SharePct != 100 {
		t.Fatalf("sole buyer's share = %v want 100", b.SharePct)
	}
	if summary.Animals != 15 {
		t.Fatalf("summary animals fanned out: %v", summary.Animals)
	}
}

func TestBucketWeightGapBucketsAreDisjoint(t *testing.T) {
	s := WeightAuditSummary{}
	s.BucketWeightGap(23, 23)     // 0 -> within 0.3
	s.BucketWeightGap(22.7, 23)   // 0.3 exactly -> within 0.3
	s.BucketWeightGap(22, 23)     // 1.0 exactly -> within 1
	s.BucketWeightGap(25.5, 23)   // 2.5 -> over 1
	s.BucketWeightGap(23, 26.001) // book above video still counts by absolute gap

	if s.Total != 5 {
		t.Fatalf("total = %d", s.Total)
	}
	if s.Within03Kg != 2 || s.Within1Kg != 1 || s.Over1Kg != 2 {
		t.Fatalf("buckets = %d/%d/%d", s.Within03Kg, s.Within1Kg, s.Over1Kg)
	}
	if s.Within03Kg+s.Within1Kg+s.Over1Kg != s.Total {
		t.Fatal("buckets must partition the total")
	}
	if math.Abs(s.MaxGapKg-3.001) > 1e-9 {
		t.Fatalf("max gap = %v", s.MaxGapKg)
	}
}
