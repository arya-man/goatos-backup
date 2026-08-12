package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
)

func TestSweeperSkipsSideEffectsWhenAttachClaimsNoRows(t *testing.T) {
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{{ObligationID: "obl-1", ScopeType: "park", ScopeID: "park-1"}},
	}
	tasks := &fakeSweepTaskCreator{}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		SOPVersionID:  "sop-version-1",
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 0 || result.Obligations != 0 {
		t.Fatalf("result = %#v, want no created work", result)
	}
	if tasks.calls != 0 {
		t.Fatalf("tasks created = %d, want 0", tasks.calls)
	}
	if reserver.calls != 0 {
		t.Fatalf("reservations = %d, want 0", reserver.calls)
	}
	if repo.setTaskCalls != 0 {
		t.Fatalf("batch task links = %d, want 0", repo.setTaskCalls)
	}
}

func strPtr(value string) *string {
	return &value
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestDriveAssignmentsForUnbatchedPersistPhysicalShedPartitions(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	operatorID := "operator-1"
	assignments := driveAssignmentsForUnbatched("batch-1", domain.NewBatch{
		ScopeID:     "park-1",
		PlannedDate: &planned,
		ConductedBy: &operatorID,
	}, []domain.UnbatchedDue{
		{ObligationID: "obl-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi - Part 1", TargetID: "goat-1"},
		{ObligationID: "obl-2", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", ShedName: "Gandhi - Part 1", TargetID: "goat-2"},
		{ObligationID: "obl-3", ScopeType: "shed", ScopeID: "shed-2", ParkID: "park-1", ShedName: "Gandhi - Part 2", TargetID: "goat-3"},
		{ObligationID: "obl-4", ScopeType: "shed", ScopeID: "shed-3", ParkID: "park-1", ShedName: "Gandhi 3", TargetID: "goat-4"},
		{ObligationID: "obl-5", ScopeType: "shed", ScopeID: "shed-4", ParkID: "park-1", ShedName: "Old Yashoda", TargetID: "goat-5"},
	})
	if len(assignments) != 4 {
		t.Fatalf("assignments = %d, want 4: %#v", len(assignments), assignments)
	}
	got := map[string]int32{}
	for _, assignment := range assignments {
		got[assignment.PhysicalShed+"|"+assignment.PartitionLabel] = assignment.AnimalCount
	}
	want := map[string]int32{
		"Gandhi - Part 1|whole": 2,
		"Gandhi - Part 2|whole": 1,
		"Gandhi 3|whole":        1,
		"Old Yashoda|whole":     1,
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("assignment %s = %d, want %d; all=%#v", key, got[key], count, got)
		}
	}
}

func TestDriveAssignmentsForParkConsolidationPersistPartSuffixPartitions(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	assignments := driveAssignmentsForParkConsolidation("batch-1", domain.NewBatch{
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}, []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", ShedID: "shed-1", ShedName: "Godel 1 - Part 4", ParkID: "park-1", TargetID: "goat-1"},
		{ObligationID: "obl-2", ShedID: "shed-1", ShedName: "Godel 1 - Part 4", ParkID: "park-1", TargetID: "goat-1"},
	})
	if len(assignments) != 1 {
		t.Fatalf("assignments = %d, want 1: %#v", len(assignments), assignments)
	}
	if assignments[0].PhysicalShed != "Godel 1 - Part 4" || assignments[0].PartitionLabel != "whole" || assignments[0].AnimalCount != 1 {
		t.Fatalf("assignment = %#v, want exact shed Godel 1 - Part 4 with one distinct animal", assignments[0])
	}
}

func TestDistributeVaccinationDriveAssignmentsBalancesAvailableOperators(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1", "op-2", "op-3"}}
	svc := NewSweeperService(repo, nil, nil)
	assignments := []domain.DriveAssignment{
		{BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1", PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 90, CapacityStatus: "within_cap"},
		{BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1", PhysicalShed: "Gandhi", PartitionLabel: "2", AnimalCount: 80, CapacityStatus: "within_cap"},
		{BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1", PhysicalShed: "Gandhi", PartitionLabel: "3", AnimalCount: 90, CapacityStatus: "within_cap"},
	}

	got, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", domain.NewBatch{
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}, 200, assignments, NewSweepSession())
	if err != nil {
		t.Fatalf("distribute assignments: %v", err)
	}
	seen := map[string]bool{}
	for _, assignment := range got {
		if assignment.OperatorID == nil {
			t.Fatalf("assignment missing operator: %#v", assignment)
		}
		seen[*assignment.OperatorID] = true
	}
	for _, operatorID := range repo.operators {
		if !seen[operatorID] {
			t.Fatalf("operator %s got no work; assignments=%#v", operatorID, got)
		}
	}
}

func TestDistributeVaccinationDriveAssignmentsSplitsOversizedPartitionWithPlanner(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1", "op-2"}}
	svc := NewSweeperService(repo, nil, nil)
	assignments := []domain.DriveAssignment{{
		BatchID:        "batch-1",
		PlannedDate:    planned,
		ParkID:         "park-1",
		ShedID:         strPtr("shed-1"),
		PhysicalShed:   "Gandhi",
		PartitionLabel: "1",
		AnimalCount:    120,
		CapacityStatus: "within_cap",
	}}

	got, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", domain.NewBatch{
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}, 50, assignments, NewSweepSession())
	if err != nil {
		t.Fatalf("distribute assignments: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("assignments = %d, want two operator chunks plus residual capacity action for oversized partition: %#v", len(got), got)
	}
	totals := map[string]int32{}
	residualSeen := false
	for _, assignment := range got {
		if assignment.OperatorID == nil {
			if assignment.AnimalCount != 20 || assignment.CapacityStatus != "capacity_action" {
				t.Fatalf("unassigned residual = %#v, want 20-animal capacity_action", assignment)
			}
			residualSeen = true
			continue
		}
		if assignment.PhysicalShed != "Gandhi" || assignment.PartitionLabel != "1" {
			t.Fatalf("assignment lost partition grain: %#v", assignment)
		}
		totals[*assignment.OperatorID] += assignment.AnimalCount
		if !containsString(assignment.Warnings, "partition split because one partition exceeded available operator capacity") {
			t.Fatalf("assignment missing split warning: %#v", assignment)
		}
	}
	if totals["op-1"] != 50 || totals["op-2"] != 50 {
		t.Fatalf("operator totals = %#v, want both operators at cap with no over-cap top-up", totals)
	}
	if !residualSeen {
		t.Fatalf("expected a capacity-action residual for the oversized partition: %#v", got)
	}
}

func TestDistributeVaccinationDriveAssignmentsCarriesWholePartitionPastResidualOperatorCaps(t *testing.T) {
	planned := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		operators:     []string{"op-low", "op-high"},
		operatorCaps:  map[string]int32{"op-low": 1, "op-high": 3},
	}
	svc := NewSweeperService(repo, nil, nil)
	assignments := []domain.DriveAssignment{{
		BatchID:        "batch-1",
		PlannedDate:    planned,
		ParkID:         "park-1",
		ShedID:         strPtr("shed-1"),
		PhysicalShed:   "Gandhi",
		PartitionLabel: "1",
		AnimalCount:    4,
		CapacityStatus: "within_cap",
	}}

	got, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", domain.NewBatch{
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}, 200, assignments, NewSweepSession())
	if err != nil {
		t.Fatalf("distribute assignments: %v", err)
	}
	totals := map[string]int32{}
	residualSeen := false
	for _, assignment := range got {
		if assignment.OperatorID == nil {
			if assignment.AnimalCount != 4 || assignment.CapacityStatus != "capacity_action" {
				t.Fatalf("unassigned partition = %#v, want intact 4-animal capacity_action", assignment)
			}
			residualSeen = true
			continue
		}
		totals[*assignment.OperatorID] += assignment.AnimalCount
	}
	if totals["op-low"] != 0 || totals["op-high"] != 0 {
		t.Fatalf("operator totals = %#v, want no residual-cap partition split", totals)
	}
	if !residualSeen {
		t.Fatalf("expected intact unassigned partition for capacity action: %#v", got)
	}
}

func TestOperatorCapacityPlannerScalesByAvailableOperators(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1", "op-2", "op-3"}}
	svc := NewSweeperService(repo, nil, nil)

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, NewSweepSession())
	if err != nil {
		t.Fatalf("operatorCapacityPlanner: %v", err)
	}
	if planner.MaxGoatsPerDrive != 600 {
		t.Fatalf("effective animal cap = %d, want 600 for 3 operators at 200 each", planner.MaxGoatsPerDrive)
	}
}

// Fail-closed regression (Codex P1): when a park HAS operator-assignment config but the
// resolver yields zero executable operators (all off/leave, missing shift config, or the
// resolved operator is not in the candidate set), availableVaccinationOperatorsForDrive
// returns domain.ErrOperatorAssignmentConfigPresentButEmpty. operatorCapacityPlanner must
// then fail CLOSED by scaling MaxGoatsPerDrive to 0 so driveOperatorCapacityExhausted fires
// and the sweeper defers the day — it must NOT silently return the original base cap.
func TestOperatorCapacityPlannerFailsClosedOnConfigPresentButEmpty(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		returnErr:     domain.ErrOperatorAssignmentConfigPresentButEmpty,
	}
	svc := NewSweeperService(repo, nil, nil)
	original := domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, original, NewSweepSession())
	if err != nil {
		t.Fatalf("operatorCapacityPlanner returned error, want fail-closed nil error: %v", err)
	}
	if planner.MaxGoatsPerDrive != 0 {
		t.Fatalf("fail-closed cap = %d, want 0 (must NOT fall back to base cap 200)", planner.MaxGoatsPerDrive)
	}
	if !driveOperatorCapacityExhausted(original, planner) {
		t.Fatalf("driveOperatorCapacityExhausted = false, want true so the sweeper skips this day (fail closed)")
	}

	// effectiveOperatorAnimalCap must also fail closed to zero usable capacity.
	cap, err := svc.effectiveOperatorAnimalCap(context.Background(), "tenant-1", "park-1", &planned, 200, NewSweepSession())
	if err != nil {
		t.Fatalf("effectiveOperatorAnimalCap returned error, want fail-closed: %v", err)
	}
	if cap != 0 {
		t.Fatalf("effectiveOperatorAnimalCap = %d, want 0 (fail closed)", cap)
	}
}

// Config-ABSENT must stay unchanged: no config row => nil error, empty operator list =>
// the planner keeps its base cap (least-loaded fallback), NOT fail-closed.
func TestOperatorCapacityPlannerConfigAbsentKeepsBaseCap(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}} // no operators, no error
	svc := NewSweeperService(repo, nil, nil)
	original := domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, original, NewSweepSession())
	if err != nil {
		t.Fatalf("operatorCapacityPlanner: %v", err)
	}
	if planner.MaxGoatsPerDrive != 200 {
		t.Fatalf("config-absent cap = %d, want unchanged 200 (least-loaded fallback)", planner.MaxGoatsPerDrive)
	}
	if driveOperatorCapacityExhausted(original, planner) {
		t.Fatalf("driveOperatorCapacityExhausted = true for config-absent park, want false")
	}
}

func TestOperatorCapacityPlannerHonorsSingleOperatorHRMSCap(t *testing.T) {
	planned := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		operators:     []string{"op-1"},
		operatorCaps:  map[string]int32{"op-1": 1},
	}
	svc := NewSweeperService(repo, nil, nil)

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, NewSweepSession())
	if err != nil {
		t.Fatalf("operatorCapacityPlanner: %v", err)
	}
	if planner.MaxGoatsPerDrive != 1 {
		t.Fatalf("effective animal cap = %d, want HRMS single-operator cap 1", planner.MaxGoatsPerDrive)
	}
}

// F1 regression: totalVaccinationOperatorCap must NOT fall back to fallbackCap when operators
// WERE found but every one has 0 remaining capacity -- the real repository reports a fully-loaded
// operator as present with Cap=0 (GREATEST(daily_cap-loaded,0)), never absent from the list. The
// old code treated a summed total of 0 the same as "no operators found" and returned the full
// fallback batch cap, letting the planner still create/lock a drive onto operators who are all
// already at capacity.
func TestTotalVaccinationOperatorCapAllOperatorsZeroRemainingReturnsZeroNotFallback(t *testing.T) {
	operators := []domain.DriveOperatorCapacity{
		{OperatorID: "op-1", Cap: 0},
		{OperatorID: "op-2", Cap: 0},
		{OperatorID: "op-3", Cap: 0},
	}
	got := totalVaccinationOperatorCap("tenant-1", "park-1", time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), operators, 200, nil)
	if got != 0 {
		t.Fatalf("totalVaccinationOperatorCap = %d, want 0 (all operators found but zero remaining; must NOT fall back to 200)", got)
	}
}

func TestTotalVaccinationOperatorCapOneOperatorWithRemainingReturnsThatRemaining(t *testing.T) {
	operators := []domain.DriveOperatorCapacity{
		{OperatorID: "op-1", Cap: 0},
		{OperatorID: "op-2", Cap: 37},
		{OperatorID: "op-3", Cap: 0},
	}
	got := totalVaccinationOperatorCap("tenant-1", "park-1", time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), operators, 200, nil)
	if got != 37 {
		t.Fatalf("totalVaccinationOperatorCap = %d, want 37 (only op-2's remaining capacity)", got)
	}
}

func TestTotalVaccinationOperatorCapUncappedOperatorsSumToFallbackPerOperator(t *testing.T) {
	operators := []domain.DriveOperatorCapacity{
		{OperatorID: "op-1", Cap: 200},
		{OperatorID: "op-2", Cap: 200},
		{OperatorID: "op-3", Cap: 200},
	}
	got := totalVaccinationOperatorCap("tenant-1", "park-1", time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), operators, 200, nil)
	if got != 600 {
		t.Fatalf("totalVaccinationOperatorCap = %d, want 600 (3 uncapped operators at fallback-per-operator 200 each)", got)
	}
}

func TestTotalVaccinationOperatorCapNoOperatorsFoundReturnsFallbackUnchanged(t *testing.T) {
	got := totalVaccinationOperatorCap("tenant-1", "park-1", time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), nil, 200, nil)
	if got != 200 {
		t.Fatalf("totalVaccinationOperatorCap = %d, want fallbackCap 200 unchanged when no operators were found", got)
	}
}

