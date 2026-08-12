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

func TestParkConsolidationOverrideWindowKeepsPPRBlueTongueComboTogether(t *testing.T) {
	due := businessDate(time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
	windowEnd := due.AddDate(0, 0, 1)
	cfg := SweepConfig{
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-ppr": {VaccineCode: "PPR", VaccinePriority: 2},
			"rule-bt":  {VaccineCode: "BLUE_TONGUE", VaccinePriority: 4},
		},
	}
	ppr := domain.ParkConsolidationCandidate{
		ParkID:       "park-cpt",
		RuleID:       "rule-ppr",
		ObligationID: "obl-ppr",
		TargetID:     "goat-1",
		DueAt:        due,
		WindowStart:  &due,
		WindowEnd:    &windowEnd,
	}
	bt := ppr
	bt.RuleID = "rule-bt"
	bt.ObligationID = "obl-bt"

	if got, want := parkConsolidationGroupKey(cfg, ppr), "park-cpt|combo:PPR+Blue Tongue"; got != want {
		t.Fatalf("PPR group key = %q, want %q", got, want)
	}
	if got, want := parkConsolidationGroupKey(cfg, bt), "park-cpt|combo:PPR+Blue Tongue"; got != want {
		t.Fatalf("Blue Tongue group key = %q, want %q", got, want)
	}
}

func TestParkConsolidationOverrideWindowKeepsSheepPoxBlueTongueComboTogether(t *testing.T) {
	due := businessDate(time.Date(2027, 7, 24, 12, 0, 0, 0, time.UTC))
	windowEnd := due.AddDate(0, 0, 1)
	cfg := SweepConfig{RuleVaccineIDs: map[string]RuleVaccineIdentity{
		"rule-sp": {VaccineCode: "SHEEP_POX", VaccinePriority: 3},
		"rule-bt": {VaccineCode: "BLUE_TONGUE", VaccinePriority: 4},
	}}
	sp := domain.ParkConsolidationCandidate{ParkID: "park-cpt", RuleID: "rule-sp", ObligationID: "obl-sp", TargetID: "sheep-1", DueAt: due, WindowStart: &due, WindowEnd: &windowEnd}
	bt := sp
	bt.RuleID = "rule-bt"
	bt.ObligationID = "obl-bt"
	want := "park-cpt|combo:Sheep Pox+Blue Tongue"
	if got := parkConsolidationGroupKey(cfg, sp); got != want {
		t.Fatalf("Sheep Pox group key = %q, want %q", got, want)
	}
	if got := parkConsolidationGroupKey(cfg, bt); got != want {
		t.Fatalf("Blue Tongue group key = %q, want %q", got, want)
	}
}

