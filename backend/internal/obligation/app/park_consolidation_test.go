package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestPickBestParkDriveDateMaximizesFeasibleGoats(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-1",
			TargetID:     "goat-1",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			TargetID:     "goat-2",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			TargetID:     "goat-3",
			ShedID:       "shed-c",
			DueAt:        time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)),
		},
	}

	planned, ids := pickBestParkDriveDate(now, rows, 2)
	if planned == nil {
		t.Fatal("planned date is nil")
	}
	if got := businessDate(*planned).Format("2006-01-02"); got != "2026-07-05" {
		t.Fatalf("planned date = %s, want 2026-07-05", got)
	}
	if len(ids) != 3 {
		t.Fatalf("selected ids = %#v, want all three obligations", ids)
	}
}

func TestPickBestParkDriveDateRespectsLatestWindow(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-late",
			TargetID:     "goat-late",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-ok",
			TargetID:     "goat-ok",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
	}

	_, ids := pickBestParkDriveDate(now, rows, 1)
	if len(ids) != 1 || ids[0] != "obl-ok" {
		t.Fatalf("selected ids = %#v, want only obl-ok", ids)
	}
}

func TestParkObligationNilWindowEndIsBoundedToDueDate(t *testing.T) {
	row := domain.ParkConsolidationCandidate{
		ObligationID: "obl-1",
		TargetID:     "goat-1",
		ShedID:       "shed-a",
		DueAt:        time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	}
	if !parkObligationFeasibleOnDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), row) {
		t.Fatal("nil window_end obligation must be feasible on due date")
	}
	if parkObligationFeasibleOnDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC), row) {
		t.Fatal("nil window_end obligation must not be treated as unbounded after due date")
	}
}

func TestConsolidateParkDrivesCanMergeSameShedAnimalsAtParkLevel(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-1",
				TargetID:     "goat-1",
				RuleID:       "rule-a",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-2",
				TargetID:     "goat-2",
				RuleID:       "rule-a",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
		},
		createBatchAttached: 2,
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 || repo.createBatchCalls != 1 {
		t.Fatalf("park batches = %d obligations = %d create calls = %d, want same-shed animals merged at park level", res.ParkBatches, res.ParkObligations, repo.createBatchCalls)
	}
}

func TestConsolidateParkDrivesHonorsAnimalCapOnNextSafeDay(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", TargetID: "goat-1", RuleID: "rule-a", ShedID: "shed-1", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "obl-2", TargetID: "goat-2", RuleID: "rule-a", ShedID: "shed-1", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "obl-3", TargetID: "goat-3", RuleID: "rule-a", ShedID: "shed-2", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "obl-4", TargetID: "goat-4", RuleID: "rule-a", ShedID: "shed-2", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
	}
	repo := &fakeSweepRepo{parkRows: rows, attachAll: true}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:          true,
			MaxGoatsPerDrive: 2,
		},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, now)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 2 || res.ParkObligations != 4 {
		t.Fatalf("result = %#v, want two capped park batches covering four animals", res)
	}
	if len(repo.createdBatches) != 2 {
		t.Fatalf("created batches = %d, want 2", len(repo.createdBatches))
	}
	if got := dateKey(repo.createdBatches[0].PlannedDate); got != "2026-08-10" {
		t.Fatalf("first planned date = %s, want 2026-08-10", got)
	}
	if got := dateKey(repo.createdBatches[1].PlannedDate); got != "2026-08-11" {
		t.Fatalf("second planned date = %s, want next safe day 2026-08-11", got)
	}
	for i, batch := range repo.createdBatches {
		if batch.EstimatedTargets != 2 {
			t.Fatalf("batch %d estimated targets = %d, want 2 distinct animals", i, batch.EstimatedTargets)
		}
	}
}

func TestConsolidateParkDrivesAllowsOverCapOnLastSafeDay(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", TargetID: "goat-1", RuleID: "rule-a", ShedID: "shed-1", ParkID: "park-1", DueAt: now, WindowEnd: &now},
		{ObligationID: "obl-2", TargetID: "goat-2", RuleID: "rule-a", ShedID: "shed-1", ParkID: "park-1", DueAt: now, WindowEnd: &now},
		{ObligationID: "obl-3", TargetID: "goat-3", RuleID: "rule-a", ShedID: "shed-2", ParkID: "park-1", DueAt: now, WindowEnd: &now},
	}
	repo := &fakeSweepRepo{parkRows: rows, attachAll: true}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:          true,
			MaxGoatsPerDrive: 2,
		},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, now)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 3 {
		t.Fatalf("result = %#v, want one over-cap park batch because all animals are on last safe day", res)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(repo.createdBatches))
	}
	if repo.createdBatches[0].EstimatedTargets != 3 {
		t.Fatalf("estimated targets = %d, want 3 animals despite cap 2", repo.createdBatches[0].EstimatedTargets)
	}
}

