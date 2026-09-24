package domain

import (
	"errors"
	"strings"
	"testing"
)

// The seeded defaults validate; a figure outside its band, a missing bucket, a duplicate bucket and
// an unknown bucket are each refused naming the field (never clamped).
//
// The set is every stage BY GENDER since 2026-09-23, which is why the count is twelve.
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
		"unsold_stock_price_rupees": func(v *ValuationAssumptions) { p := 5.0; v.UnsoldStockPriceRupees = &p },
		"expected 12 buckets":       func(v *ValuationAssumptions) { v.Buckets = v.Buckets[1:] },
		"listed twice":              func(v *ValuationAssumptions) { v.Buckets[6].Bucket = "K0_male" },
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

// EVERY STAGE IS PRICED BY GENDER (maintainer instruction 2026-09-23: "we have different price at
// different genders"). The set is the six stages times the two genders, each exactly once -- a
// missing one would value that slice of the herd at nothing, and a stage present for only one
// gender would value half of it.
func TestEveryStageIsPricedByGender(t *testing.T) {
	if len(ValuationBucketKeys) != len(ValuationStages)*len(ValuationGenders) {
		t.Fatalf("expected %d buckets, got %d", len(ValuationStages)*len(ValuationGenders), len(ValuationBucketKeys))
	}
	seen := map[string]bool{}
	for _, stage := range ValuationStages {
		for _, gender := range ValuationGenders {
			key := ValuationBucketKey(stage.Key, gender.Key)
			found := false
			for _, k := range ValuationBucketKeys {
				if k == key {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s is not a valuation bucket", key)
			}
			seen[key] = true
		}
	}
	for _, k := range ValuationBucketKeys {
		if !seen[k] {
			t.Fatalf("%s is a bucket no stage and gender produce", k)
		}
	}
	// And the defaults carry all of them, so a farm that never opens the screen still values its
	// whole herd.
	if err := ValidateValuationAssumptions(DefaultValuationAssumptions()); err != nil {
		t.Fatalf("defaults must carry every bucket: %v", err)
	}
}

// The figures a farm valued its herd at yesterday are what the split starts from: each stage's one
// rate became its two rows, so nothing moves until somebody edits a half that really differs.
func TestTheGenderSplitStartsFromWhatTheFarmAlreadyValued(t *testing.T) {
	byKey := map[string]ValuationBucketRate{}
	for _, b := range DefaultValuationAssumptions().Buckets {
		byKey[b.Bucket] = b
	}
	for _, stage := range []string{"fattening", "K0", "K1", "K2", "K3"} {
		f, m := byKey[stage+"_female"], byKey[stage+"_male"]
		if f.PricePerKg != m.PricePerKg {
			t.Fatalf("%s starts split at one rate, got %v and %v", stage, f.PricePerKg, m.PricePerKg)
		}
		switch {
		case f.FixedWeightKg == nil && m.FixedWeightKg == nil:
		case f.FixedWeightKg != nil && m.FixedWeightKg != nil && *f.FixedWeightKg == *m.FixedWeightKg:
		default:
			t.Fatalf("%s starts split at one weight", stage)
		}
	}
	// The adults were ALREADY priced apart, and keep their own figures.
	if byKey["adult_female"].PricePerKg == byKey["adult_male"].PricePerKg {
		t.Fatal("adults were priced apart before the split and must stay so")
	}
}
