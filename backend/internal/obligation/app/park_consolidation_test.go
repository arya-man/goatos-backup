package app

import (
	"context"
	"errors"
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

func TestConsolidateParkDrivesPagesParkCandidatesWithCursor(t *testing.T) {
	rows := []domain.ParkConsolidationCandidate{
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
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-3",
			RuleID:       "rule-a",
			ShedID:       "shed-3",
			ParkID:       "park-1",
			DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
		},
		{
			ObligationID: "obl-4",
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

// TestParkConsolidationRaisesTieForSamePriorityDifferentVaccines is the FINDING 4 (P1) regression:
// when a park-consolidation merge co-locates obligations for one animal from MULTIPLE rules that
// resolve to DIFFERENT vaccines at EQUAL priority, and the drive would exceed
// MaxShotsPerAnimalPerDrive, the sweeper must raise *ShotCapPriorityTieError instead of silently
// dropping the overflow -- exactly as the main shed-batching path already does. Here goat-1 is due
// three distinct same-priority vaccines (rule-a/rule-b/rule-c) in three sheds of one park, and the
// cap is 2. The 3rd same-target obligation must tie. This is asserted in BOTH the real park merge
// path (SweepVersion) AND the write-free preflight park replay (PreflightVisitShotCapTies).
//
// FAILING-FIRST: on origin/main selectParkIDsWithinVisitShotCapForSession credited EVERY selected
// park row with the single VERSION-level vaccine code/priority, so the three different-vaccine rows
// all looked like the SAME vaccine to rejectOrTie -- which then treated the 3rd as an ordinary
// same-vaccine overflow (returns nil) and silently swallowed the tie. This fix resolves vaccine
// identity per row via cfg.getRuleVaccineIdentity(row.RuleID), so the genuine cross-vaccine tie is
// surfaced.
func TestParkConsolidationRaisesTieForSamePriorityDifferentVaccines(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	parkRows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-a", RuleID: "rule-a", ParkID: "park-1", ShedID: "shed-1", TargetID: "goat-1", TargetSpecies: "goat", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-b", RuleID: "rule-b", ParkID: "park-1", ShedID: "shed-2", TargetID: "goat-1", TargetSpecies: "goat", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-c", RuleID: "rule-c", ParkID: "park-1", ShedID: "shed-3", TargetID: "goat-1", TargetSpecies: "goat", DueAt: due, WindowEnd: &winEnd},
	}
	// Three rule IDs -> three distinct vaccine codes at EQUAL priority: a genuine unresolved tie.
	ruleIDs := map[string]RuleVaccineIdentity{
		"rule-a": {VaccineCode: "Vaccine A", VaccinePriority: 50},
		"rule-b": {VaccineCode: "Vaccine B", VaccinePriority: 50},
		"rule-c": {VaccineCode: "Vaccine C", VaccinePriority: 50},
	}
	park := domain.ParkConsolidationSettings{Enabled: true, MinShedDriveTargets: 5, MinParkMergeTargets: 1, MinParkMergeSheds: 1}
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	cfg := SweepConfig{VaccineCode: "Vaccine A", ParkConsolidation: park, DrivePlanner: planner, RuleVaccineIDs: ruleIDs}
	ctx := context.Background()

	// Part 1: the REAL park merge path (SweepVersion) must return the tie.
	t.Run("real_park_merge", func(t *testing.T) {
		repo := &fakeSweepRepo{
			parkRows:  append([]domain.ParkConsolidationCandidate(nil), parkRows...),
			attachAll: true,
		}
		svc := NewSweeperService(repo, nil, nil)
		_, err := svc.SweepVersion(ctx, "tenant-1", "v-1", cfg, due)
		var tieErr *ShotCapPriorityTieError
		if err == nil || !errors.As(err, &tieErr) {
			t.Fatalf("SweepVersion err = %v, want *ShotCapPriorityTieError", err)
		}
		if tieErr.TargetID != "goat-1" {
			t.Fatalf("tie target = %q, want goat-1", tieErr.TargetID)
		}
		if repo.createBatchCalls != 0 {
			t.Fatalf("createBatchCalls = %d, want 0 (merge must abort on the tie, not batch a partial drive)", repo.createBatchCalls)
		}
	})

	// Part 2: the write-free preflight park replay must ALSO surface the same tie, write-free.
	t.Run("preflight_park_replay", func(t *testing.T) {
		repo := &fakeSweepRepo{
			rowsByVersion: map[string][]domain.UnbatchedDue{"v-1": nil, "v-2": nil},
			parkRows:      append([]domain.ParkConsolidationCandidate(nil), parkRows...),
			attachAll:     true,
		}
		tasks := &fakeSweepTaskCreator{id: "task-1"}
		reserver := &fakeSweepStockReserver{}
		svc := NewSweeperService(repo, tasks, reserver)
		// PreflightVisitShotCapTies needs >= 2 plans; the tie is detected on the first plan's park
		// replay before the second is reached (parkRows is shared across versions in the fake).
		plans := []SweepVersionPriority{
			{VersionID: "v-1", Config: cfg},
			{VersionID: "v-2", Config: cfg},
		}
		err := svc.PreflightVisitShotCapTies(ctx, "tenant-1", plans, due)
		var tieErr *ShotCapPriorityTieError
		if err == nil || !errors.As(err, &tieErr) {
			t.Fatalf("PreflightVisitShotCapTies err = %v, want *ShotCapPriorityTieError", err)
		}
		if tieErr.TargetID != "goat-1" {
			t.Fatalf("tie target = %q, want goat-1", tieErr.TargetID)
		}
		if repo.createBatchCalls != 0 || tasks.calls != 0 || reserver.calls != 0 {
			t.Fatalf("preflight side effects: batches=%d tasks=%d stock=%d, want 0/0/0", repo.createBatchCalls, tasks.calls, reserver.calls)
		}
	})
}
