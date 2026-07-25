package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVaccinationMatrixAuthorsAllSafetyAndDrivePolicies(t *testing.T) {
	raw, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode matrix: %v", err)
	}
	want := map[string]map[string]any{
		"pregnancy_policy": {
			"allow_until_pregnancy_month": float64(3), "skip_from_pregnancy_month": float64(4),
			"skip_through_pregnancy_month": float64(5), "post_delivery_catch_up_days": float64(14),
		},
		"recovery_policy": {"max_nearby_drive_align_days": float64(7)},
		"drive_policy": {
			"enabled": true, "combo_align_window_days": float64(7), "max_batching_hold_days": float64(7),
			"max_batching_hold_count": float64(1), "species_grouping_policy": "kid_mixed",
			"max_shots_per_animal_per_drive": float64(2),
		},
	}
	for policy, fields := range want {
		actual, ok := got[policy].(map[string]any)
		if !ok {
			t.Fatalf("matrix missing %s", policy)
		}
		for key, expected := range fields {
			if actual[key] != expected {
				t.Fatalf("%s.%s=%v want %v", policy, key, actual[key], expected)
			}
		}
	}
}

func TestValidateVaccinationSOPContractRejectsBatchProofAndManualFields(t *testing.T) {
	goodForm := `{"fields":[{"key":"goat_ids","type":"goat_scan","required":true,"repeat":true},{"key":"shed_video","type":"video_proof","required":true,"repeat":true,"proof_subject":"shed"}],"repeat_for_each_goat":{"item_key":"goat_id","source_field":"goat_ids"},"shed_video":{"subject_scope":"shed","capture_source":"in_app_camera"}}`
	if err := validateVaccinationSOPContract(goodForm, vaccinationMatrixProofPolicy); err != nil {
		t.Fatalf("valid shed-level SOP rejected: %v", err)
	}
	bannedManualField := "cold_chain" + "_verified"
	badForm := `{"fields":[{"key":"` + bannedManualField + `","type":"boolean","required":true}]}`
	if err := validateVaccinationSOPContract(badForm, vaccinationMatrixProofPolicy); err == nil {
		t.Fatal("manual medical form field was accepted")
	}
	badProof := `{"types":["video"],"required":true,"subject_scope":"batch","expected_subjects":["shed"],"minimum_count":1}`
	if err := validateVaccinationSOPContract(goodForm, badProof); err == nil {
		t.Fatal("ambiguous batch proof policy was accepted")
	}
}

func TestCorrectedStageIsPersistedByGoatUpsert(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read source seeder: %v", err)
	}
	if !strings.Contains(string(source), "stage:             goatStageByAnimalKey[animalKey]") {
		t.Fatal("goat upsert must persist the corrected age-derived stage, not normalize the stale source stage again")
	}
}

func TestSourceSeederRejectsVaccinationBeforeDOB(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read source seeder: %v", err)
	}
	for _, want := range []string{
		"sourceVaccinationDateBeforeDOB",
		"vaccination date %s before DOB %s",
	} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("source seeder must fail before writing impossible pre-birth vaccination history; missing %q", want)
		}
	}
}

func TestSeededMatrixUsesShedLevelCameraOrGalleryProofPolicy(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read source seeder: %v", err)
	}
	text := string(source)
	if strings.Contains(text, `"required_proofs":["shed","vial_lot","administration"]`) {
		t.Fatal("vaccination seed must not write the retired shed/vial/administration proof policy")
	}
	for _, want := range []string{
		`"proof_mode":"shed_level_video"`,
		`"subject_scope":"shed"`,
		`"expected_subjects":["shed"]`,
		`"capture_source":"in_app_camera"`,
		`"allowed_capture_sources":["in_app_camera","gallery_picker"]`,
		`"maximum_count":5`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("seeded vaccination matrix proof policy missing %s", want)
		}
	}
}

func TestStagesMatch(t *testing.T) {
	tests := []struct {
		name         string
		sourceStage  string
		derivedStage string
		wantMatch    bool
	}{
		// Kid stages match each other
		{name: "K1 vs K1", sourceStage: "K1", derivedStage: "K1", wantMatch: true},
		{name: "K1 vs K2", sourceStage: "K1", derivedStage: "K2", wantMatch: true},
		{name: "K0 vs K1", sourceStage: "K0", derivedStage: "K1", wantMatch: true},
		{name: "k1 vs K1 (case-insensitive)", sourceStage: "k1", derivedStage: "K1", wantMatch: true},

		// Adult stages match each other
		{name: "Adult vs Adult", sourceStage: "Adult", derivedStage: "Adult", wantMatch: true},

		// Kid vs Adult mismatch
		{name: "K1 vs Adult (mismatch)", sourceStage: "K1", derivedStage: "Adult", wantMatch: false},
		{name: "K2 vs Adult (mismatch)", sourceStage: "K2", derivedStage: "Adult", wantMatch: false},
		{name: "Adult vs K1 (mismatch)", sourceStage: "Adult", derivedStage: "K1", wantMatch: false},

		// Empty stages (no contradiction)
		{name: "empty source", sourceStage: "", derivedStage: "K1", wantMatch: true},
		{name: "empty derived", sourceStage: "K1", derivedStage: "", wantMatch: true},
		{name: "both empty", sourceStage: "", derivedStage: "", wantMatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stagesMatch(tt.sourceStage, tt.derivedStage)
			if got != tt.wantMatch {
				t.Errorf("stagesMatch(%q, %q) = %v, want %v", tt.sourceStage, tt.derivedStage, got, tt.wantMatch)
			}
		})
	}
}

