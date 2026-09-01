package domain

import (
	"math"
	"testing"
)

// testAsOf pins the age clock so a test that asserts DaysSincePurchase reads the same figure
// tomorrow. Every FinalizeLoadwise call in this file passes it; a test that cares about the age
// states its own purchase date relative to it.
const testAsOf = "2026-09-01"

func TestFinalizeLoadwiseDerivesValueAndBasisPerLoad(t *testing.T) {
	overall := lw(9000)
	loads := []LoadwiseLoad{
		{
			// Fully recorded load: its own priced sales form the basis.
			LoadID: "a", Purchased: 100, Sold: 97, Mortality: 3, Remaining: 0,
			AnimalCost: lw(500000), TransportCost: lw(20000), OtherCost: lw(5000),
			SoldValue: 970000, SoldPriced: 97,
		},
		{
			// No sales yet: remaining stock priced off the overall average; cost not recorded.
			LoadID: "b", Purchased: 50, Sold: 0, Mortality: 1, Remaining: 49,
			SoldValue: 0, SoldPriced: 0,
		},
		{
			// Sold animals exist but none carry a tagged deal share: sold count stays honest
			// (unpriced), and the basis falls back to overall.
			LoadID: "c", Purchased: 10, Sold: 4, Mortality: 0, Remaining: 5, Unaccounted: 1,
			SoldValue: 0, SoldPriced: 0,
		},
	}

	out := FinalizeLoadwise(loads, 3, overall, testAsOf)

	a := out.Loads[0]
	if a.PurchaseValue == nil || *a.PurchaseValue != 525000 {
		t.Fatalf("load a purchase value = %v, want 525000 (animal+transport+other)", a.PurchaseValue)
	}
	if a.PriceBasis != LoadwisePriceBasisLoad || a.AvgSoldPrice == nil || *a.AvgSoldPrice != 10000 {
		t.Fatalf("load a basis = %s avg = %v, want its own 10000", a.PriceBasis, a.AvgSoldPrice)
	}
	if a.RemainingValue == nil || *a.RemainingValue != 0 {
		t.Fatalf("load a remaining value = %v, want 0 with a basis (nothing left, priced)", a.RemainingValue)
	}

	b := out.Loads[1]
	if b.PurchaseValue != nil {
		t.Fatalf("load b purchase value = %v, want nil: cost not recorded is never zero", b.PurchaseValue)
	}
	if b.PriceBasis != LoadwisePriceBasisOverall || b.RemainingValue == nil || *b.RemainingValue != 49*9000 {
		t.Fatalf("load b basis = %s remaining = %v, want overall 441000", b.PriceBasis, b.RemainingValue)
	}

	c := out.Loads[2]
	if c.PriceBasis != LoadwisePriceBasisOverall {
		t.Fatalf("load c basis = %s, want overall: zero-value sold shares never price stock", c.PriceBasis)
	}

	s := out.Summary
	if s.Purchased != 160 || s.Sold != 101 || s.Mortality != 4 || s.Remaining != 54 || s.Unaccounted != 1 {
		t.Fatalf("summary counts = %+v, want sums of the rows", s)
	}
	if s.PurchaseValue != 525000 || s.CostedLoads != 1 {
		t.Fatalf("summary purchase value = %v over %d loads, want 525000 over exactly the 1 costed load", s.PurchaseValue, s.CostedLoads)
	}
	if s.SoldValue != 970000 {
		t.Fatalf("summary sold value = %v, want 970000", s.SoldValue)
	}
	if want := 0.0 + 49*9000 + 5*9000; math.Abs(s.RemainingValue-want) > 0.001 {
		t.Fatalf("summary remaining value = %v, want %v", s.RemainingValue, want)
	}
}