func TestParkConsolidationBlueTongueRetainsPPRComboWhenPPRIsPublished(t *testing.T) {
	due := businessDate(time.Date(2027, 7, 24, 12, 0, 0, 0, time.UTC))
	windowEnd := due.AddDate(0, 0, 1)
	cfg := SweepConfig{RuleVaccineIDs: map[string]RuleVaccineIdentity{
		"rule-ppr": {VaccineCode: "PPR", VaccinePriority: 2},
		"rule-sp":  {VaccineCode: "SHEEP_POX", VaccinePriority: 3},
		"rule-bt":  {VaccineCode: "BLUE_TONGUE", VaccinePriority: 4},
	}}
	bt := domain.ParkConsolidationCandidate{
		ParkID: "park-cpt", RuleID: "rule-bt", ObligationID: "obl-bt", TargetID: "sheep-1",
		DueAt: due, WindowStart: &due, WindowEnd: &windowEnd,
	}
	if got, want := parkConsolidationGroupKey(cfg, bt), "park-cpt|combo:PPR+Blue Tongue"; got != want {
		t.Fatalf("Blue Tongue group key = %q, want %q when PPR remains published", got, want)
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

func TestConsolidateParkDrivesMergesSpeciesForSameParkVaccineSession(t *testing.T) {
	due := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{ObligationID: "obl-goat", TargetID: "goat-1", RuleID: "rule-ettt", ShedID: "shed-1", ShedName: "Godel 1 - Part 1", ParkID: "park-1", TargetSpecies: "goat", TargetAnimalStage: "adult", DueAt: due, WindowEnd: ptrTime(due.AddDate(0, 0, 7))},
			{ObligationID: "obl-sheep", TargetID: "sheep-1", RuleID: "rule-ettt", ShedID: "shed-2", ShedName: "Godel 2 - Part 4", ParkID: "park-1", TargetSpecies: "sheep", TargetAnimalStage: "adult", DueAt: due, WindowEnd: ptrTime(due.AddDate(0, 0, 7))},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-ettt": {VaccineCode: "ET_TT", VaccinePriority: 1},
		},
		DrivePlanner:      domain.DrivePlannerSettings{Enabled: true, MaxGoatsPerDrive: 200, SpeciesGroupingPolicy: "species_specific"},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, due)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one same-vaccine park batch across adult species", res)
	}
	if len(repo.createdBatches) != 1 || repo.createdBatches[0].EstimatedTargets != 2 {
		t.Fatalf("created batches = %#v, want one two-animal batch", repo.createdBatches)
	}
}

func TestOrderParkConsolidationGroupsUsesEffectiveLatestSafeBeforeDiscoveryOrder(t *testing.T) {
	etStart := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	etEnd := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	btStart := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	btEnd := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	cfg := SweepConfig{
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-ettt": {VaccineCode: "ET_TT", VaccinePriority: 1},
			"rule-bt":   {VaccineCode: "Blue Tongue", VaccinePriority: 4},
		},
	}
	groups := map[string][]domain.ParkConsolidationCandidate{
		"cpt|rule:rule-bt":   {{ObligationID: "obl-bt", RuleID: "rule-bt", ParkID: "cpt", DueAt: btStart, WindowEnd: &btEnd}},
		"cpt|rule:rule-ettt": {{ObligationID: "obl-ettt", RuleID: "rule-ettt", ParkID: "cpt", DueAt: etStart, WindowEnd: &etEnd}},
	}

	got := orderParkConsolidationGroups([]string{"cpt|rule:rule-bt", "cpt|rule:rule-ettt"}, groups, cfg)
	want := []string{"cpt|rule:rule-ettt", "cpt|rule:rule-bt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group order = %#v, want %#v", got, want)
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

