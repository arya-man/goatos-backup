package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestPreflightVisitShotCapTiesDetectsCrossVersionTieWriteFree is the VAX-REV-04 guard: when 3
// vaccines (2 at equal, unresolved priority) compete for one animal's over-cap visit, the
// write-free preflight must surface *ShotCapPriorityTieError WITHOUT ever calling
// CreateBatchWithObligations, spawning a SOP task, or reserving stock -- proving that an
// orchestrator following the "preflight, then sweep for real only if clean" pattern (see
// kernelstages.ObligationSweeperStage.Run and cmd/obligation-sweeper) leaves ZERO new
// batches/tasks/reservations behind for a run that ultimately reports failure, instead of the
// pre-fix behavior where earlier-processed versions' writes were already committed by the time a
// later version's tie aborted the loop.
func TestPreflightVisitShotCapTiesDetectsCrossVersionTieWriteFree(t *testing.T) {
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
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	// v-alpha and v-beta would legitimately fill the visit's 2 slots; v-gamma resolves to the
	// SAME unconfigured default priority as whichever of them claims the last slot, so it ties --
	// a genuine, unresolved cross-version conflict, exactly like
	// TestSweepVersionWithSessionBlocksOnUnresolvedPriorityTie but exercised through the preflight
	// entry point instead of the real per-version sweep loop.
	plans := []SweepVersionPriority{
		{VersionID: "v-alpha", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Alpha", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
		{VersionID: "v-beta", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Beta", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
		{VersionID: "v-gamma", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Gamma", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
	}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due, time.Time{})
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie error target = %q, want goat-1", tieErr.TargetID)
	}

	// The core assertion: an orchestrator that checks this error before its real sweep loop (as
	// kernelstages.Run and cmd/obligation-sweeper now do) never reaches CreateBatchWithObligations,
	// SOP task creation, or stock reservation for ANY of the 3 plans -- not just v-gamma.
	if repo.createBatchCalls != 0 {
		t.Fatalf("createBatchCalls = %d, want 0 (preflight must not write any batch)", repo.createBatchCalls)
	}
	if len(repo.createdBatches) != 0 {
		t.Fatalf("createdBatches = %#v, want none", repo.createdBatches)
	}
	if tasks.calls != 0 {
		t.Fatalf("SOP task calls = %d, want 0", tasks.calls)
	}
	if reserver.calls != 0 {
		t.Fatalf("stock reservation calls = %d, want 0", reserver.calls)
	}
}

// TestPreflightVisitShotCapTiesCleanWhenNoConflict is the negative-case companion: when the
// competing plans resolve to distinct priorities and never tie (the same fixture
// TestSweepVersionWithSessionSharesShotCapAcrossVersions exercises for the real sweep), the
// preflight must return nil -- it should never false-positive-block a legitimate multi-vaccine
// sweep. The real sweep is then run against a SEPARATE, fresh session (mirroring how the
// production caller uses the preflight's own throwaway session only for detection) and produces
// the same result as without a preflight at all.
func TestPreflightVisitShotCapTiesCleanWhenNoConflict(t *testing.T) {
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
	plans := []SweepVersionPriority{
		{VersionID: "v-ettt", Config: SweepConfig{VaccineCode: "ET+TT", DrivePlanner: planner}},
		{VersionID: "v-ppr", Config: SweepConfig{VaccineCode: "PPR", DrivePlanner: planner}},
		{VersionID: "v-fmd", Config: SweepConfig{VaccineCode: "FMD", DrivePlanner: planner}},
	}
	plans = SortSweepVersionsByPriority(plans)

	if err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due, time.Time{}); err != nil {
		t.Fatalf("PreflightVisitShotCapTies: %v, want nil (no unresolved tie)", err)
	}

	session := NewSweepSession()
	for _, plan := range plans {
		if _, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", plan.VersionID, plan.Config, due, session); err != nil {
			t.Fatalf("sweep %s: %v", plan.VersionID, err)
		}
	}
	dates := obligationIDPlannedDates(repo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want ET+TT and PPR both on 2026-07-01", dates)
	}
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want FMD overflowed off 2026-07-01", dates)
	}
}

func TestPreflightVisitShotCapTiesDetectsFallbackOnlyTieWriteFree(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	baseRepo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-matrix": {
				{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-hs", RuleID: "rule-hs", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
			},
		},
		attachAll: true,
	}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
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
			"rule-fmd": {VaccineCode: "Unmapped FMD", VaccinePriority: 5},
			"rule-ppr": {VaccineCode: "Unmapped PPR", VaccinePriority: 5},
			"rule-hs":  {VaccineCode: "Unmapped HS", VaccinePriority: 5},
		},
	}

	_, err := svc.PreflightVisitShotCapTiesWithSnapshot(
		context.Background(),
		"tenant-1",
		[]SweepVersionPriority{{VersionID: "v-matrix", Config: cfg}},
		due,
		time.Now(),
	)
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError from fallback replay", err)
	}
	if repo.createBatchCalls != 0 {
		t.Fatalf("createBatchCalls = %d, want 0 (preflight must stay write-free)", repo.createBatchCalls)
	}
}

func TestPreflightVisitShotCapTiesReplaysFallbackBeforeNextPlan(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	baseRepo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-a": {{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-b": {{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-c": {{ObligationID: "obl-c", RuleID: "rule-c", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	repo := &snapshotChunkFakeRepo{fakeSweepRepo: baseRepo}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	park := domain.ParkConsolidationSettings{
		Enabled:             true,
		MinShedDriveTargets: 10,
		MinParkMergeTargets: 1,
		MinParkMergeSheds:   2,
	}
	plans := []SweepVersionPriority{
		{VersionID: "v-a", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Alpha", DrivePlanner: planner, ParkConsolidation: park}},
		{VersionID: "v-b", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Beta", DrivePlanner: planner}},
		{VersionID: "v-c", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Gamma", DrivePlanner: planner}},
	}
	plans = SortSweepVersionsByPriority(plans)

	_, err := svc.PreflightVisitShotCapTiesWithSnapshot(context.Background(), "tenant-1", plans, due, time.Now())
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError after per-plan fallback replay", err)
	}
	if repo.createBatchCalls != 0 {
		t.Fatalf("createBatchCalls = %d, want 0 (preflight must stay write-free)", repo.createBatchCalls)
	}
}