func TestFinalizeLoadwiseWithNoPriceBasisAnywhere(t *testing.T) {
	out := FinalizeLoadwise([]LoadwiseLoad{
		{LoadID: "a", Purchased: 5, Remaining: 5},
	}, 1, nil, testAsOf)
	row := out.Loads[0]
	if row.PriceBasis != LoadwisePriceBasisNone || row.AvgSoldPrice != nil || row.RemainingValue != nil {
		t.Fatalf("no sales anywhere must yield basis=none and NO estimate, got basis=%s avg=%v remaining=%v",
			row.PriceBasis, row.AvgSoldPrice, row.RemainingValue)
	}
	if out.Summary.RemainingValue != 0 {
		t.Fatalf("summary remaining value = %v, want 0 when no load has a basis", out.Summary.RemainingValue)
	}
}

func TestFinalizeLoadwiseFoldsPriorOutcomesIntoTheReconciliation(t *testing.T) {
	out := FinalizeLoadwise([]LoadwiseLoad{
		{
			// GoatOS tracks only the 60 animals still on farm; 30 were already sold for 300000
			// and 10 already died before tracking started.
			LoadID: "legacy", DeclaredCount: 100, Purchased: 60, Remaining: 60,
			PriorSold: LoadwisePriorOutcome{Count: 30, Value: lw(300000), FirstOn: "2026-04-30", LastOn: "2026-08-17"},
			PriorDead: LoadwisePriorOutcome{Count: 10, FirstOn: "2025-11-24", LastOn: "2025-11-24"},
		},
	}, 1, nil, testAsOf)
	row := out.Loads[0]
	if row.Purchased != 100 || row.Sold != 30 || row.Mortality != 10 || row.Remaining != 60 || row.Unaccounted != 0 {
		t.Fatalf("folded counts = %+v, want the whole load reconciled (100 = 30 + 10 + 60)", row)
	}
	if row.SoldValue != 300000 || row.SoldPriced != 30 {
		t.Fatalf("folded money = %v over %d, want the prior revenue attributed", row.SoldValue, row.SoldPriced)
	}
	if row.PriceBasis != LoadwisePriceBasisLoad || row.AvgSoldPrice == nil || *row.AvgSoldPrice != 10000 {
		t.Fatalf("basis = %s avg %v, want the prior sales to price remaining stock", row.PriceBasis, row.AvgSoldPrice)
	}
	if row.RemainingValue == nil || *row.RemainingValue != 600000 {
		t.Fatalf("remaining value = %v, want 60 x 10000", row.RemainingValue)
	}
	if s := out.Summary; s.Purchased != 100 || s.Sold != 30 || s.Mortality != 10 || s.SoldValue != 300000 {
		t.Fatalf("summary = %+v, want folded totals", s)
	}
}

func TestFinalizeLoadwiseUsesTheDeclaredCountAsTheDenominator(t *testing.T) {
	// The load says it brought in 76. The register accounts for 69 sold and 6 dead (both prior)
	// and nothing on farm, so ONE animal is unaccounted for — the discrepancy the column exists
	// to show. Deriving purchased from the parts instead would report 75 and a tidy zero.
	out := FinalizeLoadwise([]LoadwiseLoad{
		{
			LoadID: "declared", DeclaredCount: 76,
			PriorSold: LoadwisePriorOutcome{Count: 69, Value: lw(1226428)},
			PriorDead: LoadwisePriorOutcome{Count: 6},
		},
		{
			// No declared count: the attributed animals are the whole load, so it reconciles.
			LoadID: "undeclared", Purchased: 4, Remaining: 4,
		},
		{
			// MORE attributed than declared: negative unaccounted, still shown, never clamped.
			LoadID: "over", DeclaredCount: 10, Purchased: 12, Remaining: 12,
		},
	}, 3, nil, testAsOf)

	if row := out.Loads[0]; row.Purchased != 76 || row.Sold != 69 || row.Mortality != 6 || row.Unaccounted != 1 {
		t.Fatalf("declared load = %+v, want 76 purchased and 1 unaccounted", row)
	}
	if row := out.Loads[1]; row.Purchased != 4 || row.Unaccounted != 0 {
		t.Fatalf("undeclared load = %+v, want the attributed animals as the whole load", row)
	}
	if row := out.Loads[2]; row.Purchased != 10 || row.Unaccounted != -2 {
		t.Fatalf("over-attributed load = %+v, want a NEGATIVE unaccounted rather than a clamp", row)
	}
}

