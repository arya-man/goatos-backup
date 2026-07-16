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

func TestSeedObligationScopePrefersAnimalLocation(t *testing.T) {
	t.Run("shed scope wins for calendar-visible history", func(t *testing.T) {
		scopeType, scopeID := seedObligationScope("tenant-1", "park-1", "shed-1")
		if scopeType != "shed" || scopeID != "shed-1" {
			t.Fatalf("scope = %s/%s, want shed/shed-1", scopeType, scopeID)
		}
	})

	t.Run("park fallback keeps park-filtered history visible", func(t *testing.T) {
		scopeType, scopeID := seedObligationScope("tenant-1", "park-1", "")
		if scopeType != "park" || scopeID != "park-1" {
			t.Fatalf("scope = %s/%s, want park/park-1", scopeType, scopeID)
		}
	})

	t.Run("tenant fallback is last resort only", func(t *testing.T) {
		scopeType, scopeID := seedObligationScope("tenant-1", "", "")
		if scopeType != "tenant" || scopeID != "tenant-1" {
			t.Fatalf("scope = %s/%s, want tenant/tenant-1", scopeType, scopeID)
		}
	})
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

func TestSourceSeedNeverRetiresCanonicalShedsBySourceAbsence(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read source seeder: %v", err)
	}
	for _, forbidden := range []string{
		"retireActiveShedsOutsideSource",
		"active shed not present in canonical goat source",
		"pruned_non_source_active_sheds",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("source seed must not retire canonical sheds by source absence; found %q", forbidden)
		}
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

	if !sourceVaccinationDateOnOrBeforeBusinessDate(sourceDate, asOfBeforeSeedTimestamp, loc) {
		t.Fatalf("same source business date must import as completed history even before 09:00 as_of=%s", asOfBeforeSeedTimestamp)
	}
}

func TestSourceVaccinationDateIsHistoryRejectsFutureBusinessDate(t *testing.T) {
	loc := mustKolkata(t)
	sourceDate := time.Date(2026, time.July, 12, 0, 0, 0, 0, loc)
	asOf := time.Date(2026, time.July, 11, 23, 59, 0, 0, loc)

	if sourceVaccinationDateOnOrBeforeBusinessDate(sourceDate, asOf, loc) {
		t.Fatalf("future source business date must remain scheduled, source=%s as_of=%s", sourceDate, asOf)
	}
}

func TestSeedSchedulePathUsesLiveCutoffForSourceHistory(t *testing.T) {
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 210) // 30 weeks: past the default 20w kid-course finish window.

	path := seedSchedulePathForGoat("birth", &dob, "K1", nil, asOf, seedKidsNormalScheduleUntilWeeks, nil)
	if path != "adult" {
		t.Fatalf("over-cutoff birth/K-stage source row path = %q, want adult", path)
	}

	doseCode, later := mapSheetDoseToRuleCode("PPR", "first", path, buildCanonicalVaccinationMatrix())
	if later {
		t.Fatal("PPR first adult source date must not be treated as a later same-rule merge")
	}
	if doseCode != "ppr_adult_w1" {
		t.Fatalf("over-cutoff PPR source date mapped to %q, want ppr_adult_w1", doseCode)
	}
}