// TestTotalVaccinationOperatorCapSubtractsSessionLoad is the isolated-function twin of
// TestOperatorCapacityPlannerHonorsCrossVersionOperatorDayLoad: proves totalVaccinationOperatorCap
// itself subtracts session.vaccinationOperatorLoad per operator, not just its production callers.
func TestTotalVaccinationOperatorCapSubtractsSessionLoad(t *testing.T) {
	planned := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	session := NewSweepSession()
	session.rememberVaccinationOperatorLoad("tenant-1", "park-1", planned, "op-1", 190)
	operators := []domain.DriveOperatorCapacity{{OperatorID: "op-1", Cap: 200}}
	got := totalVaccinationOperatorCap("tenant-1", "park-1", planned, operators, 200, session)
	if got != 10 {
		t.Fatalf("totalVaccinationOperatorCap = %d, want 10 (200 cap - 190 already reserved this session)", got)
	}
}

// TestOperatorCapacityPlannerAllOperatorsZeroRemainingDoesNotCreateBatch reproduces the REAL
// production caller path (operatorCapacityPlanner -> availableVaccinationOperatorsForDrive ->
// totalVaccinationOperatorCap), not just the isolated function: 3 operators are found but every
// one is already fully loaded (Cap 0 remaining). Before the F1 fix this returned the fallback cap
// (200), which a real sweep would then use to admit a full batch onto exhausted operators. After
// the fix the planner's effective MaxGoatsPerDrive is 0, so limitUnbatchedSelectionByDriveAnimals
// admits nothing and batchDueGroup does NOT create a batch.
func TestOperatorCapacityPlannerAllOperatorsZeroRemainingDoesNotCreateBatch(t *testing.T) {
	planned := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo:    &fakeSweepRepo{},
		operators:        []string{"op-1", "op-2", "op-3"},
		zeroCapOperators: map[string]bool{"op-1": true, "op-2": true, "op-3": true},
	}
	svc := NewSweeperService(repo, nil, nil)

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, NewSweepSession())
	if err != nil {
		t.Fatalf("operatorCapacityPlanner: %v", err)
	}
	if planner.MaxGoatsPerDrive != 0 {
		t.Fatalf("effective animal cap = %d, want 0 (3 operators found, all zero remaining -- must not fall back to 200)", planner.MaxGoatsPerDrive)
	}

	g := &dueGroup{
		scopeType: "park",
		scopeID:   "park-1",
		ruleID:    "rule-1",
		parkID:    "park-1",
		ids:       []string{"obl-1"},
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1", ParkID: "park-1", TargetID: "goat-1", DueAt: planned},
		},
	}
	unscaledPlanner := domain.DrivePlannerSettings{Enabled: true, MaxGoatsPerDrive: 200}
	batched, obligations, err := svc.batchDueGroup(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: unscaledPlanner,
	}, unscaledPlanner, planned, planned, NewSweepSession(), g)
	if err != nil {
		t.Fatalf("batchDueGroup: %v", err)
	}
	if batched || obligations != 0 || len(repo.createdBatches) != 0 {
		t.Fatalf("batched=%v obligations=%d batches=%#v, want no batch created onto fully-loaded operators", batched, obligations, repo.createdBatches)
	}
}

func TestVaccinationOperatorAvailabilityCachedAcrossPlannerAndAssignment(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1", "op-2", "op-3"}}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	batch := domain.NewBatch{
		TenantID:    "tenant-1",
		ScopeType:   "park",
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}

	planner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, session)
	if err != nil {
		t.Fatalf("operatorCapacityPlanner: %v", err)
	}
	if planner.MaxGoatsPerDrive != 600 {
		t.Fatalf("effective animal cap = %d, want 600", planner.MaxGoatsPerDrive)
	}
	if err := svc.assignVaccinationOperator(context.Background(), &batch, 200, session); err != nil {
		t.Fatalf("assignVaccinationOperator: %v", err)
	}
	if batch.ConductedBy == nil || *batch.ConductedBy != "op-1" {
		t.Fatalf("conducted by = %#v, want op-1", batch.ConductedBy)
	}
	assignments := []domain.DriveAssignment{{
		BatchID:        "batch-1",
		PlannedDate:    planned,
		ParkID:         "park-1",
		PhysicalShed:   "Gandhi",
		PartitionLabel: "Part 1",
		AnimalCount:    90,
		CapacityStatus: "within_cap",
	}}
	if _, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", batch, 200, assignments, session); err != nil {
		t.Fatalf("distributeVaccinationDriveAssignments: %v", err)
	}
	if repo.operatorListCalls != 1 {
		t.Fatalf("AvailableVaccinationOperatorsForDrive calls = %d, want 1 for shared park/date/cap session", repo.operatorListCalls)
	}
}

func TestDistributeVaccinationDriveAssignmentsHonorsCrossBatchOperatorDayLoad(t *testing.T) {
	planned := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1", "op-2", "op-3"}}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	batch := domain.NewBatch{
		TenantID:    "tenant-1",
		ScopeType:   "park",
		ScopeID:     "park-1",
		PlannedDate: &planned,
	}
	first := []domain.DriveAssignment{
		{BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1", PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: 160, CapacityStatus: "within_cap"},
		{BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1", PhysicalShed: "Godel 1", PartitionLabel: "Part 1", AnimalCount: 80, CapacityStatus: "within_cap"},
	}
	if _, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", batch, 200, first, session); err != nil {
		t.Fatalf("first distribute: %v", err)
	}
	second := []domain.DriveAssignment{{
		BatchID:        "batch-2",
		PlannedDate:    planned,
		ParkID:         "park-1",
		PhysicalShed:   "Mandela 2",
		PartitionLabel: "Part 8",
		AnimalCount:    80,
		CapacityStatus: "within_cap",
	}}
	got, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", batch, 200, second, session)
	if err != nil {
		t.Fatalf("second distribute: %v", err)
	}
	totals := map[string]int32{}
	for _, assignment := range got {
		if assignment.OperatorID == nil {
			t.Fatalf("assignment missing operator: %#v", assignment)
		}
		totals[*assignment.OperatorID] += assignment.AnimalCount
	}
	if totals["op-1"] > 40 {
		t.Fatalf("second batch reused op-1 beyond remaining capacity: got %#v", got)
	}
	if totals["op-2"] == 0 && totals["op-3"] == 0 {
		t.Fatalf("second batch did not move work to an operator with remaining capacity: %#v", got)
	}
}

// TestOperatorCapacityPlannerHonorsCrossVersionOperatorDayLoad reproduces the real production
// over-cap bug found on a clean CPT reseed (2026-08-24, single operator Darshan cap 200, two
// vaccine rule-versions -- blue_tongue_adult_w1 then sheep_pox_adult_w1 -- both due that date):
// operatorCapacityPlanner/effectiveOperatorAnimalCap (the SELECTION-limiting call, which bounds
// how many obligations a due-group is even allowed to pull into a batch) call
// availableVaccinationOperatorsForDrive -> totalVaccinationOperatorCap, which sums the raw
// DB-queried remaining Cap WITHOUT subtracting session.vaccinationOperatorLoad -- unlike
// planVaccinationDriveAssignments (used by distributeVaccinationDriveAssignments, see
// TestDistributeVaccinationDriveAssignmentsHonorsCrossBatchOperatorDayLoad above), which already
// does this subtraction correctly. So the FIRST due-group's batch consumes 190/200 of the day's
// only operator, session.rememberVaccinationOperatorLoad records that, but the SECOND due-group's
// selection cap is computed from the cached (pre-session-load) DB snapshot and reports the full
// 200 again -- letting the second due-group select up to 200 MORE obligations into its own batch,
// over-committing the single operator's real 10-remaining capacity for that business date. With a
// single operator (N=1, exactly the CPT production config), there is no second operator for the
// downstream assignment-split step to move the overflow onto, so the over-selected obligations
// stay attached to the batch and obligation_instances/vaccination_drive_assignments end up with
// more than 200 unique animals for one operator/date.
func TestOperatorCapacityPlannerHonorsCrossVersionOperatorDayLoad(t *testing.T) {
	planned := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1"}}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()

	// First due-group (blue_tongue_adult_w1) selects and attaches 190 animals to op-1 for
	// 2026-08-24, then the assignment-split step records the real load into the shared session --
	// exactly what sweepVersion's real batchDueGroup -> distributeVaccinationDriveAssignments ->
	// rememberVaccinationDriveAssignmentLoads path does for a real batch.
	firstPlanner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, session)
	if err != nil {
		t.Fatalf("first operatorCapacityPlanner: %v", err)
	}
	if firstPlanner.MaxGoatsPerDrive != 200 {
		t.Fatalf("first due-group planner cap = %d, want 200 (nothing reserved yet)", firstPlanner.MaxGoatsPerDrive)
	}
	first := []domain.DriveAssignment{{
		BatchID: "batch-1", PlannedDate: planned, ParkID: "park-1",
		PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: 190, CapacityStatus: "within_cap",
	}}
	if _, err := svc.distributeVaccinationDriveAssignments(context.Background(), "tenant-1", domain.NewBatch{
		TenantID: "tenant-1", ScopeType: "park", ScopeID: "park-1", PlannedDate: &planned,
	}, 200, first, session); err != nil {
		t.Fatalf("first distribute: %v", err)
	}

	// Second due-group (sheep_pox_adult_w1), same park/date/session: only 10 animals of the single
	// operator's 200 cap remain. The SELECTION cap must reflect that -- not the full 200 again.
	secondPlanner, err := svc.operatorCapacityPlanner(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, session)
	if err != nil {
		t.Fatalf("second operatorCapacityPlanner: %v", err)
	}
	if secondPlanner.MaxGoatsPerDrive != 10 {
		t.Fatalf("second due-group planner cap = %d, want 10 (200 cap - 190 already reserved this session on the same operator/date); the second due-group's SELECTION step is not accounting for load reserved by the first due-group in this sweep, which is the root cause of the 2026-08-24 >200-unique-animal production bug", secondPlanner.MaxGoatsPerDrive)
	}

	secondCap, err := svc.effectiveOperatorAnimalCap(context.Background(), "tenant-1", "park-1", &planned, 200, session)
	if err != nil {
		t.Fatalf("effectiveOperatorAnimalCap: %v", err)
	}
	if secondCap != 10 {
		t.Fatalf("effectiveOperatorAnimalCap = %d, want 10 (same cross-version session-load gap)", secondCap)
	}
}

func TestOperatorCapacityPlannerReusesSameAnimalsForCompatibleSecondVaccine(t *testing.T) {
	planned := time.Date(2027, 7, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{fakeSweepRepo: &fakeSweepRepo{}, operators: []string{"op-1"}}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	targets := make([]string, 190)
	for i := range targets {
		targets[i] = fmt.Sprintf("sheep-%03d", i)
		session.claimDriveCapacity("park-1", planned, targets[i], 1)
	}
	session.rememberVaccinationOperatorLoad("tenant-1", "park-1", planned, "op-1", 190)
	planner, err := svc.operatorCapacityPlannerForTargets(context.Background(), "tenant-1", "park-1", &planned, domain.DrivePlannerSettings{MaxGoatsPerDrive: 200}, session, targets)
	if err != nil {
		t.Fatalf("operatorCapacityPlannerForTargets: %v", err)
	}
	if planner.MaxGoatsPerDrive != 200 {
		t.Fatalf("compatible second-lane cap = %d, want 200 reused animal slots", planner.MaxGoatsPerDrive)
	}
}

func TestSweeperRollsBackShotCapClaimWhenAttachNoOps(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"version-noop": {
				{ObligationID: "obl-noop", RuleID: "rule-noop", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due},
			},
			"version-valid": {
				{ObligationID: "obl-valid", RuleID: "rule-valid", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due},
			},
		},
		createBatchAttachedSeq: []int64{0, 1},
	}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	cfg := SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1},
	}

	first, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "version-noop", cfg, due, due, session)
	if err != nil {
		t.Fatalf("first SweepVersionWithSessionAsOf: %v", err)
	}
	if first.Obligations != 0 {
		t.Fatalf("first result = %#v, want no attached obligations", first)
	}
	second, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "version-valid", cfg, due, due, session)
	if err != nil {
		t.Fatalf("second SweepVersionWithSessionAsOf: %v", err)
	}
	if second.Obligations != 1 {
		t.Fatalf("second result = %#v, want later valid obligation to attach after rollback", second)
	}
	key := visitShotCountKey(due, "goat-1")
	if session.visitShotCounts[key] != 1 {
		t.Fatalf("shot count after no-op then attach = %d, want 1", session.visitShotCounts[key])
	}
}

func TestSweeperAppliesVaccineDriveDateOverrideBeforeCapSelection(t *testing.T) {
	original := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	postponed := original.AddDate(0, 0, 7)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"version-ppr": {
				{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-1", DueAt: original, WindowEnd: &postponed},
			},
		},
		attachAll: true,
		overrides: map[string]domain.VaccineDriveDateOverride{
			"tenant-1|park-1|ppr|" + businessDate(original).Format("2006-01-02"): {
				TenantID:          "tenant-1",
				ParkID:            "park-1",
				VaccineCode:       "PPR",
				OriginalDriveDate: original,
				OverrideDate:      postponed,
				Reason:            "CEO postponement",
			},
			"tenant-1|park-for-shed-1|ppr|" + businessDate(original).Format("2006-01-02"): {
				TenantID:          "tenant-1",
				ParkID:            "park-for-shed-1",
				VaccineCode:       "PPR",
				OriginalDriveDate: original,
				OverrideDate:      postponed,
				Reason:            "CEO postponement",
			},
		},
	}
	svc := NewSweeperService(repo, nil, nil)
	g := &dueGroup{
		scopeType: "shed",
		scopeID:   "shed-1",
		ruleID:    "rule-ppr",
		parkID:    "park-1",
		ids:       []string{"obl-ppr"},
		rows:      repo.rowsByVersion["version-ppr"],
	}
	batched, obligations, err := svc.batchDueGroup(context.Background(), "tenant-1", "version-ppr", SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2, MaxBatchingHoldDays: 7, MaxBatchingHoldCount: 1},
	}, domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2, MaxBatchingHoldDays: 7, MaxBatchingHoldCount: 1}, original, postponed, NewSweepSession(), g)
	if err != nil {
		t.Fatalf("batchDueGroup: %v", err)
	}
	if !batched || obligations != 1 || len(repo.createdBatches) != 1 || repo.createdBatches[0].PlannedDate == nil {
		t.Fatalf("batched=%v obligations=%d batches=%#v", batched, obligations, repo.createdBatches)
	}
	if got := businessDate(*repo.createdBatches[0].PlannedDate); !got.Equal(businessDate(postponed)) {
		t.Fatalf("planned date = %s, want override %s", got.Format("2006-01-02"), businessDate(postponed).Format("2006-01-02"))
	}
}