func TestFinalizeLoadwiseProfitCountsStockAndRefusesToGuessACost(t *testing.T) {
	overall := lw(10000)
	out := FinalizeLoadwise([]LoadwiseLoad{
		{
			// Sold out and costed: profit is purely realised.
			LoadID: "soldout", DeclaredCount: 10, Purchased: 10,
			AnimalCost: lw(80000), SoldValue: 120000, SoldPriced: 10,
		},
		{
			// Nothing sold yet: the profit is the STOCK on farm against the cost. Without the
			// stock this healthy load would report a total loss of its purchase price.
			LoadID: "onfarm", DeclaredCount: 10, Purchased: 10, Remaining: 10,
			AnimalCost: lw(80000),
		},
		{
			// A real loss: cost exceeds sales plus stock. Reported signed, never clamped.
			LoadID: "loss", DeclaredCount: 10, Purchased: 10,
			AnimalCost: lw(200000), SoldValue: 120000, SoldPriced: 10,
		},
		{
			// NO recorded cost: no profit can be stated. Treating the missing cost as zero would
			// report the entire sale as profit.
			LoadID: "uncosted", DeclaredCount: 10, Purchased: 10, SoldValue: 120000, SoldPriced: 10,
		},
	}, 4, overall, testAsOf)

	if p := out.Loads[0].ProfitLoss; p == nil || *p != 40000 {
		t.Fatalf("sold-out profit = %v, want 120000 - 80000", p)
	}
	if p := out.Loads[1].ProfitLoss; p == nil || *p != 20000 {
		t.Fatalf("on-farm profit = %v, want 10 x 10000 stock - 80000 cost", p)
	}
	if p := out.Loads[2].ProfitLoss; p == nil || *p != -80000 {
		t.Fatalf("loss = %v, want a NEGATIVE -80000, never clamped to zero", p)
	}
	if p := out.Loads[3].ProfitLoss; p != nil {
		t.Fatalf("uncosted profit = %v, want ABSENT: an unrecorded cost is not a free load", p)
	}
	// The summary ranges over the SAME key set as CostedLoads — the uncosted load contributes to
	// neither, so a priced and an unpriced load are never mixed into one total.
	if out.Summary.CostedLoads != 3 || out.Summary.ProfitLoss != 40000+20000-80000 {
		t.Fatalf("summary profit = %v over %d costed loads", out.Summary.ProfitLoss, out.Summary.CostedLoads)
	}
}

func TestLoadCostEditValidate(t *testing.T) {
	cases := []struct {
		name    string
		edit    LoadCostEdit
		wantErr string // "" = valid
	}{
		{"full cost", LoadCostEdit{AnimalCost: lw(100), TransportCost: lw(10), OtherCost: lw(1)}, ""},
		{"animal only", LoadCostEdit{AnimalCost: lw(100)}, ""},
		{"clear everything", LoadCostEdit{}, ""},
		{"zero animal cost is a recorded free load, allowed", LoadCostEdit{AnimalCost: lw(0)}, ""},
		{"negative animal", LoadCostEdit{AnimalCost: lw(-1)}, "animal_cost"},
		{"negative transport", LoadCostEdit{AnimalCost: lw(100), TransportCost: lw(-5)}, "transport_cost"},
		{"detail without animal cost", LoadCostEdit{TransportCost: lw(10)}, "animal_cost"},
	}
	for _, tc := range cases {
		err := tc.edit.Validate()
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		v, ok := err.(ErrLoadwiseValidation)
		if !ok || v.Field != tc.wantErr {
			t.Fatalf("%s: got %v, want validation on %s", tc.name, err, tc.wantErr)
		}
	}
}

