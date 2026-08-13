package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestSelectIDsWithinVisitShotCapForSessionFiltersUnsafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	open := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxShotsPerAnimalPerDrive = 2

	selected, _, err := selectIDsWithinVisitShotCapForSession(planned, []domain.UnbatchedDue{
		{ObligationID: "expired", TargetID: "goat-1", DueAt: planned, WindowEnd: &expired},
		{ObligationID: "open", TargetID: "goat-2", DueAt: planned, WindowEnd: &open},
	}, &planned, planner, RuleVaccineIdentity{VaccineCode: "FMD", VaccinePriority: 5, VaccineType: "killed"}, NewSweepSession())
	if err != nil {
		t.Fatalf("selectIDsWithinVisitShotCapForSession: %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"open"}) {
		t.Fatalf("selected = %#v, want only row still safe on planned date", selected)
	}
}

func TestVaccineFeasibleOnPlannerDateBlocksThirdSameDayVaccine(t *testing.T) {
	planned := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	session := NewSweepSession()
	session.rememberPlannedVaccine("goat-1", planned, RuleVaccineIdentity{VaccineCode: "ET_TT", VaccineType: "killed"})
	session.rememberPlannedVaccine("goat-1", planned, RuleVaccineIdentity{VaccineCode: "PPR", VaccineType: "live"})

	candidate := driveCandidate{
		TargetID:  "goat-1",
		DueAt:     planned,
		WindowEnd: &planned,
	}
	if session.vaccineFeasibleOnPlannerDate(planned, planned, candidate, planner, RuleVaccineIdentity{VaccineCode: "BLUE_TONGUE", VaccineType: "killed"}) {
		t.Fatal("third same-day vaccine was feasible; want blocked by max two vaccines per animal session")
	}
	if !session.vaccineFeasibleOnPlannerDate(planned, planned, candidate, planner, RuleVaccineIdentity{VaccineCode: "PPR", VaccineType: "live"}) {
		t.Fatal("same vaccine re-check should not count as a third distinct same-day vaccine")
	}
}

func TestVaccineFeasibleOnPlannerDateBlocksUnapprovedSameDayPair(t *testing.T) {
	planned := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	session := NewSweepSession()
	session.rememberPlannedVaccine("goat-1", planned, RuleVaccineIdentity{VaccineCode: "FMD", VaccineType: "killed"})

	candidate := driveCandidate{TargetID: "goat-1", DueAt: planned, WindowEnd: &planned}
	if session.vaccineFeasibleOnPlannerDate(planned, planned, candidate, planner, RuleVaccineIdentity{VaccineCode: "PPR", VaccineType: "live"}) {
		t.Fatal("FMD+PPR same-day pair was feasible; want blocked because it is not an approved combo")
	}
	if !session.vaccineFeasibleOnPlannerDate(planned, planned, candidate, planner, RuleVaccineIdentity{VaccineCode: "HS", VaccineType: "killed"}) {
		t.Fatal("FMD+HS should remain feasible as an approved same-day combo")
	}
}

func TestScoreUnbatchedDriveDateRanksMedicalWindowBeforeOverflowDensity(t *testing.T) {
	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	inWindowDay := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	overflowDay := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	hsWindowEnd := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	looseWindowEnd := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	rows := []domain.UnbatchedDue{
		{ObligationID: "hs-tight-1", TargetID: "tight-1", DueAt: now, WindowEnd: &hsWindowEnd},
		{ObligationID: "hs-tight-2", TargetID: "tight-2", DueAt: now, WindowEnd: &hsWindowEnd},
		{ObligationID: "hs-loose-1", TargetID: "loose-1", DueAt: now, WindowEnd: &looseWindowEnd},
		{ObligationID: "hs-loose-2", TargetID: "loose-2", DueAt: now, WindowEnd: &looseWindowEnd},
		{ObligationID: "hs-loose-3", TargetID: "loose-3", DueAt: now, WindowEnd: &looseWindowEnd},
	}
	inWindow := scoreUnbatchedDriveDate(now, inWindowDay, rows, []string{"hs-tight-1", "hs-tight-2"}, planner)
	overflow := scoreUnbatchedDriveDate(now, overflowDay, rows, []string{"hs-tight-1", "hs-tight-2", "hs-loose-1", "hs-loose-2", "hs-loose-3"}, planner)

	if !inWindow.betterThan(overflow) {
		t.Fatalf("in-window score %#v should beat denser overflow score %#v", inWindow, overflow)
	}
}

