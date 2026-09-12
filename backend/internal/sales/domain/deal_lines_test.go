package domain

import (
	"errors"
	"math"
	"testing"
)

// mixedWrite is the sale the maintainer described on 2026-09-12: sheep and goats of different
// breeds, sold to one buyer in one deal.
func mixedWrite() DealWrite {
	return DealWrite{
		SaleDate: "2026-09-12", Farm: FarmCPT,
		BuyerName: "Tanveer", BuyerPlace: "Madur",
		BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AdvanceAmount: fp(50000),
		Lines: []DealLineWrite{
			{ProductType: ProductSheep, Breed: "Anantapur", AnimalCount: fp(10), TotalWeightKg: fp(300), SalesValue: 120000},
			{ProductType: ProductSheep, Breed: "Kenguri", AnimalCount: fp(5), TotalWeightKg: fp(140), SalesValue: 56000},
			{ProductType: ProductGoat, Breed: "Sirohi", AnimalCount: fp(4), TotalWeightKg: fp(100), SalesValue: 45000},
		},
	}
}

func TestNormalizeRollsTheLinesUpOntoTheDeal(t *testing.T) {
	w := mixedWrite().Normalize()
	if err := w.Validate(); err != nil {
		t.Fatalf("mixed sale rejected: %v", err)
	}
	if w.ProductType != ProductMixed || w.Breed != ProductMixed {
		t.Fatalf("rollup product/breed = %q/%q, want Mixed/Mixed", w.ProductType, w.Breed)
	}
	if w.AnimalCount == nil || *w.AnimalCount != 19 {
		t.Fatalf("rollup animals = %v, want 19", w.AnimalCount)
	}
	if w.TotalWeightKg == nil || *w.TotalWeightKg != 540 {
		t.Fatalf("rollup weight = %v, want 540", w.TotalWeightKg)
	}
	if w.SalesValue != 221000 {
		t.Fatalf("rollup value = %v, want 221000 (the sum of the lines, whatever the client sent)", w.SalesValue)
	}
	// Male/female were recorded on no line, so the rollup must say "not recorded", not 0.
	if w.MaleCount != nil || w.FemaleCount != nil {
		t.Fatalf("male/female rollup = %v/%v, want nil (no line recorded a split)", w.MaleCount, w.FemaleCount)
	}
}

func TestRollupKeepsASingleProductWhenEveryLineAgrees(t *testing.T) {
	w := mixedWrite()
	w.Lines = w.Lines[:2] // two sheep breeds
	w = w.Normalize()
	if w.ProductType != ProductSheep {
		t.Fatalf("product = %q, want Sheep: two breeds of one product are still that product", w.ProductType)
	}
	if w.Breed != ProductMixed {
		t.Fatalf("breed = %q, want Mixed", w.Breed)
	}
}

func TestClientSentDealValueIsReplacedByTheLineSum(t *testing.T) {
	w := mixedWrite()
	w.SalesValue = 1 // a stale or hand-typed total
	w = w.Normalize()
	if w.SalesValue != 221000 {
		t.Fatalf("deal value = %v, want 221000: the deal can never disagree with its lines", w.SalesValue)
	}
}

func TestLegacySingleProductBodyBecomesOneLine(t *testing.T) {
	w := validWrite().Normalize()
	if len(w.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 built from the legacy fields", len(w.Lines))
	}
	l := w.Lines[0]
	if l.ProductType != ProductSheep || l.Breed != "Anantapur" || l.SalesValue != 201500 || *l.AnimalCount != 23 {
		t.Fatalf("legacy line = %+v", l)
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("legacy body rejected: %v", err)
	}
}