// lw is this file's optional-money literal helper (feed_purchase_test.go owns f).
func lw(v float64) *float64 { return &v }

// TestPurchaseValueIsLandedCostNotExFarm is the regression for the 2026-09-01 defect the
// maintainer reported: "Purchase value must include all costs including transport. It is not just
// ex-farm animal value."
//
// The FORMULA was never wrong — loadPurchaseValue has always summed all three — so a test that
// only asserted the sum would have passed while the screen was wrong. What was wrong was that only
// the animal figure ever reached the three columns, because the farm's sheet records cost as one
// row per event per load and the import took the Purchase row alone. This therefore tests the
// whole path a cost now travels: the sheet's cost EVENTS roll into the three buckets, and those
// buckets produce purchase value.
//
// The numbers are Load 131's real ones, so a change that drops a cost kind shows up as a rupee
// figure someone can check against the sheet rather than as an abstract failure.
func TestPurchaseValueIsLandedCostNotExFarm(t *testing.T) {
	lines := []LoadCostLine{
		{Kind: CostKindAnimal, Amount: 502000},
		{Kind: CostKindTransport, Amount: 19000},
		{Kind: CostKindLabour, Amount: 5400},
		{Kind: CostKindTransit, Amount: 2500},
	}

	animal, transport, other := RollUpCostLines(lines)
	if animal == nil || *animal != 502000 {
		t.Fatalf("animal bucket = %v, want 502000", animal)
	}
	if transport == nil || *transport != 19000 {
		t.Fatalf("transport bucket = %v, want 19000", transport)
	}
	// Labour and transit share the "other" column — the maintainer kept the list at three columns
	// and put the itemisation behind the click (2026-09-01).
	if other == nil || *other != 7900 {
		t.Fatalf("other bucket = %v, want 5400+2500=7900", other)
	}

	value := loadPurchaseValue(animal, transport, other)
	if value == nil || *value != 528900 {
		t.Fatalf("purchase value = %v, want the LANDED 528900, not the ex-farm 502000", value)
	}
	// The defect stated plainly: the ex-farm figure is NOT the answer.
	if value != nil && *value == 502000 {
		t.Fatal("purchase value is the ex-farm animal price; transport and the rest were dropped")
	}
}

// TestEveryCostKindReachesABucket pins the maintainer's 2026-09-01 answer that ALL SIX cost types
// count toward landed cost. A kind that reached no bucket would silently understate purchase
// value — the exact defect being fixed — so this walks the whole declared vocabulary rather than a
// sample, and any kind added to CostLineKinds without a bucket turns it red.
func TestEveryCostKindReachesABucket(t *testing.T) {
	for _, kind := range CostLineKinds {
		lines := []LoadCostLine{{Kind: kind, Amount: 1000}}
		animal, transport, other := RollUpCostLines(lines)
		value := loadPurchaseValue(animal, transport, other)
		if value == nil {
			// animal is the anchor: a bucket that is not animal produces no value on its own,
			// which is the schema rule (cost detail cannot exist without an animal cost), so
			// re-check with the anchor present.
			animal, transport, other = RollUpCostLines(append(lines, LoadCostLine{Kind: CostKindAnimal, Amount: 0}))
			value = loadPurchaseValue(animal, transport, other)
		}
		if value == nil || *value != 1000 {
			t.Fatalf("cost kind %q contributes %v to purchase value, want 1000 — a kind that reaches no bucket understates landed cost", kind, value)
		}
	}
}

// TestUnknownCostKindIsCountedNotDropped: a cost nobody has classified is still money the farm
// spent. Dropping it would understate purchase value in exactly the way this change exists to fix,
// so an unrecognised kind lands in "other" rather than vanishing.
func TestUnknownCostKindIsCountedNotDropped(t *testing.T) {
	_, _, other := RollUpCostLines([]LoadCostLine{{Kind: "insurance", Amount: 4200}})
	if other == nil || *other != 4200 {
		t.Fatalf("unknown cost kind produced other=%v, want it counted as 4200", other)
	}
}

