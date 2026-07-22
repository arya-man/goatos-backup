package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestPreflightDetectsParkConsolidationTie is the RV-01 guard: a same-priority, cross-vaccine tie
// that only manifests when the main loop's DEFERRED small shed groups merge in the park-consolidation
// pass must be surfaced by the write-free preflight -- not slip past it and abort the real sweep
// mid-run after earlier versions already committed batches. The two competing obligations live only
// in the park-candidate set (empty main-loop rows), so the tie is reachable exclusively through the
// park replay this fix adds. The preflight must still touch ZERO write paths.
func TestPreflightDetectsParkConsolidationTie(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		// Both versions have no main-loop shed/tenant rows: the only work is park consolidation.
		rowsByVersion: map[string][]domain.UnbatchedDue{"v-a": nil, "v-b": nil},
		parkRows: []domain.ParkConsolidationCandidate{
			{ObligationID: "obl-a", RuleID: "rule-a", ParkID: "park-1", ShedID: "shed-1", TargetID: "goat-1", TargetSpecies: "goat", DueAt: due, WindowEnd: &winEnd},
			{ObligationID: "obl-b", RuleID: "rule-b", ParkID: "park-1", ShedID: "shed-2", TargetID: "goat-1", TargetSpecies: "goat", DueAt: due, WindowEnd: &winEnd},
		},
		attachAll: true,
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	park := domain.ParkConsolidationSettings{Enabled: true, MinShedDriveTargets: 5, MinParkMergeTargets: 1, MinParkMergeSheds: 1}
	// maxShots=1: park merge for v-a claims goat-1's single slot; v-a and v-b resolve to the same
	// unconfigured default priority, so v-b's park merge for the same animal/date ties.
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1}
	plans := []SweepVersionPriority{
		{VersionID: "v-a", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Alpha", ParkConsolidation: park, DrivePlanner: planner}},
		{VersionID: "v-b", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Beta", ParkConsolidation: park, DrivePlanner: planner}},
	}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due, time.Time{})
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError from park-consolidation replay", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie target = %q, want goat-1", tieErr.TargetID)
	}
	if repo.createBatchCalls != 0 {
		t.Fatalf("createBatchCalls = %d, want 0 (preflight must not write)", repo.createBatchCalls)
	}
	if tasks.calls != 0 || reserver.calls != 0 {
		t.Fatalf("task/stock side effects: tasks=%d stock=%d, want 0/0", tasks.calls, reserver.calls)
	}
}

// fakeVisitShotLockerRepo makes a fakeSweepRepo satisfy visitShotLocker so the RV-02 lock-and-refresh
// path is exercised without a real Postgres advisory lock. persisted is the "already committed" shot
// count per target; onLock lets a test simulate a concurrent worker committing a shot in between
// this session's groups (the exact window RV-02 closes).
type fakeVisitShotLockerRepo struct {
	*fakeSweepRepo
	persisted map[string]int32
	lockCalls int
	onLock    func(f *fakeVisitShotLockerRepo)
}

func (f *fakeVisitShotLockerRepo) CountVisitShotsForTargets(_ context.Context, _ string, targetIDs []string, _ time.Time) (map[string]int32, error) {
	out := make(map[string]int32, len(targetIDs))
	for _, t := range targetIDs {
		out[t] = f.persisted[t]
	}
	return out, nil
}

func (f *fakeVisitShotLockerRepo) LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, func(context.Context) error, error) {
	f.lockCalls++
	if f.onLock != nil {
		f.onLock(f)
	}
	counts, err := f.CountVisitShotsForTargets(ctx, tenantID, targetIDs, date)
	if err != nil {
		return nil, nil, err
	}
	return counts, func(context.Context) error { return nil }, nil
}

// TestLockAndRefreshReLocksAlreadyLoadedKey is the RV-02 guard: a second group touching a visit an
// earlier group already resolved must RE-ACQUIRE the per-visit lock and REFRESH the count from the
// authoritative persisted value -- not reuse the stale in-memory count with no lock. Before the fix,
// the loaded/unresolvedTargets short-circuit skipped the second lock entirely, so a concurrent worker
// could fill the remaining slot in between and this worker would then commit an over-cap shot. Here a
// concurrent worker commits a shot right before the second lock; the refreshed session count must
// reflect it (2), and the lock must have been taken twice.
func TestLockAndRefreshReLocksAlreadyLoadedKey(t *testing.T) {
	repo := &fakeVisitShotLockerRepo{
		fakeSweepRepo: &fakeSweepRepo{attachAll: true},
		persisted:     map[string]int32{},
	}
	svc := NewSweeperService(repo, nil, nil)
	session := NewSweepSession()
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	key := visitShotCountKey(date, "goat-1")
	ctx := context.Background()

	// Group 1: lock, read fresh (0), claim one slot, "commit" it to persisted, release.
	rel1, err := svc.lockAndRefreshVisitShots(ctx, "tenant-1", []string{"goat-1"}, &date, 2, session)
	if err != nil {
		t.Fatalf("group1 lock: %v", err)
	}
	if session.visitShotCounts[key] != 0 {
		t.Fatalf("group1 seeded count = %d, want 0", session.visitShotCounts[key])
	}
	session.claim(key, "vaccineA", 5)
	repo.persisted["goat-1"] = 1 // group 1's shot is now durably committed
	if err := rel1(ctx); err != nil {
		t.Fatalf("group1 release: %v", err)
	}

	// A concurrent worker fills the remaining slot right before group 2 acquires the lock.
	repo.onLock = func(f *fakeVisitShotLockerRepo) {
		if f.lockCalls == 2 {
			f.persisted["goat-1"] = 2
		}
	}

	// Group 2: SAME visit key (already loaded). Must re-lock and reset the count to fresh persisted.
	rel2, err := svc.lockAndRefreshVisitShots(ctx, "tenant-1", []string{"goat-1"}, &date, 2, session)
	if err != nil {
		t.Fatalf("group2 lock: %v", err)
	}
	defer func() { _ = rel2(ctx) }()

	if repo.lockCalls != 2 {
		t.Fatalf("lockCalls = %d, want 2 (group 2 must re-lock an already-loaded key)", repo.lockCalls)
	}
	if session.visitShotCounts[key] != 2 {
		t.Fatalf("refreshed count = %d, want 2 (must pick up the concurrent worker's committed shot, not the stale in-memory 1)", session.visitShotCounts[key])
	}
	// The visit is now at cap: a further claim attempt for a different vaccine is a genuine overflow.
	planner := domain.DefaultDrivePlannerSettings()
	planner.MaxShotsPerAnimalPerDrive = 2
	selected, _, err := selectIDsWithinVisitShotCapForSession(
		date,
		[]domain.UnbatchedDue{{ObligationID: "obl-x", TargetID: "goat-1"}},
		&date, planner, RuleVaccineIdentity{VaccineCode: "vaccineB", VaccinePriority: 6, VaccineType: "killed"}, session,
	)
	if err != nil {
		t.Fatalf("select at cap: %v", err)
	}
	if len(selected) != 0 {
		t.Fatalf("selected = %v, want none (visit already at cap after refresh)", selected)
	}
}
