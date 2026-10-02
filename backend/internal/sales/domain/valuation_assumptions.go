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
// sale-ready weight line -- is one row per tenant, edited on Sales Config, re-read per request. The
// same ₹/kg values the animals Load wise still holds on farm (docs/decisions/loadwise-stock-valuation.md).
// The bucket KEYS are the classification the valuation SQL files animals into; everything about a
// bucket's pricing is authored.
//
// PRICED BY SPECIES AS WELL AS STAGE AND GENDER (maintainer decision 2026-10-02): a goat and a
// sheep in the same stage fetch different rupees per kg (₹450 vs ₹430 for fattening on the day this
// was written), so every stage carries four rows -- goat female, goat male, sheep female, sheep
// male -- keyed `<stage>_<species>_<gender>`. The stages themselves stay one list for both species.

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
	// four rows per stage (species x gender); the stage list is what says which four.
	Stages  []ValuationStage
	Buckets []ValuationBucketRate
	// UnsoldStockPriceRupees is RETIRED (2026-10-02): Load wise values unsold animals by weight x
	// the bucket ₹/kg and reads nothing from it. The column stays so an older client's write is
	// still accepted; the current screen sends it blank, clearing it.
	UnsoldStockPriceRupees *float64
	RowVersion             int
	UpdatedAt              string
	UpdatedByName          string
}

// SeededValuationStages is what the six hard-coded stages MEANT, written out as authored rows --
// the translation migration 000425 applies to every farm, and what a farm with no row reads. Each
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

// ValuationSpecies is the species half, in display order. These are the two values the herd
// register's `goats.species` carries; an animal of any other species is not valued, and is shown.
var ValuationSpecies = []struct{ Key, Label string }{
	{"goat", "Goat"},
	{"sheep", "Sheep"},
}

// ValuationBucketKey joins the three halves the one way every surface must join them; the SQL
// mirror is farmvaluation.BucketKeySQL.
func ValuationBucketKey(stage, species, gender string) string {
	return stage + "_" + species + "_" + gender
}

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
	return ValuationAssumptions{
		// COPIED, never shared. SeededValuationStages is a package-level slice: handing it out and
		// having one caller append a stage or rename a label would rewrite what every OTHER farm
		// with no authored row reads, and the translation migration's own oracle with it.
		Stages: copyValuationStages(SeededValuationStages),
		// The figures the farm carried before the gender and species splits, each stage's single
		// rate becoming its four rows -- so a farm that has not touched this screen values its herd
		// at exactly what it valued yesterday, and then edits the rows that really differ.
		Buckets: defaultBuckets(),
	}
}

// seededStageRates is each seeded stage's (fixed weight, ₹/kg) before any split. A nil weight is
// "price at the measured weight" (fattening).
var seededStageRates = map[string]struct {
	weight *float64
	price  float64
}{
	"fattening": {nil, 450},
	"adult":     {nil, 0}, // per gender below
	"K0":        {seededKg(3), 500},
	"K1":        {seededKg(3), 500},
	"K2":        {seededKg(8), 500},
	"K3":        {seededKg(15), 500},
}

func seededKg(v float64) *float64 { return &v }

// defaultBuckets expands the seeded stages over species and gender in screen order. Adults keep
// the farm's two figures: a female at 40 kg and ₹600, a male at 60 kg and ₹500.
func defaultBuckets() []ValuationBucketRate {
	out := []ValuationBucketRate{}
	for _, st := range SeededValuationStages {
		for _, sp := range ValuationSpecies {
			for _, g := range ValuationGenders {
				r := seededStageRates[st.Stage]
				weight, price := r.weight, r.price
				if st.Stage == "adult" {
					if g.Key == "female" {
						weight, price = seededKg(40), 600
					} else {
						weight, price = seededKg(60), 500
					}
				}
				out = append(out, ValuationBucketRate{
					Bucket:        ValuationBucketKey(st.Stage, sp.Key, g.Key),
					Label:         BucketLabel(st.Label, sp.Label, g.Label),
					FixedWeightKg: weight,
					PricePerKg:    price,
					DisplayOrder:  len(out) + 1,
				})
			}
		}
	}
	return out
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