// TestEmptyBucketsAreAbsentNotZero: nil means NOT RECORDED and zero asserts the farm spent
// nothing. Collapsing the two would make a load with no transport line claim it was transported
// free of charge, and — worse — would let a load with no lines at all read as fully costed at
// zero, erasing every hand-entered cost when the roll-up ran.
func TestEmptyBucketsAreAbsentNotZero(t *testing.T) {
	animal, transport, other := RollUpCostLines(nil)
	if animal != nil || transport != nil || other != nil {
		t.Fatalf("no lines produced %v/%v/%v, want three nils (not recorded)", animal, transport, other)
	}

	// A recorded ZERO is a real fact and must survive as zero, not become "not recorded".
	_, transport, _ = RollUpCostLines([]LoadCostLine{{Kind: CostKindTransport, Amount: 0}})
	if transport == nil || *transport != 0 {
		t.Fatalf("a recorded zero transport came back %v, want 0", transport)
	}
}

// --- Adversarial grain tests for the load-wise money read (2026-09-01) -------------------------
//
// The read gained two cost facts and an itemisation. These four cover the grain classes an
// aggregate over money must not get wrong: one-to-many fan-out, page-vs-whole totals, per-row
// scope, and disjoint outcome buckets.

// TestLoadwiseOneToManyCostLinesDoNotMultiplyTheLoad is the fan-out case. A load's cost
// ITEMISATION is 1:N — five cost events on one load — and the classic defect would be joining
// those lines into the load query, multiplying purchased counts and money by the number of lines.
// The lines are attached in Go by load_id instead, so the row stays one row: this asserts that
// carrying many lines changes NO count and NO figure.
func TestLoadwiseOneToManyCostLinesDoNotMultiplyTheLoad(t *testing.T) {
	base := func(lines []LoadCostLine) LoadwiseSales {
		animal, transport, other := 502000.0, 19000.0, 7900.0
		weight := 1225.0
		return FinalizeLoadwise([]LoadwiseLoad{{
			LoadID: "load-131", DeclaredCount: 63, Purchased: 63, Remaining: 63,
			AnimalCost: &animal, TransportCost: &transport, OtherCost: &other,
			PurchaseWeightKg: &weight, CostLines: lines,
		}}, 1, nil, testAsOf)
	}

	bare := base(nil)
	itemised := base([]LoadCostLine{
		{Kind: CostKindAnimal, Amount: 502000},
		{Kind: CostKindTransport, Amount: 19000},
		{Kind: CostKindLabour, Amount: 5400},
		{Kind: CostKindTransit, Amount: 2500},
		{Kind: CostKindBooking, Amount: 0},
	})

	if len(itemised.Loads) != 1 {
		t.Fatalf("five cost lines produced %d rows, want 1 — the itemisation must not fan the load out", len(itemised.Loads))
	}
	if itemised.Summary.Purchased != bare.Summary.Purchased {
		t.Fatalf("purchased = %d with lines vs %d without; a 1:N attach multiplied the count",
			itemised.Summary.Purchased, bare.Summary.Purchased)
	}
	if itemised.Summary.PurchaseValue != bare.Summary.PurchaseValue {
		t.Fatalf("purchase value = %v with lines vs %v without; a 1:N attach multiplied the money",
			itemised.Summary.PurchaseValue, bare.Summary.PurchaseValue)
	}
	// And the per-kg figure must be the load's own, not one divided by a multiplied weight.
	if got := itemised.Loads[0].LandedPricePerKg; got == nil || int(*got*100) != 43175 {
		t.Fatalf("landed price per kg = %v, want 528900/1225 = 431.75...", got)
	}
}