func TestConsolidateParkDrivesWalksPastUnderThresholdShotCapDate(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", TargetID: "goat-1", RuleID: "rule-a", ShedID: "shed-1", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "obl-2", TargetID: "goat-2", RuleID: "rule-a", ShedID: "shed-2", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
		{ObligationID: "obl-3", TargetID: "goat-3", RuleID: "rule-a", ShedID: "shed-3", ParkID: "park-1", DueAt: now, WindowEnd: &winEnd},
	}
	base := &fakeSweepRepo{parkRows: rows, attachAll: true}
	repo := &fakeDateVisitShotLockerRepo{
		fakeSweepRepo: base,
		persisted: map[string]int32{
			visitShotCountKey(now, "goat-2"): 1,
			visitShotCountKey(now, "goat-3"): 1,
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:                   true,
			MaxShotsPerAnimalPerDrive: 1,
		},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, now)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 3 {
		t.Fatalf("result = %#v, want one later park batch with all three animals", res)
	}
	if got := dateKey(base.createdBatches[0].PlannedDate); got != "2026-08-11" {
		t.Fatalf("planned date = %s, want 2026-08-11 after under-threshold cap day", got)
	}
}

func TestPickBestParkDriveDateRanksDistinctAnimalsBeforeObligationRows(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "goat-1-fmd",
			TargetID:     "goat-1",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "goat-1-hs",
			TargetID:     "goat-1",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "goat-2-fmd",
			TargetID:     "goat-2",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "goat-3-fmd",
			TargetID:     "goat-3",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)),
		},
	}

	planned, ids := pickBestParkDriveDate(now, rows, 2)
	if planned == nil {
		t.Fatal("planned date is nil")
	}
	if got := businessDate(*planned).Format("2006-01-02"); got != "2026-08-02" {
		t.Fatalf("planned date = %s, want 2026-08-02 with two distinct animals", got)
	}
	if strings.Join(ids, ",") != "goat-2-fmd,goat-3-fmd" {
		t.Fatalf("selected ids = %#v, want two distinct animal obligations", ids)
	}
}

func TestConsolidateParkDrivesCreatesParkBatchAcrossSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-1",
				TargetID:     "goat-1",
				RuleID:       "rule-a",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-2",
				TargetID:     "goat-2",
				RuleID:       "rule-a",
				ShedID:       "shed-2",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
			},
		},
		createBatchAttached: 2,
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one park batch with two obligations", res)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(repo.createdBatches))
	}
	batch := repo.createdBatches[0]
	if batch.ScopeType != "park" || batch.ScopeID != "park-1" {
		t.Fatalf("batch scope = %s/%s, want park/park-1", batch.ScopeType, batch.ScopeID)
	}
	if batch.Session != "park-consolidation:obl-1" {
		t.Fatalf("batch session = %q", batch.Session)
	}
}

func TestConsolidateParkDrivesPagesParkCandidatesWithCursor(t *testing.T) {
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-1",
			TargetID:     "goat-1",
			RuleID:       "rule-a",
			ShedID:       "shed-1",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			TargetID:     "goat-2",
			RuleID:       "rule-a",
			ShedID:       "shed-2",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			TargetID:     "goat-3",
			RuleID:       "rule-a",
			ShedID:       "shed-3",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-4",
			TargetID:     "goat-4",
			RuleID:       "rule-a",
			ShedID:       "shed-4",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
	}
	repo := &fakeSweepRepo{parkRows: rows, attachAll: true}
	svc := NewSweeperService(repo, nil, nil)
	svc.page = 2

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if repo.parkListCalls != 3 {
		t.Fatalf("park candidate list calls = %d, want 3 pages including final empty page", repo.parkListCalls)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 4 {
		t.Fatalf("result = %#v, want one park batch with all four obligations", res)
	}
}

func TestConsolidateParkDrivesFailsWhenCandidateCursorDoesNotAdvance(t *testing.T) {
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-1",
			TargetID:     "goat-1",
			RuleID:       "rule-a",
			ShedID:       "shed-1",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			TargetID:     "goat-2",
			RuleID:       "rule-a",
			ShedID:       "shed-2",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
	}
	repo := &fakeSweepRepo{parkRows: rows, repeatParkPage: true}
	svc := NewSweeperService(repo, nil, nil)
	svc.page = 2

	_, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "pagination did not advance") {
		t.Fatalf("error = %v, want pagination progress failure", err)
	}
}

