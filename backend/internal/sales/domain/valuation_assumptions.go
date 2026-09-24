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
	// Stages is the authored list of stages the herd is valued in (2026-09-24). Buckets carries
	// two rows per stage; the stage list is what says which two.
	Stages  []ValuationStage
	Buckets []ValuationBucketRate
	// UnsoldStockPriceRupees nil keeps Load wise on the overall average sold price; a figure
	// prices every unsold animal at it instead.
	UnsoldStockPriceRupees *float64
	RowVersion             int
	UpdatedAt              string
	UpdatedByName          string
}

// SeededValuationStages is what the six hard-coded stages MEANT, written out as authored rows --
// the translation migration 000405 applies to every farm, and what a farm with no row reads. Each
// row's matches are the register entries the retired SQL CASE filed into that bucket:
//
//	fattening  F2, F2-Male, F2-Female  -- the three spellings the register carries for fattening
//	adult      every register entry the herd calls an adult, plus a plain ICU tag, which is one
//	K0..K3     their own codes; ICU-Kid joins K2, the maintainer's stated default for a kid whose
//	           milk band was lost when it moved to ICU (2026-09-10)
//
// Two rules the CASE carried are deliberately NOT reproduced, and both are recorded rather than
// lost. `age_band = 'adult'` was a catch-all for an adult stage nobody had named; it is replaced by
// naming them, because a catch-all is the thing that made Warmup invisible -- a stage the farm adds
// tomorrow must appear in the not-valued list asking to be priced, not be swallowed by a band. And
// `Mother` was forced onto the female row whatever the sex column said; every Mother on this farm
// is recorded female, so it moved nothing, and a Mother the register calls male is now a register
// error that shows as one instead of being silently corrected inside a valuation.
var SeededValuationStages = []ValuationStage{
	{Stage: "fattening", Label: "Fattening", DisplayOrder: 1, Matches: []string{"F2", "F2-Male", "F2-Female"}},
	{Stage: "adult", Label: "Adult", DisplayOrder: 2, Matches: []string{"Buck", "Mother", "Milking", "M0", "Pregnant", "Non-Pregnant", "ICU"}},
	{Stage: "K0", Label: "K0", DisplayOrder: 3, Matches: []string{"K0"}},
	{Stage: "K1", Label: "K1", DisplayOrder: 4, Matches: []string{"K1"}},
	{Stage: "K2", Label: "K2", DisplayOrder: 5, Matches: []string{"K2", "ICU-Kid"}},
	{Stage: "K3", Label: "K3", DisplayOrder: 6, Matches: []string{"K3"}},
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
	if err := validateValuationStages(v, bad); err != nil {
		return err
	}
	// The bucket set is no longer a constant: it is every authored stage against both genders, so
	// a farm that adds a stage must price it and one that removes a stage must stop pricing it.
	expected := BucketKeysForStages(v.Stages)
	if len(v.Buckets) != len(expected) {
		return bad("buckets: expected %d buckets, got %d", len(expected), len(v.Buckets))
	}
	seen := map[string]bool{}
	for i, b := range v.Buckets {
		known := false
		for _, k := range expected {
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
		// COPIED, never shared. SeededValuationStages is a package-level slice: handing it out and
		// having one caller append a stage or rename a label would rewrite what every OTHER farm
		// with no authored row reads, and the translation migration's own oracle with it.
		Stages: copyValuationStages(SeededValuationStages),
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

// copyValuationStages deep-copies the stage rows, matches and all -- a shallow copy still shares
// every row's Matches slice, which is the half a caller is most likely to append to.
func copyValuationStages(in []ValuationStage) []ValuationStage {
	out := make([]ValuationStage, len(in))
	for i, s := range in {
		out[i] = s
		out[i].Matches = append([]string(nil), s.Matches...)
	}
	return out
}