// TestLoadwisePaginationKeepsWholeTenantTotalDistinct: the summary is over the SERVED rows, while
// TotalLoads is the whole-tenant count. Collapsing the two would make "8 / 8 recorded costs" mean
// "8 of this page" on a tenant with 60 loads — a page-local number presented as business truth.
func TestLoadwisePaginationKeepsWholeTenantTotalDistinct(t *testing.T) {
	animal := 100000.0
	page := []LoadwiseLoad{
		{LoadID: "a", DeclaredCount: 10, Purchased: 10, Remaining: 10, AnimalCost: &animal},
		{LoadID: "b", DeclaredCount: 20, Purchased: 20, Remaining: 20},
	}
	out := FinalizeLoadwise(page, 57, nil, testAsOf)

	if out.TotalLoads != 57 {
		t.Fatalf("TotalLoads = %d, want the whole-tenant 57 — never the page size", out.TotalLoads)
	}
	if out.Summary.Purchased != 30 {
		t.Fatalf("summary purchased = %d, want 30 over the served rows", out.Summary.Purchased)
	}
	// CostedLoads counts served loads WITH a cost, so the "n / m recorded costs" line cannot
	// imply the whole tenant is costed.
	if out.Summary.CostedLoads != 1 {
		t.Fatalf("costed loads = %d, want 1 of the 2 served", out.Summary.CostedLoads)
	}
}

// TestLoadwiseParkScopeStaysWithItsOwnLoad: farm is resolved per load, agree-or-go-bare, and one
// load's park must never leak onto another's row when they are summarised together.
func TestLoadwiseParkScopeStaysWithItsOwnLoad(t *testing.T) {
	out := FinalizeLoadwise([]LoadwiseLoad{
		{LoadID: "cpt", Farm: "CPT", DeclaredCount: 5, Purchased: 5, Remaining: 5},
		{LoadID: "cbe", Farm: "CBE", DeclaredCount: 7, Purchased: 7, Remaining: 7},
		// A load whose animals span parks reports NO farm rather than a majority pick.
		{LoadID: "mixed", Farm: "", DeclaredCount: 3, Purchased: 3, Remaining: 3},
	}, 3, nil, testAsOf)

	for _, want := range []struct{ id, farm string }{{"cpt", "CPT"}, {"cbe", "CBE"}, {"mixed", ""}} {
		var got string
		for _, load := range out.Loads {
			if load.LoadID == want.id {
				got = load.Farm
			}
		}
		if got != want.farm {
			t.Fatalf("load %s farm = %q, want %q", want.id, got, want.farm)
		}
	}
}

// TestLoadwiseStatusBucketsStayDisjoint: sold / mortality / other exits / remaining / unaccounted
// partition the load exactly once each. Overlapping them would let one animal be counted twice and
// make Unaccounted — the column that exists to surface a real discrepancy — arithmetically
// impossible to trust.
func TestLoadwiseStatusBucketsStayDisjoint(t *testing.T) {
	out := FinalizeLoadwise([]LoadwiseLoad{{
		LoadID: "l", DeclaredCount: 70, Purchased: 70,
		Sold: 40, Mortality: 4, OtherExits: 2, Remaining: 20,
	}}, 1, nil, testAsOf)

	row := out.Loads[0]
	if row.Unaccounted != 70-40-4-2-20 {
		t.Fatalf("unaccounted = %d, want %d — the buckets must subtract from the declared size exactly once each",
			row.Unaccounted, 70-40-4-2-20)
	}
	if sum := row.Sold + row.Mortality + row.OtherExits + row.Remaining + row.Unaccounted; sum != row.Purchased {
		t.Fatalf("buckets sum to %d against purchased %d — they are not disjoint", sum, row.Purchased)
	}
}

