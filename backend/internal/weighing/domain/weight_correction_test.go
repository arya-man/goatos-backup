package domain

import (
	"math"
	"testing"
)

// The verifier's weight correction is a WRITE OVER AN OPERATOR'S RECORD, so every
// refusal here is load-bearing: a value this validator lets through overwrites a
// real weight and then flows into every average, ADG series and export that reads
// the row. These pin the rules that keep it honest.

func TestIndividualCorrectionRefusesAHeadCount(t *testing.T) {
	// An individual capture weighs exactly ONE animal, so a head count on it is a
	// value with no meaning. Refusing it beats ignoring it: a client that sent one
	// believes a correction landed that never did.
	_, code := ValidateWeightCorrection(WeightCorrectionCommand{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		ObservationID:  "22222222-2222-2222-2222-222222222222",
		RefType:        VerificationRefTypeAnimal,
		WeightKg:       28.5,
		AnimalCount:    31,
		CorrectedBy:    "33333333-3333-3333-3333-333333333333",
		IdempotencyKey: "k",
	})
	if code != "animal_count_not_applicable" {
		t.Fatalf("individual correction carrying a head count must be refused, got %q", code)
	}
}

// Maintainer decision 2026-08-24: the lump-sum head count is snapshotted from
// the herd register at submit and FROZEN, so a correction naming one is refused
// on BOTH grains — never silently dropped — while a weight-only correction on
// either grain stays valid.
func TestCorrectionRefusesAHeadCountOnBothGrainsAndAcceptsWeightOnly(t *testing.T) {
	base := WeightCorrectionCommand{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		ObservationID:  "22222222-2222-2222-2222-222222222222",
		WeightKg:       732,
		CorrectedBy:    "33333333-3333-3333-3333-333333333333",
		IdempotencyKey: "k",
	}
	lumpWithCount := base
	lumpWithCount.RefType = VerificationRefTypeShed
	lumpWithCount.AnimalCount = 31
	if _, code := ValidateWeightCorrection(lumpWithCount); code != "animal_count_not_applicable" {
		t.Fatalf("lump-sum correction with a head count must be refused (frozen census snapshot), got %q", code)
	}

	lump := base
	lump.RefType = VerificationRefTypeShed
	if _, code := ValidateWeightCorrection(lump); code != "" {
		t.Fatalf("lump-sum weight-only correction must be accepted, refused as %q", code)
	}

	individual := base
	individual.RefType = VerificationRefTypeAnimal
	individual.WeightKg = 28.5
	if _, code := ValidateWeightCorrection(individual); code != "" {
		t.Fatalf("individual correction with no head count must be accepted, refused as %q", code)
	}
}

func TestCorrectionRefusesOutOfRangeAndNonFiniteWeights(t *testing.T) {
	// NaN and ±Inf are the case a naive range check misses entirely: every
	// comparison against NaN is false, so `< min || > max` passes them straight
	// through and Postgres stores a weight that breaks every average that reads it.
	for name, weight := range map[string]float64{
		"zero":     0,
		"negative": -5,
		"absurd":   MaxCorrectableWeightKg + 1,
		"nan":      math.NaN(),
		"+inf":     math.Inf(1),
		"-inf":     math.Inf(-1),
	} {
		t.Run(name, func(t *testing.T) {
			_, code := ValidateWeightCorrection(WeightCorrectionCommand{
				TenantID:       "11111111-1111-1111-1111-111111111111",
				ObservationID:  "22222222-2222-2222-2222-222222222222",
				RefType:        VerificationRefTypeAnimal,
				WeightKg:       weight,
				CorrectedBy:    "33333333-3333-3333-3333-333333333333",
				IdempotencyKey: "k",
			})
			if code != "weight_out_of_range" {
				t.Fatalf("weight %v must be refused as out of range, got %q", weight, code)
			}
		})
	}
}

