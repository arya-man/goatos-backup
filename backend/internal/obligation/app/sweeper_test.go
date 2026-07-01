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
		createBatchID:       "batch-1",
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
	if repo.createBatchCalls != 0 {
		t.Fatalf("new batches created = %d, want 0", repo.createBatchCalls)
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

type fakeSweepRepo struct {
	rows                 []domain.UnbatchedDue
	parkRows             []domain.ParkConsolidationCandidate
	createBatchID        string
	createBatchIDs       []string
	createBatchAttached  int64
	attachAll            bool
	createBatchCalls     int
	setTaskCalls         int
	stockBlockCalls      int
	clearStockBlockCalls int
	lastTaskID           string
	missedPages          []int
	missedCalls          int
	createdBatches       []domain.NewBatch
	finalizationPages    [][]domain.PlannedBatchFinalization
	finalizationCalls    int
	createdFinalization  []domain.PlannedBatchFinalization
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

func (f *fakeSweepRepo) MarkBatchStockBlocked(context.Context, string, string, string, int64, string) error {
	f.stockBlockCalls++
	return nil
}

func (f *fakeSweepRepo) ClearBatchStockBlock(context.Context, string, string) error {
	f.clearStockBlockCalls++
	return nil
}

func (f *fakeSweepRepo) ListPlannedBatchesNeedingFinalization(context.Context, string, string, bool, bool, int32) ([]domain.PlannedBatchFinalization, error) {
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

func (f *fakeSweepRepo) ListUnbatchedDueForVersion(context.Context, string, string, time.Time, int32) ([]domain.UnbatchedDue, error) {
	return f.rows, nil
}

func (f *fakeSweepRepo) ListUnbatchedShedDueForParkConsolidation(context.Context, string, string, time.Time, int32) ([]domain.ParkConsolidationCandidate, error) {
	return f.parkRows, nil
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
	sopVersionIDs []string
}

func (f *fakeSweepTaskCreator) CreateTaskForBatch(_ context.Context, _, _, sopVersionID, _, _, _, _ string) (string, error) {
	f.calls++
	f.sopVersionIDs = append(f.sopVersionIDs, sopVersionID)
	if f.id == "" {
		f.id = "task-1"
	}
	return f.id, nil
}

type fakeSweepStockReserver struct {
	calls       int
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
	return t.UTC().Format("2006-01-02")
}

func validOnKeys(values []time.Time) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += v.UTC().Format("2006-01-02")
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