// TestSaleWeightAverageUsesOnlyTheWeighedAnimals is the grain regression for the 2026-09-01
// growth charts. Load 101 really sold 66 animals but only 26 of them were weighed on the way out,
// so the average must divide by 26, not 66.
//
// This is the classic ratio defect: numerator and denominator drawn from different key sets. With
// the wrong denominator the screen reports a 13.6 kg sale animal against an 18.9 kg purchase
// animal — the farm appears to have SHRUNK its stock over 204 days of fattening, which is a
// conclusion drawn entirely from a missing-data artefact.
func TestSaleWeightAverageUsesOnlyTheWeighedAnimals(t *testing.T) {
	weight, purchaseWeight := 882.58, 1320.0
	weighed := 26
	out := FinalizeLoadwise([]LoadwiseLoad{{
		LoadID: "load-101", DeclaredCount: 70, Purchased: 70, Sold: 66, Remaining: 3,
		PurchaseWeightKg: &purchaseWeight,
		SoldWeightKg:     &weight,
		// Fewer than Sold on purpose — that is the whole point.
		SoldWeighedAnimals: &weighed,
	}}, 1, nil, testAsOf)

	row := out.Loads[0]
	if row.AvgSaleWeightKg == nil {
		t.Fatal("avg sale weight absent; the load has a weight and a weighed count")
	}
	if got := *row.AvgSaleWeightKg; got < 33.9 || got > 34.0 {
		t.Fatalf("avg sale weight = %.2f, want 882.58/26 = 33.9 — dividing by the 66 SOLD gives 13.4 and invents a shrinking animal", got)
	}
	// The purchase side divides by the animals the load brought in.
	if row.AvgPurchaseWeightKg == nil || int(*row.AvgPurchaseWeightKg*10) != 188 {
		t.Fatalf("avg purchase weight = %v, want 1320/70 = 18.8...", row.AvgPurchaseWeightKg)
	}
}

// TestSalePricePerKgDividesOneSetOfSales: value and weight must come from the SAME weighed rows.
// sold_weighed_value exists precisely so the ratio is not the load's full sold value (a different
// legacy source the maintainer kept) over a partial weight, which would inflate price per kg on
// every load whose sales were not all weighed.
func TestSalePricePerKgDividesOneSetOfSales(t *testing.T) {
	weight, value := 882.58, 372354.99
	weighed := 26
	// SoldValue is the OTHER source's figure and must not reach the ratio.
	out := FinalizeLoadwise([]LoadwiseLoad{{
		LoadID: "l", DeclaredCount: 70, Purchased: 70, Sold: 66, SoldValue: 1118399,
		SoldWeightKg: &weight, SoldWeighedAnimals: &weighed, SoldWeighedValue: &value,
	}}, 1, nil, testAsOf)

	row := out.Loads[0]
	if row.SalePricePerKg == nil {
		t.Fatal("sale price per kg absent")
	}
	if got := *row.SalePricePerKg; got < 421.8 || got > 422.0 {
		t.Fatalf("sale price per kg = %.2f, want 372354.99/882.58 = 421.89 — using the %v sold value gives 1267 and prices a kg nobody sold",
			got, row.SoldValue)
	}
}

// TestUnsoldLoadHasNoSaleFiguresAtAll: a load that has sold nothing must report absence, never
// zero. A 0 kg sale animal or a ₹0/kg sale price would be drawn as a real bar beside the purchase
// bar and read as "these animals are worthless", when the truth is simply that they have not been
// sold yet.
func TestUnsoldLoadHasNoSaleFiguresAtAll(t *testing.T) {
	purchaseWeight := 1225.0
	animal := 502000.0
	out := FinalizeLoadwise([]LoadwiseLoad{{
		LoadID: "load-131", DeclaredCount: 63, Purchased: 63, Remaining: 63,
		PurchaseWeightKg: &purchaseWeight, AnimalCost: &animal,
	}}, 1, nil, testAsOf)

	row := out.Loads[0]
	if row.AvgSaleWeightKg != nil {
		t.Fatalf("unsold load reported an avg sale weight of %v", *row.AvgSaleWeightKg)
	}
	if row.SalePricePerKg != nil {
		t.Fatalf("unsold load reported a sale price per kg of %v", *row.SalePricePerKg)
	}
	// The purchase half is still fully reported — absence on one side must not blank the other.
	if row.AvgPurchaseWeightKg == nil || row.LandedPricePerKg == nil {
		t.Fatal("the purchase side went absent along with the sale side")
	}
}
