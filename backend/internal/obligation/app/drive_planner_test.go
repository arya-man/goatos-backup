package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestComboSessionKeyFMDHS(t *testing.T) {
	if got := comboSessionKey("FMD"); got != "combo:FMD+HS" {
		t.Fatalf("FMD session = %q, want combo:FMD+HS", got)
	}
	if got := comboSessionKey("HS"); got != "combo:FMD+HS" {
		t.Fatalf("HS session = %q, want combo:FMD+HS", got)
	}
}

func TestBatchSessionUsesComboWhenKnown(t *testing.T) {
	if got := batchSession("rule-fmd", "FMD"); got != "combo:FMD+HS" {
		t.Fatalf("session = %q, want combo:FMD+HS", got)
	}
	if got := batchSession("rule-ppr", "PPR"); got != "combo:PPR+Blue Tongue" {
		t.Fatalf("session = %q, want combo:PPR+Blue Tongue", got)
	}
	if got := batchSession("rule-gpox", "Goat Pox"); got != "rule:rule-gpox" {
		t.Fatalf("session = %q, want rule:rule-gpox", got)
	}
}

func TestVaccineMatrixPriority(t *testing.T) {
	if got := VaccineMatrixPriority("ET+TT"); got != 1 {
		t.Fatalf("ET+TT priority = %d, want 1", got)
	}
	if got := VaccineMatrixPriority("PPR"); got != 2 {
		t.Fatalf("PPR priority = %d, want 2", got)
	}
	if got := VaccineMatrixPriority("Goat Pox"); got != 3 {
		t.Fatalf("Goat Pox priority = %d, want 3", got)
	}
	if got := VaccineMatrixPriority("FMD"); got != 4 {
		t.Fatalf("FMD priority = %d, want 4", got)
	}
}

func TestDrivePlannerFromRuleDSLUsesMatrixPriority(t *testing.T) {
	_, planner := DrivePlannerFromRuleDSL([]byte(`{"vaccine":{"code":"PPR"}}`))
	if planner.VaccinePriority != 2 {
		t.Fatalf("PPR planner priority = %d, want 2", planner.VaccinePriority)
	}
	if planner.MaxGoatsPerDrive != 0 {
		t.Fatalf("max goats = %d, want unlimited (0)", planner.MaxGoatsPerDrive)
	}
}

func TestPickBestDriveDatePrefersMaxCoverage(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	rows := []driveCandidate{
		{ObligationID: "a", DueAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
		{ObligationID: "b", DueAt: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
	}
	got := pickBestDriveDate(now, rows, 2)
	if got == nil {
		t.Fatal("expected planned date")
	}
	// Both obligations fit on Jul 8; scorer prefers later date with equal coverage when urgency rises.
	want := businessDate(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))
	if !got.Equal(want) {
		t.Fatalf("planned=%s want %s", got, want)
	}
}

func TestSplitObligationIDsByMaxGoats(t *testing.T) {
	ids := []string{"1", "2", "3", "4", "5"}
	chunks := splitObligationIDs(ids, 2)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d want 3", len(chunks))
	}
	if len(chunks[0]) != 2 || len(chunks[1]) != 2 || len(chunks[2]) != 1 {
		t.Fatalf("unexpected chunk sizes: %#v", chunks)
	}
}

func TestSpeciesGroupingKeyUsesKidMixedByDefault(t *testing.T) {
	goatKid := domain.UnbatchedDue{TargetAnimalStage: "K1", TargetSpecies: "goat"}
	sheepKid := domain.UnbatchedDue{TargetAnimalStage: "K2", TargetSpecies: "sheep"}
	if sweepWindowGroupKey(goatKid, "kid_mixed") != sweepWindowGroupKey(sheepKid, "kid_mixed") {
		t.Fatal("kid goat and sheep should share kid_mixed sweep group")
	}
	adult := domain.UnbatchedDue{TargetAnimalStage: "ADULT", TargetSpecies: "goat"}
	if sweepWindowGroupKey(goatKid, "kid_mixed") == sweepWindowGroupKey(adult, "kid_mixed") {
		t.Fatal("adult goat must not share kid_mixed group with kids")
	}
}

func TestDrivePlannerSettingsIncludeBatchingHoldDefaults(t *testing.T) {
	got := domain.DefaultDrivePlannerSettings()
	if got.MaxBatchingHoldDays != 7 || got.MaxBatchingHoldCount != 1 || got.SpeciesGroupingPolicy != "kid_mixed" {
		t.Fatalf("defaults=%#v, want batching hold + kid_mixed policy", got)
	}
}

func TestNormalizedDrivePlannerSettingsDefaults(t *testing.T) {
	got := normalizedDrivePlannerSettings(domain.DrivePlannerSettings{Enabled: true}, "PPR")
	if got.VaccinePriority != 2 {
		t.Fatalf("priority = %d, want matrix PPR=2", got.VaccinePriority)
	}
	if got.ComboAlignWindowDays != domain.DefaultDrivePlannerSettings().ComboAlignWindowDays {
		t.Fatalf("combo window = %d", got.ComboAlignWindowDays)
	}
}