func TestConsolidateParkDrivesMergesDifferentVaccinesAcrossSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-et",
				TargetID:     "goat-et",
				RuleID:       "rule-et",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-tt",
				TargetID:     "goat-tt",
				RuleID:       "rule-tt",
				ShedID:       "shed-2",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
		},
		createBatchAttached: 2,
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one multi-vaccine park batch", res)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(repo.createdBatches))
	}
	if repo.createdBatches[0].PlannedQuantity != "2" {
		t.Fatalf("planned quantity = %q, want 2 doses across rules", repo.createdBatches[0].PlannedQuantity)
	}
}

func TestPickBestParkDriveDateIteratesRemainder(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-1",
			TargetID:     "goat-1",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			TargetID:     "goat-2",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			TargetID:     "goat-3",
			ShedID:       "shed-c",
			DueAt:        time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-4",
			TargetID:     "goat-4",
			ShedID:       "shed-d",
			DueAt:        time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
	}
	firstDate, firstIDs := pickBestParkDriveDate(now, rows, 2)
	if len(firstIDs) != 2 {
		t.Fatalf("first pass ids = %#v, want 2", firstIDs)
	}
	remaining := removeRows(rows, firstIDs)
	secondDate, secondIDs := pickBestParkDriveDate(now, remaining, 2)
	if secondDate == nil || len(secondIDs) != 2 {
		t.Fatalf("second pass date=%v ids=%#v, want 2 goats on another date", secondDate, secondIDs)
	}
	if firstDate.Equal(*secondDate) {
		t.Fatalf("expected two different drive dates, both %s", firstDate.Format("2006-01-02"))
	}
}

func TestParkDriveWindowUsesSelectedIntersection(t *testing.T) {
	startA := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	endA := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	startB := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	endB := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

	windowStart, windowEnd := parkDriveWindow([]domain.ParkConsolidationCandidate{
		{ObligationID: "obl-a", DueAt: startA, WindowStart: &startA, WindowEnd: &endA},
		{ObligationID: "obl-b", DueAt: startB, WindowStart: &startB, WindowEnd: &endB},
	}, []string{"obl-a", "obl-b"})
	if got := dateKey(windowStart); got != "2026-08-12" {
		t.Fatalf("window start = %s, want latest selected start 2026-08-12", got)
	}
	if got := dateKey(windowEnd); got != "2026-08-18" {
		t.Fatalf("window end = %s, want binding selected safe-until 2026-08-18", got)
	}
}

// fakeParkDriveCapacityRepo overlays a persisted park/date dose-cell ledger onto fakeSweepRepo so
// capacity-aware date scoring (VAXCAP-005) can be exercised without Postgres.
type fakeParkDriveCapacityRepo struct {
	*fakeSweepRepo
	cells map[string]int32 // driveCapacityKey(parkID, date) -> persisted cells
}

func (f *fakeParkDriveCapacityRepo) CountDriveCellsForParkDate(_ context.Context, _ string, parkID string, date time.Time) (int32, error) {
	return f.cells[driveCapacityKey(parkID, date)], nil
}

// TestConsolidateParkDrivesPicksLaterDateWithMoreFreeCapacity is the VAXCAP-005 guard scenario:
// D1 has two free cells, D2 has ten; ten safe animals must produce ONE D2 drive, never a 2+8 split.
func TestConsolidateParkDrivesPicksLaterDateWithMoreFreeCapacity(t *testing.T) {
	d1 := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	rows := make([]domain.ParkConsolidationCandidate, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, domain.ParkConsolidationCandidate{
			ObligationID: "obl-" + string(rune('a'+i)),
			TargetID:     "goat-" + string(rune('a'+i)),
			RuleID:       "rule-a",
			ShedID:       "shed-1",
			ParkID:       "park-1",
			DueAt:        d1,
			WindowEnd:    &winEnd,
		})
	}
	base := &fakeSweepRepo{parkRows: rows, attachAll: true}
	repo := &fakeParkDriveCapacityRepo{
		fakeSweepRepo: base,
		cells: map[string]int32{
			driveCapacityKey("park-1", d1): 8, // cap 10 => only 2 free on D1
			// D2 has no persisted cells => 10 free
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:          true,
			MaxGoatsPerDrive: 10,
		},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, d1)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 10 {
		t.Fatalf("result = %#v, want ONE full drive on the free-capacity date, never a 2+8 split", res)
	}
	if len(base.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(base.createdBatches))
	}
	if got := dateKey(base.createdBatches[0].PlannedDate); got != "2026-08-11" {
		t.Fatalf("planned date = %s, want 2026-08-11 (the date whose free capacity fits all ten animals)", got)
	}
	if base.createdBatches[0].EstimatedTargets != 10 {
		t.Fatalf("estimated targets = %d, want all 10 animals in one drive", base.createdBatches[0].EstimatedTargets)
	}
}

