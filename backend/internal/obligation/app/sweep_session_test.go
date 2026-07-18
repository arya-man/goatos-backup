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
