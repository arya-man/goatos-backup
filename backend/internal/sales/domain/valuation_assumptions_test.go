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
		"expected 24 buckets":       func(v *ValuationAssumptions) { v.Buckets = v.Buckets[1:] },
		"listed twice":              func(v *ValuationAssumptions) { v.Buckets[6].Bucket = "K0_goat_male" },
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
	// The stage list is the farm's now (2026-09-24), so the closed set is derived from it rather
	// than fixed; what must hold is that it is still CLOSED and still covers both genders.
	stages := SeededValuationStages
	keys := BucketKeysForStages(stages)
	if want := len(stages) * len(ValuationSpecies) * len(ValuationGenders); len(keys) != want {
		t.Fatalf("expected %d buckets, got %d", want, len(keys))
	}
	seen := map[string]bool{}
	for _, stage := range stages {
		for _, species := range ValuationSpecies {
			for _, gender := range ValuationGenders {
				key := ValuationBucketKey(stage.Stage, species.Key, gender.Key)
				found := false
				for _, k := range keys {
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
	}
	for _, k := range keys {
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
	for _, stage := range []string{"fattening_goat", "fattening_sheep", "K0_goat", "K0_sheep", "K1_goat", "K1_sheep", "K2_goat", "K2_sheep", "K3_goat", "K3_sheep"} {
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
	if byKey["adult_goat_female"].PricePerKg == byKey["adult_goat_male"].PricePerKg {
		t.Fatal("adults were priced apart before the split and must stay so")
	}
}

// SPECIES SPLIT (maintainer decision 2026-10-02): a goat and a sheep in the same stage carry their
// own ₹/kg. The split starts from the one figure each stage already had, so nothing moves until the
// farm edits it, and a write that prices one species but not the other is refused.
func TestEveryStageIsPricedBySpeciesAndStartsFromTheFarmsFigure(t *testing.T) {
	byKey := map[string]ValuationBucketRate{}
	for _, b := range DefaultValuationAssumptions().Buckets {
		byKey[b.Bucket] = b
	}
	for _, st := range SeededValuationStages {
		for _, g := range ValuationGenders {
			goat, sheep := byKey[ValuationBucketKey(st.Stage, "goat", g.Key)], byKey[ValuationBucketKey(st.Stage, "sheep", g.Key)]
			if goat.Bucket == "" || sheep.Bucket == "" {
				t.Fatalf("%s %s is missing a species row", st.Stage, g.Key)
			}
			if goat.PricePerKg != sheep.PricePerKg {
				t.Fatalf("%s %s starts split at one rate, got goat %v sheep %v", st.Stage, g.Key, goat.PricePerKg, sheep.PricePerKg)
			}
		}
	}
	if got := byKey["fattening_sheep_female"].Label; got != "Fattening · Sheep · Female" {
		t.Fatalf("label = %q", got)
	}
	v := DefaultValuationAssumptions()
	kept := v.Buckets[:0]
	for _, b := range v.Buckets {
		if b.Bucket != "fattening_sheep_male" {
			kept = append(kept, b)
		}
	}
	v.Buckets = kept
	if err := ValidateValuationAssumptions(v); err == nil {
		t.Fatal("a write missing one species row must be refused")
	}
}