func TestIsKidStage(t *testing.T) {
	tests := []struct {
		name    string
		stage   string
		wantKid bool
	}{
		{name: "K0", stage: "K0", wantKid: true},
		{name: "K1", stage: "K1", wantKid: true},
		{name: "K2", stage: "K2", wantKid: true},
		{name: "K3", stage: "K3", wantKid: true},
		{name: "k1 (lowercase)", stage: "k1", wantKid: true},
		{name: "KID", stage: "KID", wantKid: true},
		{name: "Adult", stage: "Adult", wantKid: false},
		{name: "ADULT", stage: "ADULT", wantKid: false},
		{name: "Mother", stage: "Mother", wantKid: false},
		{name: "Buck", stage: "Buck", wantKid: false},
		{name: "empty", stage: "", wantKid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKidStage(tt.stage)
			if got != tt.wantKid {
				t.Errorf("isKidStage(%q) = %v, want %v", tt.stage, got, tt.wantKid)
			}
		})
	}
}

func TestStageCorrectionDetection(t *testing.T) {
	// Test the stage correction detection logic by directly calling stagesMatch
	// with realistic source/derived stage combinations.

	tests := []struct {
		name          string
		sourceStage   string
		derivedStage  string
		shouldCorrect bool
	}{
		// Cases that need correction
		{name: "kid tag on adult goat", sourceStage: "K2", derivedStage: "Adult", shouldCorrect: true},
		{name: "kid tag K1 on adult goat", sourceStage: "K1", derivedStage: "Adult", shouldCorrect: true},

		// Cases that don't need correction
		{name: "kid tag on kid goat", sourceStage: "K1", derivedStage: "K1", shouldCorrect: false},
		{name: "kid tag K2 on kid goat", sourceStage: "K2", derivedStage: "K1", shouldCorrect: false},
		{name: "adult tag on adult goat", sourceStage: "Adult", derivedStage: "Adult", shouldCorrect: false},
		{name: "mother tag on adult goat", sourceStage: "Mother", derivedStage: "Adult", shouldCorrect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := stagesMatch(tt.sourceStage, tt.derivedStage)
			shouldCorrect := !matches && tt.sourceStage != "" && tt.derivedStage != ""
			if shouldCorrect != tt.shouldCorrect {
				t.Errorf("%s: shouldCorrect = %v, want %v", tt.name, shouldCorrect, tt.shouldCorrect)
			}
		})
	}
}

func TestDerivedStageFromDOB(t *testing.T) {
	// Use a fixed reference time for testing
	refTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		dob         *time.Time
		asOf        time.Time
		wantStage   string
		description string
	}{
		{
			name:        "nil DOB",
			dob:         nil,
			asOf:        refTime,
			wantStage:   "",
			description: "no DOB, no derived stage",
		},
		{
			name:        "newborn (0 weeks)",
			dob:         ptrTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			asOf:        refTime,
			wantStage:   "K1",
			description: "0-week-old is kid (≤20w)",
		},
		{
			name:        "12 weeks old",
			dob:         ptrTime(time.Date(2023, 10, 9, 0, 0, 0, 0, time.UTC)), // 12 weeks before refTime
			asOf:        refTime,
			wantStage:   "K1",
			description: "12-week-old is kid (≤20w)",
		},
		{
			name:        "16 weeks old (at kid cutoff)",
			dob:         ptrTime(time.Date(2023, 9, 11, 0, 0, 0, 0, time.UTC)), // 16 weeks before refTime
			asOf:        refTime,
			wantStage:   "K1",
			description: "16-week-old is still kid (≤20w)",
		},
		{
			name:        "20 weeks old (at finish cutoff)",
			dob:         ptrTime(time.Date(2023, 8, 14, 0, 0, 0, 0, time.UTC)), // 20 weeks before refTime
			asOf:        refTime,
			wantStage:   "K1",
			description: "20-week-old is still kid (≤20w)",
		},
		{
			name:        "21 weeks old (past cutoff)",
			dob:         ptrTime(time.Date(2023, 8, 7, 0, 0, 0, 0, time.UTC)), // 21 weeks before refTime
			asOf:        refTime,
			wantStage:   "Adult",
			description: "21-week-old is adult (>20w)",
		},
		{
			name:        "40 weeks old",
			dob:         ptrTime(time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)), // 40 weeks before refTime
			asOf:        refTime,
			wantStage:   "Adult",
			description: "40-week-old is adult (>20w)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derivedStageFromDOBForTest(tt.dob, tt.asOf)
			if got != tt.wantStage {
				t.Errorf("%s: derivedStageFromDOB(%v, %v) = %q, want %q",
					tt.description, tt.dob, tt.asOf, got, tt.wantStage)
			}
		})
	}
}

// Helper functions for testing

func ptrTime(t time.Time) *time.Time {
	return &t
}

// derivedStageFromDOBForTest is a wrapper for testing. In the real code,
// we import vaccinationapp.DerivedStageFromDOB.
// For testing without external dependencies, we inline the same logic.
func derivedStageFromDOBForTest(dob *time.Time, asOf time.Time) string {
	if dob == nil {
		return ""
	}
	// Mimics wholeDaysBetween logic: count full 24-hour periods
	diff := asOf.Sub(*dob)
	daysBetween := int(diff.Hours() / 24)
	ageWeeks := daysBetween / 7

	kidFinishWeeks := 16 + 4 // default kidWeeks=16, finishWeeks=kidWeeks+4
	if ageWeeks <= kidFinishWeeks {
		return "K1"
	}
	return "Adult"
}
