package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestDriveAssignmentCountersKeepAnimalAndDoseGrainsApart pins the two DIFFERENT grains a drive
// assignment row carries, at the point the sweeper produces them:
//
//	animal_count = COUNT(DISTINCT animal) in the cell -- the OPERATOR CAP unit. AGENTS.md: capacity
//	               is "unique animals per assigned operator/day", so an animal needing two vaccines
//	               from the same operator on the same day consumes ONE cap slot, not two.
//	total_doses  = COUNT(DISTINCT (animal, rule)) in the cell -- the DOSE/STOCK unit. That same
//	               animal consumes TWO doses.
//
// Getting this backwards is a real defect in either direction: counting the two-vaccine animal
// twice under-fills every operator's day, and counting two animals as one over-fills it.
//
// This test is written so that COLLAPSING the two grains fails it: a fixture where the cell's
// animal count and its dose count are deliberately different (3 animals, 4 doses -- one animal
// needs both vaccines) rejects both `animal_count = number of obligations` and
// `total_doses = animal_count x cardinality(vaccine_rule_ids)` (which would say 3 x 2 = 6).
func TestDriveAssignmentCountersKeepAnimalAndDoseGrainsApart(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	batch := domain.NewBatch{ScopeID: "park-1", PlannedDate: &planned}
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", TargetID: "goat-1", ParkID: "park-1", ScopeType: "shed", ScopeID: "shed-1", ShedName: "Gandhi 1", RuleID: "rule-ppr"},
		{ObligationID: "obl-2", TargetID: "goat-2", ParkID: "park-1", ScopeType: "shed", ScopeID: "shed-1", ShedName: "Gandhi 1", RuleID: "rule-ppr"},
		{ObligationID: "obl-3", TargetID: "goat-3", ParkID: "park-1", ScopeType: "shed", ScopeID: "shed-1", ShedName: "Gandhi 1", RuleID: "rule-ppr"},
		// goat-1 also needs the second vaccine in the SAME cell on the SAME day.
		{ObligationID: "obl-4", TargetID: "goat-1", ParkID: "park-1", ScopeType: "shed", ScopeID: "shed-1", ShedName: "Gandhi 1", RuleID: "rule-fmd"},
	}

	got := driveAssignmentsForUnbatched("batch-1", batch, rows)
	if len(got) != 1 {
		t.Fatalf("assignments = %d, want ONE row for the one (park, shed, physical shed, partition) cell: %#v", len(got), got)
	}
	row := got[0]
	if row.AnimalCount != 3 {
		t.Fatalf("animal_count = %d, want 3 DISTINCT animals -- the two-vaccine animal consumes ONE operator cap slot, not two (obligations in the cell = %d)",
			row.AnimalCount, len(rows))
	}
	if row.TotalDoses != 4 {
		t.Fatalf("total_doses = %d, want 4 DISTINCT (animal, rule) doses -- not animal_count x cardinality(vaccine_rule_ids) = %d",
			row.TotalDoses, row.AnimalCount*int32(len(row.VaccineRuleIDs)))
	}
	if len(row.VaccineRuleIDs) != 2 {
		t.Fatalf("vaccine_rule_ids = %#v, want both vaccines planned in the cell", row.VaccineRuleIDs)
	}
}

// TestDriveAssignmentOperatorCapChargesTwoVaccineAnimalOnce is the cap half of the same rule, on the
// planner rather than the producer: the OperatorDrivePlanner is fed the cell's ANIMAL count, so a
// cell of 3 animals carrying 4 doses fits an operator whose remaining cap is 3. If the planner were
// ever fed doses instead (or the row's animal_count were sized per dose), this cell would be split
// across operators or spill over cap for work that is really one operator-day of animals.
func TestDriveAssignmentOperatorCapChargesTwoVaccineAnimalOnce(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1"}}
	svc := NewSweeperService(repo, nil, nil)
	assignments := []domain.DriveAssignment{{
		BatchID:        "batch-1",
		PlannedDate:    planned,
		ParkID:         "park-1",
		ShedID:         strPtr("shed-1"),
		PhysicalShed:   "Gandhi",
		PartitionLabel: "1",
		AnimalCount:    3,
		VaccineRuleIDs: []string{"rule-fmd", "rule-ppr"},
		TotalDoses:     4,
		CapacityStatus: "within_cap",
	}}

	got, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", domain.NewBatch{
		ScopeID: "park-1", PlannedDate: &planned,
	}, 3, assignments, NewSweepSession())
	if err != nil {
		t.Fatalf("distribute assignments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("assignments = %d, want ONE row: 3 unique animals fit a cap of 3 even though they carry 4 doses: %#v", len(got), got)
	}
	if got[0].AnimalCount != 3 || got[0].TotalDoses != 4 {
		t.Fatalf("planned row = %d animals / %d doses, want 3/4 preserved through the planner: %#v",
			got[0].AnimalCount, got[0].TotalDoses, got[0])
	}
	if got[0].CapacityStatus != "within_cap" {
		t.Fatalf("capacity_status = %q, want within_cap: 3 unique animals against a cap of 3 is not over cap: %#v",
			got[0].CapacityStatus, got[0])
	}
}
