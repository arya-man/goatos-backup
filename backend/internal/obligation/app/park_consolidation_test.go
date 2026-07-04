package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestPickBestParkDriveDateMaximizesFeasibleGoats(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{
			ObligationID: "obl-1",
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			ShedID:       "shed-c",
			DueAt:        time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)),
		},
	}

	planned, ids := pickBestParkDriveDate(now, rows)
	if planned == nil {
		t.Fatal("planned date is nil")
	}
	if got := planned.UTC().Format("2006-01-02"); got != "2026-07-05" {
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
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-ok",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
	}

	_, ids := pickBestParkDriveDate(now, rows)
	if len(ids) != 1 || ids[0] != "obl-ok" {
		t.Fatalf("selected ids = %#v, want only obl-ok", ids)
	}
}

func TestConsolidateParkDrivesRequiresMultipleSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-1",
				RuleID:       "rule-a",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-2",
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
	if res.ParkBatches != 0 || repo.createBatchCalls != 0 {
		t.Fatalf("park batches = %d create calls = %d, want no single-shed consolidation", res.ParkBatches, repo.createBatchCalls)
	}
}

func TestConsolidateParkDrivesCreatesParkBatchAcrossSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-1",
				RuleID:       "rule-a",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-2",
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

func TestConsolidateParkDrivesMergesDifferentVaccinesAcrossSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-et",
				RuleID:       "rule-et",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-tt",
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
			ShedID:       "shed-a",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-2",
			ShedID:       "shed-b",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			ShedID:       "shed-c",
			DueAt:        time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-4",
			ShedID:       "shed-d",
			DueAt:        time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		},
	}
	firstDate, firstIDs := pickBestParkDriveDate(now, rows)
	if len(firstIDs) != 2 {
		t.Fatalf("first pass ids = %#v, want 2", firstIDs)
	}
	remaining := removeRows(rows, firstIDs)
	secondDate, secondIDs := pickBestParkDriveDate(now, remaining)
	if secondDate == nil || len(secondIDs) != 2 {
		t.Fatalf("second pass date=%v ids=%#v, want 2 goats on another date", secondDate, secondIDs)
	}
	if firstDate.Equal(*secondDate) {
		t.Fatalf("expected two different drive dates, both %s", firstDate.Format("2006-01-02"))
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
