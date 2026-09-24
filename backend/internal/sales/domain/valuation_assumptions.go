package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// FARM VALUATION ASSUMPTIONS (maintainer instruction 2026-09-19, docs/decisions/
// sales-valuation-assumptions.md). Every figure the live herd is VALUED at that someone decided
// rather than measured -- the per-bucket fixed weights and rupees per kg behind Farm value, the
// sale-ready weight line, and the price every unsold animal on Load wise is carried at -- is one
// row per tenant, edited on Sales Config, re-read per request. The bucket KEYS are the
// classification the valuation SQL files animals into and are fixed; everything about a bucket's
// pricing is authored.

// ValuationBucketRate is one row of the valuation formula as authored.
type ValuationBucketRate struct {
	Bucket string `json:"bucket"`
	Label  string `json:"label"`
	// FixedWeightKg nil = "price at the measured weight" (the fattening bucket's rule).
	FixedWeightKg *float64 `json:"fixed_weight_kg"`
	PricePerKg    float64  `json:"price_per_kg"`
	DisplayOrder  int      `json:"display_order"`
}

// ValuationAssumptions is the tenant's row.
type ValuationAssumptions struct {
	Buckets []ValuationBucketRate
	// UnsoldStockPriceRupees nil keeps Load wise on the overall average sold price; a figure
	// prices every unsold animal at it instead.
	UnsoldStockPriceRupees *float64
	RowVersion             int
	UpdatedAt              string
	UpdatedByName          string
}

// ValuationBucketKeys is the closed set of buckets the valuation SQL classifies into, in display
// order. A write must carry exactly these, once each: a missing bucket would silently value that
// slice of the herd at nothing, an extra one would never match an animal.
//
// EVERY STAGE IS SPLIT BY GENDER (maintainer instruction 2026-09-23: "we have different price at
// different genders"). The adults always were -- a buck is not worth what a doe is -- and the rest
// now say so too: a male kid can be carried at its own weight AND its own rate, which one figure
// per stage could not express.
//
// An animal whose gender is NOT recorded is valued on the FEMALE row (maintainer decision, same
// day). Females are the larger share of this herd, so it is the closer guess; it is a guess all
// the same, and `sex_missing` on each bucket is what makes it visible rather than silent.
var ValuationBucketKeys = []string{
	"fattening_female", "fattening_male",
	"adult_female", "adult_male",
	"K0_female", "K0_male",
	"K1_female", "K1_male",
	"K2_female", "K2_male",
	"K3_female", "K3_male",
}

// ValuationStages is the stage half of a bucket key, in display order, with the words for it.
// The SQL classifies an animal into one of these and then appends its gender.
var ValuationStages = []struct{ Key, Label string }{
	{"fattening", "Fattening"},
	{"adult", "Adult"},
	{"K0", "K0"},
	{"K1", "K1"},
	{"K2", "K2"},
	{"K3", "K3"},
}

// ValuationGenders is the gender half, in display order.
var ValuationGenders = []struct{ Key, Label string }{
	{"female", "Female"},
	{"male", "Male"},
}

// ValuationBucketKey joins the two halves the one way every surface must join them.
func ValuationBucketKey(stage, gender string) string { return stage + "_" + gender }

// Business bands. Outside them the write is REFUSED, never clamped: a typo of 5000 rupees per kg
// or a 0.1 kg adult is not a figure to silently fix.
const (
	ValuationPriceMinINR  = 1.0
	ValuationPriceMaxINR  = 10000.0
	ValuationWeightMinKg  = 0.5
	ValuationWeightMaxKg  = 200.0
	ValuationUnsoldMinINR = 100.0
	ValuationUnsoldMaxINR = 1000000.0
)

// ErrValuationInvalid is a write outside the bands or missing a bucket; the message names the field.
var ErrValuationInvalid = errors.New("sales: valuation assumptions invalid")

