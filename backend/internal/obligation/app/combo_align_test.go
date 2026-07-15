package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestPickComboAlignDateWithinWindow(t *testing.T) {
	d1 := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	batches := []domain.ComboDriveBatch{
		{BatchID: "b1", PlannedDate: &d1},
		{BatchID: "b2", PlannedDate: &d2},
	}
	got := pickComboAlignDate(batches, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 7*24*time.Hour)
	want := businessDate(d2)
	if got == nil || !got.Equal(want) {
		t.Fatalf("aligned=%v want %s", got, want)
	}
}

func TestPickComboAlignDateRejectsWideSpread(t *testing.T) {
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	batches := []domain.ComboDriveBatch{
		{BatchID: "b1", PlannedDate: &d1},
		{BatchID: "b2", PlannedDate: &d2},
	}
	if got := pickComboAlignDate(batches, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 7*24*time.Hour); got != nil {
		t.Fatalf("expected nil align when spread > window, got %v", got)
	}
}

// fakeComboAlignRepo layers ComboDriveAligner on top of fakeSweepRepo so AlignComboDrives can be
// exercised end-to-end (SweeperService.repo must satisfy both ports.Repository and
// ComboDriveAligner).
type fakeComboAlignRepo struct {
	*fakeSweepRepo
	comboBatches []domain.ComboDriveBatch
	updates      []comboPlannedDateUpdate
}

type comboPlannedDateUpdate struct {
	BatchID     string
	PlannedDate time.Time
}

func (f *fakeComboAlignRepo) ListPlannedComboBatches(context.Context, string, time.Time, int32) ([]domain.ComboDriveBatch, error) {
	return f.comboBatches, nil
}

// ListPlannedComboBatchesKeyset implements keyset pagination for combo batches (BUG #7 fix).
// For tests, simply return all batches if after == nil, or empty if after != nil (simulating next page).
func (f *fakeComboAlignRepo) ListPlannedComboBatchesKeyset(_ context.Context, _ string, _ time.Time, after *domain.ComboBatchCursor, _ int32) ([]domain.ComboDriveBatch, error) {
	if after == nil {
		return f.comboBatches, nil
	}
	// Cursor provided means "give me the next page", which for tests is empty (all batches fit in one page).
	return []domain.ComboDriveBatch{}, nil
}

func (f *fakeComboAlignRepo) UpdateBatchPlannedDate(_ context.Context, _, batchID string, plannedDate time.Time) error {
	f.updates = append(f.updates, comboPlannedDateUpdate{BatchID: batchID, PlannedDate: plannedDate})
	return nil
}

// TestAlignComboDrivesSkipsBatchThatWouldExceedShotCap covers BUG2 requirement 6:
// AlignComboDrives must not push an animal past MaxShotsPerAnimalPerDrive when it co-locates
// approved combo batches onto one shared date. goat-1's cap is already exhausted on the target
// alignment date (simulating shots claimed by earlier per-version sweeps against the same
// shared session), so its batch must be left on its own safe date, while goat-3's unrelated
// batch -- not at cap -- aligns normally onto the same target date.
func TestAlignComboDrivesSkipsBatchThatWouldExceedShotCap(t *testing.T) {
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)  // batchA: needs to move to target
	d2 := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC) // batchB: already on target, no move
	d3 := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)  // batchC: needs to move to target
	repo := &fakeComboAlignRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		comboBatches: []domain.ComboDriveBatch{
			{BatchID: "batch-a", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d1, TargetIDs: []string{"goat-1"}},
			{BatchID: "batch-b", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d2, TargetIDs: []string{"goat-2"}},
			{BatchID: "batch-c", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d3, TargetIDs: []string{"goat-3"}},
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	target := businessDate(d2)
	session := NewSweepSession()
	session.visitShotCounts[visitShotCountKey(target, "goat-1")] = 2 // already at cap on the target date

	aligned, err := svc.AlignComboDrives(context.Background(), "tenant-1", 10, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 2, session)
	if err != nil {
		t.Fatalf("AlignComboDrives: %v", err)
	}
	if aligned != 1 {
		t.Fatalf("aligned = %d, want 1 (only batch-c)", aligned)
	}
	if len(repo.updates) != 1 || repo.updates[0].BatchID != "batch-c" {
		t.Fatalf("updates = %#v, want only batch-c moved", repo.updates)
	}
	if !businessDate(repo.updates[0].PlannedDate).Equal(target) {
		t.Fatalf("batch-c planned date = %s, want %s", repo.updates[0].PlannedDate, target)
	}
}

// TestAlignComboDrivesAlignsWithinShotCap is the sanity-check sibling: when no member animal
// would be pushed past the cap, normal cross-version combo alignment still happens.
func TestAlignComboDrivesAlignsWithinShotCap(t *testing.T) {
	d1 := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	repo := &fakeComboAlignRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		comboBatches: []domain.ComboDriveBatch{
			{BatchID: "batch-a", ScopeType: "shed", ScopeID: "shed-1", Session: "combo:FMD+HS", PlannedDate: &d1, TargetIDs: []string{"goat-1"}},
			{BatchID: "batch-b", ScopeType: "shed", ScopeID: "shed-1", Session: "combo:FMD+HS", PlannedDate: &d2, TargetIDs: []string{"goat-2"}},
		},
	}
	svc := NewSweeperService(repo, nil, nil)

	aligned, err := svc.AlignComboDrives(context.Background(), "tenant-1", 7, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 2, NewSweepSession())
	if err != nil {
		t.Fatalf("AlignComboDrives: %v", err)
	}
	if aligned != 1 {
		t.Fatalf("aligned = %d, want 1 (batch-a moves onto batch-b's date)", aligned)
	}
	if len(repo.updates) != 1 || repo.updates[0].BatchID != "batch-a" {
		t.Fatalf("updates = %#v, want batch-a moved", repo.updates)
	}
}