func TestSeedImportedCompletionKeepsSourceDateWhileUsingAdultPath(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 16, 12, 0, 0, 0, loc)
	dob := asOf.AddDate(0, 0, -210)
	cell := vaccCell{AnimalKey: "old-k-stage", Vaccine: "FMD", DoseCode: "booster", Sequence: 2, Value: "2026-06-21"}

	path := seedSchedulePathForGoat("birth", &dob, "K1", nil, asOf, seedKidsNormalScheduleUntilWeeks, nil)
	if path != "adult" {
		t.Fatalf("over-cutoff FMD source row path = %q, want adult", path)
	}
	doseCode, later := mapSheetDoseToRuleCode(cell.Vaccine, cell.DoseCode, path, buildCanonicalVaccinationMatrix())
	if doseCode != "fmd_adult_w1" || !later {
		t.Fatalf("FMD booster source date mapped to dose=%q later=%v, want fmd_adult_w1/true", doseCode, later)
	}

	sourceDate, err := time.ParseInLocation("2006-01-02", cell.Value, loc)
	if err != nil {
		t.Fatalf("parse source date: %v", err)
	}
	administeredAt := sourceVaccinationDateTime(sourceDate, loc)
	if got := administeredAt.Format("2006-01-02 15:04 MST"); got != "2026-06-21 09:00 IST" {
		t.Fatalf("administeredAt = %s, want 2026-06-21 09:00 IST", got)
	}
	if !sourceVaccinationDateOnOrBeforeBusinessDate(sourceDate, asOf, loc) {
		t.Fatal("past source vaccination date must be imported as completed history")
	}
	if got, want := historyCompletionIdem(cell, vaccines["FMD"], "2026-06-21"), "vacc-real-cmp:old-k-stage:fmd:booster:2026-06-21"; got != want {
		t.Fatalf("history completion idem = %q, want %q", got, want)
	}
}

func TestSeedSchedulePathHonorsConfigurableCutoffAndHistorySignal(t *testing.T) {
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 154) // 22 weeks.

	// With the default 16w cutoff, the 4w finish window ends at 20w; even a stale K-stage
	// tag must not force a kid-course source mapping.
	if got := seedSchedulePathForGoat("birth", &dob, "K2", nil, asOf, 16, nil); got != "adult" {
		t.Fatalf("22w with default cutoff = %q, want adult", got)
	}

	// If the active rule config moves the normal kid cutoff to 20w, the finish window ends
	// at 24w and a fresh K-stage tag remains valid in-course evidence.
	if got := seedSchedulePathForGoat("birth", &dob, "K2", nil, asOf, 20, nil); got != "kid" {
		t.Fatalf("22w with configured 20w cutoff and K-stage = %q, want kid", got)
	}

	kidHistory := []vaccinationdomain.RecentVaccineAdministration{{
		AdministeredAt: dob.AddDate(0, 0, 84),
		VaccineCode:    "FMD",
		DoseCode:       "fmd_kid_12w",
	}}
	if got := seedSchedulePathForGoat("", nil, "", nil, asOf, 16, kidHistory); got != "kid" {
		t.Fatalf("no DOB/stage with accepted kid-course history = %q, want kid", got)
	}
	if got := seedSchedulePathForGoat("", nil, "", nil, asOf, 16, nil); got != "adult" {
		t.Fatalf("no DOB/stage/history = %q, want adult", got)
	}
}

func TestSeedLoopFeedsKidCourseHistoryIntoSharedClassifier(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 16, 9, 0, 0, 0, loc)
	matrix := buildCanonicalVaccinationMatrix()
	cells := []vaccCell{{
		AnimalKey: "unknown-dob-non-k-stage",
		Vaccine:   "FMD",
		DoseType:  "First Dose",
		DoseCode:  "first",
		Sequence:  1,
		Value:     "2026-06-01",
	}}

	historyByAnimal := buildSeedKidCourseHistoryByAnimalKey(cells, loc, asOf, matrix)
	history := historyByAnimal["unknown-dob-non-k-stage"]
	if len(history) != 1 {
		t.Fatalf("kid-course history rows = %d, want 1", len(history))
	}
	if history[0].DoseCode != "fmd_kid_12w" {
		t.Fatalf("kid history dose code = %q, want fmd_kid_12w", history[0].DoseCode)
	}
	if history[0].AdministeredAt.IsZero() {
		t.Fatal("kid history AdministeredAt must be populated")
	}

	path := seedSchedulePathForGoat("", nil, "adult", nil, asOf, seedKidsNormalScheduleUntilWeeks, history)
	if path != "kid" {
		t.Fatalf("unknown DOB/non-K stage with source kid history path = %q, want kid", path)
	}
	doseCode, later := mapSheetDoseToRuleCode(cells[0].Vaccine, cells[0].DoseCode, path, matrix)
	if doseCode != "fmd_kid_12w" || later {
		t.Fatalf("seed loop mapped source kid dose to dose=%q later=%v, want fmd_kid_12w/false", doseCode, later)
	}
}

