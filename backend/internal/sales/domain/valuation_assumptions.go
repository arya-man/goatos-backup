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
var ValuationBucketKeys = []string{"fattening", "adult_female", "adult_male_buck", "K0", "K1", "K2", "K3"}

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
		Buckets: []ValuationBucketRate{
			{Bucket: "fattening", Label: "Fattening animals", FixedWeightKg: nil, PricePerKg: 450, DisplayOrder: 1},
			{Bucket: "adult_female", Label: "Adult females", FixedWeightKg: kg(40), PricePerKg: 600, DisplayOrder: 2},
			{Bucket: "adult_male_buck", Label: "Adult males / bucks", FixedWeightKg: kg(60), PricePerKg: 500, DisplayOrder: 3},
			{Bucket: "K0", Label: "K0", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 4},
			{Bucket: "K1", Label: "K1", FixedWeightKg: kg(3), PricePerKg: 500, DisplayOrder: 5},
			{Bucket: "K2", Label: "K2", FixedWeightKg: kg(8), PricePerKg: 500, DisplayOrder: 6},
			{Bucket: "K3", Label: "K3", FixedWeightKg: kg(15), PricePerKg: 500, DisplayOrder: 7},
		},
	}
}
