package app

import (
	"context"
	"errors"
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
		},
		createBatchID:       "batch-1",
		createBatchAttached: 1,
	}
	reserver := &fakeSweepStockReserver{err: errors.New("inventory: stock unavailable")}
	svc := NewSweeperService(repo, nil, reserver)

	_, err := svc.SweepVersion(context.Background(), "tenant-1", "version-1", SweepConfig{
		VaccineItemID: "vaccine-1",
		DosesPerGoat:  1,
	}, time.Now())
	if err == nil {
		t.Fatal("SweepVersion expected stock error")
	}
	if repo.stockBlockCalls != 1 {
		t.Fatalf("stock block calls = %d, want 1", repo.stockBlockCalls)
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

type fakeSweepRepo struct {
	rows                []domain.UnbatchedDue
	createBatchID       string
	createBatchAttached int64
	createBatchCalls    int
	setTaskCalls        int
	stockBlockCalls     int
	lastTaskID          string
}

func (f *fakeSweepRepo) Ping(context.Context) error { return nil }

func (f *fakeSweepRepo) InsertObligation(context.Context, domain.NewObligation) (string, bool, error) {
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

func (f *fakeSweepRepo) CreateBatchWithObligations(_ context.Context, _ domain.NewBatch, _ []string) (string, int64, error) {
	f.createBatchCalls++
	return f.createBatchID, f.createBatchAttached, nil
}

func (f *fakeSweepRepo) SetBatchSOPTask(_ context.Context, _, _, taskID string) error {
	f.setTaskCalls++
	f.lastTaskID = taskID
	return nil
}

func (f *fakeSweepRepo) MarkBatchStockBlocked(context.Context, string, string, string, int64, string) error {
	f.stockBlockCalls++
	return nil
}

func (f *fakeSweepRepo) ListUnbatchedDueForVersion(context.Context, string, string, time.Time, int32) ([]domain.UnbatchedDue, error) {
	return f.rows, nil
}

func (f *fakeSweepRepo) AttachObligationsToBatch(context.Context, string, string, []string) (int64, error) {
	return 0, nil
}

func (f *fakeSweepRepo) MarkCompleted(context.Context, string, string) (bool, error) {
	return false, nil
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
	id    string
	calls int
}

func (f *fakeSweepTaskCreator) CreateTaskForBatch(context.Context, string, string, string, string, string, string) (string, error) {
	f.calls++
	if f.id == "" {
		f.id = "task-1"
	}
	return f.id, nil
}

type fakeSweepStockReserver struct {
	calls       int
	lastBatchID string
	lastQty     int64
	err         error
}

func (f *fakeSweepStockReserver) ReserveForBatch(_ context.Context, _, batchID, _, _ string, qty int64) error {
	f.calls++
	f.lastBatchID = batchID
	f.lastQty = qty
	return f.err
}
