package app

import (
	"context"
	"sort"
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

// ListPlannedComboBatchesKeyset implements GENUINE keyset pagination for combo batches over
// f.comboBatches, ordered by (scope_type, scope_id, session, planned_date, batch_id) exactly like
// the production query. It honors both the after cursor AND the limit, so a test can set a small
// svc.page and drive a real multi-page walk in which a single combo group STRADDLES page
// boundaries -- the case AlignComboDrives' cross-page group carry-over must handle. With the
// default large svc.page every batch still comes back in the first page, so the existing
// single-page tests are unaffected.
func (f *fakeComboAlignRepo) ListPlannedComboBatchesKeyset(_ context.Context, _ string, _ time.Time, after *domain.ComboBatchCursor, limit int32) ([]domain.ComboDriveBatch, error) {
	ordered := append([]domain.ComboDriveBatch(nil), f.comboBatches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return comboBatchLess(ordered[i], ordered[j])
	})
	start := 0
	for start < len(ordered) && !comboBatchAfterCursor(ordered[start], after) {
		start++
	}
	if start >= len(ordered) {
		return []domain.ComboDriveBatch{}, nil
	}
	end := len(ordered)
	if limit > 0 && start+int(limit) < end {
		end = start + int(limit)
	}
	return ordered[start:end], nil
}

// comboBatchLess orders combo batches by (scope_type, scope_id, session, planned_date, batch_id).
func comboBatchLess(a, b domain.ComboDriveBatch) bool {
	if a.ScopeType != b.ScopeType {
		return a.ScopeType < b.ScopeType
	}
	if a.ScopeID != b.ScopeID {
		return a.ScopeID < b.ScopeID
	}
	if a.Session != b.Session {
		return a.Session < b.Session
	}
	ad, bd := comboBatchDate(a.PlannedDate), comboBatchDate(b.PlannedDate)
	if !ad.Equal(bd) {
		return ad.Before(bd)
	}
	return a.BatchID < b.BatchID
}

// comboBatchAfterCursor reports whether batch b sorts strictly AFTER cursor c (nil = before all).
func comboBatchAfterCursor(b domain.ComboDriveBatch, c *domain.ComboBatchCursor) bool {
	if c == nil {
		return true
	}
	if b.ScopeType != c.ScopeType {
		return b.ScopeType > c.ScopeType
	}
	if b.ScopeID != c.ScopeID {
		return b.ScopeID > c.ScopeID
	}
	if b.Session != c.Session {
		return b.Session > c.Session
	}
	bd, cd := comboBatchDate(b.PlannedDate), comboBatchDate(c.PlannedDate)
	if !bd.Equal(cd) {
		return bd.After(cd)
	}
	return b.BatchID > c.BatchID
}

func comboBatchDate(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
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

// TestAlignComboDrivesAlignsGroupStraddlingPages is the FINDING 2 (P1) regression: a single combo
// group whose batches span MORE than one keyset page must still be aligned onto ONE shared target
// date, not fragmented into a separate per-page target. Here five batches for the SAME
// (scope_type, scope_id, combo session) sit on Aug 1..Aug 5, and svc.page is forced to 2 so the
// group straddles three pages. The correct behavior aligns all four movable batches onto Aug 5
// (the group's single max date, within the 7-day window).
//
// FAILING-FIRST: on origin/main AlignComboDrives rebuilt groups PER PAGE and processed them there,
// so page 1 (batch-1,batch-2) picked its own local target (Aug 2), page 2 (batch-3,batch-4) picked
// Aug 4, and page 3 (batch-5 alone) had <2 batches and was skipped -- producing multiple different
// planned dates and never a single shared Aug 5. This fix carries the tail group across pages and
// only flushes it once a different group key or EOF is seen.
func TestAlignComboDrivesAlignsGroupStraddlingPages(t *testing.T) {
	d1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	d4 := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	d5 := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	repo := &fakeComboAlignRepo{
		fakeSweepRepo: &fakeSweepRepo{},
		comboBatches: []domain.ComboDriveBatch{
			{BatchID: "batch-1", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d1, TargetIDs: []string{"goat-1"}},
			{BatchID: "batch-2", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d2, TargetIDs: []string{"goat-2"}},
			{BatchID: "batch-3", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d3, TargetIDs: []string{"goat-3"}},
			{BatchID: "batch-4", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d4, TargetIDs: []string{"goat-4"}},
			{BatchID: "batch-5", ScopeType: "park", ScopeID: "park-1", Session: "combo:FMD+HS", PlannedDate: &d5, TargetIDs: []string{"goat-5"}},
		},
	}
	svc := NewSweeperService(repo, nil, nil)
	svc.page = 2 // force the single group to straddle three keyset pages

	// window = 7 days, so Aug1..Aug5 spread (4 days) is within window; target is Aug 5 (max date).
	aligned, err := svc.AlignComboDrives(context.Background(), "tenant-1", 7, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 0, NewSweepSession())
	if err != nil {
		t.Fatalf("AlignComboDrives: %v", err)
	}
	target := businessDate(d5)
	// batch-5 already sits on Aug 5, so 4 batches move onto it.
	if aligned != 4 {
		t.Fatalf("aligned = %d, want 4 (all movable batches onto the single group target)", aligned)
	}
	if len(repo.updates) != 4 {
		t.Fatalf("updates = %#v, want 4 moves", repo.updates)
	}
	moved := make(map[string]bool)
	for _, u := range repo.updates {
		if !businessDate(u.PlannedDate).Equal(target) {
			t.Fatalf("batch %s aligned to %s, want single shared target %s (group must not fragment per page)", u.BatchID, u.PlannedDate, target)
		}
		moved[u.BatchID] = true
	}
	for _, id := range []string{"batch-1", "batch-2", "batch-3", "batch-4"} {
		if !moved[id] {
			t.Fatalf("%s was not aligned; updates=%#v", id, repo.updates)
		}
	}
}