func TestSelectParkIDsWithinVisitShotCapForSessionFiltersUnsafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	open := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxShotsPerAnimalPerDrive = 2

	selected, _, err := selectParkIDsWithinVisitShotCapForSession(planned, []domain.ParkConsolidationCandidate{
		{ObligationID: "expired", RuleID: "rule-fmd", TargetID: "goat-1", DueAt: planned, WindowEnd: &expired},
		{ObligationID: "open", RuleID: "rule-fmd", TargetID: "goat-2", DueAt: planned, WindowEnd: &open},
	}, []string{"expired", "open"}, &planned, planner, func(string) RuleVaccineIdentity {
		return RuleVaccineIdentity{VaccineCode: "FMD", VaccinePriority: 5}
	}, NewSweepSession())
	if err != nil {
		t.Fatalf("selectParkIDsWithinVisitShotCapForSession: %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"open"}) {
		t.Fatalf("selected = %#v, want only park row still safe on planned date", selected)
	}
}

// TestSweepSessionDriveCapacityNotResetOnRefresh verifies VAXCAP-007: preflight does not lose
// earlier capacity claims when the next group refreshes the DB capacity counter.
func TestSweepSessionDriveCapacityNotResetOnRefresh(t *testing.T) {
	parkID := "park-1"
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	session := NewSweepSession()

	// First group seeds the baseline from DB: 5 cells persisted.
	session.resetDriveCapacity(parkID, date, 5)
	if got := session.driveCapacityUsed(parkID, date); got != 5 {
		t.Fatalf("initial capacity = %d, want 5", got)
	}

	// First group claims 3 cells (in-memory).
	claim1 := session.claimDriveCapacity(parkID, date, "obl-1", 3)
	if got := session.driveCapacityUsed(parkID, date); got != 8 {
		t.Fatalf("after first claim = %d, want 8 (5 + 3)", got)
	}

	// Second group calls refresh, which should NOT reset the count to DB baseline since already loaded.
	session.resetDriveCapacity(parkID, date, 5)
	if got := session.driveCapacityUsed(parkID, date); got != 8 {
		t.Fatalf("after second group refresh = %d, want 8 (not reset to 5)", got)
	}

	// Second group can now add its own claims on top.
	session.claimDriveCapacity(parkID, date, "obl-2", 2)
	if got := session.driveCapacityUsed(parkID, date); got != 10 {
		t.Fatalf("after second claim = %d, want 10 (5 + 3 + 2)", got)
	}

	// Releasing individual claims works correctly.
	session.releaseDriveCapacityClaims([]driveCapacityReservation{claim1})
	if got := session.driveCapacityUsed(parkID, date); got != 7 {
		t.Fatalf("after release claim1 = %d, want 7 (5 + 2)", got)
	}
}

func TestSweepSessionDriveCapacityDeduplicatesAnimalAcrossCompatibleVaccineLanes(t *testing.T) {
	date := time.Date(2027, 7, 24, 0, 0, 0, 0, time.UTC)
	session := NewSweepSession()
	first := session.claimDriveCapacity("park-1", date, "sheep-1", 1)
	duplicate := session.claimDriveCapacity("park-1", date, "sheep-1", 1)
	if got := session.driveCapacityUsed("park-1", date); got != 1 {
		t.Fatalf("animal capacity = %d, want 1 for two vaccine lanes on the same sheep", got)
	}
	session.releaseDriveCapacityClaims([]driveCapacityReservation{duplicate})
	if got := session.driveCapacityUsed("park-1", date); got != 1 {
		t.Fatalf("duplicate claim release changed capacity to %d, want 1", got)
	}
	session.releaseDriveCapacityClaims([]driveCapacityReservation{first})
}