func TestLineValidationNamesTheLine(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DealWrite)
		field  string
	}{
		{"no lines at all", func(w *DealWrite) { w.Lines = nil }, "lines"},
		{"unknown product on line 2", func(w *DealWrite) { w.Lines[1].ProductType = "Cattle" }, "lines[2].product_type"},
		{"Mixed is a rollup word, never a product", func(w *DealWrite) { w.Lines[0].ProductType = ProductMixed }, "lines[1].product_type"},
		{"blank breed on line 3", func(w *DealWrite) { w.Lines[2].Breed = "  " }, "lines[3].breed"},
		{"zero value on line 1", func(w *DealWrite) { w.Lines[0].SalesValue = 0 }, "lines[1].sales_value"},
		{"negative weight on line 2", func(w *DealWrite) { w.Lines[1].TotalWeightKg = fp(-1) }, "lines[2].total_weight_kg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := mixedWrite()
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
	t.Run("too many lines", func(t *testing.T) {
		w := mixedWrite()
		for len(w.Lines) <= MaxDealLines {
			w.Lines = append(w.Lines, w.Lines[0])
		}
		var v ErrDealValidation
		if err := w.Normalize().Validate(); !errors.As(err, &v) || v.Field != "lines" {
			t.Fatalf("want lines cap refusal, got %v", err)
		}
	})
}

// A mixed deal lands in BOTH product buckets and in one price band per (product, breed), at each
// line's own weight and value -- the whole point of per-line values. Summing the buckets must give
// back the deal's revenue exactly once.
func TestBuildDealAggregatesSplitsAMixedDealByLine(t *testing.T) {
	deal := Deal{
		DealID: "d1", SaleDate: "2026-09-12", Farm: FarmCPT, BuyerName: "Tanveer",
		ProductType: ProductMixed, Breed: ProductMixed,
		AnimalCount: fp(19), TotalWeightKg: fp(540), SalesValue: 221000, Status: StatusDealClosed,
		Lines: []DealLine{
			{LineNo: 1, ProductType: ProductSheep, Breed: "Anantapur", AnimalCount: fp(10), TotalWeightKg: fp(300), SalesValue: 120000},
			{LineNo: 2, ProductType: ProductSheep, Breed: "Kenguri", AnimalCount: fp(5), TotalWeightKg: fp(140), SalesValue: 56000},
			{LineNo: 3, ProductType: ProductGoat, Breed: "Sirohi", AnimalCount: fp(4), TotalWeightKg: fp(100), SalesValue: 45000},
		},
	}
	summary, monthly, bands, buyers := BuildDealAggregates([]Deal{deal})

	if summary.Deals != 1 || summary.Revenue != 221000 || summary.LiveRevenue != 221000 {
		t.Fatalf("summary = %+v: one deal, revenue counted once", summary)
	}
	if summary.Sheep != 15 || summary.Goats != 4 || summary.Animals != 19 {
		t.Fatalf("sheep/goats/animals = %v/%v/%v, want 15/4/19", summary.Sheep, summary.Goats, summary.Animals)
	}
	if summary.LiveWeightKg != 540 {
		t.Fatalf("live weight = %v, want 540", summary.LiveWeightKg)
	}
	if got, want := summary.RealizedPricePerKg, 221000.0/540; math.Abs(got-want) > 1e-9 {
		t.Fatalf("realized price = %v, want %v", got, want)
	}
	if len(monthly) != 1 || monthly[0].SheepRevenue != 176000 || monthly[0].GoatRevenue != 45000 || monthly[0].SheepCount != 15 || monthly[0].GoatCount != 4 {
		t.Fatalf("monthly = %+v", monthly)
	}
	if len(bands) != 3 {
		t.Fatalf("bands = %d, want one per (product, breed) line", len(bands))
	}
	byKey := map[string]PriceBand{}
	for _, b := range bands {
		byKey[b.ProductType+"/"+b.Breed] = b
	}
	if b := byKey["Sheep/Anantapur"]; b.Deals != 1 || b.Animals != 10 || b.WeightKg != 300 || math.Abs(b.AvgPricePerKg-400) > 1e-9 {
		t.Fatalf("Anantapur band = %+v", b)
	}
	if b := byKey["Goat/Sirohi"]; b.Animals != 4 || math.Abs(b.AvgPricePerKg-450) > 1e-9 {
		t.Fatalf("Sirohi band = %+v", b)
	}
	if len(buyers) != 1 || buyers[0].Deals != 1 || buyers[0].Animals != 19 || buyers[0].Revenue != 221000 {
		t.Fatalf("buyers = %+v: the deal is ONE deal to the buyer board", buyers)
	}
	if got := buyers[0].ProductTypes; len(got) != 2 || got[0] != ProductGoat || got[1] != ProductSheep {
		t.Fatalf("buyer product types = %v, want [Goat Sheep] from the lines", got)
	}
}

// A deal read without lines (older fixture, or a reader that never attached them) must aggregate
// exactly as it did before 000294 -- its own columns ARE its one line.
func TestBuildDealAggregatesWithoutLinesIsTheSingleLineCase(t *testing.T) {
	deal := Deal{
		DealID: "d1", SaleDate: "2026-09-12", Farm: FarmCPT, BuyerName: "Tanveer",
		ProductType: ProductGoat, Breed: "Sirohi",
		AnimalCount: fp(4), TotalWeightKg: fp(100), SalesValue: 45000, Status: StatusDealClosed,
	}
	summary, _, bands, _ := BuildDealAggregates([]Deal{deal})
	if summary.Goats != 4 || summary.Sheep != 0 || summary.LiveWeightKg != 100 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(bands) != 1 || bands[0].Breed != "Sirohi" || math.Abs(bands[0].AvgPricePerKg-450) > 1e-9 {
		t.Fatalf("bands = %+v", bands)
	}
}

func TestDealAnimalsSumsLinesAndSkipsManure(t *testing.T) {
	d := Deal{ProductType: ProductMixed, AnimalCount: fp(99), Lines: []DealLine{
		{ProductType: ProductSheep, AnimalCount: fp(3)},
		{ProductType: ProductGoat, MaleCount: fp(1), FemaleCount: fp(2)},
		{ProductType: ProductManure, AnimalCount: fp(500)},
	}}
	if got := d.Animals(); got != 6 {
		t.Fatalf("animals = %v, want 6 (3 + 1 + 2, manure never counts, rollup column ignored)", got)
	}
}