func TestApplyUnbatchedDriveDateOverrideUsesTwoDayCloseoutWindow(t *testing.T) {
	original := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	postponed := original.AddDate(0, 0, 7)
	repo := &fakeSweepRepo{
		overrides: map[string]domain.VaccineDriveDateOverride{
			"tenant-1|park-1|ppr|" + businessDate(original).Format("2006-01-02"): {
				TenantID:          "tenant-1",
				ParkID:            "park-1",
				VaccineCode:       "PPR",
				OriginalDriveDate: original,
				OverrideDate:      postponed,
				Reason:            "CEO postponement",
			},
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	rows, err := svc.applyUnbatchedDriveDateOverrides(context.Background(), "tenant-1", "park-1", "PPR", []domain.UnbatchedDue{
		{ObligationID: "obl-ppr", TargetID: "goat-1", RuleID: "rule-ppr", ParkID: "park-1", DueAt: original, WindowEnd: ptrTime(original.AddDate(0, 0, 14)), BatchingHoldCount: 1, FirstBatchingHoldUntil: ptrTime(original.AddDate(0, 0, 7))},
	})
	if err != nil {
		t.Fatalf("applyUnbatchedDriveDateOverrides: %v", err)
	}
	if len(rows) != 1 || rows[0].WindowStart == nil || rows[0].WindowEnd == nil {
		t.Fatalf("rewritten rows = %#v, want one row with override window", rows)
	}
	if got := businessDate(rows[0].DueAt); !got.Equal(businessDate(postponed)) {
		t.Fatalf("due date = %s, want override %s", got.Format("2006-01-02"), businessDate(postponed).Format("2006-01-02"))
	}
	if got := businessDate(*rows[0].WindowEnd); !got.Equal(businessDate(postponed).AddDate(0, 0, 1)) {
		t.Fatalf("window end = %s, want override + 1 day", got.Format("2006-01-02"))
	}
	if rows[0].BatchingHoldCount != 0 || rows[0].FirstBatchingHoldUntil != nil {
		t.Fatalf("hold metadata = count %d until %v, want cleared", rows[0].BatchingHoldCount, rows[0].FirstBatchingHoldUntil)
	}
}

func TestSweeperRollsBackShotCapClaimsWhenAttachPartiallySucceeds(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"version-partial": {
				{ObligationID: "obl-attached", RuleID: "rule-partial", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due},
				{ObligationID: "obl-raced", RuleID: "rule-partial", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-2", DueAt: due},
			},
			"version-valid": {
				{ObligationID: "obl-valid", RuleID: "rule-valid", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-2", DueAt: due},
			},
		},
		createBatchAttachedSeq: []int64{1, 1},
	}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	cfg := SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1},
	}

	first, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "version-partial", cfg, due, due, session)
	if err != nil {
		t.Fatalf("first SweepVersionWithSessionAsOf: %v", err)
	}
	if first.Obligations != 1 {
		t.Fatalf("first result = %#v, want one attached obligation", first)
	}
	second, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "version-valid", cfg, due, due, session)
	if err != nil {
		t.Fatalf("second SweepVersionWithSessionAsOf: %v", err)
	}
	if second.Obligations != 1 {
		t.Fatalf("second result = %#v, want later goat-2 obligation to attach after partial rollback", second)
	}
}

func TestSweeperMarksBatchBlockedWhenStockReservationFails(t *testing.T) {
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
			{ObligationID: "obl-2", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
			{ObligationID: "obl-3", ScopeType: "shed", ScopeID: "shed-2", ParkID: "park-2", DueAt: time.Now()},
			{ObligationID: "obl-4", ScopeType: "shed", ScopeID: "shed-2", ParkID: "park-2", DueAt: time.Now()},
		},
		createBatchIDs:      []string{"batch-1", "batch-2"},
		createBatchAttached: 2,
	}
	reserver := &fakeSweepStockReserver{err: errors.New("inventory: stock unavailable")}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if repo.stockBlockCalls != 2 {
		t.Fatalf("stock block calls = %d, want 2", repo.stockBlockCalls)
	}
	if result.Batches != 2 || result.Obligations != 4 {
		t.Fatalf("result = %#v, want blocked batch counted", result)
	}
}

func TestUnbatchedDriveWindowUsesSelectedIntersection(t *testing.T) {
	startA := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	endA := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	startB := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	endB := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

	windowStart, windowEnd := unbatchedDriveWindow([]domain.UnbatchedDue{
		{ObligationID: "obl-a", DueAt: startA, WindowStart: &startA, WindowEnd: &endA},
		{ObligationID: "obl-b", DueAt: startB, WindowStart: &startB, WindowEnd: &endB},
	})
	if got := dateKey(windowStart); got != "2026-08-12" {
		t.Fatalf("window start = %s, want latest selected start 2026-08-12", got)
	}
	if got := dateKey(windowEnd); got != "2026-08-18" {
		t.Fatalf("window end = %s, want binding selected safe-until 2026-08-18", got)
	}
}

func TestRejectNonShedUnbatchedDueFailsClosed(t *testing.T) {
	err := rejectNonShedUnbatchedDue([]domain.UnbatchedDue{{
		ObligationID: "obl-park",
		ScopeType:    "park",
		ScopeID:      "park-1",
	}})
	if err == nil {
		t.Fatal("rejectNonShedUnbatchedDue err=nil, want fail-closed non-shed obligation")
	}
	if !strings.Contains(err.Error(), "non-shed goat obligation") {
		t.Fatalf("error = %q, want non-shed invariant message", err.Error())
	}
}

func TestVaccinationFallbackShedRowsCreateParkDriveBatch(t *testing.T) {
	due := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-goat-pox", ScopeType: "shed", ScopeID: "shed-y7", ParkID: "park-cbe", TargetID: "goat-1", DueAt: due},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineCode: "Goat Pox",
	}, due)
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 1 || result.Obligations != 1 {
		t.Fatalf("result = %#v, want one planned drive", result)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(repo.createdBatches))
	}
	batch := repo.createdBatches[0]
	if batch.ScopeType != "park" || batch.ScopeID != "park-cbe" {
		t.Fatalf("batch scope = %s/%s, want park/park-cbe", batch.ScopeType, batch.ScopeID)
	}
}

func TestVaccinationFallbackFailsWhenShedRowHasNoPark(t *testing.T) {
	due := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{attachAll: true}
	svc := NewSweeperService(repo, nil, nil)
	group := &dueGroup{
		scopeType: "shed",
		scopeID:   "shed-y7",
		ruleID:    "rule-goat-pox",
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-goat-pox", ScopeType: "shed", ScopeID: "shed-y7", TargetID: "goat-1", DueAt: due},
		},
		ids: []string{"obl-1"},
	}

	_, _, err := svc.batchDueGroup(context.Background(), "tenant-1", "version-1", SweepConfig{VaccineCode: "Goat Pox"}, domain.DrivePlannerSettings{}, due, due, NewSweepSession(), group)
	if err == nil {
		t.Fatal("batchDueGroup err=nil, want missing park placement to fail closed")
	}
	if !strings.Contains(err.Error(), "missing park_id") {
		t.Fatalf("err = %q, want missing park_id", err.Error())
	}
	if len(repo.createdBatches) != 0 {
		t.Fatalf("created batches = %d, want no non-park batch", len(repo.createdBatches))
	}
}

func TestSweeperCreatesSideEffectsAfterAttach(t *testing.T) {
	due := time.Now()
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: due},
			{ObligationID: "obl-2", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: due},
			{ObligationID: "obl-raced", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: due},
		},
		createBatchID:       "batch-1",
		createBatchAttached: 2,
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		SOPVersionID:  "sop-version-1",
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  2,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 1 || result.Obligations != 2 {
		t.Fatalf("result = %#v, want one batch and two obligations", result)
	}
	if tasks.calls != 1 || repo.setTaskCalls != 1 || repo.lastTaskID != "task-1" {
		t.Fatalf("task side effects calls=%d setCalls=%d taskID=%q", tasks.calls, repo.setTaskCalls, repo.lastTaskID)
	}
	if reserver.calls != 1 || reserver.lastBatchID != "batch-1" || reserver.lastQty != 4 {
		t.Fatalf("reservation calls=%d batch=%q qty=%d", reserver.calls, reserver.lastBatchID, reserver.lastQty)
	}
}

func TestSweeperGroupsByRuleAndReservesAgainstPlannedDate(t *testing.T) {
	dueA := time.Date(2026, time.August, 14, 9, 30, 0, 0, time.UTC)
	dueB := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.UTC)
	windowEnd := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: dueA, WindowEnd: &windowEnd},
			{ObligationID: "obl-2", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: dueB, WindowEnd: &windowEnd},
		},
		createBatchIDs: []string{"batch-a", "batch-b"},
		attachAll:      true,
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersionAsOf(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SweepVersionAsOf: %v", err)
	}
	if result.Batches != 2 || result.Obligations != 2 {
		t.Fatalf("result = %#v, want two separately planned batches", result)
	}
	if len(repo.createdBatches) != 2 {
		t.Fatalf("created batches = %d, want 2", len(repo.createdBatches))
	}
	if repo.createdBatches[0].Session != "rule:rule-a" || repo.createdBatches[1].Session != "rule:rule-b" {
		t.Fatalf("sessions = %q/%q, want rule sessions", repo.createdBatches[0].Session, repo.createdBatches[1].Session)
	}
	if got := dateKey(repo.createdBatches[0].PlannedDate); got != "2026-08-20" {
		t.Fatalf("batch A planned date = %s, want 2026-08-20", got)
	}
	if got := dateKey(repo.createdBatches[1].PlannedDate); got != "2026-08-20" {
		t.Fatalf("batch B planned date = %s, want 2026-08-20", got)
	}
	if reserver.calls != 2 {
		t.Fatalf("reservation calls = %d, want 2", reserver.calls)
	}
	if got := validOnKeys(reserver.validOns); got != "2026-08-20,2026-08-20" {
		t.Fatalf("reservation validOn dates = %s, want planned dates", got)
	}
}

func TestSweeperFailsNoWindowObligationsWeeksLate(t *testing.T) {
	dueA := time.Date(2026, time.August, 14, 9, 30, 0, 0, time.UTC)
	dueB := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", DueAt: dueA},
			{ObligationID: "obl-2", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", DueAt: dueB},
		},
		createBatchIDs: []string{"batch-a", "batch-b"},
		attachAll:      true,
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersionAsOf(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("SweepVersionAsOf err=nil, want stale eligible row to fail closed")
	}
	if !strings.Contains(err.Error(), "no feasible date") {
		t.Fatalf("SweepVersionAsOf err = %q, want no feasible date failure", err.Error())
	}
	if result.Batches != 0 || result.Obligations != 0 {
		t.Fatalf("result = %#v, want no late no-window work", result)
	}
	if len(repo.createdBatches) != 0 {
		t.Fatalf("created batches = %d, want 0", len(repo.createdBatches))
	}
	if reserver.calls != 0 {
		t.Fatalf("reservation calls = %d, want 0", reserver.calls)
	}
}

func TestSweeperFutureHorizonDoesNotActAsOperationalDate(t *testing.T) {
	dueA := time.Date(2026, time.August, 14, 9, 30, 0, 0, time.UTC)
	dueB := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", DueAt: dueA},
			{ObligationID: "obl-2", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", DueAt: dueB},
		},
		createBatchIDs: []string{"batch-a", "batch-b"},
		attachAll:      true,
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 2 || result.Obligations != 2 {
		t.Fatalf("result = %#v, want due rows batched under future horizon", result)
	}
	if len(repo.createdBatches) != 2 {
		t.Fatalf("created batches = %d, want 2", len(repo.createdBatches))
	}
	if got := dateKey(repo.createdBatches[0].PlannedDate); got != "2026-08-14" {
		t.Fatalf("batch A planned date = %s, want 2026-08-14", got)
	}
	if got := dateKey(repo.createdBatches[1].PlannedDate); got != "2026-08-15" {
		t.Fatalf("batch B planned date = %s, want 2026-08-15", got)
	}
	if reserver.calls != 2 {
		t.Fatalf("reservation calls = %d, want 2", reserver.calls)
	}
}

func TestSweeperUsesRuleSpecificExecutionConfig(t *testing.T) {
	due := time.Date(2026, time.August, 14, 9, 30, 0, 0, time.UTC)
	windowEnd := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: due, WindowEnd: &windowEnd},
			{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: due, WindowEnd: &windowEnd},
		},
		createBatchIDs: []string{"batch-a", "batch-b"},
		attachAll:      true,
	}
	tasks := &fakeSweepTaskCreator{}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)

	_, err := svc.SweepVersionAsOf(context.Background(), "tenant-1", "version-1", SweepConfig{
		SOPVersionID:  "sop-default",
		VaccineItemID: "vaccine-default",
		DosesPerGoat:  1,
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-b": {SOPVersionID: "sop-rule-b", VaccineItemID: "vaccine-rule-b", DosesPerGoat: 3},
		},
	}, time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if got := strings.Join(tasks.sopVersionIDs, ","); got != "sop-default,sop-rule-b" {
		t.Fatalf("task SOP versions = %s, want version fallback then rule override", got)
	}
	if got := strings.Join(reserver.itemIDs, ","); got != "vaccine-default,vaccine-rule-b" {
		t.Fatalf("reservation item IDs = %s, want version fallback then rule override", got)
	}
	if got := qtyKeys(reserver.quantities); got != "1,3" {
		t.Fatalf("reservation quantities = %s, want per-rule doses", got)
	}
}

func TestSweeperFinalizesRuleSpecificStockWithoutVersionStockItem(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-fmd",
				RuleID:              "rule-fmd",
				ScopeType:           "shed",
				ScopeID:             "shed-1",
				AttachedObligations: 2,
				HasSOPTask:          true,
			},
			{
				BatchID:             "batch-hs",
				RuleID:              "rule-hs",
				ScopeType:           "shed",
				ScopeID:             "shed-1",
				AttachedObligations: 3,
				HasSOPTask:          true,
			},
		}},
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		DosesPerGoat: 1,
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-fmd": {VaccineItemID: "item-fmd", DosesPerGoat: 1},
			"rule-hs":  {VaccineItemID: "item-hs", DosesPerGoat: 2},
		},
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if reserver.calls != 2 || reserver.batchCalls != 1 || repo.countBatchCalls != 1 {
		t.Fatalf("stock finalization calls reserve=%d batchReserve=%d count=%d, want 2/1/1", reserver.calls, reserver.batchCalls, repo.countBatchCalls)
	}
	if got := strings.Join(reserver.itemIDs, ","); got != "item-fmd,item-hs" {
		t.Fatalf("reservation item IDs = %s, want per-rule item-fmd,item-hs", got)
	}
	if got := qtyKeys(reserver.quantities); got != "2,6" {
		t.Fatalf("reservation quantities = %s, want per-rule dose quantities 2,6", got)
	}
}