func TestSeedKidCourseHistoryIgnoresFutureSourceDates(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 16, 9, 0, 0, 0, loc)
	matrix := buildCanonicalVaccinationMatrix()
	cells := []vaccCell{{
		AnimalKey: "future-kid-dose",
		Vaccine:   "FMD",
		DoseType:  "First Dose",
		DoseCode:  "first",
		Sequence:  1,
		Value:     "2026-07-17",
	}}

	historyByAnimal := buildSeedKidCourseHistoryByAnimalKey(cells, loc, asOf, matrix)
	if got := len(historyByAnimal["future-kid-dose"]); got != 0 {
		t.Fatalf("future source-date kid history rows = %d, want 0", got)
	}
	path := seedSchedulePathForGoat("", nil, "", nil, asOf, seedKidsNormalScheduleUntilWeeks, historyByAnimal["future-kid-dose"])
	if path != "adult" {
		t.Fatalf("unknown DOB/stage with only future source date path = %q, want adult", path)
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

func TestValidateSeedReconciliation(t *testing.T) {
	t.Run("clean seed passes", func(t *testing.T) {
		if err := validateSeedReconciliation(seedReconciliation{SourceAcceptedHistory: 3389}, 3389); err != nil {
			t.Fatalf("validate clean reconciliation: %v", err)
		}
	})

	t.Run("every corruption class blocks handoff", func(t *testing.T) {
		got := seedReconciliation{
			SourceAcceptedHistory:      3388,
			AcceptedStatusMismatches:   1,
			DuplicateActiveRuleTargets: 2,
			ActivePrimaryAfterHistory:  3,
			SeededPendingPlaceholders:  4,
			RepeatObligationsNotFuture: 5,
			MissingBreedForeignKeys:    6,
			MissingPrimaryIdentifiers:  7,
			MissingAnchorNormalWork:    8,
		}
		err := validateSeedReconciliation(got, 3389)
		if err == nil {
			t.Fatal("validate corrupt reconciliation returned nil")
		}
		for _, want := range []string{
			"accepted source history=3388 want=3389",
			"accepted completion/status mismatches=1",
			"duplicate active goat/rule groups=2",
			"active primary obligations already satisfied by accepted history=3",
			"seed-owned Pending placeholders=4",
			"repeat obligations not strictly future=5",
			"goats missing species-owned breed foreign key=6",
			"goats missing primary animal identifier=7",
			"missing trigger anchor goats with normal active work=8",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%q, want %q", err, want)
			}
		}
	})
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

func TestSourceNormalizationPreservesProvenance(t *testing.T) {
	for raw, want := range map[string]string{
		"Birth":    "birth",
		"Purchase": "procured",
		"procured": "procured",
		"Imported": "imported",
		"":         "",
	} {
		if got := normalizeOriginType(raw); got != want {
			t.Fatalf("normalizeOriginType(%q)=%q, want %q", raw, got, want)
		}
	}
	if got := seedBreedKey("sheep", "Anantapur Sheep"); got != "sheep\x00anantapur sheep" {
		t.Fatalf("seedBreedKey=%q", got)
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

// ---- Fix Plan A1: zero-silent-drop FMD/HS reconciliation ----

func TestMapSheetDoseToRuleCodeReconcilesFMDBoosterAsLaterAdministration(t *testing.T) {
	matrix := buildCanonicalVaccinationMatrix()

	for _, path := range []string{"kid", "adult"} {
		doseCode, later := mapSheetDoseToRuleCode("FMD", "booster", path, matrix)
		if doseCode == "" {
			t.Fatalf("FMD booster (%s path) must resolve a dose code, got empty (silently dropped)", path)
		}
		if !later {
			t.Fatalf("FMD booster (%s path) must be flagged as a later administration", path)
		}

		firstDoseCode, firstLater := mapSheetDoseToRuleCode("FMD", "first", path, matrix)
		if firstDoseCode == "" {
			t.Fatalf("FMD first dose (%s path) must resolve a dose code", path)
		}
		if firstLater {
			t.Fatalf("FMD first dose (%s path) must not be flagged as a later administration", path)
		}
		if doseCode != firstDoseCode {
			t.Fatalf("FMD booster (%s path) dose code = %q, want the SAME single wave as first dose %q (no fabricated booster rule)", path, doseCode, firstDoseCode)
		}
	}
}

func TestMapSheetDoseToRuleCodeReconcilesHSBoosterAsLaterAdministration(t *testing.T) {
	matrix := buildCanonicalVaccinationMatrix()
	doseCode, later := mapSheetDoseToRuleCode("HS", "booster", "adult", matrix)
	if doseCode == "" || !later {
		t.Fatalf("HS booster (adult path) = (%q, %v), want a non-empty dose code flagged as later administration", doseCode, later)
	}
}

func TestMapSheetDoseToRuleCodeTwoWaveVaccinesUnaffected(t *testing.T) {
	matrix := buildCanonicalVaccinationMatrix()

	// ET+TT has two real birth-age/post-arrival waves: booster must map to the
	// SECOND wave and must NOT be flagged as a reconciled "later administration" —
	// that classification is reserved for single-wave (FMD/HS-shaped) vaccines.
	firstKid, laterKid := mapSheetDoseToRuleCode("ET+TT", "first", "kid", matrix)
	boosterKid, laterBoosterKid := mapSheetDoseToRuleCode("ET+TT", "booster", "kid", matrix)
	if laterKid || laterBoosterKid {
		t.Fatalf("ET+TT kid path must never be flagged as a later administration (first later=%v booster later=%v)", laterKid, laterBoosterKid)
	}
	if firstKid == boosterKid {
		t.Fatalf("ET+TT kid first/booster must map to DIFFERENT waves, both got %q", firstKid)
	}

	firstAdult, _ := mapSheetDoseToRuleCode("ET+TT", "first", "adult", matrix)
	boosterAdult, laterBoosterAdult := mapSheetDoseToRuleCode("ET+TT", "booster", "adult", matrix)
	if laterBoosterAdult {
		t.Fatal("ET+TT adult booster must not be flagged as a later administration (real second wave exists)")
	}
	if firstAdult == boosterAdult {
		t.Fatalf("ET+TT adult first/booster must map to DIFFERENT waves, both got %q", firstAdult)
	}
}

func TestCountDatedFactsExcludesBlankNAAndPending(t *testing.T) {
	cells := []vaccCell{
		{Value: "2026-03-20"},
		{Value: " 2026-03-21 "},
		{Value: ""},
		{Value: "NA"},
		{Value: "na"},
		{Value: "Pending"},
		{Value: "pending"},
	}
	if got, want := countDatedFacts(cells), 2; got != want {
		t.Fatalf("countDatedFacts = %d, want %d", got, want)
	}
}

func TestReconcileDatedFactsPassesWhenEveryFactIsAccountedFor(t *testing.T) {
	cells := []vaccCell{
		{Value: "2026-03-20"}, // lands as Completed
		{Value: "2026-08-01"}, // lands as Scheduled
		{Value: "NA"},         // not a dated fact
		{Value: "Pending"},    // not a dated fact
	}
	st := stats{Completed: 1, Scheduled: 1}
	if err := reconcileDatedFacts(cells, st); err != nil {
		t.Fatalf("reconcileDatedFacts() = %v, want nil (every dated fact accounted for)", err)
	}
}

func TestReconcileDatedFactsFailsLoudlyOnASilentDrop(t *testing.T) {
	cells := []vaccCell{
		{Value: "2026-03-20"},
		{Value: "2026-03-21"}, // this one is NOT reflected in any stats bucket below
	}
	st := stats{Completed: 1}
	err := reconcileDatedFacts(cells, st)
	if err == nil {
		t.Fatal("reconcileDatedFacts() = nil, want an error: one dated fact is unaccounted for")
	}
	if !strings.Contains(err.Error(), "dated_source_facts=2 reconciled=1") {
		t.Fatalf("error = %q, want it to name the gap (2 facts, 1 reconciled)", err)
	}
}

// ---- Fix Plan A2: health_status case-log vocabulary ----

func TestNormalizeHealthMapsCaseLogVocabulary(t *testing.T) {
	for raw, want := range map[string]string{
		"Open":     "sick",
		"Extended": "under_treatment",
		"Closed":   "recovering",
		"open":     "sick",
		"CLOSED":   "recovering",
		"Healthy":  "healthy",
		"ICU":      "icu",
	} {
		got := normalizeHealth(raw)
		if got == nil {
			t.Fatalf("normalizeHealth(%q) = nil, want %q (never silently NULL a populated source cell)", raw, want)
		}
		if *got != want {
			t.Fatalf("normalizeHealth(%q) = %q, want %q", raw, *got, want)
		}
	}
}

func TestNormalizeHealthBlankStaysNil(t *testing.T) {
	if got := normalizeHealth(""); got != nil {
		t.Fatalf("normalizeHealth(\"\") = %v, want nil", *got)
	}
}

// ---- Fix Plan A3/A4: shed_tag clinical/reproductive/location signals ----

func TestShedTagClinicalSignalMapsDocumentedTags(t *testing.T) {
	cases := []struct {
		tag          string
		wantHealth   string
		wantRepro    string
		hasHealth    bool
		hasReproduce bool
	}{
		{tag: "ICU", wantHealth: "icu", hasHealth: true},
		{tag: "ICU-Kid", wantHealth: "icu", hasHealth: true},
		{tag: "Quarantine kids", wantHealth: "quarantine", hasHealth: true},
		{tag: "Pregnant", wantRepro: "pregnant", hasReproduce: true},
		{tag: "Non-Pregnant", wantRepro: "non_pregnant", hasReproduce: true},
		{tag: "ICU-Non-Pregnant", wantHealth: "icu", wantRepro: "non_pregnant", hasHealth: true, hasReproduce: true},
	}
	for _, tc := range cases {
		health, repro := shedTagClinicalSignal(tc.tag)
		if tc.hasHealth {
			if health == nil || *health != tc.wantHealth {
				t.Fatalf("shedTagClinicalSignal(%q) health = %v, want %q", tc.tag, health, tc.wantHealth)
			}
		} else if health != nil {
			t.Fatalf("shedTagClinicalSignal(%q) health = %q, want nil", tc.tag, *health)
		}
		if tc.hasReproduce {
			if repro == nil || *repro != tc.wantRepro {
				t.Fatalf("shedTagClinicalSignal(%q) reproductive = %v, want %q", tc.tag, repro, tc.wantRepro)
			}
		} else if repro != nil {
			t.Fatalf("shedTagClinicalSignal(%q) reproductive = %q, want nil", tc.tag, *repro)
		}
	}
}

func TestShedTagClinicalSignalLeavesGrowthCohortTagsUnmapped(t *testing.T) {
	for _, tag := range []string{"K0", "K1", "K2", "K3", "F2", "F2-Male", "F2-Female", "Buck", "Mother", "Milking", "M0", "Warmup"} {
		health, repro := shedTagClinicalSignal(tag)
		if health != nil || repro != nil {
			t.Fatalf("shedTagClinicalSignal(%q) = (%v, %v), want (nil, nil) — growth-cohort/management tags are not clinical/reproductive signals", tag, health, repro)
		}
	}
}

func TestResolveGoatHealthPrefersCurrentShedTagOverClosedCaseLog(t *testing.T) {
	// A goat with a CLOSED historical case but currently housed in the ICU shed must
	// read as icu — current placement is a stronger signal than a resolved case log.
	got := resolveGoatHealth("Closed", "ICU")
	if got == nil || *got != "icu" {
		t.Fatalf("resolveGoatHealth(Closed, ICU) = %v, want icu", got)
	}
}

func TestResolveGoatHealthFallsBackToCaseLogWhenShedTagIsNotClinical(t *testing.T) {
	got := resolveGoatHealth("Open", "Pregnant")
	if got == nil || *got != "sick" {
		t.Fatalf("resolveGoatHealth(Open, Pregnant) = %v, want sick (Pregnant carries no health signal)", got)
	}
}

func TestResolveGoatReproductiveStatusImportsFromShedTag(t *testing.T) {
	if got := resolveGoatReproductiveStatus("Pregnant"); got == nil || *got != "pregnant" {
		t.Fatalf("resolveGoatReproductiveStatus(Pregnant) = %v, want pregnant", got)
	}
	if got := resolveGoatReproductiveStatus("Non-Pregnant"); got == nil || *got != "non_pregnant" {
		t.Fatalf("resolveGoatReproductiveStatus(Non-Pregnant) = %v, want non_pregnant", got)
	}
	if got := resolveGoatReproductiveStatus("K2"); got != nil {
		t.Fatalf("resolveGoatReproductiveStatus(K2) = %v, want nil (not a reproductive tag)", got)
	}
}

func TestLoadGoatsPopulatesShedTag(t *testing.T) {
	dir := t.TempDir()
	data := `{"values":[["rfid","old_id","old_id_suffix","farm","shed","shed_tag","stage","age","breed","gender","dob","stage_entry_date","purchase_date","status","health_status"],["RFID-1","","","CBE","Shed 1","ICU-Non-Pregnant","Adult","","Sojat","Female","","","","Alive","Closed"]]}`
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
	if goats[0].ShedTag != "ICU-Non-Pregnant" {
		t.Fatalf("ShedTag = %q, want ICU-Non-Pregnant", goats[0].ShedTag)
	}
	if goats[0].Health != "Closed" {
		t.Fatalf("Health = %q, want Closed", goats[0].Health)
	}
}

// ---- End-to-end importer accounting: full source shape, zero silent drops ----

func TestLoadVaccinationCellsReconcilesFMDBoosterColumnEndToEnd(t *testing.T) {
	dir := t.TempDir()
	// Mirrors the reviewed source shape: two header rows, FMD has a Booster column
	// (col index 12) that the pre-fix importer silently dropped because FMD has only
	// one wave per path. RFID/Old ID/Old ID Suffix occupy columns 0-2 as in the real sheet.
	header := `["RFID","Old ID","Old ID Suffix","c3","c4","c5","c6","c7","c8","c9","c10","FMD","FMD"]`
	doseRow := `["","","","","","","","","","","","First Dose","Booster"]`
	dataRow := `["RFID-FMD-1","","","","","","","","","","","2026-03-20","2026-09-20"]`
	data := `{"values":[` + header + `,` + doseRow + `,` + dataRow + `]}`
	if err := os.WriteFile(dir+"/vaccination.json", []byte(data), 0644); err != nil {
		t.Fatalf("write vaccination source: %v", err)
	}

	cells, err := loadVaccinationCells(dir)
	if err != nil {
		t.Fatalf("load vaccination cells: %v", err)
	}

	total := countDatedFacts(cells)
	if total != 2 {
		t.Fatalf("dated facts parsed = %d, want 2 (FMD first + FMD booster)", total)
	}

	matrix := buildCanonicalVaccinationMatrix()
	var reconciled, laterAdmins int
	for _, c := range cells {
		val := strings.TrimSpace(c.Value)
		if val == "" || strings.EqualFold(val, "NA") || strings.EqualFold(val, "Pending") {
			continue
		}
		doseCode, later := mapSheetDoseToRuleCode(c.Vaccine, c.DoseCode, "adult", matrix)
		if doseCode == "" {
			t.Fatalf("FMD cell (dose=%s) failed to resolve a dose code — silently dropped", c.DoseCode)
		}
		reconciled++
		if later {
			laterAdmins++
		}
	}
	if reconciled != total {
		t.Fatalf("reconciled=%d, want %d (zero silent drops)", reconciled, total)
	}
	if laterAdmins != 1 {
		t.Fatalf("later administrations reconciled=%d, want 1 (the FMD booster cell)", laterAdmins)
	}
}

type seedPartialGenerationErr struct{}

func (seedPartialGenerationErr) Error() string {
	return "partial generation"
}

func (seedPartialGenerationErr) Is(target error) bool {
	return target != nil && target.Error() == "vaccination: generation completed with failed goats"
}