// ValidateValuationAssumptions checks a write before it is stored.
func ValidateValuationAssumptions(v ValuationAssumptions) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrValuationInvalid, fmt.Sprintf(format, args...))
	}
	if len(v.Buckets) != len(ValuationBucketKeys) {
		return bad("buckets: expected %d buckets, got %d", len(ValuationBucketKeys), len(v.Buckets))
	}
	seen := map[string]bool{}
	for i, b := range v.Buckets {
		known := false
		for _, k := range ValuationBucketKeys {
			if k == b.Bucket {
				known = true
			}
		}
		if !known {
			return bad("buckets[%d].bucket: %q is not a valuation bucket", i, b.Bucket)
		}
		if seen[b.Bucket] {
			return bad("buckets[%d].bucket: %q is listed twice", i, b.Bucket)
		}
		seen[b.Bucket] = true
		if strings.TrimSpace(b.Label) == "" {
			return bad("buckets[%d].label: required", i)
		}
		if b.PricePerKg < ValuationPriceMinINR || b.PricePerKg > ValuationPriceMaxINR || math.IsNaN(b.PricePerKg) {
			return bad("buckets[%d].price_per_kg: must be between %v and %v rupees per kg", i, ValuationPriceMinINR, ValuationPriceMaxINR)
		}
		if b.FixedWeightKg != nil && (*b.FixedWeightKg < ValuationWeightMinKg || *b.FixedWeightKg > ValuationWeightMaxKg || math.IsNaN(*b.FixedWeightKg)) {
			return bad("buckets[%d].fixed_weight_kg: must be between %v and %v kg, or blank to price at the measured weight", i, ValuationWeightMinKg, ValuationWeightMaxKg)
		}
	}
	if v.UnsoldStockPriceRupees != nil && (*v.UnsoldStockPriceRupees < ValuationUnsoldMinINR || *v.UnsoldStockPriceRupees > ValuationUnsoldMaxINR || math.IsNaN(*v.UnsoldStockPriceRupees)) {
		return bad("unsold_stock_price_rupees: must be between %v and %v rupees, or blank to use the average sold price", ValuationUnsoldMinINR, ValuationUnsoldMaxINR)
	}
	return nil
}

// DefaultValuationAssumptions is the seeded row (migration 000367), what a tenant without a row
// reads and what the overview SQL falls back to; the figures the hard-coded formula carried.
func DefaultValuationAssumptions() ValuationAssumptions {
	kg := func(v float64) *float64 { return &v }
	return ValuationAssumptions{
		// The figures the farm carried before the gender split, each stage's single rate becoming
		// its two rows -- so a farm that has not touched this screen values its herd at exactly
		// what it valued yesterday, and then edits the halves that really differ.
		Buckets: []ValuationBucketRate{
			{Bucket: "fattening_female", Label: "Fattening · Female", FixedWeightKg: nil, PricePerKg: 450, DisplayOrder: 1},
			{Bucket: "fattening_male", Label: "Fattening · Male", FixedWeightKg: nil, PricePerKg: 450, DisplayOrder: 2},
			{Bucket: "adult_female", Label: "Adult · Female", FixedWeightKg: kg(40), PricePerKg: 600, DisplayOrder: 3},
			{Bucket: "adult_male", Label: "Adult · Male", FixedWeightKg: kg(60), PricePerKg: 500, DisplayOrder: 4},
			{Bucket: "K0_female", Label: "K0 · Female", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 5},
			{Bucket: "K0_male", Label: "K0 · Male", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 6},
			{Bucket: "K1_female", Label: "K1 · Female", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 7},
			{Bucket: "K1_male", Label: "K1 · Male", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 8},
			{Bucket: "K2_female", Label: "K2 · Female", FixedWeightKg: kg(8), PricePerKg: 500, DisplayOrder: 9},
			{Bucket: "K2_male", Label: "K2 · Male", FixedWeightKg: kg(8), PricePerKg: 500, DisplayOrder: 10},
			{Bucket: "K3_female", Label: "K3 · Female", FixedWeightKg: kg(15), PricePerKg: 500, DisplayOrder: 11},
			{Bucket: "K3_male", Label: "K3 · Male", FixedWeightKg: kg(15), PricePerKg: 500, DisplayOrder: 12},
		},
	}
}