func TestSweeperMovesOverflowDoseToNextDriveWhenAnimalShotCapReached(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
			{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
			{ObligationID: "obl-c", RuleID: "rule-c", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), WindowEnd: &winEnd},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2},
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Obligations != 3 || len(repo.createdBatches) != 3 {
		t.Fatalf("result=%#v batches=%#v, want three one-obligation batches", result, repo.createdBatches)
	}
	plannedDates := make(map[string]int)
	for _, b := range repo.createdBatches {
		if b.PlannedDate == nil {
			t.Fatalf("batch missing planned date: %#v", b)
		}
		plannedDates[b.PlannedDate.Format("2006-01-02")]++
	}
	if plannedDates["2026-07-01"] != 2 || plannedDates["2026-07-02"] != 1 {
		t.Fatalf("planned dates=%#v, want two shots on Jul 1 and overflow on Jul 2", plannedDates)
	}
}

// obligationIDPlannedDates maps each obligation id attached by CreateBatchWithObligations calls
// to the planned date of the batch it landed in, using the parallel createdBatches /
// createdBatchObligationIDs slices recorded by fakeSweepRepo.
func obligationIDPlannedDates(repo *fakeSweepRepo) map[string]string {
	out := make(map[string]string)
	for i, batch := range repo.createdBatches {
		if batch.PlannedDate == nil {
			continue
		}
		date := batch.PlannedDate.Format("2006-01-02")
		if i >= len(repo.createdBatchObligationIDs) {
			continue
		}
		for _, id := range repo.createdBatchObligationIDs[i] {
			out[id] = date
		}
	}
	return out
}

// TestSweepVersionWithSessionSharesShotCapAcrossVersions covers BUG2 requirement 1: an animal
// due 3 different vaccines (3 protocol versions) on the same planned date must get exactly
// MaxShotsPerAnimalPerDrive (2) shots that day when the versions are swept against one shared
// SweepSession, with the 3rd vaccine's obligation pushed to a later safe date -- not 3 shots in
// one visit, which is what happened before the cap spanned versions.
func TestSweepVersionWithSessionSharesShotCapAcrossVersions(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-ettt": {{ObligationID: "obl-ettt", RuleID: "rule-ettt", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-ppr":  {{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-fmd":  {{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	session := NewSweepSession()

	// Sweep in ascending priority order (ET+TT=1, PPR=2, FMD=5), as the production caller does
	// after SortSweepVersionsByPriority.
	for _, v := range []struct{ versionID, vaccine string }{
		{"v-ettt", "ET+TT"}, {"v-ppr", "PPR"}, {"v-fmd", "FMD"},
	} {
		if _, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", v.versionID, SweepConfig{
			VaccineCode: v.vaccine, DrivePlanner: planner,
		}, due, session); err != nil {
			t.Fatalf("sweep %s: %v", v.versionID, err)
		}
	}

	dates := obligationIDPlannedDates(repo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want ET+TT and PPR both on 2026-07-01", dates)
	}
	if dates["obl-fmd"] != "2026-07-15" {
		t.Fatalf("dates=%#v, want FMD overflowed to 2026-07-15 after the 14-day live-to-killed gap", dates)
	}
}

func TestSweepVersionWalksEverySafeOverflowDateWhenShotCapFull(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := due.AddDate(0, 0, 2)
	repo := &fakeDateVisitShotLockerRepo{
		fakeSweepRepo: &fakeSweepRepo{
			rowsByVersion: map[string][]domain.UnbatchedDue{
				"v-overflow": {
					{ObligationID: "obl-overflow", RuleID: "rule-overflow", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				},
			},
			attachAll: true,
		},
		persisted: map[string]int32{
			visitShotCountKey(due, "goat-1"):                  1,
			visitShotCountKey(due.AddDate(0, 0, 1), "goat-1"): 1,
		},
	}
	svc := NewSweeperService(repo, nil, nil)
	_, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "v-overflow", SweepConfig{
		VaccineCode:  "FMD",
		DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1},
	}, due, due, NewSweepSession())
	if err != nil {
		t.Fatalf("SweepVersionWithSessionAsOf: %v", err)
	}

	dates := obligationIDPlannedDates(repo.fakeSweepRepo)
	if dates["obl-overflow"] != "2026-07-03" {
		t.Fatalf("dates=%#v, want obligation to skip two capped days and land on 2026-07-03", dates)
	}
}

func TestSweepVersionPrefersLaterDateWithMoreAnimalsOverTinyPartialCapFit(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	next := due.AddDate(0, 0, 1)
	repo := &fakeDateVisitShotLockerRepo{
		fakeSweepRepo: &fakeSweepRepo{
			rowsByVersion: map[string][]domain.UnbatchedDue{
				"v-club": {
					{ObligationID: "obl-1", RuleID: "rule-club", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-1", DueAt: due, WindowEnd: &next},
					{ObligationID: "obl-2", RuleID: "rule-club", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-2", DueAt: due, WindowEnd: &next},
					{ObligationID: "obl-3", RuleID: "rule-club", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-3", DueAt: due, WindowEnd: &next},
				},
			},
			attachAll: true,
		},
		persisted: map[string]int32{
			visitShotCountKey(due, "goat-2"): 1,
			visitShotCountKey(due, "goat-3"): 1,
		},
	}
	svc := NewSweeperService(repo, nil, nil)
	_, err := svc.SweepVersionWithSessionAsOf(context.Background(), "tenant-1", "v-club", SweepConfig{
		VaccineCode: "FMD",
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:                   true,
			MaxShotsPerAnimalPerDrive: 1,
		},
	}, due, due, NewSweepSession())
	if err != nil {
		t.Fatalf("SweepVersionWithSessionAsOf: %v", err)
	}

	if len(repo.fakeSweepRepo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want one clubbed batch", len(repo.fakeSweepRepo.createdBatches))
	}
	if got := dateKey(repo.fakeSweepRepo.createdBatches[0].PlannedDate); got != "2026-07-02" {
		t.Fatalf("planned date = %s, want later legal date 2026-07-02 with more animals", got)
	}
	if got := len(repo.fakeSweepRepo.createdBatchObligationIDs[0]); got != 3 {
		t.Fatalf("attached obligations = %d, want all three animals clubbed", got)
	}
}

func TestParkMergeStepWalksEverySafeOverflowDateWhenShotCapFull(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := due.AddDate(0, 0, 2)
	repo := &fakeDateVisitShotLockerRepo{
		fakeSweepRepo: &fakeSweepRepo{attachAll: true},
		persisted: map[string]int32{
			visitShotCountKey(due, "goat-1"):                  1,
			visitShotCountKey(due.AddDate(0, 0, 1), "goat-1"): 1,
		},
	}
	svc := NewSweeperService(repo, nil, nil)
	remaining := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-park-overflow", RuleID: "rule-overflow", ParkID: "park-1", ShedID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
	}
	_, attached, plannedDate, _, _, err := svc.parkMergeStep(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineCode: "FMD",
	}, domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1}, due, due, NewSweepSession(), "park-1", remaining, 1, map[string]struct{}{})
	if err != nil {
		t.Fatalf("parkMergeStep: %v", err)
	}
	if attached != 1 || plannedDate == nil || plannedDate.Format("2006-01-02") != "2026-07-03" {
		t.Fatalf("attached=%d plannedDate=%v, want one obligation on 2026-07-03", attached, plannedDate)
	}
}

// TestSweepVersionWithSessionRetainsHighestPriorityPairNotArrivalOrder covers BUG2 requirement
// 2: when 3 vaccines compete for one animal's over-cap visit, the retained pair is the two
// highest resolved-priority vaccines -- determined by sweeping in the order
// SortSweepVersionsByPriority produces, not by whatever order the versions were originally
// discovered/listed in (here deliberately scrambled: lowest priority first).
func TestSweepVersionWithSessionRetainsHighestPriorityPairNotArrivalOrder(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-fmd":  {{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-ettt": {{ObligationID: "obl-ettt", RuleID: "rule-ettt", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-ppr":  {{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	// Discovery/arrival order is deliberately WRONG (lowest priority, FMD, listed first) -- the
	// production caller must sort by priority before sweeping rather than trust discovery order.
	plans := []SweepVersionPriority{
		{VersionID: "v-fmd", Config: SweepConfig{VaccineCode: "FMD", DrivePlanner: planner}},
		{VersionID: "v-ettt", Config: SweepConfig{VaccineCode: "ET+TT", DrivePlanner: planner}},
		{VersionID: "v-ppr", Config: SweepConfig{VaccineCode: "PPR", DrivePlanner: planner}},
	}
	sorted := SortSweepVersionsByPriority(plans)
	wantOrder := []string{"v-ettt", "v-ppr", "v-fmd"}
	for i, p := range sorted {
		if p.VersionID != wantOrder[i] {
			t.Fatalf("sorted order = %#v, want %#v (ET+TT=1, PPR=2, FMD=5)", sorted, wantOrder)
		}
	}

	session := NewSweepSession()
	for _, plan := range sorted {
		if _, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", plan.VersionID, plan.Config, due, session); err != nil {
			t.Fatalf("sweep %s: %v", plan.VersionID, err)
		}
	}

	dates := obligationIDPlannedDates(repo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want the two highest-priority vaccines (ET+TT, PPR) retained on 2026-07-01", dates)
	}
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want lowest-priority FMD overflowed off 2026-07-01", dates)
	}
}

func TestSweepVersionPrioritizesRuleVaccinesWithinSingleMatrix(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-matrix": {
				{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-ettt", RuleID: "rule-ettt", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
			},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	_, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", "v-matrix", SweepConfig{
		VaccineCode: "Matrix Version",
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:                   true,
			MaxShotsPerAnimalPerDrive: 2,
		},
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-fmd":  {VaccineCode: "FMD", VaccinePriority: 5},
			"rule-ettt": {VaccineCode: "ET+TT", VaccinePriority: 1},
			"rule-ppr":  {VaccineCode: "PPR", VaccinePriority: 2},
		},
	}, due, session)
	if err != nil {
		t.Fatalf("sweep matrix: %v", err)
	}
	dates := obligationIDPlannedDates(repo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want ET+TT and PPR retained on first drive", dates)
	}
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want lower-priority FMD overflowed off first drive", dates)
	}
}

func TestSweepVersionSnapshotFallbackPrioritizesRuleVaccinesWithinSingleMatrix(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-ettt", RuleID: "rule-ettt", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
	}
	baseRepo := &fakeSweepRepo{rows: rows, attachAll: true}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"v-matrix": {"obl-fmd", "obl-ettt", "obl-ppr"},
	}}
	cfg := SweepConfig{
		VaccineCode: "Matrix Version",
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:                   true,
			MaxShotsPerAnimalPerDrive: 2,
		},
		ParkConsolidation: domain.ParkConsolidationSettings{
			Enabled:             true,
			MinShedDriveTargets: 10,
			MinParkMergeTargets: 1,
			MinParkMergeSheds:   2,
		},
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-fmd":  {VaccineCode: "FMD", VaccinePriority: 5},
			"rule-ettt": {VaccineCode: "ET+TT", VaccinePriority: 1},
			"rule-ppr":  {VaccineCode: "PPR", VaccinePriority: 2},
		},
	}
	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshot(
		context.Background(),
		"tenant-1",
		"v-matrix",
		cfg,
		due,
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshot: %v", err)
	}
	if result.Batches != 3 || result.Obligations != 3 {
		t.Fatalf("result = %#v, want fallback to batch all three obligations", result)
	}
	dates := obligationIDPlannedDates(repo.fakeSweepRepo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want ET+TT and PPR retained on first fallback drive", dates)
	}
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want lower-priority FMD overflowed off first fallback drive", dates)
	}
}

func TestSweepVersionSnapshotDrainsPartialShotCapLeftovers(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-capped", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-open", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-2", DueAt: due, WindowEnd: &winEnd},
	}
	baseRepo := &fakeSweepRepo{rows: rows, attachAll: true}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	key := visitShotCountKey(due, "goat-1")
	session.claim(key, "ET+TT", 1)
	session.claim(key, "PPR", 2)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"v-fmd": {"obl-capped", "obl-open"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshot(
		context.Background(),
		"tenant-1",
		"v-fmd",
		SweepConfig{
			VaccineCode: "FMD",
			DrivePlanner: domain.DrivePlannerSettings{
				Enabled:                   true,
				MaxShotsPerAnimalPerDrive: 2,
			},
			ParkConsolidation: domain.ParkConsolidationSettings{Enabled: false},
		},
		due,
		session,
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshot: %v", err)
	}
	if result.Obligations != 2 {
		t.Fatalf("result = %#v, want both snapshot obligations drained", result)
	}
	dates := obligationIDPlannedDates(repo.fakeSweepRepo)
	if dates["obl-open"] != "2026-07-02" {
		t.Fatalf("dates=%#v, want open goat held one day to club with capped goat", dates)
	}
	if dates["obl-capped"] != "2026-07-02" {
		t.Fatalf("dates=%#v, want capped goat retried onto next safe date, not stranded", dates)
	}
	if len(repo.rows) != 0 {
		t.Fatalf("remaining rows = %#v, want snapshot drained", repo.rows)
	}
}

// TestSweepVersionWithSessionBlocksOnUnresolvedPriorityTie covers BUG2 requirement 3: when more
// than MaxShotsPerAnimalPerDrive vaccines compete for one animal's visit and the deciding
// (boundary) vaccines resolve to the SAME priority, the sweeper must surface an explicit
// blocker naming both vaccines instead of silently picking an arrival-order winner. Neither
// vaccine here is in the source matrix, so both resolve to the same unconfigured default
// priority -- a genuine, unresolved tie.
func TestSweepVersionWithSessionBlocksOnUnresolvedPriorityTie(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-alpha": {{ObligationID: "obl-alpha", RuleID: "rule-alpha", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-beta":  {{ObligationID: "obl-beta", RuleID: "rule-beta", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-gamma": {{ObligationID: "obl-gamma", RuleID: "rule-gamma", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	session := NewSweepSession()

	for _, v := range []struct{ versionID, vaccine string }{
		{"v-alpha", "Unmapped Vaccine Alpha"}, {"v-beta", "Unmapped Vaccine Beta"},
	} {
		if _, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", v.versionID, SweepConfig{
			VaccineCode: v.vaccine, DrivePlanner: planner,
		}, due, session); err != nil {
			t.Fatalf("sweep %s: %v", v.versionID, err)
		}
	}
	// A 3rd unmapped vaccine now competes for the same over-cap visit; its resolved priority
	// ties with whichever of alpha/beta claimed the visit's last slot.
	_, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", "v-gamma", SweepConfig{
		VaccineCode: "Unmapped Vaccine Gamma", DrivePlanner: planner,
	}, due, session)
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie error target = %q, want goat-1", tieErr.TargetID)
	}
	if tieErr.VaccineA == "" || tieErr.VaccineB == "" || tieErr.VaccineA == tieErr.VaccineB {
		t.Fatalf("tie error vaccines = %q/%q, want two distinct named vaccines", tieErr.VaccineA, tieErr.VaccineB)
	}
}

