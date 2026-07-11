package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestVaccinationMatrixRowsUseSpeciesScopedEligibility(t *testing.T) {
	dsl, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build vaccination matrix rule DSL: %v", err)
	}

	var payload struct {
		MatrixRows []struct {
			Species     []string `json:"species"`
			Eligibility struct {
				Species []string `json:"species"`
			} `json:"eligibility"`
			Vaccine struct {
				Code string `json:"code"`
			} `json:"vaccine"`
		} `json:"matrix_rows"`
	}
	if err := json.Unmarshal([]byte(dsl), &payload); err != nil {
		t.Fatalf("unmarshal vaccination matrix DSL: %v", err)
	}

	byCode := make(map[string][]string, len(payload.MatrixRows))
	for _, row := range payload.MatrixRows {
		if !reflect.DeepEqual(row.Eligibility.Species, row.Species) {
			t.Fatalf("%s eligibility species = %#v, want row species %#v", row.Vaccine.Code, row.Eligibility.Species, row.Species)
		}
		byCode[row.Vaccine.Code] = row.Eligibility.Species
	}

	for code, want := range map[string][]string{
		"GOAT_POX":    {"goat"},
		"BLUE_TONGUE": {"sheep"},
		"SHEEP_POX":   {"sheep"},
	} {
		if got, ok := byCode[code]; !ok {
			t.Fatalf("matrix row for %s missing", code)
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s eligibility species = %#v, want %#v", code, got, want)
		}
	}
}

