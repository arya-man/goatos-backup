package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
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

func TestNextDueAfterLastVaccinationUsesHistoricalCellAsAnchor(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 11, 17, 4, 0, 0, loc)
	lastAdministered := sourceVaccinationDateTime(time.Date(2026, time.June, 21, 0, 0, 0, 0, loc), loc)

	got := nextDueAfterLastVaccination(lastAdministered, "ET+TT", asOf)
	want := time.Date(2026, time.December, 20, 9, 0, 0, 0, loc)

	if !got.Equal(want) {
		t.Fatalf("next due = %s, want %s", got, want)
	}
	if !got.After(asOf) {
		t.Fatalf("next due = %s must be after import as-of %s", got, asOf)
	}
}

func TestNextDueAfterLastVaccinationRollsForwardPastImportAsOf(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 11, 17, 4, 0, 0, loc)
	lastAdministered := sourceVaccinationDateTime(time.Date(2025, time.January, 1, 0, 0, 0, 0, loc), loc)

	got := nextDueAfterLastVaccination(lastAdministered, "ET+TT", asOf)
	want := time.Date(2026, time.December, 30, 9, 0, 0, 0, loc)

	if !got.Equal(want) {
		t.Fatalf("next due = %s, want %s", got, want)
	}
	if !got.After(asOf) {
		t.Fatalf("next due = %s must be after import as-of %s", got, asOf)
	}
}

func TestShouldDeriveNextCycleAfterHistoryOnlyForTerminalDose(t *testing.T) {
	if shouldDeriveNextCycleAfterHistory(vaccCell{Vaccine: "ET+TT", DoseCode: "first"}) {
		t.Fatal("ET+TT first-dose history must not seed a flat-interval next cycle while booster owns course progress")
	}
	if !shouldDeriveNextCycleAfterHistory(vaccCell{Vaccine: "ET+TT", DoseCode: "booster"}) {
		t.Fatal("ET+TT booster history must seed the terminal-dose revaccination cycle")
	}
	if !shouldDeriveNextCycleAfterHistory(vaccCell{Vaccine: "PPR", DoseCode: "first"}) {
		t.Fatal("single-dose vaccine history must seed its revaccination cycle")
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