func TestCorrectionRoundsToTheStoredScaleBeforeJudgingTheFloor(t *testing.T) {
	// The columns store three decimals. A value is judged on the number that would
	// actually be STORED, so a fourth-decimal input cannot be accepted here and then
	// round to something the column's own (> 0) CHECK rejects.
	cmd, code := ValidateWeightCorrection(WeightCorrectionCommand{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		ObservationID:  "22222222-2222-2222-2222-222222222222",
		RefType:        VerificationRefTypeAnimal,
		WeightKg:       28.50049,
		CorrectedBy:    "33333333-3333-3333-3333-333333333333",
		IdempotencyKey: "k",
	})
	if code != "" {
		t.Fatalf("a normal weight must be accepted, refused as %q", code)
	}
	if cmd.WeightKg != 28.5 {
		t.Fatalf("weight must be rounded to the stored scale, got %v", cmd.WeightKg)
	}

	// A value that only exists below the stored scale rounds to zero, which is not a
	// weight at all.
	if _, code := ValidateWeightCorrection(WeightCorrectionCommand{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		ObservationID:  "22222222-2222-2222-2222-222222222222",
		RefType:        VerificationRefTypeAnimal,
		WeightKg:       0.0004,
		CorrectedBy:    "33333333-3333-3333-3333-333333333333",
		IdempotencyKey: "k",
	}); code != "weight_out_of_range" {
		t.Fatalf("a sub-gram weight rounds to zero and must be refused, got %q", code)
	}
}

func TestRecomputedAverageFollowsTheCorrectedTotal(t *testing.T) {
	// average_weight_kg is a STORED column. Leaving it at the pre-correction value
	// would make a shed's average disagree with the shed's own total -- two numbers
	// for one fact, on two different screens.
	if got := RecomputeAverageWeightKg(732, 31); got != 23.613 {
		t.Fatalf("average must follow the corrected total, got %v", got)
	}
	// A non-positive count has no average, and the column's own (> 0) CHECK would
	// reject one. Zero is the signal the caller must not write it.
	if got := RecomputeAverageWeightKg(732, 0); got != 0 {
		t.Fatalf("a zero head count has no average, got %v", got)
	}
}

func TestCorrectedLabelRestatesTheWeightAtBothGrains(t *testing.T) {
	// The label is what the verifier READS while she decides. If it is not
	// recomposed after a correction, she sees the number she just replaced and has
	// no way to tell whether her correction landed.
	lump := CorrectedSubjectLabel(VerificationRefTypeShed, "Godel 1 - Part 3", "", 732, 31)
	if lump != "Godel 1 - Part 3 · 732.0 kg · 31 goats" {
		t.Fatalf("lump-sum label must name shed, corrected total and count, got %q", lump)
	}
	individual := CorrectedSubjectLabel(VerificationRefTypeAnimal, "Castro 2", "9010123", 28.5, 0)
	if individual != "Castro 2 · Tag 9010123 · 28.5 kg" {
		t.Fatalf("individual label must name shed, tag and corrected weight, got %q", individual)
	}
	// A bucket whose shed lookup found nothing degrades to the shed-less form rather
	// than printing a UUID or a dangling separator (LOCKED SPEC section 5).
	if bare := CorrectedSubjectLabel(VerificationRefTypeShed, "", "", 732, 0); bare != "Whole pen · 732.0 kg" {
		t.Fatalf("a shed-less lump-sum label must degrade, got %q", bare)
	}
}

func TestCorrectionRequiresItsIdentityFields(t *testing.T) {
	// Each of these is a distinct code because each has a distinct remedy on screen.
	for code, mutate := range map[string]func(*WeightCorrectionCommand){
		"missing_tenant":          func(c *WeightCorrectionCommand) { c.TenantID = "  " },
		"missing_observation":     func(c *WeightCorrectionCommand) { c.ObservationID = "" },
		"missing_verifier":        func(c *WeightCorrectionCommand) { c.CorrectedBy = "" },
		"missing_idempotency_key": func(c *WeightCorrectionCommand) { c.IdempotencyKey = "" },
		"invalid_ref_type":        func(c *WeightCorrectionCommand) { c.RefType = "vaccination_submission" },
	} {
		t.Run(code, func(t *testing.T) {
			cmd := WeightCorrectionCommand{
				TenantID:       "11111111-1111-1111-1111-111111111111",
				ObservationID:  "22222222-2222-2222-2222-222222222222",
				RefType:        VerificationRefTypeAnimal,
				WeightKg:       28.5,
				CorrectedBy:    "33333333-3333-3333-3333-333333333333",
				IdempotencyKey: "k",
			}
			mutate(&cmd)
			if _, got := ValidateWeightCorrection(cmd); got != code {
				t.Fatalf("expected refusal %q, got %q", code, got)
			}
		})
	}
}