// TestSweepVersionWithSessionReplayIsIdempotent covers BUG2's replay/idempotency requirement:
// re-running the same version sweep against a repo that reports no remaining unbatched rows
// (simulating the DB no-op once obligations are attached) creates no duplicate batches.
func TestSweepVersionWithSessionReplayIsIdempotent(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-1": {{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	cfg := SweepConfig{VaccineCode: "PPR", DrivePlanner: domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}}
	session := NewSweepSession()

	first, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", "v-1", cfg, due, session)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if first.Obligations != 1 || len(repo.createdBatches) != 1 {
		t.Fatalf("first sweep result=%#v batches=%d, want one obligation batched once", first, len(repo.createdBatches))
	}

	// Simulate the real repo's idempotent behavior once obl-1 has a batch_id: it no longer shows
	// up as unbatched, so a re-sweep (even against the SAME shared session) finds nothing to do.
	repo.rowsByVersion["v-1"] = nil

	second, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", "v-1", cfg, due, session)
	if err != nil {
		t.Fatalf("replay sweep: %v", err)
	}
	if second.Obligations != 0 || second.Batches != 0 {
		t.Fatalf("replay result=%#v, want no-op", second)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches after replay = %d, want still 1 (no duplicate)", len(repo.createdBatches))
	}
}

func TestSweeperFinalizesExistingPlannedBatchMissingTask(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-1",
				ScopeType:           "park",
				ScopeID:             "park-1",
				EstimatedTargets:    3,
				AttachedObligations: 3,
				HasStockReservation: true,
			},
		}},
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	svc := NewSweeperService(repo, tasks, nil)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		SOPVersionID: "sop-version-1",
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 0 || result.Obligations != 0 {
		t.Fatalf("result = %#v, want no newly-created work", result)
	}
	if tasks.calls != 1 || repo.setTaskCalls != 1 || repo.lastTaskID != "task-1" {
		t.Fatalf("task repair calls=%d setCalls=%d taskID=%q", tasks.calls, repo.setTaskCalls, repo.lastTaskID)
	}
	if tasks.batchCalls != 1 || repo.batchSetTaskCalls != 1 {
		t.Fatalf("batch task calls=%d batch links=%d, want 1/1", tasks.batchCalls, repo.batchSetTaskCalls)
	}
	if repo.createBatchCalls != 0 {
		t.Fatalf("new batches created = %d, want 0", repo.createBatchCalls)
	}
}

func TestSweeperFinalizationPagesPastConfigSkippedBatches(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{
			{
				{
					BatchID:             "batch-skipped-1",
					RuleID:              "rule-without-config",
					ScopeType:           "shed",
					ScopeID:             "shed-old-1",
					AttachedObligations: 1,
					HasStockReservation: true,
				},
				{
					BatchID:             "batch-skipped-2",
					RuleID:              "another-rule-without-config",
					ScopeType:           "shed",
					ScopeID:             "shed-old-2",
					AttachedObligations: 1,
					HasStockReservation: true,
				},
			},
			{
				{
					BatchID:             "batch-actionable",
					RuleID:              "rule-actionable",
					ScopeType:           "shed",
					ScopeID:             "shed-new",
					AttachedObligations: 1,
					HasStockReservation: true,
				},
			},
		},
	}
	tasks := &fakeSweepTaskCreator{id: "task-actionable"}
	svc := NewSweeperService(repo, tasks, nil)
	svc.page = 2

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-actionable": {SOPVersionID: "sop-actionable"},
		},
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if repo.finalizationCalls != 2 {
		t.Fatalf("finalization pages fetched = %d, want 2", repo.finalizationCalls)
	}
	if tasks.calls != 1 || repo.setTaskCalls != 1 {
		t.Fatalf("task calls=%d setTaskCalls=%d, want 1/1", tasks.calls, repo.setTaskCalls)
	}
	if tasks.batchCalls != 1 || repo.batchSetTaskCalls != 1 {
		t.Fatalf("batch task calls=%d batch links=%d, want 1/1", tasks.batchCalls, repo.batchSetTaskCalls)
	}
	if got := strings.Join(tasks.batchIDs, ","); got != "batch-actionable" {
		t.Fatalf("task batches = %q, want batch-actionable", got)
	}
	if got := strings.Join(tasks.sopVersionIDs, ","); got != "sop-actionable" {
		t.Fatalf("task SOP versions = %q, want sop-actionable", got)
	}
}

func TestSweeperFinalizesExistingPlannedBatchMissingStockReservation(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-1",
				ScopeType:           "park",
				ScopeID:             "park-1",
				EstimatedTargets:    2,
				AttachedObligations: 2,
				HasSOPTask:          true,
			},
		}},
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  2,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if result.Batches != 0 || result.Obligations != 0 {
		t.Fatalf("result = %#v, want no newly-created work", result)
	}
	if reserver.calls != 1 || reserver.lastBatchID != "batch-1" || reserver.lastQty != 4 {
		t.Fatalf("reservation repair calls=%d batch=%q qty=%d", reserver.calls, reserver.lastBatchID, reserver.lastQty)
	}
	if repo.countBatchCalls != 1 || reserver.batchCalls != 1 {
		t.Fatalf("batch count/reserve calls=%d/%d, want 1/1", repo.countBatchCalls, reserver.batchCalls)
	}
}

func TestSweeperRetriesAndClearsBlockedBatchAfterStockRecovery(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-1",
				ScopeType:           "park",
				ScopeID:             "park-1",
				AttachedObligations: 2,
				HasSOPTask:          true,
				StockBlocked:        true,
			},
		}},
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if reserver.calls != 1 || reserver.lastBatchID != "batch-1" || reserver.lastQty != 2 {
		t.Fatalf("reservation retry calls=%d batch=%q qty=%d", reserver.calls, reserver.lastBatchID, reserver.lastQty)
	}
	if repo.clearStockBlockCalls != 1 {
		t.Fatalf("clear stock block calls = %d, want 1", repo.clearStockBlockCalls)
	}
	if repo.countBatchCalls != 1 || reserver.batchCalls != 1 {
		t.Fatalf("batch count/reserve calls=%d/%d, want 1/1", repo.countBatchCalls, reserver.batchCalls)
	}
}

func TestSweeperRetriesOnlyBlockedStockItemWhenBatchAlreadyHasReservation(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-1",
				ScopeType:           "park",
				ScopeID:             "park-1",
				AttachedObligations: 3,
				HasSOPTask:          true,
				HasStockReservation: true,
				StockBlocked:        true,
				StockBlockItemID:    "item-hs",
			},
		}},
		ruleCountsByBatch: map[string][]domain.RuleAttachmentCount{
			"batch-1": {
				{RuleID: "rule-fmd", Count: 2},
				{RuleID: "rule-hs", Count: 1},
			},
		},
	}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, nil, reserver)

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		DosesPerGoat: 1,
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-fmd": {VaccineItemID: "item-fmd", DosesPerGoat: 1},
			"rule-hs":  {VaccineItemID: "item-hs", DosesPerGoat: 2},
		},
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if reserver.calls != 1 || reserver.batchCalls != 1 || repo.countBatchCalls != 1 {
		t.Fatalf("stock retry calls reserve=%d batchReserve=%d count=%d, want 1/1/1", reserver.calls, reserver.batchCalls, repo.countBatchCalls)
	}
	if got := strings.Join(reserver.itemIDs, ","); got != "item-hs" {
		t.Fatalf("reservation item IDs = %s, want only blocked item-hs", got)
	}
	if got := qtyKeys(reserver.quantities); got != "2" {
		t.Fatalf("reservation quantities = %s, want blocked item quantity 2", got)
	}
	if repo.clearStockBlockCalls != 1 {
		t.Fatalf("clear stock block calls = %d, want 1", repo.clearStockBlockCalls)
	}
}

func TestSweeperMarksExistingBatchBlockedWhenFinalizedReservationFails(t *testing.T) {
	repo := &fakeSweepRepo{
		finalizationPages: [][]domain.PlannedBatchFinalization{{
			{
				BatchID:             "batch-1",
				ScopeType:           "park",
				ScopeID:             "park-1",
				AttachedObligations: 1,
				HasSOPTask:          true,
			},
			{
				BatchID:             "batch-2",
				ScopeType:           "park",
				ScopeID:             "park-2",
				AttachedObligations: 2,
				HasSOPTask:          true,
			},
		}},
	}
	reserver := &fakeSweepStockReserver{err: errors.New("inventory: stock unavailable")}
	svc := NewSweeperService(repo, nil, reserver)

	result, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
	}
	if repo.stockBlockCalls != 2 {
		t.Fatalf("stock block calls = %d, want 2", repo.stockBlockCalls)
	}
	if repo.countBatchCalls != 1 || reserver.batchCalls != 1 {
		t.Fatalf("batch count/reserve calls=%d/%d, want 1/1", repo.countBatchCalls, reserver.batchCalls)
	}
	if result.Batches != 0 || result.Obligations != 0 {
		t.Fatalf("result = %#v, want no newly-created work for repair", result)
	}
}

func TestSweeperMarkMissedPagesUntilDrained(t *testing.T) {
	repo := &fakeSweepRepo{missedPages: []int{1000, 2}}
	svc := NewSweeperService(repo, nil, nil)

	total, err := svc.MarkMissed(context.Background(), "tenant-1", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MarkMissed: %v", err)
	}
	if total != 1002 || repo.missedCalls != 2 {
		t.Fatalf("total=%d calls=%d, want 1002/2", total, repo.missedCalls)
	}
}

func TestSweepVersionSnapshotReadsCandidatesInBoundedChunks(t *testing.T) {
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-3", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-4", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-5", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
	}
	baseRepo := &fakeSweepRepo{
		rows:      rows,
		attachAll: true,
	}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	svc.SetPageSize(2)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"version-1": {"obl-1", "obl-2", "obl-3", "obl-4", "obl-5"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshot(
		context.Background(),
		"tenant-1",
		"version-1",
		SweepConfig{ParkConsolidation: domain.ParkConsolidationSettings{Enabled: false}},
		time.Now(),
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshot: %v", err)
	}
	if result.Batches != 1 || result.Obligations != 5 {
		t.Fatalf("result = %#v, want one preserved group with five obligations", result)
	}
	wantSizes := []int{2, 2, 1}
	if len(repo.snapshotListSizes) != len(wantSizes) {
		t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
	}
	for i := range wantSizes {
		if repo.snapshotListSizes[i] != wantSizes[i] {
			t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
		}
	}
	if got := repo.createdBatchObligationIDs; len(got) != 1 || len(got[0]) != 5 {
		t.Fatalf("created batch ids = %#v, want one group containing all five snapshot rows", got)
	}
}

func TestSweepVersionSnapshotFallbackReadsCandidatesInBoundedChunks(t *testing.T) {
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-3", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-4", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-5", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
	}
	baseRepo := &fakeSweepRepo{
		rows:      rows,
		attachAll: true,
	}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	svc.SetPageSize(2)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"version-1": {"obl-1", "obl-2", "obl-3", "obl-4", "obl-5"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshot(
		context.Background(),
		"tenant-1",
		"version-1",
		SweepConfig{ParkConsolidation: domain.ParkConsolidationSettings{
			Enabled:             true,
			MinShedDriveTargets: 10,
			MinParkMergeTargets: 10,
			MinParkMergeSheds:   10,
		}},
		time.Now(),
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshot: %v", err)
	}
	if result.Batches == 0 || result.Obligations == 0 {
		t.Fatalf("result = %#v, want fallback to create at least one shed batch", result)
	}
	wantSizes := []int{2, 2, 1, 2, 2, 1}
	if len(repo.snapshotListSizes) != len(wantSizes) {
		t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
	}
	for i := range wantSizes {
		if repo.snapshotListSizes[i] != wantSizes[i] {
			t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
		}
	}
	if wantParkSizes := []int{2, 2, 1}; len(repo.parkSnapshotListSizes) != len(wantParkSizes) {
		t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
	} else {
		for i := range wantParkSizes {
			if repo.parkSnapshotListSizes[i] != wantParkSizes[i] {
				t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
			}
		}
	}
	if got := repo.createdBatchObligationIDs; len(got) == 0 {
		t.Fatalf("created batch ids = %#v, want fallback to create a shed batch", got)
	}
}