func TestSheepPoxBlueTongueComboIsSymmetric(t *testing.T) {
	day := time.Date(2027, 7, 24, 0, 0, 0, 0, time.UTC)
	session := NewSweepSession()
	session.rememberPlannedVaccine("sheep-1", day, RuleVaccineIdentity{VaccineCode: "SHEEP_POX", VaccineType: "live"})
	if !session.sameDayCompatibleWithPlannedVaccines("sheep-1", RuleVaccineIdentity{VaccineCode: "BLUE_TONGUE", VaccineType: "killed"}, day) {
		t.Fatal("Blue Tongue must remain compatible when Sheep Pox was planned first")
	}

	reverse := NewSweepSession()
	reverse.rememberPlannedVaccine("sheep-1", day, RuleVaccineIdentity{VaccineCode: "BLUE_TONGUE", VaccineType: "killed"})
	if !reverse.sameDayCompatibleWithPlannedVaccines("sheep-1", RuleVaccineIdentity{VaccineCode: "SHEEP_POX", VaccineType: "live"}, day) {
		t.Fatal("Sheep Pox must remain compatible when Blue Tongue was planned first")
	}
}

// TestSweepSessionDriveCapacityMultipleParkDates verifies capacity is tracked independently per
// park/date combination, and reset only happens once per unique key in a session.
func TestSweepSessionDriveCapacityMultipleParkDates(t *testing.T) {
	session := NewSweepSession()
	parkID := "park-1"
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	// Seed D1 with 10 cells, D2 with 20 cells.
	session.resetDriveCapacity(parkID, d1, 10)
	session.resetDriveCapacity(parkID, d2, 20)

	// Claim 5 on D1.
	claim1 := session.claimDriveCapacity(parkID, d1, "obl-1", 5)
	if got := session.driveCapacityUsed(parkID, d1); got != 15 {
		t.Fatalf("D1 after claim = %d, want 15 (10 + 5)", got)
	}
	if got := session.driveCapacityUsed(parkID, d2); got != 20 {
		t.Fatalf("D2 should be independent = %d, want 20", got)
	}

	// Claim 8 on D2.
	session.claimDriveCapacity(parkID, d2, "obl-2", 8)
	if got := session.driveCapacityUsed(parkID, d2); got != 28 {
		t.Fatalf("D2 after claim = %d, want 28 (20 + 8)", got)
	}

	// Release one claim from D1, verify D2 unaffected.
	session.releaseDriveCapacityClaims([]driveCapacityReservation{claim1})
	if got := session.driveCapacityUsed(parkID, d1); got != 10 {
		t.Fatalf("D1 after release = %d, want 10", got)
	}
	if got := session.driveCapacityUsed(parkID, d2); got != 28 {
		t.Fatalf("D2 should not be affected by D1 release = %d, want 28", got)
	}
}

// TestSweepSessionAdoptsHigherPersistedOnRefresh is the C-2 guard (session half): date probes
// release the park/date advisory lock between probes, so another worker can commit cells in the
// gap. A later refresh carrying a HIGHER persisted count must be adopted (monotonically) without
// resetting this session's claims; a lower/equal refresh must never clobber claims (VAXCAP-007).
func TestSweepSessionAdoptsHigherPersistedOnRefresh(t *testing.T) {
	parkID := "park-1"
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	session := NewSweepSession()

	// Probe 1 seeds baseline 4 and claims 3 in-session (total 7).
	session.resetDriveCapacity(parkID, date, 4)
	session.claimDriveCapacity(parkID, date, "obl-1", 3)
	if got := session.driveCapacityUsed(parkID, date); got != 7 {
		t.Fatalf("after claim = %d, want 7", got)
	}

	// Concurrent worker B committed 5 more cells; final re-lock reads persisted 9 (> 7).
	session.resetDriveCapacity(parkID, date, 9)
	if got := session.driveCapacityUsed(parkID, date); got != 9 {
		t.Fatalf("after higher persisted refresh = %d, want 9 (B's cells observed)", got)
	}

	// A refresh at or below the running count must not reset claims (VAXCAP-007 preserved).
	session.resetDriveCapacity(parkID, date, 4)
	if got := session.driveCapacityUsed(parkID, date); got != 9 {
		t.Fatalf("after lower persisted refresh = %d, want 9 (never lower, claims kept)", got)
	}
	session.claimDriveCapacity(parkID, date, "obl-2", 2)
	session.resetDriveCapacity(parkID, date, 9)
	if got := session.driveCapacityUsed(parkID, date); got != 11 {
		t.Fatalf("claims after adopt = %d, want 11 (claims accumulate on top)", got)
	}
}
