package app

import "testing"

// TestCohortStageMapClassifiesByUnderlyingCohortAndNeverFallsBackToAdults pins the maintainer's
// cohort membership rule (2026-08-06).
//
// Adults used to be the LAST rung of a prefix-matching ladder, i.e. the catch-all. Two live defects
// came out of that single design: F2-Female/F2-Male were counted as adults (372 against a true
// 324), and ICU-Kid — a KID carrying a health-state prefix — sat in Adults as well. Anything the
// ladder failed to recognise silently inflated the adult herd.
//
// The rule now: membership is DECLARED, a composite operational label classifies by its UNDERLYING
// cohort (never by its health/arrival prefix), and an unmapped stage is NOT guessed — it renders as
// its own visible row so the CEO can see it and define it.
func TestCohortStageMapClassifiesByUnderlyingCohortAndNeverFallsBackToAdults(t *testing.T) {
	stageMap := map[string]string{}
	for _, group := range pageOptionGroups("vaccination") {
		if group.ID != "command_board_cohort_stage_map" {
			continue
		}
		for _, opt := range group.Options {
			stageMap[opt.Key] = opt.Label
		}
	}
	if len(stageMap) == 0 {
		t.Fatalf("vaccination page contract has no command_board_cohort_stage_map: the matrix would fall " +
			"back to prefix matching and Adults would silently absorb every unrecognised stage")
	}

	adults := map[string]bool{}
	for stage, row := range stageMap {
		if row == "Adults" {
			adults[stage] = true
		}
	}
	wantAdults := []string{"NON-PREGNANT", "BUCK", "MOTHER"}
	for _, stage := range wantAdults {
		if !adults[stage] {
			t.Errorf("stage %q does not map to Adults; Adults is exactly Non-Pregnant, Buck and Mother", stage)
		}
	}

	// A kid in ICU is a kid. An adult in ICU is an adult. The prefix is a health state, not a cohort.
	for stage, wantRow := range map[string]string{
		"ICU-KID":          "Kid",
		"ICU-KIDS":         "Kid",
		"QUARANTINE KIDS":  "Kid",
		"ICU-NON-PREGNANT": "Adults",
	} {
		if got := stageMap[stage]; got != wantRow {
			t.Errorf("stage %q maps to %q, want %q -- a composite label classifies by its UNDERLYING "+
				"cohort, never by the health-state prefix", stage, got, wantRow)
		}
	}

	// F2 is the fattening KID cohort split by sex. It must never land in Adults.
	for _, stage := range []string{"F2-FEMALE", "F2-MALE"} {
		if got := stageMap[stage]; got != "F2" {
			t.Errorf("stage %q maps to %q, want \"F2\" -- F2 animals are kids and counting them as "+
				"adults is what reported 372 adults against a true 324", stage, got)
		}
		if got := stageMap[stage]; got == "Adults" {
			t.Errorf("stage %q is in Adults", stage)
		}
	}

	// Warmup is an arrival/acclimation state, neither adult nor kid by label. It must stay UNMAPPED
	// so the matrix shows it under its own name instead of guessing a cohort for it.
	if row, mapped := stageMap["WARMUP"]; mapped {
		t.Errorf("WARMUP maps to %q, but arrival/acclimation is not a cohort: leave it unmapped so it "+
			"renders as its own visible row until the CEO bucket is defined", row)
	}

	// Every declared target row must exist in the ladder, or a mapped stage would vanish.
	ladder := map[string]bool{}
	for _, group := range pageOptionGroups("vaccination") {
		if group.ID != "command_board_cohort_ladder" {
			continue
		}
		for _, opt := range group.Options {
			ladder[opt.Label] = true
		}
	}
	for stage, row := range stageMap {
		if !ladder[row] {
			t.Errorf("stage %q maps to row %q which is not in the cohort ladder -- those animals would "+
				"disappear from the matrix", stage, row)
		}
	}
}