func TestSweepVersionSnapshotDefersTwoAnimalShedGroupToNearbyParkDrive(t *testing.T) {
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "tiny-1", RuleID: "rule-hs", ScopeType: "shed", ScopeID: "shed-y1", TargetID: "goat-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "tiny-2", RuleID: "rule-hs", ScopeType: "shed", ScopeID: "shed-y1", TargetID: "goat-2", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "near-1", RuleID: "rule-hs", ScopeType: "shed", ScopeID: "shed-y2", TargetID: "goat-3", DueAt: nextDay, WindowEnd: &winEnd},
		{ObligationID: "near-2", RuleID: "rule-hs", ScopeType: "shed", ScopeID: "shed-y3", TargetID: "goat-4", DueAt: nextDay, WindowEnd: &winEnd},
	}
	parkRows := []domain.ParkConsolidationCandidate{
		{ObligationID: "tiny-1", RuleID: "rule-hs", ParkID: "park-cbe", ShedID: "shed-y1", TargetID: "goat-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "tiny-2", RuleID: "rule-hs", ParkID: "park-cbe", ShedID: "shed-y1", TargetID: "goat-2", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "near-1", RuleID: "rule-hs", ParkID: "park-cbe", ShedID: "shed-y2", TargetID: "goat-3", DueAt: nextDay, WindowEnd: &winEnd},
		{ObligationID: "near-2", RuleID: "rule-hs", ParkID: "park-cbe", ShedID: "shed-y3", TargetID: "goat-4", DueAt: nextDay, WindowEnd: &winEnd},
	}
	baseRepo := &fakeSweepRepo{rows: rows, parkRows: parkRows, attachAll: true}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"version-1": {"tiny-1", "tiny-2", "near-1", "near-2"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshotAsOf(
		context.Background(),
		"tenant-1",
		"version-1",
		SweepConfig{
			DrivePlanner: domain.DrivePlannerSettings{Enabled: true},
			ParkConsolidation: domain.ParkConsolidationSettings{
				Enabled:             true,
				MinShedDriveTargets: 2,
				MinParkMergeTargets: 2,
				MinParkMergeSheds:   2,
			},
		},
		now,
		nextDay,
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshotAsOf: %v", err)
	}
	if result.ParkBatches != 1 || result.ParkObligations != 4 {
		t.Fatalf("result = %#v, want one 4-animal park batch", result)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1 park batch", len(repo.createdBatches))
	}
	batch := repo.createdBatches[0]
	if batch.ScopeType != "park" || batch.ScopeID != "park-cbe" {
		t.Fatalf("batch scope = %s/%s, want park/park-cbe", batch.ScopeType, batch.ScopeID)
	}
	if got := dateKey(batch.PlannedDate); got != "2026-08-05" {
		t.Fatalf("planned date = %s, want 2026-08-05", got)
	}
	if got := repo.createdBatchObligationIDs[0]; strings.Join(got, ",") != "tiny-1,tiny-2,near-1,near-2" {
		t.Fatalf("attached ids = %#v, want tiny obligations clubbed with nearby park work", got)
	}
}

func TestSweepVersionSnapshotParksWholeCandidateWindowBeforeShedFallback(t *testing.T) {
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	bigDriveDay := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "tiny-1", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-y1", TargetID: "goat-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "big-1", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-y2", TargetID: "goat-2", DueAt: bigDriveDay, WindowEnd: &winEnd},
		{ObligationID: "big-2", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-y2", TargetID: "goat-3", DueAt: bigDriveDay, WindowEnd: &winEnd},
		{ObligationID: "big-3", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-y2", TargetID: "goat-4", DueAt: bigDriveDay, WindowEnd: &winEnd},
	}
	parkRows := []domain.ParkConsolidationCandidate{
		{ObligationID: "tiny-1", RuleID: "rule-fmd", ParkID: "park-cpt", ShedID: "shed-y1", TargetID: "goat-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "big-1", RuleID: "rule-fmd", ParkID: "park-cpt", ShedID: "shed-y2", TargetID: "goat-2", DueAt: bigDriveDay, WindowEnd: &winEnd},
		{ObligationID: "big-2", RuleID: "rule-fmd", ParkID: "park-cpt", ShedID: "shed-y2", TargetID: "goat-3", DueAt: bigDriveDay, WindowEnd: &winEnd},
		{ObligationID: "big-3", RuleID: "rule-fmd", ParkID: "park-cpt", ShedID: "shed-y2", TargetID: "goat-4", DueAt: bigDriveDay, WindowEnd: &winEnd},
	}
	baseRepo := &fakeSweepRepo{rows: rows, parkRows: parkRows, attachAll: true}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"version-1": {"tiny-1", "big-1", "big-2", "big-3"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshotAsOf(
		context.Background(),
		"tenant-1",
		"version-1",
		SweepConfig{
			DrivePlanner: domain.DrivePlannerSettings{Enabled: true},
			ParkConsolidation: domain.ParkConsolidationSettings{
				Enabled:             true,
				MinShedDriveTargets: 2,
				MinParkMergeTargets: 2,
				MinParkMergeSheds:   2,
			},
		},
		now,
		bigDriveDay,
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshotAsOf: %v", err)
	}
	if result.ParkBatches != 1 || result.ParkObligations != 4 {
		t.Fatalf("result = %#v, want tiny obligation clubbed into one 4-animal park batch", result)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1 park batch", len(repo.createdBatches))
	}
	batch := repo.createdBatches[0]
	if batch.ScopeType != "park" || batch.ScopeID != "park-cpt" {
		t.Fatalf("batch scope = %s/%s, want park/park-cpt", batch.ScopeType, batch.ScopeID)
	}
	if got := dateKey(batch.PlannedDate); got != "2026-08-05" {
		t.Fatalf("planned date = %s, want 2026-08-05", got)
	}
	if got := strings.Join(repo.createdBatchObligationIDs[0], ","); got != "tiny-1,big-1,big-2,big-3" {
		t.Fatalf("attached ids = %s, want tiny row clubbed with larger nearby drive", got)
	}
}

func TestSweepVersionSnapshotDoesNotLeakToFullHWMScanWhenNoParkCandidatesRemain(t *testing.T) {
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", DueAt: time.Now()},
		{ObligationID: "obl-raced", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-raced", ParkID: "park-1", DueAt: time.Now()},
	}
	baseRepo := &fakeSweepRepo{
		rows:      rows,
		attachAll: true,
	}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	svc.SetPageSize(2)
	snapshot := &SweepCandidateSnapshot{byVersion: map[string][]string{
		"version-1": {"obl-1", "obl-2"},
	}}

	result, err := svc.SweepVersionWithSessionNoFinalizeSnapshot(
		context.Background(),
		"tenant-1",
		"version-1",
		SweepConfig{ParkConsolidation: domain.ParkConsolidationSettings{
			Enabled:             true,
			MinShedDriveTargets: 1,
			MinParkMergeTargets: 2,
			MinParkMergeSheds:   2,
		}},
		time.Now(),
		NewSweepSession(),
		time.Now(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("SweepVersionWithSessionNoFinalizeSnapshot: %v", err)
	}
	if result.Batches != 1 || result.Obligations != 2 {
		t.Fatalf("result = %#v, want only the two snapshot obligations batched", result)
	}
	for _, ids := range repo.createdBatchObligationIDs {
		for _, id := range ids {
			if id == "obl-raced" {
				t.Fatalf("raced non-snapshot obligation was batched: %#v", repo.createdBatchObligationIDs)
			}
		}
	}
	wantSizes := []int{2, 2}
	if len(repo.snapshotListSizes) != len(wantSizes) {
		t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
	}
	for i := range wantSizes {
		if repo.snapshotListSizes[i] != wantSizes[i] {
			t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
		}
	}
	wantParkSizes := []int{2}
	if len(repo.parkSnapshotListSizes) != len(wantParkSizes) {
		t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
	}
	for i := range wantParkSizes {
		if repo.parkSnapshotListSizes[i] != wantParkSizes[i] {
			t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
		}
	}
}

func TestPreflightSnapshotParkReplayReadsCandidatesInBoundedChunks(t *testing.T) {
	due := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-2", TargetID: "goat-2", DueAt: due},
		{ObligationID: "obl-3", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-3", TargetID: "goat-3", DueAt: due},
		{ObligationID: "obl-4", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-4", TargetID: "goat-4", DueAt: due},
		{ObligationID: "obl-5", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-5", TargetID: "goat-5", DueAt: due},
	}
	parkRows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", RuleID: "rule-1", ParkID: "park-1", ShedID: "shed-1", TargetID: "goat-1", DueAt: due},
		{ObligationID: "obl-2", RuleID: "rule-1", ParkID: "park-1", ShedID: "shed-2", TargetID: "goat-2", DueAt: due},
		{ObligationID: "obl-3", RuleID: "rule-1", ParkID: "park-1", ShedID: "shed-3", TargetID: "goat-3", DueAt: due},
		{ObligationID: "obl-4", RuleID: "rule-1", ParkID: "park-1", ShedID: "shed-4", TargetID: "goat-4", DueAt: due},
		{ObligationID: "obl-5", RuleID: "rule-1", ParkID: "park-1", ShedID: "shed-5", TargetID: "goat-5", DueAt: due},
	}
	baseRepo := &fakeSweepRepo{rows: rows, parkRows: parkRows, attachAll: true}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	svc.SetPageSize(2)

	_, err := svc.PreflightVisitShotCapTiesWithSnapshot(
		context.Background(),
		"tenant-1",
		[]SweepVersionPriority{{VersionID: "version-1", Config: SweepConfig{ParkConsolidation: domain.ParkConsolidationSettings{
			Enabled:             true,
			MinShedDriveTargets: 10,
			MinParkMergeTargets: 10,
			MinParkMergeSheds:   10,
		}}}},
		due,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("PreflightVisitShotCapTiesWithSnapshot: %v", err)
	}
	wantParkSizes := []int{2, 2, 1}
	if len(repo.parkSnapshotListSizes) != len(wantParkSizes) {
		t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
	}
	for i := range wantParkSizes {
		if repo.parkSnapshotListSizes[i] != wantParkSizes[i] {
			t.Fatalf("park snapshot list call sizes = %#v, want %#v", repo.parkSnapshotListSizes, wantParkSizes)
		}
	}
}

type fakeSweepRepo struct {
	rows                      []domain.UnbatchedDue
	rowsByVersion             map[string][]domain.UnbatchedDue // optional: per-version override for cross-version sweep tests
	parkRows                  []domain.ParkConsolidationCandidate
	createBatchID             string
	createBatchIDs            []string
	createBatchAttached       int64
	createBatchAttachedSeq    []int64
	attachAll                 bool
	createBatchCalls          int
	setTaskCalls              int
	batchSetTaskCalls         int
	stockBlockCalls           int
	clearStockBlockCalls      int
	countBatchCalls           int
	lastTaskID                string
	missedPages               []int
	missedCalls               int
	createdBatches            []domain.NewBatch
	createdBatchObligationIDs [][]string // obligation ids attached per createdBatches entry, same index
	finalizationPages         [][]domain.PlannedBatchFinalization
	finalizationCalls         int
	createdFinalization       []domain.PlannedBatchFinalization
	ruleCountsByBatch         map[string][]domain.RuleAttachmentCount
	parkListCalls             int
	repeatParkPage            bool
	upsertDriveAssignments    [][]domain.DriveAssignment
	replaceDriveAssignments   [][]domain.DriveAssignment // recorded ReplaceVaccinationDriveAssignmentsForBatch calls, in order
	replaceDriveBatchIDs      []string
	overrides                 map[string]domain.VaccineDriveDateOverride
}

type fakeDateVisitShotLockerRepo struct {
	*fakeSweepRepo
	persisted map[string]int32
}

type fakeVaccinationOperatorListRepo struct {
	*fakeSweepRepo
	operators         []string
	operatorCaps      map[string]int32
	zeroCapOperators  map[string]bool
	returnErr         error
	operatorListCalls int
}

func (f *fakeVaccinationOperatorListRepo) AvailableVaccinationOperatorsForDrive(_ context.Context, _, _ string, _ time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error) {
	f.operatorListCalls++
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	out := make([]domain.DriveOperatorCapacity, 0, len(f.operators))
	for _, operatorID := range f.operators {
		cap := capPerOperator
		if f.operatorCaps != nil && f.operatorCaps[operatorID] > 0 {
			cap = f.operatorCaps[operatorID]
		}
		if f.zeroCapOperators != nil && f.zeroCapOperators[operatorID] {
			// Mirrors the real repository query: Cap is GREATEST(daily_cap - loaded, 0), so an
			// operator already fully loaded for the day is reported present with remaining Cap 0,
			// never simply absent from the list.
			cap = 0
		}
		out = append(out, domain.DriveOperatorCapacity{OperatorID: operatorID, Cap: cap, ConfiguredCap: capPerOperator})
	}
	return out, nil
}

func (f *fakeDateVisitShotLockerRepo) CountVisitShotsForTargets(_ context.Context, _ string, targetIDs []string, date time.Time) (map[string]int32, error) {
	out := make(map[string]int32, len(targetIDs))
	for _, targetID := range targetIDs {
		out[targetID] = f.persisted[visitShotCountKey(date, targetID)]
	}
	return out, nil
}

func (f *fakeDateVisitShotLockerRepo) LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, func(context.Context) error, error) {
	counts, err := f.CountVisitShotsForTargets(ctx, tenantID, targetIDs, date)
	if err != nil {
		return nil, nil, err
	}
	return counts, func(context.Context) error { return nil }, nil
}

func (f *fakeSweepRepo) Ping(context.Context) error { return nil }

func (f *fakeSweepRepo) ActiveVaccinationDriveDateOverride(_ context.Context, tenantID, parkID, vaccineCode string, originalDate time.Time) (*domain.VaccineDriveDateOverride, error) {
	if f.overrides == nil {
		return nil, nil
	}
	override, ok := f.overrides[tenantID+"|"+parkID+"|"+strings.ToLower(strings.TrimSpace(vaccineCode))+"|"+businessDate(originalDate).Format("2006-01-02")]
	if !ok {
		return nil, nil
	}
	return &override, nil
}

func (f *fakeSweepRepo) InsertObligation(context.Context, domain.NewObligation) (string, bool, error) {
	return "", false, nil
}

func (f *fakeSweepRepo) CancelOpenObligationByIdempotencyKey(context.Context, string, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *fakeSweepRepo) GetByIdempotencyKey(context.Context, string, string) (domain.ObligationRef, error) {
	return domain.ObligationRef{}, ports.ErrNotFound
}

func (f *fakeSweepRepo) ListDue(context.Context, string, string, time.Time, int32) ([]domain.DueObligation, error) {
	return nil, nil
}

func (f *fakeSweepRepo) CountByScope(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func (f *fakeSweepRepo) CreateBatch(context.Context, domain.NewBatch) (string, error) {
	return "", nil
}

func (f *fakeSweepRepo) CreateBatchWithObligations(_ context.Context, in domain.NewBatch, ids []string) (string, int64, error) {
	batchID, attachedIDs, err := f.CreateBatchWithObligationsReturningAttachedIDs(context.Background(), in, ids)
	return batchID, int64(len(attachedIDs)), err
}

func (f *fakeSweepRepo) CreateBatchWithObligationsReturningAttachedIDs(_ context.Context, in domain.NewBatch, ids []string) (string, []string, error) {
	f.createBatchCalls++
	f.createdBatches = append(f.createdBatches, in)
	f.createdBatchObligationIDs = append(f.createdBatchObligationIDs, append([]string(nil), ids...))
	batchID := f.createBatchID
	if idx := f.createBatchCalls - 1; idx >= 0 && idx < len(f.createBatchIDs) {
		batchID = f.createBatchIDs[idx]
	}
	attached := f.createBatchAttached
	if idx := f.createBatchCalls - 1; idx >= 0 && idx < len(f.createBatchAttachedSeq) {
		attached = f.createBatchAttachedSeq[idx]
	}
	if f.attachAll {
		attached = int64(len(ids))
	}
	attachedIDs := []string(nil)
	if attached > 0 {
		attachedCount := int(attached)
		if attachedCount > len(ids) {
			attachedCount = len(ids)
		}
		attachedIDs = append([]string(nil), ids[:attachedCount]...)
		f.removeAttachedObligations(attachedIDs)
		f.createdFinalization = append(f.createdFinalization, domain.PlannedBatchFinalization{
			BatchID:             batchID,
			RuleID:              ruleIDFromSession(in.Session),
			ScopeType:           in.ScopeType,
			ScopeID:             in.ScopeID,
			PlannedDate:         in.PlannedDate,
			EstimatedTargets:    int32(attached),
			AttachedObligations: attached,
		})
	}
	return batchID, attachedIDs, nil
}

// UpsertVaccinationDriveAssignments and ReplaceVaccinationDriveAssignmentsForBatch let sweeper
// tests observe exactly what the production caller (batchDueGroup) persists for drive assignments
// without needing Postgres: they record the call, mirroring the real repository's contract
// (replace = delete-then-insert scoped to the batch; upsert = add/update only, never removes a
// stale key). Tests assert against these recorded slices instead of a live DB.
func (f *fakeSweepRepo) UpsertVaccinationDriveAssignments(_ context.Context, _ string, assignments []domain.DriveAssignment) error {
	f.upsertDriveAssignments = append(f.upsertDriveAssignments, append([]domain.DriveAssignment(nil), assignments...))
	return nil
}

func (f *fakeSweepRepo) ReplaceVaccinationDriveAssignmentsForBatch(_ context.Context, _, batchID string, assignments []domain.DriveAssignment) error {
	f.replaceDriveBatchIDs = append(f.replaceDriveBatchIDs, batchID)
	f.replaceDriveAssignments = append(f.replaceDriveAssignments, append([]domain.DriveAssignment(nil), assignments...))
	return nil
}

func (f *fakeSweepRepo) removeAttachedObligations(ids []string) {
	if len(ids) == 0 {
		return
	}
	attached := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		attached[id] = struct{}{}
	}
	f.rows = removeUnbatchedDueIDs(f.rows, attached)
	for versionID, rows := range f.rowsByVersion {
		f.rowsByVersion[versionID] = removeUnbatchedDueIDs(rows, attached)
	}
	f.parkRows = removeParkConsolidationIDs(f.parkRows, attached)
}

func removeUnbatchedDueIDs(rows []domain.UnbatchedDue, attached map[string]struct{}) []domain.UnbatchedDue {
	out := rows[:0]
	for _, row := range rows {
		if _, ok := attached[row.ObligationID]; ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

func removeParkConsolidationIDs(rows []domain.ParkConsolidationCandidate, attached map[string]struct{}) []domain.ParkConsolidationCandidate {
	out := rows[:0]
	for _, row := range rows {
		if _, ok := attached[row.ObligationID]; ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (f *fakeSweepRepo) SetBatchSOPTask(_ context.Context, _, _, taskID string) error {
	f.setTaskCalls++
	f.lastTaskID = taskID
	return nil
}

func (f *fakeSweepRepo) SetBatchSOPTasks(_ context.Context, _ string, taskIDsByBatch map[string]string) error {
	f.batchSetTaskCalls++
	for _, taskID := range taskIDsByBatch {
		f.setTaskCalls++
		f.lastTaskID = taskID
	}
	return nil
}

func (f *fakeSweepRepo) MarkBatchStockBlocked(context.Context, string, string, string, int64, string) error {
	f.stockBlockCalls++
	return nil
}

func (f *fakeSweepRepo) MarkBatchStockBlocks(_ context.Context, _ string, blocks []BatchStockBlock) error {
	f.stockBlockCalls += len(blocks)
	return nil
}

func (f *fakeSweepRepo) ClearBatchStockBlock(context.Context, string, string) error {
	f.clearStockBlockCalls++
	return nil
}

func (f *fakeSweepRepo) ClearBatchStockBlocks(_ context.Context, _ string, batchIDs []string) error {
	f.clearStockBlockCalls += len(batchIDs)
	return nil
}

func (f *fakeSweepRepo) ListPlannedBatchesNeedingFinalization(context.Context, string, string, bool, bool, *domain.PlannedBatchFinalizationCursor, int32) ([]domain.PlannedBatchFinalization, error) {
	if f.finalizationCalls >= len(f.finalizationPages) {
		if len(f.createdFinalization) > 0 {
			rows := f.createdFinalization
			f.createdFinalization = nil
			return rows, nil
		}
		return nil, nil
	}
	rows := f.finalizationPages[f.finalizationCalls]
	f.finalizationCalls++
	return rows, nil
}

func (f *fakeSweepRepo) ListUnbatchedDueForVersion(_ context.Context, _, versionID string, _ time.Time, _ int32) ([]domain.UnbatchedDue, error) {
	if f.rowsByVersion != nil {
		return normalizeFakeUnbatchedDue(f.rowsByVersion[versionID]), nil
	}
	return normalizeFakeUnbatchedDue(f.rows), nil
}

func normalizeFakeUnbatchedDue(rows []domain.UnbatchedDue) []domain.UnbatchedDue {
	out := make([]domain.UnbatchedDue, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].ScopeType == "shed" && out[i].ParkID == "" {
			out[i].ParkID = "park-for-" + out[i].ScopeID
		}
	}
	return out
}

type snapshotChunkFakeRepo struct {
	*fakeSweepRepo
	snapshotListSizes     []int
	parkSnapshotListSizes []int
}

func (f *snapshotChunkFakeRepo) ListUnbatchedDueForVersionSnapshot(_ context.Context, _, versionID string, _ time.Time, limit int32, _ time.Time, candidateIDs []string) ([]domain.UnbatchedDue, error) {
	f.snapshotListSizes = append(f.snapshotListSizes, len(candidateIDs))
	rows := f.rows
	if f.rowsByVersion != nil {
		rows = f.rowsByVersion[versionID]
	}
	out := filterUnbatchedDueSnapshot(normalizeFakeUnbatchedDue(rows), candidateIDs)
	if limit > 0 && int32(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeSweepRepo) ListUnbatchedShedDueForParkConsolidation(_ context.Context, _, _ string, _ time.Time, limit int32, after *domain.ParkConsolidationCursor) ([]domain.ParkConsolidationCandidate, error) {
	f.parkListCalls++
	if limit <= 0 {
		limit = int32(len(f.parkRows))
	}
	if f.repeatParkPage {
		end := int(limit)
		if end > len(f.parkRows) {
			end = len(f.parkRows)
		}
		return f.parkRows[:end], nil
	}
	start := 0
	if after != nil {
		for idx, row := range f.parkRows {
			if row.ObligationID == after.ObligationID {
				start = idx + 1
				break
			}
		}
	}
	if start >= len(f.parkRows) {
		return nil, nil
	}
	end := start + int(limit)
	if end > len(f.parkRows) {
		end = len(f.parkRows)
	}
	return f.parkRows[start:end], nil
}

func (f *snapshotChunkFakeRepo) ListUnbatchedShedDueForParkConsolidationSnapshot(_ context.Context, _, _ string, _ time.Time, limit int32, _ *domain.ParkConsolidationCursor, _ time.Time, candidateIDs []string) ([]domain.ParkConsolidationCandidate, error) {
	f.parkSnapshotListSizes = append(f.parkSnapshotListSizes, len(candidateIDs))
	out := filterParkConsolidationSnapshot(f.parkRows, candidateIDs)
	if limit > 0 && int32(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeSweepRepo) CountAttachedObligationsByRule(_ context.Context, _, batchID string) ([]domain.RuleAttachmentCount, error) {
	if f.ruleCountsByBatch != nil {
		if counts, ok := f.ruleCountsByBatch[batchID]; ok {
			return counts, nil
		}
	}
	for _, b := range f.createdFinalization {
		if b.BatchID != batchID {
			continue
		}
		return []domain.RuleAttachmentCount{{RuleID: b.RuleID, Count: b.AttachedObligations}}, nil
	}
	return nil, nil
}

func (f *fakeSweepRepo) CountAttachedObligationsByRuleForBatches(_ context.Context, _ string, batchIDs []string) (map[string][]domain.RuleAttachmentCount, error) {
	f.countBatchCalls++
	out := make(map[string][]domain.RuleAttachmentCount, len(batchIDs))
	for _, batchID := range batchIDs {
		counts, err := f.CountAttachedObligationsByRule(context.Background(), "", batchID)
		if err != nil {
			return nil, err
		}
		out[batchID] = counts
	}
	return out, nil
}

func (f *fakeSweepRepo) AttachObligationsToBatch(context.Context, string, string, []string) (int64, error) {
	return 0, nil
}

func (f *fakeSweepRepo) MarkCompleted(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeSweepRepo) ReopenObligation(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeSweepRepo) MarkMissedBefore(context.Context, string, time.Time, int32) (int, error) {
	if f.missedCalls >= len(f.missedPages) {
		return 0, nil
	}
	n := f.missedPages[f.missedCalls]
	f.missedCalls++
	return n, nil
}

func (f *fakeSweepRepo) GetBoosterContext(context.Context, string, string) (string, string, string, int32, error) {
	return "", "", "", 0, ports.ErrNotFound
}

func (f *fakeSweepRepo) ListOpenByGoat(context.Context, string, string, int32) ([]domain.OpenObligation, error) {
	return nil, nil
}

func (f *fakeSweepRepo) ReScopeOpenForGoat(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}

func (f *fakeSweepRepo) CancelOpenForGoat(context.Context, string, string, string) (int, error) {
	return 0, nil
}

func (f *fakeSweepRepo) SyncPartitionMoveForGoat(context.Context, string, string, string, string, string) (int, error) {
	return 0, nil
}

func (f *fakeSweepRepo) RecordStatusEvent(context.Context, domain.NewStatusEvent) (string, bool, error) {
	return "", false, nil
}

func (f *fakeSweepRepo) NextSuccessorSuffix(context.Context, string, string) (int, error) {
	return 1, nil
}

type fakeSweepTaskCreator struct {
	id            string
	calls         int
	batchCalls    int
	batchIDs      []string
	sopVersionIDs []string
}

func (f *fakeSweepTaskCreator) CreateTaskForBatch(_ context.Context, _, batchID, sopVersionID, _, _, _, _ string) (string, error) {
	f.calls++
	f.batchIDs = append(f.batchIDs, batchID)
	f.sopVersionIDs = append(f.sopVersionIDs, sopVersionID)
	if f.id == "" {
		f.id = "task-1"
	}
	return f.id, nil
}

func (f *fakeSweepTaskCreator) CreateTasksForBatches(ctx context.Context, tenantID string, batches []BatchTaskCreate) (map[string]string, error) {
	f.batchCalls++
	out := make(map[string]string, len(batches))
	for _, batch := range batches {
		taskID, err := f.CreateTaskForBatch(ctx, tenantID, batch.BatchID, batch.SOPVersionID, batch.TaskType, batch.Title, batch.ScopeType, batch.ScopeID)
		if err != nil {
			return nil, err
		}
		out[batch.BatchID] = taskID
	}
	return out, nil
}

type fakeSweepStockReserver struct {
	calls       int
	batchCalls  int
	lastBatchID string
	lastQty     int64
	validOns    []time.Time
	itemIDs     []string
	quantities  []int64
	err         error
}

func (f *fakeSweepStockReserver) ReserveForBatch(_ context.Context, _, batchID, _, itemID string, qty int64, validOn time.Time) error {
	f.calls++
	f.lastBatchID = batchID
	f.lastQty = qty
	f.validOns = append(f.validOns, validOn)
	f.itemIDs = append(f.itemIDs, itemID)
	f.quantities = append(f.quantities, qty)
	return f.err
}

func (f *fakeSweepStockReserver) ReserveForBatches(ctx context.Context, tenantID string, reservations []BatchStockReservation) (map[string]error, error) {
	f.batchCalls++
	failures := make(map[string]error)
	for _, req := range reservations {
		if err := f.ReserveForBatch(ctx, tenantID, req.BatchID, req.LocationID, req.ItemID, req.Qty, req.ValidOn); err != nil {
			failures[StockReservationKey(req)] = err
		}
	}
	return failures, nil
}

func ruleIDFromSession(session string) string {
	const prefix = "rule:"
	if len(session) > len(prefix) && session[:len(prefix)] == prefix {
		return session[len(prefix):]
	}
	return ""
}

func dateKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return businessDate(*t).Format("2006-01-02")
}

func validOnKeys(values []time.Time) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += businessDate(v).Format("2006-01-02")
	}
	return out
}

func qtyKeys(values []int64) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += strconv.FormatInt(v, 10)
	}
	return out
}

// TestLimitUnbatchedSelectionReservesCapacityForLastSafeRows is the VAXCAP-006 sweeper guard: at
// cap 1, a movable row listed FIRST must not consume the only cell a last-safe row needs.
func TestLimitUnbatchedSelectionReservesCapacityForLastSafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	lastSafe := planned
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-movable", TargetID: "goat-1", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-last-safe", TargetID: "goat-2", ParkID: "park-1", DueAt: planned, WindowEnd: &lastSafe},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitUnbatchedSelectionByDriveAnimals(planned, rows, []string{"obl-movable", "obl-last-safe"}, &planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-last-safe" {
		t.Fatalf("admitted = %#v, want only obl-last-safe (movable row must yield its cell)", out)
	}
}

// TestLimitUnbatchedSelectionAllLastSafeStillRespectsCap: when every selected row is on its last
// safe day, the operator-day cap is still hard.
func TestLimitUnbatchedSelectionAllLastSafeStillRespectsCap(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", TargetID: "goat-1", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-2", TargetID: "goat-2", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-3", TargetID: "goat-3", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitUnbatchedSelectionByDriveAnimals(planned, rows, []string{"obl-1", "obl-2", "obl-3"}, &planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-1" {
		t.Fatalf("admitted = %#v, want only the first last-safe row inside cap 1", out)
	}
}

func TestLimitUnbatchedSelectionRejectsPastWindowRideAlongOverflow(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	tightEnd := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	looseEnd := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-blue-tongue", TargetID: "goat-1", ParkID: "park-1", DueAt: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC), WindowEnd: &tightEnd, BatchingHoldCount: 1},
		{ObligationID: "obl-loose", TargetID: "goat-2", ParkID: "park-1", DueAt: planned, WindowEnd: &looseEnd},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitUnbatchedSelectionByDriveAnimals(now, rows, []string{"obl-blue-tongue", "obl-loose"}, &planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-loose" {
		t.Fatalf("admitted = %#v, want only loose-window row; past-window blue_tongue must not ride 07-31 batch", out)
	}
}

func TestLimitUnbatchedSelectionCountsDistinctAnimals(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-ettt", TargetID: "goat-1", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-ppr", TargetID: "goat-1", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-goat-2", TargetID: "goat-2", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 2

	out := limitUnbatchedSelectionByDriveAnimals(planned, rows, []string{"obl-ettt", "obl-ppr", "obl-goat-2"}, &planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 3 {
		t.Fatalf("admitted = %#v, want all obligations for two distinct animals within cap 2", out)
	}
}

func TestLimitUnbatchedSelectionCarriesWholePartitionPastResidualCapacity(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := make([]domain.UnbatchedDue, 0, 42)
	selected := make([]string, 0, 42)
	for i := 0; i < 42; i++ {
		id := fmt.Sprintf("obl-%02d", i+1)
		rows = append(rows, domain.UnbatchedDue{
			ObligationID: id,
			RuleID:       "rule-fmd",
			ScopeType:    "shed",
			ScopeID:      "shed-gandhi",
			ParkID:       "park-1",
			TargetID:     fmt.Sprintf("goat-%02d", i+1),
			ShedName:     "Gandhi 3",
			DueAt:        planned,
			WindowEnd:    &movableEnd,
		})
		selected = append(selected, id)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 4

	out := limitUnbatchedSelectionByDriveAnimals(planned, rows, selected, &planned, planner, 200, NewSweepSession())
	if len(out) != 0 {
		t.Fatalf("admitted = %#v, want no 4-animal fragment from Gandhi Part 3", out)
	}
}

func TestLimitUnbatchedSelectionOnlySplitsPartitionLargerThanCap(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := make([]domain.UnbatchedDue, 0, 6)
	selected := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("obl-oversized-%02d", i+1)
		rows = append(rows, domain.UnbatchedDue{
			ObligationID: id,
			RuleID:       "rule-fmd",
			ScopeType:    "shed",
			ScopeID:      "shed-gandhi",
			ParkID:       "park-1",
			TargetID:     fmt.Sprintf("goat-oversized-%02d", i+1),
			ShedName:     "Gandhi 3",
			DueAt:        planned,
			WindowEnd:    &movableEnd,
		})
		selected = append(selected, id)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 4

	out := limitUnbatchedSelectionByDriveAnimals(planned, rows, selected, &planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 4 {
		t.Fatalf("admitted = %#v, want 4 rows only because the partition itself exceeds cap 4", out)
	}
}

// TestPartialAttachScopesVaccinationDriveAssignmentsToAttachedIDs tests that drive assignments
// are rebuilt to scope ONLY the obligations that actually attached to the batch.
// This is a regression test for Bug #2: partial-attach ledger overcount.
// TestBatchDueGroupPartialAttachDoesNotPersistStaleDriveAssignmentForUnattachedShed reproduces the
// REAL F2 production path end to end via batchDueGroup (not the isolated driveAssignmentsForUnbatched
// helper the old test below only exercised): 2 selected obligations in DIFFERENT sheds/partitions of
// the same park; only 1 (obl-1, shed "Gandhi 1") attaches, obl-2 (shed "Godel 2") does not. Before the
// F2 fix, newBatch.DriveAssignments was built from BOTH sheds and handed to the create call BEFORE
// attachedIDs was known, so the repository's create-tx upsert would persist a row for the unattached
// shed too. After the fix: (1) the create call must carry NO drive assignments (the unfiltered
// pre-attach set must never reach it), and (2) the final persisted set -- via a REPLACE scoped to
// (tenant, batch) -- must contain exactly one assignment, for the attached shed only.
func TestBatchDueGroupPartialAttachDoesNotPersistStaleDriveAssignmentForUnattachedShed(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-gandhi-1", ParkID: "park-1", TargetID: "goat-1", ShedName: "Gandhi 1", DueAt: planned},
		{ObligationID: "obl-2", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-godel-2", ParkID: "park-1", TargetID: "goat-2", ShedName: "Godel 2", DueAt: planned},
	}
	repo := &fakeSweepRepo{createBatchID: "batch-1", createBatchAttached: 1} // only the first id (obl-1) attaches
	svc := NewSweeperService(repo, nil, nil)
	g := &dueGroup{
		scopeType: "shed",
		scopeID:   "shed-gandhi-1",
		ruleID:    "rule-ppr",
		parkID:    "park-1",
		ids:       []string{"obl-1", "obl-2"},
		rows:      rows,
	}
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2, MaxBatchingHoldDays: 7, MaxBatchingHoldCount: 1}

	batched, obligations, err := svc.batchDueGroup(context.Background(), "tenant-1", "version-ppr", SweepConfig{
		VaccineCode:  "PPR",
		DrivePlanner: planner,
	}, planner, planned, planned, NewSweepSession(), g)
	if err != nil {
		t.Fatalf("batchDueGroup: %v", err)
	}
	if !batched || obligations != 1 {
		t.Fatalf("batched=%v obligations=%d, want batched=true obligations=1 (only obl-1 attaches)", batched, obligations)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("createdBatches = %d, want 1", len(repo.createdBatches))
	}
	// Root-cause assertion: the create call must never carry the unfiltered pre-attach assignment
	// set. Before the fix this held 2 assignments (Gandhi 1 AND Godel 2).
	if got := repo.createdBatches[0].DriveAssignments; len(got) != 0 {
		t.Fatalf("create-call DriveAssignments = %#v, want empty -- the pre-attach set must never reach the create path", got)
	}
	// The final replace-scoped write must persist exactly the attached shed, nothing else.
	if len(repo.replaceDriveAssignments) != 1 {
		t.Fatalf("replaceDriveAssignments calls = %d, want exactly 1 replace-scoped write", len(repo.replaceDriveAssignments))
	}
	final := repo.replaceDriveAssignments[0]
	if len(final) != 1 {
		t.Fatalf("final persisted assignment set = %#v, want exactly 1 (attached shed only)", final)
	}
	if final[0].PhysicalShed != "Gandhi 1" || final[0].PartitionLabel != "whole" {
		t.Fatalf("final persisted assignment = %#v, want physical_shed=Gandhi 1 whole (obl-1's shed), not the unattached Godel/2 shed", final[0])
	}
	if final[0].AnimalCount != 1 {
		t.Fatalf("final persisted assignment animal_count = %d, want 1", final[0].AnimalCount)
	}
	// Never any leftover legacy upsert-only call for this batch either.
	if len(repo.upsertDriveAssignments) != 0 {
		t.Fatalf("upsertDriveAssignments calls = %#v, want none -- the replace path must be used, not the legacy upsert-only writer", repo.upsertDriveAssignments)
	}
}

func TestPartialAttachScopesVaccinationDriveAssignmentsToAttachedIDs(t *testing.T) {
	// Setup: 2 selected obligations, but only 1 attaches.
	// Build assignments from all selected (both), then scope to only attached (1).
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	selectedRows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", TargetID: "goat-1", ParkID: "park-1", ShedName: "shed-a", RuleID: "rule-1"},
		{ObligationID: "obl-2", TargetID: "goat-2", ParkID: "park-1", ShedName: "shed-a", RuleID: "rule-1"},
	}
	attachedIDs := []string{"obl-1"} // Only obl-1 attaches; obl-2 rejected

	// Initial: build assignments from all selectedRows (would include both animals before scoping)
	newBatch := domain.NewBatch{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		ScopeType:         "park",
		ScopeID:           "park-1",
		PlannedDate:       &planned,
	}
	allAssignments := driveAssignmentsForUnbatched("batch-id-full", newBatch, selectedRows)
	if len(allAssignments) != 1 {
		t.Fatalf("allAssignments count = %d, want 1 (one park/shed partition)", len(allAssignments))
	}
	if allAssignments[0].AnimalCount != 2 {
		t.Fatalf("allAssignments animal count = %d, want 2 (both goats before scoping)", allAssignments[0].AnimalCount)
	}

	// FIX: scope to only attached obligations
	attachedRows := selectedUnbatchedRows(selectedRows, attachedIDs)
	scopedAssignments := driveAssignmentsForUnbatched("batch-id-scoped", newBatch, attachedRows)

	// Verify: only 1 animal (for obl-1), not 2 (for both obl-1 and obl-2)
	if len(scopedAssignments) != 1 {
		t.Fatalf("scopedAssignments count = %d, want 1", len(scopedAssignments))
	}
	if scopedAssignments[0].AnimalCount != 1 {
		t.Fatalf("scopedAssignments animal count = %d, want 1 (only obl-1's goat after scoping to attached)", scopedAssignments[0].AnimalCount)
	}

	// Verify batch ID was updated
	if scopedAssignments[0].BatchID != "batch-id-scoped" {
		t.Fatalf("scopedAssignments BatchID = %s, want batch-id-scoped", scopedAssignments[0].BatchID)
	}
}

// P1 fail-closed leak (Codex): preflight.selectBestUnbatchedDriveDateWithVisitCap probe loop
// (line ~306-310) calls operatorCapacityPlanner then lockAndRefreshDriveCapacity WITHOUT
// checking driveOperatorCapacityExhausted, so cap 0 (exhausted) is treated as "uncapped/do not limit",
// admitting all animals onto a no-operator day. This test reproduces that leak and asserts the fix
// properly skips the day (admits nothing).
func TestPreflightProbeLoopFailsClosedOnOperatorCapExhausted(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{
			rowsByVersion: map[string][]domain.UnbatchedDue{
				"v-test": {
					{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-1", DueAt: planned, WindowEnd: &winEnd},
					{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-2", DueAt: planned, WindowEnd: &winEnd},
				},
			},
			attachAll: true,
		},
		returnErr: domain.ErrOperatorAssignmentConfigPresentButEmpty, // Config present but zero operators
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{
		Enabled:                   true,
		MaxGoatsPerDrive:          200, // Cap configured
		MaxShotsPerAnimalPerDrive: 10,
	}

	// Preflight: this should NOT find and lock a best date if all dates have no-operator exhaustion.
	// Before fix: selects obl-1 + obl-2 on planned date (cap 0 treated as unbounded → admits all).
	// After fix: no selection (date skipped on driveOperatorCapacityExhausted check).
	session := NewSweepSession()
	_, selectedIDs, _, err := svc.preflightBestUnbatchedDriveDateWithVisitCap(
		context.Background(),
		"tenant-1", planned, repo.rowsByVersion["v-test"],
		[]string{"goat-1", "goat-2"},
		&planned,
		planner,
		RuleVaccineIdentity{VaccineCode: "Test Vaccine", VaccinePriority: 1},
		1, // cellsPerObligation
		session,
	)
	if err != nil {
		t.Fatalf("preflightBestUnbatchedDriveDateWithVisitCap: %v", err)
	}
	// When operators are exhausted on all feasible dates, selectedIDs must be empty (no animals selected).
	// The function may return plannedDate (as a reference point), but selectedIDs should be nil/empty to signal
	// that no animals can be admitted.
	if len(selectedIDs) != 0 {
		t.Fatalf("leak: selectedIDs=%v, want empty (operators exhausted, must admit nothing)", selectedIDs)
	}
}

// P1 fail-closed leak (Codex): preflight chosen-best-date path (line ~343-347) same pattern as probe loop.
func TestPreflightBestDateFailsClosedOnOperatorCapExhausted(t *testing.T) {
	// Setup identical to probe test, but no other feasible dates so the best-date re-evaluation fires.
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) // Same day, no overflow window
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{
			rowsByVersion: map[string][]domain.UnbatchedDue{
				"v-test": {
					{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1", ParkID: "park-1", TargetID: "goat-1", DueAt: planned, WindowEnd: &winEnd},
				},
			},
			attachAll: true,
		},
		returnErr: domain.ErrOperatorAssignmentConfigPresentButEmpty,
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{
		Enabled:                   true,
		MaxGoatsPerDrive:          200,
		MaxShotsPerAnimalPerDrive: 10,
	}

	session := NewSweepSession()
	_, selectedIDs, _, err := svc.preflightBestUnbatchedDriveDateWithVisitCap(
		context.Background(),
		"tenant-1", planned, repo.rowsByVersion["v-test"],
		[]string{"goat-1"},
		&planned,
		planner,
		RuleVaccineIdentity{VaccineCode: "Test Vaccine", VaccinePriority: 1},
		1,
		session,
	)
	if err != nil {
		t.Fatalf("preflightBestUnbatchedDriveDateWithVisitCap: %v", err)
	}
	// When operators are exhausted on all feasible dates (including the only feasible date),
	// selectedIDs must be empty to signal that no animals can be admitted.
	if len(selectedIDs) != 0 {
		t.Fatalf("leak: selectedIDs=%v, want empty (operators exhausted on only feasible date)", selectedIDs)
	}
}

// P1 fail-closed leak (Codex): park_consolidation.parkMergeStep (line ~142) and selectBestParkDriveDateWithCapacity
// (line ~519) call operatorCapacityPlanner then lockAndRefreshDriveCapacity WITHOUT driveOperatorCapacityExhausted check.
func TestParkConsolidationFailsClosedOnOperatorCapExhausted(t *testing.T) {
	planned := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	repo := &fakeVaccinationOperatorListRepo{
		fakeSweepRepo: &fakeSweepRepo{
			parkRows: []domain.ParkConsolidationCandidate{
				{
					ObligationID:      "obl-park-1",
					RuleID:            "rule-1",
					TargetID:          "goat-1",
					ParkID:            "park-1",
					ShedName:          "shed-a",
					TargetSpecies:     "goat",
					TargetAnimalStage: "kid",
					DueAt:             planned,
				},
				{
					ObligationID:      "obl-park-2",
					RuleID:            "rule-1",
					TargetID:          "goat-2",
					ParkID:            "park-1",
					ShedName:          "shed-a",
					TargetSpecies:     "goat",
					TargetAnimalStage: "kid",
					DueAt:             planned,
				},
			},
		},
		returnErr: domain.ErrOperatorAssignmentConfigPresentButEmpty, // Config present, zero operators
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{
		Enabled:          true,
		MaxGoatsPerDrive: 200,
	}
	cfg := SweepConfig{
		VaccineCode:  "Test Vaccine",
		DrivePlanner: planner,
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-1": {DosesPerGoat: 1},
		},
		ParkConsolidation: domain.ParkConsolidationSettings{
			Enabled:             true,
			MinParkMergeTargets: 1,
		},
	}

	// Park consolidation should NOT admit animals when operators are exhausted.
	result, err := svc.consolidateParkDrivesWithVisitCounts(
		context.Background(),
		"tenant-1", "version-1", cfg,
		time.Time{}, planned,
		planner,
		NewSweepSession(),
		time.Time{},
		nil,
	)
	if err != nil {
		t.Fatalf("consolidateParkDrivesWithVisitCounts: %v", err)
	}
	if result.ParkBatches != 0 || result.ParkObligations != 0 {
		t.Fatalf("leak: park result=%#v, want 0 batches/obligations (operators exhausted, must skip day)", result)
	}
}
