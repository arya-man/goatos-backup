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

	selected, _, err := selectIDsWithinVisitShotCapForSession([]domain.UnbatchedDue{
		{ObligationID: "expired", TargetID: "goat-1", DueAt: planned, WindowEnd: &expired},
		{ObligationID: "open", TargetID: "goat-2", DueAt: planned, WindowEnd: &open},
	}, &planned, planner, "FMD", 5, NewSweepSession())
	if err != nil {
		t.Fatalf("selectIDsWithinVisitShotCapForSession: %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"open"}) {
		t.Fatalf("selected = %#v, want only row still safe on planned date", selected)
	}
}

func TestSelectParkIDsWithinVisitShotCapForSessionFiltersUnsafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	open := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxShotsPerAnimalPerDrive = 2

	selected, _, err := selectParkIDsWithinVisitShotCapForSession([]domain.ParkConsolidationCandidate{
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