// TestLimitParkSelectionReservesCapacityForLastSafeRows is the VAXCAP-006 park guard: at cap 1,
// a movable row listed FIRST must not consume the only cell a last-safe row needs. The last-safe
// row is admitted, the movable row is parked for a later date, and the cap is never exceeded.
func TestLimitParkSelectionReservesCapacityForLastSafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	lastSafe := planned
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-movable", TargetID: "goat-1", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-last-safe", TargetID: "goat-2", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &lastSafe},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-movable", "obl-last-safe"}, planned, planner, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-last-safe" {
		t.Fatalf("admitted = %#v, want only obl-last-safe (movable row must yield its cell)", out)
	}
}

// TestLimitParkSelectionAllLastSafeExceedsCap is the VAXCAP-006 legitimate-overflow guard: when
// every selected row is on its last safe day, all are admitted even beyond the cap.
func TestLimitParkSelectionAllLastSafeExceedsCap(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", TargetID: "goat-1", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-2", TargetID: "goat-2", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-3", TargetID: "goat-3", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-1", "obl-2", "obl-3"}, planned, planner, NewSweepSession())
	if len(out) != 3 {
		t.Fatalf("admitted = %#v, want all three last-safe rows despite cap 1 (legitimate overflow)", out)
	}
}

func TestLimitParkSelectionRejectsPastWindowRideAlongOverflow(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	tightEnd := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	looseEnd := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-blue-tongue", TargetID: "goat-1", RuleID: "rule-bt", ParkID: "park-1", DueAt: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC), WindowEnd: &tightEnd, BatchingHoldCount: 1},
		{ObligationID: "obl-loose", TargetID: "goat-2", RuleID: "rule-loose", ParkID: "park-1", DueAt: planned, WindowEnd: &looseEnd},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitParkSelectionByDriveAnimals(now, rows, []string{"obl-blue-tongue", "obl-loose"}, planned, planner, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-loose" {
		t.Fatalf("admitted = %#v, want only loose-window row; past-window blue_tongue must not ride 07-31 batch", out)
	}
}

func TestLimitParkSelectionCountsDistinctAnimals(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	movableEnd := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-ettt", TargetID: "goat-1", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-ppr", TargetID: "goat-1", RuleID: "rule-b", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
		{ObligationID: "obl-goat-2", TargetID: "goat-2", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &movableEnd},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 2

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-ettt", "obl-ppr", "obl-goat-2"}, planned, planner, NewSweepSession())
	if len(out) != 3 {
		t.Fatalf("admitted = %#v, want all obligations for two distinct animals within cap 2", out)
	}
}

func TestLimitParkSelectionPacksWholePhysicalShedsBeforeFillingCap(t *testing.T) {
	planned := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	movableEnd := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	var rows []domain.ParkConsolidationCandidate
	add := func(shed string, n int) {
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("%s-%03d", strings.NewReplacer(" ", "-", "-", "").Replace(strings.ToLower(shed)), i)
			rows = append(rows, domain.ParkConsolidationCandidate{
				ObligationID: "obl-" + id,
				TargetID:     "goat-" + id,
				RuleID:       "rule-ettt",
				ParkID:       "cpt",
				ShedName:     shed,
				DueAt:        planned,
				WindowEnd:    &movableEnd,
			})
		}
	}
	// Deliberately list Gandhi first to prove cap admission is not raw scan-order bin packing.
	add("Gandhi 1", 115)
	add("Godel 1 - Part 1", 120)
	add("Godel 2 - Part 4", 32)
	add("Mandela 2 - Part 8", 47)
	add("Old Yashoda 1", 10)
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		selected = append(selected, row.ObligationID)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 200

	out := limitParkSelectionByDriveAnimals(planned, rows, selected, planned, planner, NewSweepSession())
	gotRows := filterRows(rows, out)
	if got := uniqueParkTargetCount(gotRows); got != 199 {
		t.Fatalf("admitted animals = %d, want 199", got)
	}
	gotByShed := map[string]int{}
	for _, row := range gotRows {
		physical, _ := normalizeAssignmentShed(row.ShedName)
		gotByShed[physical]++
	}
	want := map[string]int{"Godel 1": 120, "Godel 2": 32, "Mandela 2": 47}
	if !reflect.DeepEqual(gotByShed, want) {
		t.Fatalf("admitted shed rollup = %#v, want %#v", gotByShed, want)
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
