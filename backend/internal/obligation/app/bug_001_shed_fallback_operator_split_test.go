package app

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestShedFallbackPersistsDistributedOperatorSplit is the BUG-001 red test.
//
// It drives the PRODUCTION entrypoint SweepVersion down the vaccination shed-fallback batching
// path (shed-scoped unbatched rows -> park-scoped drive batch, batchDueGroup), with two partitions
// of two animals each and two operators capped at two animals each -- so the cap-aware
// distribution plan MUST place one partition on each operator.
//
// The assertion is on the payload handed to ReplaceVaccinationDriveAssignmentsForBatch, which is
// the exact row set the Postgres adapter writes into vaccination_drive_assignments (it deletes the
// batch's rows and re-inserts precisely this slice -- visit_shot_lock.go
// ReplaceVaccinationDriveAssignmentsForBatch).
//
// Before the fix, batchDueGroup computed the distributed plan and DISCARDED it, rebuilding the
// persisted rows from driveAssignmentsForUnbatched, which stamps batch.ConductedBy (one operator)
// on every row -- collapsing the split onto a single operator.
func TestShedFallbackPersistsDistributedOperatorSplit(t *testing.T) {
	due := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	base := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi 1", TargetID: "goat-1", DueAt: due},
			{ObligationID: "obl-2", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi 1", TargetID: "goat-2", DueAt: due},
			{ObligationID: "obl-3", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi 2", TargetID: "goat-3", DueAt: due},
			{ObligationID: "obl-4", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi 2", TargetID: "goat-4", DueAt: due},
		},
		attachAll:     true,
		createBatchID: "batch-1",
	}
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: base,
		operators:     []string{"op-1", "op-2"},
		operatorCaps:  map[string]int32{"op-1": 2, "op-2": 2},
	}
	svc := NewSweeperService(repo, nil, nil)

	if _, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineCode: "PPR",
	}, due); err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}

	if len(base.replaceDriveAssignments) != 1 {
		t.Fatalf("ReplaceVaccinationDriveAssignmentsForBatch calls = %d, want exactly 1 persisted set; recorded=%#v",
			len(base.replaceDriveAssignments), base.replaceDriveAssignments)
	}
	persisted := base.replaceDriveAssignments[0]

	animalsByOperator := map[string]int32{}
	for _, assignment := range persisted {
		if assignment.OperatorID == nil {
			t.Fatalf("persisted drive assignment has no operator: %#v (persisted=%#v)", assignment, persisted)
		}
		if assignment.BatchID != "batch-1" {
			t.Fatalf("persisted drive assignment batch = %q, want batch-1: %#v", assignment.BatchID, assignment)
		}
		animalsByOperator[*assignment.OperatorID] += assignment.AnimalCount
	}

	gotOperators := make([]string, 0, len(animalsByOperator))
	for operatorID := range animalsByOperator {
		gotOperators = append(gotOperators, operatorID)
	}
	sort.Strings(gotOperators)
	if len(gotOperators) != 2 {
		t.Fatalf("persisted operators = %v, want the distributed plan's two operators; persisted=%#v", gotOperators, persisted)
	}
	for _, operatorID := range gotOperators {
		if animalsByOperator[operatorID] != 2 {
			t.Fatalf("operator %s persisted animal_count = %d, want 2 (one 2-animal partition each); persisted=%#v",
				operatorID, animalsByOperator[operatorID], persisted)
		}
	}

	var total int32
	for _, assignment := range persisted {
		total += assignment.AnimalCount
	}
	if total != 4 {
		t.Fatalf("persisted animal_count total = %d, want 4 attached animals; persisted=%#v", total, persisted)
	}
}
