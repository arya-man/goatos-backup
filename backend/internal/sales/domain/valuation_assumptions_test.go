package domain

import (
	"errors"
	"strings"
	"testing"
)

// The seeded defaults validate; a figure outside its band, a missing bucket, a duplicate bucket and
// an unknown bucket are each refused naming the field (never clamped).
func TestValidateValuationAssumptions(t *testing.T) {
	v := DefaultValuationAssumptions()
	if err := ValidateValuationAssumptions(v); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	cases := map[string]func(v *ValuationAssumptions){
		"buckets[1].price_per_kg": func(v *ValuationAssumptions) { v.Buckets[1].PricePerKg = 0 },
		"buckets[3].fixed_weight_kg": func(v *ValuationAssumptions) {
			w := 500.0
			v.Buckets[3].FixedWeightKg = &w
		},
		"sale_ready_kg":             func(v *ValuationAssumptions) { v.SaleReadyKg = 1 },
		"unsold_stock_price_rupees": func(v *ValuationAssumptions) { p := 5.0; v.UnsoldStockPriceRupees = &p },
		"expected 7 buckets":        func(v *ValuationAssumptions) { v.Buckets = v.Buckets[1:] },
		"listed twice":              func(v *ValuationAssumptions) { v.Buckets[6].Bucket = "K2" },
		"not a valuation bucket":    func(v *ValuationAssumptions) { v.Buckets[0].Bucket = "kids" },
		"buckets[2].label":          func(v *ValuationAssumptions) { v.Buckets[2].Label = " " },
	}
	for want, mutate := range cases {
		v := DefaultValuationAssumptions()
		mutate(&v)
		err := ValidateValuationAssumptions(v)
		if !errors.Is(err, ErrValuationInvalid) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v", want, err)
		}
	}
	// A blank fixed weight on any bucket is "measured weight", never an error.
	v = DefaultValuationAssumptions()
	v.Buckets[1].FixedWeightKg = nil
	if err := ValidateValuationAssumptions(v); err != nil {
		t.Fatalf("blank weight = measured: %v", err)
	}
}
