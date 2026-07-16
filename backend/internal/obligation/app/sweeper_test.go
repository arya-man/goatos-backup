package app

import (
	"context"
	"errors"
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

func TestSweeperMarksBatchBlockedWhenStockReservationFails(t *testing.T) {
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", ScopeType: "park", ScopeID: "park-1"},
			{ObligationID: "obl-2", ScopeType: "park", ScopeID: "park-1"},
			{ObligationID: "obl-3", ScopeType: "park", ScopeID: "park-2"},
			{ObligationID: "obl-4", ScopeType: "park", ScopeID: "park-2"},
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

func TestSweeperCreatesSideEffectsAfterAttach(t *testing.T) {
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-1", ScopeType: "park", ScopeID: "park-1"},
			{ObligationID: "obl-2", ScopeType: "park", ScopeID: "park-1"},
			{ObligationID: "obl-raced", ScopeType: "park", ScopeID: "park-1"},
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

func TestSweeperGroupsByRuleAndDueDateAndReservesAgainstPlannedDate(t *testing.T) {
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
	}, time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SweepVersion: %v", err)
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
	if got := dateKey(repo.createdBatches[0].PlannedDate); got != "2026-08-14" {
		t.Fatalf("batch A planned date = %s, want 2026-08-14", got)
	}
	if got := dateKey(repo.createdBatches[1].PlannedDate); got != "2026-08-15" {
		t.Fatalf("batch B planned date = %s, want 2026-08-15", got)
	}
	if reserver.calls != 2 {
		t.Fatalf("reservation calls = %d, want 2", reserver.calls)
	}
	if got := validOnKeys(reserver.validOns); got != "2026-08-14,2026-08-15" {
		t.Fatalf("reservation validOn dates = %s, want planned dates", got)
	}
}

func TestSweeperUsesRuleSpecificExecutionConfig(t *testing.T) {
	due := time.Date(2026, time.August, 14, 9, 30, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rows: []domain.UnbatchedDue{
			{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", DueAt: due},
			{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", DueAt: due},
		},
		createBatchIDs: []string{"batch-a", "batch-b"},
		attachAll:      true,
	}
	tasks := &fakeSweepTaskCreator{}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		SOPVersionID:  "sop-default",
		VaccineItemID: "vaccine-default",
		DosesPerGoat:  1,
		RuleConfigs: map[string]SweepRuleConfig{
			"rule-b": {SOPVersionID: "sop-rule-b", VaccineItemID: "vaccine-rule-b", DosesPerGoat: 3},
		},
	}, time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC))
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
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want FMD overflowed off 2026-07-01 instead of a 3rd same-day shot", dates)
	}
	if dates["obl-fmd"] == "" {
		t.Fatalf("dates=%#v, want FMD batched on a later safe date, not dropped", dates)
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
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1"},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1"},
		{ObligationID: "obl-3", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1"},
		{ObligationID: "obl-4", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1"},
		{ObligationID: "obl-5", RuleID: "rule-1", ScopeType: "park", ScopeID: "park-1"},
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
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-3", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-4", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-5", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
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

func TestSweepVersionSnapshotDoesNotLeakToFullHWMScanWhenNoParkCandidatesRemain(t *testing.T) {
	rows := []domain.UnbatchedDue{
		{ObligationID: "obl-1", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-2", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-1"},
		{ObligationID: "obl-raced", RuleID: "rule-1", ScopeType: "shed", ScopeID: "shed-raced"},
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
			MinShedDriveTargets: 2,
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
	wantSizes := []int{2, 0}
	if len(repo.snapshotListSizes) != len(wantSizes) {
		t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
	}
	for i := range wantSizes {
		if repo.snapshotListSizes[i] != wantSizes[i] {
			t.Fatalf("snapshot list call sizes = %#v, want %#v", repo.snapshotListSizes, wantSizes)
		}
	}
	wantParkSizes := []int{0}
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
	parkListCalls             int
	repeatParkPage            bool
}

func (f *fakeSweepRepo) Ping(context.Context) error { return nil }

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
	f.createBatchCalls++
	f.createdBatches = append(f.createdBatches, in)
	f.createdBatchObligationIDs = append(f.createdBatchObligationIDs, append([]string(nil), ids...))
	batchID := f.createBatchID
	if idx := f.createBatchCalls - 1; idx >= 0 && idx < len(f.createBatchIDs) {
		batchID = f.createBatchIDs[idx]
	}
	attached := f.createBatchAttached
	if f.attachAll {
		attached = int64(len(ids))
	}
	if attached > 0 {
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
	return batchID, attached, nil
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
		return f.rowsByVersion[versionID], nil
	}
	return f.rows, nil
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
	out := filterUnbatchedDueSnapshot(rows, candidateIDs)
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

func (f *fakeSweepRepo) RecordStatusEvent(context.Context, domain.NewStatusEvent) (string, bool, error) {
	return "", false, nil
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
