package domain

import (
	"math"
	"testing"
)


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

	out := FinalizeLoadwise(loads, 3, overall)

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
	}, 1, nil)
	row := out.Loads[0]
	if row.PriceBasis != LoadwisePriceBasisNone || row.AvgSoldPrice != nil || row.RemainingValue != nil {
		t.Fatalf("no sales anywhere must yield basis=none and NO estimate, got basis=%s avg=%v remaining=%v",
			row.PriceBasis, row.AvgSoldPrice, row.RemainingValue)
	}
	if out.Summary.RemainingValue != 0 {
		t.Fatalf("summary remaining value = %v, want 0 when no load has a basis", out.Summary.RemainingValue)
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