func TestHistoryIdempotencyIncludesAdministeredSourceDate(t *testing.T) {
	cell := vaccCell{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first"}
	def := vaccines["ET+TT"]

	firstObligationID := historyObligationID(defaultTenantID, cell, def, "2026-06-21")
	secondObligationID := historyObligationID(defaultTenantID, cell, def, "2026-06-22")
	if firstObligationID == secondObligationID {
		t.Fatal("history obligation IDs must differ by administered source date")
	}
	if got, want := historyObligationIdem(cell, def, "2026-06-21"), "vacc-real-obl:history:goat-1:et_tt:first:2026-06-21"; got != want {
		t.Fatalf("history obligation idem = %q, want %q", got, want)
	}

	firstCompletionID := historyCompletionID(defaultTenantID, cell, def, "2026-06-21")
	secondCompletionID := historyCompletionID(defaultTenantID, cell, def, "2026-06-22")
	if firstCompletionID == secondCompletionID {
		t.Fatal("history completion IDs must differ by administered source date")
	}
	if got, want := historyCompletionIdem(cell, def, "2026-06-21"), "vacc-real-cmp:goat-1:et_tt:first:2026-06-21"; got != want {
		t.Fatalf("history completion idem = %q, want %q", got, want)
	}
}

func TestBuildEntryDateMappingUsesEntrySourcesOnly(t *testing.T) {
	got := buildEntryDateMapping([]goatRecord{
		{RFID: "rfid-dob-only", DOB: "2026-01-01"},
		{RFID: "rfid-stage-entry", DOB: "2026-01-01", StageEntryDate: "2026-04-05"},
		{RFID: "rfid-purchase", DOB: "2026-01-01", StageEntryDate: "2026-04-05", PurchaseDate: "2026-05-06"},
	})
	if _, ok := got["rfid-dob-only"]; ok {
		t.Fatal("DOB-only source rows must not get a synthetic entry_date")
	}
	if got["rfid-stage-entry"] == nil || got["rfid-stage-entry"].Format("2006-01-02") != "2026-04-05" {
		t.Fatalf("stage entry date mapping = %v, want 2026-04-05", got["rfid-stage-entry"])
	}
	if got["rfid-purchase"] == nil || got["rfid-purchase"].Format("2006-01-02") != "2026-05-06" {
		t.Fatalf("purchase date must win over stage entry date, got %v", got["rfid-purchase"])
	}
}

func TestLoadGoatsMissingSpeciesColumnLeavesSpeciesEmpty(t *testing.T) {
	dir := t.TempDir()
	data := `{"values":[["rfid","old_id","old_id_suffix","farm","shed","stage","age","breed","gender","dob","stage_entry_date","purchase_date","status","health_status"],["RFID-SHEEP-HINT","","","CBE","Shed 1","Adult","","Sojat","Female","","","","Alive","Healthy"]]}`
	if err := os.WriteFile(dir+"/goats.json", []byte(data), 0644); err != nil {
		t.Fatalf("write goats source: %v", err)
	}

	goats, err := loadGoats(dir)
	if err != nil {
		t.Fatalf("load goats: %v", err)
	}
	if len(goats) != 1 {
		t.Fatalf("goats = %d, want 1", len(goats))
	}
	if goats[0].Species != "" {
		t.Fatalf("missing species column species = %q, want empty", goats[0].Species)
	}
}

func TestDeriveSeedSpeciesUsesSheepBreed(t *testing.T) {
	if got := deriveSeedSpecies("", "Anantapur Sheep"); got != "sheep" {
		t.Fatalf("species = %q, want sheep", got)
	}
	if got := normalizeBreed("Anantapur Sheep"); got != "Anantapur Sheep" {
		t.Fatalf("breed = %q, want Anantapur Sheep", got)
	}
}

func TestDeriveSeedSpeciesDefaultsGoatForKnownGoatBreed(t *testing.T) {
	if got := deriveSeedSpecies("", "Sojat"); got != "goat" {
		t.Fatalf("species = %q, want goat", got)
	}
}

func TestDeriveSeedSpeciesUsesExplicitSpeciesHint(t *testing.T) {
	if got := deriveSeedSpecies("sheep", "Sojat"); got != "sheep" {
		t.Fatalf("species = %q, want sheep", got)
	}
	if got := deriveSeedSpecies("goat", "Anantapur Sheep"); got != "goat" {
		t.Fatalf("species = %q, want explicit goat hint to win", got)
	}
}

func TestSeedGenerationErrorRejectsPartialFailureByDefault(t *testing.T) {
	err := seedGenerationError(vaccinationdomain.GenerateResult{
		Generated:        8,
		Deferred:         2,
		FailedGoats:      3,
		SkippedNoDueDate: 1,
	}, seedPartialGenerationErr{}, false)
	if err == nil {
		t.Fatal("partial generation failure must fail the seed by default")
	}
	if !strings.Contains(err.Error(), "failed_goats=3") {
		t.Fatalf("error = %q, want failed goat counter", err)
	}
}

func TestSeedGenerationErrorAllowsPartialFailureWhenFlagged(t *testing.T) {
	err := seedGenerationError(vaccinationdomain.GenerateResult{FailedGoats: 3}, seedPartialGenerationErr{}, true)
	if err != nil {
		t.Fatalf("allow partial generation error = %v, want nil", err)
	}
}

func TestSourceVaccinationDateIsHistoryUsesBusinessDateNotClockTime(t *testing.T) {
	loc := mustKolkata(t)
	sourceDate := time.Date(2026, time.July, 11, 0, 0, 0, 0, loc)
	asOfBeforeSeedTimestamp := time.Date(2026, time.July, 11, 6, 30, 0, 0, loc)

	if !sourceVaccinationDateIsHistory(sourceDate, asOfBeforeSeedTimestamp, loc) {
		t.Fatalf("same source business date must import as completed history even before 09:00 as_of=%s", asOfBeforeSeedTimestamp)
	}
}

func TestSourceVaccinationDateIsHistoryRejectsFutureBusinessDate(t *testing.T) {
	loc := mustKolkata(t)
	sourceDate := time.Date(2026, time.July, 12, 0, 0, 0, 0, loc)
	asOf := time.Date(2026, time.July, 11, 23, 59, 0, 0, loc)

	if sourceVaccinationDateIsHistory(sourceDate, asOf, loc) {
		t.Fatalf("future source business date must remain scheduled, source=%s as_of=%s", sourceDate, asOf)
	}
}

func mustKolkata(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	return loc
}

func TestVaccinationMatrixUsesNextCycleOnlyForRepeatRows(t *testing.T) {
	raw, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("vaccinationMatrixRuleDSL: %v", err)
	}
	var payload struct {
		Schedule []struct {
			TriggerType string `json:"trigger_type"`
			Repeat      string `json:"repeat"`
			CatchUp     string `json:"catch_up"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal matrix: %v", err)
	}
	var repeats int
	for _, row := range payload.Schedule {
		if row.TriggerType == "after_previous_completion" && row.Repeat != "none" {
			repeats++
			if row.CatchUp != "next_cycle" {
				t.Fatalf("repeat row catch_up=%q, want next_cycle", row.CatchUp)
			}
			continue
		}
		if row.CatchUp != "immediate" {
			t.Fatalf("primary/booster row catch_up=%q, want immediate", row.CatchUp)
		}
	}
	if repeats != len(vaccineOrder) {
		t.Fatalf("repeat rows=%d, want %d", repeats, len(vaccineOrder))
	}
}

func TestVaccinationMatrixIsPublishable(t *testing.T) {
	raw, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("vaccinationMatrixRuleDSL: %v", err)
	}
	if err := protocolapp.ValidateRuleDSL([]byte(raw)); err != nil {
		t.Fatalf("ValidateRuleDSL: %v", err)
	}
	if err := protocolapp.ValidatePublishable([]byte(raw)); err != nil {
		t.Fatalf("ValidatePublishable: %v", err)
	}
}

func TestVaccinationMatrixKeepsLegacyUnknownHealthSchedulableWithSafetyDeferrals(t *testing.T) {
	eligibility := vaccinationSeedEligibility()
	if got := eligibility["health"]; !reflect.DeepEqual(got, []string{"any"}) {
		t.Fatalf("health eligibility = %#v, want [any]", got)
	}
	if got := eligibility["animal_stage"]; !reflect.DeepEqual(got, []string{"all"}) {
		t.Fatalf("animal_stage eligibility = %#v, want [all]", got)
	}
	if got := eligibility["breed"]; !reflect.DeepEqual(got, []string{"all"}) {
		t.Fatalf("breed eligibility = %#v, want [all]", got)
	}
	wantDeferrals := []string{"sick", "under_treatment", "icu", "quarantine"}
	if got := eligibility["defer_states"]; !reflect.DeepEqual(got, wantDeferrals) {
		t.Fatalf("defer_states = %#v, want %#v", got, wantDeferrals)
	}
}

type seedPartialGenerationErr struct{}

func (seedPartialGenerationErr) Error() string {
	return "partial generation"
}

func (seedPartialGenerationErr) Is(target error) bool {
	return target != nil && target.Error() == "vaccination: generation completed with failed goats"
}