func TestConsolidateParkDrivesKeepsLastSafeDayUnderCap(t *testing.T) {
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
	if res.ParkBatches != 1 || res.ParkObligations != 2 {
		t.Fatalf("result = %#v, want one capped park batch on last safe day", res)
	}
	if len(repo.createdBatches) != 1 {
		t.Fatalf("created batches = %d, want 1", len(repo.createdBatches))
	}
	if repo.createdBatches[0].EstimatedTargets != 2 {
		t.Fatalf("estimated targets = %d, want 2 animals inside cap 2", repo.createdBatches[0].EstimatedTargets)
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

func TestConsolidateParkDrivesMergesApprovedComboVaccinesAcrossSheds(t *testing.T) {
	repo := &fakeSweepRepo{
		parkRows: []domain.ParkConsolidationCandidate{
			{
				ObligationID: "obl-fmd",
				TargetID:     "goat-fmd",
				RuleID:       "rule-fmd",
				ShedID:       "shed-1",
				ParkID:       "park-1",
				DueAt:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WindowEnd:    ptrTime(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)),
			},
			{
				ObligationID: "obl-hs",
				TargetID:     "goat-hs",
				RuleID:       "rule-hs",
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
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-fmd": {VaccineCode: "FMD", VaccinePriority: 5},
			"rule-hs":  {VaccineCode: "HS", VaccinePriority: 6},
		},
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

func TestConsolidateParkDrivesAppliesVaccineDateOverride(t *testing.T) {
	original := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	postponed := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-ppr-1", TargetID: "goat-1", RuleID: "rule-ppr", ShedID: "shed-1", ShedName: "Godel 1 - Part 1", ParkID: "park-1", DueAt: original, WindowEnd: ptrTime(original.AddDate(0, 0, 7))},
		{ObligationID: "obl-ppr-2", TargetID: "goat-2", RuleID: "rule-ppr", ShedID: "shed-1", ShedName: "Godel 1 - Part 1", ParkID: "park-1", DueAt: original, WindowEnd: ptrTime(original.AddDate(0, 0, 7))},
	}
	repo := &fakeSweepRepo{
		parkRows:  rows,
		attachAll: true,
		overrides: map[string]domain.VaccineDriveDateOverride{
			"tenant-1|park-1|ppr|" + businessDate(original).Format("2006-01-02"): {
				TenantID:          "tenant-1",
				ParkID:            "park-1",
				VaccineCode:       "PPR",
				OriginalDriveDate: original,
				OverrideDate:      postponed,
				Reason:            "CPT validation override",
			},
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	res, err := svc.consolidateParkDrives(context.Background(), "tenant-1", "version-1", SweepConfig{
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-ppr": {VaccineCode: "PPR", VaccinePriority: 2},
		},
		DrivePlanner: domain.DrivePlannerSettings{
			Enabled:                   true,
			MaxGoatsPerDrive:          200,
			MaxShotsPerAnimalPerDrive: 2,
			MaxBatchingHoldDays:       7,
			MaxBatchingHoldCount:      1,
		},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
	}, original)
	if err != nil {
		t.Fatalf("consolidateParkDrives: %v", err)
	}
	if res.ParkBatches != 1 || len(repo.createdBatches) != 1 || repo.createdBatches[0].PlannedDate == nil {
		t.Fatalf("result = %#v batches=%#v, want one overridden park batch", res, repo.createdBatches)
	}
	if got := dateKey(repo.createdBatches[0].PlannedDate); got != "2026-08-07" {
		t.Fatalf("planned date = %s, want override date 2026-08-07", got)
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

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-movable", "obl-last-safe"}, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-last-safe" {
		t.Fatalf("admitted = %#v, want only obl-last-safe (movable row must yield its cell)", out)
	}
}

// TestLimitParkSelectionAllLastSafeStillRespectsCap is the VAXCAP-006 hard-cap guard: even when
// every selected row is on its last safe day, the operator-day cap is not exceeded.
func TestLimitParkSelectionAllLastSafeStillRespectsCap(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	rows := []domain.ParkConsolidationCandidate{
		{ObligationID: "obl-1", TargetID: "goat-1", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-2", TargetID: "goat-2", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
		{ObligationID: "obl-3", TargetID: "goat-3", RuleID: "rule-a", ParkID: "park-1", DueAt: planned, WindowEnd: &planned},
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 1

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-1", "obl-2", "obl-3"}, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	if len(out) != 1 || out[0] != "obl-1" {
		t.Fatalf("admitted = %#v, want only the first last-safe row inside cap 1", out)
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

	out := limitParkSelectionByDriveAnimals(now, rows, []string{"obl-blue-tongue", "obl-loose"}, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
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

	out := limitParkSelectionByDriveAnimals(planned, rows, []string{"obl-ettt", "obl-ppr", "obl-goat-2"}, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
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
			ruleID := "rule-history-repeat"
			holdCount := int32(1)
			if i%2 == 0 {
				ruleID = "rule-blank-history-catchup"
				holdCount = 0
			}
			rows = append(rows, domain.ParkConsolidationCandidate{
				ObligationID:      "obl-" + id,
				TargetID:          "goat-" + id,
				RuleID:            ruleID,
				ParkID:            "cpt",
				ShedName:          shed,
				DueAt:             planned,
				WindowEnd:         &movableEnd,
				BatchingHoldCount: holdCount,
			})
		}
	}
	// CPT's canonical route and exact staging-clone shed totals. Alternating rule IDs prove that
	// history-repeat and blank-history catch-up rows cannot split one physical shed.
	add("Gandhi 1", 114)
	add("Godel 1 - Part 1", 120)
	add("Godel 2 - Part 4", 32)
	add("Mandela 2 - Part 8", 47)
	add("Old Yashoda 1", 11)
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		selected = append(selected, row.ObligationID)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 200

	out := limitParkSelectionByDriveAnimals(planned, rows, selected, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	gotRows := filterRows(rows, out)
	if got := uniqueParkTargetCount(gotRows); got != 193 {
		t.Fatalf("admitted animals = %d, want 193", got)
	}
	gotByShed := map[string]int{}
	for _, row := range gotRows {
		physical, _ := normalizeAssignmentShed(row.ShedName)
		gotByShed[physical]++
	}
	want := map[string]int{"Gandhi 1": 114, "Godel 2 - Part 4": 32, "Mandela 2 - Part 8": 47}
	if !reflect.DeepEqual(gotByShed, want) {
		t.Fatalf("admitted shed rollup = %#v, want %#v", gotByShed, want)
	}
}

func TestLimitParkSelectionFallsBackToWholePartitionsWhenShedExceedsRemainingCapacity(t *testing.T) {
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
	// Gandhi leaves 170 slots. Godel 1 is itself larger than the configured 200-animal cap,
	// so whole-partition fallback is allowed: Parts 1 and 2 fit, while Part 3 carries.
	add("Gandhi 1", 30)
	add("Godel 1 - Part 1", 100)
	add("Godel 1 - Part 2", 70)
	add("Godel 1 - Part 3", 50)
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		selected = append(selected, row.ObligationID)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 200

	out := limitParkSelectionByDriveAnimals(planned, rows, selected, planned, planner, planner.MaxGoatsPerDrive, NewSweepSession())
	gotRows := filterRows(rows, out)
	if got := uniqueParkTargetCount(gotRows); got != 200 {
		t.Fatalf("admitted animals = %d, want 200", got)
	}
	gotByShed := map[string]int{}
	for _, row := range gotRows {
		gotByShed[row.ShedName]++
	}
	want := map[string]int{
		"Gandhi 1":         30,
		"Godel 1 - Part 1": 100,
		"Godel 1 - Part 2": 70,
	}
	if !reflect.DeepEqual(gotByShed, want) {
		t.Fatalf("admitted shed partitions = %#v, want %#v", gotByShed, want)
	}
}

func TestLimitParkSelectionLatestSafeKeepsWholePartitionPastResidualCapacity(t *testing.T) {
	planned := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	var rows []domain.ParkConsolidationCandidate
	add := func(shed string, n int) {
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("%s-%03d", strings.NewReplacer(" ", "-", "-", "").Replace(strings.ToLower(shed)), i)
			rows = append(rows, domain.ParkConsolidationCandidate{
				ObligationID: "obl-" + id,
				TargetID:     "goat-" + id,
				RuleID:       "rule-pox",
				ParkID:       "cpt",
				ShedName:     shed,
				DueAt:        planned.AddDate(0, 0, -7),
				WindowEnd:    &planned,
			})
		}
	}
	add("Godel 1 - Part 1", 3)
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		selected = append(selected, row.ObligationID)
	}
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxGoatsPerDrive = 200
	session := NewSweepSession()
	session.claimDriveCapacity("cpt", planned, "existing-199", 199)

	out := limitParkSelectionByDriveAnimals(planned, rows, selected, planned, planner, planner.MaxGoatsPerDrive, session)
	if len(out) != 0 {
		t.Fatalf("admitted = %#v, want none; latest-safe residual capacity must not split a normal partition", out)
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
